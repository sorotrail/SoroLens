.PHONY: build run test test-db lint fmt up down clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

build:
	go build -ldflags "-X github.com/sorotrail/sorolens/internal/buildinfo.Version=$(VERSION) -X github.com/sorotrail/sorolens/internal/buildinfo.Commit=$(COMMIT) -X github.com/sorotrail/sorolens/internal/buildinfo.Date=$(DATE)" -o bin/sorolens ./cmd/sorolens

run: build
	./bin/sorolens

test:
	go test ./...

# Run every test including the Postgres integration tests, against the
# docker-compose database (make up first, or point at your own Postgres).
test-db:
	TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://sorolens:sorolens@localhost:5432/sorolens?sslmode=disable} go test ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

up:
	docker compose up --build -d

down:
	docker compose down

clean:
	rm -rf bin
