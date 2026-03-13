package postprocess

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/notify"
	"github.com/omeyang/clarion/internal/provider"
	"github.com/omeyang/clarion/internal/store"
)

// mockNotifier 用于测试的通知记录器。
type mockNotifier struct {
	sent  []notify.FollowUpNotification
	errFn func() error
}

func (m *mockNotifier) SendFollowUpNotification(_ context.Context, n notify.FollowUpNotification) error {
	m.sent = append(m.sent, n)
	if m.errFn != nil {
		return m.errFn()
	}
	return nil
}

// mockRedisClient 用于 processMessage 单元测试的最小 Redis 替身。
// 注意：processMessage 会调用 ack（XAck），因此仍依赖 store.RDS。
// 以下测试通过真实 Redis 或 skip 来处理 ack。

func TestWorkerConfig_Defaults(t *testing.T) {
	cfg := WorkerConfig{
		StreamKey:     "test:stream",
		ConsumerGroup: "test-group",
		ConsumerName:  "worker-1",
		BatchSize:     10,
		BlockMs:       1000,
	}

	assert.Equal(t, "test:stream", cfg.StreamKey)
	assert.Equal(t, "test-group", cfg.ConsumerGroup)
	assert.Equal(t, "worker-1", cfg.ConsumerName)
	assert.Equal(t, int64(10), cfg.BatchSize)
	assert.Equal(t, int64(1000), cfg.BlockMs)
}

func TestNewWorker(t *testing.T) {
	cfg := WorkerConfig{
		StreamKey:     "test:stream",
		ConsumerGroup: "test-group",
		ConsumerName:  "worker-1",
		BatchSize:     10,
		BlockMs:       1000,
	}

	rds := &store.RDS{Client: redis.NewClient(&redis.Options{Addr: "localhost:6379"})}
	defer rds.Client.Close()

	notif := &mockNotifier{}
	logger := slog.Default()

	w := NewWorker(cfg, rds, nil, nil, notif, logger)
	assert.NotNil(t, w)
	assert.Equal(t, cfg.StreamKey, w.cfg.StreamKey)
}

func TestWorker_RunCancellation(t *testing.T) {
	// Skip if Redis is not available.
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skip("Redis not available, skipping integration test")
	}

	streamKey := "test:postprocess:" + time.Now().Format("20060102150405")
	defer client.Del(context.Background(), streamKey)

	cfg := WorkerConfig{
		StreamKey:     streamKey,
		ConsumerGroup: "test-group",
		ConsumerName:  "worker-1",
		BatchSize:     10,
		BlockMs:       100,
	}

	rds := &store.RDS{Client: client}
	logger := slog.Default()

	w := NewWorker(cfg, rds, nil, nil, nil, logger)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Run(ctx)
	}()

	// Give the worker a moment to start.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not shut down in time")
	}
}

func TestWorker_ProcessMessage(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skip("Redis not available, skipping integration test")
	}

	streamKey := "test:postprocess:msg:" + time.Now().Format("20060102150405")
	defer client.Del(context.Background(), streamKey)

	cfg := WorkerConfig{
		StreamKey:     streamKey,
		ConsumerGroup: "test-group",
		ConsumerName:  "worker-1",
		BatchSize:     10,
		BlockMs:       100,
	}

	rds := &store.RDS{Client: client}
	notif := &mockNotifier{}
	logger := slog.Default()

	w := NewWorker(cfg, rds, nil, nil, notif, logger)

	// Publish a message.
	event := CallCompletionEvent{
		CallID:          99,
		Grade:           engine.GradeA,
		CollectedFields: map[string]string{"name": "Test"},
		Summary:         "pre-existing summary",
		ShouldNotify:    true,
		ContactName:     "TestUser",
		ContactPhone:    "13800000000",
	}
	data, err := json.Marshal(event)
	require.NoError(t, err)

	client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]any{"data": string(data)},
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Run(ctx)
	}()

	// Wait for message processing.
	time.Sleep(500 * time.Millisecond)
	cancel()

	<-errCh

	// Verify notification was sent.
	require.Len(t, notif.sent, 1)
	assert.Equal(t, int64(99), notif.sent[0].CallID)
	assert.Equal(t, "TestUser", notif.sent[0].ContactName)
}

