# AGENTS.md

## Project: AgentHub

AgentHub is a production-oriented AI backend project written in Go.

The long-term goal is to build a high-quality backend project that demonstrates real backend engineering skills and can be used as a strong internship/resume project.

The project is intentionally developed incrementally.

Previous milestones established:

### Day 1 — HTTP foundation

```text
Client
  ↓
net/http
  ↓
ServeMux
  ↓
Handler
  ↓
Service
  ↓
Repository
```

Day 1 introduced:

* Go standard `net/http`;
* modern `http.ServeMux`;
* JSON APIs;
* Handler → Service → Repository separation;
* request `context.Context` propagation;
* graceful HTTP shutdown;
* concurrency-safe `MemoryRepository`;
* unit and HTTP tests.

### Day 2 — PostgreSQL persistence

The runtime persistence layer was replaced with PostgreSQL while preserving the existing architecture:

```text
HTTP
  ↓
Handler
  ↓
Service
  ↓
Repository interface
  ↓
PostgresRepository
  ↓
pgxpool.Pool
  ↓
PostgreSQL
```

Day 2 introduced:

* PostgreSQL as the runtime source of truth;
* `pgxpool.Pool`;
* SQL migrations;
* `PostgresRepository`;
* UUID Agent IDs;
* `created_at`;
* PostgreSQL integration tests;
* mandatory `DATABASE_URL`;
* fail-fast behavior when PostgreSQL is unavailable.

Do not undo or bypass the Day 1 / Day 2 architecture.

---

# 1. Day 3 Goal

Today's milestone introduces **Redis as a cache in front of PostgreSQL**.

The goal is NOT to turn Redis into another database.

The goal is to implement and understand the Cache-Aside pattern:

```text
GET /api/v1/agents/{id}
             │
             ▼
           Service
             │
             ▼
        Redis Cache
         /       \
       hit       miss
       │           │
       │           ▼
       │       Repository
       │           │
       │           ▼
       │      PostgreSQL
       │           │
       │           ▼
       │       cache.Set
       │           │
       └───────────┴────→ return Agent
```

PostgreSQL remains the **Source of Truth**.

Redis is only a performance optimization.

If Redis becomes unavailable:

```text
Redis failure
     ↓
PostgreSQL
     ↓
request still succeeds
```

The system should experience degraded performance rather than unnecessary business downtime.

This is **graceful degradation**.

---

# 2. Today's Required Outcome

By the end of Day 3:

* Redis can be started locally with Docker Compose;
* the application can connect to Redis;
* Agent caching is hidden behind an `AgentCache` abstraction;
* `GET /api/v1/agents/{id}` uses Cache-Aside;
* cache hit avoids PostgreSQL;
* cache miss loads from PostgreSQL and fills Redis;
* Redis failures fall back to PostgreSQL;
* cache write failures do not fail otherwise-successful reads;
* cached values have a TTL;
* cache miss is distinguished from cache infrastructure failure;
* request cancellation continues to propagate correctly;
* Redis integration tests are available;
* ordinary unit tests do not require Redis;
* README documents the architecture and local setup.

The public HTTP contract must remain unchanged.

---

# 3. Current API Contract Must Stay Unchanged

Keep the existing endpoints:

```text
GET  /health

GET  /api/v1/agents

POST /api/v1/agents

GET  /api/v1/agents/{id}
```

Do NOT add new API endpoints today.

In particular, do NOT add:

```text
PUT    /api/v1/agents/{id}
PATCH  /api/v1/agents/{id}
DELETE /api/v1/agents/{id}
```

just to demonstrate cache invalidation.

Cache invalidation will be introduced naturally when the business model actually supports mutation.

Do not invent business features to justify infrastructure.

---

# 4. Important Scope Restrictions

Do NOT add any of the following unless explicitly requested:

* RabbitMQ;
* Kafka;
* NATS;
* gRPC;
* Kubernetes;
* microservices;
* Redis Cluster;
* Redis Sentinel;
* distributed locks;
* Redlock;
* rate limiting;
* sessions;
* background cache refresh;
* write-behind caching;
* write-through caching;
* CDC;
* Outbox Pattern;
* cache invalidation workers;
* `singleflight`;
* negative caching;
* caching the Agent list;
* OpenTelemetry;
* Prometheus;
* Grafana;
* authentication;
* JWT;
* Gin;
* Echo;
* Fiber;
* ORM frameworks;
* RAG;
* pgvector;
* LLM calls;
* LangChain.

