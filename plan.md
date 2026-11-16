# Plan: finish leaderboard-api

Work top to bottom. Each checkbox is **one commit** (Conventional Commits, see CLAUDE.md).
After every step: `make test` must show no *new* failures.

**How to handle a bug:** first write a test that fails because of the bug, then fix the
code until the test passes. That's test-driven development (TDD): the test proves the bug
was real and proves the fix works.

---

## Phase 0: Cleanup (≈2 days)

Small, low-risk changes before any new feature. Goal: less code to read and reason
about before the hard part (Phase 1b).

- [x] **0.1 Remove dead code.** `handler/http/response.go` (helpers nobody calls),
  `redis_repo/client.go` (file that only holds a comment), the unused sentinel errors
  `ErrNotFound`/`ErrInvalidInput`/`ErrConflict`/`ErrInternal` in `domain/errors.go`.
  **Commit:** `refactor: remove unused code`
- [x] **0.2 Use `GetScore` and `CountPlayers` instead of deleting them.** They existed in
  the repository but nothing called them. Decision: expose them as endpoints.
  - `GET /api/v1/leaderboards/:id/scores/:user_id` → `{"user_id","leaderboard_id","score"}`
  - `GET /api/v1/leaderboards/:id/stats` → `{"leaderboard_id","player_count"}` (an object,
    so more stats can be added later without breaking clients)
  - Both read from Postgres only, not Redis: Redis can miss players or be stale until
    Phase 1b. Revisit after Phase 1b: once the cache is guaranteed complete, `ZSCORE` and
    `ZCARD` (O(1)) could answer these.
  - Unit tests (service + handler) and integration test steps added. The Postgres
    `CountPlayers` now wraps its error like the other methods.
  **Commit:** `feat(handler): add user score and leaderboard stats endpoints`
  - Open question for Phase 1e (auto-create decision): `stats` of a leaderboard that
    doesn't exist returns `player_count: 0`, not 404.
- [x] **0.3 One log format.** Replace `gin.Logger()` (plain text) with our own
  `middleware.Logger()` (slog JSON) in `SetupRouter`. Keep the order: Logger first
  (outermost), then Recovery, then the rate limiter. While there, fix the logger itself:
  - take the logger as a parameter (`Logger(log *slog.Logger)`) instead of the global
    `slog.Default()`, so tests can capture and check the output;
  - log latency as a number (`latency_ms`), not a string;
  - level from the status: 5xx → ERROR, 4xx → WARN, the rest → INFO;
  - tests that check the logged fields, not only the response.
  - Done: `SetupRouter(handler, log, rateLimiter)` now takes the logger; `main.go` passes
    its JSON logger. Errors attached with `c.Error(err)` already go into the request's line
    (ready for 0.13).
  **Commit:** `refactor(middleware): log requests as json with an injected logger`
- [ ] **0.4 Gin release mode.** Read `APP_ENV` in `main.go`; call
  `gin.SetMode(gin.ReleaseMode)` when it is `production`.
  **Commit:** `fix: run gin in release mode in production`
- [ ] **0.5 Remove the duplicate route** `GET /api/v1/leaderboards/` (same job as `/health`).
  **Commit:** `refactor(handler): remove duplicate status route`
- [ ] **0.6 Check `rows.Err()`** in Postgres `GetTopScores`. Without it, a connection
  dropped halfway returns a partial list as if it were complete.
  **Commit:** `fix(postgres): check row iteration errors`
- [ ] **0.7 Add `ReadHeaderTimeout`** to `http.Server` (protects against Slowloris:
  clients that send headers very slowly to hold connections open).
  **Commit:** `fix: set a read header timeout on the http server`
- [ ] **0.8 Decide on `test_api.sh`.** It repeats the integration tests and step 12
  expects a 404 the API never returns. Delete it (and its README line) or fix it.
  **Commit:** `chore: remove test_api.sh`
- [ ] **0.9 Makefile:** use `docker compose` (v2) everywhere, add `clean_volumes` to `.PHONY`.
  **Commit:** `chore: use docker compose v2 in the makefile`
- [ ] **0.10 Redis client setup in its own function** (what the deleted `redis_repo/client.go`
  was meant for). Add `redis_repo.NewClient(...)`, mirroring `postgres.NewPostgresPool`:
  read the config, set timeouts, ping. Fixes a real bug: `REDIS_URL=redis://localhost:6379`
  is documented as accepted, but `main.go` passes it as a plain address (`host:port`), so it
  can't connect. Use `redis.ParseURL` for URLs.
  **Commit:** `fix(redis): connect with a redis:// url`
- [ ] **0.11 Clean shutdown.** The Redis client is never closed (`rdb.Close()`). And
  `os.Exit` inside the server goroutine skips every `defer` (deferred cleanup), so the
  Postgres pool isn't closed either. Let `main` own the exit: send the listen error over a
  channel instead.
  **Commit:** `fix: close redis and postgres on shutdown`
