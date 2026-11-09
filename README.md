# Leaderboard API — a backend learning project

> 📚 **This is a personal learning project, not a production service.**
> I'm building it to learn backend development with Go: how to structure an API, work with a real database, use a cache, and run everything in Docker. Expect rough edges, missing tests and decisions I'll probably change as I learn more.

The idea is simple: a REST API for game leaderboards. You create users and leaderboards, submit scores, and ask "who's on top?" or "what's my rank?". A leaderboard turned out to be a good excuse to practice a lot of backend concepts in one small project.

## What I'm learning with this project

| Topic | How it shows up here |
|-------|----------------------|
| **Go project layout** | `cmd/` + `internal/` split into domain, handler, service and repository layers |
| **REST APIs with Gin** | Routing, JSON binding, error responses, middleware |
| **PostgreSQL** | Schema design, upserts, window functions for ranking, connection pooling with `pgx` |
| **Redis** | Sorted Sets (`ZSET`) for fast rankings, used as a cache in front of Postgres |
| **Caching patterns** | Cache-aside: read from Redis, fall back to Postgres, refill the cache |
| **Rate limiting** | Sliding window algorithm with a Redis Lua script (100 req/min per IP) |
| **Docker** | Multi-stage Dockerfile and `docker-compose` with health checks |
| **Graceful shutdown & logging** | `SIGTERM` handling, structured JSON logs with `slog` |
| **Load testing** | First experiments with [k6](https://k6.io/) |

## How it works

```
                  ┌──────────────────────────────┐
  Client ───────▶ │  Gin router + middleware     │
                  │  (logger, recovery,          │──────┐
                  │   rate limiter)              │      │ rate limit counters
                  └──────────────┬───────────────┘      ▼
                                 │                 ┌─────────┐
                  ┌──────────────▼───────────────┐ │  Redis  │
                  │  Handler → LeaderboardService│─▶│ (cache) │
                  └──────────────┬───────────────┘ └─────────┘
                                 │ writes go here first
                          ┌──────▼──────┐
                          │ PostgreSQL  │  ← source of truth
                          └─────────────┘
```

| Operation | Write path | Read path |
|-----------|------------|-----------|
| Submit score | Postgres → Redis | — |
| Increment score | Postgres (atomic upsert) → Redis (best effort) | — |
| Get top scores | — | Redis → Postgres if cache misses |
| Get user rank | — | Redis (`ZREVRANK`) → Postgres (window function) |
| Delete score | Postgres → Redis | — |

**Main design decisions (and why I made them):**

1. **PostgreSQL is the source of truth.** Every write goes to Postgres first, so data survives if Redis is wiped.
2. **Redis is only a cache.** Sorted Sets make "top N" and "rank of user X" cheap, which is exactly what a leaderboard needs.
3. **The rate limiter fails open.** If Redis is down, requests are let through instead of blocking everyone.

## Running it

### With Docker (easiest)

```bash
make up                              # builds and starts API, Postgres and Redis
curl http://localhost:8080/health    # → {"status":"ok"}
make down                            # stop everything
```

`scripts/init.sql` is loaded automatically into Postgres on the first start.

### Without Docker

You need Go, PostgreSQL 16+ and Redis 7+ running locally.

```bash
psql -U postgres -d leaderboard -f scripts/init.sql

export DB_HOST=localhost DB_USER=postgres DB_PASSWORD=postgres DB_NAME=leaderboard
export REDIS_ADDR=localhost:6379

make build
./bin/api
```

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP port |
| `POSTGRES_URL` | — | Full Postgres connection string (overrides the `DB_*` vars) |
| `DB_HOST` / `DB_PORT` | `localhost` / `5432` | Postgres host and port |
| `DB_USER` / `DB_PASSWORD` | `postgres` / `postgres` | Postgres credentials |
| `DB_NAME` | `leaderboard` | Database name |
| `DB_SSLMODE` | `disable` | Postgres SSL mode |
| `REDIS_ADDR` | `localhost:6379` | Redis address (`REDIS_URL` also accepted) |

## API

| Method | Endpoint | Body | Description |
|--------|----------|------|-------------|
| `GET` | `/health` | — | Health check |
| `POST` | `/api/v1/users` | `{"user_id", "username"}` | Create a user |
| `POST` | `/api/v1/leaderboards` | `{"leaderboard_id", "name"}` | Create a leaderboard |
| `POST` | `/api/v1/leaderboards/:id/scores` | `{"user_id", "score"}` | Set a user's score |
| `POST` | `/api/v1/leaderboards/:id/scores/increment` | `{"user_id", "delta"}` | Add to a user's score |
| `GET` | `/api/v1/leaderboards/:id/scores?limit=10&offset=0` | — | Top scores (paginated) |
| `GET` | `/api/v1/leaderboards/:id/rank?user_id=...` | — | A user's rank |
| `DELETE` | `/api/v1/leaderboards/:id/scores/:user_id` | — | Remove a user's score |

Every response includes `X-RateLimit-Limit`, `X-RateLimit-Remaining` and `X-RateLimit-Reset` headers. When you go over the limit you get a `429` with `Retry-After`.

### Quick example

```bash
curl -X POST localhost:8080/api/v1/users \
  -H "Content-Type: application/json" -d '{"user_id": "player-1", "username": "Marco"}'

curl -X POST localhost:8080/api/v1/leaderboards \
  -H "Content-Type: application/json" -d '{"leaderboard_id": "game-1", "name": "High Scores"}'

curl -X POST localhost:8080/api/v1/leaderboards/game-1/scores \
  -H "Content-Type: application/json" -d '{"user_id": "player-1", "score": 1500}'

curl -X POST localhost:8080/api/v1/leaderboards/game-1/scores/increment \
  -H "Content-Type: application/json" -d '{"user_id": "player-1", "delta": 500}'

curl "localhost:8080/api/v1/leaderboards/game-1/scores?limit=10"
curl "localhost:8080/api/v1/leaderboards/game-1/rank?user_id=player-1"
```

Or run the whole flow at once with `./test_api.sh` (needs `jq`).

## Project structure

```
cmd/api/main.go            # Entry point: wires Redis, Postgres, services and router
internal/
├── domain/                # Types, interfaces and errors
├── handler/http/          # Gin handlers, routes, response helpers
├── middleware/            # Logger, panic recovery, rate limiter
├── repository/
│   ├── postgres/          # SQL queries (source of truth)
│   └── redis_repo/        # ZSET cache + Lua rate limiter
└── service/               # Business logic: coordinates Postgres and Redis
scripts/
├── init.sql               # Database schema
└── benchmarks/            # k6 load tests
build/Dockerfile           # Multi-stage build
docker-compose.yml         # API + Postgres + Redis
```

## Load testing (experimental)

I started playing with k6 to see how the API behaves under load:

```bash
k6 run scripts/benchmarks/smoke_test.js   # quick sanity check
k6 run scripts/benchmarks/load_test.js    # ramp up → sustained → stress
```

The thresholds in the scripts (e.g. p95 < 200ms) are goals I set for myself on my own machine, not guarantees. See [scripts/benchmarks/README.md](scripts/benchmarks/README.md).

## Next steps

These are the things I want to learn next with this project:

- [ ] **Write tests.** Right now there are no Go tests. I want to add unit tests for the service layer (mocking the repositories through the interfaces in `internal/domain`) and integration tests that run against real Postgres and Redis containers, e.g. with [testcontainers-go](https://golang.testcontainers.org/). Good chance to learn table-driven tests and how to test code that talks to a database.
- [ ] **Set up CI with GitHub Actions.** `.github/workflows/ci-cd.yml` exists but is empty. The goal is a pipeline that runs `golangci-lint`, `go test -race` and builds the Docker image on every push, so I learn how CI catches problems before they reach `main`.
- [ ] **Deploy it somewhere real.** Put the API online on a platform like Fly.io, Render or Railway with a managed Postgres and Redis. That means learning about secrets and environment config, database migrations outside of `init.sql`, and checking logs of a running service, which is something you never see when everything runs on localhost.

---

Made by Marco while learning Go and backend development. Feedback and tips are very welcome!
