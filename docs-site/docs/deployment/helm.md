# Helm Chart 生产部署

Chart 路径：`deploy/helm/helios/`。

## 安装

```bash
helm repo add helios https://charts.helios.io   # Beta 后可用
helm install helios helios/helios \
  --namespace helios --create-namespace \
  -f my-values.yaml
```

本地 Chart：

```bash
helm install helios ./deploy/helm/helios -n helios --create-namespace
```

## 关键 Values

| 字段 | 说明 |
|------|------|
| `image.repository` / `tag` | API/Worker 镜像 |
| `postgresql.enabled` | 内置 PG（生产建议外置云数据库） |
| `redis.enabled` | 内置 Redis |
| `minio.enabled` | 对象存储 |
| `config.database.*` | 外置 DB 连接 |
| `config.jwt.*` | JWT 密钥路径与 TTL |

## 高可用建议

- API 3+ 副本 + Ingress/LB
- Worker 按并发水平扩展
- Postgres 主从 + Redis Sentinel
- MinIO 分布式或对接云 OSS

详细架构见仓库 `spec/09-deployment-roadmap.md`。
