import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
    stages: [
        { duration: '10s', target: 50 },  // Rampa a 50 VUs
        { duration: '20s', target: 200 }, // Pico de 200 VUs para forzar HTTP 429
        { duration: '10s', target: 0 },   // Bajada
    ],
    thresholds: {
        http_req_duration: ['p(99)<10'], // 99% de las peticiones deben responder en < 10ms
    },
};

http.setResponseCallback(http.expectedStatuses(200, 429));

export default function () {
    const BASE_URL = 'http://localhost:8080/api/v1/leaderboards/global/scores?limit=10&offset=0';
    const res = http.get(BASE_URL);

    check(res, {
        'status es 200 o 429': (r) => r.status === 200 || r.status === 429,
        'cabecera rate limit presente': (r) => r.headers['X-Ratelimit-Limit'] !== undefined,
    });

    sleep(0.05); // 20 RPS por usuario virtual
}