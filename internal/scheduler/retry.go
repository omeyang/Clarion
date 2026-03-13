package scheduler

import (
	"fmt"
	"time"

	"github.com/omeyang/clarion/internal/config"
)

// CallResult 表示呼叫结果，用于决定重试策略。
type CallResult string

const (
	// ResultNoAnswer 无人接听。
	ResultNoAnswer CallResult = "no_answer"
	// ResultBusy 占线。
	ResultBusy CallResult = "busy"
	// ResultFailed 呼叫失败（网络或系统错误）。
	ResultFailed CallResult = "failed"
	// ResultVoicemail 进入语音信箱。
	ResultVoicemail CallResult = "voicemail"
	// ResultCompleted 通话正常完成。
	ResultCompleted CallResult = "completed"
	// ResultInterrupted 通话意外中断。
	ResultInterrupted CallResult = "interrupted"
	// ResultPoorNetwork 网络质量差。
	ResultPoorNetwork CallResult = "poor_network"
	// ResultRejected 被拒接。
	ResultRejected CallResult = "rejected"
)

// BackoffKind 表示退避策略类型。
type BackoffKind int

const (
	// BackoffNone 不重试。
	BackoffNone BackoffKind = iota
	// BackoffFixed 固定间隔重试。
	BackoffFixed
	// BackoffExponential 指数退避重试。
	BackoffExponential
)

// RetryPolicy 定义单种呼叫结果的重试策略。
type RetryPolicy struct {
	// MaxRetries 最大重试次数。
	MaxRetries int
	// InitialInterval 首次重试间隔。
	InitialInterval time.Duration
	// Backoff 退避策略类型。
	Backoff BackoffKind
}

// defaultPolicies 内置的默认重试策略表。
var defaultPolicies = map[CallResult]RetryPolicy{
	ResultNoAnswer:    {MaxRetries: 3, InitialInterval: 30 * time.Minute, Backoff: BackoffExponential},
	ResultBusy:        {MaxRetries: 2, InitialInterval: 15 * time.Minute, Backoff: BackoffExponential},
	ResultFailed:      {MaxRetries: 1, InitialInterval: 5 * time.Minute, Backoff: BackoffFixed},
	ResultVoicemail:   {MaxRetries: 1, InitialInterval: 2 * time.Hour, Backoff: BackoffFixed},
	ResultCompleted:   {MaxRetries: 0},
	ResultInterrupted: {MaxRetries: 1, InitialInterval: 30 * time.Second, Backoff: BackoffFixed},
	ResultPoorNetwork: {MaxRetries: 1, InitialInterval: 2 * time.Hour, Backoff: BackoffFixed},
	ResultRejected:    {MaxRetries: 0},
}

// RetryConfig 重试策略的可配置参数。
type RetryConfig struct {
	// StartHour 允许重试的每日起始小时（含），默认 9。
	StartHour int
	// EndHour 允许重试的每日截止小时（不含），默认 20。
	EndHour int
	// WeekdaysOnly 是否仅在工作日重试，默认 true。
	WeekdaysOnly bool
	// MinInterval 同一号码两次呼叫的最小间隔（防骚扰），默认 2 小时。
	MinInterval time.Duration
}

// DefaultRetryConfig 返回默认的重试配置。
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		StartHour:    9,
		EndHour:      20,
		WeekdaysOnly: true,
		MinInterval:  2 * time.Hour,
	}
}

// RetryConfigFrom 从应用配置构建重试配置。
func RetryConfigFrom(cfg config.Retry) RetryConfig {
	return RetryConfig{
		StartHour:    cfg.StartHour,
		EndHour:      cfg.EndHour,
		WeekdaysOnly: cfg.WeekdaysOnly,
		MinInterval:  cfg.MinInterval(),
	}
}

// RetryDecision 是重试判定结果。
type RetryDecision struct {
	// ShouldRetry 是否应该重试。
	ShouldRetry bool
	// Delay 距现在的延迟时间。
	Delay time.Duration
	// Reason 不重试的原因（ShouldRetry=false 时有值）。
	Reason string
}

