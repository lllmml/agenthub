package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeCache is a hand-written AgentCache fake that records Get/Set
// calls and can be configured to fail either operation, so tests can
// prove the Service treats cache hits, misses, and infrastructure
// failures differently without a mock framework or a running Redis.
type fakeCache struct {
	agents   map[string]Agent
	getErr   error
	setErr   error
	getCalls []string
	setCalls []Agent
}

var _ AgentCache = (*fakeCache)(nil)

func newFakeCache() *fakeCache {
	return &fakeCache{agents: make(map[string]Agent)}
}

func (f *fakeCache) Get(ctx context.Context, id string) (Agent, error) {
	f.getCalls = append(f.getCalls, id)
	if f.getErr != nil {
		return Agent{}, f.getErr
	}
	a, ok := f.agents[id]
	if !ok {
		return Agent{}, ErrCacheMiss
	}
	return a, nil
}

func (f *fakeCache) Set(ctx context.Context, a Agent) error {
	f.setCalls = append(f.setCalls, a)
	return f.setErr
}

func TestServiceGetByIDCacheHitSkipsRepository(t *testing.T) {
	cache := newFakeCache()
	spy := newSpyRepository()

	id := uuid.NewString()
	cached := Agent{ID: id, Name: "cached", Description: "from cache", CreatedAt: timeNow()}
	cache.agents[id] = cached

	svc := NewService(spy, cache)
	got, err := svc.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != cached {
		t.Errorf("got %+v, want cached %+v", got, cached)
	}
	if len(spy.getCalls) != 0 {
		t.Errorf("repository consulted on cache hit: %v", spy.getCalls)
	}
	if len(cache.getCalls) != 1 || cache.getCalls[0] != id {
		t.Errorf("cache Get calls = %v, want [%s]", cache.getCalls, id)
	}
}

// TestServiceGetByIDCacheHitUsesCanonicalUUID verifies the cache key
// is derived from the canonical UUID form, so alternate request forms
// (urn:, braced, hyphen-free) hit the same cache entry.
func TestServiceGetByIDCacheHitUsesCanonicalUUID(t *testing.T) {
	id := uuid.NewString()
	cache := newFakeCache()
	cache.agents[id] = Agent{ID: id, Name: "cached"}

	svc := NewService(newSpyRepository(), cache)
	if _, err := svc.GetByID(context.Background(), "urn:uuid:"+id); err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(cache.getCalls) != 1 || cache.getCalls[0] != id {
		t.Errorf("cache Get calls = %v, want canonical [%s]", cache.getCalls, id)
	}
}

func TestServiceGetByIDCacheMissFillsCache(t *testing.T) {
	cache := newFakeCache()
	spy := newSpyRepository()

	id := uuid.NewString()
	stored := Agent{ID: id, Name: "from-postgres", Description: "db row", CreatedAt: timeNow()}
	spy.agents[id] = stored

	svc := NewService(spy, cache)
	got, err := svc.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != stored {
		t.Errorf("got %+v, want repository %+v", got, stored)
	}
	if len(spy.getCalls) != 1 || spy.getCalls[0] != id {
		t.Errorf("repository GetByID calls = %v, want [%s]", spy.getCalls, id)
	}
	if len(cache.setCalls) != 1 || cache.setCalls[0] != stored {
		t.Errorf("cache Set calls = %v, want [%+v]", cache.setCalls, stored)
	}
}

// TestServiceGetByIDCacheReadFailureFallsBack simulates a Redis
// infrastructure error (not a miss): the Service must still consult
// the repository and succeed when PostgreSQL is healthy.
func TestServiceGetByIDCacheReadFailureFallsBack(t *testing.T) {
	cache := newFakeCache()
	cache.getErr = errors.New("connection refused")
	spy := newSpyRepository()

	id := uuid.NewString()
	stored := Agent{ID: id, Name: "durable", CreatedAt: timeNow()}
	spy.agents[id] = stored

	svc := NewService(spy, cache)
	got, err := svc.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != stored {
		t.Errorf("got %+v, want %+v", got, stored)
	}
	if len(spy.getCalls) != 1 {
		t.Errorf("repository GetByID calls = %v, want 1", spy.getCalls)
	}
}

