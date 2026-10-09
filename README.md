# Leaderboard API

A high-performance, scalable leaderboard service built with Go, PostgreSQL, and Redis. Designed for real-time gaming leaderboards with sub-millisecond read latency and strong consistency guarantees.

## Overview

This API provides a production-ready leaderboard system with the following characteristics:

- **High Performance**: Redis Sorted Sets (ZSET) for O(log N) reads/writes
- **Data Integrity**: PostgreSQL as the Source of Truth with ACID transactions
- **Cache-Aside Pattern**: Automatic cache population on reads, synchronous updates on writes
- **Rate Limiting**: Sliding window algorithm via Redis Lua scripts (fail-open)
- **Observability**: Structured JSON logging, health endpoints, rate limit headers
- **Container Ready**: Multi-stage Docker build, docker-compose for local dev

## Architecture

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Client    │────▶│   API Gateway    │────▶│  Rate Limiter   │
└─────────────┘     │   (Gin Router)   │     │  (Sliding Win)  │
                    └────────┬─────────┘     └────────┬────────┘
                             │                       │
                    ┌────────▼────────┐     ┌────────▼────────┐
                    │  Leaderboard    │     │  RateLimiter    │
                    │    Handler      │     │    Service      │
                    └────────┬────────┘     └────────┬────────┘
                             │                       │
              ┌──────────────┼──────────────┐        │
              ▼              ▼              ▼        ▼
        ┌───────────┐ ┌───────────┐ ┌───────────┐ ┌──────────┐
        │PostgreSQL │ │   Redis   │ │PostgreSQL │ │  Redis   │
        │(Source of │ │  (Cache)  │ │(Rate Limit│ │(Rate Lmt)│
        │  Truth)   │ │           │ │  Store)   │ │          │
        └───────────┘ └───────────┘ └───────────┘ └──────────┘
```

### Data Flow

| Operation | Write Path | Read Path |
|-----------|------------|-----------|
| **Submit Score** | PostgreSQL → Redis | — |
| **Increment Score** | PostgreSQL (atomic) → Redis (best-effort) | — |
| **Get Top Scores** | — | Redis (Cache) → PostgreSQL (fallback) |
| **Get User Rank** | — | Redis (ZREVRANK+ZSCORE) → PostgreSQL (Window Fn) |
| **Delete Score** | PostgreSQL → Redis | — |

### Key Design Decisions

1. **PostgreSQL = Source of Truth**: All mutations hit PostgreSQL first for durability and consistency
2. **Redis = Real-time Cache**: Sorted Sets provide O(log N) rank/score operations
3. **Cache-Aside on Reads**: Try Redis first, fallback to PostgreSQL, async repopulate cache (first page only)
4. **Atomic Increments**: PostgreSQL `UPSERT ... RETURNING` + Redis `ZINCRBY`
5. **Fail-Open Rate Limiting**: If Redis is down, requests are allowed (never block legitimate traffic)

## API Endpoints

### Health & Info
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/` | Welcome message |
| `GET` | `/health` | Health check (returns `{"status": "ok"}`) |
| `GET` | `/api/v1/leaderboards/` | API info |

### User Management
| Method | Endpoint | Request Body | Response |
|--------|----------|--------------|----------|
| `POST` | `/api/v1/users` | `{"user_id": "string", "username": "string"}` | `201 Created` |

### Leaderboard Management
| Method | Endpoint | Request Body | Response |
|--------|----------|--------------|----------|
| `POST` | `/api/v1/leaderboards` | `{"leaderboard_id": "string", "name": "string"}` | `201 Created` |

### Score Operations
| Method | Endpoint | Request Body | Response |
|--------|----------|--------------|----------|
| `POST` | `/api/v1/leaderboards/:id/scores` | `{"user_id": "string", "score": float64}` | `201 Created` |
| `POST` | `/api/v1/leaderboards/:id/scores/increment` | `{"user_id": "string", "delta": int64}` | `200 OK` + `{"user_id": "...", "new_score": N}` |
| `GET` | `/api/v1/leaderboards/:id/scores?limit=10&offset=0` | — | `200 OK` + `[{"user_id", "leaderboard_id", "score", "rank"}, ...]` |
| `GET` | `/api/v1/leaderboards/:id/rank?user_id=string` | — | `200 OK` + `{"user_id": "...", "rank": N}` |
| `DELETE` | `/api/v1/leaderboards/:id/scores/:user_id` | — | `200 OK` |

### Rate Limit Headers
All responses include standard rate limit headers (RFC 6585):
- `X-RateLimit-Limit`: Maximum requests in window
- `X-RateLimit-Remaining`: Requests remaining
- `X-RateLimit-Reset`: Seconds until window resets
- `Retry-After` (on 429): Seconds to wait before retry

