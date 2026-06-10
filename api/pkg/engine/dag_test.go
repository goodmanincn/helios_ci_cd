// Package engine tests — dag + matrix。
package engine

import (
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/helios-cicd/helios/api/pkg/dsl"
)

// ---- helpers ----

func mkStage(id string, needs ...string) dsl.Stage {
	return dsl.Stage{ID: id, Needs: needs, Steps: []dsl.Step{{Run: "echo " + id}}}
}

func mkPipeline(stages ...dsl.Stage) *dsl.Pipeline {
	return &dsl.Pipeline{
		Version: "1",
		Name:    "test",
		Stages:  stages,
	}
}

// ---- BuildDAG ----

func TestBuildDAG_Basic(t *testing.T) {
	// a → b → c, a → d, d 独立无下游
	p := mkPipeline(
		mkStage("a"),
		mkStage("b", "a"),
		mkStage("c", "b"),
		mkStage("d", "a"),
	)
	d := BuildDAG(p)
	require.Len(t, d.Nodes, 4)
	require.Equal(t, []NodeID{"a", "b", "c", "d"}, d.Order)
	require.Empty(t, d.Dangling)

	require.ElementsMatch(t, []NodeID{"b", "d"}, d.Nodes["a"].Out)
	require.ElementsMatch(t, []NodeID{"a"}, d.Nodes["b"].In)
	require.Empty(t, d.Nodes["a"].In)
	require.Empty(t, d.Nodes["c"].Out)
}

func TestBuildDAG_DanglingNeeds(t *testing.T) {
	p := mkPipeline(
		mkStage("a"),
		mkStage("b", "ghost", "a"),
	)
	d := BuildDAG(p)
	require.Equal(t, []NodeID{"ghost"}, d.Dangling["b"])
	require.ElementsMatch(t, []NodeID{"a"}, d.Nodes["b"].In, "ghost 不进 In, 但 a 进了")
}

// ---- DetectCycles ----

func TestDetectCycles_None(t *testing.T) {
	p := mkPipeline(
		mkStage("a"),
		mkStage("b", "a"),
		mkStage("c", "a", "b"),
	)
	d := BuildDAG(p)
	require.Empty(t, d.DetectCycles())
}

func TestDetectCycles_Simple(t *testing.T) {
	// a → b → a
	p := mkPipeline(
		mkStage("a", "b"),
		mkStage("b", "a"),
	)
	d := BuildDAG(p)
	cs := d.DetectCycles()
	require.Len(t, cs, 1)
	// 闭合: [a, b, a] 或 [b, a, b], 任一即可 (起点取决于 DFS 顺序)
	first, last := cs[0].Path[0], cs[0].Path[len(cs[0].Path)-1]
	require.Equal(t, first, last, "环闭合, 起点 = 终点")
	require.Len(t, cs[0].Path, 3)
}

func TestDetectCycles_Longer(t *testing.T) {
	// a → b → c → d → b  (b 自闭)
	p := mkPipeline(
		mkStage("a"),
		mkStage("b", "a", "d"),
		mkStage("c", "b"),
		mkStage("d", "c"),
	)
	d := BuildDAG(p)
	cs := d.DetectCycles()
	require.NotEmpty(t, cs)
	// 至少一个环包含 b/c/d
	ids := map[NodeID]bool{}
	for _, x := range cs[0].Path {
		ids[x] = true
	}
	require.True(t, ids["b"] && ids["c"] && ids["d"], "环应包含 b/c/d, 得到 %v", cs[0].Path)
}

func TestDetectCycles_TwoSeparateCycles(t *testing.T) {
	// 环 1: a↔b   环 2: c↔d  ; 互不相关
	p := mkPipeline(
		mkStage("a", "b"),
		mkStage("b", "a"),
		mkStage("c", "d"),
		mkStage("d", "c"),
	)
	d := BuildDAG(p)
	cs := d.DetectCycles()
	require.Len(t, cs, 2, "两个独立环都应该报")
}

// ---- TopologicalSort ----

func TestTopological_5StageSpec(t *testing.T) {
	// 模拟 spec/04 § 4.1 结构:
	//   checkout → test → build → security-scan → deploy-staging
	p := mkPipeline(
		mkStage("checkout"),
		mkStage("test", "checkout"),
		mkStage("build", "test"),
		mkStage("security-scan", "build"),
		mkStage("deploy-staging", "security-scan"),
	)
	d := BuildDAG(p)
	layers, err := d.TopologicalSort()
	require.NoError(t, err)
	require.Equal(t, [][]NodeID{
		{"checkout"},
		{"test"},
		{"build"},
		{"security-scan"},
		{"deploy-staging"},
	}, layers, "纯链式应该分 5 层")
}

