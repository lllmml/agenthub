package agent

import "context"

// Repository is the persistence abstraction for agents. It must not
// mention pgx or any other storage-specific type.
type Repository interface {
	// Create persists agent and returns the stored row, so the storage
	// layer can supply values callers do not know (created_at).
	Create(ctx context.Context, agent Agent) (Agent, error)
	GetByID(ctx context.Context, id string) (Agent, error)
	List(ctx context.Context) ([]Agent, error)
}
