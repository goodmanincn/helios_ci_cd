// run_test.go — RunHandler 集成测试 (T1.6.1)
//
// 依赖 PG, 跟 model_test 一样的范式: 用 HELIOS_TEST_DB_DSN / HELIOS_DB_DSN,
// 在事务里建 fixture 数据 → 测完回滚。
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helios-cicd/helios/api/internal/db"
	"github.com/helios-cicd/helios/api/internal/model"
)

func openRunTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HELIOS_TEST_DB_DSN")
	if dsn == "" {
		dsn = os.Getenv("HELIOS_DB_DSN")
	}
	if dsn == "" {
		t.Skip("HELIOS_DB_DSN / HELIOS_TEST_DB_DSN 未设置, 跳过 RunHandler 集成测试")
	}
	require.NoError(t, db.Migrate(dsn), "确保 schema 是最新的")
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return gdb
}

// runFixture 在 tx 里建一套: org/user/project/pipeline/version/run(+stage+steps)
type runFixture struct {
	OrgID      int64
	ProjectID  int64
	PipelineID int64
	VersionID  int64
	RunID      int64
	StageID    int64
	Step1ID    int64
	Step2ID    int64
}

func seedRun(t *testing.T, tx *gorm.DB, status string, withStages bool) runFixture {
	t.Helper()
	var fx runFixture

	// org
	org := model.Organization{Slug: "tst-" + randSuffix(), Name: "Test Org"}
	require.NoError(t, tx.Create(&org).Error)
	fx.OrgID = org.ID

	// project
	proj := model.Project{
		OrgID:    org.ID,
		Name:     "demo",
		Slug:     "demo-" + randSuffix(),
		RepoURL:  "/tmp/dummy-bare",
		RepoType: "github",
		Config:   datatypes.JSON([]byte(`{"build_command":"echo hi"}`)),
	}
	require.NoError(t, tx.Create(&proj).Error)
	fx.ProjectID = proj.ID

	// pipeline
	pl := model.Pipeline{ProjectID: proj.ID, Name: "default", Enabled: true}
	require.NoError(t, tx.Create(&pl).Error)
	fx.PipelineID = pl.ID

	// pipeline_version
	ver := model.PipelineVersion{
		PipelineID: pl.ID,
		Version:    1,
		Spec:       datatypes.JSON([]byte(`{}`)),
		SpecRaw:    "{}",
	}
	require.NoError(t, tx.Create(&ver).Error)
	fx.VersionID = ver.ID

	// run
	now := time.Now().UTC()
	fin := now.Add(2 * time.Second)
	run := model.Run{
		PipelineID:  pl.ID,
		VersionID:   ver.ID,
		Number:      1,
		Status:      status,
		Branch:      "main",
		CommitSHA:   "abcdef1234",
		Message:     "fixture run",
		TriggerType: "webhook",
		StartedAt:   &now,
		FinishedAt:  &fin,
	}
	require.NoError(t, tx.Create(&run).Error)
	fx.RunID = run.ID

	if withStages {
		st := model.Stage{
			RunID:      run.ID,
			StageID:    "build",
			Name:       "build",
			Status:     "success",
			StartedAt:  &now,
			FinishedAt: &fin,
		}
		require.NoError(t, tx.Create(&st).Error)
		fx.StageID = st.ID

		idx1, idx2 := 0, 1
		ec := 0
		s1 := model.Step{
			StageRecordID: st.ID, StepIndex: &idx1, Name: "checkout",
			Status: "success", ExitCode: &ec,
			StartedAt: &now, FinishedAt: &fin,
		}
		s2 := model.Step{
			StageRecordID: st.ID, StepIndex: &idx2, Name: "build",
			Status: "success", ExitCode: &ec,
			LogObject: "runs/1/logs.ndjson.gz", LogSize: 1024,
			StartedAt: &now, FinishedAt: &fin,
		}
		require.NoError(t, tx.Create(&s1).Error)
		require.NoError(t, tx.Create(&s2).Error)
		fx.Step1ID = s1.ID
		fx.Step2ID = s2.ID
	}
	return fx
}

// randSuffix 8 位时间戳后缀, 避免 unique 冲突。
func randSuffix() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	now := time.Now().UnixNano()
	out := make([]byte, 8)
	for i := range out {
		out[i] = charset[now%int64(len(charset))]
		now /= int64(len(charset))
	}
	return string(out)
}

func newRunRouter(db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewRunHandler(db).Register(r.Group("/api/v1"))
	return r
}

