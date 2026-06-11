// 场景 2: 持续触发流水线校验 API (~100 RPS 目标, 5min 烟雾)
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.HELIOS_API || 'http://localhost:8080';
const TOKEN = __ENV.HELIOS_TOKEN || '';

const SPEC = `version: "1"
name: perf
stages:
  - id: s1
    steps:
      - run: echo ok
`;

export const options = {
  scenarios: {
    sustained: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '5m',
      preAllocatedVUs: 50,
      maxVUs: 200,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  const res = http.post(
    `${BASE}/api/v1/pipelines/validate`,
    JSON.stringify({ spec_raw: SPEC }),
    { headers: { Authorization: `Bearer ${TOKEN}`, 'Content-Type': 'application/json' } },
  );
  check(res, { 'validate ok': (r) => r.status === 200 });
}
