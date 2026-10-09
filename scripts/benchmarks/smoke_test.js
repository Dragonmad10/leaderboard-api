import http from 'k6/http';
import { check, sleep } from 'k6';

// Smoke test - quick validation that API works
export const options = {
    vus: 1,
    duration: '30s',
    thresholds: {
        http_req_duration: ['p(95)<200'],
        http_req_failed: ['rate<0.01'],
    },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const LEADERBOARD_ID = 'smoke-test';

export function setup() {
    // Create test leaderboard
    http.post(`${BASE_URL}/api/v1/leaderboards`, JSON.stringify({
        leaderboard_id: LEADERBOARD_ID,
        name: 'Smoke Test Leaderboard'
    }), {
        headers: { 'Content-Type': 'application/json' },
    });
}

export default function () {
    const userId = `smoke-user-${__VU}-${__ITER}`;

    // 1. Health check
    let res = http.get(`${BASE_URL}/health`);
    check(res, { 'health: 200': (r) => r.status === 200 });

    // 2. Create user
    res = http.post(`${BASE_URL}/api/v1/users`, JSON.stringify({
        user_id: userId,
        username: `Smoke User ${__ITER}`
    }), {
        headers: { 'Content-Type': 'application/json' },
    });
    check(res, { 'create user: 201 or 409': (r) => r.status === 201 || r.status === 409 });

    // 3. Submit score
    res = http.post(`${BASE_URL}/api/v1/leaderboards/${LEADERBOARD_ID}/scores`, JSON.stringify({
        user_id: userId,
        score: 1000
    }), {
        headers: { 'Content-Type': 'application/json' },
    });
    check(res, { 'submit score: 201': (r) => r.status === 201 });

    // 4. Increment score
    res = http.post(`${BASE_URL}/api/v1/leaderboards/${LEADERBOARD_ID}/scores/increment`, JSON.stringify({
        user_id: userId,
        delta: 100
    }), {
        headers: { 'Content-Type': 'application/json' },
    });
    check(res, { 'increment score: 200': (r) => r.status === 200 });
    if (res.status === 200) {
        check(res, { 'returns new_score': (r) => r.json('new_score') !== undefined });
    }

    // 5. Get top scores
    res = http.get(`${BASE_URL}/api/v1/leaderboards/${LEADERBOARD_ID}/scores?limit=10`);
    check(res, { 'get top: 200': (r) => r.status === 200 });
    if (res.status === 200) {
        check(res, { 'returns array': (r) => Array.isArray(r.json()) });
    }

    // 6. Get rank
    res = http.get(`${BASE_URL}/api/v1/leaderboards/${LEADERBOARD_ID}/rank?user_id=${userId}`);
    check(res, { 'get rank: 200': (r) => r.status === 200 });
    if (res.status === 200) {
        check(res, { 'returns rank': (r) => r.json('rank') !== undefined && r.json('rank') > 0 });
    }

    // 7. Delete score
    res = http.del(`${BASE_URL}/api/v1/leaderboards/${LEADERBOARD_ID}/scores/${userId}`);
    check(res, { 'delete score: 200': (r) => r.status === 200 });

    // 8. Error cases
    res = http.post(`${BASE_URL}/api/v1/leaderboards/${LEADERBOARD_ID}/scores`, 'invalid', {
        headers: { 'Content-Type': 'application/json' },
    });
    check(res, { 'invalid json: 400': (r) => r.status === 400 });

    res = http.post(`${BASE_URL}/api/v1/leaderboards/${LEADERBOARD_ID}/scores`, JSON.stringify({
        score: 100
    }), {
        headers: { 'Content-Type': 'application/json' },
    });
    check(res, { 'missing user_id: 400': (r) => r.status === 400 });

    res = http.get(`${BASE_URL}/api/v1/leaderboards/nonexistent/scores`);
    check(res, { 'not found: 404': (r) => r.status === 404 });

    sleep(0.1);
}