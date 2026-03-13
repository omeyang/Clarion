package resilience

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFallback_首个函数成功直接返回(t *testing.T) {
	t.Parallel()

	result, err := Fallback(context.Background(),
		func(_ context.Context) (string, error) { return "第一", nil },
		func(_ context.Context) (string, error) { return "第二", nil },
	)

	require.NoError(t, err)
	assert.Equal(t, "第一", result)
}

func TestFallback_首个失败第二个成功(t *testing.T) {
	t.Parallel()

	result, err := Fallback(context.Background(),
		func(_ context.Context) (int, error) { return 0, errors.New("主路径失败") },
		func(_ context.Context) (int, error) { return 42, nil },
	)

	require.NoError(t, err)
	assert.Equal(t, 42, result)
}

func TestFallback_全部失败返回最后错误(t *testing.T) {
	t.Parallel()

	lastErr := errors.New("最终降级失败")
	result, err := Fallback(context.Background(),
		func(_ context.Context) (string, error) { return "", errors.New("第一失败") },
		func(_ context.Context) (string, error) { return "", errors.New("第二失败") },
		func(_ context.Context) (string, error) { return "", lastErr },
	)

	require.ErrorIs(t, err, lastErr)
	assert.Empty(t, result)
}

func TestFallback_上下文取消返回上下文错误(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	result, err := Fallback(ctx,
		func(_ context.Context) (string, error) { return "不应到达", nil },
	)

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, result)
}

func TestFallback_空函数列表返回零值(t *testing.T) {
	t.Parallel()

	result, err := Fallback[string](context.Background())

	require.NoError(t, err, "空列表不应返回错误")
	assert.Empty(t, result, "应返回零值")
}
