// Package logs HTTP/SSE 日志端点 — T1.5.2 / T1.5.4.
//
// 提供两个端点 (path 不冲突, 走 Accept 头分发):
//   GET /api/v1/runs/:id/logs            — 范围拉取 (T1.5.4) 历史 / 当前快照, 返回 JSON
//   GET /api/v1/runs/:id/logs/stream     — Server-Sent Events 实时流 (T1.5.2)
//
// 为什么选 SSE 而不是 WebSocket:
//   - 日志只需单向 server→client
//   - 浏览器原生 EventSource, 自动重连
//   - 用标准库 http (no new dependency)
//   - 走 HTTP/1.1, 普通 LB / CDN / nginx 直通
//
// 鉴权: M1 dev 阶段简化, ?token=<jwt> 可选, 有就 Parse 校验 (M2 接 ACL).
//
// Stream 协议 (SSE):
//   event: log
//   id: <redis-stream-id>
//   data: {"ts":"...","stream":"stdout|stderr|system","line":"..."}
//
//   event: ping       — 每 30s 一个空 ping 防止代理切断
//   event: end        — run 终态后由前端自行重试 (M2 通过 audit 通知)
package logs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/helios-cicd/helios/api/pkg/jwt"
	"github.com/helios-cicd/helios/api/pkg/logarchive"
	"github.com/helios-cicd/helios/api/pkg/logstream"
)

// Handler 日志接入入口, 持有 Reader + 可选 jwt issuer + 可选 archive (T1.5.4 fallback).
type Handler struct {
	Reader  *logstream.Reader
	Archive *logarchive.Service // 可空: redis 没数据时不查归档
	Issuer  *jwt.Issuer         // 可空: 不校 token
}

