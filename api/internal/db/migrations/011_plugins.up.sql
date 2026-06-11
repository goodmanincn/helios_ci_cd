-- 011_plugins: 插件市场表 (M9 MVP 切片)
--
-- 三张表:
--   plugins              一个插件 (namespace/name 唯一)
--   plugin_versions      插件的多版本, action.yml 全文 + 解析后 JSONB
--   plugin_installations 一个 org 装的插件 + 选定版本 (org_id, plugin_id 唯一)
--
-- official=true 的插件由 seed 写入 (helios/echo, helios/trivy, helios/dingtalk),
-- 不允许删除. 非 official 留给后续 publish 流程.

CREATE TABLE plugins (
  id           BIGSERIAL PRIMARY KEY,
  namespace    VARCHAR(64)  NOT NULL,                 -- helios / acme / ...
  name         VARCHAR(64)  NOT NULL,                 -- echo / trivy
  slug         VARCHAR(128) GENERATED ALWAYS AS (namespace || '/' || name) STORED UNIQUE,
  description  TEXT,
  category     VARCHAR(32),                            -- security / notify / build / test / deploy
  publisher    VARCHAR(128),                           -- 显示名 (e.g. "Helios Official")
  repository   TEXT,                                   -- 源码仓库 URL
  verified     BOOLEAN NOT NULL DEFAULT FALSE,         -- 官方/经审核
  official     BOOLEAN NOT NULL DEFAULT FALSE,         -- seed 写入, 不可删除
  downloads    BIGINT  NOT NULL DEFAULT 0,             -- 累计安装次数 (展示用, 非严格)
  latest_version VARCHAR(64),                          -- 冗余冗余 plugin_versions 的最新 version 字符串
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at   TIMESTAMPTZ
);
CREATE INDEX idx_plugins_category   ON plugins(category);
CREATE INDEX idx_plugins_verified   ON plugins(verified);
CREATE INDEX idx_plugins_deleted_at ON plugins(deleted_at) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_plugins_updated_at BEFORE UPDATE ON plugins
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TABLE plugin_versions (
  id           BIGSERIAL PRIMARY KEY,
  plugin_id    BIGINT NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
  version      VARCHAR(64) NOT NULL,                   -- v1 / 1.2.3
  action_yml   TEXT  NOT NULL,                         -- 原始 action.yml
  action_spec  JSONB NOT NULL,                         -- 解析后 (Action struct JSON)
  readme       TEXT,
  changelog    TEXT,
  is_latest    BOOLEAN NOT NULL DEFAULT FALSE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(plugin_id, version)
);
CREATE INDEX idx_plugin_versions_plugin ON plugin_versions(plugin_id);

CREATE TABLE plugin_installations (
  id            BIGSERIAL PRIMARY KEY,
  org_id        BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  plugin_id     BIGINT NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
  version_id    BIGINT NOT NULL REFERENCES plugin_versions(id) ON DELETE RESTRICT,
  installed_by  BIGINT REFERENCES users(id),
  installed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(org_id, plugin_id)
);
CREATE INDEX idx_plugin_installations_org ON plugin_installations(org_id);