## Quick Start

### Using Docker Compose (Recommended)

```bash
# Start all services (API, Redis, PostgreSQL)
make up

# Or manually
docker-compose up -d

# Verify API is running
curl http://localhost:8080/health
```

### Local Development (Without Docker)

**Prerequisites:**
- Go 1.21+
- PostgreSQL 16+
- Redis 7+

```bash
# 1. Start PostgreSQL and Redis locally
# 2. Run database migrations
psql -U postgres -d leaderboard -f scripts/init.sql

# 3. Set environment variables
export DB_HOST=localhost
export DB_PORT=5432
export DB_USER=postgres
export DB_PASSWORD=postgres
export DB_NAME=leaderboard
export REDIS_ADDR=localhost:6379
export PORT=8080

# 4. Build and run
make build
./bin/api
```

### Run Tests
```bash
# Unit tests with race detection
make test

# Load test with k6 (requires running API)
k6 run scripts/benchmarks/load_test.js
```

## Configuration

All configuration via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP server port |
| `POSTGRES_URL` | — | Full PostgreSQL connection string (takes precedence) |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | PostgreSQL user |
| `DB_PASSWORD` | `postgres` | PostgreSQL password |
| `DB_NAME` | `leaderboard` | PostgreSQL database name |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode |
| `REDIS_ADDR` | `localhost:6379` | Redis address (host:port) |
| `REDIS_URL` | — | Full Redis URL (alternative to REDIS_ADDR) |
| `APP_ENV` | `development` | Environment (development/production) |

## Database Schema

```sql
-- Users
CREATE TABLE users (
    id VARCHAR(64) PRIMARY KEY,
    username VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Leaderboards
CREATE TABLE leaderboards (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Scores (composite PK: leaderboard_id + user_id)
CREATE TABLE scores (
    leaderboard_id VARCHAR(64) REFERENCES leaderboards(id) ON DELETE CASCADE,
    user_id VARCHAR(64) REFERENCES users(id) ON DELETE CASCADE,
    score BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (leaderboard_id, user_id)
);

-- Index for efficient top-score queries
CREATE INDEX idx_scores_leaderboard_score ON scores(leaderboard_id, score DESC);
```

## Project Structure

```
leaderboard-api/
├── bin/                    # Compiled binary
├── build/
│   └── Dockerfile          # Multi-stage Docker build
├── cmd/
│   └── api/
│       └── main.go         # Application entry point
├── config/
│   └── config.go           # Configuration loading
├── docs/
│   └── architecture.png    # Architecture diagram
├── internal/
│   ├── domain/             # Domain models & interfaces
│   │   ├── errors.go       # Error types & helpers
│   │   ├── leaderboard.go  # Leaderboard domain types & interfaces
│   │   └── ratelimit.go    # Rate limit domain types & interfaces
│   ├── handler/
│   │   └── http/           # HTTP handlers (Gin)
│   │       ├── leaderboard.go  # Leaderboard endpoint handlers
│   │       ├── response.go     # Response helpers
│   │       └── router.go       # Route registration & middleware
│   ├── middleware/
│   │   ├── logger.go       # Structured request logging
│   │   ├── rate_limiter.go # Rate limiting middleware
│   │   └── recovery.go     # Panic recovery
│   ├── repository/
│   │   ├── postgres/       # PostgreSQL implementations
│   │   │   ├── client.go   # Connection pool setup
│   │   │   └── leaderboard.go # Score/User/Leaderboard CRUD
│   │   └── redis_repo/     # Redis implementations
│   │       ├── client.go   # Redis client setup
│   │       ├── leaderboard.go # ZSET operations
│   │       └── ratelimit.go   # Sliding window (Lua)
│   └── service/
│       ├── leaderboard.go  # Business logic orchestration
│       └── ratelimit.go    # Rate limit service
├── scripts/
│   ├── init.sql            # Database initialization
│   └── benchmarks/         # k6 load tests
│       ├── load_test.js
│       ├── smoke_test.js
│       └── README.md
├── test_api.sh             # Manual API test script
├── docker-compose.yml      # Local development stack
├── go.mod / go.sum         # Go dependencies
├── Makefile                # Build/Run/Test commands
└── README.md               # This file
```

## Load Testing

The project includes comprehensive k6 load tests:

```bash
# Start services
docker-compose up -d

# Quick smoke test (1 VU, 10s)
k6 run --vus 1 --duration 10s scripts/benchmarks/smoke_test.js

# Full load test (stages: warmup → ramp → sustained → stress → cooldown)
k6 run scripts/benchmarks/load_test.js

# Custom target URL
k6 run -e BASE_URL=http://staging:8080 scripts/benchmarks/load_test.js
```

