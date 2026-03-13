package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spyEnqueuer 记录入队调用，用于测试。
type spyEnqueuer struct {
	calls []enqueueCall
	err   error // 模拟入队失败。
}

type enqueueCall struct {
	payload OutboundCallPayload
}

func (s *spyEnqueuer) EnqueueOutboundCall(_ context.Context, p OutboundCallPayload, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	s.calls = append(s.calls, enqueueCall{payload: p})
	if s.err != nil {
		return nil, s.err
	}
	return &asynq.TaskInfo{}, nil
}

// newTestScheduler 创建测试用重试调度器（固定在工作日上午 10:00）。
func newTestScheduler(spy *spyEnqueuer) *RetryScheduler {
	ev := NewRetryEvaluator(DefaultRetryConfig())
	ev.nowFunc = fixedNow(workdayMorning())
	return NewRetryScheduler(ev, spy, slog.Default())
}

func TestScheduleRetry_CompletedNoEnqueue(t *testing.T) {
	spy := &spyEnqueuer{}
	s := newTestScheduler(spy)

	d, err := s.ScheduleRetry(context.Background(), ResultCompleted, 1, OutboundCallPayload{CallID: 1})
	require.NoError(t, err)
	assert.False(t, d.ShouldRetry)
	assert.Empty(t, spy.calls)
}

func TestScheduleRetry_RejectedNoEnqueue(t *testing.T) {
	spy := &spyEnqueuer{}
	s := newTestScheduler(spy)

	d, err := s.ScheduleRetry(context.Background(), ResultRejected, 1, OutboundCallPayload{CallID: 2})
	require.NoError(t, err)
	assert.False(t, d.ShouldRetry)
	assert.Empty(t, spy.calls)
}

func TestScheduleRetry_NoAnswerEnqueues(t *testing.T) {
	spy := &spyEnqueuer{}
	s := newTestScheduler(spy)

	payload := OutboundCallPayload{CallID: 42, ContactID: 100, Phone: "13800138000", AttemptNo: 1}
	d, err := s.ScheduleRetry(context.Background(), ResultNoAnswer, 1, payload)
	require.NoError(t, err)
	assert.True(t, d.ShouldRetry)
	require.Len(t, spy.calls, 1)
	assert.Equal(t, int64(42), spy.calls[0].payload.CallID)
	assert.Equal(t, int64(100), spy.calls[0].payload.ContactID)
	// 验证尝试次数递增。
	assert.Equal(t, 2, spy.calls[0].payload.AttemptNo)
}

func TestScheduleRetry_ExceedsMaxRetriesNoEnqueue(t *testing.T) {
	spy := &spyEnqueuer{}
	s := newTestScheduler(spy)

	// no_answer 最多重试 3 次，第 4 次尝试不应入队。
	d, err := s.ScheduleRetry(context.Background(), ResultNoAnswer, 4, OutboundCallPayload{CallID: 10})
	require.NoError(t, err)
	assert.False(t, d.ShouldRetry)
	assert.Empty(t, spy.calls)
}

func TestScheduleRetry_BusyEnqueues(t *testing.T) {
	spy := &spyEnqueuer{}
	s := newTestScheduler(spy)

	d, err := s.ScheduleRetry(context.Background(), ResultBusy, 1, OutboundCallPayload{CallID: 5})
	require.NoError(t, err)
	assert.True(t, d.ShouldRetry)
	require.Len(t, spy.calls, 1)
}

func TestScheduleRetry_FailedEnqueues(t *testing.T) {
	spy := &spyEnqueuer{}
	s := newTestScheduler(spy)

	d, err := s.ScheduleRetry(context.Background(), ResultFailed, 1, OutboundCallPayload{CallID: 7})
	require.NoError(t, err)
	assert.True(t, d.ShouldRetry)
	require.Len(t, spy.calls, 1)
}

func TestScheduleRetry_EnqueueError(t *testing.T) {
	spy := &spyEnqueuer{err: errors.New("redis unavailable")}
	s := newTestScheduler(spy)

	d, err := s.ScheduleRetry(context.Background(), ResultNoAnswer, 1, OutboundCallPayload{CallID: 99})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "入队重试任务")
	assert.Contains(t, err.Error(), "redis unavailable")
	// 决策本身仍然是重试，只是入队失败。
	assert.True(t, d.ShouldRetry)
}

func TestScheduleRetry_MultipleResults(t *testing.T) {
	tests := []struct {
		name      string
		result    CallResult
		attempt   int
		wantRetry bool
	}{
		{"voicemail_first", ResultVoicemail, 1, true},
		{"voicemail_exceeded", ResultVoicemail, 2, false},
		{"interrupted_first", ResultInterrupted, 1, true},
		{"interrupted_exceeded", ResultInterrupted, 2, false},
		{"poor_network_first", ResultPoorNetwork, 1, true},
		{"poor_network_exceeded", ResultPoorNetwork, 2, false},
		{"unknown_result", CallResult("unknown"), 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &spyEnqueuer{}
			s := newTestScheduler(spy)

			d, err := s.ScheduleRetry(context.Background(), tt.result, tt.attempt, OutboundCallPayload{CallID: 1})
			require.NoError(t, err)
			assert.Equal(t, tt.wantRetry, d.ShouldRetry)

			if tt.wantRetry {
				assert.Len(t, spy.calls, 1)
			} else {
				assert.Empty(t, spy.calls)
			}
		})
	}
}
