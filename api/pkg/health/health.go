// Package health — /healthz (liveness) /readyz (readiness) /version 端点。
//
// 设计:
//   - Liveness 永远 200 (进程没死就活), 给 K8s 区分 "重启 vs 不就绪" 用。
//   - Readiness 检查 DB + Redis (注册到 checker 列表), 任一失败返 503。
//   - 检查并发, 单 check 超时 2s, 整体超时由 ctx 控制。
//   - JSON 响应含每项详细结果, 方便 ops 排查; K8s probe 只看状态码。
package health

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// CheckFunc 单个健康检查; 在 deadline 内 ping 资源, 失败返 error。
type CheckFunc func(ctx context.Context) error

// Checker 收集多个 CheckFunc, 提供 ready handler。
type Checker struct {
	mu     sync.RWMutex
	checks map[string]CheckFunc

	// 每个 check 单独的超时, 默认 2s
	checkTimeout time.Duration

	// version 信息 (注入 ldflags), version handler 用
	version   string
	buildTime string
	gitCommit string
}

func New() *Checker {
	return &Checker{
		checks:       make(map[string]CheckFunc),
		checkTimeout: 2 * time.Second,
	}
}

// Register 注册一个名字的 check, 重名覆盖。
func (c *Checker) Register(name string, fn CheckFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks[name] = fn
}

// WithCheckTimeout 自定义单 check 超时。
func (c *Checker) WithCheckTimeout(d time.Duration) *Checker {
	c.checkTimeout = d
	return c
}

// WithVersion 注入版本信息 (给 /version 用)。
func (c *Checker) WithVersion(version, buildTime, gitCommit string) *Checker {
	c.version = version
	c.buildTime = buildTime
	c.gitCommit = gitCommit
	return c
}

// Liveness 返回 /healthz handler — 进程活着就 200。
func (c *Checker) Liveness() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"status":    "alive",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// Readiness 返回 /readyz handler — 检查所有依赖, 全过返 200, 否则 503。
func (c *Checker) Readiness() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		results := c.runAll(ctx.Request.Context())
		body := gin.H{
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"checks":    results,
		}
		for _, r := range results {
			if r["status"] != "ok" {
				body["status"] = "not_ready"
				ctx.JSON(http.StatusServiceUnavailable, body)
				return
			}
		}
		body["status"] = "ready"
		ctx.JSON(http.StatusOK, body)
	}
}

// Version 返回 /version handler — 注入的版本信息。
func (c *Checker) Version() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"version":    nz(c.version, "dev"),
			"build_time": nz(c.buildTime, "unknown"),
			"git_commit": nz(c.gitCommit, "unknown"),
		})
	}
}

// runAll 并发跑所有 check; 返回稳定顺序的结果数组 (按 name)。
func (c *Checker) runAll(ctx context.Context) []map[string]any {
	c.mu.RLock()
	checks := make(map[string]CheckFunc, len(c.checks))
	for k, v := range c.checks {
		checks[k] = v
	}
	c.mu.RUnlock()

	type out struct {
		name   string
		status string
		err    string
		ms     int64
	}
	resCh := make(chan out, len(checks))
	wg := sync.WaitGroup{}
	for name, fn := range checks {
		wg.Add(1)
		go func(n string, f CheckFunc) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, c.checkTimeout)
			defer cancel()
			start := time.Now()
			err := f(cctx)
			r := out{name: n, ms: time.Since(start).Milliseconds()}
			if err != nil {
				r.status = "fail"
				r.err = err.Error()
			} else {
				r.status = "ok"
			}
			resCh <- r
		}(name, fn)
	}
	wg.Wait()
	close(resCh)

	// 按名字排序, 返回稳定输出
	collected := make([]out, 0, len(checks))
	for r := range resCh {
		collected = append(collected, r)
	}
	// 简单插入排序 — 数量很小 (几个 check)
	for i := 1; i < len(collected); i++ {
		for j := i; j > 0 && collected[j-1].name > collected[j].name; j-- {
			collected[j-1], collected[j] = collected[j], collected[j-1]
		}
	}

	results := make([]map[string]any, 0, len(collected))
	for _, r := range collected {
		m := map[string]any{
			"name":        r.name,
			"status":      r.status,
			"duration_ms": r.ms,
		}
		if r.err != "" {
			m["error"] = r.err
		}
		results = append(results, m)
	}
	return results
}

func nz(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
