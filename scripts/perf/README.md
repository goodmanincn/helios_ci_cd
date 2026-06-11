# Helios 性能压测 (M8 T8.4)

工具：**k6**（需本地安装 `brew install k6`）。

## 前置

```bash
export HELIOS_API=http://localhost:8080
export HELIOS_TOKEN=<jwt>
export HELIOS_ORG_ID=1
```

## 场景

| 脚本 | 说明 | 目标 |
|------|------|------|
| `scenario1_runs_burst.js` | 500 并发创建/查询 run | P95 &lt; 5min 完成调度 |
| `scenario2_pipeline_sustained.js` | 100 req/s 触发流水线 30min | 错误率 &lt; 1% |
| `scenario3_large_logs.js` | 100 并发大日志拉取 | 带宽与归档不 OOM |
| `scenario4_sse_watchers.js` | 1000 并发 SSE 日志流 | 连接稳定 |

## 运行

```bash
k6 run scripts/perf/scenario1_runs_burst.js
k6 run scripts/perf/scenario4_sse_watchers.js
```

## 报告

结果写入 `docs/perf/report-template.md`，跑完后填 P50/P95/P99 与 Grafana 截图。

正式压测报告见 `docs/perf/beta-baseline.md`（跑完环境后更新）。
