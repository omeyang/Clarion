-- 001_initial_schema.down.sql
-- Reverse of 001_initial_schema.up.sql

DROP TABLE IF EXISTS call_events CASCADE;
DROP TABLE IF EXISTS dialogue_turns CASCADE;
DROP TABLE IF EXISTS calls CASCADE;
DROP TABLE IF EXISTS call_tasks CASCADE;

-- Drop trigger before dropping table.
DROP TRIGGER IF EXISTS trg_prevent_snapshot_mutation ON template_snapshots;
DROP FUNCTION IF EXISTS prevent_snapshot_mutation();

DROP TABLE IF EXISTS template_snapshots CASCADE;
DROP TABLE IF EXISTS scenario_templates CASCADE;
DROP TABLE IF EXISTS contacts CASCADE;
