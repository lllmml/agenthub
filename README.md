# AgentHub

AgentHub is a production-oriented AI backend project written in Go.

## Current Milestone

Initial HTTP API with in-memory storage, built on the Go standard library
(`net/http`, `http.ServeMux`, `log/slog`):

- `GET /health`
- `GET /api/v1/agents`
- `POST /api/v1/agents`
- `GET /api/v1/agents/{id}`

The code follows a Handler -> Service -> Repository separation, uses a
concurrency-safe in-memory store, propagates request contexts, and shuts
down gracefully on `SIGINT`/`SIGTERM`.

## How to Run

```bash
go run ./cmd/server
```

The server listens on `:8080`. If that port is unavailable, override it
with the `PORT` environment variable:

```bash
PORT=8083 go run ./cmd/server
```

## How to Test

```bash
go test ./...
```

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
curl http://localhost:8080/api/v1/agents/agent-1
```
