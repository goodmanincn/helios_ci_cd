// Package plugin — seed.go: 启动时把官方插件写入 plugins / plugin_versions.
//
// 设计:
//   - slug 冲突跳过, 不覆盖 (避免 seed 改变让运行中的 installation 漂移)
//   - 同名 plugin 已存在但当前 latest version 字段缺失 → 不修复 (留人工)
//   - 每个 builtin 插件 action.yml 必须能通过 ParseActionYAML 才会写入, 否则启动报错
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
)

// SeedOfficial 把内置官方插件写入. 返已 seed 数量, err 表示首个失败 (启动应中止).
func SeedOfficial(db *gorm.DB) (int, error) {
	added := 0
	for _, b := range officialBuiltins() {
		action, errs := ParseActionYAML([]byte(b.ActionYML))
		if len(errs) > 0 {
			return added, fmt.Errorf("seed %s/%s@%s: %v", b.Namespace, b.Name, b.Version, errs)
		}
		actionJSON, _ := json.Marshal(action)

		var existing model.Plugin
		err := db.Where("namespace = ? AND name = ?", b.Namespace, b.Name).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return added, err
		}
		var pluginID int64
		if errors.Is(err, gorm.ErrRecordNotFound) {
			p := &model.Plugin{
				Namespace:     b.Namespace,
				Name:          b.Name,
				Description:   b.Description,
				Category:      b.Category,
				Publisher:     "Helios Official",
				Repository:    "https://github.com/helios-cicd/official-plugins",
				Verified:      true,
				Official:      true,
				LatestVersion: b.Version,
			}
			if err := db.Create(p).Error; err != nil {
				return added, err
			}
			pluginID = p.ID
			added++
		} else {
			pluginID = existing.ID
		}

		// version 已存在跳过
		var vCount int64
		db.Model(&model.PluginVersion{}).
			Where("plugin_id = ? AND version = ?", pluginID, b.Version).
			Count(&vCount)
		if vCount > 0 {
			continue
		}
		v := &model.PluginVersion{
			PluginID:   pluginID,
			Version:    b.Version,
			ActionYML:  b.ActionYML,
			ActionSpec: datatypes.JSON(actionJSON),
			Readme:     b.Readme,
			Changelog:  b.Changelog,
			IsLatest:   true,
		}
		// 把同 plugin 旧的 is_latest 撤掉
		db.Model(&model.PluginVersion{}).
			Where("plugin_id = ?", pluginID).
			UpdateColumn("is_latest", false)
		if err := db.Create(v).Error; err != nil {
			return added, err
		}
		db.Model(&model.Plugin{}).Where("id = ?", pluginID).
			UpdateColumn("latest_version", b.Version)
	}
	return added, nil
}

// builtinDef 一个 seed 项.
type builtinDef struct {
	Namespace   string
	Name        string
	Version     string
	Description string
	Category    string
	ActionYML   string
	Readme      string
	Changelog   string
}

func officialBuiltins() []builtinDef {
	return []builtinDef{
		{
			Namespace: "helios", Name: "echo", Version: "v1",
			Description: "回显输入到 stdout 并写一个 output (示例插件, 用于验证插件协议)",
			Category:    "demo",
			ActionYML:   actionEchoV1,
			Readme:      readmeEcho,
			Changelog:   "v1: 初始版本",
		},
		{
			Namespace: "helios", Name: "trivy", Version: "v1",
			Description: "用 aquasec/trivy 扫描容器镜像中的 CVE",
			Category:    "security",
			ActionYML:   actionTrivyV1,
			Readme:      readmeTrivy,
			Changelog:   "v1: 支持 image / severity / format",
		},
		{
			Namespace: "helios", Name: "dingtalk", Version: "v1",
			Description: "发送钉钉机器人消息 (支持 markdown / @ 手机号)",
			Category:    "notify",
			ActionYML:   actionDingtalkV1,
			Readme:      readmeDingtalk,
			Changelog:   "v1: 文本/markdown 消息",
		},
		{
			Namespace: "helios", Name: "slack", Version: "v1",
			Description: "发送 Slack 消息 (Incoming Webhook 模式)",
			Category:    "notify",
			ActionYML:   actionSlackV1,
			Readme:      readmeSlack,
			Changelog:   "v1: 文本 + channel + username",
		},
		{
			Namespace: "helios", Name: "email", Version: "v1",
			Description: "发送邮件 (SMTP, 需要 SMTP_HOST / SMTP_USER / SMTP_PASS)",
			Category:    "notify",
			ActionYML:   actionEmailV1,
			Readme:      readmeEmail,
			Changelog:   "v1: 文本 + html 双模式",
		},
		{
			Namespace: "helios", Name: "helm", Version: "v1",
			Description: "Helm 部署 (helm upgrade --install --wait)",
			Category:    "deploy",
			ActionYML:   actionHelmV1,
			Readme:      readmeHelm,
			Changelog:   "v1: chart / namespace / values 三件套",
		},
		{
			Namespace: "helios", Name: "kaniko", Version: "v1",
			Description: "用 Kaniko 无 docker daemon 构建并推送容器镜像",
			Category:    "build",
			ActionYML:   actionKanikoV1,
			Readme:      readmeKaniko,
			Changelog:   "v1: dockerfile / context / destination",
		},
	}
}

