-- 007_approvals: 人工审批请求 + 投票记录 (E2.6)
--
-- 设计:
--   - approval_requests 一行 = 一个 stage 的待审批请求, 每个 (run_id, stage_id) 至多一条 pending
--   - approvals 行 = 一个 approver 的投票 (approve/reject), 唯一约束防止重复投
--   - on_timeout 在创建时写死, 之后改 stage DSL 不影响存量请求
--   - status=timeout 是终态 (与 rejected 等价但区分上报), 与 runs.status='timeout' 对齐

CREATE TABLE approval_requests (
  id                  BIGSERIAL PRIMARY KEY,
  run_id              BIGINT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  stage_id            VARCHAR(64) NOT NULL,
  required_approvers  TEXT[] NOT NULL,
  mode                VARCHAR(16) NOT NULL DEFAULT 'any'
                      CHECK (mode IN ('any','all')),
  status              VARCHAR(16) NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending','approved','rejected','timeout','canceled')),
  on_timeout          VARCHAR(16) NOT NULL DEFAULT 'reject'
                      CHECK (on_timeout IN ('reject','approve','pause')),
  timeout_at          TIMESTAMPTZ,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 同 (run_id, stage_id) 只能有一条 pending; approved/rejected/timeout/canceled 可以多条 (历史)
CREATE UNIQUE INDEX uq_approval_pending_per_stage
  ON approval_requests(run_id, stage_id)
  WHERE status = 'pending';

CREATE INDEX idx_approval_requests_run    ON approval_requests(run_id);
CREATE INDEX idx_approval_requests_status ON approval_requests(status);

CREATE TABLE approvals (
  id          BIGSERIAL PRIMARY KEY,
  request_id  BIGINT NOT NULL REFERENCES approval_requests(id) ON DELETE CASCADE,
  user_id     BIGINT REFERENCES users(id),
  username    VARCHAR(64) NOT NULL,  -- 也存 username 便于 audit / 系统投票 'system'
  decision    VARCHAR(16) NOT NULL CHECK (decision IN ('approve','reject')),
  comment     TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (request_id, username)
);

CREATE INDEX idx_approvals_request ON approvals(request_id);
