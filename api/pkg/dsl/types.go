// Package dsl 定义 Helios Pipeline DSL 的 Go 结构 (YAML 双向)。
//
// 设计原则:
//   - 字段命名严格对齐 spec/04-pipeline-dsl.md (yaml tag 是真理)
//   - 字符串字段允许带 ${{ }} 表达式, 解析阶段不求值, 由 expr 包负责
//   - 所有可选字段用零值表达, json/yaml omitempty 让 round-trip 输出干净
//   - 不在结构上做 validation tag (用 jsonschema/struct 校验, 见 validator.go);
//     这样把"什么是合法 DSL"集中到一个地方而不是散落 struct tag
//
// 边界:
//   - Step 同时支持 run (shell) 和 uses (插件) 两种形式, 都可空但不能同时存在
//   - Stage 同样 type=approval 时不该带 steps; 这个在 semantic 校验里查
//   - Triggers / Matrix 字段类型多变 (字符串/数组/对象), 用 RawMessage 兜底再二次解码
package dsl

import (
	"encoding/json"
)

// Pipeline 顶层结构。
type Pipeline struct {
	Version     string                 `yaml:"version"     json:"version"`
	Name        string                 `yaml:"name"        json:"name"`
	Description string                 `yaml:"description,omitempty" json:"description,omitempty"`
	Triggers    []Trigger              `yaml:"triggers,omitempty"    json:"triggers,omitempty"`
	Env         map[string]string      `yaml:"env,omitempty"         json:"env,omitempty"`
	Variables   map[string]string      `yaml:"variables,omitempty"   json:"variables,omitempty"`
	Stages      []Stage                `yaml:"stages"                json:"stages"`

	// 透传扩展字段 (forward-compat) — 不强校验
	Extra map[string]any `yaml:",inline,omitempty" json:"-"`
}

// Trigger 触发器。on 是 discriminator, 不同 on 值带不同字段。
//
// 当前支持: push / pull_request / schedule / manual / tag
type Trigger struct {
	On          string             `yaml:"on" json:"on"`
	Branches    []string           `yaml:"branches,omitempty"     json:"branches,omitempty"`
	Tags        []string           `yaml:"tags,omitempty"         json:"tags,omitempty"`
	Paths       []string           `yaml:"paths,omitempty"        json:"paths,omitempty"`
	PathsIgnore []string           `yaml:"paths-ignore,omitempty" json:"paths_ignore,omitempty"`
	Cron        string             `yaml:"cron,omitempty"         json:"cron,omitempty"`
	Timezone    string             `yaml:"timezone,omitempty"     json:"timezone,omitempty"`
	Inputs      map[string]Input   `yaml:"inputs,omitempty"       json:"inputs,omitempty"`
}

// Input manual trigger 的可选输入参数。
type Input struct {
	Type        string   `yaml:"type"                 json:"type"`             // string / number / bool / choice
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Options     []string `yaml:"options,omitempty"    json:"options,omitempty"` // type=choice 时枚举
	Default     any      `yaml:"default,omitempty"    json:"default,omitempty"`
	Required    bool     `yaml:"required,omitempty"   json:"required,omitempty"`
}

// Stage 一个执行单元。
//
// 字段说明 (按 spec/04):
//   - ID:        DAG 节点 id, 必须 (字符串, 与其他 stage 唯一)
//   - Name:      展示用名字, 可空 (空时退化到 ID)
//   - Needs:     上游 stage id 列表 (DAG 边)
//   - Type:      "" (默认 normal) / "approval"
//   - RunsOn:    执行环境 (容器 / 主机 标签等), approval 不需要
//   - Matrix:    矩阵展开 (运行时), 展开后实例化多个并行 stage
//   - Steps:     步骤列表 (normal 类型 stage)
//   - Uses/With: 整 stage 复用插件 (与 Steps 二选一, 复合插件)
//   - If:        条件表达式
//   - Outputs:   stage 完成后注入下游的 outputs
//   - Approvers/Mode/Timeout: approval 类型才用
type Stage struct {
	ID        string            `yaml:"id"   json:"id"`
	Name      string            `yaml:"name,omitempty" json:"name,omitempty"`
	Needs     []string          `yaml:"needs,omitempty" json:"needs,omitempty"`
	Type      string            `yaml:"type,omitempty"  json:"type,omitempty"` // "approval" or ""
	If        string            `yaml:"if,omitempty"    json:"if,omitempty"`
	RunsOn    *RunsOn           `yaml:"runs-on,omitempty" json:"runs_on,omitempty"`
	Matrix    *Matrix           `yaml:"matrix,omitempty"  json:"matrix,omitempty"`
	Steps     []Step            `yaml:"steps,omitempty"   json:"steps,omitempty"`
	Uses      string            `yaml:"uses,omitempty"    json:"uses,omitempty"`
	With      map[string]any    `yaml:"with,omitempty"    json:"with,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"     json:"env,omitempty"`
	Secrets   []string          `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	Outputs   map[string]string `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	Services  map[string]Service `yaml:"services,omitempty" json:"services,omitempty"`

	// approval 子字段 (type=approval 时)
	Approvers []string `yaml:"approvers,omitempty"  json:"approvers,omitempty"`
	Mode      string   `yaml:"mode,omitempty"       json:"mode,omitempty"`        // any / all / quorum
	Timeout   string   `yaml:"timeout,omitempty"    json:"timeout,omitempty"`     // Go time.ParseDuration: 1h / 24h / 30s
	OnTimeout string   `yaml:"on_timeout,omitempty" json:"on_timeout,omitempty"` // reject (默认) / approve / pause
}

