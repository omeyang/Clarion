package dialogue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamTokensToSentences_SentenceBreakers(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	tokenCh := make(chan string, 20)
	sentenceCh := make(chan string, 10)

	// 模拟 LLM 按 token 输出。
	tokens := []string{"好", "的", "，", "我", "理", "解", "您", "的", "需", "求", "。"}
	go func() {
		for _, tok := range tokens {
			tokenCh <- tok
		}
		close(tokenCh)
	}()

	fullText := eng.streamTokensToSentences(context.Background(), tokenCh, sentenceCh)
	close(sentenceCh)

	var sentences []string
	for s := range sentenceCh {
		sentences = append(sentences, s)
	}

	// 句号始终触发分句。
	assert.NotEmpty(t, sentences)
	assert.Contains(t, fullText, "好的，我理解您的需求。")
}

func TestStreamTokensToSentences_ClauseBreakers(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	tokenCh := make(chan string, 50)
	sentenceCh := make(chan string, 10)

	// "好的我理解了，接下来我详细给您介绍一下。"
	// 逗号处：如果 rune 数 >= 6 则切分。
	tokens := []string{"好", "的", "我", "理", "解", "了", "，", "接", "下", "来", "我", "详", "细", "给", "您", "介", "绍", "一", "下", "。"}
	go func() {
		for _, tok := range tokens {
			tokenCh <- tok
		}
		close(tokenCh)
	}()

	fullText := eng.streamTokensToSentences(context.Background(), tokenCh, sentenceCh)
	close(sentenceCh)

	var sentences []string
	for s := range sentenceCh {
		sentences = append(sentences, s)
	}

	// 逗号处应分句（因为"好的我理解了，"有 7 个 rune >= minClauseRunes=6）。
	assert.Len(t, sentences, 2, "应在逗号和句号处各切分一次")
	assert.Equal(t, "好的我理解了，", sentences[0])
	assert.Equal(t, "接下来我详细给您介绍一下。", sentences[1])
	assert.Equal(t, "好的我理解了，接下来我详细给您介绍一下。", fullText)
}

func TestStreamTokensToSentences_ShortClauseNotSplit(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	tokenCh := make(chan string, 20)
	sentenceCh := make(chan string, 10)

	// "嗯，好的。" — 逗号前只有 1 个 rune，不应在逗号处切分。
	tokens := []string{"嗯", "，", "好", "的", "。"}
	go func() {
		for _, tok := range tokens {
			tokenCh <- tok
		}
		close(tokenCh)
	}()

	fullText := eng.streamTokensToSentences(context.Background(), tokenCh, sentenceCh)
	close(sentenceCh)

	var sentences []string
	for s := range sentenceCh {
		sentences = append(sentences, s)
	}

	// 逗号前太短（"嗯" 只有 1 个 rune < 6），不应在逗号切分，整句在句号处切分。
	assert.Len(t, sentences, 1)
	assert.Equal(t, "嗯，好的。", sentences[0])
	assert.Equal(t, "嗯，好的。", fullText)
}

func TestStreamTokensToSentences_StageDirectionFiltered(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	tokenCh := make(chan string, 20)
	sentenceCh := make(chan string, 10)

	// "（停顿）好的。" — 舞台指令应被过滤。
	tokens := []string{"（", "停", "顿", "）", "好", "的", "。"}
	go func() {
		for _, tok := range tokens {
			tokenCh <- tok
		}
		close(tokenCh)
	}()

	eng.streamTokensToSentences(context.Background(), tokenCh, sentenceCh)
	close(sentenceCh)

	var sentences []string
	for s := range sentenceCh {
		sentences = append(sentences, s)
	}

	assert.Len(t, sentences, 1)
	assert.Equal(t, "好的。", sentences[0])
}

func TestStreamTokensToSentences_RemainingText(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	tokenCh := make(chan string, 20)
	sentenceCh := make(chan string, 10)

	// 没有标点的文本应在通道关闭时作为剩余文本发送。
	tokens := []string{"好", "的"}
	go func() {
		for _, tok := range tokens {
			tokenCh <- tok
		}
		close(tokenCh)
	}()

	eng.streamTokensToSentences(context.Background(), tokenCh, sentenceCh)
	close(sentenceCh)

	var sentences []string
	for s := range sentenceCh {
		sentences = append(sentences, s)
	}

	assert.Len(t, sentences, 1)
	assert.Equal(t, "好的", sentences[0])
}

func TestStreamTokensToSentences_ContextCancelled(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	tokenCh := make(chan string, 20)
	sentenceCh := make(chan string, 1) // 小缓冲，容易阻塞

	ctx, cancel := context.WithCancel(context.Background())

	// 发送一个句子然后取消 context。
	tokens := []string{"好", "的", "。", "还", "有", "。"}
	go func() {
		for _, tok := range tokens {
			tokenCh <- tok
		}
		close(tokenCh)
	}()

	// 先取消 context，再调用。
	cancel()

	fullText := eng.streamTokensToSentences(ctx, tokenCh, sentenceCh)
	close(sentenceCh)

	// context 取消后应返回已处理的文本。
	assert.NotEmpty(t, fullText)
}
