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

func TestEngine_OffTopic_NilWhenNotConfigured(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)
	assert.Nil(t, eng.offTopic)
}

func TestEngine_OffTopic_NotNilWhenConfigured(t *testing.T) {
	cfg := testEngineConfig(nil)
	cfg.OffTopicCfg = &guard.OffTopicConfig{}
	eng, err := NewEngine(cfg)
	require.NoError(t, err)
	assert.NotNil(t, eng.offTopic)
}

func TestEngine_OffTopic_NormalIntentNoIntercept(t *testing.T) {
	// 正常意图不触发离题拦截。
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{Intent: engine.IntentContinue, Confidence: 0.9, SuggestedReply: "好的"},
		},
	}

	cfg := testEngineConfig(llm)
	cfg.OffTopicCfg = &guard.OffTopicConfig{ConvergeAfter: 2, EndAfter: 4}
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	reply, err := eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)
	assert.NotEqual(t, offTopicConvergeReply, reply)
	assert.NotEqual(t, offTopicEndReply, reply)
}

func TestEngine_OffTopic_ConvergeAfterThreshold(t *testing.T) {
	// 连续 2 轮 unknown 应触发收束。
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{Intent: engine.IntentUnknown, Confidence: 0.3, SuggestedReply: "不知道"},
			{Intent: engine.IntentUnknown, Confidence: 0.3, SuggestedReply: "还是不知道"},
		},
	}

	cfg := testEngineConfig(llm)
	cfg.OffTopicCfg = &guard.OffTopicConfig{ConvergeAfter: 2, EndAfter: 4}
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	// 第 1 轮 unknown：未达到阈值。
	reply1, err := eng.ProcessUserInput(context.Background(), "今天天气怎么样")
	require.NoError(t, err)
	assert.NotEqual(t, offTopicConvergeReply, reply1)

	// 第 2 轮 unknown：达到 converge 阈值。
	reply2, err := eng.ProcessUserInput(context.Background(), "你喜欢什么颜色")
	require.NoError(t, err)
	assert.Equal(t, offTopicConvergeReply, reply2)
}

func TestEngine_OffTopic_EndAfterThreshold(t *testing.T) {
	// 连续 4 轮 unknown 应触发结束。
	responses := make([]rules.LLMOutput, 4)
	for i := range responses {
		responses[i] = rules.LLMOutput{
			Intent: engine.IntentUnknown, Confidence: 0.3, SuggestedReply: "不知道",
		}
	}
	llm := &mockLLM{responses: responses}

	cfg := testEngineConfig(llm)
	cfg.OffTopicCfg = &guard.OffTopicConfig{ConvergeAfter: 2, EndAfter: 4}
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	// 前 3 轮：收束但不结束。
	for i := range 3 {
		var reply string
		reply, err = eng.ProcessUserInput(context.Background(), "离题问题")
		require.NoError(t, err, "第 %d 轮不应报错", i+1)
		assert.NotEqual(t, offTopicEndReply, reply, "第 %d 轮不应结束", i+1)
	}

	// 第 4 轮：触发结束。
	endReply, err := eng.ProcessUserInput(context.Background(), "又离题了")
	require.NoError(t, err)
	assert.Equal(t, offTopicEndReply, endReply)
}

func TestEngine_OffTopic_ResetOnNormalIntent(t *testing.T) {
	// 中间插入正常意图应重置计数器。
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{Intent: engine.IntentUnknown, Confidence: 0.3, SuggestedReply: "不知道"},
			{Intent: engine.IntentContinue, Confidence: 0.9, SuggestedReply: "好的"},
			{Intent: engine.IntentUnknown, Confidence: 0.3, SuggestedReply: "不知道"},
		},
	}

	cfg := testEngineConfig(llm)
	cfg.OffTopicCfg = &guard.OffTopicConfig{ConvergeAfter: 2, EndAfter: 4}
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	// 第 1 轮 unknown。
	_, err = eng.ProcessUserInput(context.Background(), "今天天气")
	require.NoError(t, err)

	// 第 2 轮正常意图，重置计数器。
	reply2, err := eng.ProcessUserInput(context.Background(), "好的，我想了解一下")
	require.NoError(t, err)
	assert.NotEqual(t, offTopicConvergeReply, reply2)

	// 第 3 轮 unknown：只有 1 次连续离题，不应触发收束。
	reply3, err := eng.ProcessUserInput(context.Background(), "又聊到别的")
	require.NoError(t, err)
	assert.NotEqual(t, offTopicConvergeReply, reply3)
}