func TestTopological_ParallelLayer(t *testing.T) {
	// a → {b, c} → d
	p := mkPipeline(
		mkStage("a"),
		mkStage("b", "a"),
		mkStage("c", "a"),
		mkStage("d", "b", "c"),
	)
	d := BuildDAG(p)
	layers, err := d.TopologicalSort()
	require.NoError(t, err)
	require.Len(t, layers, 3)
	require.ElementsMatch(t, []NodeID{"b", "c"}, layers[1], "b 和 c 同层并行")
}

func TestTopological_CycleErr(t *testing.T) {
	p := mkPipeline(
		mkStage("a", "b"),
		mkStage("b", "a"),
	)
	d := BuildDAG(p)
	_, err := d.TopologicalSort()
	require.Error(t, err)
	var ce *CycleError
	require.True(t, errors.As(err, &ce), "应该返 *CycleError, 得 %T", err)
	require.Contains(t, ce.Error(), "cycle:")
}

// ---- ExpandMatrix ----

func TestExpandMatrix_NoMatrix_Passthrough(t *testing.T) {
	s := mkStage("a")
	out, insts, err := ExpandMatrix(&s)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "a", out[0].ID)
	require.Len(t, insts, 1)
}

func TestExpandMatrix_TwoDims(t *testing.T) {
	// matrix: {os: [linux, darwin], arch: [amd64, arm64]} → 4 个
	s := dsl.Stage{
		ID: "build",
		Matrix: &dsl.Matrix{
			Dimensions: map[string][]any{
				"os":   {"linux", "darwin"},
				"arch": {"amd64", "arm64"},
			},
		},
		Steps: []dsl.Step{{Run: "make"}},
	}
	out, insts, err := ExpandMatrix(&s)
	require.NoError(t, err)
	require.Len(t, out, 4, "2x2 = 4 个组合")
	require.Len(t, insts, 4)

	// 收集所有组合, 跟期望一致
	got := map[string]bool{}
	for _, st := range out {
		dim := st.Matrix.Dimensions
		require.Len(t, dim["os"], 1, "展开后每维单值")
		require.Len(t, dim["arch"], 1)
		key := fmt.Sprintf("%v-%v", dim["os"][0], dim["arch"][0])
		got[key] = true
	}
	require.Equal(t, map[string]bool{
		"linux-amd64": true, "linux-arm64": true,
		"darwin-amd64": true, "darwin-arm64": true,
	}, got)

	// id 后缀稳定
	ids := make([]string, 0, 4)
	for _, st := range out {
		ids = append(ids, st.ID)
	}
	sort.Strings(ids)
	require.Equal(t, []string{"build-0", "build-1", "build-2", "build-3"}, ids)
}

func TestExpandMatrix_Exclude(t *testing.T) {
	// 同上, 但 exclude {darwin, arm64} → 3 个
	s := dsl.Stage{
		ID: "build",
		Matrix: &dsl.Matrix{
			Dimensions: map[string][]any{
				"os":   {"linux", "darwin"},
				"arch": {"amd64", "arm64"},
			},
			Exclude: []map[string]any{
				{"os": "darwin", "arch": "arm64"},
			},
		},
	}
	out, _, err := ExpandMatrix(&s)
	require.NoError(t, err)
	require.Len(t, out, 3, "排除一个剩 3")

	// 验证 darwin+arm64 不在结果里
	for _, st := range out {
		dim := st.Matrix.Dimensions
		if dim["os"][0] == "darwin" && dim["arch"][0] == "arm64" {
			t.Fatalf("应该排除 darwin+arm64, 但出现了")
		}
	}
}

func TestExpandMatrix_Include_Append(t *testing.T) {
	// 1 dim + include 1 个额外组合 → 总 3 个
	s := dsl.Stage{
		ID: "build",
		Matrix: &dsl.Matrix{
			Dimensions: map[string][]any{
				"go": {"1.22", "1.23"},
			},
			Include: []map[string]any{
				{"go": "1.21", "extra": "legacy"},
			},
		},
	}
	out, _, err := ExpandMatrix(&s)
	require.NoError(t, err)
	require.Len(t, out, 3)
	// 最后一个 (index=2) 应该是 include 来的, 带 extra
	last := out[2]
	v := last.With["__matrix_values"].(map[string]any)
	require.Equal(t, "1.21", v["go"])
	require.Equal(t, "legacy", v["extra"])
}