const actionEchoV1 = `name: echo
description: 回显输入, 验证 inputs/outputs 协议
author: Helios Official
inputs:
  message:
    description: 要回显的文本
    required: true
    type: string
outputs:
  echoed:
    description: 实际输出的内容
runs:
  using: container
  image: alpine:3
  entrypoint: ["sh", "-c"]
  args:
    - |
      echo "[helios/echo] $INPUT_MESSAGE"
      printf '::set-output name=echoed::%s\n' "$INPUT_MESSAGE"
needs_permissions: []
`

const readmeEcho = `# helios/echo

最小可执行插件, 用于验证 Helios 插件协议:

- inputs 通过 ` + "`INPUT_*`" + ` 环境变量传入
- outputs 通过 ` + "`::set-output name=X::value`" + ` 协议写入 stdout

## 用法

` + "```yaml" + `
- uses: helios/echo@v1
  with:
    message: "hello world"
` + "```" + `
`

const actionTrivyV1 = `name: trivy
description: 用 Aqua Security Trivy 扫描容器镜像
author: Helios Official
inputs:
  image:
    description: 要扫描的镜像 (registry/repo:tag)
    required: true
    type: string
  severity:
    description: 报告的最低严重级别 (UNKNOWN / LOW / MEDIUM / HIGH / CRITICAL)
    default: "HIGH,CRITICAL"
    type: string
  format:
    description: 输出格式 (table/json/sarif)
    default: "table"
    type: string
outputs:
  vulnerabilities_count:
    description: 发现的漏洞总数
runs:
  using: container
  image: aquasec/trivy:latest
  entrypoint: ["sh", "-c"]
  args:
    - |
      trivy image --severity "$INPUT_SEVERITY" --format "$INPUT_FORMAT" "$INPUT_IMAGE"
needs_permissions: [network]
`

const readmeTrivy = `# helios/trivy

镜像漏洞扫描.

## 用法

` + "```yaml" + `
- uses: helios/trivy@v1
  with:
    image: alpine:3.18
    severity: "HIGH,CRITICAL"
` + "```" + `
`

const actionDingtalkV1 = `name: dingtalk
description: 发送钉钉机器人通知
author: Helios Official
inputs:
  webhook:
    description: 钉钉机器人 webhook URL (建议配 secret)
    required: true
    type: string
  message:
    description: 消息内容 (支持 markdown)
    required: true
    type: string
  msg_type:
    description: text / markdown
    default: "text"
    type: string
  title:
    description: markdown 标题 (msg_type=markdown 时用)
    default: "Helios CI/CD"
    type: string
runs:
  using: container
  image: curlimages/curl:8.10.1
  entrypoint: ["sh", "-c"]
  args:
    - |
      if [ "$INPUT_MSG_TYPE" = "markdown" ]; then
        BODY=$(printf '{"msgtype":"markdown","markdown":{"title":"%s","text":"%s"}}' "$INPUT_TITLE" "$INPUT_MESSAGE")
      else
        BODY=$(printf '{"msgtype":"text","text":{"content":"%s"}}' "$INPUT_MESSAGE")
      fi
      curl -sS -H 'Content-Type: application/json' -d "$BODY" "$INPUT_WEBHOOK"
needs_permissions: [network]
needs_secrets: [DINGTALK_WEBHOOK]
`

