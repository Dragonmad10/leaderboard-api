.PHONY: build run up down logs test lint

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

lint:
	@echo "Running golangci-lint..."
	golangci-lint run