### Performance Targets
| Metric | Threshold |
|--------|-----------|
| p(95) overall latency | < 200ms |
| p(99) overall latency | < 500ms |
| p(95) submit score | < 50ms |
| p(95) get top scores | < 30ms |
| p(95) get rank | < 30ms |
| Error rate (excl. 429) | < 5% |

## Example Usage

### Create a user and leaderboard
```bash
curl -X POST http://localhost:8080/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{"user_id": "player-1", "username": "Marco"}'

curl -X POST http://localhost:8080/api/v1/leaderboards \
  -H "Content-Type: application/json" \
  -d '{"leaderboard_id": "game-1", "name": "High Scores"}'
```

### Submit and increment scores
```bash
# Submit absolute score
curl -X POST http://localhost:8080/api/v1/leaderboards/game-1/scores \
  -H "Content-Type: application/json" \
  -d '{"user_id": "player-1", "score": 1500}'

# Increment by delta (e.g., +500 for a win)
curl -X POST http://localhost:8080/api/v1/leaderboards/game-1/scores/increment \
  -H "Content-Type: application/json" \
  -d '{"user_id": "player-1", "delta": 500}'
```

### Query leaderboard
```bash
# Top 10 scores
curl "http://localhost:8080/api/v1/leaderboards/game-1/scores?limit=10&offset=0"

# User rank
curl "http://localhost:8080/api/v1/leaderboards/game-1/rank?user_id=player-1"

# Paginated (page 2)
curl "http://localhost:8080/api/v1/leaderboards/game-1/scores?limit=5&offset=5"
```

## In Progress / Next Steps

The following features and improvements are planned for future iterations:

### 🔧 Core Features
- [ ] **WebSocket Support** — Real-time leaderboard updates via Server-Sent Events (SSE) or WebSockets
- [ ] **Leaderboard Metadata** — Add `GetLeaderboardInfo` endpoint (name, player count, created_at)
- [ ] **Score History** — Track score changes over time (audit log / replay)
- [ ] **Team/Guild Leaderboards** — Aggregate scores by team/group
- [ ] **Seasonal Leaderboards** — Time-bounded leaderboards with automatic rotation/archival

### 🏗️ Architecture & Scalability
- [ ] **Redis Cluster Support** — Hash-tag based sharding for multi-node Redis
- [ ] **Read Replicas** — Route read queries to PostgreSQL replicas
- [ ] **Cache Invalidation Strategy** — TTL-based expiry + event-driven invalidation (Pub/Sub)
- [ ] **Distributed Tracing** — OpenTelemetry integration (Jaeger/Zipkin)
- [ ] **Metrics Export** — Prometheus metrics endpoint (`/metrics`)

### 🔐 Security & Auth
- [ ] **Authentication** — JWT/OIDC token validation middleware
- [ ] **Authorization** — Role-based access (admin vs player endpoints)
- [ ] **API Keys** — Alternative to IP-based rate limiting
- [ ] **Input Sanitization** — Additional validation (XSS, injection prevention)

### 🧪 Testing & Quality
- [ ] **Unit Tests** — Increase coverage for service/repository layers (target >80%)
- [ ] **Integration Tests** — Testcontainers-based Postgres/Redis integration tests
- [ ] **Contract Tests** — API schema validation (OpenAPI/Swagger)
- [ ] **Chaos Testing** — Failure injection (Redis/Postgres downtime, network partitions)
- [ ] **Benchmark CI** — Automated k6 runs in CI/CD with regression detection

### 🚀 DevOps & Operations
- [ ] **Helm Chart** — Kubernetes deployment with HPA, PodDisruptionBudgets
- [ ] **Database Migrations** — Versioned migration tool (golang-migrate/Atlas)
- [ ] **Structured Logging** — Correlation IDs, request tracing, log levels
- [ ] **Alerting Rules** — Prometheus alerts (latency, error rate, saturation)
- [ ] **Runbook Documentation** — Incident response procedures

### 📚 Documentation
- [ ] **OpenAPI/Swagger Spec** — Auto-generated from code annotations
- [ ] **API Reference** — Interactive docs (Swagger UI / Redoc)
- [ ] **Architecture Decision Records (ADRs)** — Document key design choices
- [ ] **Client SDKs** — Generated Go/TypeScript/Python clients

---

## License

MIT License — see [LICENSE](LICENSE) for details.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit changes (`git commit -m 'Add amazing feature'`)
4. Push to branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

Please ensure `make test` and `make lint` pass before submitting.