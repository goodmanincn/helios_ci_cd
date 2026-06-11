# Node.js + Docker + K8s 示例

对应模板 `node-docker-k8s`。

## 准备

- `Dockerfile` 在项目根
- Secrets / env：`REGISTRY`、`IMAGE_NAME`；集群名 `staging-k8s`、`prod-k8s` 与平台注册一致
- 审批人 `admin`、`ops` 需在平台存在

## 克隆

```bash
helios templates clone node-docker-k8s -p 1 -n node-ci
```
