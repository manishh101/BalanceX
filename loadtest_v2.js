import http from 'k6/http';
import { check, sleep, group } from 'k6';

// Configuration: Testing different stages of load
export const options = {
  stages: [
    { duration: '30s', target: 20 }, // Normal traffic
    { duration: '1m', target: 50 },  // Increased pressure
    { duration: '20s', target: 100 }, // Stress test (approaching rate limit)
    { duration: '30s', target: 0 },   // Cool down
  ],
  thresholds: {
    'http_req_duration': ['p(95)<500'], // 95% of requests should be below 500ms
    'http_req_failed': ['rate<0.1'],   // Error rate should be less than 10%
  },
};

const BASE_URL = 'http://loadbalancer:8082';
const DASHBOARD_URL = 'http://loadbalancer:8081';

export default function () {
  // 1. Test Default Router (Success)
  group('Default Router', () => {
    let res = http.get(`${BASE_URL}/`);
    check(res, {
      'status is 200 or 429': (r) => [200, 429].includes(r.status),
    });
  });

  // 2. Test Payment Router (Success)
  group('Payment Router', () => {
    let endpoints = ['/api/payment', '/api/checkout'];
    let endpoint = endpoints[Math.floor(Math.random() * endpoints.length)];
    let res = http.get(`${BASE_URL}${endpoint}`);
    check(res, {
      'payment route status 200 or 429': (r) => [200, 429].includes(r.status),
    });
  });

  // 3. Test Admin Router (Auth & Headers)
  group('Admin Router', () => {
    // Success Case (with header)
    let params = {
      headers: { 'X-Admin-Key': 'secret' },
    };
    let resAuth = http.get(`${BASE_URL}/admin`, params);
    check(resAuth, {
      'admin (with header) is 200': (r) => r.status === 200,
    });

    // Failure Case (no header -> should 404/Ignore router rule)
    let resFail = http.get(`${BASE_URL}/admin`);
    check(resFail, {
      'admin (missing header) is 404': (r) => r.status === 404,
    });
  });

  // 4. Test Dashboard (Basic Auth)
  group('Dashboard Auth', () => {
    // Correct Auth
    let authHeader = 'Basic ' + Buffer.from('admin:loadbalancer').toString('base64');
    let resDash = http.get(`${DASHBOARD_URL}/`, {
      headers: { 'Authorization': authHeader },
    });
    check(resDash, {
      'dashboard (correct auth) is 200': (r) => r.status === 200,
    });

    // Wrong Auth
    let wrongAuth = 'Basic ' + Buffer.from('admin:wrong').toString('base64');
    let resDashFail = http.get(`${DASHBOARD_URL}/`, {
      headers: { 'Authorization': wrongAuth },
    });
    check(resDashFail, {
      'dashboard (wrong auth) is 401': (r) => r.status === 401,
    });
  });

  // 5. Test Edge Case (404 Not Found)
  group('404 Edge Case', () => {
    let res = http.get(`${BASE_URL}/invalid/path/${Math.random()}`);
    check(res, {
      'invalid path is 404': (r) => r.status === 404,
    });
  });

  // Simulate user thinking time
  sleep(Math.random() * 0.5 + 0.1);
}
