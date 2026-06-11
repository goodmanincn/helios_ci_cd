# 升级指南

## 开发 / 单机

1. 拉取新版本代码或镜像
2. **先跑迁移**：`make dev-migrate` 或 `go -C api run ./cmd/api --migrate`
3. 滚动重启 API → Worker → Web（顺序无关，但建议 API 先于 Worker）
4. 验证 `/api/v1/health` 与登录

## Helm

```bash
helm upgrade helios ./deploy/helm/helios -n helios -f my-values.yaml
```

- Chart 会在 Job 中执行 migrate（若已配置）
- 观察 Pod Ready 与 `helm history`

## 回滚

```bash
helm rollback helios <revision> -n helios
```

数据库迁移**一般不可逆**；down 迁移仅在开发环境使用 `make migrate-down`。

## Beta 版本注意

v1.0.0-beta.1 可能存在 schema 变更，升级前阅读 `CHANGELOG.md` 与 `KNOWN_ISSUES.md`。
