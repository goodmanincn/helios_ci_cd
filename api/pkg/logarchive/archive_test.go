package logarchive_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/helios-cicd/helios/api/pkg/logarchive"
	"github.com/helios-cicd/helios/api/pkg/logstream"
)

func requireRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("HELIOS_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6380"
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		t.Skipf("redis %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func freshRunID(t *testing.T) int64 {
	t.Helper()
	return time.Now().UnixNano() % 1_000_000_000
}

// Happy path: 写 redis → ArchiveAndDrop → 文件存在, gzip 可解, redis 清空.
func TestService_ArchiveAndDrop(t *testing.T) {
	rdb := requireRedis(t)
	w := logstream.NewWriter(rdb, logstream.Config{})
	r := logstream.NewReader(rdb)
	rid := freshRunID(t)
	defer func() { _ = rdb.Del(context.Background(), logstream.StreamKey(rid)).Err() }()

	ctx := context.Background()
	w.AppendSystem(ctx, rid, "build started")
	for i := 0; i < 50; i++ {
		w.Append(ctx, rid, logstream.StreamStdout, "line-stdout-x")
		w.Append(ctx, rid, logstream.StreamStderr, "line-stderr-y")
	}
	w.AppendSystem(ctx, rid, "[SUCCESS] done")

	if got, _ := w.Len(ctx, rid); got != 102 {
		t.Fatalf("pre-Len=%d want 102", got)
	}

	root := t.TempDir()
	svc := &logarchive.Service{
		Reader:   r,
		Writer:   w,
		Backend:  logarchive.NewLocalFS(root),
		PageSize: 16, // 故意小, 走多页 ReadRange
	}

	stat, err := svc.ArchiveAndDrop(ctx, rid)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if stat.Entries != 102 {
		t.Errorf("Entries=%d want 102", stat.Entries)
	}
	if stat.Key != logarchive.ArchiveKey(rid) {
		t.Errorf("Key=%s want %s", stat.Key, logarchive.ArchiveKey(rid))
	}
	if !strings.HasPrefix(stat.Backend, "localfs:") {
		t.Errorf("Backend=%s", stat.Backend)
	}

	// redis 已清
	if got, _ := w.Len(ctx, rid); got != 0 {
		t.Errorf("post-Len=%d want 0", got)
	}

	// 文件存在
	dst := filepath.Join(root, stat.Key)
	if st, err := os.Stat(dst); err != nil || st.Size() == 0 {
		t.Fatalf("archive file: stat=%+v err=%v", st, err)
	}

	// ReadAll 还原
	entries, err := svc.ReadAll(ctx, rid)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != 102 {
		t.Fatalf("ReadAll=%d want 102", len(entries))
	}
	if entries[0].Stream != logstream.StreamSystem || entries[0].Line != "build started" {
		t.Errorf("entries[0]={%s,%s}", entries[0].Stream, entries[0].Line)
	}
	if entries[1].Stream != logstream.StreamStdout || entries[1].Line != "line-stdout-x" {
		t.Errorf("entries[1]={%s,%s}", entries[1].Stream, entries[1].Line)
	}
	last := entries[len(entries)-1]
	if last.Stream != logstream.StreamSystem || last.Line != "[SUCCESS] done" {
		t.Errorf("last={%s,%s}", last.Stream, last.Line)
	}
	// Ts 解析
	for i, e := range entries {
		if e.Ts.IsZero() {
			t.Errorf("entry[%d] Ts zero", i)
			break
		}
	}
}

// 空 stream → Skipped, 不报错, 不写文件.
func TestService_ArchiveAndDrop_EmptyStream(t *testing.T) {
	rdb := requireRedis(t)
	w := logstream.NewWriter(rdb, logstream.Config{})
	r := logstream.NewReader(rdb)
	rid := freshRunID(t)

	root := t.TempDir()
	svc := &logarchive.Service{Reader: r, Writer: w, Backend: logarchive.NewLocalFS(root)}
	stat, err := svc.ArchiveAndDrop(context.Background(), rid)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if !stat.Skipped {
		t.Errorf("Skipped=false want true")
	}
	dst := filepath.Join(root, logarchive.ArchiveKey(rid))
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("archive should not exist, got err=%v", err)
	}
}

// 幂等: 已删 redis 后, 再调一次 → Skipped.
func TestService_ArchiveAndDrop_Idempotent(t *testing.T) {
	rdb := requireRedis(t)
	w := logstream.NewWriter(rdb, logstream.Config{})
	r := logstream.NewReader(rdb)
	rid := freshRunID(t)
	defer func() { _ = rdb.Del(context.Background(), logstream.StreamKey(rid)).Err() }()

	ctx := context.Background()
	w.Append(ctx, rid, logstream.StreamStdout, "x")

	root := t.TempDir()
	svc := &logarchive.Service{Reader: r, Writer: w, Backend: logarchive.NewLocalFS(root)}

	if _, err := svc.ArchiveAndDrop(ctx, rid); err != nil {
		t.Fatalf("first: %v", err)
	}
	stat, err := svc.ArchiveAndDrop(ctx, rid)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !stat.Skipped {
		t.Errorf("second call Skipped=false want true")
	}
}

// LocalFS: Put 是 atomic, .tmp 不会残留.
func TestLocalFS_PutAtomic(t *testing.T) {
	root := t.TempDir()
	l := logarchive.NewLocalFS(root)
	key := "a/b/c.txt"
	if err := l.Put(context.Background(), key, strings.NewReader("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, key))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != "hello" {
		t.Errorf("got %q", b)
	}
	// 不应该有 .tmp
	if _, err := os.Stat(filepath.Join(root, key+".tmp")); !os.IsNotExist(err) {
		t.Errorf(".tmp leaked")
	}
}

// Open 不存在 → fs.ErrNotExist.
func TestLocalFS_OpenMissing(t *testing.T) {
	root := t.TempDir()
	l := logarchive.NewLocalFS(root)
	_, err := l.Open(context.Background(), "missing.txt")
	if !os.IsNotExist(err) {
		t.Errorf("err=%v want IsNotExist", err)
	}
}
