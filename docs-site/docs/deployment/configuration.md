# 配置参考

完整样例：仓库根目录 `.env.example`。

## API 核心

| 变量 | 说明 |
|------|------|
| `HELIOS_API_ADDR` | 监听地址，如 `:8080` |
| `HELIOS_DB_DSN` | Postgres 连接串 |
| `HELIOS_REDIS_ADDR` | Redis 地址 |
| `HELIOS_KEK_BASE64` | Secrets 加密主密钥（32 字节 base64） |
| `HELIOS_JWT_PRIVATE_KEY_PATH` | JWT RS256 私钥 |
| `HELIOS_JWT_PUBLIC_KEY_PATH` | JWT 公钥 |
| `HELIOS_CORS_ALLOW_ORIGIN` | 前端 Origin 白名单 |

## 对象存储

| 变量 | 说明 |
|------|------|
| `HELIOS_S3_ENDPOINT` | MinIO / S3 端点 |
| `HELIOS_S3_ACCESS_KEY` | Access key |
| `HELIOS_S3_SECRET_KEY` | Secret key |
| `HELIOS_S3_BUCKET_ARTIFACTS` | 制品桶 |
| `HELIOS_S3_BUCKET_LOGS` | 日志归档桶 |

## Worker / Runner

| 变量 | 说明 |
|------|------|
| `HELIOS_WORKSPACE_DIR` | Run 工作区根目录 |
| `HELIOS_BUILD_RUNTIME` | `host` / `kubernetes` |
| `HELIOS_LOG_ARCHIVE_DIR` | 日志落盘目录 |

## Web

| 变量 | 说明 |
|------|------|
| `NEXT_PUBLIC_API_BASE` | 浏览器访问的 API 根 URL |

## Helm values

见 `deploy/helm/helios/values.yaml` 中 `config` 段，与上述环境变量一一对应。
