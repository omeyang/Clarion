package postprocess

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/store"
)

// Writer 将通话结果、对话轮次和通话事件持久化到 PostgreSQL。
type Writer struct {
	pool   store.PoolQuerier
	logger *slog.Logger
}

// NewWriter 创建由给定数据库连接支持的 Writer。
func NewWriter(pool store.PoolQuerier, logger *slog.Logger) *Writer {
	return &Writer{pool: pool, logger: logger}
}

// WriteCallResult 使用最终结果更新通话记录。此操作是幂等的：
// 如果通话已设置 result_grade，则跳过写入。
func (w *Writer) WriteCallResult(ctx context.Context, event *CallCompletionEvent) error {
	fieldsJSON, err := json.Marshal(event.CollectedFields)
	if err != nil {
		return fmt.Errorf("marshal collected fields: %w", err)
	}

	tag, err := w.pool.Exec(ctx, `
		UPDATE calls
		SET status = $1,
		    result_grade = $2,
		    extracted_fields = $3,
		    ai_summary = $4,
		    next_action = $5,
		    duration = $6,
		    updated_at = NOW()
		WHERE id = $7
		  AND (result_grade IS NULL OR result_grade = '')`,
		"completed",
		string(event.Grade),
		fieldsJSON,
		event.Summary,
		event.NextAction,
		event.DurationSec,
		event.CallID,
	)
	if err != nil {
		return fmt.Errorf("update call %d: %w", event.CallID, err)
	}

	if tag.RowsAffected() == 0 {
		w.logger.Info("call result already written, skipping",
			slog.Int64("call_id", event.CallID))
	}

	return nil
}

// WriteTurns 为通话插入对话轮次。使用 ON CONFLICT DO NOTHING
// 实现幂等性。
func (w *Writer) WriteTurns(ctx context.Context, callID int64, turns []dialogue.Turn) error {
	if len(turns) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, t := range turns {
		batch.Queue(`
			INSERT INTO dialogue_turns (call_id, turn_number, speaker, content,
			    state_before, state_after, asr_latency_ms, llm_latency_ms,
			    tts_latency_ms, is_interrupted, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
			ON CONFLICT (call_id, turn_number) DO NOTHING`,
			callID, t.Number, t.Speaker, t.Content,
			t.StateBefore.String(), t.StateAfter.String(),
			t.ASRLatencyMs, t.LLMLatencyMs, t.TTSLatencyMs, t.Interrupted,
		)
	}

	br := w.pool.SendBatch(ctx, batch)
	defer func() {
		if err := br.Close(); err != nil {
			w.logger.Error("close batch results", slog.String("error", err.Error()))
		}
	}()

	for range turns {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert dialogue turn for call %d: %w", callID, err)
		}
	}

	return nil
}

// WriteOpportunity 将商机记录写入数据库。使用 ON CONFLICT (call_id) DO UPDATE
// 实现幂等性：同一通话的商机只保留最新版本。
func (w *Writer) WriteOpportunity(ctx context.Context, opp *Opportunity) error {
	painPointsJSON, err := json.Marshal(opp.PainPoints)
	if err != nil {
		return fmt.Errorf("marshal pain points: %w", err)
	}

	var followupDate *time.Time
	if opp.FollowupDate != nil {
		followupDate = opp.FollowupDate
	}

	_, err = w.pool.Exec(ctx, `
		INSERT INTO opportunities (
			call_id, contact_id, task_id, score, intent_type,
			budget_signal, timeline_signal, contact_role,
			pain_points, followup_action, followup_date,
			needs_human_review, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW())
		ON CONFLICT (call_id) DO UPDATE SET
			score = EXCLUDED.score,
			intent_type = EXCLUDED.intent_type,
			budget_signal = EXCLUDED.budget_signal,
			timeline_signal = EXCLUDED.timeline_signal,
			contact_role = EXCLUDED.contact_role,
			pain_points = EXCLUDED.pain_points,
			followup_action = EXCLUDED.followup_action,
			followup_date = EXCLUDED.followup_date,
			needs_human_review = EXCLUDED.needs_human_review,
			updated_at = NOW()`,
		opp.CallID, opp.ContactID, opp.TaskID, opp.Score, opp.IntentType,
		opp.BudgetSignal, opp.TimelineSignal, opp.ContactRole,
		painPointsJSON, opp.FollowupAction, followupDate,
		opp.NeedsHumanReview,
	)
	if err != nil {
		return fmt.Errorf("upsert opportunity for call %d: %w", opp.CallID, err)
	}

	return nil
}

// WriteEvents 为通话插入通话事件。使用 ON CONFLICT DO NOTHING
// 实现幂等性。
func (w *Writer) WriteEvents(ctx context.Context, callID int64, events []engine.RecordedEvent) error {
	if len(events) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, e := range events {
		metaJSON, err := json.Marshal(e.Metadata)
		if err != nil {
			metaJSON = []byte("{}")
		}
		batch.Queue(`
			INSERT INTO call_events (call_id, event_type, timestamp_ms,
			    metadata_json, created_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (call_id, event_type, timestamp_ms) DO NOTHING`,
			callID, string(e.EventType), e.TimestampMs, metaJSON,
		)
	}

	br := w.pool.SendBatch(ctx, batch)
	defer func() {
		if err := br.Close(); err != nil {
			w.logger.Error("close batch results", slog.String("error", err.Error()))
		}
	}()

	for range events {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert call event for call %d: %w", callID, err)
		}
	}

	return nil
}
