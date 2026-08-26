# AGENTS.md

## Project: AgentHub — Day 2

AgentHub is a production-oriented AI backend project written in Go. Its long-term purpose is to demonstrate explainable, production-grade backend engineering for internship interviews and a resume.

Day 1 is the working baseline:

- standard-library `net/http` and modern `http.ServeMux` routes;
- Handler → Service → Repository separation;
- concurrency-safe `MemoryRepository`;
- request-context propagation;
- JSON responses and domain-to-HTTP error mapping;
- graceful shutdown and tests.

Day 2 changes one infrastructure boundary:

> Replace runtime in-memory persistence with PostgreSQL while preserving the HTTP contract and domain boundaries.

Optimize for understanding the complete database request path, not feature count.

## Day 2 outcome

By the end of Day 2, AgentHub must:

- keep the existing endpoints and behaviors working;
- persist Agents in PostgreSQL across process restarts;
- use `github.com/jackc/pgx/v5` and `pgxpool`;
- use explicit, parameterized SQL rather than an ORM;
- manage schema with versioned up/down SQL migrations;
- use UUID IDs that are safe across restarts and multiple instances;
- propagate request contexts into database operations;
- translate pgx errors into domain errors at the Repository boundary;
- create one application-level connection pool and close it at shutdown;
- include focused PostgreSQL integration tests;
- document setup, migrations, running, testing, and persistence verification.

Target path:

```text
Client → net/http → Handler → Service → Repository interface
       → PostgresRepository → pgxpool.Pool → PostgreSQL
```

Handler and Service must not import or expose pgx types.

## Preserve Day 1 behavior

Do not rewrite working Day 1 code without a concrete reason.

Keep:

```text
GET  /health
GET  /api/v1/agents
POST /api/v1/agents
GET  /api/v1/agents/{id}
```

Preserve:

- JSON requests and responses;
- malformed or trailing JSON returns `400 Bad Request`;
- an empty or whitespace-only name returns `400 Bad Request`;
- a missing Agent returns a JSON `404 Not Found`;
- unexpected failures return a generic JSON `500` without internal details;
- an empty list is `[]`, not `null`;
- graceful shutdown;
- Handler → Service → Repository dependency direction;
- propagation of `r.Context()` through the entire request path.

Adding `created_at` to Agent responses is an intentional Day 2 contract change. Update affected tests and README examples coherently.

## Scope restrictions

Approved new dependencies are limited to the smallest practical set for:

- PostgreSQL access with `github.com/jackc/pgx/v5`;
- UUID generation with `github.com/google/uuid` or an equally small, justified package.

Do not add unless explicitly requested:

- GORM, Ent, Bun, SQLBoiler, sqlc, or another ORM/code generator;
- a migration framework when plain SQL files are sufficient;
- Gin, Echo, Fiber, or another web framework;
- Docker or Docker Compose;
- Redis, RabbitMQ, Kafka, gRPC, auth, JWT, or microservices;
- OpenTelemetry, Prometheus, Grafana, an LLM SDK, or a vector database;
- dependency-injection/configuration frameworks;
- a generic database layer for multiple database engines.

Do not create or alter tables from application startup code. Schema deployment and application startup are separate responsibilities.

## Inspect before editing

Before changes:

1. Read this file completely.
2. Inspect the repository tree and `git status`.
3. Read the Agent model, Repository, MemoryRepository, Service, Handler, tests, `main.go`, `go.mod`, and README.
4. Run `go test ./...` once for a baseline.
5. Identify the smallest coherent PostgreSQL change set.

Adapt to the working code instead of assuming an earlier plan is exact. Preserve unrelated user changes.

## Target structure

Prefer the current domain-oriented layout:

