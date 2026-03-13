package call

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/provider"
)

func TestSession_HandlePartialASR_StabilityTracking(t *testing.T) {
	s := newTestSession(engine.MediaUserSpeaking)
	s.ctx = context.Background()

	// 第一次 partial：初始化追踪。
	s.handlePartialASR(s.ctx, provider.ASREvent{Text: "你好", IsFinal: false})
	assert.Equal(t, "你好", s.lastPartialText)
	assert.Equal(t, 1, s.partialStableCount)

	// 第二次 partial 相同文本：计数增加。
	s.handlePartialASR(s.ctx, provider.ASREvent{Text: "你好", IsFinal: false})
	assert.Equal(t, 2, s.partialStableCount)

	// partial 变化：重置计数。
	s.handlePartialASR(s.ctx, provider.ASREvent{Text: "你好吗", IsFinal: false})
	assert.Equal(t, "你好吗", s.lastPartialText)
	assert.Equal(t, 1, s.partialStableCount)
}

func TestSession_HandlePartialASR_IgnoredInBotSpeaking(t *testing.T) {
	s := newTestSession(engine.MediaBotSpeaking)
	s.ctx = context.Background()

	s.handlePartialASR(s.ctx, provider.ASREvent{Text: "你好", IsFinal: false})
	assert.Equal(t, "", s.lastPartialText)
	assert.Equal(t, 0, s.partialStableCount)
}

func TestSession_HandlePartialASR_EmptyText(t *testing.T) {
	s := newTestSession(engine.MediaUserSpeaking)
	s.ctx = context.Background()

	s.handlePartialASR(s.ctx, provider.ASREvent{Text: "", IsFinal: false})
	assert.Equal(t, "", s.lastPartialText)
	assert.Equal(t, 0, s.partialStableCount)
}

func TestSession_CancelSpeculative_Nil(t *testing.T) {
	s := newTestSession(engine.MediaUserSpeaking)

	// 取消 nil 预推理不应 panic。
	s.cancelSpeculative()
	assert.Nil(t, s.speculative)
}

func TestSession_CancelSpeculative_Active(t *testing.T) {
	s := newTestSession(engine.MediaUserSpeaking)

	// 设置一个模拟的预推理。
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan string, 1)
	ch <- "test"
	close(ch)

	s.speculative = &speculativeRun{
		inputText:  "test input",
		sentenceCh: ch,
		commit:     func() {},
		cancel:     cancel,
	}

	s.cancelSpeculative()
	assert.Nil(t, s.speculative)
	assert.Error(t, ctx.Err()) // context 应已取消
}

func TestSession_ResetPartialTracking(t *testing.T) {
	s := newTestSession(engine.MediaUserSpeaking)
	s.lastPartialText = "test"
	s.partialStableCount = 5

	s.resetPartialTracking()
	assert.Equal(t, "", s.lastPartialText)
	assert.Equal(t, 0, s.partialStableCount)
	assert.Nil(t, s.speculative)
}

func TestEndsWithSentencePunctuation(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"你好。", true},
		{"你好！", true},
		{"你好？", true},
		{"你好.", true},
		{"你好!", true},
		{"你好?", true},
		{"你好", false},
		{"你好，", false},
		{"", false},
		{"你好。 ", true},  // 尾部空白跳过。
		{"你好？\n", true}, // 尾部换行跳过。
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := endsWithSentencePunctuation(tt.text)
			assert.Equal(t, tt.want, got, "text=%q", tt.text)
		})
	}
}
