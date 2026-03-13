package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/omeyang/clarion/internal/config"
)

// fixedNow 返回一个固定时间生成函数，用于测试。
// 默认：2026-03-12 周四 10:00 CST。
func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func workdayMorning() time.Time {
	return time.Date(2026, 3, 12, 10, 0, 0, 0, time.Local) // 周四
}

func TestEvaluate_CompletedNoRetry(t *testing.T) {
	ev := NewRetryEvaluator(DefaultRetryConfig())
	ev.nowFunc = fixedNow(workdayMorning())

	d := ev.Evaluate(ResultCompleted, 1)
	assert.False(t, d.ShouldRetry)
	assert.Equal(t, "已达最大重试次数", d.Reason)
}

func TestEvaluate_RejectedNoRetry(t *testing.T) {
	ev := NewRetryEvaluator(DefaultRetryConfig())
	ev.nowFunc = fixedNow(workdayMorning())

	d := ev.Evaluate(ResultRejected, 1)
	assert.False(t, d.ShouldRetry)
}

func TestEvaluate_UnknownResult(t *testing.T) {
	ev := NewRetryEvaluator(DefaultRetryConfig())
	ev.nowFunc = fixedNow(workdayMorning())

	d := ev.Evaluate(CallResult("alien"), 1)
	assert.False(t, d.ShouldRetry)
	assert.Contains(t, d.Reason, "未知呼叫结果")
}

func TestEvaluate_NoAnswerExponentialBackoff(t *testing.T) {
	ev := NewRetryEvaluator(DefaultRetryConfig())
	ev.nowFunc = fixedNow(workdayMorning())

	tests := []struct {
		attemptNo int
		wantRetry bool
		minDelay  time.Duration
	}{
		{1, true, 2 * time.Hour}, // 30min * 2^0 = 30min，但 minInterval=2h。
		{2, true, 2 * time.Hour}, // 30min * 2^1 = 1h，但 minInterval=2h。
		{3, true, 2 * time.Hour}, // 30min * 2^2 = 2h。
		{4, false, 0},            // 已达 maxRetries(3)+1。
	}

	for _, tt := range tests {
		d := ev.Evaluate(ResultNoAnswer, tt.attemptNo)
		assert.Equal(t, tt.wantRetry, d.ShouldRetry, "attemptNo=%d", tt.attemptNo)
		if tt.wantRetry {
			assert.GreaterOrEqual(t, d.Delay, tt.minDelay, "attemptNo=%d", tt.attemptNo)
		}
	}
}

func TestEvaluate_BusyBackoff(t *testing.T) {
	ev := NewRetryEvaluator(DefaultRetryConfig())
	ev.nowFunc = fixedNow(workdayMorning())

	d := ev.Evaluate(ResultBusy, 1)
	assert.True(t, d.ShouldRetry)
	// 15min * 2^0 = 15min，但 minInterval=2h，所以至少 2h。
	assert.GreaterOrEqual(t, d.Delay, 2*time.Hour)

	d = ev.Evaluate(ResultBusy, 3)
	assert.False(t, d.ShouldRetry) // maxRetries=2，第 3 次已超限。
}

func TestEvaluate_FailedFixedInterval(t *testing.T) {
	ev := NewRetryEvaluator(DefaultRetryConfig())
	ev.nowFunc = fixedNow(workdayMorning())

	d := ev.Evaluate(ResultFailed, 1)
	assert.True(t, d.ShouldRetry)
	assert.GreaterOrEqual(t, d.Delay, 2*time.Hour) // minInterval=2h 兜底。
}

func TestEvaluate_InterruptedQuickRetry(t *testing.T) {
	// 中断重试间隔只有 30s，但 minInterval=2h 会兜底。
	ev := NewRetryEvaluator(DefaultRetryConfig())
	ev.nowFunc = fixedNow(workdayMorning())

	d := ev.Evaluate(ResultInterrupted, 1)
	assert.True(t, d.ShouldRetry)
	assert.GreaterOrEqual(t, d.Delay, 2*time.Hour)
}

func TestEvaluate_InterruptedWithLowMinInterval(t *testing.T) {
	// 放宽 minInterval 后，中断重试应接近 30s。
	cfg := DefaultRetryConfig()
	cfg.MinInterval = 10 * time.Second
	ev := NewRetryEvaluator(cfg)
	ev.nowFunc = fixedNow(workdayMorning())

	d := ev.Evaluate(ResultInterrupted, 1)
	assert.True(t, d.ShouldRetry)
	assert.GreaterOrEqual(t, d.Delay, 30*time.Second)
	assert.Less(t, d.Delay, 5*time.Minute)
}

