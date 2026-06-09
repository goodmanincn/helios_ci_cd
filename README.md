# Helios — 多云原生 CI/CD 平台

> 自托管 / 多云 / 可视化流水线 / 类 GitHub Actions DSL

## 这是什么

Helios 是一个面向国内团队的自托管 CI/CD 平台,核心特性:

- **多云原生**:同一条流水线可同时部署到自建 K8s、腾讯 TKE、阿里 ACK、物理机
- **可视化编辑**:拖拽生成流水线,YAML 与画布双向同步
- **类 GitHub Actions DSL**:学习成本低,生态可借鉴
- **企业级安全**:RBAC + SSO + 密钥保险箱 + 审计日志
- **轻量部署**:docker-compose 单机 / Helm Chart 集群,30 分钟上手

## 状态

🚧 **开发中** — 当前阶段:M0 地基

详见 [项目路线图](./spec/ROADMAP.md)。

## 仓库结构

```
api/        Go REST API 服务 (Gin + GORM)
worker/     Go 异步任务 worker (Asynq)
runner/     Go Runner (Docker / SSH / K8s)
cli/        helios 命令行工具 (Cobra)
web/        Next.js 14 前端 (React Flow + Tailwind)
db/         数据库迁移 SQL
deploy/     部署配置 (docker / helm / k8s)
spec/       设计文档 + 任务清单
ui/         UI 高保真原型 (HTML)
scripts/    开发辅助脚本
```

## 快速开始 (开发环境)

前置:Go ≥ 1.22 · Node ≥ 20 · pnpm ≥ 9 · Docker ≥ 24 · Make

```bash
git clone <repo>
cd helios
make dev          # 一键启动后端依赖 + api + web
```

打开 http://localhost:3000 — 默认账号 `admin / admin12345`

## 文档

- [设计文档总入口](./spec/00-README.md)
- [项目路线图](./spec/ROADMAP.md)
- [任务清单 (M0~M9)](./spec/tasks/)
- [UI 原型](./ui/index.html)

## 协议

[MIT](./LICENSE)
