# 05. 多云对接方案

## 5.1 统一抽象

```go
type ClusterProvider interface {
    Connect(ctx context.Context, config ClusterConfig) (kubernetes.Interface, error)
    ListNamespaces(ctx context.Context) ([]string, error)
    Deploy(ctx context.Context, spec DeploymentSpec) (*DeploymentResult, error)
    Rollback(ctx context.Context, deployment string, revision int) error
    GetEvents(ctx context.Context, namespace string) ([]Event, error)
    HealthCheck(ctx context.Context) error
}
```

每个云厂商提供独立实现:

| Provider | 凭据来源 | 关键 SDK |
|---|---|---|
| `selfhosted` | 用户上传 kubeconfig | client-go |
| `tencent-tke` | 腾讯云 CAM + STS | tencentcloud-sdk-go (tke + cam) |
| `aliyun-ack` | 阿里云 RAM + AK/SK | aliyun-go-sdk (cs + sts) |
| `huawei-cce` | 华为云 IAM | huaweicloud-sdk-go (cce) |
| `aws-eks` | IAM Role + STS | aws-sdk-go-v2 (eks + sts) |

## 5.2 接入流程 (以 TKE 为例)

1. 用户在 UI 选择"接入腾讯云 TKE"
2. 填写: SecretId / SecretKey / Region / ClusterId
3. 后端调用 `tke.DescribeClusterKubeconfig` 获取 kubeconfig
4. 加密存储 kubeconfig (Vault),关联 cluster_id
5. 测试连通性: 调用 `clientset.CoreV1().Namespaces().List()`
6. 显示集群健康状态、节点列表、kubernetes 版本

## 5.3 STS 自动刷新

云厂商凭据建议使用 STS 临时凭据:

```go
// 每 50 分钟刷新一次 (TTL 1h)
ticker := time.NewTicker(50 * time.Minute)
go func() {
    for range ticker.C {
        newCred, err := stsClient.AssumeRole(roleArn, sessionName)
        if err != nil { metric.IncrError("sts-refresh") ; continue }
        credentialCache.Set(clusterID, newCred)
    }
}()
```

## 5.4 物理机 / VM 集群

### 主机管理
- 通过 SSH 公钥 / 密码 / 跳板机 接入
- Agent 模式 (可选): 部署 Helios Agent 主动汇报心跳
- 无 Agent 模式: 平台主动 SSH

### 主机组 (Inventory)
```yaml
groups:
  web-cluster:
    hosts: [web-1, web-2, web-3]
    vars:
      nginx_port: 80
  db-primary:
    hosts: [db-master]
  db-replica:
    hosts: [db-slave-1, db-slave-2]
```

### 部署步骤
```yaml
- id: deploy-binary
  uses: helios/ssh-deploy@v1
  with:
    hosts: web-cluster
    parallel: 2          # 滚动并发数
    upload:
      - { src: dist/app, dest: /opt/app/bin/, mode: "0755" }
      - { src: configs/app.yml, dest: /etc/app/ }
    commands:
      - "sudo systemctl restart app"
      - "sleep 5"
      - "curl -f http://localhost:8080/health"
    on_failure: rollback
```

## 5.5 部署目标统一抽象

| 目标 | 适用场景 |
|---|---|
| K8s Deployment / StatefulSet | 容器化无状态/有状态服务 |
| K8s Helm Release | 复杂应用 (含 CRD) |
| K8s Job / CronJob | 一次性 / 定时任务 |
| Docker Compose | 单机多容器开发 / 简单生产 |
| systemd service | 物理机二进制部署 |
| 静态文件 → CDN | 前端发布 (CDN / OSS / COS) |
| Serverless 函数 | 事件驱动小任务 |

## 5.6 多云策略示例

### 跨云灾备
```yaml
- id: deploy-primary
  uses: helios/k8s-deploy@v1
  with: { cluster: prod-tke, ... }

- id: deploy-dr
  if: success()
  uses: helios/k8s-deploy@v1
  with: { cluster: prod-ack, ... }
```

### 流量切换 (DNS)
```yaml
- id: switch-traffic
  uses: helios/dns-update@v1
  with:
    provider: cloudflare
    domain: api.example.com
    target: ${{ vars.new_lb_ip }}
```

### 多区域部署 (Matrix)
```yaml
- id: deploy-multi-region
  matrix:
    region:
      - { cluster: prod-tke-sh, name: shanghai }
      - { cluster: prod-tke-bj, name: beijing }
      - { cluster: prod-ack-hz, name: hangzhou }
  uses: helios/k8s-deploy@v1
  with:
    cluster: ${{ matrix.region.cluster }}
    namespace: prod
```