Those concepts may appear in future milestones.

Today is specifically about:

```text
Redis
+
Cache Aside
+
TTL
+
failure handling
+
context propagation
```

Keep the implementation small and explainable.

---

# 5. Preserve Day 2 PostgreSQL Behavior

PostgreSQL remains mandatory for runtime operation.

Keep:

```text
DATABASE_URL
```

mandatory.

The server MUST NOT silently fall back to `MemoryRepository` if PostgreSQL is unavailable.

Correct:

```text
PostgreSQL unavailable
        ↓
application startup fails
```

because PostgreSQL stores authoritative application data.

Redis is different.

Correct:

```text
Redis unavailable
       ↓
cache disabled/degraded
       ↓
PostgreSQL still serves requests
```

This distinction is intentional and important.

---

# 6. Target Architecture

The desired architecture after Day 3 is:

```text
                    ┌──────────────┐
                    │    Redis     │
                    │    Cache     │
                    └──────▲───────┘
                           │
                           │
Client
  │
  ▼
net/http
  │
  ▼
Handler
  │
  ▼
Service ───────────────────┘
  │
  ▼
Repository interface
  │
  ▼
PostgresRepository
  │
  ▼
pgxpool.Pool
  │
  ▼
PostgreSQL
```

The important architectural rule is:

> Cache and Repository are different abstractions.

Do NOT hide Redis inside `PostgresRepository`.

Do NOT make PostgreSQL repository code know Redis exists.

Do NOT put Redis operations in HTTP handlers.

---

# 7. Expected Project Structure

Prefer extending the existing domain package rather than creating many new generic packages.

A reasonable target is:

```text
agenthub/
├── cmd/
│   └── server/
│       └── main.go
│
├── internal/
│   ├── agent/
│   │   ├── model.go
│   │   ├── repository.go
│   │   ├── memory_repository.go
│   │   ├── postgres_repository.go
│   │   ├── cache.go
│   │   ├── redis_cache.go
│   │   ├── service.go
│   │   ├── handler.go
│   │   │
│   │   ├── service_test.go
│   │   ├── redis_cache_test.go
│   │   └── ...
│   │
│   └── httpx/
│       └── response.go
│
├── migrations/
│   └── ...
│
├── compose.yaml
├── go.mod
├── go.sum
├── README.md
└── AGENTS.md
```

Minor deviations are acceptable when clearly justified.

Do not create packages named things such as:

```text
common
utils
manager
helpers
infrastructure
core
```

without a strong reason.

---

# 8. Redis Dependency

Use the official/common Go Redis client:

```text
github.com/redis/go-redis/v9
```

Do not introduce multiple Redis libraries.

Do not introduce a caching framework.

We want to understand Redis directly.

---

# 9. AgentCache Abstraction

Create an Agent-specific cache abstraction.

A reasonable shape is:

```go
type AgentCache interface {
    Get(ctx context.Context, id string) (Agent, error)
    Set(ctx context.Context, agent Agent) error
}
```

The exact names may differ slightly if existing naming conventions suggest something better.

Do NOT add methods that are not needed today.

In particular, do not add:

```go
Delete(...)
Invalidate(...)
Flush(...)
List(...)
Lock(...)
```

just because they might be useful later.

Interfaces should describe capabilities currently required by the consumer.

---

# 10. Cache Miss Must Be Explicit

Define a domain/cache sentinel error for a normal cache miss.

For example:

```go
var ErrCacheMiss = errors.New("cache miss")
```

The Redis implementation should translate:

```text
redis.Nil
```

into:

```text
ErrCacheMiss
```

Callers must be able to distinguish:

```text
cache miss
```

from:

```text
Redis connection failure
Redis timeout
serialization failure
other cache infrastructure error
```

Do NOT treat every Redis error as a cache miss.

Do NOT compare error strings.

Use:

```go
errors.Is(...)
```

where appropriate.

---

# 11. Redis Cache Key

Use a stable namespaced key.

For example:

```text
agenthub:agent:v1:{uuid}
```

Example:

```text
agenthub:agent:v1:550e8400-e29b-41d4-a716-446655440000
```

Reasons:

* avoids collisions with unrelated Redis data;
* identifies the application;
* identifies the entity;
* permits future key-format versioning.

Do not use an unnecessarily complex key scheme.

---

# 12. Cache Serialization

Store the cached Agent as JSON.

The cached value must preserve at least:

