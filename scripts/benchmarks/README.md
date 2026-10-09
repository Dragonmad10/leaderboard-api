# k6 Load Tests

## Prerequisites

```bash
# Install k6
# macOS
brew install k6

# Linux
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update && sudo apt-get install k6
```

## Quick Start

```bash
# 1. Start the API (with docker-compose)
docker-compose up -d

# 2. Run quick smoke test (1 VU, 10 seconds)
k6 run --vus 1 --duration 10s scripts/benchmarks/load_test.js

# 3. Run full load test (uses stages defined in script)
k6 run scripts/benchmarks/load_test.js

# 4. Run against custom URL
k6 run -e BASE_URL=http://staging-api:8080 scripts/benchmarks/load_test.js
```

## Test Scenarios

The load test (`load_test.js`) covers:

| Phase | Duration | VUs | Purpose |
|-------|----------|-----|---------|
| Warm-up | 30s | 10 | Cache warming, JIT compilation |
| Ramp | 1m | 50 | Gradual load increase |
| Sustained | 2m | 100 | Normal production load |
| Stress | 30s | 200 | Rate limiting (429) testing |
| Cool-down | 30s | 0 | Graceful shutdown |

## Endpoints Tested

| Endpoint | Method | Tested | Notes |
|----------|--------|--------|-------|
| `/api/v1/users` | POST | ✅ | Idempotent create |
| `/api/v1/leaderboards` | POST | ✅ | Idempotent create |
| `/api/v1/leaderboards/:id/scores` | POST | ✅ | Submit absolute score |
| `/api/v1/leaderboards/:id/scores/increment` | POST | ✅ | Increment score (delta) |
| `/api/v1/leaderboards/:id/scores` | GET | ✅ | Paginated top scores |
| `/api/v1/leaderboards/:id/rank` | GET | ✅ | User rank lookup |
| `/api/v1/leaderboards/:id/scores/:user_id` | DELETE | ✅ | Remove user score |
| `/health` | GET | ✅ | Health check |

## Error Cases Tested

- Invalid JSON → 400
- Missing required fields → 400
- Negative scores → 400
- Non-existent leaderboard → 404

## Metrics Collected

### Built-in
- `http_req_duration` - Overall request latency
- `http_req_failed` - Request failure rate
- `vus` - Virtual users over time

### Custom (per-endpoint)
- `create_user_duration`
- `create_leaderboard_duration`
- `submit_score_duration`
- `increment_score_duration`
- `get_top_scores_duration`
- `get_rank_duration`
- `delete_score_duration`
- `total_requests` / `successful_requests`
- `errors` - Error rate (excluding 429)

## Thresholds (Pass/Fail Criteria)

| Metric | Threshold |
|--------|-----------|
| p(95) overall latency | < 200ms |
| p(99) overall latency | < 500ms |
| p(95) submit score | < 50ms |
| p(95) get top scores | < 30ms |
| p(95) get rank | < 30ms |
| Error rate (excl. 429) | < 5% |

## Output Formats

```bash
# JSON summary (for CI/CD)
k6 run --out json=results.json scripts/benchmarks/load_test.js

# InfluxDB + Grafana (real-time dashboard)
k6 run --out influxdb=http://localhost:8086/k6 scripts/benchmarks/load_test.js

# Cloud (k6 Cloud)
k6 cloud scripts/benchmarks/load_test.js
```

## CI/CD Integration

### GitHub Actions Example

```yaml
- name: Run k6 Load Test
  run: |
    docker-compose up -d
    sleep 10  # wait for services
    k6 run scripts/benchmarks/load_test.js
  env:
    BASE_URL: http://localhost:8080
```

### GitLab CI Example

```yaml
load_test:
  stage: test
  script:
    - docker-compose up -d
    - sleep 10
    - k6 run scripts/benchmarks/load_test.js
  artifacts:
    reports:
      performance: summary.json
```

## Tuning for Your Environment

Edit `load_test.js` options:

```javascript
export const options = {
    stages: [
        // Adjust targets based on your infrastructure
        { duration: '1m', target: 50 },   // Normal load
        { duration: '3m', target: 100 },  // Peak load
        { duration: '1m', target: 200 },  // Stress test
    ],
    thresholds: {
        // Tighten for production
        'http_req_duration': ['p(95)<100', 'p(99)<200'],
    },
};
```

## Common Issues

| Issue | Solution |
|-------|----------|
| Connection refused | Check API is running: `curl localhost:8080/health` |
| Too many 429s | Increase rate limit in `main.go` or reduce VUs |
| High latency | Check Redis/PostgreSQL resources, indexes |
| Out of memory | Reduce VUs or increase k6 memory: `K6_MAX_MEM=4096` |

## Debugging

```bash
# Verbose output
k6 run --verbose scripts/benchmarks/load_test.js

# Dry run (validate script)
k6 run --dry-run scripts/benchmarks/load_test.js

# Only run setup/teardown
k6 run --no-iterations scripts/benchmarks/load_test.js
```