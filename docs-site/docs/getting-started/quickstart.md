# 5 分钟快速开始

本指南帮你在本机用 **开发模式** 拉起 Helios（API + Worker + Web），适合评估与日常开发。

## 前置条件

- Go 1.25+、Node 18+、`pnpm`
- Docker（用于 Postgres / Redis / MinIO）

## 步骤

### 1. 克隆与初始化

```bash
git clone https://github.com/helios-cicd/helios.git
cd helios
make setup          # 复制 .env、安装工具链、web 依赖
```

### 2. 启动依赖与迁移

```bash
make dev-deps       # Postgres + Redis + MinIO
make dev-migrate
make dev-seed       # 种子用户 admin / admin12345
```

### 3. 一键开发栈

```bash
make dev            # API :8080 + Worker + Web :3000
```

浏览器打开 http://localhost:3000 ，使用 `admin` / `admin12345` 登录。

### 4. 验证 API

```bash
curl -s http://localhost:8080/api/v1/health
```

### 5. 安装 CLI（可选）

```bash
go install ./cli/cmd/helios
helios login --server http://localhost:8080 --username admin
helios whoami
```

## 下一步

- [核心概念](./concepts)
- [第一个流水线](./first-pipeline)
- 生产部署见 [Docker Compose](./../deployment/docker-compose) 与 [Helm](./../deployment/helm)

## 常见问题

| 问题 | 处理 |
|------|------|
| Redis 端口冲突 | `.env` 中 `HELIOS_REDIS_PORT=6380`（默认已避开 6379） |
| Web 连不上 API | 确认 `NEXT_PUBLIC_API_BASE=http://localhost:8080` |
| 迁移失败 | `make dev-deps` 确保 Postgres 健康后再 `make dev-migrate` |