```go
Agent{
    ID
    Name
    Description
    CreatedAt
}
```

Use standard library:

```go
encoding/json
```

Do not introduce Protobuf, MessagePack, Gob, or another serialization layer today.

If cached JSON cannot be decoded:

* do not return corrupted data;
* treat Redis as unusable for this read;
* allow the Service to fall back to PostgreSQL;
* optionally delete the corrupted key best-effort inside the Redis cache implementation if this remains simple.

Do not allow malformed cached data to break the business request when PostgreSQL is still healthy.

---

# 13. TTL

Cached Agent records MUST have a TTL.

Use a simple default such as:

```text
5 minutes
```

A reasonable implementation is to inject the TTL into `RedisAgentCache` when constructing it.

For example conceptually:

```go
NewRedisAgentCache(client, 5*time.Minute)
```

Do not create a complicated configuration framework.

The TTL exists for more than memory management.

It also provides a bounded recovery mechanism for stale cache data.

Do not cache Agents forever.

---

# 14. Cache-Aside Read Flow

Modify `Service.GetByID`.

Preserve the existing UUID parsing and canonicalization behavior.

The desired logical flow is:

```text
GetByID(ctx, id)

        ↓

parse UUID

        ↓ invalid

ErrAgentNotFound


valid UUID

        ↓

cache.Get(ctx, canonicalID)

     /             \
   hit             miss
    │                │
 return          repo.GetByID
                     │
                     ▼
                   Agent
                     │
                     ▼
                 cache.Set
                     │
                     ▼
                  return
```

More explicitly:

```text
1. Validate and normalize UUID.

2. Attempt cache read.

3. If cache hit:
      return cached Agent.

4. If ErrCacheMiss:
      query Repository.

5. If Redis infrastructure error:
      log the cache problem;
      continue to Repository.

6. If Repository returns an Agent:
      attempt to cache it.

7. If cache.Set fails:
      log the error;
      still return the Agent.

8. If Repository fails:
      preserve the existing repository/domain error behavior.
```

PostgreSQL remains authoritative.

---

# 15. Request Cancellation Is More Important Than Cache Fallback

Do not blindly do this:

```text
Redis error
   ↓
always query PostgreSQL
```

A Redis error may occur because the HTTP request has already been canceled.

Before performing fallback work, respect:

```go
ctx.Err()
```

If the parent request context is canceled or has expired:

```text
Client disconnected
        ↓
request context canceled
        ↓
Redis operation stops
        ↓
do not start unnecessary PostgreSQL work
```

The implementation should preserve this principle.

Never replace request context with:

```go
context.Background()
```

inside the HTTP → Service → Cache/Repository request path.

Correct:

```text
r.Context()
   ↓
Service
   ↓
Cache
```

and:

```text
r.Context()
   ↓
Service
   ↓
Repository
```

---

# 16. Redis Operations Should Be Bounded

Redis is an optimization.

A slow or broken cache should not make database-backed reads dramatically slower.

Use reasonable bounded Redis operation behavior.

If a cache-specific timeout is introduced, it MUST derive from the caller's context:

```go
cacheCtx, cancel := context.WithTimeout(ctx, ...)
defer cancel()
```

Never:

```go
context.WithTimeout(context.Background(), ...)
```

for request-scoped operations.

If a cache-specific timeout expires while the parent request context is still valid, PostgreSQL fallback is acceptable.

Do not obsess over production timeout tuning today.

The important concept is:

> cache failure should fail fast enough to permit database fallback.

---

# 17. Graceful Degradation

Redis MUST NOT become a hard dependency for serving Agent data.

There are two failure situations to consider.

## Redis unavailable during startup

If Redis configuration exists but Redis cannot be reached:

```text
log warning
    ↓
disable/bypass cache
    ↓
continue startup with PostgreSQL
```

The server should still operate.

A small no-op cache implementation is acceptable if it keeps dependency wiring explicit.

For example conceptually:

```text
AgentCache
    ▲
    ├── RedisAgentCache
    └── NoopAgentCache
```

Do not create a complicated cache provider framework.

## Redis fails after startup

If Redis was working and later becomes unavailable:

```text
cache.Get error
       ↓
log warning
       ↓
PostgreSQL fallback
```

and:

```text
PostgreSQL success
       ↓
cache.Set error
       ↓
log warning
       ↓
return PostgreSQL result anyway
```

Redis failure should degrade performance, not correctness.

---

# 18. Redis Configuration

