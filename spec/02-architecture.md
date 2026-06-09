# 02. 总体架构

## 2.1 分层视图

```
┌──────────────────────────────────────────────────────────┐
│  L1 接入层  Web UI (Next.js) · CLI (helios) · OpenAPI     │
├──────────────────────────────────────────────────────────┤
│  L2 网关    API Gateway: 认证 / RBAC / 限流 / 审计         │
├──────────────────────────────────────────────────────────┤
│  L3 核心    Project · Pipeline Engine · Runner Manager    │
│             Deployment · Cluster · Secret · Plugin        │
│             Trigger · Notification · Audit                │
├──────────────────────────────────────────────────────────┤
│  L4 执行    K8s Runner · Docker · SSH · Serverless        │
├──────────────────────────────────────────────────────────┤
│  L5 数据    PostgreSQL · Redis (Asynq) · S3/MinIO · Vault │
├──────────────────────────────────────────────────────────┤
│  L6 目标    自建 K8s · TKE · ACK · 物理机 / VM 集群        │
└──────────────────────────────────────────────────────────┘
```

完整可视化架构图见 `ui/architecture.html`。

## 2.2 核心组件职责

### Project Service
管理项目元数据、Git 仓库绑定、成员与权限。

### Pipeline Engine (流水线引擎)
- YAML DSL 解析与校验
- DAG 构建与拓扑排序
- 触发器评估 (push / tag / schedule / manual / webhook)
- 人工审批工作流
- 流水线版本管理

### Runner Manager (Runner 调度器)
- Runner 池管理 (注册 / 心跳 / 健康检查)
- 任务分发 (按标签 / 容量 / 亲和性)
- 实时日志收集 (WebSocket pub/sub)
- 任务取消 / 重试 / 超时控制

### Deployment Service
- 部署策略 (Rolling / Blue-Green / Canary / Recreate)
- 流量切换
- 健康检查与自动回滚
- 部署历史与 diff

### Cluster Service
- 多云适配 (统一 ClusterProvider 接口)
- 集群健康检查
- 命名空间 / 节点资源监控
- 工作负载列表与事件流

### Secret Vault
- AES-256-GCM 加密存储
- KMS 信封加密 (KEK / DEK 分离)
- 按 scope 隔离 (project / pipeline / global)
- 审计追踪 (谁/何时/为何使用)

### Plugin Registry
- 官方 / 社区 / 私有插件市场
- 容器化插件 (Action 风格)
- 版本管理与依赖解析

## 2.3 关键流程: 一次完整构建

```
1. Git push → Git 平台 → Webhook ──┐
2. Trigger Service 校验签名 + 匹配规则
3. Pipeline Engine 加载流水线版本 → 渲染变量 → 生成 Run
4. 创建 Asynq 任务,推入 Redis 队列
5. Runner Manager 出队 → 选择合适 Runner
6. Runner Pod (K8s Job) 启动 → 拉代码 → 执行各 stage 步骤
7. 日志实时通过 WebSocket 推到 UI
8. Stage 完成后更新 DB 状态,触发下游 stage
9. 遇到 approval 节点 → 暂停 → 等待 UI 审批
10. Deployment Service 调用对应 ClusterProvider 部署
11. Run 结束 → 日志归档 MinIO → 发送通知 (钉钉/邮件)
12. Audit Service 记录全过程
```

## 2.4 高可用设计

| 组件 | 高可用策略 |
|---|---|
| API Server | 无状态,多副本 + LB |
| Pipeline Engine | 多副本 + Redis 分布式锁 |
| Scheduler | Leader Election (k8s lease) |
| Worker | 多副本,Asynq 自动 fail-over |
| PostgreSQL | Primary + 2 Replica (流复制),pgbouncer |
| Redis | Sentinel 模式,3 副本 |
| MinIO | 分布式 4 节点 |
| Vault | Raft 共识,3 节点 |
