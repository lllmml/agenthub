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

	// Cache-Aside step 1: bounded cache read. The budget is a context
	// derived from the caller's, so a canceled request cancels the
	// cache operation too, and a slow/broken cache fails within
	// cacheOpTimeout instead of blocking the PostgreSQL fallback.
	cacheCtx, cacheCancel := context.WithTimeout(ctx, cacheOpTimeout)
	defer cacheCancel()

	a, err := s.cache.Get(cacheCtx, canonical)
	if err == nil {
		return a, nil
	}

	// The cache failure may be caused by the caller hanging up or its
	// deadline expiring. That is not a Redis infrastructure problem:
	// stop immediately, do not log a WARN, and do not start
	// PostgreSQL work the request no longer wants.
	if err := ctx.Err(); err != nil {
		return Agent{}, err
	}
	if !errors.Is(err, ErrCacheMiss) {
		slog.Warn("agent cache read failed; falling back to repository", "agent_id", canonical, "error", err)
	}

	// Cache-Aside step 2: load from the authoritative repository.
	a, err = s.repo.GetByID(ctx, canonical)
	if err != nil {
		return Agent{}, err
	}

	// Cache-Aside step 3: fill the cache best-effort with the same
	// bounded, caller-derived budget. A failed write must never turn a
	// successful database read into a failed request. The request may
	// have been canceled while the database read was in flight, in
	// which case the fill is pointless and skipped.
	if ctx.Err() != nil {
		return a, nil
	}
	setCtx, setCancel := context.WithTimeout(ctx, cacheOpTimeout)
	defer setCancel()
	if err := s.cache.Set(setCtx, a); err != nil {
		// The parent context may have been canceled while Set was in
		// flight: that is a canceled request, not a Redis failure, so
		// it must not be reported as a cache infrastructure WARN.
		if ctx.Err() != nil {
			return a, nil
		}
		slog.Warn("agent cache write failed", "agent_id", a.ID, "error", err)
	}
	return a, nil
}

func (s *Service) List(ctx context.Context) ([]Agent, error) {
	return s.repo.List(ctx)
}
