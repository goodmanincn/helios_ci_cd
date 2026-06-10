// Package engine — matrix.go: 矩阵展开 (T2.2.2)。
//
// 输入:  一个 dsl.Stage (含 Matrix.Dimensions / Include / Exclude)
// 输出:  []dsl.Stage  — 展开后多个并行实例;每个实例的 ID 是 "<原id>-<index>",
//                     Matrix 字段被填充成单值 (1×1 矩阵, 保留 matrix_values 上下文)
//
// 算法:
//   1. 取 Dimensions 笛卡尔积 → 候选组合列表 (每个组合是 map[k]v)
//   2. 应用 Exclude: 子集匹配则去掉 (与 GitHub Actions 一致: exclude 的 key 必须全部命中)
//   3. 应用 Include: 直接追加额外组合 (含完整或部分 key, 不参与笛卡尔)
//   4. 给每个组合生成一份 Stage 拷贝, ID 加 suffix, Matrix.Dimensions 写成单值 map
//
// 设计:
//   - 不修改原 stage; 返回全新 slice
//   - matrix_index 通过 stage.With["__matrix_index"] 写入 (engine 内部约定, expr 求值用),
//     dsl 层不需要看到这字段
//   - Caller 用法: 把展开结果替换回 pipeline.Stages, 同时把所有原 needs[orig] 改成
//     needs 全部展开实例 (这一步在调度器 / DAG 构建后做; matrix.go 只产 stages, 不改 needs)
package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/helios-cicd/helios/api/pkg/dsl"
)

// MatrixInstance 展开实例的元信息 (调试 + 上下文求值用)。
type MatrixInstance struct {
	Index  int                // 0-based
	Values map[string]any     // 该实例的 dim 取值
}

// ExpandMatrix 把含 matrix 的 stage 展开成多个;
// 不含 matrix 的 stage 原样返回 (单元素 slice, 内含原 stage 拷贝)。
//
// 排序约定:
//   - Dimensions 按 key 字典序固定笛卡尔轴 (避免运行不稳定)
//   - 每个轴内值按出现顺序 (yaml 顺序)
//
// 错误情况: 维度为空 / 维度值非 scalar 时返 error。
func ExpandMatrix(s *dsl.Stage) ([]dsl.Stage, []MatrixInstance, error) {
	if s == nil {
		return nil, nil, fmt.Errorf("nil stage")
	}
	if s.Matrix == nil || (len(s.Matrix.Dimensions) == 0 && len(s.Matrix.Include) == 0) {
		// 无矩阵: 直接返回单元素 (深拷贝足够浅)
		cp := *s
		return []dsl.Stage{cp}, []MatrixInstance{{Index: 0}}, nil
	}

	// 1) 笛卡尔积
	combos, err := cartesian(s.Matrix.Dimensions)
	if err != nil {
		return nil, nil, err
	}

	// 2) Exclude 过滤
	if len(s.Matrix.Exclude) > 0 {
		filtered := combos[:0]
		for _, c := range combos {
			if !matchesAnyExclude(c, s.Matrix.Exclude) {
				filtered = append(filtered, c)
			}
		}
		combos = filtered
	}

	// 3) Include 追加 (允许 include 没有完整 dim 的对象, 与 Actions 一致)
	for _, inc := range s.Matrix.Include {
		c := make(map[string]any, len(inc))
		for k, v := range inc {
			c[k] = v
		}
		combos = append(combos, c)
	}

	if len(combos) == 0 {
		return nil, nil, fmt.Errorf("matrix expanded to 0 stages (all combos excluded?)")
	}

	// 4) 拷贝 Stage, 写 suffix id + 写 matrix dim 单值
	stages := make([]dsl.Stage, 0, len(combos))
	insts := make([]MatrixInstance, 0, len(combos))
	for i, c := range combos {
		cp := *s
		cp.ID = matrixSuffix(s.ID, i, c)
		cp.Matrix = &dsl.Matrix{
			Dimensions: pinSingleValue(c),
		}
		// 写 engine 内部约定: with.__matrix_index / with.__matrix_values
		if cp.With == nil {
			cp.With = map[string]any{}
		} else {
			// shallow copy 防止改原 stage
			ncp := make(map[string]any, len(cp.With)+2)
			for k, v := range cp.With {
				ncp[k] = v
			}
			cp.With = ncp
		}
		cp.With["__matrix_index"] = i
		cp.With["__matrix_values"] = c
		stages = append(stages, cp)
		insts = append(insts, MatrixInstance{Index: i, Values: c})
	}
	return stages, insts, nil
}

