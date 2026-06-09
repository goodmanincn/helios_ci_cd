# 04. 流水线 DSL (YAML)

YAML 是规范的真理来源 (source of truth),UI 编辑器双向转换。

## 4.1 完整示例

```yaml
# helios.pipeline.yml
version: "1"
name: "build-and-deploy"
description: "API gateway 主分支自动构建并部署"

triggers:
  - on: push
    branches: [main, "release/*"]
    paths-ignore: ["docs/**", "*.md"]
  - on: schedule
    cron: "0 2 * * *"
    timezone: "Asia/Shanghai"
  - on: manual
    inputs:
      target_env:
        type: choice
        options: [staging, prod]
        default: staging

env:
  REGISTRY: ccr.tencentyun.com/acme
  GO_VERSION: "1.22"

variables:
  IMAGE_TAG: "${{ github.sha }}"

stages:
  - id: checkout
    name: "拉取代码"
    runs-on: { type: container, image: alpine/git:latest }
    steps:
      - run: git clone --depth 1 ${{ github.repo_url }} src

  - id: test
    name: "单元测试"
    needs: [checkout]
    runs-on:
      type: container
      image: "golang:${{ matrix.go-version }}"
    matrix:
      go-version: ["1.21", "1.22", "1.23"]
    steps:
      - run: |
          cd src
          go mod download
          go test ./... -race -cover

  - id: build
    name: "构建镜像"
    needs: [test]
    runs-on: { type: container, image: gcr.io/kaniko-project/executor:latest }
    steps:
      - run: |
          /kaniko/executor \
            --dockerfile=src/Dockerfile \
            --destination=${{ env.REGISTRY }}/api:${{ vars.IMAGE_TAG }}

  - id: security-scan
    name: "安全扫描"
    needs: [build]
    uses: helios/trivy-scan@v1
    with:
      image: "${{ env.REGISTRY }}/api:${{ vars.IMAGE_TAG }}"
      severity: "CRITICAL"
      exit-code: "1"

  - id: deploy-staging
    name: "部署 staging"
    needs: [security-scan]
    uses: helios/k8s-deploy@v1
    with:
      cluster: staging-tke
      namespace: api
      manifest: src/k8s/deployment.yaml
      image: "${{ env.REGISTRY }}/api:${{ vars.IMAGE_TAG }}"
      strategy: rolling

  - id: approval
    name: "生产部署审批"
    needs: [deploy-staging]
    type: approval
    approvers: [alice, bob]
    mode: any
    timeout: 24h

  - id: deploy-prod
    name: "部署生产"
    needs: [approval]
    uses: helios/k8s-deploy@v1
    with:
      cluster: prod-aliyun-ack
      namespace: api
      manifest: src/k8s/deployment.yaml
      image: "${{ env.REGISTRY }}/api:${{ vars.IMAGE_TAG }}"
      strategy: canary
      canary:
        steps: [10, 25, 50, 100]
        interval: 5m

  - id: notify
    name: "发送通知"
    needs: [deploy-prod]
    if: always()
    uses: helios/dingtalk@v1
    with:
      webhook: ${{ secrets.DINGTALK_WEBHOOK }}
      message: "API 已部署 · ${{ vars.IMAGE_TAG }} · ${{ run.status }}"
```

## 4.2 DSL 规范要点

### 表达式语法
- 变量引用: `${{ vars.X }}` / `${{ env.X }}` / `${{ inputs.X }}` / `${{ secrets.X }}`
- 上下文对象: `github.*`, `run.*`, `matrix.*`, `needs.<id>.outputs.*`
- 运算符: `==`, `!=`, `&&`, `||`, `!`, `>`, `<`, `+`, `-`
- 函数: `contains()`, `startsWith()`, `format()`, `fromJSON()`, `success()`, `failure()`, `always()`

### 条件执行
```yaml
- id: deploy
  if: "branch == 'main' && success()"
  ...
```

### 矩阵展开
```yaml
matrix:
  os: [linux, darwin]
  arch: [amd64, arm64]
  exclude:
    - { os: darwin, arch: arm64 }  # 排除组合
# 自动展开为 3 个并行任务
```

### 制品传递
```yaml
- id: build
  outputs:
    artifact-id: "${{ steps.upload.outputs.id }}"
  steps:
    - id: upload
      uses: helios/upload-artifact@v1
      with: { path: dist/ }

- id: deploy
  needs: [build]
  steps:
    - uses: helios/download-artifact@v1
      with: { id: "${{ needs.build.outputs.artifact-id }}" }
```

### 服务依赖 (test container)
```yaml
- id: integration-test
  services:
    postgres:
      image: postgres:15
      env: { POSTGRES_PASSWORD: test }
      ports: ["5432:5432"]
    redis:
      image: redis:7
  steps:
    - run: pytest tests/integration/
```

## 4.3 校验级别

1. **语法校验**: YAML 解析,基本结构
2. **Schema 校验**: JSON Schema 严格匹配
3. **语义校验**:
   - `needs` 引用的 stage 存在
   - `uses` 插件存在且版本可用
   - 变量引用合法 (拼写、scope)
4. **DAG 校验**: 无循环、有终点
5. **资源校验**: cluster / runner 标签可达

校验失败在 UI 实时高亮,API 调用 `/pipelines/validate` 可独立校验。
