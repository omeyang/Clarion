package dialogue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/rules"
	"github.com/omeyang/clarion/internal/guard"
)

// testContentEngineConfig 创建带 ContentChecker 的测试引擎配置。
func testContentEngineConfig(llm *mockLLM) EngineConfig {
	cfg := testEngineConfig(llm)
	cfg.ContentCheckerCfg = &guard.ContentCheckerConfig{}
	return cfg
}

func TestEngine_ContentChecker_NilWhenNotConfigured(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	assert.Nil(t, eng.contentChk)
}

func TestEngine_ContentChecker_NotNilWhenConfigured(t *testing.T) {
	eng, err := NewEngine(testContentEngineConfig(nil))
	require.NoError(t, err)

	assert.NotNil(t, eng.contentChk)
}

func TestEngine_ContentChecker_CleansJSONArtifact(t *testing.T) {
	// LLM 返回包含 JSON 片段的回复。
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{
				Intent:         engine.IntentContinue,
				Confidence:     0.9,
				SuggestedReply: `好的，我帮您记录 {"name": "张三"}`,
			},
		},
	}

	cfg := testContentEngineConfig(llm)
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	reply, err := eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)
	// JSON 片段应被检测到（ContentChecker 标记问题但保留清洗后文本）。
	assert.NotEmpty(t, reply)
}

func TestEngine_ContentChecker_CleansMarkdown(t *testing.T) {
	// LLM 返回包含 Markdown 格式的回复。
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{
				Intent:         engine.IntentContinue,
				Confidence:     0.9,
				SuggestedReply: "**重要提示**：请提供您的信息",
			},
		},
	}

	cfg := testContentEngineConfig(llm)
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	reply, err := eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)
	// Markdown 加粗标记应被移除。
	assert.NotContains(t, reply, "**")
	assert.Contains(t, reply, "重要提示")
}

func TestEngine_ContentChecker_CleansCodeBlock(t *testing.T) {
	// LLM 返回包含代码块的回复。
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{
				Intent:         engine.IntentContinue,
				Confidence:     0.9,
				SuggestedReply: "请参考以下代码 ```print('hello')``` 就行了",
			},
		},
	}

	cfg := testContentEngineConfig(llm)
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	reply, err := eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)
	// 代码块应被移除。
	assert.NotContains(t, reply, "```")
}

func TestEngine_ContentChecker_CleansURL(t *testing.T) {
	// LLM 返回包含 URL 的回复。
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{
				Intent:         engine.IntentContinue,
				Confidence:     0.9,
				SuggestedReply: "请访问 https://example.com/page 查看详情",
			},
		},
	}

	cfg := testContentEngineConfig(llm)
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	reply, err := eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)
	// URL 应被移除。
	assert.NotContains(t, reply, "https://")
}

func TestEngine_ContentChecker_NormalTextPasses(t *testing.T) {
	// 正常文本应原样通过。
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{
				Intent:         engine.IntentContinue,
				Confidence:     0.9,
				SuggestedReply: "好的，我了解了",
			},
		},
	}

	cfg := testContentEngineConfig(llm)
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	reply, err := eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)
	assert.Equal(t, "好的，我了解了", reply)
}

func TestEngine_ContentChecker_StreamCleansContent(t *testing.T) {
	// 流式模式下也应清洗内容。
	llm := &mockStreamLLM{
		tokens: []string{"请访问 ", "https://example.com", " 查看。"},
	}

	cfg := testContentEngineConfig(nil)
	cfg.LLM = llm
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	ch, err := eng.ProcessUserInputStream(context.Background(), "你好")
	require.NoError(t, err)

	var sentences []string
	for s := range ch {
		sentences = append(sentences, s)
	}
	// 每个句段都不应包含 URL。
	for _, s := range sentences {
		assert.NotContains(t, s, "https://")
	}
}
