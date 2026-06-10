// pipeline_test.go — PipelineHandler validate 端点 (T2.1.4)
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newPipelineRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewPipelineHandler().Register(r.Group("/api/v1"))
	return r
}

func TestPipelineHandler_Validate_OK(t *testing.T) {
	r := newPipelineRouter(t)
	srv := httptest.NewServer(r)
	defer srv.Close()

	body := map[string]string{
		"spec_raw": `version: "1"
name: minimal
stages:
  - id: build
    steps:
      - run: "echo hi"
`,
	}
	b, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/api/v1/pipelines/validate", "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, true, got["valid"])
	require.Empty(t, got["errors"])
	require.NotNil(t, got["summary"])
	sum := got["summary"].(map[string]any)
	require.Equal(t, float64(1), sum["stage_count"])
}

func TestPipelineHandler_Validate_HasErrors_Still200(t *testing.T) {
	r := newPipelineRouter(t)
	srv := httptest.NewServer(r)
	defer srv.Close()

	body := map[string]string{
		"spec_raw": `version: "1"
stages:
  - id: a
    needs: [ghost]
    steps:
      - name: nope
`,
	}
	b, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/api/v1/pipelines/validate", "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "errors return 200, not 4xx (前端拿列表展示)")

	var got map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, false, got["valid"])
	errs, ok := got["errors"].([]any)
	require.True(t, ok)
	require.GreaterOrEqual(t, len(errs), 2, "expect name + needs + step errors")
}

func TestPipelineHandler_Validate_EmptyBody_400(t *testing.T) {
	r := newPipelineRouter(t)
	srv := httptest.NewServer(r)
	defer srv.Close()

	b, _ := json.Marshal(map[string]string{})
	resp, err := http.Post(srv.URL+"/api/v1/pipelines/validate", "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPipelineHandler_Validate_TextYAML(t *testing.T) {
	r := newPipelineRouter(t)
	srv := httptest.NewServer(r)
	defer srv.Close()

	yaml := `version: "1"
name: cli-style
stages:
  - id: build
    steps:
      - run: "echo cli"
`
	resp, err := http.Post(srv.URL+"/api/v1/pipelines/validate", "text/yaml", strings.NewReader(yaml))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, true, got["valid"])
}
