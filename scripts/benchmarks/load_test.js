import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const createUserDuration = new Trend('create_user_duration');
const createLeaderboardDuration = new Trend('create_leaderboard_duration');
const submitScoreDuration = new Trend('submit_score_duration');
const incrementScoreDuration = new Trend('increment_score_duration');
const getTopScoresDuration = new Trend('get_top_scores_duration');
const getRankDuration = new Trend('get_rank_duration');
const deleteScoreDuration = new Trend('delete_score_duration');

const totalRequests = new Counter('total_requests');
const successfulRequests = new Counter('successful_requests');

export const options = {
    // Test stages: ramp up, sustained load, ramp down
    stages: [
        { duration: '30s', target: 10 },   // Warm-up: 10 VUs
        { duration: '1m', target: 50 },    // Ramp to 50 VUs
        { duration: '2m', target: 100 },   // Sustained load: 100 VUs
        { duration: '30s', target: 200 },  // Stress test: 200 VUs (will trigger 429)
        { duration: '30s', target: 0 },    // Cool down
    ],
    thresholds: {
        // Latency thresholds (relaxed for CI, tighten for production)
        'http_req_duration': ['p(95)<200', 'p(99)<500'],
        'create_user_duration': ['p(95)<100'],
        'create_leaderboard_duration': ['p(95)<100'],
        'submit_score_duration': ['p(95)<50'],
        'increment_score_duration': ['p(95)<50'],
        'get_top_scores_duration': ['p(95)<30'],
        'get_rank_duration': ['p(95)<30'],
        'delete_score_duration': ['p(95)<50'],
        // Error rate should be low (excluding expected 429s)
        'errors': ['rate<0.05'],
    },
    // Don't fail on 429 (rate limited) - it's expected behavior
    // But fail on 5xx errors
    // http.setResponseCallback(http.expectedStatuses(200, 201, 429));
};

// Global test data
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const LEADERBOARD_ID = 'k6-load-test';

// Shared test users per VU
let testUsers = [];
let testLeaderboards = [];

// Setup: Create initial test data (run once)
export function setup() {
    console.log(`Testing against: ${BASE_URL}`);
    console.log(`Leaderboard ID: ${LEADERBOARD_ID}`);

    // Create a few leaderboards for variety
    const leaderboards = [];
    for (let i = 0; i < 5; i++) {
        const lbId = `${LEADERBOARD_ID}-${i}`;
        const res = http.post(`${BASE_URL}/api/v1/leaderboards`, JSON.stringify({
            leaderboard_id: lbId,
            name: `Load Test Leaderboard ${i}`
        }), {
            headers: { 'Content-Type': 'application/json' },
        });
        if (res.status === 201 || res.status === 409) { // 409 = already exists (idempotent)
            leaderboards.push(lbId);
        }
    }

    // Create initial users
    const users = [];
    for (let i = 0; i < 20; i++) {
        const userId = `k6-user-${i}`;
        const res = http.post(`${BASE_URL}/api/v1/users`, JSON.stringify({
            user_id: userId,
            username: `K6 User ${i}`
        }), {
            headers: { 'Content-Type': 'application/json' },
        });
        if (res.status === 201 || res.status === 409) {
            users.push(userId);
        }
    }

    console.log(`Created ${leaderboards.length} leaderboards, ${users.length} users`);
    return { leaderboards, users };
}

// Teardown: Cleanup (run once)
export function teardown(data) {
    console.log('Test completed. Cleaning up...');
    // Optionally clean up test data
    // Note: In production, you might want to keep data for analysis
}

// Generate unique IDs per VU
function getUserId(vuId, iter) {
    return `vu${vuId}-iter${iter}-${Math.random().toString(36).substr(2, 6)}`;
}

function getLeaderboardId(vuId) {
    return data.leaderboards[vuId % data.leaderboards.length];
}

// Helper to record metrics
function recordResult(res, metric, successStatuses = [200, 201]) {
    totalRequests.add(1);
    const success = successStatuses.includes(res.status);
    if (success) {
        successfulRequests.add(1);
        errorRate.add(false);
    } else if (res.status !== 429) { // Don't count rate limits as errors
        errorRate.add(true);
        console.log(`Error: ${res.status} - ${res.body}`);
    }
    if (metric && res.timings.duration) {
        metric.add(res.timings.duration);
    }
    return success;
}

