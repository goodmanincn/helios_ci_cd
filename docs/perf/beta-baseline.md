# Helios v1.0 Beta 压测基线报告

> 状态：**脚本就绪，待目标环境实测填数** (M8 T8.4.2)
> 日期：2026-06-11

## 环境

| 项 | 值 |
|----|-----|
| 部署方式 | （填：docker-compose / helm） |
| API 副本 | |
| Worker 副本 | |
| Postgres | |
| Redis | |

## 结果摘要

| 场景 | VU/速率 | 时长 | P50 | P95 | P99 | 错误率 | 通过 |
|------|---------|------|-----|-----|-----|--------|------|
| 1 runs burst | 500 VU | 2m | — | — | — | — | ☐ |
| 2 validate  sustained | 100/s | 5m | — | — | — | — | ☐ |
| 3 large logs | 100 VU | 3m | — | — | — | — | ☐ |
| 4 SSE watchers | 200 VU | 2m | — | — | — | — | ☐ |

目标（`spec/01-goals.md`）：单实例 500+ 并发流水线；API P99 &lt; 200ms（非长任务）。

## 瓶颈与调优

（实测后填写：DB 索引 / Redis pool / connection pool 等）

## Grafana

（粘贴截图或链接）

## 复现

```bash
export HELIOS_API=http://localhost:8080 HELIOS_TOKEN=... HELIOS_ORG_ID=1
k6 run scripts/perf/scenario1_runs_burst.js
```
