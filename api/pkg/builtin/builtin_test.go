// builtin_test.go — upload/download-artifact + registry 测试 (端到端走 LocalFS).
package builtin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/helios-cicd/helios/api/pkg/artifact"
)

func newEC(t *testing.T) (*ExecContext, string, string) {
	t.Helper()
	work := t.TempDir()
	store := t.TempDir()
	// 铺一些样例文件
	require.NoError(t, os.MkdirAll(filepath.Join(work, "dist/css"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(work, "dist/index.html"), []byte("hi"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(work, "dist/css/a.css"), []byte("body{}"), 0o644))

	ec := &ExecContext{
		Ctx:     context.Background(),
		RunID:   500,
		StageID: "build",
		WorkDir: work,
		Storage: artifact.NewLocalFS(store),
		Log:     &bytes.Buffer{},
	}
	return ec, work, store
}

func TestRegistry_HasBuiltin(t *testing.T) {
	u, ok := Lookup("helios/upload-artifact@v1")
	require.True(t, ok)
	require.NotNil(t, u)
	d, ok := Lookup("helios/download-artifact@v1")
	require.True(t, ok)
	require.NotNil(t, d)

	_, ok = Lookup("nonexistent/x@v1")
	require.False(t, ok)
}

func TestUploadArtifact_OK(t *testing.T) {
	ec, _, store := newEC(t)
	step, _ := Lookup("helios/upload-artifact@v1")

	outs, err := step.Run(ec, map[string]any{
		"name": "dist",
		"path": "dist/",
	})
	require.NoError(t, err)
	require.Equal(t, "500/dist", outs["id"])
	require.Equal(t, "dist", outs["name"])
	require.Equal(t, 2, outs["files"])

	// .tar.gz + manifest 都在 storage
	_, err = os.Stat(filepath.Join(store, "artifacts/500/dist.tar.gz"))
	require.NoError(t, err)
}

func TestUploadArtifact_PathListAndGlob(t *testing.T) {
	ec, _, _ := newEC(t)
	step, _ := Lookup("helios/upload-artifact@v1")

	// path 用列表 + 单文件 (yaml 通常解出来是 []any)
	outs, err := step.Run(ec, map[string]any{
		"name": "mix",
		"path": []any{"dist/index.html", "dist/css/*.css"},
	})
	require.NoError(t, err)
	require.Equal(t, 2, outs["files"])
}

func TestUploadArtifact_MissingInputs(t *testing.T) {
	ec, _, _ := newEC(t)
	step, _ := Lookup("helios/upload-artifact@v1")

	_, err := step.Run(ec, map[string]any{"path": "dist/"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `missing input "name"`)

	_, err = step.Run(ec, map[string]any{"name": "x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "path required")
}

func TestDownloadArtifact_Roundtrip_SameRun(t *testing.T) {
	ec, _, _ := newEC(t)
	up, _ := Lookup("helios/upload-artifact@v1")
	dn, _ := Lookup("helios/download-artifact@v1")

	// 1) 先 upload
	upOuts, err := up.Run(ec, map[string]any{"name": "dist", "path": "dist/"})
	require.NoError(t, err)
	id := upOuts["id"].(string)

	// 2) 模拟下游 stage: 新 workspace, 同 run id
	dstWork := t.TempDir()
	ec2 := *ec
	ec2.WorkDir = dstWork

	dnOuts, err := dn.Run(&ec2, map[string]any{
		"id":   id,
		"dest": "downloaded",
	})
	require.NoError(t, err)
	require.Equal(t, 2, dnOuts["files"])

	// 解出内容验证
	html, err := os.ReadFile(filepath.Join(dstWork, "downloaded/dist/index.html"))
	require.NoError(t, err)
	require.Equal(t, "hi", string(html))
}

func TestDownloadArtifact_ShortID_SameRun(t *testing.T) {
	// id 简写为 "dist" → 当前 run 同名
	ec, _, _ := newEC(t)
	up, _ := Lookup("helios/upload-artifact@v1")
	dn, _ := Lookup("helios/download-artifact@v1")

	_, err := up.Run(ec, map[string]any{"name": "dist", "path": "dist/"})
	require.NoError(t, err)

	dst := t.TempDir()
	ec2 := *ec
	ec2.WorkDir = dst
	outs, err := dn.Run(&ec2, map[string]any{"id": "dist"})
	require.NoError(t, err)
	require.Equal(t, 2, outs["files"])
}

func TestDownloadArtifact_BadID(t *testing.T) {
	ec, _, _ := newEC(t)
	dn, _ := Lookup("helios/download-artifact@v1")

	_, err := dn.Run(ec, map[string]any{"id": "not-a-number/dist"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "run id portion")
}

func TestDownloadArtifact_NotFound(t *testing.T) {
	ec, _, _ := newEC(t)
	dn, _ := Lookup("helios/download-artifact@v1")
	_, err := dn.Run(ec, map[string]any{"id": "999999/never-existed"})
	require.Error(t, err)
}
