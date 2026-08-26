package agent

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
)

// Service implements application logic for agents. It knows nothing
// about HTTP: no response writers, status codes, or JSON encoding.
type Service struct {
	repo Repository
	// nextID is a process-local, collision-free ID generator.
	// A database would replace this later (e.g. UUID or sequence).
	nextID atomic.Uint64
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Create trims and validates the name, generates an ID, and stores
// the agent.
func (s *Service) Create(ctx context.Context, in CreateAgentInput) (Agent, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return Agent{}, ErrInvalidAgentName
	}

	a := Agent{
		ID:          fmt.Sprintf("agent-%d", s.nextID.Add(1)),
		Name:        in.Name,
		Description: in.Description,
	}
	if err := s.repo.Create(ctx, a); err != nil {
		return Agent{}, err
	}
	return a, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (Agent, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]Agent, error) {
	return s.repo.List(ctx)
}
