// run_cancel_retry_test.go — RunHandler cancel / retry (T1.6.4) 集成测试
//
// 注意: cancel 走 runstate.Machine, 它用独立 *sql.DB 不能看见 GORM tx 内的 fixture,
// 因此本文件不复用 withRunTx (那走 BEGIN+ROLLBACK), 而是直接在主连接落 fixture +
// 在 t.Cleanup 手工清理 (倒序删: stages -> runs -> versions -> pipelines -> projects -> orgs)。
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// fakeEnq 满足 handler.Enqueuer 接口, 记录入队 payload。
type fakeEnq struct {
	calls []*tasks.GitCheckoutPayload
	err   error
}

func (f *fakeEnq) EnqueueGitCheckout(_ context.Context, p *tasks.GitCheckoutPayload) (string, error) {
	f.calls = append(f.calls, p)
	if f.err != nil {
		return "", f.err
	}
	return "fake-task-id", nil
}

// --- helpers ---

// newCommittedFixture 在主 DB (非事务) 落一套 fixture, 返回 runID + cleanup 注册到 t.Cleanup。
func newCommittedFixture(t *testing.T, gdb *gorm.DB, status string) runFixture {
	t.Helper()
	fx := seedRun(t, gdb, status, false)
	t.Cleanup(func() {
		// 倒序删 (run 包含 retry 新建的可能, 按 pipeline_id 清; approval 表跟 run on-delete cascade)
		_ = gdb.Exec("DELETE FROM approvals WHERE request_id IN (SELECT id FROM approval_requests WHERE run_id IN (SELECT id FROM runs WHERE pipeline_id=?))", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM approval_requests WHERE run_id IN (SELECT id FROM runs WHERE pipeline_id=?)", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM audit_logs WHERE resource_type='run' AND resource_id IN (SELECT id FROM runs WHERE pipeline_id=?)", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM stages WHERE run_id IN (SELECT id FROM runs WHERE pipeline_id=?)", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM runs WHERE pipeline_id=?", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM pipeline_versions WHERE pipeline_id=?", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM pipelines WHERE id=?", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM projects WHERE id=?", fx.ProjectID).Error
		_ = gdb.Exec("DELETE FROM organizations WHERE id=?", fx.OrgID).Error
	})
	return fx
}

func newRunControlRouter(t *testing.T, gdb *gorm.DB, enq Enqueuer) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	sqlDB, err := gdb.DB()
	require.NoError(t, err)
	machine := runstate.New(sqlDB)
	r := gin.New()
	NewRunHandler(gdb).WithRunControl(machine, enq).Register(r.Group("/api/v1"))
	return r
}

// --- cancel ---

func TestRunHandler_Cancel_OK(t *testing.T) {
	gdb := openRunTestDB(t)
	fx := newCommittedFixture(t, gdb, "running")

	srv := httptest.NewServer(newRunControlRouter(t, gdb, &fakeEnq{}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/runs/"+itoa64(fx.RunID)+"/cancel", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "canceled", body["status"])

	// DB 状态应该真的变了
	var got model.Run
	require.NoError(t, gdb.First(&got, fx.RunID).Error)
	require.Equal(t, "canceled", got.Status)
	require.NotNil(t, got.FinishedAt)
}

func TestRunHandler_Cancel_AlreadyTerminal_409(t *testing.T) {
	gdb := openRunTestDB(t)
	fx := newCommittedFixture(t, gdb, "success")

	srv := httptest.NewServer(newRunControlRouter(t, gdb, &fakeEnq{}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/runs/"+itoa64(fx.RunID)+"/cancel", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Contains(t, strings.ToLower(body["error"].(string)), "terminal")
	require.Equal(t, "success", body["status"])
}

func TestRunHandler_Cancel_NotFound(t *testing.T) {
	gdb := openRunTestDB(t)
	srv := httptest.NewServer(newRunControlRouter(t, gdb, &fakeEnq{}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/runs/999999999/cancel", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestRunHandler_Cancel_BadID(t *testing.T) {
	gdb := openRunTestDB(t)
	srv := httptest.NewServer(newRunControlRouter(t, gdb, &fakeEnq{}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/runs/abc/cancel", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestRunHandler_Cancel_NoMachine_503(t *testing.T) {
	gdb := openRunTestDB(t)
	fx := newCommittedFixture(t, gdb, "running")
	// 不挂 machine / enq
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewRunHandler(gdb).Register(r.Group("/api/v1"))
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/runs/"+itoa64(fx.RunID)+"/cancel", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

// --- retry ---

func TestRunHandler_Retry_OK(t *testing.T) {
	gdb := openRunTestDB(t)
	fx := newCommittedFixture(t, gdb, "failed")

	enq := &fakeEnq{}
	srv := httptest.NewServer(newRunControlRouter(t, gdb, enq))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/runs/"+itoa64(fx.RunID)+"/retry", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "pending", body["status"])
	require.Equal(t, float64(fx.RunID), body["origin_run_id"])
	newRunID := int64(body["id"].(float64))
	require.NotEqual(t, fx.RunID, newRunID)

	// number 应该是 max+1=2
	require.Equal(t, float64(2), body["number"])

	// 入队被调用一次, payload 沿用原 branch/commit
	require.Len(t, enq.calls, 1)
	require.Equal(t, newRunID, enq.calls[0].RunID)
	require.Equal(t, fx.ProjectID, enq.calls[0].ProjectID)
	require.Equal(t, "main", enq.calls[0].Branch)
	require.Equal(t, "abcdef1234", enq.calls[0].CommitSHA)

	// DB 真有这条新 run, trigger_type=retry, status=pending
	var nr model.Run
	require.NoError(t, gdb.First(&nr, newRunID).Error)
	require.Equal(t, "pending", nr.Status)
	require.Equal(t, "retry", nr.TriggerType)
	require.Equal(t, 2, nr.Number)
	require.Equal(t, fx.PipelineID, nr.PipelineID)
}

func TestRunHandler_Retry_StillRunning_409(t *testing.T) {
	gdb := openRunTestDB(t)
	fx := newCommittedFixture(t, gdb, "running")

	enq := &fakeEnq{}
	srv := httptest.NewServer(newRunControlRouter(t, gdb, enq))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/runs/"+itoa64(fx.RunID)+"/retry", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Len(t, enq.calls, 0, "in-flight run 不应该入队")
}

func TestRunHandler_Retry_NotFound(t *testing.T) {
	gdb := openRunTestDB(t)
	enq := &fakeEnq{}
	srv := httptest.NewServer(newRunControlRouter(t, gdb, enq))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/runs/999999999/retry", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestRunHandler_Retry_NoEnqueuer_503(t *testing.T) {
	gdb := openRunTestDB(t)
	fx := newCommittedFixture(t, gdb, "failed")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewRunHandler(gdb).Register(r.Group("/api/v1"))
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/runs/"+itoa64(fx.RunID)+"/retry", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}
