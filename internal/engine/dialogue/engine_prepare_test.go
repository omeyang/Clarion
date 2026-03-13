package dialogue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/provider"
)

// mockStreamLLM 模拟流式 LLM，将预设的 token 逐个发送到通道。
type mockStreamLLM struct {
	tokens []string
}

func (m *mockStreamLLM) GenerateStream(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (<-chan string, error) {
	ch := make(chan string, len(m.tokens))
	for _, tok := range m.tokens {
		ch <- tok
	}
	close(ch)
	return ch, nil
}

func (m *mockStreamLLM) Generate(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
	return `{"intent":"unknown","confidence":0.5}`, nil
}

func TestEngine_PrepareStream_Basic(t *testing.T) {
	llm := &mockStreamLLM{tokens: []string{"好", "的", "，", "我", "明", "白", "了", "。"}}

	cfg := testEngineConfig(llm)
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	sentenceCh, commit, err := eng.PrepareStream(context.Background(), "你好")
	require.NoError(t, err)
	require.NotNil(t, sentenceCh)
	require.NotNil(t, commit)

	// 消费所有句子。
	var sentences []string
	for s := range sentenceCh {
		sentences = append(sentences, s)
	}
	assert.NotEmpty(t, sentences)

	// 调用前不应有轮次记录。
	assert.Empty(t, eng.Turns())

	// 确认后应记录轮次。
	commit()
	assert.Len(t, eng.Turns(), 2) // 1 user + 1 bot
	assert.Equal(t, "user", eng.Turns()[0].Speaker)
	assert.Equal(t, "bot", eng.Turns()[1].Speaker)
}

func TestEngine_PrepareStream_NilLLM(t *testing.T) {
	cfg := testEngineConfig(nil)
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	sentenceCh, commit, err := eng.PrepareStream(context.Background(), "你好")
	require.NoError(t, err)
	require.NotNil(t, sentenceCh)

	// 无 LLM 时返回默认回复。
	var sentences []string
	for s := range sentenceCh {
		sentences = append(sentences, s)
	}
	assert.Equal(t, []string{"好的，我了解了。"}, sentences)

	// commit 不应 panic。
	commit()
}

func TestEngine_PrepareStream_Cancelled(t *testing.T) {
	llm := &mockStreamLLM{tokens: []string{"好", "的", "。"}}

	cfg := testEngineConfig(llm)
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	sentenceCh, _, err := eng.PrepareStream(ctx, "你好")
	if err != nil {
		// LLM 可能因 ctx 取消而返回错误，这是正常的。
		return
	}

	// 消费通道，不应阻塞。
	for range sentenceCh {
	}

	// 取消后不应有轮次记录。
	assert.Empty(t, eng.Turns())
}
