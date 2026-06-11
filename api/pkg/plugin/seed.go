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