Prefer one simple environment variable:

```text
REDIS_URL
```

Example:

```text
redis://localhost:6379/0
```

Behavior:

### `REDIS_URL` absent

Redis caching is disabled.

The server still starts normally.

### `REDIS_URL` present and Redis reachable

Enable Redis caching.

### `REDIS_URL` present but Redis unreachable

Log a clear warning and continue without startup failure.

Do not commit credentials.

Do not add secrets to source code.

---

# 19. Docker Compose

Add a small:

```text
compose.yaml
```

for local Redis development.

The Redis service should:

* use the official Redis image;
* expose port `6379`;
* include a simple health check if practical;
* be easy to start with:

```bash
docker compose up -d redis
```

and stop with:

```bash
docker compose down
```

Redis is being used as a cache, so do NOT add unnecessary persistence configuration today.

Do not configure:

* Redis Cluster;
* Sentinel;
* replication;
* AOF tuning;
* RDB tuning;
* TLS;
* ACL complexity.

The goal is local development and integration testing.

Do not unnecessarily rewrite the existing PostgreSQL development workflow as part of this milestone.

---

# 20. Do Not Cache List Today

Keep:

```text
GET /api/v1/agents
```

using PostgreSQL through the existing Repository path.

Do NOT cache:

```text
List()
```

today.

List caching creates additional questions involving:

```text
creation invalidation
pagination
ordering
staleness
multi-key invalidation
```

Those are not today's learning objective.

Only cache:

```text
GET /api/v1/agents/{id}
```

---

# 21. Do Not Add Negative Caching Today

If PostgreSQL returns:

```text
ErrAgentNotFound
```

do not cache that result today.

Do not create:

```text
agenthub:agent:not-found:{id}
```

or similar entries.

Negative caching can reduce database load but introduces additional consistency and TTL design issues.

Defer it.

---

# 22. Do Not Add singleflight Today

There is an important future problem:

```text
hot key expires
      ↓
many concurrent cache misses
      ↓
many PostgreSQL requests
```

This is Cache Stampede / Cache Breakdown.

We will address this later with concepts such as:

```text
singleflight
```

Do NOT implement it today.

The Day 3 implementation should leave the code in a state where `singleflight` could be introduced later without major architectural changes.

---

# 23. Logging

Continue using Go standard library:

```go
log/slog
```

Useful cache logs include:

```text
cache hit
cache miss
cache read failed
cache write failed
Redis unavailable at startup
Redis cache enabled
```

Use sensible log levels.

For example:

```text
cache hit/miss       → Debug
cache unavailable    → Warn
cache operation fail → Warn
```

Do not log entire cached JSON payloads.

Do not log credentials or Redis URLs containing passwords.

Do not introduce Zap, Logrus, or another logging dependency.

Do not build a logger abstraction framework today.

---

# 24. Service Responsibilities After Day 3

The Service now coordinates application-level retrieval logic.

Conceptually:

```text
Service
 ├── validates Agent ID
 ├── normalizes UUID
 ├── checks AgentCache
 ├── falls back to Repository
 └── fills cache after DB read
```

The Service must still know nothing about:

```text
http.ResponseWriter
HTTP status codes
Redis command syntax
pgx.Row
SQL
```

The Service should depend on abstractions, not Redis client details.

---

# 25. RedisAgentCache Responsibilities

`RedisAgentCache` should own Redis-specific details such as:

* Redis keys;
* `redis.Nil`;
* Redis commands;
* JSON serialization;
* TTL;
* Redis-specific error wrapping.

It should NOT:

* query PostgreSQL;
* know about HTTP;
* return HTTP status codes;
* contain Agent business validation;
* call the Repository;
* generate Agent IDs.

---

# 26. Repository Responsibilities Must Stay Unchanged

The Repository abstraction represents authoritative persistence.

It should remain conceptually:

```go
type Repository interface {
    Create(ctx context.Context, agent Agent) (Agent, error)
    GetByID(ctx context.Context, id string) (Agent, error)
    List(ctx context.Context) ([]Agent, error)
}
```

Do not modify this interface just to add caching.

Do not add Redis concepts to Repository.

`PostgresRepository` should continue to be the only implementation that speaks SQL.

---

# 27. Creation Flow

Do NOT complicate `Service.Create` today.

The authoritative flow remains:

```text
POST /agents
    ↓
Service
    ↓
Repository
    ↓
PostgreSQL
```

It is acceptable for the first subsequent GET to produce:

