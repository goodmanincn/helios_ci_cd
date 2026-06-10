// Package builtin — download_artifact.go: helios/download-artifact@v1
//
// 输入:
//   id:   "<run_id>/<name>" 形式 (upload-artifact 的输出); 或同 run 内简写 "<name>"
//   dest: 解压目标目录 (相对 workspace; 缺省即 workspace 根)
//
// 输出:
//   files: 解出的文件数
//   dest:  实际目标目录 (绝对路径)
//
// 跨 run 引用 (id="123/dist"): 调度器只允许同一 run 的下游 stage 引 (M2 默认),
// 跨 run 引用要 caller 提前授权 (M3); 本 step 不强限制 id 来源, 校验在外层。
package builtin

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/helios-cicd/helios/api/pkg/artifact"
)

type downloadArtifactStep struct{}

func (downloadArtifactStep) Name() string { return "helios/download-artifact@v1" }

func (downloadArtifactStep) Run(ec *ExecContext, inputs map[string]any) (map[string]any, error) {
	if ec == nil || ec.Storage == nil {
		return nil, fmt.Errorf("download-artifact: nil ExecContext / Storage")
	}
	id, err := MustString(downloadArtifactStep{}.Name(), inputs, "id")
	if err != nil {
		return nil, err
	}

	runID, name, err := parseArtifactID(id, ec.RunID)
	if err != nil {
		return nil, fmt.Errorf("download-artifact: %w", err)
	}

	dest := OptString(inputs, "dest", "")
	destDir := ec.WorkDir
	if dest != "" {
		if filepath.IsAbs(dest) {
			destDir = dest
		} else {
			destDir = filepath.Join(ec.WorkDir, dest)
		}
	}

	if ec.Log != nil {
		fmt.Fprintf(ec.Log, "[download-artifact] id=%s run=%d name=%s → %s\n",
			id, runID, name, destDir)
	}

	n, err := artifact.Unpack(ec.Ctx, ec.Storage, runID, name, destDir)
	if err != nil {
		return nil, fmt.Errorf("download-artifact: %w", err)
	}

	if ec.Log != nil {
		fmt.Fprintf(ec.Log, "[download-artifact] extracted files=%d backend=%s\n",
			n, ec.Storage.Name())
	}

	return map[string]any{
		"files": n,
		"dest":  destDir,
	}, nil
}

// parseArtifactID 把 "123/dist" 拆 (runID, name). 缺斜杠则用 currentRun + 整个 id 当 name.
func parseArtifactID(id string, currentRun int64) (int64, string, error) {
	if i := strings.Index(id, "/"); i > 0 {
		runID, err := strconv.ParseInt(id[:i], 10, 64)
		if err != nil {
			return 0, "", fmt.Errorf("invalid id %q (run id portion): %w", id, err)
		}
		name := id[i+1:]
		if name == "" {
			return 0, "", fmt.Errorf("invalid id %q (empty name)", id)
		}
		return runID, name, nil
	}
	// 简写: 当作同 run 同名
	if currentRun <= 0 {
		return 0, "", fmt.Errorf("artifact id %q missing run id portion", id)
	}
	return currentRun, id, nil
}

func init() {
	Register(downloadArtifactStep{})
}
