package call

import (
	"testing"
)

// FuzzParseESLEvent 模糊测试 ESL 事件解析器，确保任意输入不会 panic。
func FuzzParseESLEvent(f *testing.F) {
	// 种子语料：典型 ESL 事件格式。
	f.Add("Event-Name: CHANNEL_ANSWER\nUnique-ID: abc-123\n")
	f.Add("Event-Name: CHANNEL_HANGUP\nHangup-Cause: NORMAL_CLEARING\n\nbody content")
	f.Add("")
	f.Add("no-colon-line\n")
	f.Add("Key: Value\r\nKey2: Value2\r\n")
	f.Add("Event-Name: HEARTBEAT\n\n\n\n")

	f.Fuzz(func(t *testing.T, raw string) {
		// parseESLEvent 不应对任何输入 panic。
		event := ParseESLEventForTest(raw)
		// 基本一致性检查：Name 应与 Headers["Event-Name"] 一致。
		if event.Headers["Event-Name"] != event.Name {
			t.Errorf("Name 不一致: Headers[Event-Name]=%q, Name=%q", event.Headers["Event-Name"], event.Name)
		}
	})
}
