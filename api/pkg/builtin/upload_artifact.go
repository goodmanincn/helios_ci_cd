// Package builtin — upload_artifact.go: helios/upload-artifact@v1
//
// 输入:
//   name: 制品名 (必填, 用作 storage key)
//   path: 文件/目录/glob (必填; string 或 []string)
//
// 输出:
//   id:        run_id/name 形式 (与 download-artifact 的 id 对齐)
//   name:      原 name
//   files:     文件数
//   bytes_raw: 原始大小
//   bytes_gz:  压缩后大小
package builtin

import (
	"fmt"

	"github.com/helios-cicd/helios/api/pkg/artifact"
)

type uploadArtifactStep struct{}

func (uploadArtifactStep) Name() string { return "helios/upload-artifact@v1" }

func (uploadArtifactStep) Run(ec *ExecContext, inputs map[string]any) (map[string]any, error) {
	if ec == nil || ec.Storage == nil {
		return nil, fmt.Errorf("upload-artifact: nil ExecContext / Storage")
	}
	name, err := MustString(uploadArtifactStep{}.Name(), inputs, "name")
	if err != nil {
		return nil, err
	}
	paths, err := StringList(inputs, "path")
	if err != nil {
		return nil, fmt.Errorf("upload-artifact: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("upload-artifact: path required (string or list)")
	}

	if ec.Log != nil {
		fmt.Fprintf(ec.Log, "[upload-artifact] name=%s paths=%v from=%s\n",
			name, paths, ec.WorkDir)
	}

	mf, err := artifact.Pack(ec.Ctx, ec.Storage, ec.RunID, name, ec.WorkDir, paths)
	if err != nil {
		return nil, fmt.Errorf("upload-artifact: %w", err)
	}

	if ec.Log != nil {
		fmt.Fprintf(ec.Log, "[upload-artifact] uploaded files=%d bytes_raw=%d bytes_gz=%d backend=%s\n",
			mf.Files, mf.BytesRaw, mf.BytesGz, ec.Storage.Name())
	}

	id := fmt.Sprintf("%d/%s", ec.RunID, name)
	return map[string]any{
		"id":        id,
		"name":      name,
		"files":     mf.Files,
		"bytes_raw": mf.BytesRaw,
		"bytes_gz":  mf.BytesGz,
	}, nil
}

func init() {
	Register(uploadArtifactStep{})
}
