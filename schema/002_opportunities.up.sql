-- 002_opportunities.up.sql
-- 商机表：从通话中提取的结构化商业信号。

CREATE TABLE IF NOT EXISTS opportunities (
    id                 BIGSERIAL    PRIMARY KEY,
    call_id            BIGINT       NOT NULL REFERENCES calls(id),
    contact_id         BIGINT       NOT NULL REFERENCES contacts(id),
    task_id            BIGINT       NOT NULL,
    score              SMALLINT     NOT NULL DEFAULT 0,
    intent_type        TEXT         NOT NULL DEFAULT '',
    budget_signal      TEXT         NOT NULL DEFAULT 'not_mentioned',
    timeline_signal    TEXT         NOT NULL DEFAULT 'not_mentioned',
    contact_role       TEXT         NOT NULL DEFAULT 'unknown',
    pain_points        JSONB        NOT NULL DEFAULT '[]',
    followup_action    TEXT         NOT NULL DEFAULT '',
    followup_date      DATE,
    needs_human_review BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- 每通通话只产生一条商机记录。
CREATE UNIQUE INDEX IF NOT EXISTS uq_opportunities_call ON opportunities (call_id);
CREATE INDEX IF NOT EXISTS idx_opportunities_contact ON opportunities (contact_id);
CREATE INDEX IF NOT EXISTS idx_opportunities_task_score ON opportunities (task_id, score DESC);
CREATE INDEX IF NOT EXISTS idx_opportunities_followup ON opportunities (followup_action, needs_human_review)
    WHERE needs_human_review = TRUE;
