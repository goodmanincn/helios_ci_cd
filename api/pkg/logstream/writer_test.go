package logstream_test

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/helios-cicd/helios/api/pkg/logstream"
)

// 复用 worker E2E 用的 redis: HELIOS_REDIS_ADDR=127.0.0.1:6380.
// 没设 / ping 不通就 skip, 别污染 CI.
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
		t.Skipf("redis %s unreachable, skip: %v", addr, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// 隔离 run_id (大于真实业务 id, 测后清理).
func freshRunID(t *testing.T) int64 {
	t.Helper()
	// time.Now().UnixMilli + offset, 不与真实 runs 表冲突
	return time.Now().UnixNano() % 1_000_000_000
}

func TestWriter_AppendAndRange(t *testing.T) {
	rdb := requireRedis(t)
	w := logstream.NewWriter(rdb, logstream.Config{MaxLen: 1000})
	r := logstream.NewReader(rdb)

	rid := freshRunID(t)
	defer func() { _ = rdb.Del(context.Background(), logstream.StreamKey(rid)).Err() }()

	ctx := context.Background()
	w.AppendSystem(ctx, rid, "build started")
	w.Append(ctx, rid, logstream.StreamStdout, "hello")
	w.Append(ctx, rid, logstream.StreamStderr, "warn: nothing")
	w.AppendSystem(ctx, rid, "build done")

	if got, _ := w.Len(ctx, rid); got != 4 {
		t.Fatalf("Len=%d want 4", got)
	}
	if got := w.Stats(); got != 4 {
		t.Errorf("Stats=%d want 4", got)
	}

	entries, err := r.ReadRange(ctx, rid, "", 100)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries=%d want 4: %+v", len(entries), entries)
	}
	want := []struct{ stream, line string }{
		{logstream.StreamSystem, "build started"},
		{logstream.StreamStdout, "hello"},
		{logstream.StreamStderr, "warn: nothing"},
		{logstream.StreamSystem, "build done"},
	}
	for i, e := range entries {
		if e.Stream != want[i].stream || e.Line != want[i].line {
			t.Errorf("entry[%d]={%s,%s} want={%s,%s}",
				i, e.Stream, e.Line, want[i].stream, want[i].line)
		}
		if e.Ts.IsZero() {
			t.Errorf("entry[%d] Ts zero", i)
		}
		if e.ID == "" {
			t.Errorf("entry[%d] ID empty", i)
		}
	}

	// 范围读 (跳过第一条): from=第二条 id-1 不行, 用 entries[1].ID 起读 (含此 ID).
	from := entries[1].ID
	part, _ := r.ReadRange(ctx, rid, from, 10)
	if len(part) != 3 {
		t.Errorf("from=%s ReadRange len=%d want 3", from, len(part))
	}
}

// MAXLEN ~ 修剪: 写 500 条, MAXLEN=100, len 应 ≤ ~ 上界.
func TestWriter_MaxLenApprox(t *testing.T) {
	rdb := requireRedis(t)
	w := logstream.NewWriter(rdb, logstream.Config{MaxLen: 100})
	rid := freshRunID(t)
	defer func() { _ = rdb.Del(context.Background(), logstream.StreamKey(rid)).Err() }()

	ctx := context.Background()
	for i := 0; i < 500; i++ {
		w.Append(ctx, rid, logstream.StreamStdout, "line "+strings.Repeat("x", 10))
	}
	got, err := w.Len(ctx, rid)
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	// approx trimming 可能略超, redis 文档 ~ 不保证精确. 给上界 1.5x.
	if got > 150 {
		t.Errorf("Len=%d want ~100 (≤150 lenient)", got)
	}
	if got < 50 {
		t.Errorf("Len=%d trimmed too aggressively", got)
	}
}

// Follow 实时跟随: 启 goroutine 写, 主 goroutine 收, 验证顺序 + ctx 取消.
func TestReader_FollowRealtime(t *testing.T) {
	rdb := requireRedis(t)
	w := logstream.NewWriter(rdb, logstream.Config{})
	r := logstream.NewReader(rdb)
	rid := freshRunID(t)
	defer func() { _ = rdb.Del(context.Background(), logstream.StreamKey(rid)).Err() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 先写 2 条已有, follower 从头读 → 应收到 2 + 后续 3
	w.Append(ctx, rid, logstream.StreamStdout, "old-1")
	w.Append(ctx, rid, logstream.StreamStdout, "old-2")

	ch := r.Follow(ctx, rid, "")

	var got atomic.Int32
	done := make(chan []logstream.Entry, 1)
	go func() {
		entries := make([]logstream.Entry, 0, 5)
		for e := range ch {
			entries = append(entries, e)
			got.Add(1)
			if got.Load() >= 5 {
				cancel()
			}
		}
		done <- entries
	}()

	// 慢慢写 3 条新的
	time.Sleep(200 * time.Millisecond)
	for i := 1; i <= 3; i++ {
		w.Append(context.Background(), rid, logstream.StreamStdout, "new-"+itoa(i))
		time.Sleep(50 * time.Millisecond)
	}

	entries := <-done
	if len(entries) < 5 {
		t.Fatalf("got %d entries want >=5: %+v", len(entries), entries)
	}
	wantLines := []string{"old-1", "old-2", "new-1", "new-2", "new-3"}
	for i, want := range wantLines {
		if entries[i].Line != want {
			t.Errorf("entry[%d].Line=%q want %q", i, entries[i].Line, want)
		}
	}
}

// "$" 起读 = 只看新到的, 老条目不发.
func TestReader_FollowFromDollar(t *testing.T) {
	rdb := requireRedis(t)
	w := logstream.NewWriter(rdb, logstream.Config{})
	r := logstream.NewReader(rdb)
	rid := freshRunID(t)
	defer func() { _ = rdb.Del(context.Background(), logstream.StreamKey(rid)).Err() }()

	ctx := context.Background()
	w.Append(ctx, rid, logstream.StreamStdout, "should-be-skipped")

	// "$" 起读
	fctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := r.Follow(fctx, rid, "$")

	time.Sleep(200 * time.Millisecond)
	w.Append(context.Background(), rid, logstream.StreamStdout, "fresh-only")

	var got []logstream.Entry
	for e := range ch {
		got = append(got, e)
		if len(got) >= 1 {
			cancel()
		}
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries want 1: %+v", len(got), got)
	}
	if got[0].Line != "fresh-only" {
		t.Errorf("got %q want fresh-only", got[0].Line)
	}
}

// Drop 删 key.
func TestWriter_Drop(t *testing.T) {
	rdb := requireRedis(t)
	w := logstream.NewWriter(rdb, logstream.Config{})
	rid := freshRunID(t)
	ctx := context.Background()
	w.Append(ctx, rid, logstream.StreamStdout, "x")
	if got, _ := w.Len(ctx, rid); got != 1 {
		t.Fatalf("len pre-drop=%d", got)
	}
	if err := w.Drop(ctx, rid); err != nil {
		t.Fatalf("Drop: %v", err)
	}
	if got, _ := w.Len(ctx, rid); got != 0 {
		t.Errorf("len post-drop=%d want 0", got)
	}
}

// Append 容错: nil writer / nil rdb 不 panic.
func TestWriter_NilSafe(t *testing.T) {
	var w *logstream.Writer
	w.Append(context.Background(), 1, "stdout", "x") // 不 panic 即可
	w2 := logstream.NewWriter(nil, logstream.Config{})
	w2.Append(context.Background(), 1, "stdout", "x")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
