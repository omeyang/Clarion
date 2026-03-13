package postprocess

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
)

// mockBatchResults 模拟 pgx.BatchResults 接口。
type mockBatchResults struct {
	execFn  func() (pgconn.CommandTag, error)
	closeFn func() error
	callIdx int
}

func (m *mockBatchResults) Exec() (pgconn.CommandTag, error) {
	m.callIdx++
	if m.execFn != nil {
		return m.execFn()
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (m *mockBatchResults) Query() (pgx.Rows, error) {
	return nil, errors.New("not implemented")
}

func (m *mockBatchResults) QueryRow() pgx.Row {
	return nil
}

func (m *mockBatchResults) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// mockPool 模拟 store.PoolQuerier 接口。
type mockPool struct {
	execFn      func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	sendBatchFn func(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

func (m *mockPool) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPool) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return nil
}

func (m *mockPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFn != nil {
		return m.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (m *mockPool) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	if m.sendBatchFn != nil {
		return m.sendBatchFn(ctx, b)
	}
	return &mockBatchResults{}
}

func TestWriteCallResult(t *testing.T) {
	tests := []struct {
		name    string
		event   *CallCompletionEvent
		execFn  func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
		wantErr bool
	}{
		{
			name: "成功更新通话结果",
			event: &CallCompletionEvent{
				CallID:          1,
				Grade:           engine.GradeA,
				CollectedFields: map[string]string{"name": "Alice"},
				Summary:         "客户有兴趣",
				NextAction:      "下周回访",
				DurationSec:     120,
			},
			execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
				assert.Equal(t, "completed", args[0])
				assert.Equal(t, string(engine.GradeA), args[1])
				assert.Equal(t, "客户有兴趣", args[3])
				assert.Equal(t, int64(1), args[6])
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		},
		{
			name: "通话结果已存在时跳过（幂等）",
			event: &CallCompletionEvent{
				CallID:          2,
				Grade:           engine.GradeB,
				CollectedFields: map[string]string{},
			},
			execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		},
		{
			name: "数据库执行失败",
			event: &CallCompletionEvent{
				CallID:          3,
				CollectedFields: map[string]string{},
			},
			execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("connection refused")
			},
			wantErr: true,
		},
		{
			name: "空 CollectedFields",
			event: &CallCompletionEvent{
				CallID:          4,
				CollectedFields: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := &mockPool{execFn: tt.execFn}
			w := NewWriter(pool, slog.Default())
			err := w.WriteCallResult(context.Background(), tt.event)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestWriteTurns(t *testing.T) {
	tests := []struct {
		name        string
		turns       []dialogue.Turn
		sendBatchFn func(ctx context.Context, b *pgx.Batch) pgx.BatchResults
		wantErr     bool
	}{
		{
			name:  "空轮次直接返回",
			turns: nil,
		},
		{
			name: "成功写入多轮对话",
			turns: []dialogue.Turn{
				{
					Number:       1,
					Speaker:      "bot",
					Content:      "你好",
					StateBefore:  engine.DialogueOpening,
					StateAfter:   engine.DialogueOpening,
					ASRLatencyMs: 100,
					LLMLatencyMs: 200,
					TTSLatencyMs: 150,
					Interrupted:  false,
				},
				{
					Number:       2,
					Speaker:      "user",
					Content:      "你好，请问有什么事？",
					StateBefore:  engine.DialogueOpening,
					StateAfter:   engine.DialogueQualification,
					ASRLatencyMs: 80,
					Interrupted:  true,
				},
			},
			sendBatchFn: func(_ context.Context, b *pgx.Batch) pgx.BatchResults {
				assert.Equal(t, 2, b.Len())
				return &mockBatchResults{}
			},
		},
		{
			name: "批量执行失败",
			turns: []dialogue.Turn{
				{Number: 1, Speaker: "bot", Content: "hello", StateBefore: engine.DialogueOpening, StateAfter: engine.DialogueOpening},
			},
			sendBatchFn: func(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
				return &mockBatchResults{
					execFn: func() (pgconn.CommandTag, error) {
						return pgconn.CommandTag{}, errors.New("unique violation")
					},
				}
			},
			wantErr: true,
		},
		{
			name: "Close 返回错误时仅记日志",
			turns: []dialogue.Turn{
				{Number: 1, Speaker: "bot", Content: "hi", StateBefore: engine.DialogueOpening, StateAfter: engine.DialogueOpening},
			},
			sendBatchFn: func(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
				return &mockBatchResults{
					closeFn: func() error {
						return errors.New("close error")
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := &mockPool{sendBatchFn: tt.sendBatchFn}
			w := NewWriter(pool, slog.Default())
			err := w.WriteTurns(context.Background(), 1, tt.turns)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestWriteEvents(t *testing.T) {
	tests := []struct {
		name        string
		events      []engine.RecordedEvent
		sendBatchFn func(ctx context.Context, b *pgx.Batch) pgx.BatchResults
		wantErr     bool
	}{
		{
			name:   "空事件直接返回",
			events: nil,
		},
		{
			name: "成功写入事件",
			events: []engine.RecordedEvent{
				{EventType: engine.EventBargeIn, TimestampMs: 1000, Metadata: map[string]string{"key": "val"}},
				{EventType: engine.EventUserSpeechStart, TimestampMs: 2000, Metadata: nil},
			},
			sendBatchFn: func(_ context.Context, b *pgx.Batch) pgx.BatchResults {
				assert.Equal(t, 2, b.Len())
				return &mockBatchResults{}
			},
		},
		{
			name: "批量执行失败",
			events: []engine.RecordedEvent{
				{EventType: engine.EventBargeIn, TimestampMs: 3000},
			},
			sendBatchFn: func(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
				return &mockBatchResults{
					execFn: func() (pgconn.CommandTag, error) {
						return pgconn.CommandTag{}, errors.New("db error")
					},
				}
			},
			wantErr: true,
		},
		{
			name: "Close 返回错误时仅记日志",
			events: []engine.RecordedEvent{
				{EventType: engine.EventBargeIn, TimestampMs: 4000},
			},
			sendBatchFn: func(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
				return &mockBatchResults{
					closeFn: func() error {
						return errors.New("close error")
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := &mockPool{sendBatchFn: tt.sendBatchFn}
			w := NewWriter(pool, slog.Default())
			err := w.WriteEvents(context.Background(), 1, tt.events)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestWriteOpportunity(t *testing.T) {
	followupDate := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		opp     *Opportunity
		execFn  func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
		wantErr bool
	}{
		{
			name: "成功写入商机",
			opp: &Opportunity{
				CallID:           1,
				ContactID:        10,
				TaskID:           100,
				Score:            75,
				IntentType:       "interested",
				BudgetSignal:     "has_budget",
				TimelineSignal:   "soon",
				ContactRole:      "decision_maker",
				PainPoints:       []string{"效率低"},
				FollowupAction:   "follow_up",
				FollowupDate:     &followupDate,
				NeedsHumanReview: true,
			},
			execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				assert.Contains(t, sql, "INSERT INTO opportunities")
				assert.Equal(t, int64(1), args[0])
				assert.Equal(t, 75, args[3])
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		},
		{
			name: "无跟进日期",
			opp: &Opportunity{
				CallID:         2,
				ContactID:      20,
				TaskID:         200,
				Score:          30,
				IntentType:     "not_interested",
				BudgetSignal:   "not_mentioned",
				TimelineSignal: "not_mentioned",
				ContactRole:    "unknown",
				PainPoints:     nil,
				FollowupAction: "abandon",
			},
		},
		{
			name: "数据库执行失败",
			opp: &Opportunity{
				CallID:     3,
				PainPoints: []string{},
			},
			execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("connection refused")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := &mockPool{execFn: tt.execFn}
			w := NewWriter(pool, slog.Default())
			err := w.WriteOpportunity(context.Background(), tt.opp)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