```text
agenthub/
├── cmd/server/main.go
├── internal/
│   ├── agent/
│   │   ├── model.go
│   │   ├── repository.go
│   │   ├── memory_repository.go
│   │   ├── postgres_repository.go
│   │   ├── postgres_repository_test.go
│   │   ├── service.go
│   │   ├── service_test.go
│   │   ├── handler.go
│   │   └── handler_test.go
│   └── httpx/response.go
├── migrations/
│   ├── 000001_create_agents.up.sql
│   └── 000001_create_agents.down.sql
├── go.mod
├── go.sum
├── README.md
└── AGENTS.md
```

Minor deviations are acceptable when they fit the current code. Keep persistence close to the `agent` domain. Do not create generic `database`, `db`, `common`, `base`, or `utils` packages for one small helper.

## Domain model and IDs

Extend the model:

```go
type Agent struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    Description string    `json:"description"`
    CreatedAt   time.Time `json:"created_at"`
}
```

Keep `CreateAgentInput` separate. Rules:

- generate a UUID in Service before calling Repository;
- keep ID as a string in the domain and JSON API;
- remove the process-local atomic counter;
- trim and validate name in Service;
- allow an empty description;
- do not expose pgx types in domain structs.

The Day 1 counter is unsafe after persistence: after restart it starts at zero while earlier IDs remain in PostgreSQL. It is also unsafe across multiple instances. Do not use `MAX(id)`, row counts, or a custom distributed ID generator.

## Schema and migrations

Create `migrations/000001_create_agents.up.sql` equivalent to:

```sql
CREATE TABLE agents (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT agents_name_not_blank CHECK (char_length(btrim(name)) > 0)
);
```

Create `migrations/000001_create_agents.down.sql` equivalent to:

```sql
DROP TABLE agents;
```

Requirements:

- use PostgreSQL `UUID` and `TIMESTAMPTZ`;
- enforce a non-blank name in both Service and database;
- rely on the primary-key index for ID lookups;
- do not add speculative indexes;
- keep migrations as versioned plain SQL and document `psql` usage;
- mark the down migration as destructive;
- do not use `CREATE TABLE IF NOT EXISTS` in application code;
- never rewrite an already-applied migration for a later schema change; add the next version.

Service validation gives friendly errors. The database constraint is the last data-integrity defense. Both are intentional.

## Repository contract

Keep the interface independent of PostgreSQL. Change create coherently to return the persisted row:

```go
type Repository interface {
    Create(ctx context.Context, agent Agent) (Agent, error)
    GetByID(ctx context.Context, id string) (Agent, error)
    List(ctx context.Context) ([]Agent, error)
}
```

Returning the row lets PostgreSQL remain the source of the `created_at` default. Update MemoryRepository, Service, and tests consistently. MemoryRepository may set `CreatedAt` when it receives a zero value so fast tests remain meaningful.

The interface must not mention `pgxpool.Pool`, `pgx.Row`, SQL strings, or PostgreSQL error codes.

## PostgresRepository and SQL

Add:

```go
type PostgresRepository struct {
    pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository
```

Requirements:

- inject the pool; never create one per request or method;
- pass the received context to every pgx call;
- use `$1`, `$2`, etc. for user-controlled values;
- never concatenate user input into SQL;
- explicitly list columns and scan into the domain model;
- wrap unexpected errors with operation context and `%w`;
- translate known infrastructure errors at this boundary;
- close query rows and check `rows.Err()`;
- do not use global database variables.

Required SQL should be equivalent to:

```sql
INSERT INTO agents (id, name, description)
VALUES ($1, $2, $3)
RETURNING id, name, description, created_at;
```

```sql
SELECT id, name, description, created_at
FROM agents
WHERE id = $1;
```

```sql
SELECT id, name, description, created_at
FROM agents
ORDER BY created_at DESC, id DESC;
```

The ID tie-breaker makes ordering deterministic when timestamps match. Do not use `SELECT *`.

## Error translation

The rest of the application must not depend on pgx errors.

```go
if errors.Is(err, pgx.ErrNoRows) {
    return Agent{}, ErrAgentNotFound
}
```

Preserve this boundary:

