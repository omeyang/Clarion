package resilience

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alwaysRetry 始终允许重试。
func alwaysRetry(_ error) bool { return true }

func TestRetry_首次成功无重试(t *testing.T) {
	t.Parallel()

	calls := 0
	err := Retry(context.Background(), RetryConfig{MaxAttempts: 3}, alwaysRetry, func() error {
		calls++
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, calls, "应当只调用一次")
}

func TestRetry_第二次成功重试一次(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	calls := 0
	errTemp := errors.New("临时错误")

	err := Retry(context.Background(), cfg, alwaysRetry, func() error {
		calls++
		if calls < 2 {
			return errTemp
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 2, calls, "应当调用两次")
}

func TestRetry_全部失败返回最后错误(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	calls := 0

	err := Retry(context.Background(), cfg, alwaysRetry, func() error {
		calls++
		return errors.New("失败")
	})

	require.Error(t, err)
	assert.Equal(t, 3, calls, "应当尝试三次")
	assert.EqualError(t, err, "失败")
}

func TestRetry_shouldRetry返回false不重试(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{MaxAttempts: 5, BaseDelay: 1 * time.Millisecond}
	calls := 0
	permanent := errors.New("永久错误")

	// shouldRetry 始终返回 false，不允许重试
	err := Retry(context.Background(), cfg, func(_ error) bool { return false }, func() error {
		calls++
		return permanent
	})

	require.ErrorIs(t, err, permanent)
	assert.Equal(t, 1, calls, "不应重试")
}

func TestRetry_上下文取消返回上下文错误(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cfg := RetryConfig{MaxAttempts: 10, BaseDelay: 50 * time.Millisecond, MaxDelay: 1 * time.Second}
	calls := 0

	// 第一次调用后取消上下文
	err := Retry(ctx, cfg, alwaysRetry, func() error {
		calls++
		if calls == 1 {
			cancel()
		}
		return errors.New("失败")
	})

	require.ErrorIs(t, err, context.Canceled)
}

func TestRetry_指数退避时间验证(t *testing.T) {
	t.Parallel()

	// 基础延迟 20ms，两次重试：第一次 20ms，第二次 40ms，总计约 60ms
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: 20 * time.Millisecond, MaxDelay: 1 * time.Second}

	start := time.Now()
	_ = Retry(context.Background(), cfg, alwaysRetry, func() error {
		return errors.New("失败")
	})
	elapsed := time.Since(start)

	// 两次退避合计至少 50ms（留一定余量）
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond, "指数退避耗时应不小于 50ms")
}

func TestRetry_MaxDelay限制延迟上限(t *testing.T) {
	t.Parallel()

	// 基础延迟 100ms，MaxDelay 10ms → 每次延迟被限制为 10ms
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: 100 * time.Millisecond, MaxDelay: 10 * time.Millisecond}

	start := time.Now()
	_ = Retry(context.Background(), cfg, alwaysRetry, func() error {
		return errors.New("失败")
	})
	elapsed := time.Since(start)

	// 两次延迟合计应接近 20ms 而非 300ms
	assert.Less(t, elapsed, 100*time.Millisecond, "MaxDelay 应限制延迟上限")
}
