package agent

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool connects to the dedicated test database selected only by
// TEST_DATABASE_URL. When it is absent the integration tests skip with
// a clear message and ordinary tests still run.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	return pool
}

// applySchema makes the test database match the versioned migration.
// The SQL is read from the migration file so tests exercise exactly
// what production applies. The drop happens only in the database
// selected by TEST_DATABASE_URL and only when integration tests run.
func applySchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS agents`); err != nil {
		t.Fatalf("drop existing agents table: %v", err)
	}

	sql, err := os.ReadFile("../../migrations/000001_create_agents.up.sql")
	if err != nil {
		t.Fatalf("read migration file: %v", err)
	}
	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply up migration: %v", err)
	}
}

func TestPostgresRepositoryCreateReturnsRow(t *testing.T) {
	pool := newTestPool(t)
	applySchema(t, pool)
	repo := NewPostgresRepository(pool)

	created, err := repo.Create(context.Background(), Agent{
		ID:          uuid.NewString(),
		Name:        "paper-assistant",
		Description: "reads papers",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Error("returned row has empty ID")
	}
	if created.Name != "paper-assistant" {
		t.Errorf("name = %q, want %q", created.Name, "paper-assistant")
	}
	if created.CreatedAt.IsZero() {
		t.Error("created_at is zero, want database-supplied timestamp")
	}
	if diff := time.Since(created.CreatedAt); diff < -time.Minute || diff > time.Minute {
		t.Errorf("created_at %v is not close to now", created.CreatedAt)
	}
}

func TestPostgresRepositoryGetByID(t *testing.T) {
	pool := newTestPool(t)
	applySchema(t, pool)
	repo := NewPostgresRepository(pool)

	created, err := repo.Create(context.Background(), Agent{
		ID:   uuid.NewString(),
		Name: "one",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != created {
		t.Errorf("GetByID = %+v, want %+v", got, created)
	}
}

func TestPostgresRepositoryListDeterministicNewestFirst(t *testing.T) {
	pool := newTestPool(t)
	applySchema(t, pool)
	repo := NewPostgresRepository(pool)

	// Insert with explicit created_at values so ordering is asserted
	// deterministically without timing-based sleeps. Two rows share a
	// timestamp to exercise the id DESC tie-breaker.
	olderID := uuid.NewString()
	newerID := uuid.NewString()
	tieID := uuid.NewString()
	base := time.Now().Add(-1 * time.Hour)

	rows := []struct {
		id        string
		createdAt time.Time
	}{
		{olderID, base.Add(-1 * time.Hour)},
		{newerID, base},
		{tieID, base},
	}
	for _, row := range rows {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO agents (id, name, description, created_at)
			 VALUES ($1, $2, '', $3)`,
			row.id, "agent", row.createdAt,
		)
		if err != nil {
			t.Fatalf("seed insert %s: %v", row.id, err)
		}
	}

	got, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3; got %#v", len(got), got)
	}

	// The two same-timestamp rows must come first, ordered by id DESC.
	first, second := tieID, newerID
	if newerID > tieID {
		first, second = newerID, tieID
	}
	if got[0].ID != first || got[1].ID != second || got[2].ID != olderID {
		t.Errorf("order = [%s %s %s], want [%s %s %s]",
			got[0].ID, got[1].ID, got[2].ID, first, second, olderID)
	}
}

func TestPostgresRepositoryGetByIDNotFound(t *testing.T) {
	pool := newTestPool(t)
	applySchema(t, pool)
	repo := NewPostgresRepository(pool)

	_, err := repo.GetByID(context.Background(), uuid.NewString())
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("err = %v, want ErrAgentNotFound", err)
	}
}

// TestPostgresRepositoryRejectsBlankName bypasses the Service and hits
// the database directly: the CHECK constraint is the last line of
// defense behind Service-side validation.
func TestPostgresRepositoryRejectsBlankName(t *testing.T) {
	pool := newTestPool(t)
	applySchema(t, pool)

	_, err := pool.Exec(context.Background(),
		`INSERT INTO agents (id, name, description) VALUES ($1, $2, $3)`,
		uuid.NewString(), "   ", "",
	)
	if err == nil {
		t.Fatal("insert with whitespace-only name succeeded, want CHECK constraint violation")
	}
}

func TestPostgresRepositoryCancellation(t *testing.T) {
	pool := newTestPool(t)
	applySchema(t, pool)
	repo := NewPostgresRepository(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repo.Create(ctx, Agent{ID: uuid.NewString(), Name: "x"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Create with cancelled context err = %v, want context.Canceled", err)
	}
	if _, err := repo.GetByID(ctx, uuid.NewString()); !errors.Is(err, context.Canceled) {
		t.Errorf("GetByID with cancelled context err = %v, want context.Canceled", err)
	}
}
