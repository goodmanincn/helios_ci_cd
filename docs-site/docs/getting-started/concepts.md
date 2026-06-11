# 核心概念

## 组织 (Organization)

多租户边界。用户可属于多个组织；API 通过 `X-Org-ID` 或 JWT 内 org 列表切换当前工作区。

## 项目 (Project)

对应一个代码仓库（GitHub / GitLab / Gitee / 自建 bare repo）。项目下可有多条流水线。

## 流水线 (Pipeline)

YAML 定义的 DAG：多个 **Stage** 组成，支持 `needs` 依赖、矩阵、审批节点。版本以 `pipeline_versions` 表 append-only 保存。

## 执行 (Run)

一次流水线触发实例。状态：`pending` → `running` → `success` / `failed` / `canceled`。每个 Stage 产生 Step 日志，可通过 Web SSE 或 `helios runs logs` 查看。

## Runner

实际执行 Step 的运行时：

| 类型 | 说明 |
|------|------|
| **container** | 在容器镜像中跑 shell / 内置 step |
| **host** | SSH 到物理机执行 |
| **k8s** | 在接入的集群中创建 Job/Pod |

Worker 负责调度；Runner sidecar 在目标环境执行命令。

## 集群 (Cluster)

已接入的 K8s：自建、腾讯云 TKE、阿里云 ACK。流水线中通过 `helios/k8s-deploy@v1` 等 step 引用集群名。

## 主机 (Host)

SSH 可达的物理机或 VM，用于传统部署场景。

## 密钥 (Secret)

加密存储的凭据（文本、kubeconfig、SSH 私钥、云 AK 等）。流水线通过 `${{ secrets.NAME }}` 引用，**API 永不返回明文**。

## 模板 (Template)

可克隆的流水线样板。内置模板在「模板市场」一键复制到项目。
