package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// scrape 起一个含 /metrics 的 gin engine, 返回纯文本输出。
func scrape(t *testing.T, setup func(r *gin.Engine)) string {
	t.Helper()
	r := gin.New()
	r.Use(GinMiddleware())
	r.GET("/metrics", Handler())
	if setup != nil {
		setup(r)
	}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/metrics: want 200 got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	return string(body)
}

func TestMiddlewareRecordsHTTPMetrics(t *testing.T) {
	r := gin.New()
	r.Use(GinMiddleware())
	r.GET("/api/v1/projects/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/metrics", Handler())

	// 1) 业务请求
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/42", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("biz req: want 200 got %d", w.Code)
	}

	// 2) /metrics 输出
	req2 := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	body, _ := io.ReadAll(w2.Body)
	out := string(body)

	// 路径模板替换 :id, status=200, method=GET
	wants := []string{
		`helios_http_requests_total`,
		`/api/v1/projects/:id`,
		`status="200"`,
		`method="GET"`,
		`helios_http_request_duration_seconds_bucket`,
	}
	for _, s := range wants {
		if !strings.Contains(out, s) {
			t.Errorf("metrics output missing %q\n--- output ---\n%s", s, out)
		}
	}
	// /metrics 不计自己
	if strings.Contains(out, `path="/metrics"`) {
		t.Errorf("metrics endpoint should not be self-recorded")
	}
}

func TestObserveRun(t *testing.T) {
	ObserveRun("succeeded", 12.5)
	ObserveRun("succeeded", 7.0)
	ObserveRun("failed", 3.0)

	out := scrape(t, nil)
	if !strings.Contains(out, `helios_run_total{status="succeeded"} 2`) {
		t.Errorf("succeeded count missing\n%s", out)
	}
	if !strings.Contains(out, `helios_run_total{status="failed"} 1`) {
		t.Errorf("failed count missing\n%s", out)
	}
	if !strings.Contains(out, `helios_run_duration_seconds_bucket`) {
		t.Errorf("duration histogram missing")
	}
}

func TestUnmatchedRouteLabel(t *testing.T) {
	r := gin.New()
	r.Use(GinMiddleware())
	r.GET("/metrics", Handler())
	// 故意请求一个没有路由的路径
	req := httptest.NewRequest(http.MethodGet, "/no/such/path", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	body, _ := io.ReadAll(w2.Body)
	if !strings.Contains(string(body), `path="unmatched"`) {
		t.Errorf("unmatched route should bucket to 'unmatched'\n%s", string(body))
	}
}

func TestQueueDepthGauge(t *testing.T) {
	QueueDepth.WithLabelValues("default").Set(7)
	out := scrape(t, nil)
	if !strings.Contains(out, `helios_queue_depth{queue="default"} 7`) {
		t.Errorf("queue depth missing\n%s", out)
	}
}
