# 09. 部署架构与路线图

## 9.1 部署架构

### 单实例 (POC / 小团队 < 20 人)
- 单台 4C8G 主机
- Docker Compose 一键拉起
- 内置 PostgreSQL / Redis / MinIO
- 适合 100 流水线 / 日 / 单机 4 并发 run

### 集群部署 (推荐生产)
```
                    ┌────────┐
                    │   LB    │
                    └────┬────┘
                         │
            ┌────────────┼────────────┐
            │            │            │
       ┌────▼───┐   ┌────▼───┐   ┌────▼───┐
       │  API   │   │  API   │   │  API   │  ← 3 副本无状态
       └────┬───┘   └────┬───┘   └────┬───┘
            └────────────┼────────────┘
                         │
       ┌─────────────────┼─────────────────┐
       │                 │                 │
  ┌────▼─────┐    ┌──────▼──────┐   ┌──────▼──────┐
  │  PG HA   │    │ Redis Sent  │   │  MinIO 4N   │
  │ 1P + 2R  │    │  3 nodes    │   │ Distributed │
  └──────────┘    └─────────────┘   └─────────────┘

  Worker 池 (无状态)        Runner 池 (按需弹性)
  └─ 3-10 副本              └─ K8s Job / SSH 节点
```

Helm Chart 提供:
- `helios-api`: API + Engine + Scheduler
- `helios-worker`: Asynq worker
- `helios-postgres`: 可选, 推荐用云数据库
- `helios-redis`: 可选, 推荐用云 Redis
- `helios-runner-k8s`: K8s 模式 runner controller

### 推荐生产配置
| 组件 | 规格 | 副本 |
|---|---|---|
| API | 2C4G | 3+ |
| Worker | 2C4G | 5+ (按并发数) |
| PostgreSQL | 4C16G + 500GB SSD | 1P+2R |
| Redis | 2C4G | Sentinel 3 |
| MinIO | 4C8G + 1TB | 4 节点 |
| Vault | 1C2G | Raft 3 |

## 9.2 监控与告警

### 指标 (Prometheus)
- API: QPS / latency P50/P95/P99 / error rate
- Pipeline Engine: queue depth / scheduling latency / runs in-flight
- Runner: idle / busy / failed
- DB: connection pool / slow query / replication lag
- Business: 流水线成功率 / 平均时长 / DAU

### 日志 (Loki)
- 所有服务 stdout/stderr 自动收集
- 关键字段索引: org_id, project_id, pipeline_id, run_id

### Tracing (OpenTelemetry)
- 一次完整 run 形成一个 trace
- 跨服务上下文传播 (HTTP / gRPC / Asynq task)

### 告警 (Alertmanager)
| 告警 | 阈值 |
|---|---|
| API error rate > 1% | 5min |
| Queue depth > 1000 | 持续 5min |
| Worker 全部 down | 即时 |
| DB replication lag > 30s | 持续 |
| MinIO 节点 down | 即时 |
| 凭据解密失败激增 | 5min > 10 次 |

## 9.3 备份与灾备

- PostgreSQL: WAL 归档 + 每日全量到 S3 (异地)
- MinIO: 双活复制到异地 bucket
- Vault: 自动 snapshot 每小时
- 配置文件: 入 git 版本控制

## 9.4 路线图

### v0.1 (2026 Q3) — 设计与原型
- ✅ 系统设计文档
- ✅ UI 高保真原型
- 🔄 核心数据模型 PoC
- 🔄 自建 K8s + 单 Git 平台跑通

### v0.5 (2026 Q4) — Alpha
- 项目 / 流水线 / 执行全功能 (单实例)
- YAML DSL 完整实现
- K8s + Docker Runner
- 自建 K8s 部署
- 内部 dogfooding

### v1.0 (2027 Q1) — Beta
- 多云对接 (TKE + ACK)
- 物理机 / SSH Runner
- 插件市场 (官方插件 20+)
- OIDC SSO + RBAC
- 集群部署模式 + 高可用
- 邀请客户试用

### v1.5 (2027 Q2) — GA
- 高级部署策略 (Canary / Blue-Green)
- 流水线模板市场
- 制品库对接 (Harbor / Nexus)
- 计费与配额
- 多语言 UI

### v2.0 (2027 H2) — Enterprise
- AI 辅助生成流水线
- ChatOps (DingTalk / Slack 双向)
- 私有插件市场
- Serverless Runner
- 跨 Region 多活
- 商业版 + 开源版分离

## 9.5 代码仓库布局

```
helios/
├── api/              # API Server (Go)
│   ├── cmd/
│   ├── internal/
│   │   ├── handler/
│   │   ├── service/
│   │   ├── repository/
│   │   └── engine/
│   └── pkg/
├── worker/           # Asynq worker
├── runner/           # Runner agent
│   ├── k8s/
│   ├── docker/
│   └── ssh/
├── web/              # Next.js 前端
├── cli/              # helios CLI
├── plugins/          # 官方插件源码
│   ├── k8s-deploy/
│   ├── trivy-scan/
│   └── ...
├── deploy/
│   ├── docker-compose.yml
│   ├── helm/
│   └── terraform/
├── docs/
└── spec/             # 设计文档 (本目录)
```
