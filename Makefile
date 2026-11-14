.PHONY: build run up down logs test test-integration lint

# Docker commands
up:
	@echo "Starting Docker containers..."
	docker-compose up -d --build

down:
	@echo "Stopping Docker containers..."
	docker-compose down

logs:
	docker-compose logs -f

# Not check if this command follows the standard
clean_volumes:
	@echo "Cleaning Docker volumes..."
	docker compose down -v

# Go commands
build:
	@echo "Building Go binary..."
	go build -o bin/api cmd/api/main.go

test:
	@echo "Running tests..."
	go test -v -race ./...

# Needs real databases: docker compose up -d postgres redis
test-integration:
	@echo "Running integration tests..."
	go test -v -race -count=1 -tags integration ./test/integration/...

lint:
	@echo "Running golangci-lint..."
	golangci-lint run