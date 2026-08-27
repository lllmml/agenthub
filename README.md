# AgentHub

AgentHub is a production-oriented AI backend project written in Go. Its
long-term purpose is to demonstrate explainable, production-grade backend
engineering.

## Current Milestone

Day 3: a Redis cache in front of the Day 2 PostgreSQL persistence
layer, using the Cache-Aside pattern. The HTTP contract is unchanged;
PostgreSQL remains the source of truth and Redis only speeds up
`GET /api/v1/agents/{id}`.

Request path for a single agent read:

```text
Client -> Handler -> Service
                      |
                      +-- Redis cache -------- hit -> return
                      |        |
                      |      miss/error
                      |        v
                      +--> Repository -> PostgreSQL -> cache fill -> return
```

Endpoints (HTTP contract unchanged from Day 1/2):

- `GET /health`
- `GET /api/v1/agents`
- `POST /api/v1/agents`
- `GET /api/v1/agents/{id}`

Only `GET /api/v1/agents/{id}` is cached. The list endpoint and
creation still go straight to PostgreSQL.

### Redis role

Redis is a cache, **not** the source of truth. It holds JSON copies of
agents under namespaced keys (`agenthub:agent:v1:{uuid}`) with a
5-minute TTL.

### PostgreSQL role

PostgreSQL remains authoritative persistent storage. Every cache miss
loads from it, and every successful database read refills the cache
best-effort.

### Graceful degradation

Redis is optional. If `REDIS_URL` is absent, invalid, or unreachable,
the server logs a warning and continues with caching disabled
(PostgreSQL serves every read):

```text
Redis unavailable
    |
PostgreSQL fallback
```

A cache read or write failure never turns a successful database read
into a failed request. PostgreSQL startup failure, in contrast, still
fails the application — PostgreSQL holds authoritative data, Redis does
not.

## Prerequisites

- Go 1.26+
- PostgreSQL (server) and `psql` (client)
- Docker (optional, for the local Redis instance)

## Setup

Create the local `agenthub` role and set its password (you type it
interactively — this is what makes password auth in the connection
strings below work):

```bash
createuser --createdb --pwprompt agenthub
```

Create the development database and a dedicated integration test
database, both owned by that role:

```bash
createdb --owner=agenthub agenthub        # local development data
createdb --owner=agenthub agenthub_test   # dedicated integration tests
```

- `agenthub` is the local development database used by the server.
- `agenthub_test` is the dedicated integration test database.
  Integration tests delete rows from / rebuild the `agents` table
  inside it.
- `TEST_DATABASE_URL` must never point at the development or a
  production database: the test code refuses to run destructive
  operations against any database whose name does not end with
  `_test`.

Configure the connection strings with the password you just set —
replace `<your-password>` below:

```bash
export DATABASE_URL='postgres://agenthub:<your-password>@localhost:5432/agenthub?sslmode=disable'
export TEST_DATABASE_URL='postgres://agenthub:<your-password>@localhost:5432/agenthub_test?sslmode=disable'
```

The examples use `sslmode=disable`, which is only appropriate for local
development. Never commit real credentials.

### Start Redis

Redis is optional and used only as a cache:

```bash
docker compose up -d redis
```

Stop it with:

```bash
docker compose down
```

### Environment variables

| Variable          | Required | Purpose                                   |
| ----------------- | -------- | ----------------------------------------- |
| `DATABASE_URL`    | yes      | PostgreSQL connection string              |
| `REDIS_URL`       | no       | Redis connection string; absent disables caching |
| `TEST_DATABASE_URL` | tests  | dedicated `_test` PostgreSQL database     |
| `TEST_REDIS_URL`  | tests    | Redis instance for integration tests      |

Safe local examples:

```bash
export DATABASE_URL='postgres://agenthub:<your-password>@localhost:5432/agenthub?sslmode=disable'
export REDIS_URL='redis://localhost:6379/0'
export TEST_DATABASE_URL='postgres://agenthub:<your-password>@localhost:5432/agenthub_test?sslmode=disable'
export TEST_REDIS_URL='redis://localhost:6379/0'
```

### Migrations

Schema is managed as versioned plain SQL in `migrations/`. Apply the up
migration with `psql` (schema deployment is a separate step from
application startup; the server never creates tables):

```bash
psql "$DATABASE_URL" -f migrations/000001_create_agents.up.sql
```

The down migration is destructive (drops the table and all its data):

```bash
psql "$DATABASE_URL" -f migrations/000001_create_agents.down.sql
```

Never edit an applied migration; add the next versioned file instead.

## How to Run

```bash
go run ./cmd/server
```

The server fails fast with a clear error if `DATABASE_URL` is missing or
PostgreSQL is unreachable — it never falls back to in-memory storage.
With Redis configured (and reachable) the cache is enabled:

```bash
export REDIS_URL='redis://localhost:6379/0'
go run ./cmd/server
```

The server listens on `:8080`; override with `PORT` if needed:

```bash
PORT=8083 go run ./cmd/server
```

On `SIGINT`/`SIGTERM` the server stops accepting requests, drains
in-flight requests, then closes the Redis client and the connection
pool.

## How to Test

Ordinary tests are database- and Redis-independent (they use
MemoryRepository and fake caches):

```bash
go test ./...
```

Integration tests exercise PostgresRepository against a real database
selected only by `TEST_DATABASE_URL`; they skip with a clear message
when it is unset. Before touching anything, the tests verify the
connected database name ends with `_test` and refuse to run against
any other database:

```bash
export TEST_DATABASE_URL='postgres://agenthub:<your-password>@localhost:5432/agenthub_test?sslmode=disable'
go test -v ./internal/agent/ -run TestPostgresRepository
```

Redis integration tests exercise `RedisAgentCache` against a real
Redis selected only by `TEST_REDIS_URL`; they skip when it is unset:

```bash
docker compose up -d redis
export TEST_REDIS_URL='redis://localhost:6379/0'
go test -v ./internal/agent -run Redis
```

Why two repositories? Runtime uses PostgresRepository because agents
must survive restarts and be shared across instances. Fast tests use
MemoryRepository so unit/HTTP tests run instantly with no database and
no pgx mocking. The Repository interface keeps both interchangeable.
The same applies to caching: runtime uses `RedisAgentCache` (or a
no-op when Redis is unavailable); tests use small fakes.

## Example Requests

```bash
curl http://localhost:8080/health
```

```bash
curl -X POST http://localhost:8080/api/v1/agents \
  -H "Content-Type: application/json" \
  -d '{
    "name": "paper-assistant",
    "description": "Help users read papers"
  }'
```

```bash
curl http://localhost:8080/api/v1/agents
```

```bash
curl http://localhost:8080/api/v1/agents/<id-from-create-response>
```

## Persistence Proof

Create an agent, then restart the process against the same database and
fetch it again:

```bash
# terminal 1
go run ./cmd/server
curl -X POST http://localhost:8080/api/v1/agents \
  -H "Content-Type: application/json" -d '{"name":"durable"}'
# note the returned id

# Ctrl+C, then start the server again (same DATABASE_URL)
go run ./cmd/server
curl http://localhost:8080/api/v1/agents/<that-id>
# the agent is still there: data survived the process restart
```
