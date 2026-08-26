package agent

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// Service implements application logic for agents. It knows nothing
// about HTTP: no response writers, status codes, or JSON encoding.
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Create trims and validates the name, generates a UUID, and stores
// the agent. UUIDs are safe across process restarts and multiple
// instances, unlike a process-local counter.
func (s *Service) Create(ctx context.Context, in CreateAgentInput) (Agent, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return Agent{}, ErrInvalidAgentName
	}

	a := Agent{
		ID:          uuid.NewString(),
		Name:        in.Name,
		Description: in.Description,
	}
	return s.repo.Create(ctx, a)
}

func (s *Service) GetByID(ctx context.Context, id string) (Agent, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]Agent, error) {
	return s.repo.List(ctx)
}