- [ ] **0.12 Use `errors.Is(err, redis.Nil)`** instead of `err == redis.Nil` in
  `redis_repo/leaderboard.go`. `==` stops matching as soon as the error is wrapped
  (`fmt.Errorf("...: %w", err)`); `errors.Is` looks inside the wrapping. The linter
  (`errorlint`) will flag this in Phase 1c anyway.
  **Commit:** `refactor(redis): compare errors with errors.Is`
- [ ] **0.13 Log the cause of every 500.** `handleError` sends "Failed to ..." to the client
  but never logs the real error (`connection refused`, the SQL error...). When something
  breaks in production, the cause is lost. Do it after 0.3: `handleError` calls
  `c.Error(err)` for 5xx, and the Logger adds the cause to the request's log line, so one
  request = one log line. Only for 5xx: 4xx errors are the client's fault and would flood
  the logs. Also inject the logger into `Recovery` (it still uses the global
  `slog.Default()`), the same way as `Logger` in 0.3.
  **Commit:** `fix(handler): log the cause of internal errors`

---

## Phase 1a: Handler bugs (≈2 days)

The 4 failing tests in `internal/handler/http`, plus bugs no test covers yet.

- [ ] `TestParseIntQuery`: huge numbers overflow and wrap to negative. Use `strconv.Atoi`
  (it returns an error on overflow) instead of the hand-written loop.
- [ ] `TestSubmitScore_ZeroIsAValidScore`: `binding:"required"` treats `0` as "missing"
  because 0 is Go's *zero value* (the default for an unset int). Fix: use a pointer
  (`*float64`, where nil means missing) or drop `required` and keep `gte=0`.
- [ ] `TestGetTopScores_EmptyLeaderboardReturnsEmptyList`: a nil slice becomes `null`
  in JSON, an empty slice becomes `[]`.
- [ ] `TestRateLimit_CannotBeBypassedWithForwardedForHeader`: by default `ClientIP()`
  trusts the `X-Forwarded-For` header, so anyone can fake a new IP on every request.
  Read about `router.SetTrustedProxies`.
- [ ] **New:** user/leaderboard IDs longer than 64 chars give a 500 (Postgres rejects the
  `VARCHAR(64)`). Write a test, then validate in the handler (`binding:"max=64"`) → 400.
- [ ] **New:** `{"delta": 0}` on `/increment` returns 400, the same zero-value trap as
  the score. First decide: is adding 0 a valid (useless) request or an error? Then test it.
- [ ] **New:** the score rules disagree. `/scores` rejects a negative score (`gte=0`), but
  `/increment` with a negative delta can push a score below 0. A huge delta can also overflow
  `BIGINT` in Postgres, which comes back as a 500. Decide the rule (can scores be negative?),
  write tests, and enforce it in one place (the service, not two handlers).

---

## Phase 1b: Cache and ranking design (≈1 week, the important part)

### Step 1: write `docs/cache-design.md` before coding
Half a page to one page. It's interview material. It must answer:

0. **Scope: what lives in the cache?** The full leaderboard, only the top N, or the full
   leaderboard for active leaderboards only (idle ones expire)? Decide from what the API
   must answer: exact rank for every player needs every player in Redis. Include a memory
   estimate (measure one key with `MEMORY USAGE`).

1. **Completeness:** how does the service know the Redis sorted set holds *every*
   player, not just some? Today "key exists" is treated as "cache is complete".
   Compare at least:
   - On a miss, load the whole leaderboard from Postgres (cost for 1M players? batching?
     do reads wait for it?).
   - A separate "complete" marker key, set only after a full load.
   - Write-through on every write + a rebuild when Redis restarts empty.
2. **The refill race:** the background goroutine in `GetTopScores` can overwrite a newer
   score with an older one. How does your choice avoid it? Also: that goroutine uses
   `context.Background()` with no timeout, and every cache miss starts a new one, so under
   load (a cold cache with 1,000 requests a second) you get a pile of goroutines all doing the
   same refill. Look up *cache stampede* and `golang.org/x/sync/singleflight`.
3. **Redis restarts:** `--save 60 1` in docker-compose reloads a snapshot that may be
   60s old, so the cache looks complete but is stale. For a pure cache, turn persistence
   off so "restarted" always means "empty".
4. **Redis write failure:** error or warning? (Postgres is the source of truth.)
5. **What does "rank" mean?** Redis `ZREVRANK` gives positions (a tie gives 1 and 2);
   Postgres `RANK()` gives shared ranks (a tie gives 1 and 1). Pick one; both sides must agree.

### Step 2: make the tests pass
- [ ] `TestSubmitScore_RedisDown_StillSucceeds`: log a warning, don't fail.
- [ ] `TestIncrementScore_EmptyCache_CacheGetsTheRealTotal`: in Redis, `ZADD` the
  `newScore` that Postgres returned instead of `ZINCRBY delta`. Setting an absolute value
  is *idempotent* (doing it twice gives the same result), so the cache can't drift.
