// Package artifact — 制品 (build outputs) 上传/下载 (M2 E2.4)。
//
// 设计:
//   - Storage 接口 + LocalFS 实现 (M2 dev; minio-go 被 spec 显式拒绝, 用 LocalFS;
//     生产可加 S3 backend, 接口已经准备好)
//   - 打包格式: tar.gz (Pack 把 paths 列表 + 可选 glob 打成 stream, Unpack 解到目标目录)
//   - Key 约定: artifacts/{run_id}/{name}.tar.gz (跟 logarchive 同套, 方便后续走同一 storage root)
//   - Manifest: 上传成功后写到 Storage (artifacts/{run_id}/{name}.json), 含原始 size / 文件数
//     便于 UI 列表展示 + DB 表 (artifacts) 一份索引数据
//
// 不做的事 (留 future):
//   - 内容寻址 / 去重
//   - 加密 (走 secrets 层另算)
//   - 大文件流式校验 (M3 加 checksum)
package artifact

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Storage 后端接口 (与 logarchive 类比, 但独立, 因为 key 命名空间不同)。
type Storage interface {
	// Name 返回后端标识 (e.g. "localfs:/tmp/helios/artifacts"), 便于日志。
	Name() string
	// Put 把 reader 内容写到 key, 失败必须保证 key 不存在 (原子)。
	Put(ctx context.Context, key string, r io.Reader) error
	// Open 打开 key 读取; 不存在返 fs.ErrNotExist 兼容错误。
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

// Manifest 制品元数据 (JSON 写到 artifacts/<run>/<name>.json)。
type Manifest struct {
	RunID     int64     `json:"run_id"`
	Name      string    `json:"name"`
	Files     int       `json:"files"`     // 包内文件个数
	BytesRaw  int64     `json:"bytes_raw"` // 解压后总大小
	BytesGz   int64     `json:"bytes_gz"`  // tar.gz 文件大小
	CreatedAt time.Time `json:"created_at"`
}

// ArtifactKey 制品文件 key (.tar.gz). ManifestKey 元数据 key (.json).
func ArtifactKey(runID int64, name string) string {
	return fmt.Sprintf("artifacts/%d/%s.tar.gz", runID, name)
}

func ManifestKey(runID int64, name string) string {
	return fmt.Sprintf("artifacts/%d/%s.json", runID, name)
}

// Pack 把 sourceDir 下匹配 paths (glob) 的文件打成 tar.gz, 写入 Storage。
//
// paths 元素含义:
//   - 普通路径 (e.g. "dist/")           → 递归吃整个目录
//   - 单文件     (e.g. "app/bin/api")   → 加入该文件
//   - glob       (e.g. "*.txt")         → 走 filepath.Glob, 命中加入
//
// 返回 Manifest (含统计) + 已经 Put 的 keys。
func Pack(ctx context.Context, s Storage, runID int64, name, sourceDir string, paths []string) (*Manifest, error) {
	if s == nil {
		return nil, fmt.Errorf("artifact: nil storage")
	}
	if name == "" {
		return nil, fmt.Errorf("artifact: empty name")
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("artifact: at least one path required")
	}

	// 收集所有具体文件 (绝对路径) + tar 里的相对路径
	var entries []packEntry
	for _, p := range paths {
		// 绝对路径化, 相对 sourceDir
		full := p
		if !filepath.IsAbs(full) {
			full = filepath.Join(sourceDir, p)
		}

		// glob 优先 (路径中含 *? 等才走 glob)
		if strings.ContainsAny(p, "*?[") {
			matches, err := filepath.Glob(full)
			if err != nil {
				return nil, fmt.Errorf("glob %q: %w", p, err)
			}
			for _, m := range matches {
				es, err := walk(m, sourceDir)
				if err != nil {
					return nil, err
				}
				entries = append(entries, es...)
			}
			continue
		}

		es, err := walk(full, sourceDir)
		if err != nil {
			return nil, fmt.Errorf("walk %q: %w", p, err)
		}
		entries = append(entries, es...)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("artifact: no files matched paths %v under %q", paths, sourceDir)
	}

	// 写到临时文件再上传 (size 统计需要全部读完, 用 file 比 buffer 省内存)
	tmp, err := os.CreateTemp("", "helios-artifact-*.tar.gz")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	gw := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gw)

	var rawTotal int64
	for _, e := range entries {
		info, err := os.Stat(e.abs)
		if err != nil {
			_ = tw.Close()
			_ = gw.Close()
			_ = tmp.Close()
			return nil, fmt.Errorf("stat %q: %w", e.abs, err)
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil, err
		}
		hdr.Name = e.rel
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("tar header %q: %w", e.rel, err)
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(e.abs)
			if err != nil {
				return nil, err
			}
			n, err := io.Copy(tw, f)
			_ = f.Close()
			if err != nil {
				return nil, fmt.Errorf("tar body %q: %w", e.rel, err)
			}
			rawTotal += n
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		return nil, err
	}
	gzSize, _ := tmp.Seek(0, io.SeekEnd)
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	// 上传 tar.gz + manifest
	key := ArtifactKey(runID, name)
	if err := s.Put(ctx, key, tmp); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("storage put %q: %w", key, err)
	}
	_ = tmp.Close()

	mf := &Manifest{
		RunID:     runID,
		Name:      name,
		Files:     len(entries),
		BytesRaw:  rawTotal,
		BytesGz:   gzSize,
		CreatedAt: time.Now().UTC(),
	}
	mb, _ := json.Marshal(mf)
	if err := s.Put(ctx, ManifestKey(runID, name), bytesReader(mb)); err != nil {
		// manifest 写失败不致命 (tar.gz 已落), 但回报错误让上层决定
		return mf, fmt.Errorf("storage put manifest: %w", err)
	}
	return mf, nil
}

