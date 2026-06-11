# 第一个流水线

约 30 分钟完成：创建项目 → 克隆模板 → 手动触发 → 查看日志。

## 1. 创建项目

Web：**项目** → **新建**，填写名称与仓库 URL（开发环境可用本地 bare repo）。

CLI：

```bash
# 当前版本以 Web 创建为主；列出已有项目：
helios projects list
```

## 2. 从模板克隆

Web：**市场** → **模板** → 选择例如 `node-docker-k8s` → **克隆到项目**。

CLI：

```bash
helios templates list
helios templates clone node-docker-k8s --project 1 --name my-ci
```

克隆后会进入流水线编辑器（Web）或记下返回的 `pipeline_id`。

## 3. 校验 YAML

```bash
helios pipelines get <pipeline-id> --raw > pipeline.yml
# 编辑后
helios pipelines validate -f pipeline.yml
helios pipelines apply <pipeline-id> -f pipeline.yml -m "tweak config"
```

## 4. 触发 Run

Web：项目详情 → 流水线 → **运行**（manual 触发）。

或通过 API / Webhook（push 到已配置仓库）。

## 5. 查看日志

Web：**执行记录** → 点进 Run → 实时日志面板。

```bash
helios runs list --project 1
helios runs logs <run-id>
```

## 6. 示例仓库

仓库内 `examples/` 目录提供与内置模板对应的参考项目结构（Node / Go / Python 等），可按模板调整 `Dockerfile` 与路径。
