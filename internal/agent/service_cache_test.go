package agent

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeCache is a hand-written AgentCache fake that records Get/Set
// calls (including the context each operation received) and can be
// configured to fail either operation, so tests can prove the Service
// treats cache hits, misses, and infrastructure failures differently
// without a mock framework or a running Redis.
type fakeCache struct {
	agents map[string]Agent
	getErr error
	setErr error

	// blockOnGet makes Get wait until its context is done and then
	// return ctx.Err(), simulating a hung Redis that only a
	// caller-derived deadline can interrupt.
	blockOnGet bool

	// cancelOnSet, when set, is invoked at the start of Set to
	// simulate the request being canceled while the cache fill is in
	// flight.
	cancelOnSet func()

	getCalls []string
	setCalls []Agent
	getCtx   context.Context
	setCtx   context.Context
}

var _ AgentCache = (*fakeCache)(nil)

func newFakeCache() *fakeCache {
	return &fakeCache{agents: make(map[string]Agent)}
}

func (f *fakeCache) Get(ctx context.Context, id string) (Agent, error) {
	f.getCalls = append(f.getCalls, id)
	f.getCtx = ctx
	if f.blockOnGet {
		<-ctx.Done()
		return Agent{}, ctx.Err()
	}
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
	f.setCtx = ctx
	if f.cancelOnSet != nil {
		f.cancelOnSet()
	}
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
// caller context is not silently replaced with context.Background()
// and stops the request before any repository work.
// Case 1: the cache reports an infrastructure error because the request
// was canceled — the Service must not start repository work. Case 2: a
// plain cache miss on an already-canceled request — same outcome. In
// both cases the cache itself must have observed the cancellation
// through the context it received.
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
		if cache.getCtx == nil || !errors.Is(cache.getCtx.Err(), context.Canceled) {
			t.Errorf("cache did not observe the canceled caller context (got %v); caller context was lost", cache.getCtx)
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
		if cache.getCtx == nil || !errors.Is(cache.getCtx.Err(), context.Canceled) {
			t.Errorf("cache did not observe the canceled caller context (got %v); caller context was lost", cache.getCtx)
		}
		if len(spy.getCalls) != 0 {
			t.Errorf("repository consulted after cancellation: %v", spy.getCalls)
		}
	})
}

// TestServiceGetByIDCacheReceivesCallerDeadline proves the cache
// receives a context derived from the caller's — a WithTimeout child is
// acceptable and expected — so the observed deadline must be present
// and no later than the caller's. A cache that ran on
// context.Background() would fail this test because it would have no
// deadline at all.
func TestServiceGetByIDCacheReceivesCallerDeadline(t *testing.T) {
	cache := newFakeCache()
	spy := newSpyRepository()

	id := uuid.NewString()
	stored := Agent{ID: id, Name: "durable", CreatedAt: timeNow()}
	spy.agents[id] = stored

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	callerDeadline, _ := ctx.Deadline()

	svc := NewService(spy, cache)
	if _, err := svc.GetByID(ctx, id); err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	for name, cctx := range map[string]context.Context{"Get": cache.getCtx, "Set": cache.setCtx} {
		if cctx == nil {
			t.Fatalf("cache.%s did not receive a context", name)
		}
		deadline, ok := cctx.Deadline()
		if !ok {
			t.Errorf("cache.%s context has no deadline; caller deadline was lost", name)
			continue
		}
		if deadline.After(callerDeadline) {
			t.Errorf("cache.%s deadline %v is later than caller deadline %v", name, deadline, callerDeadline)
		}
	}
}

// TestServiceGetByIDDeadlineExceededSkipsRepository covers the
// deadline-exceeded sibling of cancellation: an already-expired caller
// context must stop the request before cache WARN logging and before
// any repository work.
func TestServiceGetByIDDeadlineExceededSkipsRepository(t *testing.T) {
	cache := newFakeCache()
	cache.getErr = context.DeadlineExceeded
	spy := newSpyRepository()
	svc := NewService(spy, cache)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := svc.GetByID(ctx, uuid.NewString())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if len(spy.getCalls) != 0 {
		t.Errorf("repository consulted after deadline expiry: %v", spy.getCalls)
	}
}