- [ ] `TestGetTopScores_CacheHasOnlySomePlayers_StillReturnsEveryone`
- [ ] `TestGetTopScores_SmallPageCachedFirst_BiggerPageStillComplete`
- [ ] `TestGetUserRank_CacheHasOnlySomePlayers_RankIsStillCorrect`
- [ ] `TestGetTopScores_CacheMiss_ResultsIncludeRank`: not a cache problem. The Postgres
  query never fills in `Rank`. Fix the SQL **and** the fake (CLAUDE.md: keep fakes in sync).
  Do it together with question 5 above.
- [ ] **New:** stable pagination with ties: `ORDER BY score DESC, user_id` (test first).
- [ ] **New:** `DELETE` of a score that doesn't exist should return 404 (test first).
- [ ] **New:** turn off Redis persistence in `docker-compose.yml` (from question 3).

---

## Phase 1c: Tooling and CI (≈3 days)

Prepare the tools first, or the pipeline fails for the wrong reasons.

- [ ] Install golangci-lint locally; fill `.golangci.yml` (v2 needs `version: "2"`);
  make `make lint` pass.
- [ ] Dockerfile: pin the Go image to the version in `go.mod`; remove the hard-coded
  `GOARCH=amd64` so it builds on ARM too; drop `git` if it isn't needed.
- [ ] `.github/workflows/ci-cd.yml`:
  - Job 1: lint + `go test -race ./...`
  - Job 2: integration tests with Postgres and Redis as GitHub Actions `services:`
  - Job 3: `docker build`
- [ ] CI badge in the README.

---

## Phase 1d: Migrations (≈1 day)

- [ ] Replace `scripts/init.sql` with `golang-migrate` migrations. Why: `init.sql` only
  runs on an empty database; a real database with data needs small, ordered, versioned
  changes.
- [ ] Remove the `init.sql` mount from docker-compose so only one thing creates the schema.
- [ ] Update the README and the CI integration job.

---

## Phase 1e: Smaller decisions (when the rest is done)

Each one: think, decide, write the reason in the README or `docs/`, then code.

- [ ] Score type: the API takes `float64` but stores `int64` (`1e300` becomes garbage).
  Switch to an integer? Note: `TestSubmitScore_TruncatesDecimals` asserts the current
  behaviour, so changing it is a deliberate API change.
- [ ] Auto-create on write: every score write runs an extra transaction, and a typo in a
  leaderboard ID quietly creates a new leaderboard. Auto-created users also get their ID as
  username, and auto-created leaderboards their ID as name. Should an unknown leaderboard be
  a 404?
- [ ] Health checks: exempt `/health` from the rate limiter; add a readiness check that
  pings Postgres and Redis (look up *liveness vs readiness*).
- [ ] Rate limiter details:
  - `X-RateLimit-Reset`: report when the oldest request leaves the window, not always 60s.
  - The Lua script uses the API server's clock (`time.Now()` in Go). With two API
    instances whose clocks differ by a second, the window is wrong. Use Redis's own clock
    (`redis.call('TIME')`) inside the script so there's one clock.
  - `results[0].(int64)` in `ratelimit.go` is an unchecked type assertion (Go converting an
    `interface{}` to a concrete type). If the script ever returns something else, it panics.
    Use the `v, ok := x.(int64)` form and return an error.
- [ ] Load test (`scripts/benchmarks/load_test.js`):
  - All k6 users share one IP, so almost everything is a 429 within a second. Make the limit
    configurable (env var) and raise it for benchmarks.
  - It expects a 404 for an unknown leaderboard, but the API returns 200. Every such
    request counts as an error. Fix it after the auto-create decision above.
  - The k6 README lists "Non-existent leaderboard → 404" as tested. Same fix.
- [ ] docker-compose publishes Postgres (5432) and Redis (6379) on your machine's network.
  Handy for local dev and integration tests, but anyone on your network can reach them with
  the default passwords. Bind them to `127.0.0.1:5432:5432` instead.
- [ ] README: say clearly that there's no authentication.
- [ ] **Error design: who knows about HTTP?** (what the deleted sentinel errors were for).
  Today `domain.AppError` stores the HTTP status, so the domain layer knows about HTTP.
  Alternative: the domain defines meanings (`ErrNotFound`, `ErrInvalidInput`), repositories
  and services return them (wrapped with `fmt.Errorf("...: %w", err)` for context), and only
  the handler maps them to statuses. Write both options down, pick one, and say why. Useful
  if a second transport (gRPC, a CLI, a worker) is ever added.
- [ ] **One error response format** (what the deleted `response.go` was for). Today there
  are three shapes: handlers send `{"error"}`, recovery sends `{"error","message"}`, the rate
  limiter sends `{"error","message","retry_in"}`. Bind errors also say "Invalid JSON format"
  even when the JSON is fine and validation failed, and they leak Go internals
  (`Key: 'req.Score' Error:Field validation...`). Pick one shape (look at RFC 9457
  "Problem Details"), use it everywhere through one helper, and write a test per shape.

---

**Done when:** `make test` and `make lint` are green, CI is green on `main`,
`docs/cache-design.md` exists, and the README "Next steps" boxes are ticked.

Deploying (Fly.io/Render) is optional, after Phase 2.
