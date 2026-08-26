package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestMemoryRepository(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	// List starts empty and non-nil.
	got, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("initial List = %#v, want empty non-nil slice", got)
	}

	// GetByID on a missing ID returns ErrAgentNotFound.
	if _, err := repo.GetByID(ctx, "missing"); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("GetByID(missing) err = %v, want ErrAgentNotFound", err)
	}

	a := Agent{ID: "agent-1", Name: "one", Description: "first"}
	created, err := repo.Create(ctx, a)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Error("Create returned zero CreatedAt, want a meaningful timestamp")
	}

	gotAgent, err := repo.GetByID(ctx, "agent-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if gotAgent != created {
		t.Errorf("GetByID = %+v, want %+v", gotAgent, created)
	}

	got, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0] != created {
		t.Errorf("List = %+v, want [%+v]", got, created)
	}
}

// TestMemoryRepositoryConcurrent exercises the mutex under parallel
// writes; it is meaningful when run with -race.
func TestMemoryRepositoryConcurrent(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			a := Agent{ID: fmt.Sprintf("agent-%d", i), Name: fmt.Sprintf("name-%d", i)}
			// t.Errorf is safe from goroutines; t.Fatalf is not.
			if _, err := repo.Create(ctx, a); err != nil {
				t.Errorf("Create: %v", err)
			}
		}(i)
	}
	wg.Wait()

	got, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != n {
		t.Fatalf("len = %d, want %d", len(got), n)
	}
}
