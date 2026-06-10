// Package logstream 把 run 日志按行写入 Redis Stream, 供前端实时订阅 (E1.5).
//
// 设计:
//   - 一个 run 一个 stream, key = logs:run:{run_id}
//   - 每条 entry 字段: ts (RFC3339Nano), stream (stdout|stderr|system), line (raw)
//   - MAXLEN ~ (默认 10000) 防无限增长; 用 ~ 让 redis 做近似裁剪 (性能更好)
//   - Append 失败只 log 不 return error: 日志写丢比 build 退出更可接受
//   - Close/Archive 在 T1.5.3 做, 这里只管写
//
// 字段名约束:
//   FieldTs / FieldStream / FieldLine 给消费者 (WS / 归档) 用同一份常量
package logstream

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// 字段名 / key 前缀.
const (
	KeyPrefix   = "logs:run:"
	FieldTs     = "ts"
	FieldStream = "stream"
	FieldLine   = "line"

	StreamStdout = "stdout"
	StreamStderr = "stderr"
	StreamSystem = "system" // 由 handler 自己写的元信息行
)

// StreamKey 返回某 run 的 stream key.
func StreamKey(runID int64) string {
	return fmt.Sprintf("%s%d", KeyPrefix, runID)
}

// Writer 写入端.
type Writer struct {
	rdb    *redis.Client
	maxLen int64 // ~MAXLEN, 默认 10000

	// 内部统计 (atomic-ish via mu)
	mu       sync.Mutex
	appended int64
}

// Config 写入配置.
type Config struct {
	MaxLen int64 // <=0 用默认 10000
}

// NewWriter 构造.
func NewWriter(rdb *redis.Client, cfg Config) *Writer {
	if cfg.MaxLen <= 0 {
		cfg.MaxLen = 10000
	}
	return &Writer{rdb: rdb, maxLen: cfg.MaxLen}
}

// Append 写一行到 run 的 stream. 写失败只记日志 (best-effort), 不返回 err.
// 调用方(executor LogSink)应在不阻塞 build 的前提下调用.
func (w *Writer) Append(ctx context.Context, runID int64, stream, line string) {
	if w == nil || w.rdb == nil {
		return
	}
	if stream == "" {
		stream = StreamStdout
	}
	args := &redis.XAddArgs{
		Stream: StreamKey(runID),
		MaxLen: w.maxLen,
		Approx: true, // ~MAXLEN
		Values: map[string]any{
			FieldTs:     time.Now().UTC().Format(time.RFC3339Nano),
			FieldStream: stream,
			FieldLine:   line,
		},
	}
	if _, err := w.rdb.XAdd(ctx, args).Result(); err != nil {
		log.Printf("logstream: XADD run=%d failed: %v", runID, err)
		return
	}
	w.mu.Lock()
	w.appended++
	w.mu.Unlock()
}

// AppendSystem 写一行 system 级别 (run started / build runtime= / archive done).
// 跟普通行同 stream 同 key, 只是 stream=system, 前端可着色区分.
func (w *Writer) AppendSystem(ctx context.Context, runID int64, line string) {
	w.Append(ctx, runID, StreamSystem, line)
}

// Stats 返回累计写入条数 (诊断用).
func (w *Writer) Stats() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.appended
}

// Len 返回当前 stream 长度 (XLEN).
func (w *Writer) Len(ctx context.Context, runID int64) (int64, error) {
	return w.rdb.XLen(ctx, StreamKey(runID)).Result()
}

// Drop 删除 stream key (归档后调用, T1.5.3).
func (w *Writer) Drop(ctx context.Context, runID int64) error {
	return w.rdb.Del(ctx, StreamKey(runID)).Err()
}
