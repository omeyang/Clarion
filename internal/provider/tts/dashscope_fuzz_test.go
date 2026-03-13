package tts

import (
	"log/slog"
	"testing"
)

// FuzzParseWSMessage 模糊测试 DashScope WebSocket 文本消息解析，确保任意输入不会 panic。
func FuzzParseWSMessage(f *testing.F) {
	// 种子语料：典型 WebSocket 响应 JSON。
	f.Add([]byte(`{"header":{"event":"task-started","task_id":"abc"},"payload":{}}`))
	f.Add([]byte(`{"header":{"event":"task-finished","task_id":"abc"},"payload":{}}`))
	f.Add([]byte(`{"header":{"event":"task-failed","error_code":"E001","error_message":"fail"},"payload":{}}`))
	f.Add([]byte(`{"header":{"event":"result-generated"},"payload":{}}`))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{}`))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, data []byte) {
		d := &DashScope{
			logger: slog.Default(),
		}
		// handleTextMessage 不应对任何输入 panic。
		_ = d.handleTextMessage(data)
	})
}