```text
cache miss
   ↓
PostgreSQL
   ↓
cache fill
```

Do not add Redis to the write path merely for the sake of using Redis more often.

---

# 28. Tests Required

Tests are a major part of this milestone.

Ordinary tests must remain fast and must NOT require a running Redis instance.

Use hand-written fakes/stubs where appropriate.

Do not introduce a mocking framework unless there is a compelling existing reason.

---

## 28.1 Service: Cache Hit

Test:

```text
cache contains Agent
      ↓
Service.GetByID
      ↓
returns cached Agent
```

Verify:

* Repository `GetByID` is NOT called;
* correct Agent is returned;
* canonical UUID behavior is preserved.

This proves Redis actually prevents database access.

---

## 28.2 Service: Cache Miss

Test:

```text
cache.Get
    ↓
ErrCacheMiss
    ↓
Repository.GetByID
    ↓
Agent
    ↓
cache.Set
    ↓
return Agent
```

Verify:

* Repository is called exactly as expected;
* returned Agent comes from Repository;
* cache Set receives the retrieved Agent.

---

## 28.3 Service: Redis Read Failure

Simulate:

```text
cache.Get
    ↓
connection/timeout error
```

Verify:

```text
Repository.GetByID
```

is still called.

If Repository succeeds, the Service should succeed.

---

## 28.4 Service: Redis Write Failure

Simulate:

```text
cache miss
    ↓
Repository success
    ↓
cache.Set failure
```

Verify:

* Service still returns the PostgreSQL Agent;
* cache Set failure does not become a business failure.

---

## 28.5 Repository Failure

If cache misses and Repository returns a real error:

```text
Service
```

must return that error.

Redis must not hide PostgreSQL failure.

---

## 28.6 Agent Not Found

Verify existing behavior remains:

```text
valid UUID
    ↓
cache miss
    ↓
Repository
    ↓
ErrAgentNotFound
```

returns:

```text
ErrAgentNotFound
```

to the Handler layer.

Do not change the existing HTTP `404` contract.

---

## 28.7 Invalid UUID

Existing Day 2 behavior must remain.

For malformed IDs:

```text
Service.GetByID
      ↓
UUID validation fails
      ↓
ErrAgentNotFound
```

Verify malformed IDs do NOT result in unnecessary Redis or PostgreSQL operations.

---

## 28.8 Context Cancellation

Add a test proving cancellation is respected.

Conceptually:

```text
request context canceled
        ↓
cache operation fails/cancels
        ↓
Service does not start unnecessary DB work
```

At minimum, ensure canceled caller contexts are not silently replaced with `context.Background()`.

---

# 29. Redis Integration Tests

Add focused integration tests for `RedisAgentCache`.

Use an environment variable such as:

```text
TEST_REDIS_URL
```

Example:

```bash
export TEST_REDIS_URL='redis://localhost:6379/0'
```

If `TEST_REDIS_URL` is absent:

```go
t.Skip(...)
```

with a clear message.

Integration tests may verify:

### Set and Get

```text
Set Agent
  ↓
Get Agent
  ↓
equal values
```

including:

```text
ID
Name
Description
CreatedAt
```

### Cache Miss

Request a key that does not exist.

Verify:

```text
ErrCacheMiss
```

using:

```go
errors.Is(...)
```

### TTL

Use a short TTL in the test.

Verify the key eventually expires.

Keep the test reasonably fast and reliable.

### Corrupt JSON

Optional but valuable:

* write malformed data directly using Redis;
* call `RedisAgentCache.Get`;
* verify corrupted data is not returned as a valid Agent.

Integration tests MUST NOT flush a Redis instance indiscriminately if that could destroy unrelated developer data.

Prefer unique key prefixes/UUIDs and cleanup only test-created keys.

---

# 30. Local Integration Test Workflow

The following workflow should be documented and work:

```bash
docker compose up -d redis
```

Then:

```bash
export TEST_REDIS_URL='redis://localhost:6379/0'
```

Then:

```bash
go test -v ./internal/agent -run Redis
```

or the equivalent test command matching actual test names.

Ordinary tests must continue working without Redis:

```bash
go test ./...
```

---

# 31. Existing PostgreSQL Integration Tests Must Continue Working

Do not weaken Day 2 database tests.

Keep the existing safety rule that destructive PostgreSQL integration tests only operate against an explicitly dedicated `_test` database.

Day 3 must not change this behavior.

---

