-- 001_initial_schema.up.sql
-- Core schema for Clarion AI outbound voice engine.

-- Contacts: outbound call targets.
CREATE TABLE IF NOT EXISTS contacts (
    id              BIGSERIAL PRIMARY KEY,
    phone_masked    TEXT        NOT NULL,
    phone_hash      TEXT        NOT NULL,
    source          TEXT        NOT NULL DEFAULT '',
    profile_json    JSONB       NOT NULL DEFAULT '{}',
    current_status  TEXT        NOT NULL DEFAULT 'new',
    do_not_call     BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_contacts_phone_hash ON contacts (phone_hash);
CREATE INDEX IF NOT EXISTS idx_contacts_source_status ON contacts (source, current_status);
CREATE INDEX IF NOT EXISTS idx_contacts_profile ON contacts USING GIN (profile_json);

-- Scenario templates: define dialogue flows.
CREATE TABLE IF NOT EXISTS scenario_templates (
    id                      BIGSERIAL PRIMARY KEY,
    name                    TEXT        NOT NULL,
    domain                  TEXT        NOT NULL DEFAULT '',
    opening_script          TEXT        NOT NULL DEFAULT '',
    state_machine_config    JSONB       NOT NULL DEFAULT '{}',
    extraction_schema       JSONB       NOT NULL DEFAULT '{}',
    grading_rules           JSONB       NOT NULL DEFAULT '{}',
    prompt_templates        JSONB       NOT NULL DEFAULT '{}',
    notification_config     JSONB       NOT NULL DEFAULT '{}',
    call_protection_config  JSONB       NOT NULL DEFAULT '{}',
    precompiled_audios      JSONB       NOT NULL DEFAULT '{}',
    status                  TEXT        NOT NULL DEFAULT 'draft',
    version                 INT         NOT NULL DEFAULT 1,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Template snapshots: immutable copies for runtime use.
CREATE TABLE IF NOT EXISTS template_snapshots (
    id              BIGSERIAL PRIMARY KEY,
    template_id     BIGINT      NOT NULL REFERENCES scenario_templates(id),
    snapshot_data   JSONB       NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Prevent mutation of snapshots.
CREATE OR REPLACE FUNCTION prevent_snapshot_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'template_snapshots is immutable: UPDATE and DELETE are not allowed';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_prevent_snapshot_mutation
    BEFORE UPDATE OR DELETE ON template_snapshots
    FOR EACH ROW EXECUTE FUNCTION prevent_snapshot_mutation();

-- Call tasks: batched outbound call jobs.
CREATE TABLE IF NOT EXISTS call_tasks (
    id                      BIGSERIAL PRIMARY KEY,
    name                    TEXT        NOT NULL,
    scenario_template_id    BIGINT      NOT NULL REFERENCES scenario_templates(id),
    template_snapshot_id    BIGINT      REFERENCES template_snapshots(id),
    contact_filter          JSONB       NOT NULL DEFAULT '{}',
    schedule_config         JSONB       NOT NULL DEFAULT '{}',
    daily_limit             INT         NOT NULL DEFAULT 0,
    max_concurrent          INT         NOT NULL DEFAULT 1,
    status                  TEXT        NOT NULL DEFAULT 'draft',
    total_contacts          INT         NOT NULL DEFAULT 0,
    completed_contacts      INT         NOT NULL DEFAULT 0,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Calls: individual call records.
CREATE TABLE IF NOT EXISTS calls (
    id                      BIGSERIAL PRIMARY KEY,
    contact_id              BIGINT      NOT NULL REFERENCES contacts(id),
    task_id                 BIGINT      NOT NULL REFERENCES call_tasks(id),
    template_snapshot_id    BIGINT      REFERENCES template_snapshots(id),
    session_id              TEXT        NOT NULL DEFAULT '',
    status                  TEXT        NOT NULL DEFAULT 'pending',
    answer_type             TEXT        NOT NULL DEFAULT 'unknown',
    duration                INT         NOT NULL DEFAULT 0,
    record_url              TEXT        NOT NULL DEFAULT '',
    transcript              TEXT        NOT NULL DEFAULT '',
    extracted_fields        JSONB       NOT NULL DEFAULT '{}',
    result_grade            TEXT        NOT NULL DEFAULT '',
    next_action             TEXT        NOT NULL DEFAULT '',
    rule_trace              JSONB       NOT NULL DEFAULT '{}',
    ai_summary              TEXT        NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_calls_task_status ON calls (task_id, status);
CREATE INDEX IF NOT EXISTS idx_calls_contact ON calls (contact_id);
CREATE INDEX IF NOT EXISTS idx_calls_created_at ON calls (created_at);

-- Partial unique index: prevent duplicate active calls for same contact+task.
CREATE UNIQUE INDEX IF NOT EXISTS uq_calls_contact_task
    ON calls (contact_id, task_id)
    WHERE status NOT IN ('failed', 'no_answer');

-- Dialogue turns: per-call conversation log.
CREATE TABLE IF NOT EXISTS dialogue_turns (
    id              BIGSERIAL PRIMARY KEY,
    call_id         BIGINT      NOT NULL REFERENCES calls(id),
    turn_number     INT         NOT NULL,
    speaker         TEXT        NOT NULL,
    content         TEXT        NOT NULL DEFAULT '',
    state_before    TEXT        NOT NULL DEFAULT '',
    state_after     TEXT        NOT NULL DEFAULT '',
    asr_latency_ms  INT         NOT NULL DEFAULT 0,
    llm_latency_ms  INT         NOT NULL DEFAULT 0,
    tts_latency_ms  INT         NOT NULL DEFAULT 0,
    asr_confidence  REAL        NOT NULL DEFAULT 0.0,
    is_interrupted  BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dialogue_turns_call_turn ON dialogue_turns (call_id, turn_number);

-- Call events: media-level event log with millisecond timestamps.
CREATE TABLE IF NOT EXISTS call_events (
    id              BIGSERIAL PRIMARY KEY,
    call_id         BIGINT      NOT NULL REFERENCES calls(id),
    event_type      TEXT        NOT NULL,
    timestamp_ms    BIGINT      NOT NULL,
    metadata_json   JSONB       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_call_events_call_type ON call_events (call_id, event_type);
CREATE INDEX IF NOT EXISTS idx_call_events_timestamp ON call_events (call_id, timestamp_ms);
