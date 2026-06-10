// Package dsl 测试 — 主要场景:
//   - 完整 spec/04 示例能 unmarshal + 校验通过
//   - 10+ 种错误的精确定位 (T2.1.2 验收要求: 故意写错全部能精确报位)
//   - 边界: 空 / 仅 version / 重复 id / 环 / 表达式拼写错
package dsl

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---- T2.1.1: 完整示例往返 ----

// spec/04 § 4.1 的完整 YAML (拷贝, 验证 unmarshal + 全部校验通过)
const specExampleYAML = `version: "1"
name: "build-and-deploy"
description: "API gateway 主分支自动构建并部署"

triggers:
  - on: push
    branches: [main, "release/*"]
    paths-ignore: ["docs/**", "*.md"]
  - on: schedule
    cron: "0 2 * * *"
    timezone: "Asia/Shanghai"
  - on: manual
    inputs:
      target_env:
        type: choice
        options: [staging, prod]
        default: staging

env:
  REGISTRY: ccr.tencentyun.com/acme
  GO_VERSION: "1.22"

variables:
  IMAGE_TAG: "${{ github.sha }}"

stages:
  - id: checkout
    name: "拉取代码"
    runs-on: { type: container, image: "alpine/git:latest" }
    steps:
      - run: git clone --depth 1 ${{ github.repo_url }} src

  - id: test
    name: "单元测试"
    needs: [checkout]
    runs-on:
      type: container
      image: "golang:${{ matrix.go-version }}"
    matrix:
      go-version: ["1.21", "1.22", "1.23"]
    steps:
      - run: |
          cd src
          go mod download
          go test ./... -race -cover

  - id: build
    name: "构建镜像"
    needs: [test]
    runs-on: { type: container, image: "gcr.io/kaniko-project/executor:latest" }
    steps:
      - run: |
          /kaniko/executor \
            --dockerfile=src/Dockerfile \
            --destination=${{ env.REGISTRY }}/api:${{ vars.IMAGE_TAG }}

  - id: security-scan
    name: "安全扫描"
    needs: [build]
    uses: "helios/trivy-scan@v1"
    with:
      image: "${{ env.REGISTRY }}/api:${{ vars.IMAGE_TAG }}"
      severity: "CRITICAL"
      exit-code: "1"

  - id: deploy-staging
    name: "部署 staging"
    needs: [security-scan]
    uses: "helios/k8s-deploy@v1"
    with:
      cluster: staging-tke
      namespace: api
      manifest: src/k8s/deployment.yaml
      image: "${{ env.REGISTRY }}/api:${{ vars.IMAGE_TAG }}"
      strategy: rolling

  - id: approval
    name: "生产部署审批"
    needs: [deploy-staging]
    type: approval
    approvers: [alice, bob]
    mode: any
    timeout: 24h

  - id: deploy-prod
    name: "部署生产"
    needs: [approval]
    uses: "helios/k8s-deploy@v1"
    with:
      cluster: prod-aliyun-ack
      namespace: api
      manifest: src/k8s/deployment.yaml
      image: "${{ env.REGISTRY }}/api:${{ vars.IMAGE_TAG }}"
      strategy: canary

  - id: notify
    name: "发送通知"
    needs: [deploy-prod]
    if: "always()"
    uses: "helios/dingtalk@v1"
    with:
      webhook: "${{ secrets.DINGTALK_WEBHOOK }}"
      message: "API 已部署 · ${{ vars.IMAGE_TAG }} · ${{ run.status }}"
`

func TestParse_SpecExample_Roundtrip(t *testing.T) {
	r, err := Parse([]byte(specExampleYAML))
	require.NoError(t, err)
	require.NotNil(t, r.Pipeline)
	p := r.Pipeline

	require.Equal(t, "1", p.Version)
	require.Equal(t, "build-and-deploy", p.Name)
	require.Len(t, p.Triggers, 3)
	require.Equal(t, "push", p.Triggers[0].On)
	require.Equal(t, []string{"main", "release/*"}, p.Triggers[0].Branches)
	require.Equal(t, "0 2 * * *", p.Triggers[1].Cron)
	require.Equal(t, "Asia/Shanghai", p.Triggers[1].Timezone)
	require.Contains(t, p.Triggers[2].Inputs, "target_env")
	require.Equal(t, "choice", p.Triggers[2].Inputs["target_env"].Type)
	require.Equal(t, []string{"staging", "prod"}, p.Triggers[2].Inputs["target_env"].Options)

	require.Equal(t, "ccr.tencentyun.com/acme", p.Env["REGISTRY"])
	require.Equal(t, "${{ github.sha }}", p.Variables["IMAGE_TAG"])

	require.Len(t, p.Stages, 8)
	// 第 2 stage (test) 带 matrix
	test := p.Stages[1]
	require.Equal(t, "test", test.ID)
	require.Equal(t, []string{"checkout"}, test.Needs)
	require.NotNil(t, test.Matrix)
	require.Contains(t, test.Matrix.Dimensions, "go-version")
	require.Len(t, test.Matrix.Dimensions["go-version"], 3)

	// approval 节点
	apv := p.Stages[5]
	require.Equal(t, "approval", apv.ID)
	require.Equal(t, "approval", apv.Type)
	require.Equal(t, []string{"alice", "bob"}, apv.Approvers)
	require.Equal(t, "any", apv.Mode)
	require.Equal(t, "24h", apv.Timeout)
}

