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
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/helios-cicd/helios/api/pkg/jwt"
	"github.com/helios-cicd/helios/api/pkg/logstream"
)

// Handler 日志接入入口, 持有 Reader + 可选 jwt issuer.
type Handler struct {
	Reader *logstream.Reader
	Issuer *jwt.Issuer // 可空: 不校 token
}

// New 构造.
func New(r *logstream.Reader, iss *jwt.Issuer) *Handler {
	return &Handler{Reader: r, Issuer: iss}
}

// Register 挂到 /api/v1 group 下.
func (h *Handler) Register(r *gin.RouterGroup) {
	g := r.Group("/runs/:id/logs")
	g.GET("", h.history)        // T1.5.4
	g.GET("/stream", h.stream)  // T1.5.2
}

// history GET /api/v1/runs/:id/logs?from=<id>&count=<n>
// 返回 {entries:[...], next:"<id>", has_more:bool}.
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
	entries, err := h.Reader.ReadRange(c.Request.Context(), runID, from, count+1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	hasMore := int64(len(entries)) > count
	if hasMore {
		entries = entries[:count]
	}
	resp := gin.H{
		"entries":  toDTOs(entries),
		"has_more": hasMore,
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
