package runengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/helios-cicd/helios/api/pkg/dsl"
	"github.com/helios-cicd/helios/api/pkg/engine"
)

// Bootstrap 首次 orchestrate 时把 pipeline spec 展开并写入 stages/steps 表。
// 幂等: 已有 stage 行则跳过。
func Bootstrap(ctx context.Context, db *sql.DB, runID, versionID int64) error {
	n, err := CountStages(ctx, db, runID)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	raw, err := LoadPipelineSpec(ctx, db, versionID)
	if err != nil {
		return err
	}
	p, err := parsePipeline(raw)
	if err != nil {
		return fmt.Errorf("parse pipeline: %w", err)
	}
	if len(p.Stages) == 0 {
		return fmt.Errorf("pipeline has no stages")
	}

	expanded, err := engine.ExpandPipeline(p)
	if err != nil {
		return err
	}

	for i := range expanded.Stages {
		st := &expanded.Stages[i]
		var matrixIndex *int
		var matrixVals json.RawMessage
		if st.With != nil {
			if idx, ok := st.With["__matrix_index"]; ok {
				if f, ok := idx.(float64); ok {
					v := int(f)
					matrixIndex = &v
				}
			}
			if mv, ok := st.With["__matrix_values"]; ok {
				b, _ := json.Marshal(mv)
				matrixVals = b
			}
		}
		name := st.Name
		if name == "" {
			name = st.ID
		}
		stageRecordID, err := InsertStage(ctx, db, runID, st.ID, name, st.Needs, matrixIndex, matrixVals)
		if err != nil {
			return fmt.Errorf("insert stage %s: %w", st.ID, err)
		}

		steps := st.Steps
		if st.Uses != "" && len(steps) == 0 {
			// stage 级 uses 合成一个 step
			steps = []dsl.Step{{Name: st.Uses, Uses: st.Uses, With: st.With}}
		}
		for j, step := range steps {
			stepName := step.Name
			if stepName == "" {
				if step.Run != "" {
					stepName = fmt.Sprintf("run-%d", j)
				} else {
					stepName = step.Uses
				}
			}
			if _, err := InsertStep(ctx, db, stageRecordID, j, stepName, step.Uses); err != nil {
				return fmt.Errorf("insert step %s: %w", stepName, err)
			}
		}
	}
	return nil
}
