# Docker Compose 单机部署

## 开发依赖（推荐入门）

仅拉起 **Postgres + Redis + MinIO**，应用本机 `go run`：

```bash
docker compose -f deploy/docker/dev.compose.yml up -d
make dev-migrate && make dev-seed && make dev
```

等价：`make dev-deps`。

## 单机全栈（POC）

适合 &lt; 20 人团队评估。应用进程仍建议用 systemd 或单独容器化（官方镜像 `v1.0.0-beta.1` 发布后替换 `image` 字段）。

```bash
# 1. 依赖
docker compose -f deploy/docker/dev.compose.yml up -d

# 2. 配置 .env（见 configuration.md）

# 3. 迁移与种子
make dev-migrate && make dev-seed

# 4. 构建并运行（示例）
make build-go
./bin/helios-api &
./bin/helios-worker &
cd web && pnpm build && pnpm start
```

生产 POC 请至少：

- 修改所有默认密码
- 配置 TLS 反向代理（Nginx / Caddy）
- 挂载持久卷备份 Postgres

## 端口默认

| 服务 | 端口 |
|------|------|
| API | 8080 |
| Web | 3000 |
| Postgres | 5432 |
| Redis | 6380（host） |
| MinIO | 9000 / 9001 |
