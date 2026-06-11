// Package metrics — Prometheus 指标定义 + Gin 采集中间件 + handler。
//
// 设计:
//   - Registry 单例:进程级 default registry,所有 collector 在 init 时注册一次。
//   - HTTP 指标:请求数 / 请求时延 / 当前并发,labels = method/path_template/status。
//   - 业务指标:run 状态变更计数 / run 时长 / 队列长度 / 容量,留 hook 给业务侧 SetXxx。
//   - 路径模板:用 gin 的 FullPath() 避免高基数 (`/runs/:id` 而不是具体 ID)。
//     FullPath 为空(404 等无匹配)归到 `unmatched`。
//
// 业务指标暴露为函数,业务方按 spec 表的事件触发 (worker / handler 都能用)。
package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry 进程级 prometheus registry。
// 用独立 registry 避免 default registry 被其他依赖污染。
var Registry = prometheus.NewRegistry()

// ===== HTTP 指标 =====

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "helios",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "HTTP 请求总数, by method/path/status",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "helios",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP 请求时延 (秒)",
			// 适合 web 接口的桶:5ms ~ 30s
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		},
		[]string{"method", "path", "status"},
	)

	httpRequestsInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "helios",
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "当前正在处理的 HTTP 请求数",
		},
	)
)

// ===== 业务指标 =====

var (
	// RunsTotal — 按状态终结统计 (succeeded/failed/canceled/timeout)
	RunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "helios",
			Subsystem: "run",
			Name:      "total",
			Help:      "Run 终态计数",
		},
		[]string{"status"},
	)

	// RunDurationSeconds — 完整 run 时长
	RunDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "helios",
			Subsystem: "run",
			Name:      "duration_seconds",
			Help:      "Run 完整执行时长 (秒)",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 14), // 1s ~ 16384s ≈ 4.5h
		},
		[]string{"status"},
	)

	// QueueDepth — Asynq 队列待办任务数 (worker 定期上报)
	QueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "helios",
			Subsystem: "queue",
			Name:      "depth",
			Help:      "队列当前待处理任务数",
		},
		[]string{"queue"},
	)

	// RunnerCapacity — 当前活跃 runner 数
	RunnerCapacity = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "helios",
			Subsystem: "runner",
			Name:      "capacity",
			Help:      "Runner 容量 / 已用",
		},
		[]string{"runner_type", "state"}, // state=idle|busy
	)
)

func init() {
	// 进程级 collector: Go runtime + process
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		httpRequestsTotal,
		httpRequestDurationSeconds,
		httpRequestsInFlight,
		RunsTotal,
		RunDurationSeconds,
		QueueDepth,
		RunnerCapacity,
	)
}

// Handler 返回 /metrics gin.HandlerFunc。
func Handler() gin.HandlerFunc {
	h := promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		Registry:          Registry,
		EnableOpenMetrics: true,
	})
	return func(c *gin.Context) { h.ServeHTTP(c.Writer, c.Request) }
}

// GinMiddleware 采集 HTTP 请求指标。
// 路径用 c.FullPath() 取路由模板; 未匹配的路由归到 "unmatched" 避免高基数。
func GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// /metrics 自己不要计入
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}
		start := time.Now()
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		c.Next()

		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method
		dur := time.Since(start).Seconds()

		httpRequestsTotal.WithLabelValues(method, path, status).Inc()
		httpRequestDurationSeconds.WithLabelValues(method, path, status).Observe(dur)
	}
}

// ObserveRun — 业务侧 hook, run 终态时调一次。
func ObserveRun(status string, durationSeconds float64) {
	RunsTotal.WithLabelValues(status).Inc()
	if durationSeconds > 0 {
		RunDurationSeconds.WithLabelValues(status).Observe(durationSeconds)
	}
}
