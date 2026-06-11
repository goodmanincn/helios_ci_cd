// 场景 3: 100 VU 并发拉取 run 历史日志
import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE = __ENV.HELIOS_API || 'http://localhost:8080';
const TOKEN = __ENV.HELIOS_TOKEN || '';
const ORG = __ENV.HELIOS_ORG_ID || '1';
const RUN_ID = __ENV.HELIOS_RUN_ID || '1';

export const options = {
  vus: 100,
  duration: '3m',
  thresholds: {
    http_req_failed: ['rate<0.05'],
    http_req_duration: ['p(95)<3000'],
  },
};

export default function () {
  const res = http.get(
    `${BASE}/api/v1/runs/${RUN_ID}/logs?source=auto&count=5000`,
    {
      headers: {
        Authorization: `Bearer ${TOKEN}`,
        'X-Org-ID': ORG,
      },
      timeout: '60s',
    },
  );
  check(res, { 'logs 200': (r) => r.status === 200 });
  sleep(0.5);
}