const readmeDingtalk = `# helios/dingtalk

钉钉机器人通知.

## 用法

` + "```yaml" + `
- uses: helios/dingtalk@v1
  with:
    webhook: ${{ secrets.DINGTALK_WEBHOOK }}
    message: "构建已完成"
` + "```" + `
`

// ===== Slack =====

const actionSlackV1 = `name: slack
description: Slack Incoming Webhook 消息
author: Helios Official
inputs:
  webhook:
    description: Slack Incoming Webhook URL
    required: true
    type: string
  message:
    description: 文本内容
    required: true
    type: string
  channel:
    description: 频道 (覆盖 webhook 默认), 例如 #builds
    default: ""
    type: string
  username:
    description: 显示昵称
    default: "Helios CI"
    type: string
runs:
  using: container
  image: curlimages/curl:8.10.1
  entrypoint: ["sh", "-c"]
  args:
    - |
      EXTRA=""
      if [ -n "$INPUT_CHANNEL" ]; then
        EXTRA=$(printf ',"channel":"%s"' "$INPUT_CHANNEL")
      fi
      BODY=$(printf '{"text":"%s","username":"%s"%s}' "$INPUT_MESSAGE" "$INPUT_USERNAME" "$EXTRA")
      curl -sS -H 'Content-Type: application/json' -d "$BODY" "$INPUT_WEBHOOK"
needs_permissions: [network]
needs_secrets: [SLACK_WEBHOOK]
`

const readmeSlack = `# helios/slack

Slack Incoming Webhook 通知.

## 用法

` + "```yaml" + `
- uses: helios/slack@v1
  with:
    webhook: ${{ secrets.SLACK_WEBHOOK }}
    message: ":rocket: build *#${{ github.run_id }}* passed"
    channel: "#ci"
` + "```" + `
`

// ===== Email (SMTP) =====

const actionEmailV1 = `name: email
description: 通过 SMTP 发送邮件 (alpine + msmtp 客户端)
author: Helios Official
inputs:
  smtp_host:
    description: SMTP 服务器 (e.g. smtp.gmail.com)
    required: true
    type: string
  smtp_port:
    description: SMTP 端口 (默认 587)
    default: "587"
    type: string
  smtp_user:
    description: SMTP 用户名
    required: true
    type: string
  smtp_pass:
    description: SMTP 密码 / app-specific token
    required: true
    type: string
  from:
    description: 发件人地址
    required: true
    type: string
  to:
    description: 收件人 (逗号分隔)
    required: true
    type: string
  subject:
    description: 主题
    required: true
    type: string
  body:
    description: 正文 (text/plain)
    required: true
    type: string
runs:
  using: container
  # 用 alpine + msmtp 作为最小 SMTP 客户端
  image: alpine:3.20
  entrypoint: ["sh", "-c"]
  args:
    - |
      set -e
      apk add --no-cache --quiet msmtp >/dev/null
      cat >/tmp/msmtprc <<EOF
      account default
      host $INPUT_SMTP_HOST
      port $INPUT_SMTP_PORT
      auth on
      tls on
      tls_starttls on
      from $INPUT_FROM
      user $INPUT_SMTP_USER
      password $INPUT_SMTP_PASS
      EOF
      chmod 600 /tmp/msmtprc
      (
        printf 'From: %s\n' "$INPUT_FROM"
        printf 'To: %s\n' "$INPUT_TO"
        printf 'Subject: %s\n' "$INPUT_SUBJECT"
        printf 'Content-Type: text/plain; charset=UTF-8\n\n'
        printf '%s\n' "$INPUT_BODY"
      ) | msmtp -C /tmp/msmtprc -t
      echo "sent"
needs_permissions: [network]
needs_secrets: [SMTP_PASS]
`

const readmeEmail = `# helios/email

SMTP 邮件通知.

## 用法

` + "```yaml" + `
- uses: helios/email@v1
  with:
    smtp_host: smtp.gmail.com
    smtp_user: ci@example.com
    smtp_pass: ${{ secrets.SMTP_PASS }}
    from: ci@example.com
    to: team@example.com
    subject: "Build #${{ github.run_id }} 完成"
    body: "构建已完成, 详情见 Helios"
` + "```" + `

> 注: 镜像启动后会 ` + "`apk add msmtp`" + ` (~3s); 高频通知建议自打镜像替换 image 字段.
`

// ===== Helm =====

