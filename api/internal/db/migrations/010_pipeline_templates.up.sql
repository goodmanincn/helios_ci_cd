-- 010_pipeline_templates: 流水线模板表 (M8 T8.2.1)
--
-- 设计:
-- 单独表与 pipelines 解耦, 模板不归属任何 project, 全局可见或 org 私有.
-- spec_raw 存原始 YAML, spec 存解析后 JSONB 给 UI 快速展示.
-- builtin=true 的模板由 seed 写入, 不可删除.

CREATE TABLE pipeline_templates (
  id           BIGSERIAL PRIMARY KEY,
  slug         VARCHAR(64)  UNIQUE NOT NULL,         -- 标识, e.g. "node-docker-k8s"
  name         VARCHAR(128) NOT NULL,                -- 显示名
  description  TEXT,
  category     VARCHAR(32),                          -- "build" / "deploy" / "release" / "fullstack"
  tags         TEXT[] DEFAULT '{}',                  -- ["node", "docker", "k8s"]
  spec         JSONB NOT NULL,                       -- 解析后, UI 渲染节点用
  spec_raw     TEXT  NOT NULL,                       -- 原始 YAML, 克隆时写入新 pipeline_version
  builtin      BOOLEAN NOT NULL DEFAULT FALSE,       -- seed 写入的内置模板
  org_id       BIGINT REFERENCES organizations(id) ON DELETE CASCADE, -- NULL = 全局可见
  created_by   BIGINT REFERENCES users(id),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at   TIMESTAMPTZ
);
CREATE INDEX idx_pipeline_templates_category ON pipeline_templates(category);
CREATE INDEX idx_pipeline_templates_org ON pipeline_templates(org_id) WHERE org_id IS NOT NULL;
CREATE INDEX idx_pipeline_templates_tags ON pipeline_templates USING GIN (tags);
CREATE INDEX idx_pipeline_templates_deleted_at ON pipeline_templates(deleted_at) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_pipeline_templates_updated_at BEFORE UPDATE ON pipeline_templates
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