```text
pgx error → PostgresRepository translation → domain error
          → Service → Handler → safe HTTP response
```

- map `pgx.ErrNoRows` to existing `ErrAgentNotFound`;
- preserve `errors.Is` when wrapping;
- never compare error strings;
- never expose SQL, connection strings, PostgreSQL messages, or driver details;
- unexpected failures become the existing generic `500` response;
- do not build elaborate conflict handling for extremely unlikely UUID collisions.

## Pool lifecycle and wiring

`cmd/server/main.go` remains the composition root.

Startup:

1. read `DATABASE_URL`;
2. fail clearly if missing;
3. parse the pgxpool configuration;
4. create one application-level pool;
5. ping PostgreSQL using a bounded startup context;
6. construct `PostgresRepository`;
7. inject Repository → Service → Handler;
8. start the existing HTTP server.

Shutdown:

1. stop accepting requests;
2. drain in-flight requests within the existing timeout;
3. close the pool;
4. exit cleanly.

Do not silently fall back to MemoryRepository when configuration or PostgreSQL is unavailable. Keep MemoryRepository for fast tests, but runtime must use PostgresRepository. Do not log `DATABASE_URL`.

Use pgxpool defaults unless a small setting has a clear reason. If configuring `MaxConns`, document that total possible connections are approximately `service instances × MaxConns`. A pool provides reuse and bounds database concurrency; larger is not automatically better.

## Context rules

Preserve:

```text
r.Context() → Handler → Service → PostgresRepository
            → pool.QueryRow / pool.Query
```

- do not replace request context with `context.Background()`;
- do not create unrelated contexts inside Repository methods;
- return cancellation/deadline errors promptly;
- use a separate bounded context only for startup operations.

## Health endpoint

Keep `GET /health` as simple process liveness:

```json
{"status":"ok"}
```

Check database availability during startup. Do not add a full liveness/readiness subsystem in Day 2.

## Fast tests

Keep unit and HTTP tests independent of PostgreSQL. Use MemoryRepository or a small domain fake to test:

- blank-name validation;
- generated IDs are parseable UUIDs;
- create/list/get behavior;
- Handler status codes and JSON;
- missing Agent maps to `404`;
- unexpected Repository errors map to `500` without leaks;
- `created_at` is meaningful without exact-time assertions.

Do not mock pgx internals for Service or Handler tests.

## PostgreSQL integration tests

Test PostgresRepository against a real, dedicated test database selected only by:

```text
TEST_DATABASE_URL
```

Rules:

- when absent, skip only integration tests with a clear message;
- ordinary tests must still run;
- never use `DATABASE_URL` for destructive test cleanup;
- require/apply the Day 2 schema before tests;
- clean only data in the dedicated test database;
- keep isolation explicit and avoid timing-based sleeps;
- report whether these tests ran or were skipped.

At minimum, test:

- create returns the row with non-zero `created_at`;
- get returns the correct Agent;
- list has deterministic newest-first order;
- missing UUID becomes `ErrAgentNotFound` through `errors.Is`;
- the database rejects a blank name when Service is bypassed;
- cancellation reaches a database call where reliably testable.

## README requirements

Document:

- the PostgreSQL milestone and prerequisites: Go, PostgreSQL, `psql`;
- creating/selecting a local database;
- placeholder `DATABASE_URL` configuration;
- applying the up migration and destructive down migration;
- running the server and ordinary tests;
- integration tests with `TEST_DATABASE_URL`;
- why runtime uses PostgresRepository while fast tests use MemoryRepository;
- manual persistence proof: create an Agent, restart the process against the same database, then fetch it.

Local-only example:

```bash
export DATABASE_URL='postgres://agenthub:agenthub@localhost:5432/agenthub?sslmode=disable'
```

Never commit credentials or secret `.env` files. Do not use `sslmode=disable` outside clearly local examples.

## Implementation order

