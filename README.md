# User API

REST API for user management built with Go. Demonstrates a typical microservice architecture: PostgreSQL for persistence, Redis for caching, Kafka for event publishing.

## Architecture

```
HTTP Request
     │
     ▼
  Handler
  ├── Redis Cache (GET by ID — cache-aside, TTL 5 min)
  ├── PostgreSQL  (source of truth — all CRUD)
  └── Kafka       (publish user-created events → downstream consumers)
```

## Stack

| Layer | Technology |
|---|---|
| Language | Go |
| Router | [chi v5](https://github.com/go-chi/chi) |
| Database | PostgreSQL + [pgx](https://github.com/jackc/pgx) driver |
| Cache | Redis ([go-redis/v9](https://github.com/redis/go-redis)) |
| Broker | Kafka ([segmentio/kafka-go](https://github.com/segmentio/kafka-go)) |
| Migrations | [goose](https://github.com/pressly/goose) |
| Config | env vars + [godotenv](https://github.com/joho/godotenv) |
| Logging | log/slog (JSON) |
| Container | Docker (multi-stage build, non-root) |
| CI | GitHub Actions (build, vet, gofmt, test -race) |

## Project structure

```
user-api/
├── cmd/
│   ├── main.go          # entry point, wiring, graceful shutdown
│   └── loadtest/        # standalone HTTP load generator
├── internal/
│   ├── handler/         # HTTP handlers, UserStorage interface, health checks
│   ├── db/              # PostgreSQL implementation, connection pool
│   ├── cache/           # Redis cache (cache-aside, miss vs error)
│   ├── broker/          # Kafka producer
│   ├── config/          # typed config, fail-fast env validation
│   └── model/           # User struct
├── migrations/          # goose SQL migrations
├── .github/workflows/   # CI pipeline
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── .env.example
```

## Configuration

All configuration is read from environment variables (or `.env`) and validated at startup — the server fails fast if a required variable is missing. See `.env.example`.

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | yes | — | PostgreSQL connection string |
| `REDIS_URL` | yes | — | Redis address (`host:port`) |
| `KAFKA_ADDR` | yes | — | Kafka broker address (`host:port`) |
| `KAFKA_TOPIC` | no | `user-created` | Topic for user-created events |
| `HTTP_ADDR` | no | `:8080` | HTTP listen address |
| `PPROF_ENABLED` | no | `false` | Enable pprof debug server |
| `PPROF_ADDR` | no | `localhost:6060` | pprof listen address (localhost only) |

## Getting started

### Local (without Docker)

**Prerequisites:** Go 1.26+, PostgreSQL, Redis, Kafka running locally.

1. Copy `.env.example` to `.env` and fill in the values.

2. Run migrations:

```bash
make migrate
# or: goose -dir migrations postgres "$DATABASE_URL" up
```

3. Start the server:

```bash
make run
```

### Docker Compose

Brings up the app together with PostgreSQL, Redis and Kafka:

```bash
docker compose up --build
```

Postgres credentials are parametrized via `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` (with dev defaults). Kafka is reachable as `kafka:9092` from inside the compose network and as `localhost:29092` from the host.

## Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/users?limit=&offset=` | List users (paginated) |
| GET | `/users/{id}` | Get user by ID (cached) |
| POST | `/users` | Create user + publish Kafka event |
| PUT | `/users/{id}` | Update user |
| DELETE | `/users/{id}` | Delete user |
| GET | `/healthz` | Liveness probe |
| GET | `/readyz` | Readiness probe (pings PostgreSQL and Redis) |

### Pagination

`GET /users` accepts `limit` (default 50, max 100) and `offset` (default 0):

```bash
curl "http://localhost:8080/users?limit=10&offset=20"
```

### Status codes

- `400` — invalid ID, body, pagination params, or validation failure (name/email required, valid email format, name up to 100 characters)
- `404` — user not found (also for update/delete of a missing ID)
- `409` — email already taken (unique constraint)
- `413` — request body larger than 1 MiB

## Example requests

**Create user**
```bash
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice", "email": "alice@example.com"}'
```

**Get user by ID** (first call hits DB and caches; second call served from Redis)
```bash
curl http://localhost:8080/users/1
curl http://localhost:8080/users/1  # served from Redis cache
```

**Update user**
```bash
curl -X PUT http://localhost:8080/users/1 \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice Smith", "email": "alice@example.com"}'
```

**Delete user**
```bash
curl -X DELETE http://localhost:8080/users/1
```

## Caching strategy

`GET /users/{id}` uses **cache-aside**:
1. Check Redis → cache hit: return immediately, skip DB.
2. Cache miss → query PostgreSQL → store result in Redis with 5-minute TTL → return.

A Redis failure is logged and treated as a miss (the request still succeeds from the DB); a corrupted cache entry is logged and refreshed from the DB. On `PUT`/`DELETE` the cached `user:{id}` key is invalidated, so subsequent reads never serve stale data.

## Kafka events

Creating a user publishes a `user-created` event to the `user-created` topic.

```json
{"id": 1, "name": "Alice", "email": "alice@example.com"}
```

Subscribe with the console consumer (from the host):
```bash
kafka-console-consumer.sh \
  --bootstrap-server localhost:29092 \
  --topic user-created \
  --from-beginning
```

## Operations

- **Graceful shutdown**: SIGINT/SIGTERM drains in-flight requests (10s timeout) and closes DB/Kafka connections.
- **Timeouts**: the HTTP server sets read/write/idle timeouts; request bodies are capped at 1 MiB.
- **pprof**: set `PPROF_ENABLED=true` to expose the profiler on `localhost:6060` (never exposed by default).

## Testing & CI

Handler tests use mocked storage/cache — no real DB required:

```bash
make test        # go test -race -cover ./...
make lint        # golangci-lint run
```

CI (GitHub Actions) runs `go build`, `go vet`, a gofmt check and `go test -race -cover` on every push and pull request.

## Benchmarks & load test

`bench_test.go` (`go test -bench .`) and an in-process load test (`go test -run TestLoad`)
exercise the HTTP layer end-to-end through the chi router with an in-memory storage/cache mock.
They measure handler + JSON (de)serialization + cache-aside throughput in isolation — **not**
real PostgreSQL/Redis latency, so treat them as relative figures, not production numbers.

```bash
go test ./internal/handler/ -run TestLoad -v   # in-process load test
go test ./internal/handler/ -bench . -benchmem  # micro-benchmarks
```
