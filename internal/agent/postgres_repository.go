package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository implements Repository on top of a pgxpool.Pool.
// It is the only file in the codebase that speaks SQL; errors leaving
// this boundary are either domain errors or wrapped with %w so callers
// never depend on pgx types.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository injects the application-level pool. The pool is
// created once in main and reused; repositories never create one.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Create inserts the agent and returns the persisted row. RETURNING
// keeps PostgreSQL the source of created_at (its TIMESTAMPTZ DEFAULT).
func (r *PostgresRepository) Create(ctx context.Context, a Agent) (Agent, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO agents (id, name, description)
		 VALUES ($1, $2, $3)
		 RETURNING id, name, description, created_at`,
		a.ID, a.Name, a.Description,
	)

	var created Agent
	if err := row.Scan(&created.ID, &created.Name, &created.Description, &created.CreatedAt); err != nil {
		return Agent{}, fmt.Errorf("create agent %q: %w", a.ID, err)
	}
	return created, nil
}

// GetByID fetches one agent. pgx.ErrNoRows becomes the domain error
// ErrAgentNotFound; nothing outside this package sees pgx errors.
func (r *PostgresRepository) GetByID(ctx context.Context, id string) (Agent, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, description, created_at
		 FROM agents
		 WHERE id = $1`,
		id,
	)

	var a Agent
	err := row.Scan(&a.ID, &a.Name, &a.Description, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, ErrAgentNotFound
	}
	if err != nil {
		return Agent{}, fmt.Errorf("get agent %q: %w", id, err)
	}
	return a, nil
}

// List returns all agents, newest first. The id DESC tie-breaker makes
// the order deterministic when created_at values match.
func (r *PostgresRepository) List(ctx context.Context) ([]Agent, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, description, created_at
		 FROM agents
		 ORDER BY created_at DESC, id DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	// Pre-allocated non-nil so an empty store still encodes as [].
	out := make([]Agent, 0)
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("list agents: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	return out, nil
}