// newTestWorker 创建不依赖真实 Redis 的 Worker，用于 processMessage 单元测试。
// ack 调用会因 Redis 不可达而记录错误日志，但不影响测试逻辑。
func newTestWorker() *Worker {
	client := redis.NewClient(&redis.Options{
		Addr:         "localhost:1", // 故意不可达，ack 失败只记日志
		MaxRetries:   0,
		DialTimeout:  time.Millisecond,
		ReadTimeout:  time.Millisecond,
		WriteTimeout: time.Millisecond,
	})

	return &Worker{
		cfg: WorkerConfig{
			StreamKey:     "test:stream",
			ConsumerGroup: "test-group",
			ConsumerName:  "worker-1",
		},
		rds:    &store.RDS{Client: client},
		logger: slog.Default(),
	}
}

func TestProcessMessage_MissingDataField(t *testing.T) {
	w := newTestWorker()

	// 消息缺少 data 字段时应跳过处理，不 panic。
	msg := redis.XMessage{
		ID:     "1-0",
		Values: map[string]any{"other": "value"},
	}
	// 不应 panic。
	w.processMessage(context.Background(), msg)
}

func TestProcessMessage_InvalidJSON(t *testing.T) {
	w := newTestWorker()

	// data 字段包含非法 JSON 时应跳过处理。
	msg := redis.XMessage{
		ID:     "2-0",
		Values: map[string]any{"data": "not-valid-json"},
	}
	w.processMessage(context.Background(), msg)
}

func TestProcessMessage_NonStringData(t *testing.T) {
	w := newTestWorker()

	// data 字段不是字符串时应跳过处理。
	msg := redis.XMessage{
		ID:     "3-0",
		Values: map[string]any{"data": 12345},
	}
	w.processMessage(context.Background(), msg)
}

func TestProcessMessage_WithSummarizer(t *testing.T) {
	w := newTestWorker()

	w.summarizer = NewSummarizer(&mockLLM{
		generateFn: func(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
			return "AI生成的摘要", nil
		},
	}, slog.Default())

	event := CallCompletionEvent{
		CallID: 10,
		Grade:  engine.GradeA,
		Turns: []dialogue.Turn{
			{Number: 1, Speaker: "bot", Content: "你好"},
		},
	}
	data, err := json.Marshal(event)
	require.NoError(t, err)

	msg := redis.XMessage{
		ID:     "4-0",
		Values: map[string]any{"data": string(data)},
	}
	w.processMessage(context.Background(), msg)
}

func TestProcessMessage_WithWriter(t *testing.T) {
	w := newTestWorker()

	var writeCalled bool
	pool := &mockPool{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			writeCalled = true
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
		sendBatchFn: func(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
			return &mockBatchResults{}
		},
	}
	w.writer = NewWriter(pool, slog.Default())

	event := CallCompletionEvent{
		CallID:          20,
		Grade:           engine.GradeB,
		CollectedFields: map[string]string{"k": "v"},
		Turns: []dialogue.Turn{
			{Number: 1, Speaker: "bot", Content: "hello", StateBefore: engine.DialogueOpening, StateAfter: engine.DialogueOpening},
		},
		Events: []engine.RecordedEvent{
			{EventType: engine.EventBargeIn, TimestampMs: 1000},
		},
	}
	data, err := json.Marshal(event)
	require.NoError(t, err)

	msg := redis.XMessage{
		ID:     "5-0",
		Values: map[string]any{"data": string(data)},
	}
	w.processMessage(context.Background(), msg)

	assert.True(t, writeCalled, "Writer.WriteCallResult should have been called")
}

func TestProcessMessage_WithNotification(t *testing.T) {
	w := newTestWorker()

	notif := &mockNotifier{}
	w.notifier = notif

	event := CallCompletionEvent{
		CallID:       30,
		Grade:        engine.GradeA,
		Summary:      "已有摘要",
		ShouldNotify: true,
		ContactName:  "张三",
		ContactPhone: "13800000000",
		NextAction:   "回访",
	}
	data, err := json.Marshal(event)
	require.NoError(t, err)

	msg := redis.XMessage{
		ID:     "6-0",
		Values: map[string]any{"data": string(data)},
	}
	w.processMessage(context.Background(), msg)

	require.Len(t, notif.sent, 1)
	assert.Equal(t, int64(30), notif.sent[0].CallID)
	assert.Equal(t, "张三", notif.sent[0].ContactName)
	assert.Equal(t, "回访", notif.sent[0].NextAction)
}

func TestProcessMessage_NotificationNotSentWhenFlagFalse(t *testing.T) {
	w := newTestWorker()

	notif := &mockNotifier{}
	w.notifier = notif

	event := CallCompletionEvent{
		CallID:       31,
		ShouldNotify: false,
	}
	data, err := json.Marshal(event)
	require.NoError(t, err)

	msg := redis.XMessage{
		ID:     "7-0",
		Values: map[string]any{"data": string(data)},
	}
	w.processMessage(context.Background(), msg)

	assert.Empty(t, notif.sent, "不应发送通知")
}

