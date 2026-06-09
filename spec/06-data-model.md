# 06. 数据模型

PostgreSQL 15 主库,所有 JSONB 字段建 GIN 索引。

## 6.1 核心表

```sql
-- 用户与组织
CREATE TABLE users (
  id BIGSERIAL PRIMARY KEY,
  username VARCHAR(64) UNIQUE NOT NULL,
  email VARCHAR(255) UNIQUE NOT NULL,
  password_hash VARCHAR(255),
  oidc_subject VARCHAR(255),
  is_active BOOLEAN DEFAULT true,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE organizations (
  id BIGSERIAL PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  slug VARCHAR(64) UNIQUE NOT NULL,
  owner_id BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE org_members (
  org_id BIGINT REFERENCES organizations(id),
  user_id BIGINT REFERENCES users(id),
  role VARCHAR(32) NOT NULL,  -- owner / admin / member
  PRIMARY KEY (org_id, user_id)
);

-- 项目
CREATE TABLE projects (
  id BIGSERIAL PRIMARY KEY,
  org_id BIGINT NOT NULL REFERENCES organizations(id),
  name VARCHAR(128) NOT NULL,
  slug VARCHAR(64) NOT NULL,
  repo_url TEXT NOT NULL,
  repo_type VARCHAR(32) NOT NULL,  -- github / gitlab / gitee / gitea
  default_branch VARCHAR(128) DEFAULT 'main',
  visibility VARCHAR(16) DEFAULT 'private',
  config JSONB DEFAULT '{}',
  created_at TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(org_id, slug)
);

-- 流水线
CREATE TABLE pipelines (
  id BIGSERIAL PRIMARY KEY,
  project_id BIGINT NOT NULL REFERENCES projects(id),
  name VARCHAR(128) NOT NULL,
  current_version_id BIGINT,
  enabled BOOLEAN DEFAULT true,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE pipeline_versions (
  id BIGSERIAL PRIMARY KEY,
  pipeline_id BIGINT NOT NULL REFERENCES pipelines(id),
  version INT NOT NULL,
  spec JSONB NOT NULL,        -- 完整 YAML 解析后的结构
  spec_raw TEXT NOT NULL,     -- 原始 YAML
  created_by BIGINT REFERENCES users(id),
  message TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(pipeline_id, version)
);

-- 执行
CREATE TABLE runs (
  id BIGSERIAL PRIMARY KEY,
  pipeline_id BIGINT NOT NULL REFERENCES pipelines(id),
  version_id BIGINT NOT NULL REFERENCES pipeline_versions(id),
  number INT NOT NULL,            -- 项目内自增 (#247)
  status VARCHAR(32) NOT NULL,    -- pending / running / success / failed / canceled / approval
  trigger_type VARCHAR(32),       -- push / tag / manual / schedule / webhook
  trigger_data JSONB,
  commit_sha VARCHAR(64),
  branch VARCHAR(255),
  triggered_by BIGINT REFERENCES users(id),
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  duration_ms INT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_runs_pipeline_status ON runs(pipeline_id, status, created_at DESC);

CREATE TABLE stages (
  id BIGSERIAL PRIMARY KEY,
  run_id BIGINT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  stage_id VARCHAR(64) NOT NULL,   -- DSL 中的 id
  name VARCHAR(128),
  status VARCHAR(32),
  needs TEXT[],
  matrix_index INT,                -- 矩阵展开索引
  matrix_values JSONB,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  exit_code INT
);

CREATE TABLE steps (
  id BIGSERIAL PRIMARY KEY,
  stage_record_id BIGINT NOT NULL REFERENCES stages(id) ON DELETE CASCADE,
  step_index INT,
  name VARCHAR(255),
  uses VARCHAR(255),
  status VARCHAR(32),
  exit_code INT,
  log_offset BIGINT,               -- MinIO 对象内偏移
  log_size BIGINT,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ
);

-- 集群
CREATE TABLE clusters (
  id BIGSERIAL PRIMARY KEY,
  org_id BIGINT NOT NULL REFERENCES organizations(id),
  name VARCHAR(128) NOT NULL,
  provider VARCHAR(32) NOT NULL,    -- selfhosted / tke / ack / cce / eks
  region VARCHAR(64),
  endpoint TEXT,
  credential_id BIGINT REFERENCES secrets(id),
  config JSONB,
  status VARCHAR(32) DEFAULT 'unknown',
  last_health_check TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE hosts (
  id BIGSERIAL PRIMARY KEY,
  org_id BIGINT NOT NULL REFERENCES organizations(id),
  name VARCHAR(128) NOT NULL,
  ip INET NOT NULL,
  ssh_port INT DEFAULT 22,
  ssh_user VARCHAR(64) DEFAULT 'root',
  credential_id BIGINT REFERENCES secrets(id),
  os VARCHAR(64),
  status VARCHAR(32) DEFAULT 'unknown',
  last_heartbeat TIMESTAMPTZ
);

CREATE TABLE host_groups (
  id BIGSERIAL PRIMARY KEY,
  org_id BIGINT NOT NULL REFERENCES organizations(id),
  name VARCHAR(128) NOT NULL,
  vars JSONB DEFAULT '{}'
);

CREATE TABLE host_group_members (
  group_id BIGINT REFERENCES host_groups(id) ON DELETE CASCADE,
  host_id BIGINT REFERENCES hosts(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, host_id)
);

-- 密钥
CREATE TABLE secrets (
  id BIGSERIAL PRIMARY KEY,
  scope VARCHAR(32) NOT NULL,       -- org / project / pipeline
  scope_id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL,
  type VARCHAR(32) NOT NULL,        -- text / file / kubeconfig / ssh-key / cloud-credential
  encrypted_value BYTEA NOT NULL,
  encryption_kek_id VARCHAR(128),
  created_by BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(scope, scope_id, name)
);

-- Runner
CREATE TABLE runners (
  id BIGSERIAL PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  type VARCHAR(32) NOT NULL,        -- k8s / docker / ssh / serverless
  labels TEXT[],
  capacity INT DEFAULT 1,
  current_load INT DEFAULT 0,
  status VARCHAR(32),
  last_heartbeat TIMESTAMPTZ
);

-- 审计
CREATE TABLE audit_logs (
  id BIGSERIAL PRIMARY KEY,
  actor_id BIGINT REFERENCES users(id),
  actor_ip INET,
  action VARCHAR(128) NOT NULL,     -- pipeline.create / secret.read / deploy.execute
  resource_type VARCHAR(64),
  resource_id BIGINT,
  payload JSONB,
  result VARCHAR(16),               -- success / fail
  created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_audit_actor_time ON audit_logs(actor_id, created_at DESC);
```

## 6.2 索引策略

- runs 表: 按 pipeline_id + status + created_at 复合索引,支持"最近运行"快速查询
- audit_logs: actor + time 索引 + 月度分区
- 历史 run 超过 6 个月归档到冷表 (runs_archive)

## 6.3 数据保留与清理

| 表 | 保留策略 |
|---|---|
| runs | 1 年 (可配置) |
| stages / steps | 跟随 runs |
| 日志 (MinIO) | 90 天 (可配置) |
| audit_logs | 3 年 (合规要求) |
| pipeline_versions | 永久保留 (轻量) |
