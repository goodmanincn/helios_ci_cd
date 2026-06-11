# helios CLI

安装：

```bash
# 开发构建
go install ./cli/cmd/helios

# 发布版（Beta 后）
brew install helios
# 或 curl -sSL https://get.helios.io | bash
```

配置目录：`~/.helios/config.yaml` + `credentials.yaml`。

## 认证

```bash
helios login --server https://helios.example.com
helios whoami
helios logout
```

## 常用命令

```bash
helios projects list
helios pipelines list --project 1
helios pipelines validate -f pipeline.yml
helios runs list --status running
helios runs cancel 99
helios templates clone go-multi-platform -p 1 -n release
helios secrets list
helios hosts test 3
helios hosts dispatch-key 3 --public-key-file ~/.ssh/id_ed25519.pub
```

多环境使用 `--profile staging` 切换 server/token。