func TestProcessMessage_NotificationError(t *testing.T) {
	w := newTestWorker()

	notif := &mockNotifier{
		errFn: func() error { return errors.New("webhook failed") },
	}
	w.notifier = notif

	event := CallCompletionEvent{
		CallID:       32,
		ShouldNotify: true,
		Summary:      "some summary",
	}
	data, err := json.Marshal(event)
	require.NoError(t, err)

	msg := redis.XMessage{
		ID:     "8-0",
		Values: map[string]any{"data": string(data)},
	}

	// 通知失败不应 panic，只记录日志。
	w.processMessage(context.Background(), msg)
}

func TestProcessMessage_SummarizerError(t *testing.T) {
	w := newTestWorker()

	w.summarizer = NewSummarizer(&mockLLM{
		generateFn: func(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
			return "", errors.New("llm timeout")
		},
	}, slog.Default())

	// Summary 为空时触发 Summarizer，Summarizer 内部 fallback 不返回 error，
	// 但这里测试的是 GenerateSummary 的调用路径覆盖。
	event := CallCompletionEvent{
		CallID: 50,
		Turns: []dialogue.Turn{
			{Number: 1, Speaker: "bot", Content: "你好"},
		},
	}
	data, err := json.Marshal(event)
	require.NoError(t, err)

	msg := redis.XMessage{
		ID:     "10-0",
		Values: map[string]any{"data": string(data)},
	}
	w.processMessage(context.Background(), msg)
}

func TestProcessMessage_WithExtractor(t *testing.T) {
	w := newTestWorker()

	w.extractor = NewOpportunityExtractor(nil, slog.Default())

	var oppWritten bool
	pool := &mockPool{
		execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "opportunities") {
				oppWritten = true
			}
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
		sendBatchFn: func(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
			return &mockBatchResults{}
		},
	}
	w.writer = NewWriter(pool, slog.Default())

	event := CallCompletionEvent{
		CallID:          70,
		ContactID:       700,
		TaskID:          7000,
		Grade:           engine.GradeA,
		CollectedFields: map[string]string{"name": "Test"},
		Turns: []dialogue.Turn{
			{Number: 1, Speaker: "bot", Content: "你好", StateBefore: engine.DialogueOpening, StateAfter: engine.DialogueOpening},
		},
	}
	data, err := json.Marshal(event)
	require.NoError(t, err)

	msg := redis.XMessage{
		ID:     "12-0",
		Values: map[string]any{"data": string(data)},
	}
	w.processMessage(context.Background(), msg)

	assert.True(t, oppWritten, "商机应已写入数据库")
}

func TestProcessMessage_WriterErrors(t *testing.T) {
	w := newTestWorker()

	// Writer 各方法返回错误时不应 panic，只记录日志。
	pool := &mockPool{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("db down")
		},
		sendBatchFn: func(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
			return &mockBatchResults{
				execFn: func() (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("batch error")
				},
			}
		},
	}
	w.writer = NewWriter(pool, slog.Default())

	event := CallCompletionEvent{
		CallID:          60,
		CollectedFields: map[string]string{"k": "v"},
		Turns: []dialogue.Turn{
			{Number: 1, Speaker: "bot", Content: "hi", StateBefore: engine.DialogueOpening, StateAfter: engine.DialogueOpening},
		},
		Events: []engine.RecordedEvent{
			{EventType: engine.EventBargeIn, TimestampMs: 1000},
		},
	}
	data, err := json.Marshal(event)
	require.NoError(t, err)

	msg := redis.XMessage{
		ID:     "11-0",
		Values: map[string]any{"data": string(data)},
	}

	// 所有 writer 错误只记日志，不 panic。
	w.processMessage(context.Background(), msg)
}

func TestProcessMessage_ExistingSummarySkipsSummarizer(t *testing.T) {
	w := newTestWorker()

	var called bool
	w.summarizer = NewSummarizer(&mockLLM{
		generateFn: func(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
			called = true
			return "should not be called", nil
		},
	}, slog.Default())

	event := CallCompletionEvent{
		CallID:  40,
		Summary: "已有摘要不应被覆盖",
	}
	data, err := json.Marshal(event)
	require.NoError(t, err)

	msg := redis.XMessage{
		ID:     "9-0",
		Values: map[string]any{"data": string(data)},
	}
	w.processMessage(context.Background(), msg)

	assert.False(t, called, "已有摘要时不应调用 Summarizer")
}