const actionHelmV1 = `name: helm
description: helm upgrade --install --wait, 部署 chart 到 K8s
author: Helios Official
inputs:
  kubeconfig:
    description: kubeconfig 全文 (建议从 secret 取)
    required: true
    type: string
  release:
    description: Helm release 名
    required: true
    type: string
  chart:
    description: chart 引用 (repo/chart 或 ./local-chart)
    required: true
    type: string
  namespace:
    description: 目标 namespace
    default: "default"
    type: string
  values:
    description: 内嵌 values YAML (多行)
    default: ""
    type: string
  version:
    description: chart version (可空 → 用最新)
    default: ""
    type: string
  timeout:
    description: helm 等待 timeout
    default: "5m"
    type: string
outputs:
  release:
    description: 部署完成的 release 名
runs:
  using: container
  image: alpine/helm:3.16.2
  entrypoint: ["sh", "-c"]
  args:
    - |
      set -e
      echo "$INPUT_KUBECONFIG" > /tmp/kubeconfig
      export KUBECONFIG=/tmp/kubeconfig
      EXTRA=""
      if [ -n "$INPUT_VERSION" ]; then EXTRA="$EXTRA --version $INPUT_VERSION"; fi
      if [ -n "$INPUT_VALUES" ]; then
        printf '%s' "$INPUT_VALUES" > /tmp/values.yaml
        EXTRA="$EXTRA -f /tmp/values.yaml"
      fi
      helm upgrade --install "$INPUT_RELEASE" "$INPUT_CHART" \
        --namespace "$INPUT_NAMESPACE" --create-namespace \
        --wait --timeout "$INPUT_TIMEOUT" $EXTRA
      printf '::set-output name=release::%s\n' "$INPUT_RELEASE"
needs_permissions: [network, fs]
needs_secrets: [KUBECONFIG]
`

const readmeHelm = `# helios/helm

Helm chart 部署. 镜像 ` + "`alpine/helm:3.16.2`" + `, 走 ` + "`helm upgrade --install --wait`" + `.

## 用法

` + "```yaml" + `
- uses: helios/helm@v1
  with:
    kubeconfig: ${{ secrets.KUBECONFIG }}
    release: myapp
    chart: bitnami/nginx
    namespace: prod
    values: |
      service:
        type: LoadBalancer
` + "```" + `
`

// ===== Kaniko =====

const actionKanikoV1 = `name: kaniko
description: Kaniko 无 docker daemon 构建并推送容器镜像
author: Helios Official
inputs:
  dockerfile:
    description: Dockerfile 相对路径
    default: "Dockerfile"
    type: string
  context:
    description: 构建 context 目录
    default: "."
    type: string
  destination:
    description: 推送目标镜像 ref (e.g. ccr.example.com/foo/app:v1)
    required: true
    type: string
  build_args:
    description: 多行 KEY=VAL 形式的 build args (每行一个)
    default: ""
    type: string
  registry_username:
    description: registry 用户名
    default: ""
    type: string
  registry_password:
    description: registry 密码 / token
    default: ""
    type: string
outputs:
  image:
    description: 推送成功的镜像 ref
runs:
  using: container
  image: gcr.io/kaniko-project/executor:v1.23.2
  # Kaniko executor 自己是 entrypoint, 直接传 args
  args:
    - "--dockerfile=$(DOCKERFILE)"
    - "--context=$(CONTEXT)"
    - "--destination=$(DESTINATION)"
  env:
    DOCKERFILE: "Dockerfile"
    CONTEXT: "."
    DESTINATION: ""
needs_permissions: [network, fs]
needs_secrets: [REGISTRY_PASSWORD]
`

const readmeKaniko = `# helios/kaniko

无 daemon 构镜像并推送, 适合 K8s pod 内执行 (不需要挂 docker.sock).

## 用法

` + "```yaml" + `
- uses: helios/kaniko@v1
  with:
    destination: ccr.example.com/acme/app:${{ github.sha }}
    registry_password: ${{ secrets.REGISTRY_PASSWORD }}
` + "```" + `

> 注: kaniko 走自己的 entrypoint, 通过 ` + "`$(VAR)`" + ` 占位符插值 args (Kaniko 不解析 ` + "`$VAR`" + ` shell 风格). build_args 字段当前仅作占位, 未真正传给 kaniko (留后续轮次).
`
