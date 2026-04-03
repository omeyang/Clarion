-- 003_multi_tenant.down.sql
-- 回滚租户体系：移除 tenant_id 列和相关表。

-- 移除新索引
DROP INDEX IF EXISTS idx_opportunities_tenant_score;
DROP INDEX IF EXISTS uq_calls_tenant_contact_task;
DROP INDEX IF EXISTS idx_calls_tenant_created;
DROP INDEX IF EXISTS idx_tasks_tenant_status;
DROP INDEX IF EXISTS idx_templates_tenant_status;
DROP INDEX IF EXISTS idx_contacts_tenant_status;
DROP INDEX IF EXISTS idx_contacts_tenant_phone;

-- 恢复原索引
CREATE UNIQUE INDEX IF NOT EXISTS idx_contacts_phone_hash ON contacts (phone_hash);
CREATE UNIQUE INDEX IF NOT EXISTS uq_calls_contact_task
    ON calls (contact_id, task_id)
    WHERE status NOT IN ('failed', 'no_answer');

-- 移除 tenant_id 列
ALTER TABLE opportunities DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE calls DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE call_tasks DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE scenario_templates DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE contacts DROP COLUMN IF EXISTS tenant_id;

-- 删除新表
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS tenants;
