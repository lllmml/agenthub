package agent

import "context"

// Repository is the persistence abstraction for agents.
type Repository interface {
	Create(ctx context.Context, agent Agent) error
	GetByID(ctx context.Context, id string) (Agent, error)
	List(ctx context.Context) ([]Agent, error)
}
