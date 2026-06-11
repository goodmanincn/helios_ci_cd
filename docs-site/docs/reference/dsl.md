# 流水线 DSL 参考

YAML 是**唯一真理来源**。完整规范见仓库 `spec/04-pipeline-dsl.md`。

## 顶层字段

```yaml
version: "1"          # 必填，当前仅支持 "1"
name: "my-pipeline"
description: "可选说明"
triggers: [...]
env: { KEY: val }     # 流水线级环境变量
variables: { TAG: "${{ github.sha }}" }
stages: [...]         # 必填，至少一个 stage
```

## Stage

```yaml
- id: build              # 必填，字母开头
  name: "构建"
  needs: [test]          # 依赖其他 stage id
  if: "success()"
  runs-on: { type: container, image: "node:20" }
  matrix:
    go-version: ["1.22", "1.23"]
  steps: [...]
  # 或插件 stage（与 steps 二选一）
  uses: "helios/k8s-deploy@v1"
  with: { cluster: prod, namespace: app }
```

### 审批节点

```yaml
- id: approval
  type: approval
  needs: [staging]
  approvers: [alice, bob]
  mode: any              # any / all / quorum
  timeout: 24h
  on_timeout: reject
```

## Step

```yaml
- id: test
  name: "单元测试"
  run: |
    npm test
  # 或
  uses: "helios/upload-artifact@v1"
  with:
    name: dist
    path: dist/*
  env:
    NODE_ENV: test
  if: "success()"
```

## 表达式

| 前缀 | 含义 |
|------|------|
| `env.` | 流水线 `env` |
| `vars.` | `variables` |
| `secrets.` | 密钥（运行时注入） |
| `matrix.` | 矩阵维度 |
| `github.sha` / `github.ref_name` | Git 上下文 |
| `run.status` | 当前 Run 状态 |
| `needs.<id>.outputs.<k>` | 上游输出 |

函数：`success()` `failure()` `always()` `contains()` `startsWith()` 等。

## 内置 Step

| uses | 说明 |
|------|------|
| `helios/upload-artifact@v1` | 上传制品 |
| `helios/download-artifact@v1` | 下载制品 |
| `helios/ssh-deploy@v1` | SSH 部署 |

插件市场 step（如 `helios/dingtalk@v1`）需先安装对应插件。

## 校验

```bash
helios pipelines validate -f pipeline.yml
```

API：`POST /api/v1/pipelines/validate`
