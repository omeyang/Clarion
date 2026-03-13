package tts

import (
	"encoding/json"
	"testing"
)

// BenchmarkSynthesizeMessage 基准测试 wsEnvelope 的 JSON 序列化/反序列化。
func BenchmarkSynthesizeMessage(b *testing.B) {
	msg := wsEnvelope{
		Header: wsHeader{
			Action:    "run-task",
			TaskID:    "bench-task-id-12345678",
			Streaming: "duplex",
		},
		Payload: wsPayload{
			TaskGroup: "audio",
			Task:      "tts",
			Function:  "SpeechSynthesizer",
			Model:     "cosyvoice-v3-flash",
			Parameters: &ttsParameters{
				TextType:   "PlainText",
				Voice:      "longanyang",
				Format:     "pcm",
				SampleRate: 16000,
				Volume:     50,
				Rate:       1,
				Pitch:      1,
			},
			Input: &ttsInput{Text: "你好，请问您方便了解一下我们的产品吗？"},
		},
	}

	b.ResetTimer()
	for range b.N {
		data, _ := json.Marshal(msg)
		var out wsEnvelope
		_ = json.Unmarshal(data, &out)
	}
}
