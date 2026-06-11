# 流水线模板示例

与内置模板 `api/pkg/builtintmpl/` 对应，便于克隆后改路径与密钥。

| 目录 | 模板 slug | 说明 |
|------|-----------|------|
| `node-docker-k8s/` | `node-docker-k8s` | Node + Docker + K8s + 审批 |
| `go-multi-platform/` | `go-multi-platform` | Go 矩阵 + Release |
| `static-site-s3/` | `static-site-s3` | Next 静态站 + S3 |
| `python-pypi/` | `python-pypi` | pytest + PyPI |
| `multi-cloud-tke-ack/` | `multi-cloud-tke-ack` | TKE/ACK 矩阵 |

使用：在 Web **模板市场** 克隆，或：

```bash
helios templates clone <slug> --project <id> --name <name>
```

各子目录 README 说明所需 Secrets 与环境变量。
