package agent

import (
	"context"
	"errors"
	"time"
)

// ErrCacheMiss is the sentinel for a normal cache miss (the key simply
// does not exist). It is deliberately distinct from infrastructure
// errors (connection failure, timeout, corrupt payload) so the Service
// can fall back to the Repository only when that is safe.
var ErrCacheMiss = errors.New("cache miss")

// DefaultCacheTTL bounds how long a cached Agent may serve reads before
// the Repository is consulted again. TTL is also the bounded recovery
// mechanism for stale cached data.
const DefaultCacheTTL = 5 * time.Minute

// cacheOpTimeout is the total time budget for one cache operation (a
// Get or a Set). Redis is a performance optimization: a slow or broken
// cache must fail within this budget so the Service can fall back to
// PostgreSQL instead of letting the request hang. The budget is applied
// as a context derived from the caller's (context.WithTimeout(ctx, ...)),
// so the caller's cancellation and deadline always win over it.
const cacheOpTimeout = 250 * time.Millisecond

// AgentCache is the caching abstraction in front of Repository. It is
// a performance optimization only: PostgreSQL remains the source of
// truth, and every cache implementation must fail safe (a miss or an
// error must never be treated as a real Agent).
type AgentCache interface {
	Get(ctx context.Context, id string) (Agent, error)
	Set(ctx context.Context, agent Agent) error
}

// NoopAgentCache never stores anything: Get always reports a miss and
// Set succeeds. main uses it when Redis is not configured or
// unreachable, so the rest of the dependency wiring stays explicit
// while the server keeps serving from PostgreSQL.
type NoopAgentCache struct{}

// NewNoopAgentCache returns an AgentCache that never stores anything,
// for the disabled-cache path in main.
func NewNoopAgentCache() AgentCache {
	return NoopAgentCache{}
}

func (NoopAgentCache) Get(ctx context.Context, id string) (Agent, error) {
	return Agent{}, ErrCacheMiss
}

func (NoopAgentCache) Set(ctx context.Context, agent Agent) error {
	return nil
}