export default function (data) {
    const vuId = __VU;
    const iter = __ITER;
    const userId = getUserId(vuId, iter);
    const leaderboardId = getLeaderboardId(vuId);

    // --- Test Flow ---
    // 1. Create User (idempotent)
    group('Create User', () => {
        const res = http.post(`${BASE_URL}/api/v1/users`, JSON.stringify({
            user_id: userId,
            username: `User ${userId}`
        }), {
            headers: { 'Content-Type': 'application/json' },
            tags: { endpoint: 'create_user' },
        });
        recordResult(res, createUserDuration, [201, 409]); // 409 = already exists
    });

    // 2. Create Leaderboard (idempotent) - occasionally
    if (iter % 10 === 0) {
        group('Create Leaderboard', () => {
            const lbId = `${LEADERBOARD_ID}-extra-${vuId}-${iter}`;
            const res = http.post(`${BASE_URL}/api/v1/leaderboards`, JSON.stringify({
                leaderboard_id: lbId,
                name: `Extra LB ${vuId}-${iter}`
            }), {
                headers: { 'Content-Type': 'application/json' },
                tags: { endpoint: 'create_leaderboard' },
            });
            recordResult(res, createLeaderboardDuration, [201, 409]);
        });
    }

    // 3. Submit Score (absolute)
    group('Submit Score', () => {
        const score = Math.floor(Math.random() * 10000) + 100;
        const res = http.post(`${BASE_URL}/api/v1/leaderboards/${leaderboardId}/scores`, JSON.stringify({
            user_id: userId,
            score: score
        }), {
            headers: { 'Content-Type': 'application/json' },
            tags: { endpoint: 'submit_score' },
        });
        recordResult(res, submitScoreDuration, [201, 409]);
    });

    // 4. Increment Score (delta) - multiple times
    for (let i = 0; i < 3; i++) {
        group('Increment Score', () => {
            const delta = Math.floor(Math.random() * 100) + 1;
            const res = http.post(`${BASE_URL}/api/v1/leaderboards/${leaderboardId}/scores/increment`, JSON.stringify({
                user_id: userId,
                delta: delta
            }), {
                headers: { 'Content-Type': 'application/json' },
                tags: { endpoint: 'increment_score' },
            });
            recordResult(res, incrementScoreDuration, [200, 409]);
        });
        sleep(0.01); // Small delay between increments
    }

    // 5. Get Top Scores (with pagination)
    group('Get Top Scores', () => {
        const limit = Math.floor(Math.random() * 20) + 10; // 10-30
        const offset = Math.floor(Math.random() * 5) * 10; // 0, 10, 20, 30, 40
        const res = http.get(`${BASE_URL}/api/v1/leaderboards/${leaderboardId}/scores?limit=${limit}&offset=${offset}`, {
            tags: { endpoint: 'get_top_scores' },
        });
        const success = recordResult(res, getTopScoresDuration);
        if (success) {
            check(res, {
                'response is array': (r) => Array.isArray(r.json()),
                'has correct limit': (r) => {
                    const body = r.json();
                    return body.length <= limit;
                },
            });
        }
    });

    // 6. Get User Rank
    group('Get User Rank', () => {
        const res = http.get(`${BASE_URL}/api/v1/leaderboards/${leaderboardId}/rank?user_id=${userId}`, {
            tags: { endpoint: 'get_rank' },
        });
        const success = recordResult(res, getRankDuration);
        if (success) {
            check(res, {
                'has rank field': (r) => r.json('rank') !== undefined,
                'rank is positive': (r) => r.json('rank') > 0,
            });
        }
    });

    // 7. Delete User Score (occasionally - 10% of iterations)
    if (iter % 10 === 0) {
        group('Delete User Score', () => {
            const res = http.del(`${BASE_URL}/api/v1/leaderboards/${leaderboardId}/scores/${userId}`, null, {
                tags: { endpoint: 'delete_score' },
            });
            recordResult(res, deleteScoreDuration, [200, 404]); // 404 = already deleted
        });
    }

    // 8. Test Error Cases (occasionally)
    if (iter % 20 === 0) {
        group('Error: Invalid JSON', () => {
            const res = http.post(`${BASE_URL}/api/v1/leaderboards/${leaderboardId}/scores`, 'invalid json', {
                headers: { 'Content-Type': 'application/json' },
                tags: { endpoint: 'error_invalid_json' },
            });
            check(res, { 'returns 400': (r) => r.status === 400 });
            errorRate.add(res.status !== 400);
        });

        group('Error: Missing Field', () => {
            const res = http.post(`${BASE_URL}/api/v1/leaderboards/${leaderboardId}/scores`, JSON.stringify({
                score: 100 // missing user_id
            }), {
                headers: { 'Content-Type': 'application/json' },
                tags: { endpoint: 'error_missing_field' },
            });
            check(res, { 'returns 400': (r) => r.status === 400 });
            errorRate.add(res.status !== 400);
        });

        group('Error: Negative Score', () => {
            const res = http.post(`${BASE_URL}/api/v1/leaderboards/${leaderboardId}/scores`, JSON.stringify({
                user_id: userId,
                score: -100
            }), {
                headers: { 'Content-Type': 'application/json' },
                tags: { endpoint: 'error_negative_score' },
            });
            check(res, { 'returns 400': (r) => r.status === 400 });
            errorRate.add(res.status !== 400);
        });

        group('Error: Not Found Leaderboard', () => {
            const res = http.get(`${BASE_URL}/api/v1/leaderboards/nonexistent-${vuId}/scores`, {
                tags: { endpoint: 'error_not_found' },
            });
            check(res, { 'returns 404': (r) => r.status === 404 });
            errorRate.add(res.status !== 404);
        });
    }

    // 9. Health Check (lightweight)
    if (iter % 5 === 0) {
        group('Health Check', () => {
            const res = http.get(`${BASE_URL}/health`, {
                tags: { endpoint: 'health' },
            });
            check(res, { 'health ok': (r) => r.status === 200 });
        });
    }

    // Think time - simulate realistic user behavior
    sleep(Math.random() * 0.1 + 0.02); // 20-120ms
}

