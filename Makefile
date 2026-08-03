.PHONY: build test lint run migrate up down

build:
	go build ./...

test:
	go test -race -cover ./...

lint:
	golangci-lint run

run:
	go run ./cmd/

migrate:
	goose -dir migrations postgres "$(DATABASE_URL)" up

.env.docker: .env.docker.example
	cp .env.docker.example .env.docker

up: .env.docker
	docker compose up --build

down:
	docker compose down
