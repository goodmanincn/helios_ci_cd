// Package service — ApprovalService: 人工审批 (E2.6).
//
// 设计:
//   - Create: scheduler 命中 approval 节点时调 (T2.6 范围外, E2.4 接 IO 时一并接),
//     落 approval_requests 行, 同时把 run 推进 running→approval, 入 asynq 延时超时任务
//   - Approve/Reject: handler 调用, 校验 caller 在 RequiredApprovers + run 在 approval 状态
//   - mode=any: 任一 approve 即 approved; 任一 reject 即 rejected
//   - mode=all: 全员一票 approve 才 approved; 任一 reject 即 rejected
//   - approve 成功后 run approval→running, reject 成功后 run approval→failed
//
// 事务边界 (重要):
//   - approvals 写入用 GORM tx
//   - runstate.Machine 走独立 *sql.DB (看不到 GORM tx), 必须在 tx Commit 之后调
//   - Machine 转移失败不回滚 tx (approvals 行已落, 前端轮询可见), 只 log 告警
//
// scope guard (M2 简化):
//   - mode=quorum 不支持 (DSL 留字段, Service 返 ErrUnsupportedMode)
//   - approvers 只支持 username; '*' 通配 = 任意登录用户
//   - secrets/role 扩展留 M3
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/pkg/runengine"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// ===== 业务错误 =====

var (
	ErrApprovalNotFound     = errors.New("approval request not found")
	ErrRunNotInApproval     = errors.New("run is not in approval state")
	ErrRequestNotPending    = errors.New("approval request is not pending")
	ErrNotApprover          = errors.New("user is not an approver")
	ErrAlreadyVoted         = errors.New("user has already voted on this request")
	ErrUnsupportedMode      = errors.New("approval mode not supported")
	ErrApprovalDBUnavailable = errors.New("approval service not configured")
)

// ===== DTO =====

// ApprovalSummary 给 handler / 前端展示用. 含历史投票.
type ApprovalSummary struct {
	model.ApprovalRequest
	Votes []model.Approval `json:"approvals"`
}

// CreateInput scheduler 调用 Create 时传入.
type CreateInput struct {
	RunID             int64
	StageID           string
	RequiredApprovers []string
	Mode              string // any / all (quorum 暂不支持)
	OnTimeout         string // reject / approve / pause (空值由 Service 补 reject)
	Timeout           time.Duration // 0 表示永不超时
}

// VoteInput Approve/Reject 入参.
type VoteInput struct {
	RunID    int64
	StageID  string // 路由级别识别
	UserID   *int64 // 可空 (system 调用时为 nil)
	Username string // 必填; 用 'system' 表示系统投票 (on_timeout=approve)
	Comment  string
}

// VoteResult 投票结果. NextRunStatus 是 caller 在 tx Commit 后传给 runstate.Machine 的目标状态.
type VoteResult struct {
	Request       *model.ApprovalRequest
	Vote          *model.Approval
	NextRunStatus string // "" 表示无需推进 run (mode=all 部分通过); 否则 running / failed
}

// ===== Service =====

// TimeoutEnqueuer 窄接口, 让 Service 不强依赖整个 queue.Enqueuer.
// queue.AsynqEnqueuer 自动满足.
type TimeoutEnqueuer interface {
	EnqueueApprovalTimeout(ctx context.Context, p *tasks.ApprovalTimeoutPayload, delay time.Duration) (taskID string, err error)
}

// OrchestrateEnqueuer 审批通过后恢复多 stage 调度。
type OrchestrateEnqueuer interface {
	EnqueueRunOrchestrate(ctx context.Context, p *tasks.RunOrchestratePayload) (taskID string, err error)
}

// ApprovalService 审批业务层. db 必填, machine 可空 (空则 Approve/Reject 跳过 run 推进只落投票).
// timeoutEnq 可空 (空则不发延时任务, 即审批永不超时, 用于单元测试).
type ApprovalService struct {
	db             *gorm.DB
	machine        *runstate.Machine
	timeoutEnq     TimeoutEnqueuer
	orchestrateEnq OrchestrateEnqueuer
}

// NewApprovalService 构造.
func NewApprovalService(db *gorm.DB, m *runstate.Machine) *ApprovalService {
	return &ApprovalService{db: db, machine: m}
}

// WithTimeoutEnqueuer 链式注入延时任务 enqueuer, 启用超时分支.
func (s *ApprovalService) WithTimeoutEnqueuer(enq TimeoutEnqueuer) *ApprovalService {
	s.timeoutEnq = enq
	return s
}

// WithOrchestrateEnqueuer 审批通过后入队 run orchestrate (T2.2.4).
func (s *ApprovalService) WithOrchestrateEnqueuer(enq OrchestrateEnqueuer) *ApprovalService {
	s.orchestrateEnq = enq
	return s
}

