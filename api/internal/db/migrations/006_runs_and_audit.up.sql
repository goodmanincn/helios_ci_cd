-- 006_runs_and_audit: 执行记录、阶段、步骤、审计日志

CREATE TABLE runs (
  id            BIGSERIAL PRIMARY KEY,
  pipeline_id   BIGINT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
  version_id    BIGINT NOT NULL REFERENCES pipeline_versions(id),
  number        INT NOT NULL,
  status        VARCHAR(32) NOT NULL CHECK (status IN ('pending','queued','running','success','failed','canceled','approval','timeout')),
  trigger_type  VARCHAR(32) CHECK (trigger_type IN ('push','tag','manual','schedule','webhook','api','retry')),
  trigger_data  JSONB,
  commit_sha    VARCHAR(64),
  branch        VARCHAR(255),
  message       TEXT,
  triggered_by  BIGINT REFERENCES users(id),
  started_at    TIMESTAMPTZ,
  finished_at   TIMESTAMPTZ,
  duration_ms   INT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (pipeline_id, number)
);
CREATE INDEX idx_runs_pipeline_status ON runs(pipeline_id, status, created_at DESC);
CREATE INDEX idx_runs_triggered_by    ON runs(triggered_by) WHERE triggered_by IS NOT NULL;
CREATE INDEX idx_runs_trigger_gin     ON runs USING GIN (trigger_data);

CREATE TABLE stages (
  id            BIGSERIAL PRIMARY KEY,
  run_id        BIGINT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  stage_id      VARCHAR(64) NOT NULL,
  name          VARCHAR(128),
  status        VARCHAR(32) CHECK (status IN ('pending','queued','running','success','failed','skipped','canceled','approval')),
  needs         TEXT[] DEFAULT '{}',
  matrix_index  INT,
  matrix_values JSONB,
  started_at    TIMESTAMPTZ,
  finished_at   TIMESTAMPTZ,
  exit_code     INT
);
CREATE INDEX idx_stages_run ON stages(run_id);

CREATE TABLE steps (
  id               BIGSERIAL PRIMARY KEY,
  stage_record_id  BIGINT NOT NULL REFERENCES stages(id) ON DELETE CASCADE,
  step_index       INT,
  name             VARCHAR(255),
  uses             VARCHAR(255),
  status           VARCHAR(32) CHECK (status IN ('pending','running','success','failed','skipped','canceled')),
  exit_code        INT,
  log_object       TEXT,    -- MinIO 对象 key (替代 spec 中的 log_offset)
  log_size         BIGINT,
  started_at       TIMESTAMPTZ,
  finished_at      TIMESTAMPTZ
);
CREATE INDEX idx_steps_stage ON steps(stage_record_id);

CREATE TABLE audit_logs (
  id             BIGSERIAL PRIMARY KEY,
  actor_id       BIGINT REFERENCES users(id),
  actor_ip       INET,
  org_id         BIGINT REFERENCES organizations(id),
  action         VARCHAR(128) NOT NULL,
  resource_type  VARCHAR(64),
  resource_id    BIGINT,
  payload        JSONB,
  result         VARCHAR(16) CHECK (result IN ('success','fail','denied')),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_actor_time    ON audit_logs(actor_id, created_at DESC);
CREATE INDEX idx_audit_resource      ON audit_logs(resource_type, resource_id);
CREATE INDEX idx_audit_action_time   ON audit_logs(action, created_at DESC);
CREATE INDEX idx_audit_payload_gin   ON audit_logs USING GIN (payload);
