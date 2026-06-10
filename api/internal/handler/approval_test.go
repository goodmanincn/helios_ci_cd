// approval_test.go — ApprovalHandler 单元 + handler/service 协同 (T2.6.2).
//
// 用 fake ApprovalVoter 隔离 service, 不依赖 DB.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/helios-cicd/helios/api/internal/middleware"
	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/service"
	heliosjwt "github.com/helios-cicd/helios/api/pkg/jwt"
)

// fakeVoter 满足 ApprovalVoter.
type fakeVoter struct {
	approveCalls []service.VoteInput
	rejectCalls  []service.VoteInput
	res          *service.VoteResult
	err          error
}

func (f *fakeVoter) Approve(_ context.Context, in service.VoteInput) (*service.VoteResult, error) {
	f.approveCalls = append(f.approveCalls, in)
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

func (f *fakeVoter) Reject(_ context.Context, in service.VoteInput) (*service.VoteResult, error) {
	f.rejectCalls = append(f.rejectCalls, in)
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

func fakeAuthWithUsername(uid int64, uname string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(middleware.CtxClaimsKey, &heliosjwt.Claims{
			UserID:   uid,
			Username: uname,
		})
		c.Set(middleware.CtxUserIDKey, uid)
		c.Next()
	}
}

func newApprovalRouter(t *testing.T, v *fakeVoter, uname string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(fakeAuthWithUsername(7, uname))
	NewApprovalHandler(v).Register(g)
	return r
}

func okResult() *service.VoteResult {
	return &service.VoteResult{
		Request: &model.ApprovalRequest{ID: 11, Status: "approved"},
		Vote:    &model.Approval{ID: 99, Decision: "approve", Username: "alice"},
		NextRunStatus: "running",
	}
}

// ---- approve ----

func TestApprovalHandler_Approve_OK(t *testing.T) {
	v := &fakeVoter{res: okResult()}
	srv := httptest.NewServer(newApprovalRouter(t, v, "alice"))
	defer srv.Close()

	body, _ := json.Marshal(map[string]string{"comment": "lgtm"})
	resp, err := http.Post(srv.URL+"/api/v1/runs/1/approvals/manual/approve", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, "approved", got["request_status"])
	require.Equal(t, "running", got["next_run_status"])

	require.Len(t, v.approveCalls, 1)
	require.Equal(t, int64(1), v.approveCalls[0].RunID)
	require.Equal(t, "manual", v.approveCalls[0].StageID)
	require.Equal(t, "alice", v.approveCalls[0].Username)
	require.Equal(t, "lgtm", v.approveCalls[0].Comment)
}

func TestApprovalHandler_Reject_NeedsComment(t *testing.T) {
	v := &fakeVoter{res: okResult()}
	srv := httptest.NewServer(newApprovalRouter(t, v, "alice"))
	defer srv.Close()

	// reject 不带 comment 应 400
	resp, err := http.Post(srv.URL+"/api/v1/runs/1/approvals/manual/reject",
		"application/json", bytes.NewReader([]byte(`{"comment":""}`)))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Len(t, v.rejectCalls, 0)
}

func TestApprovalHandler_Reject_OK(t *testing.T) {
	v := &fakeVoter{res: &service.VoteResult{
		Request: &model.ApprovalRequest{ID: 11, Status: "rejected"},
		Vote:    &model.Approval{ID: 99, Decision: "reject", Username: "alice"},
		NextRunStatus: "failed",
	}}
	srv := httptest.NewServer(newApprovalRouter(t, v, "alice"))
	defer srv.Close()

	body, _ := json.Marshal(map[string]string{"comment": "nope ahead"})
	resp, err := http.Post(srv.URL+"/api/v1/runs/1/approvals/manual/reject",
		"application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, v.rejectCalls, 1)
}

// ---- 错误映射 ----

func TestApprovalHandler_NotApprover_403(t *testing.T) {
	v := &fakeVoter{err: service.ErrNotApprover}
	srv := httptest.NewServer(newApprovalRouter(t, v, "mallory"))
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/api/v1/runs/1/approvals/manual/approve",
		"application/json", bytes.NewReader([]byte(`{}`)))
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestApprovalHandler_AlreadyVoted_409(t *testing.T) {
	v := &fakeVoter{err: service.ErrAlreadyVoted}
	srv := httptest.NewServer(newApprovalRouter(t, v, "alice"))
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/api/v1/runs/1/approvals/manual/approve",
		"application/json", bytes.NewReader([]byte(`{}`)))
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestApprovalHandler_RunNotInApproval_409(t *testing.T) {
	v := &fakeVoter{err: service.ErrRunNotInApproval}
	srv := httptest.NewServer(newApprovalRouter(t, v, "alice"))
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/api/v1/runs/1/approvals/manual/approve",
		"application/json", bytes.NewReader([]byte(`{}`)))
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestApprovalHandler_RequestNotFound_404(t *testing.T) {
	v := &fakeVoter{err: service.ErrApprovalNotFound}
	srv := httptest.NewServer(newApprovalRouter(t, v, "alice"))
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/api/v1/runs/1/approvals/manual/approve",
		"application/json", bytes.NewReader([]byte(`{}`)))
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestApprovalHandler_SvcUnavailable_503(t *testing.T) {
	v := &fakeVoter{err: service.ErrApprovalDBUnavailable}
	srv := httptest.NewServer(newApprovalRouter(t, v, "alice"))
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/api/v1/runs/1/approvals/manual/approve",
		"application/json", bytes.NewReader([]byte(`{}`)))
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestApprovalHandler_GenericErr_500(t *testing.T) {
	v := &fakeVoter{err: errors.New("oh no")}
	srv := httptest.NewServer(newApprovalRouter(t, v, "alice"))
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/api/v1/runs/1/approvals/manual/approve",
		"application/json", bytes.NewReader([]byte(`{}`)))
	defer resp.Body.Close()
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// ---- 路径 / 鉴权 ----

func TestApprovalHandler_BadRunID_400(t *testing.T) {
	v := &fakeVoter{res: okResult()}
	srv := httptest.NewServer(newApprovalRouter(t, v, "alice"))
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/api/v1/runs/abc/approvals/manual/approve",
		"application/json", bytes.NewReader([]byte(`{}`)))
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestApprovalHandler_NoAuth_401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewApprovalHandler(&fakeVoter{res: okResult()}).Register(r.Group("/api/v1"))
	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/api/v1/runs/1/approvals/manual/approve",
		"application/json", bytes.NewReader([]byte(`{}`)))
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