// New 构造.
func New(r *logstream.Reader, iss *jwt.Issuer, opts ...Option) *Handler {
	h := &Handler{Reader: r, Issuer: iss}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Option 配置项.
type Option func(*Handler)

// WithArchive 配置归档 fallback (T1.5.4).
func WithArchive(a *logarchive.Service) Option {
	return func(h *Handler) { h.Archive = a }
}

// Register 挂到 /api/v1 group 下.
func (h *Handler) Register(r *gin.RouterGroup) {
	g := r.Group("/runs/:id/logs")
	g.GET("", h.history)        // T1.5.4
	g.GET("/stream", h.stream)  // T1.5.2
}

// history GET /api/v1/runs/:id/logs?from=<id>&count=<n>&source=auto|redis|archive
// 返回 {entries:[...], next:"<id>", has_more:bool, source:"redis|archive"}.
// source=auto (默认): 先查 redis, 0 条则查 archive.
func (h *Handler) history(c *gin.Context) {
	runID, ok := parseRunID(c)
	if !ok {
		return
	}
	if !h.authOK(c) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	from := c.Query("from")
	count, _ := strconv.ParseInt(c.DefaultQuery("count", "200"), 10, 64)
	if count <= 0 || count > 1000 {
		count = 200
	}
	source := c.DefaultQuery("source", "auto")

	// 1) Redis 优先
	if source == "auto" || source == "redis" {
		entries, err := h.Reader.ReadRange(c.Request.Context(), runID, from, count+1)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if len(entries) > 0 || source == "redis" {
			respondHistory(c, entries, count, "redis")
			return
		}
	}

	// 2) Archive fallback (run 终态后 redis 已删)
	if h.Archive == nil {
		respondHistory(c, nil, count, "redis")
		return
	}
	all, err := h.Archive.ReadAll(c.Request.Context(), runID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			// 既无 redis 也无归档 → 空集 (200)
			respondHistory(c, nil, count, "archive")
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// archive 是 ndjson 全集, 按 from / count 切
	entries := sliceEntriesFrom(all, from, count+1)
	respondHistory(c, entries, count, "archive")
}

// sliceEntriesFrom 从 archive 全集里取 from 之后的最多 limit 条.
// from 为空 / "0-0" → 从头; 否则跳过 <= from 的, 取之后的 limit 条.
// from 前缀 "(" 表示 exclusive (与 redis XRANGE 一致), 不带前缀也按 exclusive 处理 (与 Reader.ReadRange "(<id>" 等价).
func sliceEntriesFrom(all []logstream.Entry, from string, limit int64) []logstream.Entry {
	if len(all) == 0 {
		return nil
	}
	if from == "" || from == "0-0" {
		if int64(len(all)) > limit {
			return all[:limit]
		}
		return all
	}
	target := from
	if len(target) > 0 && target[0] == '(' {
		target = target[1:]
	}
	start := 0
	for i, e := range all {
		if e.ID == target {
			start = i + 1
			break
		}
	}
	if start >= len(all) {
		return nil
	}
	end := start + int(limit)
	if end > len(all) {
		end = len(all)
	}
	return all[start:end]
}

func respondHistory(c *gin.Context, entries []logstream.Entry, count int64, source string) {
	hasMore := int64(len(entries)) > count
	if hasMore {
		entries = entries[:count]
	}
	resp := gin.H{
		"entries":  toDTOs(entries),
		"has_more": hasMore,
		"source":   source,
	}
	if len(entries) > 0 {
		resp["next"] = entries[len(entries)-1].ID
	}
	c.JSON(http.StatusOK, resp)
}

// stream GET /api/v1/runs/:id/logs/stream?from=<id>
// SSE: 持续推送, ctx 取消时关流.
func (h *Handler) stream(c *gin.Context) {
	runID, ok := parseRunID(c)
	if !ok {
		return
	}
	if !h.authOK(c) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	from := c.Query("from")
	if from == "" {
		from = "0-0" // 从头读 (常用: 页面打开就回放)
	}

	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx 不要缓冲
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher == nil {
		// gin Writer 实现 Flusher, 兜底
		_, _ = w.WriteString("event: error\ndata: streaming not supported\n\n")
		return
	}
	// 主动 flush 一下 header 让客户端立刻拿到 200
	flusher.Flush()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	ch := h.Reader.Follow(ctx, runID, from)
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if _, err := w.WriteString("event: ping\ndata: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case e, ok := <-ch:
			if !ok {
				// follower 退出 — 可能是 ctx 取消或后端错; 给客户端一个 end 信号
				_, _ = w.WriteString("event: end\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			d, _ := json.Marshal(entryDTO{
				Ts:     e.Ts.UTC().Format(time.RFC3339Nano),
				Stream: e.Stream,
				Line:   e.Line,
			})
			if _, err := fmt.Fprintf(w, "event: log\nid: %s\ndata: %s\n\n", e.ID, d); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// authOK 简化鉴权: 无 token → 放行 (M1 dev), 有 token → Parse 必须成功.
// query string 优先 (EventSource 不能加 Header), Authorization Header 兜底.
func (h *Handler) authOK(c *gin.Context) bool {
	tok := c.Query("token")
	if tok == "" {
		auth := c.GetHeader("Authorization")
		if len(auth) > 7 && auth[:7] == "Bearer " {
			tok = auth[7:]
		}
	}
	if tok == "" {
		// M1 dev: 不强制
		return true
	}
	if h.Issuer == nil {
		return true
	}
	_, err := h.Issuer.Parse(tok)
	return err == nil
}

func parseRunID(c *gin.Context) (int64, bool) {
	s := c.Param("id")
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid run id"})
		return 0, false
	}
	return id, true
}

// entryDTO 对外 JSON 形状.
type entryDTO struct {
	ID     string `json:"id,omitempty"`
	Ts     string `json:"ts"`
	Stream string `json:"stream"`
	Line   string `json:"line"`
}

func toDTOs(es []logstream.Entry) []entryDTO {
	out := make([]entryDTO, len(es))
	for i, e := range es {
		out[i] = entryDTO{
			ID:     e.ID,
			Ts:     e.Ts.UTC().Format(time.RFC3339Nano),
			Stream: e.Stream,
			Line:   e.Line,
		}
	}
	return out
}
