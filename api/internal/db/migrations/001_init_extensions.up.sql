-- 001_init_extensions: 启用 PostgreSQL 扩展 + 全局触发器

-- 用 inet/cidr 类型自带,无需扩展
-- pgcrypto 用于种子数据 gen_random_uuid (可选)
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- 通用 updated_at 自动更新触发器函数
CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
