package llm

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// testDiscardLogger 返回静默日志记录器，用于测试。
func testDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// FuzzParseSSEChunk 模糊测试 SSE 数据块的 JSON 反序列化，确保任意输入不会 panic。
func FuzzParseSSEChunk(f *testing.F) {
	// 种子语料：典型 SSE chunk JSON。
	f.Add(`{"choices":[{"delta":{"content":"你好"}}]}`)
	f.Add(`{"choices":[]}`)
	f.Add(`{}`)
	f.Add(`{"choices":[{"delta":{"content":""}}]}`)
	f.Add(`not json at all`)
	f.Add(`{"choices":[{"delta":{"content":"hello"}},{"delta":{"content":"world"}}]}`)

	f.Fuzz(func(t *testing.T, data string) {
		// 直接测试 sseChunk 反序列化，与 emitSSEToken 的核心逻辑一致。
		var chunk sseChunk
		_ = json.Unmarshal([]byte(data), &chunk)

		// 同时测试 emitSSEToken 不会 panic（使用带缓冲的通道避免阻塞）。
		d := &DeepSeek{logger: testDiscardLogger()}
		ch := make(chan string, 1)
		ctx := context.Background()
		d.emitSSEToken(ctx, data, ch)
	})
}
