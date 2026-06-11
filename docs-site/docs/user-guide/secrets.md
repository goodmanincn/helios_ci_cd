# 密钥管理

密钥按 **scope** 存储：`org` / `project` / `pipeline`。类型包括 `text`、`file`、`kubeconfig`、`ssh-key`、`cloud-credential` 等。

## Web

**基础设施** → **密钥** → 新建。填写 scope、名称与值（仅创建时输入一次）。

## CLI

```bash
helios secrets list
helios secrets set MY_TOKEN --scope org --scope-id 1 --type text --from-file ./token.txt
helios secrets rm 42
```

## 流水线引用

```yaml
steps:
  - run: echo "${{ secrets.DEPLOY_TOKEN }}"
```

Stage 声明 `secrets: [DEPLOY_TOKEN]` 时由 Worker 解密注入环境。

## 安全说明

- API **从不**返回密钥明文
- 生产环境必须配置 `HELIOS_KEK_BASE64`（32 字节 AES 密钥）
- 轮换 KEK 见 [配置参考](../deployment/configuration)
