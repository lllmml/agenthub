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
