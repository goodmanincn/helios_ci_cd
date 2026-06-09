-- 005_infrastructure: 集群 / 主机 / 主机组 / Runner

CREATE TABLE clusters (
  id                BIGSERIAL PRIMARY KEY,
  org_id            BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name              VARCHAR(128) NOT NULL,
  provider          VARCHAR(32)  NOT NULL CHECK (provider IN ('selfhosted','tke','ack','cce','eks','gke','aks')),
  region            VARCHAR(64),
  endpoint          TEXT,
  credential_id     BIGINT REFERENCES secrets(id) ON DELETE SET NULL,
  config            JSONB DEFAULT '{}',
  status            VARCHAR(32) DEFAULT 'unknown' CHECK (status IN ('unknown','healthy','degraded','unhealthy','disconnected')),
  last_health_check TIMESTAMPTZ,
  created_by        BIGINT REFERENCES users(id),
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at        TIMESTAMPTZ,
  UNIQUE (org_id, name)
);
CREATE INDEX idx_clusters_org ON clusters(org_id);
CREATE INDEX idx_clusters_deleted_at ON clusters(deleted_at) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_clusters_updated_at BEFORE UPDATE ON clusters
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TABLE hosts (
  id              BIGSERIAL PRIMARY KEY,
  org_id          BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name            VARCHAR(128) NOT NULL,
  ip              INET NOT NULL,
  ssh_port        INT DEFAULT 22 CHECK (ssh_port > 0 AND ssh_port <= 65535),
  ssh_user        VARCHAR(64) DEFAULT 'root',
  credential_id   BIGINT REFERENCES secrets(id) ON DELETE SET NULL,
  os              VARCHAR(64),
  arch            VARCHAR(32),
  labels          JSONB DEFAULT '{}',
  status          VARCHAR(32) DEFAULT 'unknown' CHECK (status IN ('unknown','online','offline','error')),
  last_heartbeat  TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at      TIMESTAMPTZ,
  UNIQUE (org_id, name)
);
CREATE INDEX idx_hosts_org ON hosts(org_id);
CREATE INDEX idx_hosts_labels_gin ON hosts USING GIN (labels);
CREATE INDEX idx_hosts_deleted_at ON hosts(deleted_at) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_hosts_updated_at BEFORE UPDATE ON hosts
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TABLE host_groups (
  id         BIGSERIAL PRIMARY KEY,
  org_id     BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name       VARCHAR(128) NOT NULL,
  vars       JSONB DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (org_id, name)
);
CREATE TRIGGER trg_host_groups_updated_at BEFORE UPDATE ON host_groups
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TABLE host_group_members (
  group_id BIGINT NOT NULL REFERENCES host_groups(id) ON DELETE CASCADE,
  host_id  BIGINT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, host_id)
);
CREATE INDEX idx_host_group_members_host ON host_group_members(host_id);

CREATE TABLE runners (
  id             BIGSERIAL PRIMARY KEY,
  name           VARCHAR(128) NOT NULL UNIQUE,
  type           VARCHAR(32) NOT NULL CHECK (type IN ('k8s','docker','ssh','serverless')),
  labels         TEXT[] DEFAULT '{}',
  capacity       INT DEFAULT 1 CHECK (capacity > 0),
  current_load   INT DEFAULT 0,
  status         VARCHAR(32) DEFAULT 'unknown' CHECK (status IN ('unknown','idle','busy','offline','error')),
  last_heartbeat TIMESTAMPTZ,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_runners_labels_gin ON runners USING GIN (labels);
CREATE TRIGGER trg_runners_updated_at BEFORE UPDATE ON runners
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