1. Inspect code and establish a test baseline.
2. Add `CreatedAt` and update the Repository create contract.
3. Replace the atomic counter with UUIDs.
4. Update MemoryRepository and fast tests.
5. Add up/down migrations.
6. Add PostgresRepository and explicit SQL.
7. Add error translation.
8. Add integration tests.
9. Wire `DATABASE_URL`, startup ping, pool, and repository in `main.go`.
10. Preserve graceful shutdown and close the pool.
11. Update HTTP tests and README.
12. Run quality gates and review the diff.

Do not begin by rewriting handlers or adding unrelated infrastructure.

## Quality gates

Before declaring completion, run:

```bash
go mod tidy
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
```

Also run integration tests with `TEST_DATABASE_URL` when available. Report exact commands and outcomes; never claim skipped tests passed.

## Coding and safety rules

Prefer idiomatic Go, small functions, explicit constructors, direct SQL near Repository methods, early returns, `%w`, `errors.Is`, table-driven tests where useful, `t.Cleanup`, deterministic data, and comments that explain why.

Avoid global pools, generic base repositories, a query builder for three statements, interfaces wrapping every pgx type, `SELECT *`, SQL concatenation, swallowed `rows.Err()`, duplicate logging at every layer, panics for expected startup errors, sleeps in tests, and transaction abstractions for single-statement operations.

Never expose/log credentials or database internals. Never run a down migration or destructive cleanup against an unidentified database. Preserve unrelated user changes.

## Definition of done

- [ ] Existing endpoints and error contracts still work.
- [ ] Runtime persistence uses PostgreSQL; fast tests may retain MemoryRepository.
- [ ] IDs are UUIDs and JSON contains `created_at`.
- [ ] Versioned up/down migrations exist.
- [ ] Schema uses UUID primary key, `TIMESTAMPTZ`, and constraints.
- [ ] PostgresRepository implements the domain interface.
- [ ] SQL is explicit, parameterized, and lists columns.
- [ ] List ordering is deterministic.
- [ ] `pgx.ErrNoRows` becomes `ErrAgentNotFound`.
- [ ] Unexpected database errors never leak to clients.
- [ ] Request context reaches every database operation.
- [ ] One pool is created, pinged with a timeout, and closed at shutdown.
- [ ] Missing/invalid database configuration fails clearly with no memory fallback.
- [ ] Fast tests remain database-independent.
- [ ] PostgreSQL integration tests exist and skips are reported honestly.
- [ ] README covers setup, migrations, tests, and restart persistence.
- [ ] tidy, format, vet, tests, and race tests pass.
- [ ] No ORM, Docker, cache, queue, auth, observability stack, or unrelated framework was added.

## Learning checkpoint

Keep the result simple enough to explain:

- why PostgreSQL data survives process restart;
- PostgreSQL Server vs TCP connection vs `pgx.Conn` vs `pgxpool.Pool`;
- pool reuse and concurrency limits;
- why the Day 1 atomic ID fails after persistence and why UUID fixes it;
- table, row, column, primary key, constraint, index, and migration;
- why migrations version schema changes;
- why Handler and Service do not import pgx;
- why `pgx.ErrNoRows` becomes a domain error;
- why request context reaches pgx;
- why list SQL needs `ORDER BY`;
- why MemoryRepository remains useful in tests;
- when a multi-statement workflow requires a transaction;
- what GORM automates and why Day 2 intentionally uses direct SQL.

## Required final report

The coding agent must summarize:

- files changed and behavior implemented;
- Repository, schema, SQL, and error-mapping decisions;
- exact validation commands and results;
- whether integration tests actually ran or were skipped;
- remaining setup or blockers;
- any deliberate deviation and its reason.

If PostgreSQL or `TEST_DATABASE_URL` is unavailable, complete database-independent work, leave integration tests runnable, and report the limitation precisely. Never fabricate results.

Day 2 milestone:

> A clean PostgreSQL persistence layer that proves the Repository boundary, survives process restarts, propagates cancellation, preserves the HTTP contract, and remains small enough to explain line by line.