# 32. Runtime Dependency Wiring

`cmd/server/main.go` remains the composition root.

Conceptually:

```text
DATABASE_URL
     ↓
pgxpool.Pool
     ↓
PostgresRepository
     │
     │
     ├──────────────┐
     │              │
     │          Redis config
     │              ↓
     │        Redis client
     │              ↓
     │        RedisAgentCache
     │              │
     └──────┐       │
            ▼       ▼
              Service
                 ↓
               Handler
                 ↓
              ServeMux
                 ↓
            http.Server
```

Dependencies should be constructed explicitly.

Avoid global mutable clients.

Create one application-level PostgreSQL pool.

Create at most one application-level Redis client.

Reuse them across requests.

Close resources during application shutdown.

---

# 33. Redis Startup Behavior

If:

```text
REDIS_URL
```

is configured, perform a bounded startup connectivity check.

Do NOT allow Redis Ping to hang startup indefinitely.

Example conceptually:

```text
small timeout context
       ↓
PING Redis
```

If successful:

```text
enable Redis cache
```

If unsuccessful:

```text
log warning
close unusable client if appropriate
use disabled/no-op cache
continue startup
```

Do not call:

```text
os.Exit
```

just because Redis is unavailable.

PostgreSQL startup failure and Redis startup failure intentionally have different semantics.

---

# 34. Resource Lifecycle

During graceful shutdown:

```text
HTTP server stops accepting requests
        ↓
in-flight HTTP requests drain
        ↓
Redis client closes
        ↓
PostgreSQL pool closes
```

Exact `defer` ordering may differ if correct.

Do not introduce resource leaks.

Do not create a Redis client per HTTP request.

Do not create a PostgreSQL pool per HTTP request.

---

# 35. Error Design

Continue using sentinel/domain errors where appropriate.

At minimum there should be a clear distinction between:

```text
ErrAgentNotFound
ErrInvalidAgentName
ErrCacheMiss
```

Infrastructure errors should be wrapped with context using:

```go
fmt.Errorf("...: %w", err)
```

where useful.

Do not expose Redis implementation errors directly in HTTP responses.

A Redis outage should normally never reach the Handler as a user-facing failure when PostgreSQL successfully serves the request.

---

# 36. HTTP Behavior Must Not Change

Existing handler tests should continue to pass.

Examples:

```text
GET /health
```

still returns `200`.

```text
POST /api/v1/agents
```

still returns `201`.

```text
GET /api/v1/agents/{valid-id}
```

still returns `200` when the Agent exists.

Missing Agent still produces the existing JSON `404`.

Redis must remain invisible to API consumers.

---

# 37. README Requirements

Update README with a concise Day 3 section.

Explain the new request path:

```text
Client
  -> Handler
  -> Service
  -> Redis
       hit  -> return
       miss -> PostgreSQL -> Redis fill -> return
```

Document:

### Redis role

Explain clearly:

> Redis is a cache, not the source of truth.

### PostgreSQL role

Explain clearly:

> PostgreSQL remains authoritative persistent storage.

### Environment variables

Document:

```text
DATABASE_URL
REDIS_URL
TEST_DATABASE_URL
TEST_REDIS_URL
```

with safe local examples.

### Start Redis

```bash
docker compose up -d redis
```

### Run server with Redis

Example:

```bash
export DATABASE_URL='...'
export REDIS_URL='redis://localhost:6379/0'

go run ./cmd/server
```

### Run ordinary tests

```bash
go test ./...
```

### Run Redis integration tests

Document the actual command.

### Graceful degradation

Explain:

```text
Redis unavailable
    ↓
PostgreSQL fallback
```

Do not turn README into a giant Redis tutorial.

---

# 38. Quality Gates

Before considering the milestone complete, run:

```bash
go mod tidy
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
```

All ordinary tests should pass without requiring Redis or PostgreSQL integration-test environment variables.

When a test Redis instance is available, also run the Redis integration tests.

If PostgreSQL integration-test configuration is available, ensure existing PostgreSQL tests still pass.

Do not declare completion while known test failures remain.

---

# 39. Suggested Implementation Order

Follow this order unless existing code strongly suggests otherwise.

## Step 1

Inspect the current repository thoroughly.

Read at minimum:

```text
internal/agent/model.go
internal/agent/repository.go
internal/agent/postgres_repository.go
internal/agent/memory_repository.go
internal/agent/service.go
internal/agent/service_test.go
internal/agent/handler.go
cmd/server/main.go
README.md
go.mod
migrations/
```

