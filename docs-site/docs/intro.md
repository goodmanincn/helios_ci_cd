# Helios CI/CD 文档

Helios 是**国内自托管、多云原生**的 CI/CD 平台，提供类 GitHub Actions 的 YAML DSL、可视化流水线编辑器，以及自建 K8s / 腾讯云 TKE / 阿里云 ACK / SSH 物理机部署能力。

## 你能做什么

- 用 YAML 或画布编辑流水线，触发构建与部署
- 接入多个 Git 仓库与多云 K8s 集群
- 用 RBAC、密钥保险箱与审计满足企业安全要求
- 通过 `helios` CLI 在终端完成日常操作

## 推荐阅读路径

1. [5 分钟快速开始](./getting-started/quickstart) — 本地拉起并登录
2. [核心概念](./getting-started/concepts) — 项目 / 流水线 / Run / Runner
3. [第一个流水线](./getting-started/first-pipeline) — 从模板克隆并触发执行
4. [部署指南](./deployment/docker-compose) — 单机或 Helm 生产部署

## 版本

当前文档对应 **v1.0 Beta** 里程碑。英文文档将在后续版本补充。
