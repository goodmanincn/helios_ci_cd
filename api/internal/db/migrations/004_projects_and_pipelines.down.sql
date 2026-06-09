ALTER TABLE pipelines DROP CONSTRAINT IF EXISTS fk_pipelines_current_version;
DROP TABLE IF EXISTS pipeline_versions;
DROP TABLE IF EXISTS pipelines;
DROP TABLE IF EXISTS projects;
