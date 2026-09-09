.PHONY: build run test test-db lint fmt up down clean

build:
	go build -o bin/sorolens ./cmd/sorolens

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
