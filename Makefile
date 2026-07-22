.PHONY: build test lint run migrate

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
