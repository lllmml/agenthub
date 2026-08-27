package agent

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// newTestRedisClient connects to the Redis instance selected only by
// TEST_REDIS_URL. When it is absent the integration tests skip with a
// clear message and ordinary tests still run.
func newTestRedisClient(t *testing.T) *redis.Client {
	t.Helper()

	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set; skipping Redis integration tests")
	}

	opts, err := NewRedisClientOptions(url)
	if err != nil {
		t.Fatalf("parse TEST_REDIS_URL: %v", err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping test redis: %v", err)
	}
	return client
}

// cleanupKey removes only the test-created key, never unrelated data.
func cleanupKey(t *testing.T, client *redis.Client, key string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Del(ctx, key).Err(); err != nil {
		t.Errorf("cleanup key %q: %v", key, err)
	}
}

// TestNewRedisClientOptions pins the fast-failure client configuration.
// Without it, go-redis defaults (3 retries with backoff, 5s timeouts,
// context replaced by context.Background()) let a dead Redis block the
// PostgreSQL fallback for seconds. This test runs without a server.
func TestNewRedisClientOptions(t *testing.T) {
	opts, err := NewRedisClientOptions("redis://localhost:6379/0")
	if err != nil {
		t.Fatalf("NewRedisClientOptions: %v", err)
	}
	if opts.MaxRetries != -1 {
		t.Errorf("MaxRetries = %d, want -1 (retries disabled; 0 means the default 3)", opts.MaxRetries)
	}
	if !opts.ContextTimeoutEnabled {
		t.Error("ContextTimeoutEnabled = false, want true (caller-derived deadline must reach Redis I/O)")
	}
	if opts.DialTimeout <= 0 || opts.ReadTimeout <= 0 || opts.WriteTimeout <= 0 {
		t.Errorf("timeouts must be set explicitly, got dial=%v read=%v write=%v",
			opts.DialTimeout, opts.ReadTimeout, opts.WriteTimeout)
	}
	if _, err := NewRedisClientOptions("not a redis url"); err == nil {
		t.Error("invalid URL parsed without error")
	}
}

func TestRedisAgentCacheSetGetRoundTrip(t *testing.T) {
	client := newTestRedisClient(t)

	createdAt := time.Date(2025, 1, 2, 3, 4, 5, 123456789, time.UTC)
	a := Agent{
		ID:          uuid.NewString(),
		Name:        "paper-assistant",
		Description: "reads papers",
		CreatedAt:   createdAt,
	}
	t.Cleanup(func() { cleanupKey(t, client, cacheKey(a.ID)) })

	cache := NewRedisAgentCache(client, time.Minute)
	if err := cache.Set(context.Background(), a); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := cache.Get(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != a {
		t.Errorf("Get = %+v, want %+v (all fields including CreatedAt must round-trip)", got, a)
	}
}

func TestRedisAgentCacheMiss(t *testing.T) {
	client := newTestRedisClient(t)
	cache := NewRedisAgentCache(client, time.Minute)

	// A fresh UUID key cannot exist unless a previous test leaked it.
	_, err := cache.Get(context.Background(), uuid.NewString())
	if !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("err = %v, want ErrCacheMiss", err)
	}
}

// TestRedisAgentCacheTTLExpires uses a short TTL and polls until the
// key is gone, so the test does not depend on exact expiry timing.
func TestRedisAgentCacheTTLExpires(t *testing.T) {
	client := newTestRedisClient(t)
	cache := NewRedisAgentCache(client, 150*time.Millisecond)

	a := Agent{ID: uuid.NewString(), Name: "short-lived"}
	t.Cleanup(func() { cleanupKey(t, client, cacheKey(a.ID)) })

	if err := cache.Set(context.Background(), a); err != nil {
		t.Fatalf("Set: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		_, err := cache.Get(context.Background(), a.ID)
		if errors.Is(err, ErrCacheMiss) {
			return // expired as expected
		}
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("key did not expire within deadline")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestRedisAgentCacheCorruptJSON writes malformed data directly to
// Redis and verifies it is never returned as a valid Agent: Get must
// fail (not a miss, not a valid value) and the corrupt key must be
// removed best-effort so the next read can repopulate from PostgreSQL.
func TestRedisAgentCacheCorruptJSON(t *testing.T) {
	client := newTestRedisClient(t)
	cache := NewRedisAgentCache(client, time.Minute)

	id := uuid.NewString()
	key := cacheKey(id)
	t.Cleanup(func() { cleanupKey(t, client, key) })

	if err := client.Set(context.Background(), key, `{"id": `, 0).Err(); err != nil {
		t.Fatalf("seed corrupt value: %v", err)
	}

	if _, err := cache.Get(context.Background(), id); err == nil {
		t.Fatal("Get succeeded on corrupt payload, want error")
	} else if errors.Is(err, ErrCacheMiss) {
		t.Fatalf("err = ErrCacheMiss, want infrastructure error for corrupt payload")
	}

	n, err := client.Exists(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("check key cleanup: %v", err)
	}
	if n != 0 {
		t.Error("corrupt key was not deleted best-effort")
	}
}
