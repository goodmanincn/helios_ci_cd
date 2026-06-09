-- 004_projects_and_pipelines: 项目、流水线、版本化

CREATE TABLE projects (
  id              BIGSERIAL PRIMARY KEY,
  org_id          BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name            VARCHAR(128) NOT NULL,
  slug            VARCHAR(64)  NOT NULL,
  description     TEXT,
  repo_url        TEXT NOT NULL,
  repo_type       VARCHAR(32) NOT NULL CHECK (repo_type IN ('github','gitlab','gitee','gitea','bitbucket')),
  default_branch  VARCHAR(128) DEFAULT 'main',
  visibility      VARCHAR(16) DEFAULT 'private' CHECK (visibility IN ('private','public','internal')),
  config          JSONB NOT NULL DEFAULT '{}',
  created_by      BIGINT REFERENCES users(id),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at      TIMESTAMPTZ,
  UNIQUE (org_id, slug)
);
CREATE INDEX idx_projects_org ON projects(org_id);
CREATE INDEX idx_projects_config_gin ON projects USING GIN (config);
CREATE INDEX idx_projects_deleted_at ON projects(deleted_at) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_projects_updated_at BEFORE UPDATE ON projects
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TABLE pipelines (
  id                 BIGSERIAL PRIMARY KEY,
  project_id         BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name               VARCHAR(128) NOT NULL,
  description        TEXT,
  current_version_id BIGINT,            -- FK 在 005 中添加 (forward ref)
  enabled            BOOLEAN DEFAULT TRUE,
  created_by         BIGINT REFERENCES users(id),
  created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at         TIMESTAMPTZ
);
CREATE INDEX idx_pipelines_project ON pipelines(project_id);
CREATE INDEX idx_pipelines_deleted_at ON pipelines(deleted_at) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_pipelines_updated_at BEFORE UPDATE ON pipelines
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TABLE pipeline_versions (
  id          BIGSERIAL PRIMARY KEY,
  pipeline_id BIGINT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
  version     INT NOT NULL,
  spec        JSONB NOT NULL,           -- 解析后的结构
  spec_raw    TEXT  NOT NULL,           -- 原始 YAML
  created_by  BIGINT REFERENCES users(id),
  message     TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (pipeline_id, version)
);
CREATE INDEX idx_pipeline_versions_pipeline ON pipeline_versions(pipeline_id, version DESC);
CREATE INDEX idx_pipeline_versions_spec_gin ON pipeline_versions USING GIN (spec);

-- forward reference 闭环: pipelines.current_version_id -> pipeline_versions.id
ALTER TABLE pipelines
  ADD CONSTRAINT fk_pipelines_current_version
  FOREIGN KEY (current_version_id) REFERENCES pipeline_versions(id) ON DELETE SET NULL;
