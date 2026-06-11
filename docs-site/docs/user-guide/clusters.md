# 多云集群接入

## 自建 K8s

1. **集群** → **添加** → 提供商 `selfhosted`
2. 粘贴 kubeconfig 或上传文件
3. **测试连接** 确认版本与节点数

```bash
helios clusters list
helios clusters test --provider selfhosted --kubeconfig ~/.kube/config
```

## 腾讯云 TKE / 阿里云 ACK

1. 先在 **密钥** 中创建云凭据（`tencent_cloud` / `aliyun_cloud`）
2. **集群** → **发现集群** 拉取列表后导入
3. 流水线 `helios/k8s-deploy@v1` 的 `cluster` 字段填注册名

## 多云矩阵部署

使用内置模板 `multi-cloud-tke-ack`：矩阵维度 `cluster: [prod-tke, prod-ack]` 并行 rolling 部署。

## 工作负载与回滚

Web 集群详情页可查看 Workloads、事件、Deployment 修订历史并回滚（API：`/clusters/:id/deployments/:name/history`）。