func TestValidate_SpecExample_NoErrors(t *testing.T) {
	_, es := ValidateRaw([]byte(specExampleYAML))
	require.Empty(t, es, "spec example should pass all validation; got: %v", es)
}

// ---- T2.1.2/T2.1.3: 10 种错的 YAML 全部精确报位 ----

func TestValidate_ErrorCases(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		want    string // substring of any error message
		wantPath string // optional, "" 跳过
	}{
		{
			name: "missing version",
			yaml: `name: "x"
stages:
  - id: a
    runs-on: { type: container, image: x }
    steps: [{ run: "echo" }]
`,
			want:    "version is required",
			wantPath: "version",
		},
		{
			name: "missing name",
			yaml: `version: "1"
stages:
  - id: a
    runs-on: { type: container, image: x }
    steps: [{ run: "echo" }]
`,
			want:    "name is required",
			wantPath: "name",
		},
		{
			name: "unsupported version",
			yaml: `version: "2"
name: x
stages:
  - id: a
    runs-on: { type: container, image: x }
    steps: [{ run: "echo" }]
`,
			want: "unsupported version",
		},
		{
			name: "missing stage id",
			yaml: `version: "1"
name: x
stages:
  - name: nopeNoId
    runs-on: { type: container, image: x }
    steps: [{ run: "echo" }]
`,
			want:    "stage id is required",
			wantPath: "stages[0].id",
		},
		{
			name: "duplicate stage id",
			yaml: `version: "1"
name: x
stages:
  - id: build
    steps: [{ run: "echo a" }]
  - id: build
    steps: [{ run: "echo b" }]
`,
			want:    "duplicate stage id",
			wantPath: "stages[1].id",
		},
		{
			name: "needs unknown",
			yaml: `version: "1"
name: x
stages:
  - id: a
    steps: [{ run: "echo" }]
  - id: b
    needs: [ghost]
    steps: [{ run: "echo" }]
`,
			want:    `unknown stage "ghost"`,
			wantPath: "stages[1].needs[0]",
		},
		{
			name: "self-need",
			yaml: `version: "1"
name: x
stages:
  - id: a
    needs: [a]
    steps: [{ run: "echo" }]
`,
			want: "cannot need itself",
		},
		{
			name: "cycle a→b→a",
			yaml: `version: "1"
name: x
stages:
  - id: a
    needs: [b]
    steps: [{ run: "echo" }]
  - id: b
    needs: [a]
    steps: [{ run: "echo" }]
`,
			want: "cycle detected",
		},
		{
			name: "step missing run/uses",
			yaml: `version: "1"
name: x
stages:
  - id: a
    steps:
      - name: empty
`,
			want: "step must have either run or uses",
		},
		{
			name: "step run and uses both",
			yaml: `version: "1"
name: x
stages:
  - id: a
    steps:
      - run: "echo"
        uses: "helios/foo@v1"
`,
			want: "cannot have both run and uses",
		},
		{
			name: "approval missing approvers",
			yaml: `version: "1"
name: x
stages:
  - id: a
    type: approval
`,
			want: "approval stage must declare approvers",
		},
		{
			name: "approval bad mode",
			yaml: `version: "1"
name: x
stages:
  - id: a
    type: approval
    approvers: [alice]
    mode: vote
`,
			want: "approval mode \"vote\" invalid",
		},
		{
			name: "expression unknown ref root",
			yaml: `version: "1"
name: x
stages:
  - id: a
    steps:
      - run: "echo ${{ universe.answer }}"
`,
			want: "unknown reference root",
		},
		{
			name: "expression unbalanced",
			yaml: `version: "1"
name: x
stages:
  - id: a
    steps:
      - run: "echo ${{ vars.x"
`,
			want: "unbalanced ${{ }}",
		},
		{
			name: "empty expression",
			yaml: `version: "1"
name: x
stages:
  - id: a
    steps:
      - run: "echo ${{  }}"
`,
			want: "empty expression",
		},
		{
			name: "stage id invalid char",
			yaml: `version: "1"
name: x
stages:
  - id: "bad id with space"
    steps: [{ run: "echo" }]
`,
			want: "invalid",
		},
		{
			name: "approval cannot have steps",
			yaml: `version: "1"
name: x
stages:
  - id: a
    type: approval
    approvers: [alice]
    steps: [{ run: "echo" }]
`,
			want: "approval stage must not have steps",
		},
		{
			name: "approval timeout bad format",
			yaml: `version: "1"
name: x
stages:
  - id: a
    type: approval
    approvers: [alice]
    timeout: "5x"
`,
			want: "approval timeout \"5x\" invalid",
		},
		{
			name: "approval on_timeout invalid",
			yaml: `version: "1"
name: x
stages:
  - id: a
    type: approval
    approvers: [alice]
    on_timeout: explode
`,
			want: "approval on_timeout \"explode\" invalid",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, es := ValidateRaw([]byte(tc.yaml))
			require.NotEmpty(t, es, "expected errors, got none")
			matched := false
			for _, e := range es {
				if strings.Contains(e.Message, tc.want) {
					matched = true
					if tc.wantPath != "" {
						require.Equal(t, tc.wantPath, e.Path,
							"path mismatch for case %s: %v", tc.name, e)
					}
					require.Greater(t, e.Line, 0, "error should have line: %v", e)
					break
				}
			}
			require.True(t, matched,
				"none of %d errors contained %q; errors: %v", len(es), tc.want, es)
		})
	}
}