// cartesian 笛卡尔积, 按 key 字典序固定轴顺序。
func cartesian(dims map[string][]any) ([]map[string]any, error) {
	if len(dims) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(dims))
	for k := range dims {
		// 过滤 engine 内部约定字段 (理论 dsl 不该有, 防御性)
		if strings.HasPrefix(k, "__") {
			continue
		}
		if len(dims[k]) == 0 {
			return nil, fmt.Errorf("matrix dimension %q is empty", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	combos := []map[string]any{{}}
	for _, k := range keys {
		vals := dims[k]
		next := make([]map[string]any, 0, len(combos)*len(vals))
		for _, c := range combos {
			for _, v := range vals {
				nc := make(map[string]any, len(c)+1)
				for kk, vv := range c {
					nc[kk] = vv
				}
				nc[k] = v
				next = append(next, nc)
			}
		}
		combos = next
	}
	return combos, nil
}

// matchesAnyExclude exclude 规则: 子集语义 (exclude 里所有 k 都命中才算)。
func matchesAnyExclude(c map[string]any, excludes []map[string]any) bool {
	for _, ex := range excludes {
		if subsetEq(ex, c) {
			return true
		}
	}
	return false
}

func subsetEq(sub, full map[string]any) bool {
	if len(sub) == 0 {
		return false // 空子集不构成 exclude (与 Actions 一致, 避免误杀)
	}
	for k, v := range sub {
		if fv, ok := full[k]; !ok || !valEq(fv, v) {
			return false
		}
	}
	return true
}

// valEq 浅比较 (string/number/bool); yaml decode 出来的数字常是 int 或 float64, 兼容下。
func valEq(a, b any) bool {
	if a == b {
		return true
	}
	// 字符串化 fallback (覆盖 int vs int64, float 等)
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// pinSingleValue 把组合 map → Dimensions 形态 (每个 key 一个单元素切片)。
func pinSingleValue(c map[string]any) map[string][]any {
	out := make(map[string][]any, len(c))
	for k, v := range c {
		out[k] = []any{v}
	}
	return out
}

// matrixSuffix 用 dim 值生成稳定后缀 (避免 collision)。
//   build → build-0 / build-1 ... (按 index, 简单稳定)
// 多 dim 时 dim 名+值会很长, 用 index 更简洁; 真正名字调试通过 With["__matrix_values"] 看。
func matrixSuffix(id string, idx int, _ map[string]any) string {
	return id + "-" + strconv.Itoa(idx)
}

// ExpandPipeline 全 pipeline 矩阵展开 + needs 重连。
//
// 行为:
//   1. 遍历 stages, 含 matrix 的展开为多实例 (ExpandMatrix)
//   2. 不含 matrix 的原样保留, ID 不变
//   3. 任何 stage 的 needs[X] 若 X 被展开过, 自动改为 X 的所有实例 id (e.g. test → test-0, test-1, test-2)
//   4. ID 已被使用的下游 (downstream 自己也 matrix) 自然继承新 needs 集合
//
// 返回新的 Pipeline (浅拷贝顶层 + 新 stages slice), 不改原 p。
//
// 调用时机: dsl.ValidateRaw 通过后, 调度器 BuildDAG 之前。
// (校验阶段不展开是有意的: 校验报错时用户看到的是原始 stage id, 不是 "build-0" 这种)
func ExpandPipeline(p *dsl.Pipeline) (*dsl.Pipeline, error) {
	if p == nil {
		return nil, fmt.Errorf("nil pipeline")
	}

	// 1) 展开每个 stage, 同时构建 originID → []newID 映射
	expansion := make(map[string][]string, len(p.Stages))
	allStages := make([]dsl.Stage, 0, len(p.Stages))
	for i := range p.Stages {
		s := &p.Stages[i]
		instances, _, err := ExpandMatrix(s)
		if err != nil {
			return nil, fmt.Errorf("stage %q: %w", s.ID, err)
		}
		ids := make([]string, 0, len(instances))
		for _, inst := range instances {
			ids = append(ids, inst.ID)
		}
		expansion[s.ID] = ids
		allStages = append(allStages, instances...)
	}

	// 2) 重连 needs: 每个 stage 实例的 needs[X] → expansion[X] 全部 id
	for i := range allStages {
		s := &allStages[i]
		if len(s.Needs) == 0 {
			continue
		}
		newNeeds := make([]string, 0, len(s.Needs))
		seen := make(map[string]bool, len(s.Needs))
		for _, dep := range s.Needs {
			expanded, ok := expansion[dep]
			if !ok {
				// 引用未知 stage; 保留让后续 DAG/Dangling 处理
				if !seen[dep] {
					newNeeds = append(newNeeds, dep)
					seen[dep] = true
				}
				continue
			}
			for _, e := range expanded {
				if !seen[e] {
					newNeeds = append(newNeeds, e)
					seen[e] = true
				}
			}
		}
		s.Needs = newNeeds
	}

	cp := *p
	cp.Stages = allStages
	return &cp, nil
}
