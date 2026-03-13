package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/clarion/internal/service"
)

// 编译时接口一致性检查。
var _ service.CallRepo = (*PgCallStore)(nil)

// PgCallStore 提供基于 PostgreSQL 的通话数据操作。
type PgCallStore struct {
	pool PoolQuerier
}

// NewPgCallStore 创建新的 PgCallStore。
func NewPgCallStore(pool PoolQuerier) *PgCallStore {
	return &PgCallStore{pool: pool}
}

// Create 插入新的通话记录并返回其 ID。
func (s *PgCallStore) Create(ctx context.Context, c *model.Call) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO calls (contact_id, task_id, template_snapshot_id, session_id,
		 status, answer_type, duration, record_url, transcript, extracted_fields,
		 result_grade, next_action, rule_trace, ai_summary)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING id`,
		c.ContactID, c.TaskID, c.TemplateSnapshotID, c.SessionID,
		c.Status, c.AnswerType, c.Duration, c.RecordURL, c.Transcript,
		c.ExtractedFields, c.ResultGrade, c.NextAction, c.RuleTrace, c.AISummary,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create call: %w", err)
	}
	return id, nil
}

// GetByID 根据 ID 查询通话记录，未找到时返回 nil。
func (s *PgCallStore) GetByID(ctx context.Context, id int64) (*model.Call, error) {
	c := &model.Call{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, contact_id, task_id, template_snapshot_id, session_id,
		 status, answer_type, duration, record_url, transcript, extracted_fields,
		 result_grade, next_action, rule_trace, ai_summary, created_at, updated_at
		 FROM calls WHERE id = $1`, id,
	).Scan(&c.ID, &c.ContactID, &c.TaskID, &c.TemplateSnapshotID, &c.SessionID,
		&c.Status, &c.AnswerType, &c.Duration, &c.RecordURL, &c.Transcript,
		&c.ExtractedFields, &c.ResultGrade, &c.NextAction, &c.RuleTrace,
		&c.AISummary, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get call %d: %w", id, err)
	}
	return c, nil
}

// ListByTask 返回指定任务的分页通话列表及总数。
func (s *PgCallStore) ListByTask(ctx context.Context, taskID int64, offset, limit int) ([]model.Call, int, error) {
	var total int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM calls WHERE task_id = $1`, taskID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count calls: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, contact_id, task_id, template_snapshot_id, session_id,
		 status, answer_type, duration, record_url, transcript, extracted_fields,
		 result_grade, next_action, rule_trace, ai_summary, created_at, updated_at
		 FROM calls WHERE task_id = $1 ORDER BY id LIMIT $2 OFFSET $3`,
		taskID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list calls: %w", err)
	}
	defer rows.Close()

	var calls []model.Call
	for rows.Next() {
		var c model.Call
		if err := rows.Scan(&c.ID, &c.ContactID, &c.TaskID, &c.TemplateSnapshotID, &c.SessionID,
			&c.Status, &c.AnswerType, &c.Duration, &c.RecordURL, &c.Transcript,
			&c.ExtractedFields, &c.ResultGrade, &c.NextAction, &c.RuleTrace,
			&c.AISummary, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan call: %w", err)
		}
		calls = append(calls, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate calls: %w", err)
	}

	return calls, total, nil
}

// Update 修改已有的通话记录。
func (s *PgCallStore) Update(ctx context.Context, c *model.Call) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE calls SET status = $1, answer_type = $2, duration = $3,
		 record_url = $4, transcript = $5, extracted_fields = $6,
		 result_grade = $7, next_action = $8, rule_trace = $9,
		 ai_summary = $10, updated_at = $11 WHERE id = $12`,
		c.Status, c.AnswerType, c.Duration, c.RecordURL, c.Transcript,
		c.ExtractedFields, c.ResultGrade, c.NextAction, c.RuleTrace,
		c.AISummary, time.Now(), c.ID)
	if err != nil {
		return fmt.Errorf("update call: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("call %d not found", c.ID)
	}
	return nil
}

// CreateTurn 插入新的对话轮次并返回其 ID。
func (s *PgCallStore) CreateTurn(ctx context.Context, t *model.DialogueTurn) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO dialogue_turns (call_id, turn_number, speaker, content,
		 state_before, state_after, asr_latency_ms, llm_latency_ms, tts_latency_ms,
		 asr_confidence, is_interrupted)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id`,
		t.CallID, t.TurnNumber, t.Speaker, t.Content,
		t.StateBefore, t.StateAfter, t.ASRLatencyMs, t.LLMLatencyMs, t.TTSLatencyMs,
		t.ASRConfidence, t.IsInterrupted,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create turn: %w", err)
	}
	return id, nil
}

// ListTurns 返回指定通话的所有对话轮次，按轮次编号排序。
func (s *PgCallStore) ListTurns(ctx context.Context, callID int64) ([]model.DialogueTurn, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, call_id, turn_number, speaker, content, state_before, state_after,
		 asr_latency_ms, llm_latency_ms, tts_latency_ms, asr_confidence, is_interrupted, created_at
		 FROM dialogue_turns WHERE call_id = $1 ORDER BY turn_number`, callID)
	if err != nil {
		return nil, fmt.Errorf("list turns: %w", err)
	}
	defer rows.Close()

	var turns []model.DialogueTurn
	for rows.Next() {
		var t model.DialogueTurn
		if err := rows.Scan(&t.ID, &t.CallID, &t.TurnNumber, &t.Speaker, &t.Content,
			&t.StateBefore, &t.StateAfter, &t.ASRLatencyMs, &t.LLMLatencyMs, &t.TTSLatencyMs,
			&t.ASRConfidence, &t.IsInterrupted, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		turns = append(turns, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate turns: %w", err)
	}

	return turns, nil
}

// CreateEvent 插入新的通话事件并返回其 ID。
func (s *PgCallStore) CreateEvent(ctx context.Context, e *model.CallEvent) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO call_events (call_id, event_type, timestamp_ms, metadata_json)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		e.CallID, e.EventType, e.TimestampMs, e.MetadataJSON,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create event: %w", err)
	}
	return id, nil
}

// ListEvents 返回指定通话的所有事件，按时间戳排序。
func (s *PgCallStore) ListEvents(ctx context.Context, callID int64) ([]model.CallEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, call_id, event_type, timestamp_ms, metadata_json, created_at
		 FROM call_events WHERE call_id = $1 ORDER BY timestamp_ms`, callID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var events []model.CallEvent
	for rows.Next() {
		var e model.CallEvent
		if err := rows.Scan(&e.ID, &e.CallID, &e.EventType, &e.TimestampMs,
			&e.MetadataJSON, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}

	return events, nil
}
