-- 010_pipeline_templates: rollback
DROP TRIGGER IF EXISTS trg_pipeline_templates_updated_at ON pipeline_templates;
DROP TABLE IF EXISTS pipeline_templates;
