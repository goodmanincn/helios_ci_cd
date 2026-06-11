package runengine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// OutputsFile 在 workspace 下持久化 stage outputs (跨 asynq 任务共享)。
type OutputsFile struct {
	path string
	mu   sync.Mutex
}

func NewOutputsFile(workspaceDir string, runID int64) *OutputsFile {
	return &OutputsFile{
		path: filepath.Join(workspaceDir, fmt.Sprintf("%d", runID), "stage_outputs.json"),
	}
}

func (o *OutputsFile) load() (map[string]map[string]any, error) {
	data, err := os.ReadFile(o.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]map[string]any{}, nil
		}
		return nil, err
	}
	var m map[string]map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]map[string]any{}
	}
	return m, nil
}

func (o *OutputsFile) save(m map[string]map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(o.path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(o.path, data, 0o644)
}

// Set 写一个 stage 的 outputs。
func (o *OutputsFile) Set(stageID string, outputs map[string]any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	m, err := o.load()
	if err != nil {
		return err
	}
	cp := make(map[string]any, len(outputs))
	for k, v := range outputs {
		cp[k] = v
	}
	m[stageID] = cp
	return o.save(m)
}

// Snapshot 返回全部 stage outputs 副本。
func (o *OutputsFile) Snapshot() (map[string]map[string]any, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.load()
}