// TestServiceGetByIDCacheTimeoutFallsBack simulates a hung Redis whose
// cache operation only returns when its context is done, and proves the
// Service's per-operation budget terminates the cache call within
// ~cacheOpTimeout, then falls back to PostgreSQL. The caller context
// carries a generous 10s deadline so even a regression that passes the
// caller context straight through cannot hang the test forever.
func TestServiceGetByIDCacheTimeoutFallsBack(t *testing.T) {
	capture := captureLogs(t)

	cache := newFakeCache()
	cache.blockOnGet = true
	spy := newSpyRepository()

	id := uuid.NewString()
	stored := Agent{ID: id, Name: "durable", CreatedAt: timeNow()}
	spy.agents[id] = stored

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	svc := NewService(spy, cache)
	start := time.Now()
	got, err := svc.GetByID(ctx, id)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != stored {
		t.Errorf("got %+v, want repository %+v", got, stored)
	}
	if len(spy.getCalls) != 1 {
		t.Errorf("repository GetByID calls = %v, want 1", spy.getCalls)
	}
	// The budget is 250ms; allow generous slack for CI. A default-retry
	// implementation (3 retries with backoff, 5s timeouts) would blow
	// this bound by an order of magnitude.
	if elapsed > time.Second {
		t.Errorf("cache timeout took %v, want bounded well under 1s", elapsed)
	}
	// The parent context stayed valid, so this is a real cache failure:
	// the fallback WARN must be logged.
	if !contains(capture.messages(), "agent cache read failed; falling back to repository") {
		t.Errorf("expected fallback WARN on real cache failure, got %v", capture.messages())
	}
}

// TestServiceCacheCancellationNotLoggedAsWarn proves normal client
// cancellation never pollutes the logs with Redis infrastructure WARNs,
// on both the cache-read path and the cache-fill path.
func TestServiceCacheCancellationNotLoggedAsWarn(t *testing.T) {
	t.Run("cache read canceled", func(t *testing.T) {
		capture := captureLogs(t)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		cache := newFakeCache()
		cache.getErr = context.Canceled
		svc := NewService(newSpyRepository(), cache)

		if _, err := svc.GetByID(ctx, uuid.NewString()); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if contains(capture.messages(), "agent cache read failed; falling back to repository") {
			t.Errorf("canceled request logged a cache read WARN: %v", capture.messages())
		}
	})

	t.Run("cache fill canceled while in flight", func(t *testing.T) {
		capture := captureLogs(t)

		cache := newFakeCache()
		cache.setErr = context.Canceled
		spy := newSpyRepository()
		id := uuid.NewString()
		spy.agents[id] = Agent{ID: id, Name: "durable", CreatedAt: timeNow()}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cache.cancelOnSet = cancel // the request dies while Set is in flight

		svc := NewService(spy, cache)
		if _, err := svc.GetByID(ctx, id); err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if contains(capture.messages(), "agent cache write failed") {
			t.Errorf("canceled request logged a cache write WARN: %v", capture.messages())
		}
	})
}

// slogCapture records slog messages so tests can assert which cache
// failures are logged as infrastructure WARNs and which are silenced
// because they were caused by request cancellation.
type slogCapture struct {
	mu   sync.Mutex
	msgs []string
}

func (c *slogCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *slogCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, r.Message)
	return nil
}

func (c *slogCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *slogCapture) WithGroup(string) slog.Handler      { return c }

func (c *slogCapture) messages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.msgs...)
}

// captureLogs swaps the default slog logger for the duration of the
// test (tests in this package do not run in parallel) and returns the
// capture.
func captureLogs(t *testing.T) *slogCapture {
	t.Helper()
	c := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(c))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return c
}

func contains(msgs []string, want string) bool {
	for _, m := range msgs {
		if m == want {
			return true
		}
	}
	return false
}

// timeNow returns a stable timestamp for cache assertions.
func timeNow() time.Time { return time.Now() }
