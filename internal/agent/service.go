package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

// Service implements application logic for agents. It knows nothing
// about HTTP: no response writers, status codes, or JSON encoding.
type Service struct {
	repo  Repository
	cache AgentCache
}

// NewService wires the authoritative Repository and the optional cache
// in front of it. The cache is a performance optimization; PostgreSQL
// remains the source of truth.
func NewService(repo Repository, cache AgentCache) *Service {
	return &Service{repo: repo, cache: cache}
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

// GetByID returns one agent using Cache-Aside. The ID is parsed and
// normalized to the canonical 36-character lowercase UUID form before
// it reaches the cache or repository: uuid.Parse accepts urn:uuid:...,
// {braced}, and hyphen-free forms that PostgreSQL does not accept as
// raw strings, so passing the original would surface a driver error.
// Malformed IDs map to ErrAgentNotFound without consulting either.
func (s *Service) GetByID(ctx context.Context, id string) (Agent, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return Agent{}, ErrAgentNotFound
	}
	canonical := parsed.String()

	// Cache-Aside step 1: try the cache. A hit avoids PostgreSQL.
	a, err := s.cache.Get(ctx, canonical)
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, ErrCacheMiss) {
		// Infrastructure failure (connection, timeout, corrupt
		// payload): degrade performance, not correctness.
		slog.Warn("agent cache read failed; falling back to repository", "agent_id", canonical, "error", err)
	}

	// The cache failure may itself be caused by the caller hanging up:
	// do not start repository work the request no longer wants.
	if err := ctx.Err(); err != nil {
		return Agent{}, err
	}

	// Cache-Aside step 2: load from the authoritative repository.
	a, err = s.repo.GetByID(ctx, canonical)
	if err != nil {
		return Agent{}, err
	}

	// Cache-Aside step 3: fill the cache best-effort. A failed write
	// must not turn a successful database read into a failed request.
	if err := s.cache.Set(ctx, a); err != nil {
		slog.Warn("agent cache write failed", "agent_id", a.ID, "error", err)
	}
	return a, nil
}

func (s *Service) List(ctx context.Context) ([]Agent, error) {
	return s.repo.List(ctx)
}
