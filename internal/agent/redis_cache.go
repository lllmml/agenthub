package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// cacheKeyPrefix namespaces all Agent cache keys: it identifies the
// application, the entity, and permits future key-format versioning
// without colliding with unrelated Redis data.
const cacheKeyPrefix = "agenthub:agent:v1:"

func cacheKey(id string) string {
	return cacheKeyPrefix + id
}

// RedisAgentCache implements AgentCache on top of a shared *redis.Client.
// It owns all Redis-specific concerns (keys, redis.Nil translation,
// JSON serialization, TTL) and knows nothing about PostgreSQL or HTTP.
type RedisAgentCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisClientOptions parses a redis:// URL and applies the client
// configuration this project relies on for fast failure. It is a
// separate function so main and the integration tests exercise the
// exact same options instead of duplicating constants.
//
// The defaults this overrides matter: go-redis retries failed commands
// up to 3 times by default (MaxRetries 0 means "use default") and, when
// ContextTimeoutEnabled is false, runs socket reads/writes with
// context.Background() instead of the caller's context. Both defaults
// would let a dead Redis block the PostgreSQL fallback for seconds and
// would swallow request cancellation, so neither can be left implicit.
func NewRedisClientOptions(rawURL string) (*redis.Options, error) {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}

	// Single attempt per command: no hidden retries amplifying latency.
	// -1 is the documented value that disables retries (0 means default 3).
	opts.MaxRetries = -1

	// One TCP dial attempt for cache operations.
	// Redis is optional, so repeated dial retries only delay DB fallback.
	opts.DialerRetries = 1

	// Honor the caller-derived context deadline for dials and socket
	// I/O instead of replacing the context with context.Background().
	opts.ContextTimeoutEnabled = true

	// Safety net timeouts for the case where no context deadline is
	// present (e.g. the startup ping). The per-operation budget in the
	// Service is the primary bound; these are fallbacks, not the budget.
	opts.DialTimeout = 1 * time.Second
	opts.ReadTimeout = 1 * time.Second
	opts.WriteTimeout = 1 * time.Second

	return opts, nil
}

// NewRedisAgentCache injects the application-level client and the TTL
// applied to every cached Agent. The client is created once in main
// and reused; caches never create one.
func NewRedisAgentCache(client *redis.Client, ttl time.Duration) *RedisAgentCache {
	return &RedisAgentCache{client: client, ttl: ttl}
}

// Get fetches one Agent by its canonical ID. redis.Nil (missing key)
// becomes ErrCacheMiss; any other error, including undecodable cached
// JSON, is an infrastructure failure so the Service can fall back to
// PostgreSQL instead of serving corrupted data.
func (c *RedisAgentCache) Get(ctx context.Context, id string) (Agent, error) {
	key := cacheKey(id)

	raw, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return Agent{}, ErrCacheMiss
	}
	if err != nil {
		return Agent{}, fmt.Errorf("cache get %q: %w", id, err)
	}

	var a Agent
	if err := json.Unmarshal(raw, &a); err != nil {
		// A malformed cached payload must never surface as a valid
		// Agent. Delete it best-effort so the next read repopulates
		// from PostgreSQL, then report an infrastructure error.
		_ = c.client.Del(ctx, key).Err()
		return Agent{}, fmt.Errorf("cache get %q: decode cached agent: %w", id, err)
	}
	return a, nil
}

// Set stores one Agent as JSON with the cache TTL. A TTL bounds how
// long stale data may be served and gives the cache a natural
// recovery mechanism.
func (c *RedisAgentCache) Set(ctx context.Context, a Agent) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("cache set %q: encode agent: %w", a.ID, err)
	}
	if err := c.client.Set(ctx, cacheKey(a.ID), raw, c.ttl).Err(); err != nil {
		return fmt.Errorf("cache set %q: %w", a.ID, err)
	}
	return nil
}
