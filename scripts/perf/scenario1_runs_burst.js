// 场景 1: 500 VU 并发查询 runs 列表 (中等读压)
import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE = __ENV.HELIOS_API || 'http://localhost:8080';
const TOKEN = __ENV.HELIOS_TOKEN || '';
const ORG = __ENV.HELIOS_ORG_ID || '1';

export const options = {
  vus: 500,
  duration: '2m',
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<500'],
  },
};

export default function () {
  const res = http.get(`${BASE}/api/v1/runs?limit=20`, {
    headers: {
      Authorization: `Bearer ${TOKEN}`,
      'X-Org-ID': ORG,
    },
  });
  check(res, { 'status 200': (r) => r.status === 200 });
  sleep(0.1);
}
