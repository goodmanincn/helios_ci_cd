// Package logstream — Reader: 从 Redis Stream 读日志, 支持范围读 (历史) + 阻塞跟随 (实时).
//
// 用途:
//   - T1.5.4 HTTP 历史拉取: ReadRange(runID, fromID, count) → []Entry
//   - T1.5.2 WebSocket 实时: Follow(runID, fromID) → 推送 chan, 一直 XREAD BLOCK 到 ctx 取消
//
// fromID 语义:
//   - ""    : 从头 ("0-0")
//   - "$"   : 从最新 (Follow 专用; ReadRange 别用)
//   - "x-y" : redis 原生 stream id, 用上次返回的最后一个 + 0 做 next
//
// XREAD BLOCK 选 1s 短周期, 便于 ctx 取消时尽快收手 (而不是等到 daemon-side 阻塞超时).
package logstream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Entry 单条日志.
type Entry struct {
	ID     string    // redis stream entry id, 形如 "1718012345678-0"
	Ts     time.Time // RFC3339Nano 反序列化; 解析失败时为 zero
	Stream string    // stdout|stderr|system
	Line   string    // 原始行 (不含末尾 \n)
}

// Reader 读端.
type Reader struct {
	rdb *redis.Client
}

// NewReader 构造.
func NewReader(rdb *redis.Client) *Reader {
	return &Reader{rdb: rdb}
}

// ReadRange 一次性读 [from, +∞) 最多 count 条.
// from="" 时从头读. count<=0 取 100.
// 返回的最后一条 ID 可作为下次 from (再加 1 用 NextID).
func (r *Reader) ReadRange(ctx context.Context, runID int64, from string, count int64) ([]Entry, error) {
	if count <= 0 {
		count = 100
	}
	min := from
	if min == "" {
		min = "-"
	}
	res, err := r.rdb.XRangeN(ctx, StreamKey(runID), min, "+", count).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("XRANGE: %w", err)
	}
	out := make([]Entry, 0, len(res))
	for _, m := range res {
		out = append(out, msgToEntry(m))
	}
	return out, nil
}

// Follow 长连接: 从 from 起阻塞读, 每批通过 out 推出. ctx 取消 → 关 out, 返回.
// from="$" 表示只读新到的; from="0-0" / "" 表示从头.
// 出错(非 ctx 取消)会写一条 stream=system 的本地 Entry 然后结束, 让消费方看到原因.
func (r *Reader) Follow(ctx context.Context, runID int64, from string) <-chan Entry {
	out := make(chan Entry, 64)
	if from == "" {
		from = "0-0"
	}
	go func() {
		defer close(out)
		next := from
		key := StreamKey(runID)
		for {
			if ctx.Err() != nil {
				return
			}
			streams, err := r.rdb.XRead(ctx, &redis.XReadArgs{
				Streams: []string{key, next},
				Count:   200,
				Block:   1 * time.Second, // 短 block 便于尽快感知 ctx 取消
			}).Result()
			if err != nil {
				if errors.Is(err, redis.Nil) {
					continue // block 超时, 没新数据, 重读
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				// 其他错: 报一条 system 再退出, 不无限重试
				select {
				case out <- Entry{Ts: time.Now().UTC(), Stream: StreamSystem,
					Line: "[logstream] follow error: " + err.Error()}:
				case <-ctx.Done():
				}
				return
			}
			for _, s := range streams {
				for _, m := range s.Messages {
					e := msgToEntry(m)
					select {
					case out <- e:
					case <-ctx.Done():
						return
					}
					next = e.ID
				}
			}
		}
	}()
	return out
}

func msgToEntry(m redis.XMessage) Entry {
	e := Entry{ID: m.ID}
	if v, ok := m.Values[FieldStream].(string); ok {
		e.Stream = v
	}
	if v, ok := m.Values[FieldLine].(string); ok {
		e.Line = v
	}
	if v, ok := m.Values[FieldTs].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			e.Ts = t
		}
	}
	return e
}
