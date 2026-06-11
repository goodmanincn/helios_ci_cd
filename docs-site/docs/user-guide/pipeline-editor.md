# 流水线编辑

Helios 支持 **YAML 源码** 与 **可视化画布** 双向编辑（画布以 YAML 为真理来源）。

## YAML 模式

- 编辑器内实时调用 `POST /api/v1/pipelines/validate` 校验
- 保存时创建新版本，可查看历史并回滚

## 画布模式

- 左侧 Stage 列表，中间 DAG，右侧属性面板
- 拖拽连线表达 `needs` 依赖
- 审批节点、矩阵在属性面板配置

## 常用操作

| 操作 | Web | CLI |
|------|-----|-----|
| 校验 | 编辑器内自动 | `helios pipelines validate -f file.yml` |
| 保存 | 保存按钮 | `helios pipelines apply <id> -f file.yml` |
| 版本历史 | 流水线 → 版本 | `GET /api/v1/pipelines/:id/versions` |

## DSL 语法

完整说明见 [DSL 参考](../reference/dsl)。
