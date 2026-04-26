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
| Router | [chi](https://github.com/go-chi/chi) |
| Database | PostgreSQL + [pgx](https://github.com/jackc/pgx) driver |
| Cache | Redis ([go-redis/v9](https://github.com/redis/go-redis)) |
| Broker | Kafka ([segmentio/kafka-go](https://github.com/segmentio/kafka-go)) |
| Migrations | [goose](https://github.com/pressly/goose) |
| Config | [godotenv](https://github.com/joho/godotenv) |
| Container | Docker (multi-stage build) |

## Project structure

```
user-api/
├── cmd/
│   └── main.go          # entry point, wiring
├── internal/
│   ├── handler/
│   │   ├── user.go      # HTTP handlers + UserStorage interface
│   │   └── user_test.go # unit tests with mocked storage
│   ├── db/
│   │   └── db.go        # PostgreSQL implementation
│   ├── cache/
│   │   └── redis.go     # Redis cache
│   ├── broker/
│   │   └── kafka.go     # Kafka producer
│   └── model/
│       └── user.go      # User struct
├── migrations/          # goose SQL migrations
├── Dockerfile
└── .env.example
```

## Getting started

### Local (without Docker)

**Prerequisites:** Go 1.22+, PostgreSQL, Redis, Kafka running locally.

1. Copy and fill `.env`:

```
DATABASE_URL=postgres://user:password@localhost:5432/dbname
REDIS_URL=localhost:6379
KAFKA_ADDR=localhost:9092
```

2. Run migrations:

```bash
goose -dir migrations postgres "$DATABASE_URL" up
```

3. Start the server:

```bash
go run ./cmd/
```

### Docker

```bash
docker build -t user-api .
docker run --env-file .env.docker -p 8080:8080 user-api
```

> Use `host.docker.internal` instead of `localhost` in `.env.docker` to reach services on the host machine.

## Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/users` | Get all users |
| GET | `/users/{id}` | Get user by ID (cached) |
| POST | `/users` | Create user + publish Kafka event |
| PUT | `/users/{id}` | Update user |
| DELETE | `/users/{id}` | Delete user |

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

## Kafka events

Creating a user publishes a `user-created` event to the `user-created` topic.

```json
{"id": 1, "name": "Alice", "email": "alice@example.com"}
```

Subscribe with the console consumer:
```bash
kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic user-created \
  --from-beginning
```

## Testing

Handler tests use a mock `UserStorage` interface — no real DB required:

```bash
go test ./internal/handler/...
```
