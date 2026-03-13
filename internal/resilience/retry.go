package resilience

import (
	"context"
	"fmt"
	"math"
	"time"
)

// RetryConfig 配置重试策略。
type RetryConfig struct {
	MaxAttempts int           // 最大尝试次数（包含首次，默认 3）。
	BaseDelay   time.Duration // 首次重试延迟（默认 100ms）。
	MaxDelay    time.Duration // 最大延迟上限（默认 5s）。
}

// Retry 使用指数退避策略执行 fn，直到成功或耗尽重试次数。
// 仅在 shouldRetry 返回 true 时重试。
func Retry(ctx context.Context, cfg RetryConfig, shouldRetry func(error) bool, fn func() error) error {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 100 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 5 * time.Second
	}

	var lastErr error
	for attempt := range cfg.MaxAttempts {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !shouldRetry(lastErr) {
			return lastErr
		}
		if attempt == cfg.MaxAttempts-1 {
			break
		}

		delay := time.Duration(float64(cfg.BaseDelay) * math.Pow(2, float64(attempt)))
		delay = min(delay, cfg.MaxDelay)
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry: %w", ctx.Err())
		case <-time.After(delay):
		}
	}
	return lastErr
}
