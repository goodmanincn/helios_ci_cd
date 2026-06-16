// pipeline_trigger_test.go — POST /pipelines/:id/trigger 手动触发试运行
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/middleware"
	"github.com/helios-cicd/helios/api/internal/model"
	"gorm.io/datatypes"
	"github.com/helios-cicd/helios/api/pkg/queue"
	"github.com/helios-cicd/helios/api/pkg/tasks"
	heliosjwt "github.com/helios-cicd/helios/api/pkg/jwt"
)

// fakeTriggerEnq 满足 queue.Enqueuer, 只记录 EnqueueRunOrchestrate 调用.
type fakeTriggerEnq struct {
	calls []*tasks.RunOrchestratePayload
}

func (f *fakeTriggerEnq) EnqueueGitCheckout(_ context.Context, p *tasks.GitCheckoutPayload) (string, error) {
	return "fake-gc-" + itoa64(p.RunID), nil
}
func (f *fakeTriggerEnq) EnqueueWebhookRegister(_ context.Context, p *tasks.WebhookRegisterPayload) (string, error) {
	return "fake-wr", nil
}
func (f *fakeTriggerEnq) EnqueueRunBuild(_ context.Context, p *tasks.RunBuildPayload) (string, error) {
	return "fake-rb", nil
}
func (f *fakeTriggerEnq) EnqueueApprovalTimeout(_ context.Context, p *tasks.ApprovalTimeoutPayload, _ time.Duration) (string, error) {
	return "fake-at", nil
}
func (f *fakeTriggerEnq) EnqueueRunOrchestrate(_ context.Context, p *tasks.RunOrchestratePayload) (string, error) {
	f.calls = append(f.calls, p)
	return "fake-ro-" + itoa64(p.RunID), nil
}
func (f *fakeTriggerEnq) EnqueueStageExecute(_ context.Context, p *tasks.StageExecutePayload) (string, error) {
	return "fake-se", nil
}
func (f *fakeTriggerEnq) Close() error { return nil }

var _ queue.Enqueuer = (*fakeTriggerEnq)(nil)

func newTriggerRouter(t *testing.T, tx *gorm.DB, orgID int64, enq *fakeTriggerEnq) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(fakeAuthWithOrg(1, orgID))
	NewPipelineHandler(tx).WithQueue(enq).Register(g)
	return r
}

// seedPipelineWithVersion 在 tx 里建 pipeline + version + 更新 current_version_id.
func seedPipelineWithVersion(t *testing.T, tx *gorm.DB, projectID int64) (*model.Pipeline, *model.PipelineVersion) {
	t.Helper()
	p := &model.Pipeline{ProjectID: projectID, Name: "trigger-test-" + randSuffix(), Enabled: true}
	require.NoError(t, tx.Create(p).Error)
	pv := &model.PipelineVersion{
		PipelineID: p.ID,
		Version:    1,
		Spec:       datatypes.JSON([]byte(`{}`)),
		SpecRaw:    `version: "1"` + "\nname: t\nstages:\n  - id: s\n    steps:\n      - run: echo hi\n",
	}
	require.NoError(t, tx.Create(pv).Error)
	require.NoError(t, tx.Model(p).Update("current_version_id", pv.ID).Error)
	return p, pv
}

func TestPipelineHandler_Trigger_OK(t *testing.T) {
	withRunTx(t, func(tx *gorm.DB) {
		orgID, _ := seedOrgUser(t, tx)
		proj := &model.Project{OrgID: orgID, Name: "tp-" + randSuffix(), Slug: "tp-" + randSuffix(), RepoType: "github"}
		require.NoError(t, tx.Create(proj).Error)
		p, _ := seedPipelineWithVersion(t, tx, proj.ID)

		enq := &fakeTriggerEnq{}
		srv := httptest.NewServer(newTriggerRouter(t, tx, orgID, enq))
		defer srv.Close()

		resp, err := http.Post(srv.URL+"/api/v1/pipelines/"+itoa64(p.ID)+"/trigger", "application/json", bytes.NewReader(nil))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		var got map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
		runID, _ := got["run_id"].(float64)
		require.Greater(t, runID, float64(0))

		require.Len(t, enq.calls, 1)
		require.Equal(t, int64(runID), enq.calls[0].RunID)
		require.Equal(t, proj.ID, enq.calls[0].ProjectID)
	})
}

func TestPipelineHandler_Trigger_NotFound(t *testing.T) {
	withRunTx(t, func(tx *gorm.DB) {
		orgID, _ := seedOrgUser(t, tx)

		enq := &fakeTriggerEnq{}
		srv := httptest.NewServer(newTriggerRouter(t, tx, orgID, enq))
		defer srv.Close()

		resp, err := http.Post(srv.URL+"/api/v1/pipelines/999999/trigger", "application/json", nil)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestPipelineHandler_Trigger_NoVersion(t *testing.T) {
	withRunTx(t, func(tx *gorm.DB) {
		orgID, _ := seedOrgUser(t, tx)
		proj := &model.Project{OrgID: orgID, Name: "nv-" + randSuffix(), Slug: "nv-" + randSuffix(), RepoType: "github"}
		require.NoError(t, tx.Create(proj).Error)
		p := &model.Pipeline{ProjectID: proj.ID, Name: "no-ver-" + randSuffix(), Enabled: true}
		require.NoError(t, tx.Create(p).Error)

		enq := &fakeTriggerEnq{}
		srv := httptest.NewServer(newTriggerRouter(t, tx, orgID, enq))
		defer srv.Close()

		resp, err := http.Post(srv.URL+"/api/v1/pipelines/"+itoa64(p.ID)+"/trigger", "application/json", nil)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusConflict, resp.StatusCode)
	})
}

func TestPipelineHandler_Trigger_WrongOrg(t *testing.T) {
	gdb := openRunTestDB(t)
	tx := gdb.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { _ = tx.Rollback() })
	orgID, uid := seedOrgUser(t, tx)
	proj := &model.Project{OrgID: orgID, Name: "wo-" + randSuffix(), Slug: "wo-" + randSuffix(), RepoType: "github"}
	require.NoError(t, tx.Create(proj).Error)
	p, _ := seedPipelineWithVersion(t, tx, proj.ID)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(func(c *gin.Context) {
		c.Set(middleware.CtxClaimsKey, &heliosjwt.Claims{UserID: uid, OrgIDs: []int64{orgID + 99999}})
		c.Next()
	})
	NewPipelineHandler(tx).WithQueue(&fakeTriggerEnq{}).Register(g)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/pipelines/"+itoa64(p.ID)+"/trigger", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
