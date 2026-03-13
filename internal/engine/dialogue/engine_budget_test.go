package dialogue

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/rules"
	"github.com/omeyang/clarion/internal/guard"
)

// testBudgetEngineConfig 创建带预算配置的测试引擎配置。
func testBudgetEngineConfig(llm *mockLLM, budgetCfg guard.BudgetConfig) EngineConfig {
	cfg := testEngineConfig(llm)
	cfg.BudgetConfig = &budgetCfg
	return cfg
}

func TestEngine_Budget_NilWhenNotConfigured(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	assert.Nil(t, eng.Budget())
}

func TestEngine_Budget_NotNilWhenConfigured(t *testing.T) {
	cfg := testBudgetEngineConfig(nil, guard.BudgetConfig{
		MaxTokens: 1000,
		MaxTurns:  10,
	})
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	assert.NotNil(t, eng.Budget())
}

func TestEngine_Budget_TokenExhausted_ReturnsFallbackReply(t *testing.T) {
	llm := &mockLLM{}

	cfg := testBudgetEngineConfig(llm, guard.BudgetConfig{
		MaxTokens:        5, // 极低 token 上限。
		DegradeThreshold: 0.99,
	})
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	// 手动消耗 token 超过上限。
	eng.budget.RecordTokens(5)

	// 预算已耗尽，应返回结束回复。
	reply, err := eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)
	assert.Equal(t, budgetFallbackReply, reply)
	// LLM 不应被调用。
	assert.Equal(t, 0, llm.callCount)
}

func TestEngine_Budget_TurnExhausted_ReturnsFallbackReply(t *testing.T) {
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{Intent: engine.IntentContinue, Confidence: 0.9, SuggestedReply: "好的"},
		},
	}

	cfg := testBudgetEngineConfig(llm, guard.BudgetConfig{
		MaxTurns:         1, // 仅允许 1 轮。
		DegradeThreshold: 0.99,
	})
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	// 第一轮正常，消耗 1 轮。
	_, err = eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)

	// 第二轮应被预算拦截。
	reply, err := eng.ProcessUserInput(context.Background(), "继续")
	require.NoError(t, err)
	assert.Equal(t, budgetFallbackReply, reply)
}

func TestEngine_Budget_Degrade_ReturnsTemplateReply(t *testing.T) {
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{Intent: engine.IntentContinue, Confidence: 0.9, SuggestedReply: "好的"},
			{Intent: engine.IntentContinue, Confidence: 0.9, SuggestedReply: "好的"},
		},
	}

	cfg := testBudgetEngineConfig(llm, guard.BudgetConfig{
		MaxTurns:         5,
		DegradeThreshold: 0.4, // 40% 触发降级 → 2 轮后降级。
	})
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	// 第一轮正常。
	_, err = eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)

	// 第二轮正常（消耗 2 轮，40% = 2/5 触发降级）。
	_, err = eng.ProcessUserInput(context.Background(), "继续")
	require.NoError(t, err)

	// 第三轮应降级为模板回复。
	reply, err := eng.ProcessUserInput(context.Background(), "再继续")
	require.NoError(t, err)
	assert.Equal(t, budgetDegradeReply, reply)
	// LLM 应只被调用了 2 次（第三轮跳过 LLM）。
	assert.Equal(t, 2, llm.callCount)
}

func TestEngine_Budget_RecordTokens(t *testing.T) {
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{Intent: engine.IntentContinue, Confidence: 0.9, SuggestedReply: "好的"},
		},
	}

	cfg := testBudgetEngineConfig(llm, guard.BudgetConfig{
		MaxTokens: 10000,
		MaxTurns:  100,
	})
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	_, err = eng.ProcessUserInput(context.Background(), "你好啊")
	require.NoError(t, err)

	budget := eng.Budget()
	assert.Greater(t, budget.UsedTokens(), 0, "应记录 token 消耗")
	assert.Equal(t, 1, budget.UsedTurns(), "应记录 1 轮")
}

func TestEngine_Budget_StreamReturnsTemplateWhenExhausted(t *testing.T) {
	llm := &mockLLM{}

	cfg := testBudgetEngineConfig(llm, guard.BudgetConfig{
		MaxTurns:         1,
		DegradeThreshold: 0.99,
	})
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	// 手动消耗预算。
	eng.budget.RecordTurn()

	// ProcessUserInputStream 应返回预算拦截回复。
	ch, err := eng.ProcessUserInputStream(context.Background(), "你好")
	require.NoError(t, err)

	var replies []string
	for s := range ch {
		replies = append(replies, s)
	}
	require.Len(t, replies, 1)
	assert.Equal(t, budgetFallbackReply, replies[0])
}

func TestEngine_Budget_PrepareStreamReturnsTemplateWhenExhausted(t *testing.T) {
	llm := &mockLLM{}

	cfg := testBudgetEngineConfig(llm, guard.BudgetConfig{
		MaxTurns:         1,
		DegradeThreshold: 0.99,
	})
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	// 手动消耗预算。
	eng.budget.RecordTurn()

	// PrepareStream 应返回预算拦截回复。
	ch, commitFn, err := eng.PrepareStream(context.Background(), "你好")
	require.NoError(t, err)

	var replies []string
	for s := range ch {
		replies = append(replies, s)
	}
	require.Len(t, replies, 1)
	assert.Equal(t, budgetFallbackReply, replies[0])

	// 调用 commitFn 不应 panic。
	commitFn()
}

func TestEngine_Budget_DurationExhausted(t *testing.T) {
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{Intent: engine.IntentContinue, Confidence: 0.9, SuggestedReply: "好的"},
		},
	}

	cfg := testBudgetEngineConfig(llm, guard.BudgetConfig{
		MaxDuration:      50 * time.Millisecond,
		DegradeThreshold: 0.99,
	})
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	// 等待时长超过预算。
	time.Sleep(60 * time.Millisecond)

	reply, err := eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)
	assert.Equal(t, budgetFallbackReply, reply)
}
