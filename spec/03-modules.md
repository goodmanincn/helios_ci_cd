# 03. 核心模块详细设计

## 3.1 Project Service

管理项目元数据、Git 仓库绑定、成员与权限。

**关键字段**:
```sql
projects(id, name, slug, repo_url, repo_type, default_branch,
         owner_id, visibility, created_at)
project_members(project_id, user_id, role)
```

**Git 平台适配** (统一 GitProvider 接口):

```go
type GitProvider interface {
    GetRepo(ctx, repoURL) (*Repo, error)
    ListBranches(ctx, repoURL) ([]string, error)
    GetFileContent(ctx, repoURL, ref, path) ([]byte, error)
    CreateWebhook(ctx, repoURL, callbackURL, secret) error
    VerifyWebhookSignature(headers, body, secret) bool
}
```

实现: GitHubProvider / GitLabProvider / GiteeProvider / GiteaProvider

## 3.2 Pipeline Engine

### YAML 解析与校验
- yaml.v3 解析
- JSON Schema 验证 (内嵌 schema)
- 语义校验: 引用步骤存在、变量引用合法、循环检测

### DAG 构建
- 拓扑排序 (Kahn 算法)
- 检测循环依赖 → 报错
- 支持并行分组、串行链、矩阵展开

### 触发器评估
- **push**: 分支/路径过滤,正则
- **tag**: tag pattern 匹配
- **pull_request**: 源/目标分支过滤
- **schedule**: 标准 cron + 时区
- **manual**: UI / API / CLI 触发
- **webhook**: 通用 webhook,JSON 透传

### 审批工作流
- 配置审批人列表 (用户 / 用户组)
- 配置审批模式 (any / all / quorum N)
- 超时策略 (auto-reject / auto-approve / pause)
- 审批历史与评论

## 3.3 Runner Manager

### Runner 类型
| 类型 | 适用场景 |
|---|---|
| **K8s Runner** | 默认,弹性最强,镜像隔离 |
| **Docker Runner** | 远程 docker daemon,轻量 |
| **SSH Runner** | 物理机直跑命令 |
| **Serverless** | 一次性轻量任务 (函数计算) |
| **Builtin** | 内置 step (k8s-deploy / helm-release 等) |

### 任务分发策略
1. 标签匹配 (runner labels ⊇ job requires)
2. 容量检查 (CPU/Mem 配额)
3. 亲和性 (同项目优先复用 cache)
4. 负载均衡 (least-busy)

### 实时日志
- Runner stdout/stderr → 行缓冲 → gRPC stream → Runner Manager
- Manager 写 Redis Stream (key: `logs:run:{id}:step:{id}`)
- WebSocket subscriber 从 Redis Stream fan-out 给 UI
- Run 结束后批量 flush 到 MinIO,DB 仅保留 offset 索引

## 3.4 Deployment Service

### 部署策略
| 策略 | 说明 |
|---|---|
| **Rolling** | 默认,按 maxUnavailable/maxSurge 滚动 |
| **Blue-Green** | 准备 green → 切流量 → 删 blue |
| **Canary** | 按百分比 5/25/50/100 灰度,中间健康检查 |
| **Recreate** | 全部删后重建 (有状态服务) |

### 回滚机制
- 自动回滚: 健康检查失败 / SLO 触发
- 手动回滚: UI 一键回滚到任意历史版本
- 数据库 migration 兼容性提示
- 回滚操作必须审计

## 3.5 Plugin Registry

### 插件类型
- **Container Action** (主流): 容器化执行,符合契约
- **JS Action**: Node.js 脚本 (轻量)
- **Composite**: 多步骤组合
- **Native Plugin**: Go 编译插件 (官方独享)

### 插件契约 (action.yml)
```yaml
name: trivy-scan
description: Trivy 漏洞扫描
inputs:
  image: { required: true }
  severity: { default: "HIGH,CRITICAL" }
  exit-code: { default: "1" }
outputs:
  report-path: { description: "JSON 报告路径" }
runs:
  using: container
  image: aquasec/trivy:0.50
  args: ["image", "--severity", "${{ inputs.severity }}", "${{ inputs.image }}"]
```

### 插件分发
- 官方插件: 内置,随平台升级
- 社区插件: Helios Hub (helios.com/hub)
- 私有插件: 组织私有,推送到自建 registry