func TestEvaluate_WeekendPushToMonday(t *testing.T) {
	// 周六 10:00 应该推迟到周一 9:00。
	saturday := time.Date(2026, 3, 14, 10, 0, 0, 0, time.Local)
	ev := NewRetryEvaluator(DefaultRetryConfig())
	ev.nowFunc = fixedNow(saturday)

	d := ev.Evaluate(ResultFailed, 1)
	assert.True(t, d.ShouldRetry)

	nextRun := saturday.Add(d.Delay)
	assert.Equal(t, time.Monday, nextRun.Weekday())
	assert.Equal(t, 9, nextRun.Hour())
}

func TestEvaluate_LateNightPushToNextDay(t *testing.T) {
	// 周四 21:00（已过工作时间）应推迟到周五 9:00。
	lateNight := time.Date(2026, 3, 12, 21, 0, 0, 0, time.Local)
	ev := NewRetryEvaluator(DefaultRetryConfig())
	ev.nowFunc = fixedNow(lateNight)

	d := ev.Evaluate(ResultFailed, 1)
	assert.True(t, d.ShouldRetry)

	nextRun := lateNight.Add(d.Delay)
	assert.Equal(t, time.Friday, nextRun.Weekday())
	assert.Equal(t, 9, nextRun.Hour())
}

func TestEvaluate_EarlyMorningPushToStartHour(t *testing.T) {
	// 周四 06:00（工作时间前）应推迟到当天 9:00。
	early := time.Date(2026, 3, 12, 6, 0, 0, 0, time.Local)
	cfg := DefaultRetryConfig()
	cfg.MinInterval = 10 * time.Second // 放宽间隔以测试时间窗口。
	ev := NewRetryEvaluator(cfg)
	ev.nowFunc = fixedNow(early)

	d := ev.Evaluate(ResultInterrupted, 1)
	assert.True(t, d.ShouldRetry)

	nextRun := early.Add(d.Delay)
	assert.Equal(t, 12, nextRun.Day())
	assert.Equal(t, 9, nextRun.Hour())
}

func TestEvaluate_WeekdaysOnlyDisabled(t *testing.T) {
	saturday := time.Date(2026, 3, 14, 10, 0, 0, 0, time.Local)
	cfg := DefaultRetryConfig()
	cfg.WeekdaysOnly = false
	cfg.MinInterval = 10 * time.Second
	ev := NewRetryEvaluator(cfg)
	ev.nowFunc = fixedNow(saturday)

	d := ev.Evaluate(ResultInterrupted, 1)
	assert.True(t, d.ShouldRetry)

	nextRun := saturday.Add(d.Delay)
	// 周末不受限，应在同一天。
	assert.Equal(t, time.Saturday, nextRun.Weekday())
}

func TestComputeDelay_ExponentialBackoff(t *testing.T) {
	p := RetryPolicy{InitialInterval: 30 * time.Minute, Backoff: BackoffExponential}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 30 * time.Minute},  // 30m * 2^0
		{2, 60 * time.Minute},  // 30m * 2^1
		{3, 120 * time.Minute}, // 30m * 2^2
	}
	for _, tt := range tests {
		got := computeDelay(p, tt.attempt)
		assert.Equal(t, tt.want, got, "attempt=%d", tt.attempt)
	}
}

func TestComputeDelay_FixedBackoff(t *testing.T) {
	p := RetryPolicy{InitialInterval: 5 * time.Minute, Backoff: BackoffFixed}

	for _, attempt := range []int{1, 2, 3} {
		got := computeDelay(p, attempt)
		assert.Equal(t, 5*time.Minute, got, "attempt=%d", attempt)
	}
}

func TestComputeDelay_NoneBackoff(t *testing.T) {
	p := RetryPolicy{InitialInterval: 5 * time.Minute, Backoff: BackoffNone}
	assert.Equal(t, time.Duration(0), computeDelay(p, 1))
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	assert.Equal(t, 9, cfg.StartHour)
	assert.Equal(t, 20, cfg.EndHour)
	assert.True(t, cfg.WeekdaysOnly)
	assert.Equal(t, 2*time.Hour, cfg.MinInterval)
}

func TestRetryConfigFrom(t *testing.T) {
	cfgRetry := config.Retry{
		StartHour:      10,
		EndHour:        18,
		WeekdaysOnly:   false,
		MinIntervalMin: 60,
	}
	rc := RetryConfigFrom(cfgRetry)
	assert.Equal(t, 10, rc.StartHour)
	assert.Equal(t, 18, rc.EndHour)
	assert.False(t, rc.WeekdaysOnly)
	assert.Equal(t, 60*time.Minute, rc.MinInterval)
}