// Create 在 GORM tx 中落 approval_requests + 把 run 从 running 推到 approval.
//
// 注意: 调用方需要在 tx 外 enqueue asynq 延时超时任务 (避免 tx 长时间持锁).
// 这里返回 *ApprovalRequest 是为了让 caller 拿到 ID + 算 timeout_at.
//
// Quirk: machine.Transition 走独立 *sql.DB, 看不到本 tx 的中间状态.
// 所以 run 状态推进放到 tx Commit 之后; 任何一步失败都不留半成品.
func (s *ApprovalService) Create(ctx context.Context, in CreateInput) (*model.ApprovalRequest, error) {
	if s == nil || s.db == nil {
		return nil, ErrApprovalDBUnavailable
	}
	if in.Mode == "" {
		in.Mode = "any"
	}
	if in.Mode == "quorum" {
		return nil, fmt.Errorf("%w: quorum (use any/all)", ErrUnsupportedMode)
	}
	if in.Mode != "any" && in.Mode != "all" {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedMode, in.Mode)
	}
	if in.OnTimeout == "" {
		in.OnTimeout = "reject"
	}
	if len(in.RequiredApprovers) == 0 {
		return nil, fmt.Errorf("at least one approver required")
	}

	req := &model.ApprovalRequest{
		RunID:             in.RunID,
		StageID:           in.StageID,
		RequiredApprovers: pq.StringArray(in.RequiredApprovers),
		Mode:              in.Mode,
		Status:            "pending",
		OnTimeout:         in.OnTimeout,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if in.Timeout > 0 {
		t := time.Now().UTC().Add(in.Timeout)
		req.TimeoutAt = &t
	}

	err := s.db.WithContext(ctx).Create(req).Error
	if err != nil {
		return nil, fmt.Errorf("insert approval_request: %w", err)
	}

	// 推 run 状态: running → approval. 失败不回滚 request (前端可见有 pending 请求, 后续可手动处理).
	if s.machine != nil {
		if mErr := s.machine.MarkApproval(ctx, in.RunID, runstate.TransitionOpts{
			Reason: fmt.Sprintf("waiting approval on stage=%s", in.StageID),
		}); mErr != nil {
			log.Printf("[approval] machine.MarkApproval run=%d stage=%s err=%v (request_id=%d 已落)",
				in.RunID, in.StageID, mErr, req.ID)
		}
	}

	// 入 asynq 延时超时任务 (T2.6.3). 没配 timeoutEnq 或 timeout=0 都跳过.
	if s.timeoutEnq != nil && in.Timeout > 0 {
		if _, tErr := s.timeoutEnq.EnqueueApprovalTimeout(ctx,
			&tasks.ApprovalTimeoutPayload{RequestID: req.ID}, in.Timeout); tErr != nil {
			log.Printf("[approval] enqueue timeout task request=%d err=%v (request 已落, 超时分支失效)",
				req.ID, tErr)
		}
	}
	return req, nil
}

// Approve 校验 + 落 approve 投票 + 按 mode 推进状态.
func (s *ApprovalService) Approve(ctx context.Context, in VoteInput) (*VoteResult, error) {
	return s.vote(ctx, in, "approve")
}

// Reject 任一拒绝即整体 rejected.
func (s *ApprovalService) Reject(ctx context.Context, in VoteInput) (*VoteResult, error) {
	return s.vote(ctx, in, "reject")
}

