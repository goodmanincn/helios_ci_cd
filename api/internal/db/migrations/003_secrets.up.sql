-- 003_secrets: 密钥保险箱 (放在 clusters/hosts 之前以满足外键)

CREATE TABLE secrets (
  id                BIGSERIAL PRIMARY KEY,
  scope             VARCHAR(32) NOT NULL CHECK (scope IN ('org','project','pipeline')),
  scope_id          BIGINT NOT NULL,
  name              VARCHAR(128) NOT NULL,
  type              VARCHAR(32)  NOT NULL CHECK (type IN ('text','file','kubeconfig','ssh-key','cloud-credential')),
  description       TEXT,
  encrypted_value   BYTEA NOT NULL,
  encryption_kek_id VARCHAR(128),
  created_by        BIGINT REFERENCES users(id),
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at        TIMESTAMPTZ,
  UNIQUE (scope, scope_id, name)
);
CREATE INDEX idx_secrets_scope ON secrets(scope, scope_id);
CREATE INDEX idx_secrets_deleted_at ON secrets(deleted_at) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_secrets_updated_at BEFORE UPDATE ON secrets
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
