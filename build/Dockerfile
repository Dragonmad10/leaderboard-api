# Build stage
FROM golang:alpine AS builder

WORKDIR /app

# Install git and ca-certificates (needed for dependencies and HTTPS requests)
RUN apk add --no-cache git ca-certificates

# Copy dependency files first to leverage Docker cache
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Build a statically linked binary
# - CGO_ENABLED=0: Disables CGO for a fully static binary
# - ldflags "-w -s": Omits the DWARF symbol table and debug info to reduce binary size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o leaderboard-api ./cmd/api/main.go

# Final minimal stage
FROM scratch

# Import root certificates from the builder stage
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the static binary
COPY --from=builder /app/leaderboard-api /leaderboard-api

# Expose API port
EXPOSE 8080

# Run the binary
ENTRYPOINT ["/leaderboard-api"]