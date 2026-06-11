# 通知配置

Helios 支持流水线内 **通知 Step**（如钉钉）以及平台级通知中心（M7）。

## 流水线内通知

```yaml
stages:
  - id: notify
    needs: [deploy]
    if: "always()"
    uses: "helios/dingtalk@v1"
    with:
      webhook: "${{ secrets.DINGTALK_WEBHOOK }}"
      message: "部署完成 · ${{ run.status }}"
```

Webhook 类密钥请放在 Secrets，不要写入 YAML 明文。

## 通知中心

Web：**设置** → 通知规则，可配置 Run 失败、审批待办等事件推送到 Webhook / 邮件（具体渠道以当前版本 UI 为准）。

## 最佳实践

- 生产部署链末尾加 `if: failure()` 告警 Stage
- 审批超时在审批节点配置 `timeout` 与 `on_timeout`
