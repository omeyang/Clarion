// Package resilience 提供韧性模式：熔断、重试、降级。
package resilience

import (
	"errors"
	"sync"
	"time"
)

// BreakerState 是熔断器的当前状态。
type BreakerState int

// 熔断器状态常量。
const (
	StateClosed   BreakerState = iota // 正常通行。
	StateOpen                         // 断路，拒绝所有请求。
	StateHalfOpen                     // 探测中，允许一个请求通过。
)

func (s BreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// ErrBreakerOpen 表示熔断器处于打开状态，请求被拒绝。
var ErrBreakerOpen = errors.New("circuit breaker is open")

// BreakerConfig 配置熔断器参数。
type BreakerConfig struct {
	// FailureThreshold 连续失败次数触发断路（默认 5）。
	FailureThreshold int
	// ResetTimeout 断路后等待多久进入半开状态（默认 30s）。
	ResetTimeout time.Duration
	// HalfOpenMaxCalls 半开状态允许的最大探测次数（默认 1）。
	HalfOpenMaxCalls int
}

// Breaker 是一个线程安全的熔断器。
type Breaker struct {
	cfg           BreakerConfig
	mu            sync.Mutex
	state         BreakerState
	failures      int       // 连续失败计数。
	successes     int       // 半开状态的连续成功计数。
	lastFailure   time.Time // 最近一次失败的时间。
	halfOpenCalls int       // 半开状态已通过的请求数。
}

// NewBreaker 创建新的熔断器。
func NewBreaker(cfg BreakerConfig) *Breaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.ResetTimeout <= 0 {
		cfg.ResetTimeout = 30 * time.Second
	}
	if cfg.HalfOpenMaxCalls <= 0 {
		cfg.HalfOpenMaxCalls = 1
	}
	return &Breaker{cfg: cfg, state: StateClosed}
}

// Allow 检查是否允许请求通过。
// 返回 true 表示允许，false 表示熔断器打开。
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(b.lastFailure) >= b.cfg.ResetTimeout {
			b.state = StateHalfOpen
			b.halfOpenCalls = 1 // 当前请求算作第一个半开调用。
			b.successes = 0
			return true
		}
		return false
	case StateHalfOpen:
		if b.halfOpenCalls < b.cfg.HalfOpenMaxCalls {
			b.halfOpenCalls++
			return true
		}
		return false
	default:
		return false
	}
}

// RecordSuccess 记录一次成功调用。
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		b.failures = 0
	case StateOpen:
		// Open 状态不应有成功调用，忽略。
	case StateHalfOpen:
		b.successes++
		if b.successes >= b.cfg.HalfOpenMaxCalls {
			b.state = StateClosed
			b.failures = 0
			b.successes = 0
		}
	}
}

// RecordFailure 记录一次失败调用。
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lastFailure = time.Now()

	switch b.state {
	case StateClosed:
		b.failures++
		if b.failures >= b.cfg.FailureThreshold {
			b.state = StateOpen
		}
	case StateOpen:
		// Open 状态不应有失败调用，忽略。
	case StateHalfOpen:
		b.state = StateOpen
		b.failures = 0
		b.successes = 0
	}
}

// State 返回当前状态。
func (b *Breaker) State() BreakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Reset 重置熔断器到关闭状态。
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = StateClosed
	b.failures = 0
	b.successes = 0
}
