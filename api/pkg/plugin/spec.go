// Package plugin — Helios 插件市场协议 (M9)。
//
// 一个"插件"= 一份 action.yml 描述符 + 一个执行单元 (容器镜像 / composite stages /
// future: javascript). 设计参考 GitHub Actions 的 metadata syntax, 但裁剪到 Helios 实际能
// 跑的子集, 字段命名尽量对齐以降低用户迁移成本。
//
// MVP 范围:
//   - runs.using: "container" (实际跑)  /  "composite" (只解析, 执行待 E9.1.4)
//   - inputs/outputs 声明 + 简单默认值
//   - needs_permissions 声明 (本轮不强制 sandbox, 装机时展示给用户)
//
// 边界 (本轮不做, 在 doc 里标 deferred):
//   - using: "javascript" — 不解析
//   - composite 真正递归执行 — 解析通过但 runtime 返"not yet supported"
//   - 镜像签名 (cosign) — verified 字段只受 publisher=official 控制
package plugin

// Action 是 action.yml 的顶层结构。
//
// 单文件就足以描述一个插件版本; namespace/name/version 由 registry 元数据决定,
// 不放在 action.yml 里 (避免一份 yaml 被复制改名混用).
type Action struct {
	Name        string             `yaml:"name"        json:"name"`
	Description string             `yaml:"description,omitempty" json:"description,omitempty"`
	Author      string             `yaml:"author,omitempty"      json:"author,omitempty"`
	Inputs      map[string]Input   `yaml:"inputs,omitempty"      json:"inputs,omitempty"`
	Outputs     map[string]Output  `yaml:"outputs,omitempty"     json:"outputs,omitempty"`
	Runs        Runs               `yaml:"runs"        json:"runs"`

	// NeedsSecrets 声明插件需要的 secrets (env 名). 安装时展示 + 运行时若 stage
	// 未通过 secrets: 字段授权对应 NAME, 拒绝执行.
	NeedsSecrets []string `yaml:"needs_secrets,omitempty" json:"needs_secrets,omitempty"`

	// NeedsPermissions 声明权限: "network" / "fs" / "exec".
	// MVP 仅在装机时展示给用户; 真正 sandbox 留后续 E9.1.5.
	NeedsPermissions []string `yaml:"needs_permissions,omitempty" json:"needs_permissions,omitempty"`
}

// Input 描述一个输入参数.
type Input struct {
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty"    json:"required,omitempty"`
	Default     any    `yaml:"default,omitempty"     json:"default,omitempty"`
	// Type: 留 string 兼容性最好; future: enum/number/bool 校验.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
}

// Output 描述一个输出值. value 在 step.With 渲染期不求值; 由插件子进程在 stdout
// 用 `::set-output name=X::value` 协议写出来, worker 解析填充.
type Output struct {
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Value       string `yaml:"value,omitempty"       json:"value,omitempty"`
}

// Runs 执行声明. using 是 discriminator.
type Runs struct {
	Using string `yaml:"using" json:"using"` // container / composite / javascript

	// using=container 时的字段.
	Image      string            `yaml:"image,omitempty"      json:"image,omitempty"`
	Entrypoint []string          `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Args       []string          `yaml:"args,omitempty"       json:"args,omitempty"`
	Env        map[string]string `yaml:"env,omitempty"        json:"env,omitempty"`
	// PullPolicy: "Always" / "IfNotPresent" / "Never" (默认 IfNotPresent).
	PullPolicy string `yaml:"pull_policy,omitempty" json:"pull_policy,omitempty"`

	// using=composite 时的子 steps. 每个 step 与 dsl.Step 结构同形 (uses/run 二选一),
	// 但为避免循环依赖, 这里用裸 map 接, 由 runtime 二次解码到 dsl.Step.
	Steps []map[string]any `yaml:"steps,omitempty" json:"steps,omitempty"`
}

// Validate 对解析后的 Action 做语义校验. 返回所有错误.
//
// 校验项:
//   - Name 非空
//   - Runs.Using ∈ {container, composite, javascript}
//   - using=container 必须有 image
//   - using=composite 必须有 steps (>=1)
//   - using=javascript 返"not yet supported" — MVP 不接 JS
//   - NeedsPermissions 仅允许 network/fs/exec
func (a *Action) Validate() []error {
	var errs []error
	if a == nil {
		return []error{errf("action: nil")}
	}
	if a.Name == "" {
		errs = append(errs, errf("name: required"))
	}
	switch a.Runs.Using {
	case "container":
		if a.Runs.Image == "" {
			errs = append(errs, errf("runs.image: required when using=container"))
		}
	case "composite":
		if len(a.Runs.Steps) == 0 {
			errs = append(errs, errf("runs.steps: required when using=composite"))
		}
	case "javascript":
		errs = append(errs, errf("runs.using=javascript not yet supported (MVP)"))
	case "":
		errs = append(errs, errf("runs.using: required"))
	default:
		errs = append(errs, errf("runs.using: must be container / composite / javascript (got %q)", a.Runs.Using))
	}
	allowedPerm := map[string]bool{"network": true, "fs": true, "exec": true}
	for _, p := range a.NeedsPermissions {
		if !allowedPerm[p] {
			errs = append(errs, errf("needs_permissions: unknown %q (allowed: network/fs/exec)", p))
		}
	}
	for k, in := range a.Inputs {
		if in.Type != "" && in.Type != "string" && in.Type != "number" && in.Type != "bool" {
			errs = append(errs, errf("inputs.%s.type: must be string/number/bool (got %q)", k, in.Type))
		}
	}
	return errs
}