// vote 共用逻辑. tx 内只动 approvals/approval_requests; tx 外推 run.
func (s *ApprovalService) vote(ctx context.Context, in VoteInput, decision string) (*VoteResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrApprovalDBUnavailable
	}
	if in.Username == "" {
		return nil, fmt.Errorf("username required")
	}

	var (
		req       model.ApprovalRequest
		vote      model.Approval
		nextRun   string // run 目标状态; "" 表示不推进
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 拿 run 当前状态 + pending request (路径锁定到 stage_id)
		var runStatus string
		if err := tx.Raw(`SELECT status FROM runs WHERE id=$1 FOR UPDATE`, in.RunID).
			Scan(&runStatus).Error; err != nil {
			return fmt.Errorf("lock run: %w", err)
		}
		if runStatus == "" {
			return ErrApprovalNotFound // run 不存在也走这条统一 404
		}
		if runStatus != runstate.StatusApproval {
			return fmt.Errorf("%w: run.status=%s", ErrRunNotInApproval, runStatus)
		}

		if err := tx.Where("run_id = ? AND stage_id = ? AND status = ?",
			in.RunID, in.StageID, "pending").First(&req).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrApprovalNotFound
			}
			return fmt.Errorf("load approval_request: %w", err)
		}

		// 2. 校验 caller 在 approvers 名单 (含 '*' 通配)
		if !isApprover(in.Username, req.RequiredApprovers) {
			return ErrNotApprover
		}

		// 3. 落投票 (unique 约束保证不重复; 转 409)
		vote = model.Approval{
			RequestID: req.ID,
			UserID:    in.UserID,
			Username:  in.Username,
			Decision:  decision,
			Comment:   in.Comment,
			CreatedAt: time.Now().UTC(),
		}
		if err := tx.Create(&vote).Error; err != nil {
			// 22 = pg unique violation; gorm 不直接给 code, 文本匹配兜底
			if isUniqueViolation(err) {
				return ErrAlreadyVoted
			}
			return fmt.Errorf("insert approval: %w", err)
		}

		// 4. 按 mode 判定 request 终态
		if decision == "reject" {
			req.Status = "rejected"
			nextRun = runstate.StatusFailed
		} else {
			switch req.Mode {
			case "any":
				req.Status = "approved"
				nextRun = runstate.StatusRunning
			case "all":
				// 看是否全员都投了 approve
				var approveCount int64
				if err := tx.Model(&model.Approval{}).
					Where("request_id = ? AND decision = ?", req.ID, "approve").
					Count(&approveCount).Error; err != nil {
					return fmt.Errorf("count approves: %w", err)
				}
				if int(approveCount) >= len(req.RequiredApprovers) {
					req.Status = "approved"
					nextRun = runstate.StatusRunning
				}
				// 否则保持 pending, nextRun=""
			}
		}

		if req.Status != "pending" {
			req.UpdatedAt = time.Now().UTC()
			if err := tx.Save(&req).Error; err != nil {
				return fmt.Errorf("update request: %w", err)
			}
		}

		// 5. 写 audit_logs (tx 内一起落; resource_type='approval_request')
		//    用 raw SQL 而非 GORM Create 以避开 actor_ip="" → INET 类型解析错.
		payload := map[string]any{
			"decision":   decision,
			"username":   in.Username,
			"request_id": req.ID,
			"stage_id":   req.StageID,
			"comment":    in.Comment,
		}
		pBytes, _ := jsonMarshal(payload)
		auditAction := "approval." + decision
		if err := tx.Exec(`
			INSERT INTO audit_logs (actor_id, action, resource_type, resource_id, payload, result, created_at)
			VALUES (?, ?, 'approval_request', ?, CAST(? AS jsonb), 'success', now())
		`, in.UserID, auditAction, req.ID, string(pBytes)).Error; err != nil {
			return fmt.Errorf("insert audit: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 6. tx 外推 run 状态 (machine 走独立 *sql.DB)
	if nextRun != "" && s.machine != nil {
		reason := fmt.Sprintf("approval %s by %s on stage=%s", decision, in.Username, in.StageID)
		var mErr error
		switch nextRun {
		case runstate.StatusRunning:
			mErr = s.machine.MarkRunning(ctx, in.RunID, runstate.TransitionOpts{
				ActorID: in.UserID, Reason: reason,
			})
		case runstate.StatusFailed:
			mErr = s.machine.MarkFailed(ctx, in.RunID, reason, runstate.TransitionOpts{
				ActorID: in.UserID,
			})
		}
		if mErr != nil {
			log.Printf("[approval] machine transition run=%d → %s err=%v", in.RunID, nextRun, mErr)
		} else if nextRun == runstate.StatusRunning && s.orchestrateEnq != nil && s.db != nil {
			sqlDB, dbErr := s.db.DB()
			if dbErr == nil {
				if rErr := runengine.ResumeOrchestrate(ctx, sqlDB, s.orchestrateEnq, in.RunID); rErr != nil {
					log.Printf("[approval] resume orchestrate run=%d err=%v", in.RunID, rErr)
				}
			}
		}
	}

	return &VoteResult{
		Request:       &req,
		Vote:          &vote,
		NextRunStatus: nextRun,
	}, nil
}

// ListByRun 拿一个 run 的所有 approval_requests + 它们的投票.
// 给 GET /api/v1/runs/:id 内嵌使用.
func (s *ApprovalService) ListByRun(ctx context.Context, runID int64) ([]ApprovalSummary, error) {
	if s == nil || s.db == nil {
		return nil, ErrApprovalDBUnavailable
	}
	var reqs []model.ApprovalRequest
	if err := s.db.WithContext(ctx).Where("run_id = ?", runID).
		Order("created_at ASC, id ASC").Find(&reqs).Error; err != nil {
		return nil, fmt.Errorf("list requests: %w", err)
	}
	if len(reqs) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(reqs))
	for _, r := range reqs {
		ids = append(ids, r.ID)
	}
	var votes []model.Approval
	if err := s.db.WithContext(ctx).Where("request_id IN ?", ids).
		Order("created_at ASC, id ASC").Find(&votes).Error; err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	byReq := map[int64][]model.Approval{}
	for _, v := range votes {
		byReq[v.RequestID] = append(byReq[v.RequestID], v)
	}

	out := make([]ApprovalSummary, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, ApprovalSummary{ApprovalRequest: r, Votes: byReq[r.ID]})
	}
	return out, nil
}

// ===== helpers =====

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func isApprover(username string, list []string) bool {
	for _, a := range list {
		if a == "*" || a == username {
			return true
		}
	}
	return false
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pq 错误信息含 "duplicate key value violates unique constraint"
	msg := err.Error()
	return contains(msg, "duplicate key") || contains(msg, "unique constraint")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
