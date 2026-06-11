package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func setup(c *Checker) *gin.Engine {
	r := gin.New()
	r.GET("/healthz", c.Liveness())
	r.GET("/readyz", c.Readiness())
	r.GET("/version", c.Version())
	return r
}

func do(t *testing.T, r *gin.Engine, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

func TestLivenessAlwaysOK(t *testing.T) {
	c := New()
	// 故意注册一个挂的 check, liveness 不应该看
	c.Register("db", func(context.Context) error { return errors.New("boom") })
	r := setup(c)

	code, body := do(t, r, "/healthz")
	if code != http.StatusOK {
		t.Fatalf("want 200 got %d", code)
	}
	if body["status"] != "alive" {
		t.Fatalf("want alive got %v", body["status"])
	}
}

func TestReadinessAllOK(t *testing.T) {
	c := New()
	c.Register("db", func(context.Context) error { return nil })
	c.Register("redis", func(context.Context) error { return nil })
	r := setup(c)

	code, body := do(t, r, "/readyz")
	if code != http.StatusOK {
		t.Fatalf("want 200 got %d body=%v", code, body)
	}
	if body["status"] != "ready" {
		t.Fatalf("want ready got %v", body["status"])
	}
	checks, _ := body["checks"].([]any)
	if len(checks) != 2 {
		t.Fatalf("want 2 checks got %d", len(checks))
	}
}

func TestReadinessAnyFail(t *testing.T) {
	c := New()
	c.Register("db", func(context.Context) error { return nil })
	c.Register("redis", func(context.Context) error { return errors.New("conn refused") })
	r := setup(c)

	code, body := do(t, r, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d", code)
	}
	if body["status"] != "not_ready" {
		t.Fatalf("want not_ready got %v", body["status"])
	}
}

func TestReadinessTimeout(t *testing.T) {
	c := New().WithCheckTimeout(50 * time.Millisecond)
	c.Register("slow", func(ctx context.Context) error {
		select {
		case <-time.After(500 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	r := setup(c)

	code, body := do(t, r, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d", code)
	}
	checks, _ := body["checks"].([]any)
	if len(checks) != 1 {
		t.Fatalf("want 1 check got %d", len(checks))
	}
	first := checks[0].(map[string]any)
	if first["status"] != "fail" {
		t.Fatalf("want fail status got %v", first["status"])
	}
}

func TestVersion(t *testing.T) {
	c := New().WithVersion("1.2.3", "2026-06-11T00:00:00Z", "abc123")
	r := setup(c)
	code, body := do(t, r, "/version")
	if code != http.StatusOK {
		t.Fatalf("want 200 got %d", code)
	}
	if body["version"] != "1.2.3" || body["git_commit"] != "abc123" {
		t.Fatalf("version body mismatch: %v", body)
	}
}

func TestVersionDefaults(t *testing.T) {
	c := New() // 没注入
	r := setup(c)
	_, body := do(t, r, "/version")
	if body["version"] != "dev" || body["git_commit"] != "unknown" {
		t.Fatalf("want defaults got %v", body)
	}
}