// Summary report
export function handleSummary(data) {
    const summary = {
        'stdout': textSummary(data, { indent: ' ', enableColors: true }),
        'summary.json': JSON.stringify(data, null, 2),
    };

    // Also generate HTML report if needed
    return summary;
}

function textSummary(data, options) {
    const { metrics } = data;
    let output = '\n=== K6 LOAD TEST SUMMARY ===\n\n';

    // HTTP Request Duration
    if (metrics.http_req_duration) {
        const m = metrics.http_req_duration;
        output += `HTTP Request Duration:\n`;
        output += `  avg: ${m.values.avg.toFixed(2)}ms | `;
        output += `p(50): ${m.values['p(50)'].toFixed(2)}ms | `;
        output += `p(90): ${m.values['p(90)'].toFixed(2)}ms | `;
        output += `p(95): ${m.values['p(95)'].toFixed(2)}ms | `;
        output += `p(99): ${m.values['p(99)'].toFixed(2)}ms\n\n`;
    }

    // Custom metrics
    const customMetrics = [
        { key: 'create_user_duration', label: 'Create User' },
        { key: 'create_leaderboard_duration', label: 'Create Leaderboard' },
        { key: 'submit_score_duration', label: 'Submit Score' },
        { key: 'increment_score_duration', label: 'Increment Score' },
        { key: 'get_top_scores_duration', label: 'Get Top Scores' },
        { key: 'get_rank_duration', label: 'Get Rank' },
        { key: 'delete_score_duration', label: 'Delete Score' },
    ];

    output += 'Endpoint Latencies (p95):\n';
    for (const { key, label } of customMetrics) {
        if (metrics[key]) {
            output += `  ${label}: ${metrics[key].values['p(95)'].toFixed(2)}ms\n`;
        }
    }
    output += '\n';

    // Request counts
    output += `Total Requests: ${metrics.total_requests?.values.count || 0}\n`;
    output += `Successful: ${metrics.successful_requests?.values.count || 0}\n`;
    output += `Error Rate: ${(metrics.errors?.values.rate * 100).toFixed(2)}%\n\n`;

    // Thresholds
    output += 'Thresholds:\n';
    for (const [name, threshold] of Object.entries(data.thresholds || {})) {
        const passed = threshold.ok ? '✅ PASS' : '❌ FAIL';
        output += `  ${name}: ${passed}\n`;
    }

    return output;
}