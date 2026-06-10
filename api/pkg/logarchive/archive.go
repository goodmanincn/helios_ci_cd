// Package logarchive 把 run 的 redis log stream 归档到持久存储, 然后释放 redis.
//
// M1 阶段: 单一 LocalFS 实现 (写 workspace/<run_id>/logs.ndjson.gz)
// M2 阶段: 加 MinIO/S3 实现, Archiver 接口已经预留扩展.
//
// 调用时机: build handler MarkSuccess/MarkFailed/MarkCanceled 后 best-effort 调用 ArchiveAndDrop;
// 失败只 log, 不影响 run 状态.
//
// 归档格式 (ndjson + gzip):
//   {"id":"...","ts":"...","stream":"stdout","line":"..."}
//   {"id":"...","ts":"...","stream":"stderr","line":"..."}
//   ...
//
// 这样后续 T1.5.4 历史拉取可以 line-by-line 顺序读, 跟 Redis Stream 语义一致.
package logarchive

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/helios-cicd/helios/api/pkg/logstream"
)

// Archiver 归档接口. backing store 可以是本地文件 / S3 / MinIO.
type Archiver interface {
	// Put 写入 archive 对象, key 形如 "runs/{run_id}/logs.ndjson.gz".
	// 实现需要原子写 (avoid partial reads).
	Put(ctx context.Context, key string, r io.Reader) error
	// Open 打开 archive 对象用于读. 找不到返回 fs.ErrNotExist 兼容错.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Name 描述当前 backing store (供 audit / log).
	Name() string
}

// Service 归档服务: Reader 读 redis, Archiver 写 backing store, Writer.Drop 删 redis.
type Service struct {
	Reader   *logstream.Reader
	Writer   *logstream.Writer
	Backend  Archiver
	PageSize int64 // ReadRange 分页大小, <=0 用 500
}

// ArchiveKey run 的归档对象 key.
func ArchiveKey(runID int64) string {
	return fmt.Sprintf("runs/%d/logs.ndjson.gz", runID)
}

// ArchiveAndDrop 全量读 redis → gzip ndjson 上传 → 删 redis.
// 已经归档过 (redis stream 不存在 / 已为空) 返回 nil (idempotent).
// 任何步骤错都返回, 调用方可以 best-effort.
func (s *Service) ArchiveAndDrop(ctx context.Context, runID int64) (ArchiveStat, error) {
	if s == nil || s.Reader == nil || s.Writer == nil || s.Backend == nil {
		return ArchiveStat{}, errors.New("logarchive: service not fully initialized")
	}
	page := s.PageSize
	if page <= 0 {
		page = 500
	}

	// 1) 先查 stream 是否有数据
	n, err := s.Writer.Len(ctx, runID)
	if err != nil {
		return ArchiveStat{}, fmt.Errorf("len redis stream: %w", err)
	}
	if n == 0 {
		// 已经归档过 / 从未写过 — idempotent
		return ArchiveStat{RunID: runID, Skipped: true}, nil
	}

	// 2) 准备 gzip pipe (边读边压边上传, 不在内存里堆完整 payload)
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		gz := gzip.NewWriter(pw)
		enc := json.NewEncoder(gz)
		// 分页 ReadRange 避免单次 XRANGE 拖大
		from := ""
		var written int64
		for {
			entries, err := s.Reader.ReadRange(ctx, runID, from, page+1)
			if err != nil {
				_ = pw.CloseWithError(err)
				errCh <- err
				return
			}
			more := int64(len(entries)) > page
			if more {
				entries = entries[:page]
			}
			for _, e := range entries {
				if err := enc.Encode(archiveEntry{
					ID:     e.ID,
					Ts:     e.Ts.UTC().Format(time.RFC3339Nano),
					Stream: e.Stream,
					Line:   e.Line,
				}); err != nil {
					_ = pw.CloseWithError(err)
					errCh <- err
					return
				}
				written++
			}
			if !more {
				break
			}
			// 下一页从最后一条 ID 之后开始 (XRANGE 半开右侧)
			// 用 "(" 前缀表示 exclusive — go-redis 透传给 redis (XRANGE 6.2+ 支持)
			from = "(" + entries[len(entries)-1].ID
		}
		if err := gz.Close(); err != nil {
			_ = pw.CloseWithError(err)
			errCh <- err
			return
		}
		_ = pw.Close()
		errCh <- nil
		log.Printf("logarchive: run=%d serialized %d entries", runID, written)
	}()

	// 3) 上传
	key := ArchiveKey(runID)
	if putErr := s.Backend.Put(ctx, key, pr); putErr != nil {
		_ = pr.CloseWithError(putErr)
		return ArchiveStat{RunID: runID, Key: key}, fmt.Errorf("backend put: %w", putErr)
	}
	if encErr := <-errCh; encErr != nil {
		return ArchiveStat{RunID: runID, Key: key}, fmt.Errorf("encode/read: %w", encErr)
	}

	// 4) 删 redis
	if err := s.Writer.Drop(ctx, runID); err != nil {
		// 上传已成功, redis 删除失败 → 下次再 Archive 时 Len>0 还会再写一遍 (覆盖), 不致命
		return ArchiveStat{RunID: runID, Key: key, Entries: n}, fmt.Errorf("drop redis: %w", err)
	}
	return ArchiveStat{RunID: runID, Key: key, Entries: n, Backend: s.Backend.Name()}, nil
}

// ReadAll 从归档读全部条目 (T1.5.4 历史 HTTP 用).
// fromOffset / count 在调用方用 slice 截取 (M1 简单实现, M2 加流式).
func (s *Service) ReadAll(ctx context.Context, runID int64) ([]logstream.Entry, error) {
	rc, err := s.Backend.Open(ctx, ArchiveKey(runID))
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	gz, err := gzip.NewReader(rc)
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer gz.Close()
	dec := json.NewDecoder(gz)
	var out []logstream.Entry
	for {
		var ae archiveEntry
		if err := dec.Decode(&ae); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return out, err
		}
		ts, _ := time.Parse(time.RFC3339Nano, ae.Ts)
		out = append(out, logstream.Entry{
			ID:     ae.ID,
			Ts:     ts,
			Stream: ae.Stream,
			Line:   ae.Line,
		})
	}
	return out, nil
}

// ArchiveStat 归档统计 (供 audit 用).
type ArchiveStat struct {
	RunID   int64  `json:"run_id"`
	Key     string `json:"key,omitempty"`
	Entries int64  `json:"entries"`
	Backend string `json:"backend,omitempty"`
	Skipped bool   `json:"skipped,omitempty"` // stream 空, 没归档动作
}

type archiveEntry struct {
	ID     string `json:"id"`
	Ts     string `json:"ts"`
	Stream string `json:"stream"`
	Line   string `json:"line"`
}

// ===== LocalFS 实现 =====

// LocalFS 把 archive 写到本地目录, key 作为相对路径.
// 路径会创建父目录, 写临时文件再 rename 保证原子.
type LocalFS struct {
	Root string // 根目录, 必填
}

// NewLocalFS 构造.
func NewLocalFS(root string) *LocalFS { return &LocalFS{Root: root} }

func (l *LocalFS) Name() string { return "localfs:" + l.Root }

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
