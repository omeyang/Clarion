-- 003_multi_tenant.up.sql
-- 租户体系与认证：租户表、API Key 表、业务表增加 tenant_id。

-- ═══ 1. 新增表 ═══

CREATE TABLE IF NOT EXISTS tenants (
    id                UUID        PRIMARY KEY,
    slug              TEXT        NOT NULL UNIQUE,
    name              TEXT        NOT NULL,
    contact_person    TEXT        NOT NULL DEFAULT '',
    contact_phone     TEXT        NOT NULL DEFAULT '',
    status            TEXT        NOT NULL DEFAULT 'active',
    daily_call_limit  INT         NOT NULL DEFAULT 100,
    max_concurrent    INT         NOT NULL DEFAULT 3,
    settings          JSONB       NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_keys (
    id           BIGSERIAL   PRIMARY KEY,
    tenant_id    UUID        NOT NULL REFERENCES tenants(id),
    key_prefix   TEXT        NOT NULL,
    key_hash     TEXT        NOT NULL,
    name         TEXT        NOT NULL DEFAULT '',
    status       TEXT        NOT NULL DEFAULT 'active',
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys (key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys (tenant_id);

-- ═══ 2. 默认租户（归属已有数据）═══

INSERT INTO tenants (id, slug, name, status)
VALUES ('00000000-0000-0000-0000-000000000000', 'default', '默认租户', 'active')
ON CONFLICT (id) DO NOTHING;

-- ═══ 3. 业务表增加 tenant_id ═══
-- 分三步：加 nullable 列 → 填充默认租户 → 改 NOT NULL

-- Step 1: 加 nullable 列
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
ALTER TABLE scenario_templates ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
ALTER TABLE call_tasks ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
ALTER TABLE calls ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
ALTER TABLE opportunities ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);

-- Step 2: 已有数据归属默认租户
UPDATE contacts SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
UPDATE scenario_templates SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
UPDATE call_tasks SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
UPDATE calls SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
UPDATE opportunities SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;

-- Step 3: 改 NOT NULL
ALTER TABLE contacts ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE scenario_templates ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE call_tasks ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE calls ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE opportunities ALTER COLUMN tenant_id SET NOT NULL;

-- ═══ 4. 更新索引 ═══

-- contacts: 同一租户内手机号唯一
DROP INDEX IF EXISTS idx_contacts_phone_hash;
CREATE UNIQUE INDEX idx_contacts_tenant_phone ON contacts (tenant_id, phone_hash);
CREATE INDEX idx_contacts_tenant_status ON contacts (tenant_id, current_status);

-- scenario_templates: 按租户查询
CREATE INDEX idx_templates_tenant_status ON scenario_templates (tenant_id, status);

-- call_tasks: 按租户查询
CREATE INDEX idx_tasks_tenant_status ON call_tasks (tenant_id, status);

-- calls: 按租户查询 + 唯一约束
CREATE INDEX idx_calls_tenant_created ON calls (tenant_id, created_at);
DROP INDEX IF EXISTS uq_calls_contact_task;
CREATE UNIQUE INDEX uq_calls_tenant_contact_task
    ON calls (tenant_id, contact_id, task_id)
    WHERE status NOT IN ('failed', 'no_answer');

-- opportunities: 按租户查询
CREATE INDEX idx_opportunities_tenant_score ON opportunities (tenant_id, score DESC);
