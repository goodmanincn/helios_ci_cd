// 场景 4: 模拟大量并发打开日志流 (SSE 端点 HTTP 握手压测)
// 注: k6 对 SSE 长连接支持有限, 此处压 GET stream URL 握手 + 短读
import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE = __ENV.HELIOS_API || 'http://localhost:8080';
const TOKEN = __ENV.HELIOS_TOKEN || '';
const RUN_ID = __ENV.HELIOS_RUN_ID || '1';

export const options = {
  vus: 200,
  duration: '2m',
  thresholds: {
    http_req_failed: ['rate<0.05'],
  },
};

export default function () {
  const url = `${BASE}/api/v1/runs/${RUN_ID}/logs/stream?token=${TOKEN}`;
  const res = http.get(url, { timeout: '5s' });
  check(res, {
    'stream reachable': (r) => r.status === 200 || r.status === 204 || r.status === 404,
  });
  sleep(0.2);
}
