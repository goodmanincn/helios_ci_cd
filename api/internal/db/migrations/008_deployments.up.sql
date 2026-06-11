-- 008_deployments: 部署历史记录 (E4.5)

CREATE TABLE deployments (
  id           BIGSERIAL PRIMARY KEY,
  cluster_id   BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
  namespace    VARCHAR(128) NOT NULL,
  name         VARCHAR(128) NOT NULL,
  image        TEXT,
  revision     BIGINT,
  run_id       BIGINT REFERENCES runs(id) ON DELETE SET NULL,
  status       VARCHAR(32) DEFAULT 'success' CHECK (status IN ('success','failed','rollback')),
  spec         JSONB DEFAULT '{}',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_deployments_cluster ON deployments(cluster_id, namespace, name);
CREATE INDEX idx_deployments_run ON deployments(run_id);
CREATE INDEX idx_deployments_created_at ON deployments(created_at DESC);