// ---- syntax 错误 ----

func TestParse_SyntaxError(t *testing.T) {
	_, err := Parse([]byte(`
stages:
  - id: a
   bad-indent: x  # 缩进非法
`))
	require.Error(t, err)
	de, ok := err.(*Error)
	require.True(t, ok, "expected *Error, got %T", err)
	require.Equal(t, ErrSyntax, de.Kind)
	require.Greater(t, de.Line, 0)
}

// ---- empty doc ----

func TestParse_Empty(t *testing.T) {
	_, err := Parse([]byte(``))
	require.Error(t, err)
	de, ok := err.(*Error)
	require.True(t, ok)
	require.Equal(t, ErrSyntax, de.Kind)
}

// ---- 累积错误 (确认非 fail-fast) ----

func TestValidate_AccumulatesErrors(t *testing.T) {
	// 4 个独立错误同时存在
	src := `version: "1"
stages:
  - id: build
    steps:
      - name: x   # 无 run/uses → 1 个错
  - id: build    # 重复 id → 1 个错
    steps: [{ run: "echo" }]
  - id: deploy
    needs: [ghost]   # 引用不存在 → 1 个错
    steps: [{ run: "echo ${{ universe.x }}" }]  # 未知 ref → 1 个错
`
	_, es := ValidateRaw([]byte(src))
	require.GreaterOrEqual(t, len(es), 4,
		"expected at least 4 accumulated errors, got %d: %v", len(es), es)
}

// ---- approval 合法变体: timeout/on_timeout 空 + 合法格式 都过 ----

func TestValidate_ApprovalTimeoutAndOnTimeout_OK(t *testing.T) {
	cases := []string{
		// 空 timeout + 空 on_timeout
		`version: "1"
name: x
stages:
  - id: a
    type: approval
    approvers: [alice]
`,
		// 合法 timeout + on_timeout reject
		`version: "1"
name: x
stages:
  - id: a
    type: approval
    approvers: [alice]
    timeout: "30s"
    on_timeout: reject
`,
		// 各种合法 duration + on_timeout 三选项
		`version: "1"
name: x
stages:
  - id: a
    type: approval
    approvers: [alice]
    timeout: "1h30m"
    on_timeout: approve
  - id: b
    type: approval
    needs: [a]
    approvers: [alice]
    timeout: "24h"
    on_timeout: pause
`,
	}
	for i, y := range cases {
		t.Run(fmt.Sprintf("ok-%d", i), func(t *testing.T) {
			_, es := ValidateRaw([]byte(y))
			require.Empty(t, es, "expected no errors, got: %v", es)
		})
	}
}
