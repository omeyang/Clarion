package resilience

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 测试用的短超时，避免测试耗时过长。
const testResetTimeout = 10 * time.Millisecond

func newTestBreaker(threshold int) *Breaker {
	return NewBreaker(BreakerConfig{
		FailureThreshold: threshold,
		ResetTimeout:     testResetTimeout,
		HalfOpenMaxCalls: 1,
	})
}

func TestBreaker_InitialState(t *testing.T) {
	t.Parallel()
	// 新创建的熔断器应处于关闭状态。
	b := newTestBreaker(3)
	assert.Equal(t, StateClosed, b.State())
}

func TestBreaker_AllowWhenClosed(t *testing.T) {
	t.Parallel()
	// 关闭状态下所有请求都应放行。
	b := newTestBreaker(3)
	for range 10 {
		assert.True(t, b.Allow())
	}
}

func TestBreaker_TripsToOpenAfterThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		threshold int
	}{
		{name: "阈值为1", threshold: 1},
		{name: "阈值为3", threshold: 3},
		{name: "阈值为5", threshold: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newTestBreaker(tc.threshold)

			// 连续失败直到达到阈值。
			for range tc.threshold {
				require.True(t, b.Allow())
				b.RecordFailure()
			}

			assert.Equal(t, StateOpen, b.State(), "达到失败阈值后应转为开路状态")
		})
	}
}

func TestBreaker_AllowReturnsFalseWhenOpen(t *testing.T) {
	t.Parallel()
	// 开路状态下请求应被拒绝。
	b := newTestBreaker(1)
	b.Allow()
	b.RecordFailure()

	require.Equal(t, StateOpen, b.State())
	assert.False(t, b.Allow())
}

func TestBreaker_TransitionsToHalfOpenAfterTimeout(t *testing.T) {
	t.Parallel()
	// 超时后应从开路过渡到半开状态。
	b := newTestBreaker(1)
	b.Allow()
	b.RecordFailure()
	require.Equal(t, StateOpen, b.State())

	// 等待超时过期。
	time.Sleep(testResetTimeout + 5*time.Millisecond)

	// 下一次 Allow 应允许通过并进入半开状态。
	assert.True(t, b.Allow())
	assert.Equal(t, StateHalfOpen, b.State())
}

func TestBreaker_SuccessInHalfOpenTransitionsToClosed(t *testing.T) {
	t.Parallel()
	// 半开状态下成功应恢复到关闭状态。
	b := newTestBreaker(1)
	b.Allow()
	b.RecordFailure()

	time.Sleep(testResetTimeout + 5*time.Millisecond)

	require.True(t, b.Allow())
	require.Equal(t, StateHalfOpen, b.State())

	b.RecordSuccess()
	assert.Equal(t, StateClosed, b.State(), "半开状态成功后应恢复关闭")
}

func TestBreaker_FailureInHalfOpenTransitionsToOpen(t *testing.T) {
	t.Parallel()
	// 半开状态下失败应重新打开熔断器。
	b := newTestBreaker(1)
	b.Allow()
	b.RecordFailure()

	time.Sleep(testResetTimeout + 5*time.Millisecond)

	require.True(t, b.Allow())
	require.Equal(t, StateHalfOpen, b.State())

	b.RecordFailure()
	assert.Equal(t, StateOpen, b.State(), "半开状态失败后应重新开路")
}

func TestBreaker_Reset(t *testing.T) {
	t.Parallel()
	// Reset 应将熔断器恢复到初始关闭状态。
	b := newTestBreaker(1)
	b.Allow()
	b.RecordFailure()
	require.Equal(t, StateOpen, b.State())

	b.Reset()
	assert.Equal(t, StateClosed, b.State())
	assert.True(t, b.Allow(), "重置后应允许请求通过")
}

func TestBreakerState_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state BreakerState
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half_open"},
		{BreakerState(99), "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.state.String())
		})
	}
}

func TestBreaker_DefaultConfig(t *testing.T) {
	t.Parallel()
	// 零值配置应使用默认值。
	b := NewBreaker(BreakerConfig{})

	assert.Equal(t, StateClosed, b.State())

	// 默认阈值为 5：4 次失败不应触发断路。
	for range 4 {
		b.Allow()
		b.RecordFailure()
	}
	assert.Equal(t, StateClosed, b.State(), "默认阈值 5，4 次失败不应断路")

	// 第 5 次失败触发断路。
	b.Allow()
	b.RecordFailure()
	assert.Equal(t, StateOpen, b.State(), "默认阈值 5，第 5 次失败应断路")
}

func TestBreaker_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	// 并发访问不应触发竞态条件。
	b := newTestBreaker(100)

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 100

	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				if b.Allow() {
					// 模拟随机成功/失败。
					b.RecordSuccess()
				}
				b.RecordFailure()
				_ = b.State()
			}
		}()
	}

	wg.Wait()

	// 只要没有 panic 或竞态错误就通过。
	// 最终状态取决于时序，此处只验证安全性。
	state := b.State()
	assert.Contains(t, []BreakerState{StateClosed, StateOpen, StateHalfOpen}, state)
}

func TestBreaker_SuccessResetFailureCount(t *testing.T) {
	t.Parallel()
	// 关闭状态下成功应重置失败计数。
	b := newTestBreaker(3)

	// 连续失败 2 次（未达阈值）。
	b.Allow()
	b.RecordFailure()
	b.Allow()
	b.RecordFailure()

	// 一次成功应重置计数。
	b.Allow()
	b.RecordSuccess()

	// 再失败 2 次不应断路（因为计数已重置）。
	b.Allow()
	b.RecordFailure()
	b.Allow()
	b.RecordFailure()

	assert.Equal(t, StateClosed, b.State(), "成功后失败计数应重置")
}

func TestBreaker_HalfOpenRejectsExtraCalls(t *testing.T) {
	t.Parallel()
	// 半开状态下超过最大探测数的请求应被拒绝。
	b := newTestBreaker(1)
	b.Allow()
	b.RecordFailure()

	time.Sleep(testResetTimeout + 5*time.Millisecond)

	// 第一次调用允许通过。
	require.True(t, b.Allow())
	// 后续调用应被拒绝（HalfOpenMaxCalls=1）。
	assert.False(t, b.Allow(), "半开状态超过探测上限应拒绝")
}
