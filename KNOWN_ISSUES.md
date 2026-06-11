# 已知问题 (v1.0.0-beta.1)

## 功能缺口

| 项 | 说明 | 计划 |
|----|------|------|
| CLI OAuth 登录 | 仅用户名/密码 | v1.1 |
| `helios projects create` | CLI 未实现创建项目 | v1.1 |
| runs 实时日志流 | CLI 仅历史快照，无 SSE follow | v1.1 |
| 部分内置 step | `helios/k8s-deploy@v1` 等依赖 runner 完整实现 | 持续 |

## 运维

| 项 | 说明 |
|----|------|
| 单机 POC | 默认密码仅用于开发，生产必须改 KEK/JWT/DB |
| Helm Chart | 子 chart 依赖需按环境外置 PG/Redis |
| 文档站 | `docs.helios.io` 需配置 GitHub Pages / Cloudflare |

## 安全

- 主机 `test` 端点 dev 模式可无认证 SSH 探测；生产请绑定 `credential_id` + 已知 hosts
- Webhook dev secret 勿用于生产

## 反馈

GitHub Issues: https://github.com/helios-cicd/helios/issues