func TestExpandMatrix_EmptyDimErr(t *testing.T) {
	s := dsl.Stage{
		ID: "build",
		Matrix: &dsl.Matrix{
			Dimensions: map[string][]any{"os": {}},
		},
	}
	_, _, err := ExpandMatrix(&s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
}

func TestExpandMatrix_AllExcluded_Err(t *testing.T) {
	// 全排除场景应该报错 (不静默返 0 元素)
	s := dsl.Stage{
		ID: "build",
		Matrix: &dsl.Matrix{
			Dimensions: map[string][]any{
				"os": {"linux"},
			},
			Exclude: []map[string]any{{"os": "linux"}},
		},
	}
	_, _, err := ExpandMatrix(&s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "0 stages")
}

func TestExpandMatrix_MatrixContext_InWith(t *testing.T) {
	// 验证展开实例携带 __matrix_index / __matrix_values 给 expr 求值
	s := dsl.Stage{
		ID: "build",
		Matrix: &dsl.Matrix{
			Dimensions: map[string][]any{"v": {"a", "b"}},
		},
		With: map[string]any{"keep": "yes"}, // 原 with 保留
	}
	out, _, err := ExpandMatrix(&s)
	require.NoError(t, err)
	require.Len(t, out, 2)
	for i, st := range out {
		require.Equal(t, "yes", st.With["keep"], "原 with 字段保留")
		require.Equal(t, i, st.With["__matrix_index"])
		require.NotNil(t, st.With["__matrix_values"])
	}
}

// ---- ExpandPipeline (matrix + needs 重连) ----

func TestExpandPipeline_NoMatrix_Passthrough(t *testing.T) {
	p := mkPipeline(mkStage("a"), mkStage("b", "a"))
	out, err := ExpandPipeline(p)
	require.NoError(t, err)
	require.Len(t, out.Stages, 2)
	require.Equal(t, "a", out.Stages[0].ID)
	require.Equal(t, []string{"a"}, out.Stages[1].Needs)
}

func TestExpandPipeline_MatrixUpstream_RewiresDownstream(t *testing.T) {
	// test 矩阵 3 个, build needs test → build.needs 应该变成 [test-0, test-1, test-2]
	p := &dsl.Pipeline{
		Version: "1", Name: "x",
		Stages: []dsl.Stage{
			{ID: "checkout", Steps: []dsl.Step{{Run: "git clone"}}},
			{
				ID:    "test",
				Needs: []string{"checkout"},
				Matrix: &dsl.Matrix{
					Dimensions: map[string][]any{"go": {"1.21", "1.22", "1.23"}},
				},
				Steps: []dsl.Step{{Run: "go test"}},
			},
			{
				ID:    "build",
				Needs: []string{"test"},
				Steps: []dsl.Step{{Run: "go build"}},
			},
		},
	}
	out, err := ExpandPipeline(p)
	require.NoError(t, err)
	require.Len(t, out.Stages, 1+3+1, "checkout + 3 test 实例 + build")

	// 找 build, 检查 needs 已重连
	var build *dsl.Stage
	for i := range out.Stages {
		if out.Stages[i].ID == "build" {
			build = &out.Stages[i]
		}
	}
	require.NotNil(t, build)
	require.ElementsMatch(t, []string{"test-0", "test-1", "test-2"}, build.Needs)

	// test-* 自己仍然 needs checkout (没被矩阵, 保持原 id)
	for i := range out.Stages {
		s := &out.Stages[i]
		if s.ID[:4] == "test" && s.ID != "test" {
			require.Equal(t, []string{"checkout"}, s.Needs)
		}
	}
}

func TestExpandPipeline_BuildDAG_Integration(t *testing.T) {
	// 端到端: ExpandPipeline → BuildDAG → TopologicalSort
	// 含矩阵, 期望: 第一层 checkout, 第二层 test-0/1/2 并行, 第三层 build
	p := &dsl.Pipeline{
		Version: "1", Name: "x",
		Stages: []dsl.Stage{
			{ID: "checkout", Steps: []dsl.Step{{Run: "git"}}},
			{
				ID:    "test",
				Needs: []string{"checkout"},
				Matrix: &dsl.Matrix{
					Dimensions: map[string][]any{"go": {"1.22", "1.23"}},
				},
				Steps: []dsl.Step{{Run: "go test"}},
			},
			{ID: "build", Needs: []string{"test"}, Steps: []dsl.Step{{Run: "build"}}},
		},
	}
	expanded, err := ExpandPipeline(p)
	require.NoError(t, err)

	d := BuildDAG(expanded)
	require.Empty(t, d.Dangling)

	layers, err := d.TopologicalSort()
	require.NoError(t, err)
	require.Len(t, layers, 3, "checkout / test-* / build")
	require.Equal(t, []NodeID{"checkout"}, layers[0])
	require.ElementsMatch(t, []NodeID{"test-0", "test-1"}, layers[1])
	require.Equal(t, []NodeID{"build"}, layers[2])
}
