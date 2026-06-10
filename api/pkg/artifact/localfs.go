// Package artifact — localfs.go: 本地文件系统 Storage 实现。
//
// 与 logarchive.LocalFS 长得一样但独立, 因为:
//   - artifacts/ 和 logs/ 通常配不同的 root (运维要分卷管理)
//   - 后续接 S3 时两边可能独立换 backend, 不强耦
package artifact

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// LocalFS 把 artifact 写到本地目录, key 作为相对路径。
type LocalFS struct {
	Root string // 根目录, 必填
}

func NewLocalFS(root string) *LocalFS { return &LocalFS{Root: root} }

func (l *LocalFS) Name() string { return "localfs:" + l.Root }

// Put 走临时文件 + rename 保证原子。
func (l *LocalFS) Put(ctx context.Context, key string, r io.Reader) error {
	dst := filepath.Join(l.Root, key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func (l *LocalFS) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(l.Root, key))
}