Do not code based only on this document.

The repository is the source of truth for current implementation details.

## Step 2

Add the Redis dependency.

## Step 3

Define:

```text
AgentCache
ErrCacheMiss
```

## Step 4

Implement `RedisAgentCache`.

Include:

```text
Get
Set
JSON serialization
TTL
Redis Nil translation
error wrapping
```

## Step 5

Add small fake cache implementations inside service tests.

## Step 6

Modify `Service` dependency injection to accept the cache abstraction.

## Step 7

Implement Cache-Aside in:

```text
Service.GetByID
```

while preserving UUID normalization.

## Step 8

Add service tests for:

```text
hit
miss
Redis failure
cache Set failure
repository failure
invalid UUID
context cancellation
```

## Step 9

Add Redis integration tests.

## Step 10

Add local Redis Docker Compose configuration.

## Step 11

Wire Redis in `cmd/server/main.go`.

## Step 12

Implement startup degradation behavior.

## Step 13

Review graceful shutdown/resource closing.

## Step 14

Update README.

## Step 15

Run all quality gates.

## Step 16

Review the final diff and remove unnecessary complexity.

---

# 40. Learning-Oriented Requirements

This is a learning project.

The code should make the following flow easy to explain during a backend interview:

```text
HTTP request
    ↓
r.Context()
    ↓
Handler
    ↓
Service
    ↓
validate UUID
    ↓
Redis cache
  /        \
hit        miss/error
 │             │
 │             ▼
 │       Repository
 │             ↓
 │       PostgreSQL
 │             ↓
 │        cache fill
 │             │
 └─────────────┘
        ↓
    JSON response
```

The resulting implementation should make it possible to clearly answer:

1. Why is PostgreSQL the Source of Truth?

2. Why is Redis not treated as the primary database?

3. What is Cache-Aside?

4. What is a cache hit?

5. What is a cache miss?

6. Why must `redis.Nil` be different from a Redis connection error?

7. Why can Redis failure fall back to PostgreSQL?

8. Why should PostgreSQL startup failure still fail the application?

9. Why does cached data need a TTL?

10. Why are Redis keys namespaced?

11. Why should malformed UUIDs be rejected before accessing cache/storage?

12. Why must `context.Context` propagate into Redis?

13. Why shouldn't a canceled request continue querying PostgreSQL?

14. Why don't we cache `List()` yet?

15. What is Cache Stampede, even though we are not solving it today?

16. Where could `singleflight` be introduced later?

17. Why shouldn't Redis logic live inside `PostgresRepository`?

18. Why shouldn't handlers know whether Redis exists?

If the implementation makes these questions difficult to answer, reconsider the architecture.

---

# 41. Coding Style

Follow idiomatic Go.

Prefer:

* small functions;
* explicit dependency injection;
* early returns;
* `errors.Is`;
* `%w` error wrapping;
* interfaces near their consumer;
* standard library where practical;
* `log/slog`;
* table-driven tests when useful;
* request context propagation;
* small focused test fakes;
* clear ownership of resources.

Avoid:

* dependency injection frameworks;
* global mutable clients;
* huge constructors;
* generic repository frameworks;
* reflection-heavy abstractions;
* premature generics;
* giant `utils` packages;
* meaningless interfaces;
* duplicated serialization code;
* hidden retries;
* magic fallback behavior.

Comments should explain **why**, not simply restate what the code does.

---

# 42. Git Safety

Do not:

* merge branches;
* rebase branches;
* force push;
* rewrite Git history;
* delete branches;
* push directly to remote;
* create commits unless explicitly asked.

Only modify the working tree required for this milestone.

Leave branch/PR/merge decisions to the user unless explicitly instructed otherwise.

---

# 43. Explicit Non-Goals for Day 3

The following are intentionally deferred:

```text
Agent update API
Agent delete API
cache invalidation
double delete
distributed cache consistency
singleflight
cache stampede protection
cache penetration protection
negative caching
Bloom filters
rate limiting
distributed locks
Redis Streams
Redis Pub/Sub
Redis Cluster
Redis Sentinel
RabbitMQ
Kafka
LLM Gateway
SSE streaming
RAG
OpenTelemetry
Prometheus
Grafana
Kubernetes
microservices
```

Do not implement them.

It is acceptable to mention them briefly in comments or README only when necessary to explain a deliberate limitation.

---

# 44. Definition of Done