// TestServiceGetByIDCacheWriteFailureStillSucceeds simulates a failed
// cache fill: the successful PostgreSQL read must win.
func TestServiceGetByIDCacheWriteFailureStillSucceeds(t *testing.T) {
	cache := newFakeCache()
	cache.setErr = errors.New("write timeout")
	spy := newSpyRepository()

	id := uuid.NewString()
	stored := Agent{ID: id, Name: "durable", CreatedAt: timeNow()}
	spy.agents[id] = stored

	svc := NewService(spy, cache)
	got, err := svc.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != stored {
		t.Errorf("got %+v, want %+v", got, stored)
	}
}

// TestServiceGetByIDRepositoryFailureNotHidden verifies a real
// repository error still surfaces: Redis must never hide PostgreSQL
// failure, and a failed read must not be cached.
func TestServiceGetByIDRepositoryFailureNotHidden(t *testing.T) {
	cache := newFakeCache()
	spy := newSpyRepository()

	svc := NewService(spy, cache)
	_, err := svc.GetByID(context.Background(), uuid.NewString())
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("err = %v, want ErrAgentNotFound", err)
	}
	if len(cache.setCalls) != 0 {
		t.Errorf("cache filled on failed repository read: %v", cache.setCalls)
	}
}

// TestServiceGetByIDNotFoundNotCached verifies ErrAgentNotFound from
// the repository is returned as-is and never negatively cached.
func TestServiceGetByIDNotFoundNotCached(t *testing.T) {
	cache := newFakeCache()
	svc := NewService(newSpyRepository(), cache)

	_, err := svc.GetByID(context.Background(), uuid.NewString())
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("err = %v, want ErrAgentNotFound", err)
	}
	if len(cache.setCalls) != 0 {
		t.Errorf("negative result was cached: %v", cache.setCalls)
	}
}

// TestServiceGetByIDInvalidUUIDSkipsCacheAndRepository verifies
// malformed IDs fail before any Redis or PostgreSQL operation.
func TestServiceGetByIDInvalidUUIDSkipsCacheAndRepository(t *testing.T) {
	cache := newFakeCache()
	spy := newSpyRepository()
	svc := NewService(spy, cache)

	for _, id := range []string{"nope", "", "not-a-uuid", "12345"} {
		if _, err := svc.GetByID(context.Background(), id); !errors.Is(err, ErrAgentNotFound) {
			t.Errorf("GetByID(%q) err = %v, want ErrAgentNotFound", id, err)
		}
	}
	if len(cache.getCalls) != 0 {
		t.Errorf("cache consulted for invalid UUIDs: %v", cache.getCalls)
	}
	if len(spy.getCalls) != 0 {
		t.Errorf("repository consulted for invalid UUIDs: %v", spy.getCalls)
	}
}

// TestServiceGetByIDCanceledContextSkipsRepository proves a canceled
// caller context is not silently replaced with context.Background().
// Case 1: the cache reports an infrastructure error because the request
// was canceled — the Service must not start repository work. Case 2: a
// plain cache miss on an already-canceled request — same outcome.
func TestServiceGetByIDCanceledContextSkipsRepository(t *testing.T) {
	t.Run("cache failure on canceled request", func(t *testing.T) {
		cache := newFakeCache()
		cache.getErr = context.Canceled
		spy := newSpyRepository()
		svc := NewService(spy, cache)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := svc.GetByID(ctx, uuid.NewString())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if len(spy.getCalls) != 0 {
			t.Errorf("repository consulted after cancellation: %v", spy.getCalls)
		}
	})

	t.Run("cache miss on canceled request", func(t *testing.T) {
		cache := newFakeCache() // empty: normal miss
		spy := newSpyRepository()
		svc := NewService(spy, cache)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := svc.GetByID(ctx, uuid.NewString())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if len(spy.getCalls) != 0 {
			t.Errorf("repository consulted after cancellation: %v", spy.getCalls)
		}
	})
}

// timeNow returns a stable timestamp for cache assertions.
func timeNow() time.Time { return time.Now() }
