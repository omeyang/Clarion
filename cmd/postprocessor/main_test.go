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

// TestRun_InvalidConfig 验证无效配置路径返回错误。
func TestRun_InvalidConfig(t *testing.T) {
	err := run("/nonexistent/path/config.toml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load config")
}
