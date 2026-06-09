-- 002_users_and_orgs: 用户、组织、成员关系

CREATE TABLE users (
  id            BIGSERIAL PRIMARY KEY,
  username      VARCHAR(64)  UNIQUE NOT NULL,
  email         VARCHAR(255) UNIQUE NOT NULL,
  password_hash VARCHAR(255),
  oidc_subject  VARCHAR(255),
  display_name  VARCHAR(128),
  avatar_url    TEXT,
  is_active     BOOLEAN DEFAULT TRUE,
  last_login_at TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at    TIMESTAMPTZ
);
CREATE INDEX idx_users_oidc_subject ON users(oidc_subject) WHERE oidc_subject IS NOT NULL;
CREATE INDEX idx_users_deleted_at ON users(deleted_at) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_users_updated_at BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TABLE organizations (
  id          BIGSERIAL PRIMARY KEY,
  name        VARCHAR(128) NOT NULL,
  slug        VARCHAR(64)  UNIQUE NOT NULL,
  owner_id    BIGINT REFERENCES users(id),
  description TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at  TIMESTAMPTZ
);
CREATE INDEX idx_orgs_owner_id ON organizations(owner_id);
CREATE INDEX idx_orgs_deleted_at ON organizations(deleted_at) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_orgs_updated_at BEFORE UPDATE ON organizations
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TABLE org_members (
  org_id     BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       VARCHAR(32) NOT NULL CHECK (role IN ('owner','admin','member')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (org_id, user_id)
);
CREATE INDEX idx_org_members_user ON org_members(user_id);