// Step 一个执行步骤 (run shell / uses 插件 二选一)。
type Step struct {
	ID    string         `yaml:"id,omitempty"    json:"id,omitempty"`
	Name  string         `yaml:"name,omitempty"  json:"name,omitempty"`
	If    string         `yaml:"if,omitempty"    json:"if,omitempty"`
	Run   string         `yaml:"run,omitempty"   json:"run,omitempty"`   // shell script
	Uses  string         `yaml:"uses,omitempty"  json:"uses,omitempty"`  // 插件 ref (e.g. helios/upload-artifact@v1)
	With  map[string]any `yaml:"with,omitempty"  json:"with,omitempty"`  // uses 参数
	Env   map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	WorkingDir string    `yaml:"working-directory,omitempty" json:"working_directory,omitempty"`
	Shell string         `yaml:"shell,omitempty" json:"shell,omitempty"` // bash/sh/pwsh, 默认 sh
	ContinueOnError bool `yaml:"continue-on-error,omitempty" json:"continue_on_error,omitempty"`
	TimeoutMinutes int   `yaml:"timeout-minutes,omitempty" json:"timeout_minutes,omitempty"`
}

// RunsOn 执行环境。
//
// 两种形态:
//   - 字符串 (label/preset, e.g. "ubuntu-latest")
//   - 对象 (容器 / labels / arch)
//
// 用 raw + UnmarshalYAML/JSON 兼容; 暴露 Type / Image / Labels 等结构字段。
type RunsOn struct {
	Type    string   `yaml:"type,omitempty"    json:"type,omitempty"`    // container / host / vm
	Image   string   `yaml:"image,omitempty"   json:"image,omitempty"`   // type=container 时镜像
	Labels  []string `yaml:"labels,omitempty"  json:"labels,omitempty"`  // 调度标签
	Arch    string   `yaml:"arch,omitempty"    json:"arch,omitempty"`    // amd64/arm64
	Service string   `yaml:"service,omitempty" json:"service,omitempty"`

	// 原始未结构化形式 (字符串短格式时落到这里)
	Raw string `yaml:"-" json:"-"`
}

// Matrix 矩阵: 任意键映射到值列表, exclude/include 是数组对象。
type Matrix struct {
	// 用 raw map 兜底; 字段以外的 key 视为矩阵维度
	Dimensions map[string][]any `yaml:",inline,omitempty" json:"dimensions,omitempty"`

	Include []map[string]any `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude []map[string]any `yaml:"exclude,omitempty" json:"exclude,omitempty"`
}

// Service 集成测试用的辅助容器 (e.g. postgres/redis)。
type Service struct {
	Image string            `yaml:"image"       json:"image"`
	Env   map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Ports []string          `yaml:"ports,omitempty" json:"ports,omitempty"`
	Cmd   []string          `yaml:"cmd,omitempty"   json:"cmd,omitempty"`
}

// ---- 辅助 ----

// Clone 浅克隆 Pipeline (renderer 用, 避免改原始 spec)。
// 注意: map/slice 是浅拷, renderer 自己保证不就地改 map。
func (p *Pipeline) Clone() *Pipeline {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// String JSON 化, 主要给 audit_logs / 调试用。
func (p *Pipeline) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}