Day 3 is complete only when all of the following are true:

* [ ] Existing Day 1 HTTP behavior still works.
* [ ] Existing Day 2 PostgreSQL persistence still works.
* [ ] PostgreSQL remains the runtime source of truth.
* [ ] `DATABASE_URL` remains mandatory.
* [ ] Redis support uses a single clear client dependency.
* [ ] Redis can be started locally via Docker Compose.
* [ ] Redis configuration is provided through environment variables.
* [ ] Missing Redis configuration does not prevent server startup.
* [ ] Redis startup failure does not prevent server startup.
* [ ] `AgentCache` exists as a separate abstraction from `Repository`.
* [ ] `RedisAgentCache` implements `AgentCache`.
* [ ] Cache keys are namespaced.
* [ ] Cached Agents use JSON serialization.
* [ ] Cached Agents have a TTL.
* [ ] Redis `nil`/missing key maps to `ErrCacheMiss`.
* [ ] Cache miss is distinguished from Redis infrastructure failure.
* [ ] `Service.GetByID` preserves UUID parsing/normalization.
* [ ] Cache hit avoids Repository access.
* [ ] Cache miss queries Repository.
* [ ] Successful database reads fill Redis best-effort.
* [ ] Redis read failure falls back to PostgreSQL.
* [ ] Redis write failure does not fail a successful database read.
* [ ] Caller cancellation is respected.
* [ ] Request context is propagated into Redis operations.
* [ ] `GET /api/v1/agents` is NOT cached.
* [ ] Agent not-found results are NOT negatively cached.
* [ ] No new update/delete HTTP API was added.
* [ ] No `singleflight` was added.
* [ ] Ordinary unit tests run without Redis.
* [ ] Service tests cover cache hit.
* [ ] Service tests cover cache miss.
* [ ] Service tests cover Redis read failure.
* [ ] Service tests cover Redis write failure.
* [ ] Service tests preserve existing not-found behavior.
* [ ] Service tests preserve invalid UUID behavior.
* [ ] Redis integration tests are available.
* [ ] Redis integration tests skip clearly when `TEST_REDIS_URL` is absent.
* [ ] Existing PostgreSQL integration-test safety behavior remains intact.
* [ ] Redis resources are closed correctly.
* [ ] PostgreSQL resources are still closed correctly.
* [ ] README explains Cache-Aside.
* [ ] README explains PostgreSQL vs Redis responsibility.
* [ ] README documents local Redis setup.
* [ ] `go mod tidy` succeeds.
* [ ] `go fmt ./...` succeeds.
* [ ] `go vet ./...` succeeds.
* [ ] `go test ./...` succeeds.
* [ ] `go test -race ./...` succeeds.
* [ ] No unrelated infrastructure was introduced.
* [ ] No unnecessary business features were added.

---

# 45. Final Coding Agent Response

After implementation, do NOT only say:

```text
Done.
```

Provide a concise engineering summary containing:

## Files changed

List meaningful created/modified files.

## Architecture change

Explain the new flow:

```text
Service
  ├── AgentCache → Redis
  └── Repository → PostgreSQL
```

## Cache behavior

Explain:

```text
hit
miss
Redis failure
TTL
```

## Context behavior

Explain how caller cancellation propagates through Redis and PostgreSQL operations.

## Tests run

List the exact commands actually executed.

Do not claim an integration test passed if it was skipped because Redis/PostgreSQL was unavailable.

Clearly distinguish:

```text
PASS
SKIPPED
NOT RUN
```

## Remaining limitations

Mention important intentionally deferred concerns such as:

```text
cache invalidation
cache stampede
singleflight
```

without implementing them.

---

# 46. Final Principle

When choosing between:

```text
a simple implementation whose behavior can be clearly explained
```

and:

```text
a more "enterprise" implementation with many abstractions
```

choose the first one.

The purpose of Day 3 is not to prove that Redis can be added to a `go.mod`.

The purpose is to understand this engineering progression:

```text
PostgreSQL handles every read
          ↓
repeated hot reads create unnecessary DB load
          ↓
introduce Redis Cache
          ↓
cache miss vs cache failure must be distinguished
          ↓
Redis itself can fail
          ↓
graceful degradation
          ↓
cached data can become stale
          ↓
TTL bounds stale state
          ↓
hot keys can later create Cache Stampede
          ↓
future milestone: singleflight / stronger cache strategy
```

Keep every change aligned with that learning goal.
