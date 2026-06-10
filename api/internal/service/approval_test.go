// approval_test.go — ApprovalService 集成测试 (T2.6.2)
//
// 走真 PG (HELIOS_TEST_DB_DSN / HELIOS_DB_DSN), 不用 GORM tx 包裹 (因为 runstate.Machine
// 走独立 *sql.DB 看不到), 改用 committed fixture + t.Cleanup 倒序删.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helios-cicd/helios/api/internal/db"
	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/pkg/runstate"
)

func openApprovalTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HELIOS_TEST_DB_DSN")
	if dsn == "" {
		dsn = os.Getenv("HELIOS_DB_DSN")
	}
	if dsn == "" {
		t.Skip("HELIOS_DB_DSN / HELIOS_TEST_DB_DSN 未设置, 跳过 ApprovalService 集成测试")
	}
	require.NoError(t, db.Migrate(dsn))
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return gdb
}

type apFixture struct {
	OrgID      int64
	ProjectID  int64
	PipelineID int64
	VersionID  int64
	RunID      int64
}

// seedApproval 落一套 fixture 并把 run 状态置 approval (模拟 scheduler 命中 approval 节点之后).
func seedApproval(t *testing.T, gdb *gorm.DB, runStatus string) apFixture {
	t.Helper()
	suffix := fmt.Sprintf("ap-%d", time.Now().UnixNano())
	var fx apFixture
	org := model.Organization{Slug: suffix, Name: "ApprovalTestOrg"}
	require.NoError(t, gdb.Create(&org).Error)
	fx.OrgID = org.ID
	proj := model.Project{
		OrgID: org.ID, Name: "ap-proj", Slug: suffix,
		RepoURL: "/tmp/dummy", RepoType: "github", DefaultBranch: "main",
		Config: datatypes.JSON([]byte(`{}`)),
	}
	require.NoError(t, gdb.Create(&proj).Error)
	fx.ProjectID = proj.ID
	pl := model.Pipeline{ProjectID: proj.ID, Name: "default", Enabled: true}
	require.NoError(t, gdb.Create(&pl).Error)
	fx.PipelineID = pl.ID
	ver := model.PipelineVersion{PipelineID: pl.ID, Version: 1, Spec: datatypes.JSON([]byte(`{}`)), SpecRaw: "{}"}
	require.NoError(t, gdb.Create(&ver).Error)
	fx.VersionID = ver.ID
	now := time.Now().UTC()
	run := model.Run{
		PipelineID: pl.ID, VersionID: ver.ID, Number: 1,
		Status: runStatus, Branch: "main", CommitSHA: "deadbeef",
		TriggerType: "webhook", StartedAt: &now,
	}
	require.NoError(t, gdb.Create(&run).Error)
	fx.RunID = run.ID

	t.Cleanup(func() {
		// 倒序删 (approval 走 cascade 但保险起见单独删 audit_logs)
		_ = gdb.Exec("DELETE FROM approvals WHERE request_id IN (SELECT id FROM approval_requests WHERE run_id IN (SELECT id FROM runs WHERE pipeline_id=?))", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM approval_requests WHERE run_id IN (SELECT id FROM runs WHERE pipeline_id=?)", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM audit_logs WHERE (resource_type='run' OR resource_type='approval_request') AND resource_id IN (SELECT id FROM runs WHERE pipeline_id=?)", fx.PipelineID).Error
		// 还有 approval 类 audit 是按 request id 引的, 上面 SQL 没全, 兜底再来一发按 stage_id 模糊
		_ = gdb.Exec("DELETE FROM audit_logs WHERE action LIKE 'approval.%' AND payload->>'request_id' IN (SELECT id::text FROM approval_requests WHERE run_id IN (SELECT id FROM runs WHERE pipeline_id=?))", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM runs WHERE pipeline_id=?", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM pipeline_versions WHERE pipeline_id=?", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM pipelines WHERE id=?", fx.PipelineID).Error
		_ = gdb.Exec("DELETE FROM projects WHERE id=?", fx.ProjectID).Error
		_ = gdb.Exec("DELETE FROM organizations WHERE id=?", fx.OrgID).Error
	})
	return fx
}

// seedRequest 在已落 run 上插一条 pending approval_request, 便于 vote test 直接跑.
func seedRequest(t *testing.T, gdb *gorm.DB, runID int64, stageID, mode string, approvers []string) *model.ApprovalRequest {
	t.Helper()
	now := time.Now().UTC()
	req := &model.ApprovalRequest{
		RunID: runID, StageID: stageID,
		RequiredApprovers: pq.StringArray(approvers),
		Mode:              mode, Status: "pending", OnTimeout: "reject",
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, gdb.Create(req).Error)
	return req
}

func newApprovalSvc(t *testing.T, gdb *gorm.DB) *ApprovalService {
	t.Helper()
	sqlDB, err := gdb.DB()
	require.NoError(t, err)
	return NewApprovalService(gdb, runstate.New(sqlDB))
}

// ---- Create ----

func TestApprovalService_Create_RunningToApproval(t *testing.T) {
	gdb := openApprovalTestDB(t)
	fx := seedApproval(t, gdb, "running")
	svc := newApprovalSvc(t, gdb)
	ctx := context.Background()

	req, err := svc.Create(ctx, CreateInput{
		RunID: fx.RunID, StageID: "manual",
		RequiredApprovers: []string{"alice"}, Mode: "any",
		Timeout: 30 * time.Second,
	})
	require.NoError(t, err)
	require.NotZero(t, req.ID)
	require.Equal(t, "pending", req.Status)
	require.NotNil(t, req.TimeoutAt)

	// run 应该转 approval
	var runStatus string
	require.NoError(t, gdb.Raw("SELECT status FROM runs WHERE id=?", fx.RunID).Scan(&runStatus).Error)
	require.Equal(t, "approval", runStatus)
}

func TestApprovalService_Create_QuorumRejected(t *testing.T) {
	gdb := openApprovalTestDB(t)
	fx := seedApproval(t, gdb, "running")
	svc := newApprovalSvc(t, gdb)

	_, err := svc.Create(context.Background(), CreateInput{
		RunID: fx.RunID, StageID: "x",
		RequiredApprovers: []string{"alice"}, Mode: "quorum",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsupportedMode))
}

// ---- Approve / Reject ----

func TestApprovalService_Approve_AnyMode_RunBackToRunning(t *testing.T) {
	gdb := openApprovalTestDB(t)
	fx := seedApproval(t, gdb, "approval")
	req := seedRequest(t, gdb, fx.RunID, "manual", "any", []string{"alice", "bob"})
	svc := newApprovalSvc(t, gdb)

	res, err := svc.Approve(context.Background(), VoteInput{
		RunID: fx.RunID, StageID: "manual",
		Username: "alice", Comment: "lgtm",
	})
	require.NoError(t, err)
	require.Equal(t, "approved", res.Request.Status)
	require.Equal(t, runstate.StatusRunning, res.NextRunStatus)

	// run 状态推回 running
	var s string
	require.NoError(t, gdb.Raw("SELECT status FROM runs WHERE id=?", fx.RunID).Scan(&s).Error)
	require.Equal(t, "running", s)

	// approvals 1 行
	var cnt int64
	require.NoError(t, gdb.Model(&model.Approval{}).Where("request_id=?", req.ID).Count(&cnt).Error)
	require.Equal(t, int64(1), cnt)
}

func TestApprovalService_Approve_AllMode_PartialPending(t *testing.T) {
	gdb := openApprovalTestDB(t)
	fx := seedApproval(t, gdb, "approval")
	seedRequest(t, gdb, fx.RunID, "manual", "all", []string{"alice", "bob"})
	svc := newApprovalSvc(t, gdb)

	// 第 1 票
	res, err := svc.Approve(context.Background(), VoteInput{
		RunID: fx.RunID, StageID: "manual", Username: "alice",
	})
	require.NoError(t, err)
	require.Equal(t, "pending", res.Request.Status)
	require.Equal(t, "", res.NextRunStatus)
	// run 仍 approval
	var s string
	require.NoError(t, gdb.Raw("SELECT status FROM runs WHERE id=?", fx.RunID).Scan(&s).Error)
	require.Equal(t, "approval", s)

	// 第 2 票
	res2, err := svc.Approve(context.Background(), VoteInput{
		RunID: fx.RunID, StageID: "manual", Username: "bob",
	})
	require.NoError(t, err)
	require.Equal(t, "approved", res2.Request.Status)
	require.Equal(t, runstate.StatusRunning, res2.NextRunStatus)
	require.NoError(t, gdb.Raw("SELECT status FROM runs WHERE id=?", fx.RunID).Scan(&s).Error)
	require.Equal(t, "running", s)
}

func TestApprovalService_Reject_ImmediateFail(t *testing.T) {
	gdb := openApprovalTestDB(t)
	fx := seedApproval(t, gdb, "approval")
	seedRequest(t, gdb, fx.RunID, "manual", "all", []string{"alice", "bob"})
	svc := newApprovalSvc(t, gdb)

	res, err := svc.Reject(context.Background(), VoteInput{
		RunID: fx.RunID, StageID: "manual", Username: "alice", Comment: "no",
	})
	require.NoError(t, err)
	require.Equal(t, "rejected", res.Request.Status)
	require.Equal(t, runstate.StatusFailed, res.NextRunStatus)

	// run 应该是 failed (terminal)
	var s string
	require.NoError(t, gdb.Raw("SELECT status FROM runs WHERE id=?", fx.RunID).Scan(&s).Error)
	require.Equal(t, "failed", s)
}

func TestApprovalService_NotApprover_403(t *testing.T) {
	gdb := openApprovalTestDB(t)
	fx := seedApproval(t, gdb, "approval")
	seedRequest(t, gdb, fx.RunID, "manual", "any", []string{"alice"})
	svc := newApprovalSvc(t, gdb)

	_, err := svc.Approve(context.Background(), VoteInput{
		RunID: fx.RunID, StageID: "manual", Username: "mallory",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNotApprover))
}

func TestApprovalService_Wildcard_AnyoneCanApprove(t *testing.T) {
	gdb := openApprovalTestDB(t)
	fx := seedApproval(t, gdb, "approval")
	seedRequest(t, gdb, fx.RunID, "manual", "any", []string{"*"})
	svc := newApprovalSvc(t, gdb)

	res, err := svc.Approve(context.Background(), VoteInput{
		RunID: fx.RunID, StageID: "manual", Username: "random-user",
	})
	require.NoError(t, err)
	require.Equal(t, "approved", res.Request.Status)
}

func TestApprovalService_DoubleVote_Conflict(t *testing.T) {
	gdb := openApprovalTestDB(t)
	fx := seedApproval(t, gdb, "approval")
	seedRequest(t, gdb, fx.RunID, "manual", "all", []string{"alice", "bob"})
	svc := newApprovalSvc(t, gdb)

	// alice 第 1 票 OK
	_, err := svc.Approve(context.Background(), VoteInput{
		RunID: fx.RunID, StageID: "manual", Username: "alice",
	})
	require.NoError(t, err)
	// alice 再投一次 (即便 mode=all 还在 pending) → unique 触发
	_, err = svc.Approve(context.Background(), VoteInput{
		RunID: fx.RunID, StageID: "manual", Username: "alice",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrAlreadyVoted), "got: %v", err)
}

func TestApprovalService_RunNotInApproval_Conflict(t *testing.T) {
	gdb := openApprovalTestDB(t)
	// run 是 running (没走 Create) → Approve 应当返 ErrRunNotInApproval
	fx := seedApproval(t, gdb, "running")
	// 故意手工插一条 pending request (实际不会出现的状态, 测试错误路径)
	seedRequest(t, gdb, fx.RunID, "manual", "any", []string{"alice"})
	svc := newApprovalSvc(t, gdb)

	_, err := svc.Approve(context.Background(), VoteInput{
		RunID: fx.RunID, StageID: "manual", Username: "alice",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRunNotInApproval))
}

func TestApprovalService_RequestNotFound_404(t *testing.T) {
	gdb := openApprovalTestDB(t)
	fx := seedApproval(t, gdb, "approval")
	svc := newApprovalSvc(t, gdb)

	_, err := svc.Approve(context.Background(), VoteInput{
		RunID: fx.RunID, StageID: "ghost-stage", Username: "alice",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrApprovalNotFound))
}

// ---- ListByRun ----

func TestApprovalService_ListByRun(t *testing.T) {
	gdb := openApprovalTestDB(t)
	fx := seedApproval(t, gdb, "approval")
	req := seedRequest(t, gdb, fx.RunID, "manual", "any", []string{"alice"})
	svc := newApprovalSvc(t, gdb)
	_, err := svc.Approve(context.Background(), VoteInput{
		RunID: fx.RunID, StageID: "manual", Username: "alice", Comment: "ok",
	})
	require.NoError(t, err)

	list, err := svc.ListByRun(context.Background(), fx.RunID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, req.ID, list[0].ID)
	require.Equal(t, "approved", list[0].Status)
	require.Len(t, list[0].Votes, 1)
	require.Equal(t, "alice", list[0].Votes[0].Username)
	require.Equal(t, "ok", list[0].Votes[0].Comment)
}
