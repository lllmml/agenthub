package agent

import (
	"context"
	"sync"
	"time"
)

// MemoryRepository stores agents in memory. The HTTP server handles
// requests concurrently, so all access is guarded by a RWMutex.
type MemoryRepository struct {
	mu     sync.RWMutex
	agents map[string]Agent
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{agents: make(map[string]Agent)}
}

func (r *MemoryRepository) Create(ctx context.Context, a Agent) (Agent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// PostgreSQL supplies created_at via its DEFAULT; stamp zero values
	// here so fast tests see the same contract without a database.
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	r.agents[a.ID] = a
	return a, nil
}

func (r *MemoryRepository) GetByID(ctx context.Context, id string) (Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	a, ok := r.agents[id]
	if !ok {
		return Agent{}, ErrAgentNotFound
	}
	return a, nil
}

// List returns a copy of all agents so callers cannot mutate internal
// state through the returned slice. The pre-allocated slice is non-nil,
// which makes an empty store encode as [] rather than null.
func (r *MemoryRepository) List(ctx context.Context) ([]Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Agent, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a)
	}
	return out, nil
}
