# User API

REST API for user management built with Go, chi and PostgreSQL.

## Stack

- Go
- [chi](https://github.com/go-chi/chi) — router
- PostgreSQL — database
- [pgx](https://github.com/jackc/pgx) — PostgreSQL driver

## Getting started

1. Create a PostgreSQL database
2. Create the users table:

```sql
CREATE TABLE users (
    id    SERIAL PRIMARY KEY,
    name  TEXT NOT NULL,
    email TEXT NOT NULL
);
```

3. Create `.env` file in the project root:

```
DATABASE_URL=postgres://user:password@localhost:5432/dbname
```

4. Run the server:

```bash
go run ./cmd/
```

Server starts on `http://localhost:8080`.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /users | Get all users |
| GET | /users/{id} | Get user by ID |
| POST | /users | Create user |
| PUT | /users/{id} | Update user |
| DELETE | /users/{id} | Delete user |

## Example requests

**Create user**
```bash
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice", "email": "alice@example.com"}'
```

**Get all users**
```bash
curl http://localhost:8080/users
```

**Delete user**
```bash
curl -X DELETE http://localhost:8080/users/1
```