// Unpack 把 storage 中 runID+name 的 tar.gz 解到 destDir。返回写入的文件数。
//
// 安全: 拒收 "../" 路径 (tar slip 攻击防御)。
func Unpack(ctx context.Context, s Storage, runID int64, name, destDir string) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("artifact: nil storage")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, err
	}
	rc, err := s.Open(ctx, ArtifactKey(runID, name))
	if err != nil {
		return 0, fmt.Errorf("open artifact: %w", err)
	}
	defer rc.Close()

	gr, err := gzip.NewReader(rc)
	if err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	count := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}
		// 防 path traversal
		if strings.Contains(hdr.Name, "..") || strings.HasPrefix(hdr.Name, "/") {
			return count, fmt.Errorf("unsafe path in archive: %q", hdr.Name)
		}
		dst := filepath.Join(destDir, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, os.FileMode(hdr.Mode)); err != nil {
				return count, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return count, err
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return count, err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return count, err
			}
			_ = f.Close()
			count++
		}
	}
	return count, nil
}

// LoadManifest 拉某个制品的 manifest (UI/CLI 列表用).
func LoadManifest(ctx context.Context, s Storage, runID int64, name string) (*Manifest, error) {
	rc, err := s.Open(ctx, ManifestKey(runID, name))
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var m Manifest
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ---- helpers ----

// walk 把单个 abs path 展开成 tar entries:
//   - 文件: 直接一条
//   - 目录: 递归所有文件 (按 sourceDir 相对路径作为 tar 内 name)
func walk(absPath, sourceDir string) ([]packEntry, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		rel, _ := filepath.Rel(sourceDir, absPath)
		if rel == "" || strings.HasPrefix(rel, "..") {
			// 在 sourceDir 之外 (绝对路径), 退化用 basename 防 slip
			rel = filepath.Base(absPath)
		}
		return []packEntry{{abs: absPath, rel: filepath.ToSlash(rel)}}, nil
	}
	var out []packEntry
	err = filepath.Walk(absPath, func(p string, fi os.FileInfo, ferr error) error {
		if ferr != nil {
			return ferr
		}
		if fi.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(sourceDir, p)
		if rel == "" || strings.HasPrefix(rel, "..") {
			rel = filepath.Base(p)
		}
		out = append(out, packEntry{abs: p, rel: filepath.ToSlash(rel)})
		return nil
	})
	return out, err
}

// packEntry 在 Pack 内部用 (绝对路径 + tar 内相对名).
type packEntry struct{ abs, rel string }

// bytesReader 不引 bytes 包的廉价 ReadCloser-less reader.
func bytesReader(b []byte) io.Reader { return &readOnly{b: b} }

type readOnly struct {
	b   []byte
	off int
}

func (r *readOnly) Read(p []byte) (int, error) {
	if r.off >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.off:])
	r.off += n
	return n, nil
}
