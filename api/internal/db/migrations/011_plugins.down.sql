-- 011_plugins: rollback
DROP TRIGGER IF EXISTS trg_plugins_updated_at ON plugins;
DROP TABLE IF EXISTS plugin_installations;
DROP TABLE IF EXISTS plugin_versions;
DROP TABLE IF EXISTS plugins;
