package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewLogger 验证日志级别解析正确。
func TestNewLogger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		level string
		want  slog.Level
	}{
		{name: "debug", level: "debug", want: slog.LevelDebug},
		{name: "info", level: "info", want: slog.LevelInfo},
		{name: "warn", level: "warn", want: slog.LevelWarn},
		{name: "error", level: "error", want: slog.LevelError},
		{name: "无效级别使用默认INFO", level: "invalid", want: slog.LevelInfo},
		{name: "空字符串使用默认INFO", level: "", want: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger := newLogger(tt.level)
			require.NotNil(t, logger)
			assert.True(t, logger.Enabled(context.Background(), tt.want))
		})
	}
}

// TestRunServe_InvalidConfig 验证无效配置路径返回错误。
func TestRunServe_InvalidConfig(t *testing.T) {
	err := runServe("/nonexistent/path/config.toml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load config")
}

// TestRunSimulate_InvalidConfig 验证 simulate 使用无效配置时返回错误。
func TestRunSimulate_InvalidConfig(t *testing.T) {
	err := runSimulate("/nonexistent/path/config.toml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load config")
}