// RetryEvaluator 根据呼叫结果和已重试次数计算下一次重试。
type RetryEvaluator struct {
	policies map[CallResult]RetryPolicy
	cfg      RetryConfig
	nowFunc  func() time.Time // 用于测试注入时间。
}

// NewRetryEvaluator 创建重试评估器。
func NewRetryEvaluator(cfg RetryConfig) *RetryEvaluator {
	return &RetryEvaluator{
		policies: defaultPolicies,
		cfg:      cfg,
		nowFunc:  time.Now,
	}
}

// Evaluate 根据呼叫结果和当前尝试次数评估是否应该重试。
// attemptNo 从 1 开始，表示已完成的尝试次数。
func (e *RetryEvaluator) Evaluate(result CallResult, attemptNo int) RetryDecision {
	policy, ok := e.policies[result]
	if !ok {
		return RetryDecision{Reason: fmt.Sprintf("未知呼叫结果: %s", result)}
	}

	if policy.MaxRetries == 0 || attemptNo >= policy.MaxRetries+1 {
		return RetryDecision{Reason: "已达最大重试次数"}
	}

	delay := computeDelay(policy, attemptNo)
	now := e.nowFunc()
	nextRun := now.Add(delay)

	// 如果延迟小于最小间隔，提升到最小间隔。
	if delay < e.cfg.MinInterval {
		delay = e.cfg.MinInterval
		nextRun = now.Add(delay)
	}

	// 调整到工作时间窗口内。
	nextRun = e.adjustToWorkingHours(nextRun)
	delay = nextRun.Sub(now)

	return RetryDecision{ShouldRetry: true, Delay: delay}
}

// computeDelay 根据策略和尝试次数计算基础延迟。
func computeDelay(p RetryPolicy, attemptNo int) time.Duration {
	switch p.Backoff {
	case BackoffExponential:
		// 第 1 次尝试后重试间隔 = initial * 2^(attemptNo-1)。
		shift := max(attemptNo-1, 0)
		return p.InitialInterval * (1 << shift)
	case BackoffFixed:
		return p.InitialInterval
	default:
		return 0
	}
}

// adjustToWorkingHours 将时间调整到下一个工作时间窗口内。
// 如果 t 已在工作时间内，直接返回。
func (e *RetryEvaluator) adjustToWorkingHours(t time.Time) time.Time {
	const maxIterations = 14 // 最多跳过 14 天（两周），防止无限循环。
	for range maxIterations {
		if e.isWorkingTime(t) {
			return t
		}
		t = e.nextWorkingStart(t)
	}
	return t
}

// isWorkingTime 检查时间是否在工作时间窗口内。
func (e *RetryEvaluator) isWorkingTime(t time.Time) bool {
	if e.cfg.WeekdaysOnly {
		wd := t.Weekday()
		if wd == time.Saturday || wd == time.Sunday {
			return false
		}
	}
	hour := t.Hour()
	return hour >= e.cfg.StartHour && hour < e.cfg.EndHour
}

// nextWorkingStart 返回 t 之后最近的工作时间起始点。
func (e *RetryEvaluator) nextWorkingStart(t time.Time) time.Time {
	// 如果当天还没到开始时间，返回当天开始时间。
	todayStart := time.Date(t.Year(), t.Month(), t.Day(), e.cfg.StartHour, 0, 0, 0, t.Location())
	if t.Before(todayStart) && e.isWorkdayDate(todayStart) {
		return todayStart
	}
	// 否则跳到下一天开始时间。
	next := t.AddDate(0, 0, 1)
	return time.Date(next.Year(), next.Month(), next.Day(), e.cfg.StartHour, 0, 0, 0, t.Location())
}

// isWorkdayDate 检查日期是否是工作日。
func (e *RetryEvaluator) isWorkdayDate(t time.Time) bool {
	if !e.cfg.WeekdaysOnly {
		return true
	}
	wd := t.Weekday()
	return wd != time.Saturday && wd != time.Sunday
}
