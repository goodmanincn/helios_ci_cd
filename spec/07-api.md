# 07. API 设计

## 7.1 总体约定

- **风格**: REST + JSON, WebSocket 用于实时日志
- **版本**: URL 前缀 `/api/v1/`
- **认证**: `Authorization: Bearer <jwt>` 或 `X-API-Token: <pat>`
- **错误格式**:
  ```json
  {
    "error": {
      "code": "PIPELINE_VALIDATION_FAILED",
      "message": "stage 'build' references unknown stage 'compile'",
      "details": { "stage_id": "build", "missing_dep": "compile" }
    }
  }
  ```
- **分页**: cursor 风格 `?limit=50&cursor=<opaque>`, 返回 `next_cursor`

## 7.2 核心端点

### 项目
| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/projects` | 列出项目 (筛选: org/q/status) |
| POST | `/api/v1/projects` | 创建项目 |
| GET | `/api/v1/projects/{id}` | 项目详情 |
| PATCH | `/api/v1/projects/{id}` | 更新项目 |
| DELETE | `/api/v1/projects/{id}` | 删除项目 |
| POST | `/api/v1/projects/{id}/sync` | 触发 Git 同步 |

### 流水线
| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/projects/{pid}/pipelines` | 项目下流水线列表 |
| POST | `/api/v1/projects/{pid}/pipelines` | 创建流水线 |
| GET | `/api/v1/pipelines/{id}` | 流水线详情 (当前版本) |
| PUT | `/api/v1/pipelines/{id}/spec` | 更新 YAML/spec → 新建版本 |
| GET | `/api/v1/pipelines/{id}/versions` | 版本历史 |
| POST | `/api/v1/pipelines/validate` | 仅校验 YAML, 不保存 |
| POST | `/api/v1/pipelines/{id}/trigger` | 手动触发 (body 可带 inputs) |
| PATCH | `/api/v1/pipelines/{id}/enabled` | 启用/禁用 |

### 执行
| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/runs?pipeline_id=&status=` | 执行列表 |
| GET | `/api/v1/runs/{id}` | 执行详情 (含 stages/steps) |
| POST | `/api/v1/runs/{id}/cancel` | 取消 |
| POST | `/api/v1/runs/{id}/retry` | 重新运行 |
| POST | `/api/v1/runs/{id}/approvals/{stage}` | 提交审批 (approve/reject) |
| GET | `/api/v1/runs/{id}/logs?step=&offset=` | 拉取日志 (HTTP polling) |
| WS | `/api/v1/runs/{id}/logs/stream?step=` | 实时日志 (WebSocket) |
| GET | `/api/v1/runs/{id}/artifacts` | 制品列表 |

### 集群
| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/clusters` | 集群列表 |
| POST | `/api/v1/clusters` | 接入集群 (body 含 provider/credential) |
| POST | `/api/v1/clusters/test` | 测试连通性 (不保存) |
| GET | `/api/v1/clusters/{id}/namespaces` | 命名空间列表 |
| GET | `/api/v1/clusters/{id}/workloads?ns=` | 工作负载列表 |
| GET | `/api/v1/clusters/{id}/events` | 集群事件 |
| DELETE | `/api/v1/clusters/{id}` | 解绑 |

### 主机
| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/hosts` | 主机列表 |
| POST | `/api/v1/hosts` | 添加主机 |
| POST | `/api/v1/hosts/{id}/test` | 测试 SSH 连通性 |
| POST | `/api/v1/hosts/{id}/exec` | 远程执行单次命令 (审计) |
| GET | `/api/v1/host-groups` | 主机组列表 |
| POST | `/api/v1/host-groups` | 创建主机组 |

### 密钥
| Method | Path | 说明 |
|---|---|---|
| GET | `/api/v1/secrets?scope=&scope_id=` | 列出 (仅元数据,无 value) |
| POST | `/api/v1/secrets` | 创建 |
| PUT | `/api/v1/secrets/{id}` | 更新 (轮换) |
| DELETE | `/api/v1/secrets/{id}` | 删除 |

### Webhook (Git 平台回调)
| Method | Path | 说明 |
|---|---|---|
| POST | `/webhooks/github/{project_id}` | GitHub 回调 (HMAC-SHA256 校验) |
| POST | `/webhooks/gitlab/{project_id}` | GitLab 回调 (X-Gitlab-Token) |
| POST | `/webhooks/gitee/{project_id}` | Gitee 回调 |
| POST | `/webhooks/custom/{token}` | 通用 webhook (token 鉴权) |

## 7.3 WebSocket 实时日志

```
GET /api/v1/runs/247/logs/stream?step=build-kaniko
Upgrade: websocket
```

服务端推送 (JSON Lines):
```json
{"ts":"10:23:31.420","stream":"stdout","msg":"+ go build -o /app ./cmd/api"}
{"ts":"10:23:42.118","stream":"stdout","msg":"--> Build complete (10.7s)"}
{"ts":"10:23:48.044","stream":"meta","event":"step_finished","status":"success"}
```

## 7.4 CLI 对应

```bash
# CLI 是 OpenAPI 的瘦封装
helios auth login --server https://helios.example.com
helios projects list
helios pipelines trigger build-and-deploy --branch main
helios runs logs 247 --follow --step build
helios secrets set DB_PASSWORD --scope project --project api-gateway
helios clusters add --provider tke --region ap-shanghai --cluster-id cls-xxx
```
