# Helios CI/CD 平台 — 系统设计文档

> 版本: v0.1 (设计阶段) · 作者: Hermes Agent · 日期: 2026-06-09

## 文档导览

本设计文档分为 9 个章节,每章独立成文件:

| # | 章节 | 文件 |
|---|---|---|
| 01 | 项目目标与范围 | [01-goals.md](./01-goals.md) |
| 02 | 总体架构 | [02-architecture.md](./02-architecture.md) |
| 03 | 核心模块 | [03-modules.md](./03-modules.md) |
| 04 | 流水线 DSL (YAML) | [04-pipeline-dsl.md](./04-pipeline-dsl.md) |
| 05 | 多云对接方案 | [05-multi-cloud.md](./05-multi-cloud.md) |
| 06 | 数据模型 | [06-data-model.md](./06-data-model.md) |
| 07 | API 设计 | [07-api.md](./07-api.md) |
| 08 | 安全设计 | [08-security.md](./08-security.md) |
| 09 | 部署架构与路线图 | [09-deployment-roadmap.md](./09-deployment-roadmap.md) |

## 视觉资源

- 系统架构图: [../ui/architecture.html](../ui/architecture.html)
- UI 高保真原型入口: [../ui/index.html](../ui/index.html)
  - 项目列表: `ui/projects.html`
  - 流水线编辑器: `ui/pipeline-editor.html`
  - 执行详情 + 实时日志: `ui/run-detail.html`
  - 集群与主机管理: `ui/clusters.html`

## 一句话定位

> Helios 是一个**多云原生、自托管混合**的 CI/CD 平台,统一管理代码仓库、流水线编排、构建发布,可对接自建 K8s、腾讯云 TKE、阿里云 ACK 以及物理机集群,支持用户可视化编排个性化流水线。

## 技术栈

| 层 | 选型 |
|---|---|
| 前端 | Next.js 14 (App Router) + TypeScript + Tailwind + React Flow |
| 后端 | Go 1.22 + Gin + GORM + Asynq |
| 数据库 | PostgreSQL 15 (主库) + Redis 7 (队列/缓存) |
| 对象存储 | S3 兼容 / MinIO |
| 凭据托管 | HashiCorp Vault / 云 KMS |
| 运行时 | Kubernetes (推荐) + Docker / SSH 备选 |
| 可观测性 | Prometheus + Loki + Grafana + OpenTelemetry |
