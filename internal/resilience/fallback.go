package resilience

import (
	"context"
	"fmt"
)

// Fallback 按顺序尝试执行函数列表，返回第一个成功结果。
// 所有函数都失败时返回最后一个错误。
func Fallback[T any](ctx context.Context, fns ...func(context.Context) (T, error)) (T, error) {
	var zero T
	var lastErr error
	for _, fn := range fns {
		if ctx.Err() != nil {
			return zero, fmt.Errorf("fallback: %w", ctx.Err())
		}
		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return zero, lastErr
}