// 用 tx-scoped DB 跑 handler — Gin 拿到的 db 走 tx, 数据隔离。
func withRunTx(t *testing.T, fn func(tx *gorm.DB)) {
	t.Helper()
	gdb := openRunTestDB(t)
	tx := gdb.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { _ = tx.Rollback() })
	fn(tx)
}

func TestRunHandler_Detail_BadID(t *testing.T) {
	withRunTx(t, func(tx *gorm.DB) {
		srv := httptest.NewServer(newRunRouter(tx))
		defer srv.Close()
		resp, err := http.Get(srv.URL + "/api/v1/runs/abc")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestRunHandler_Detail_NotFound(t *testing.T) {
	withRunTx(t, func(tx *gorm.DB) {
		srv := httptest.NewServer(newRunRouter(tx))
		defer srv.Close()
		resp, err := http.Get(srv.URL + "/api/v1/runs/999999999")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestRunHandler_Detail_ReturnsTree(t *testing.T) {
	withRunTx(t, func(tx *gorm.DB) {
		fx := seedRun(t, tx, "success", true)
		srv := httptest.NewServer(newRunRouter(tx))
		defer srv.Close()
		resp, err := http.Get(srv.URL + "/api/v1/runs/" + itoa64(fx.RunID))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var got runDetailDTO
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))

		require.Equal(t, fx.RunID, got.ID)
		require.Equal(t, "success", got.Status)
		require.Equal(t, "main", got.Branch)
		require.Equal(t, fx.PipelineID, got.PipelineID)
		require.NotNil(t, got.Project)
		require.Equal(t, fx.ProjectID, got.Project.ID)
		require.True(t, got.DurationMs > 0, "DurationMs should be computed from started/finished")

		require.Len(t, got.Stages, 1)
		require.Equal(t, "build", got.Stages[0].StageID)
		require.Len(t, got.Stages[0].Steps, 2)
		// 按 step_index 排
		require.Equal(t, "checkout", got.Stages[0].Steps[0].Name)
		require.Equal(t, "build", got.Stages[0].Steps[1].Name)
		require.Equal(t, "runs/1/logs.ndjson.gz", got.Stages[0].Steps[1].LogObject)
	})
}

func TestRunHandler_Detail_NoStages_EmptyArray(t *testing.T) {
	withRunTx(t, func(tx *gorm.DB) {
		fx := seedRun(t, tx, "pending", false)
		srv := httptest.NewServer(newRunRouter(tx))
		defer srv.Close()
		resp, err := http.Get(srv.URL + "/api/v1/runs/" + itoa64(fx.RunID))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var got runDetailDTO
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
		require.NotNil(t, got.Stages)
		require.Len(t, got.Stages, 0, "stages must be [] not null")
	})
}

func TestRunHandler_List_ByProject(t *testing.T) {
	withRunTx(t, func(tx *gorm.DB) {
		fx1 := seedRun(t, tx, "success", false)
		// 第二个 run 同 pipeline (number+1)
		now := time.Now().UTC()
		r2 := model.Run{
			PipelineID: fx1.PipelineID, VersionID: fx1.VersionID,
			Number: 2, Status: "running", Branch: "main",
			CommitSHA: "deadbeef", TriggerType: "webhook", StartedAt: &now,
		}
		require.NoError(t, tx.Create(&r2).Error)

		srv := httptest.NewServer(newRunRouter(tx))
		defer srv.Close()
		resp, err := http.Get(srv.URL + "/api/v1/runs?project_id=" + itoa64(fx1.ProjectID))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var got struct {
			Items []runListItemDTO `json:"items"`
			Limit int              `json:"limit"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
		require.Len(t, got.Items, 2)
		// id desc
		require.Equal(t, r2.ID, got.Items[0].ID)
		require.Equal(t, fx1.RunID, got.Items[1].ID)
		require.Equal(t, "running", got.Items[0].Status)
		require.NotNil(t, got.Items[0].Project)
		require.Equal(t, "demo", got.Items[0].Project.Name)
	})
}

func TestRunHandler_List_BadProjectID(t *testing.T) {
	withRunTx(t, func(tx *gorm.DB) {
		srv := httptest.NewServer(newRunRouter(tx))
		defer srv.Close()
		resp, err := http.Get(srv.URL + "/api/v1/runs?project_id=abc")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

// itoa64 避免 import strconv 散落
func itoa64(i int64) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = digits[i%10]
		i /= 10
	}
	return string(buf[pos:])
}
