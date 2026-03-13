package dialogue

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/rules"
	"github.com/omeyang/clarion/internal/provider"
)

type mockLLM struct {
	responses []rules.LLMOutput
	callCount int
}

func (m *mockLLM) GenerateStream(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (<-chan string, error) {
	ch := make(chan string, 1)
	close(ch)
	return ch, nil
}

func (m *mockLLM) Generate(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
	if m.callCount >= len(m.responses) {
		return `{"intent":"unknown","confidence":0.5}`, nil
	}
	out := m.responses[m.callCount]
	m.callCount++
	data, _ := json.Marshal(out)
	return string(data), nil
}

func testEngineConfig(llm provider.LLMProvider) EngineConfig {
	return EngineConfig{
		TemplateConfig: rules.TemplateConfig{
			RequiredFields: []string{"name", "age"},
			MaxObjections:  2,
			MaxTurns:       20,
			GradingRules: rules.GradingRules{
				AIntents:        []engine.Intent{engine.IntentInterested, engine.IntentSchedule},
				AMinFields:      2,
				BMinFields:      1,
				BMinTurns:       3,
				RejectIntents:   []engine.Intent{engine.IntentReject},
				InvalidStatuses: []engine.CallStatus{engine.CallNoAnswer, engine.CallVoicemail},
			},
			Templates: map[string]string{
				"CLOSING": "感谢您的时间，再见！",
			},
		},
		LLM:        llm,
		MaxHistory: 3,
		PromptTemplates: map[string]string{
			"OPENING": "您好，我是AI客服。",
		},
	}
}

func TestEngine_ProcessUserInput_BasicFlow(t *testing.T) {
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{Intent: engine.IntentContinue, Confidence: 0.9, SuggestedReply: "好的"},
		},
	}

	eng, err := NewEngine(testEngineConfig(llm))
	require.NoError(t, err)

	reply, err := eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)
	assert.NotEmpty(t, reply)
	assert.Equal(t, engine.DialogueQualification, eng.State())
}

func TestEngine_ProcessUserInput_MultiTurn(t *testing.T) {
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{Intent: engine.IntentContinue, Confidence: 0.9, SuggestedReply: "好的"},
			{
				Intent:          engine.IntentConfirm,
				ExtractedFields: map[string]string{"name": "张先生"},
				Confidence:      0.9,
				SuggestedReply:  "请问您多大？",
			},
			{
				Intent:          engine.IntentConfirm,
				ExtractedFields: map[string]string{"age": "35"},
				Confidence:      0.9,
				SuggestedReply:  "好的",
			},
		},
	}

	eng, err := NewEngine(testEngineConfig(llm))
	require.NoError(t, err)

	_, err = eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)

	_, err = eng.ProcessUserInput(context.Background(), "我叫张先生")
	require.NoError(t, err)

	_, err = eng.ProcessUserInput(context.Background(), "我35岁")
	require.NoError(t, err)
}

func TestEngine_ProcessUserInput_NilLLM(t *testing.T) {
	cfg := testEngineConfig(nil)
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	reply, err := eng.ProcessUserInput(context.Background(), "你好")
	require.NoError(t, err)
	assert.NotEmpty(t, reply)
}

func TestEngine_GetOpeningText(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	text := eng.GetOpeningText()
	assert.Equal(t, "您好，我是AI客服。", text)
}

func TestEngine_GetOpeningText_NoTemplate(t *testing.T) {
	cfg := testEngineConfig(nil)
	cfg.PromptTemplates = nil
	eng, err := NewEngine(cfg)
	require.NoError(t, err)

	text := eng.GetOpeningText()
	assert.Equal(t, "您好，打扰您一分钟。", text)
}

func TestEngine_Result(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	result := eng.Result(engine.CallCompleted)
	assert.Equal(t, engine.GradeD, result.Grade)
	assert.Equal(t, 0, result.TurnCount)
}

func TestEngine_IsFinished(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	assert.False(t, eng.IsFinished())
	eng.fsm.ForceState(engine.DialogueClosing)
	assert.True(t, eng.IsFinished())
}

func TestEngine_InvalidPromptTemplate(t *testing.T) {
	cfg := testEngineConfig(nil)
	cfg.PromptTemplates = map[string]string{
		"BAD": "{{ .Invalid }",
	}
	_, err := NewEngine(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse prompt template")
}

// --- RestoreFromSnapshot 测试 ---

func TestEngine_RestoreFromSnapshot_State(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	// 初始状态应为 OPENING。
	assert.Equal(t, engine.DialogueOpening, eng.State())

	eng.RestoreFromSnapshot(SnapshotData{
		DialogueState: "INFORMATION_GATHERING",
	})

	assert.Equal(t, engine.DialogueInformationGathering, eng.State())
}

func TestEngine_RestoreFromSnapshot_InvalidState(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	// 无效状态应保持原状。
	eng.RestoreFromSnapshot(SnapshotData{
		DialogueState: "NONEXISTENT",
	})

	assert.Equal(t, engine.DialogueOpening, eng.State())
}

func TestEngine_RestoreFromSnapshot_Turns(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	turns := []Turn{
		{Number: 1, Speaker: "bot", Content: "你好"},
		{Number: 2, Speaker: "user", Content: "你好"},
		{Number: 3, Speaker: "bot", Content: "请问您贵姓？"},
		{Number: 4, Speaker: "user", Content: "我姓张"},
	}

	eng.RestoreFromSnapshot(SnapshotData{
		DialogueState: "QUALIFICATION",
		Turns:         turns,
	})

	assert.Len(t, eng.Turns(), 4)
	assert.Equal(t, "你好", eng.Turns()[0].Content)
	assert.Equal(t, "我姓张", eng.Turns()[3].Content)
}

func TestEngine_RestoreFromSnapshot_CollectedFields(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	eng.RestoreFromSnapshot(SnapshotData{
		DialogueState:   "INFORMATION_GATHERING",
		CollectedFields: map[string]string{"name": "张三"},
	})

	result := eng.Result(engine.CallCompleted)
	assert.Equal(t, "张三", result.CollectedFields["name"])
}

func TestEngine_RestoreFromSnapshot_EmptyData(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	// 空数据不应 panic，状态保持不变。
	eng.RestoreFromSnapshot(SnapshotData{})
	assert.Equal(t, engine.DialogueOpening, eng.State())
	assert.Empty(t, eng.Turns())
}

func TestEngine_RestoreFromSnapshot_ContinueDialogue(t *testing.T) {
	// 恢复后应能正常继续对话。
	llm := &mockLLM{
		responses: []rules.LLMOutput{
			{Intent: engine.IntentConfirm, Confidence: 0.9, SuggestedReply: "好的"},
		},
	}

	eng, err := NewEngine(testEngineConfig(llm))
	require.NoError(t, err)

	// 恢复到 QUALIFICATION 状态。
	eng.RestoreFromSnapshot(SnapshotData{
		DialogueState:   "QUALIFICATION",
		CollectedFields: map[string]string{"name": "张三"},
		Turns: []Turn{
			{Number: 1, Speaker: "bot", Content: "你好"},
			{Number: 2, Speaker: "user", Content: "你好"},
		},
	})

	reply, err := eng.ProcessUserInput(context.Background(), "好的，继续")
	require.NoError(t, err)
	assert.NotEmpty(t, reply)
	// 恢复后的轮次应包含之前的轮次加上新的轮次。
	assert.Greater(t, len(eng.Turns()), 2)
}

func TestEngine_GetRecoveryOpeningText(t *testing.T) {
	eng, err := NewEngine(testEngineConfig(nil))
	require.NoError(t, err)

	text := eng.GetRecoveryOpeningText()
	assert.Contains(t, text, "断了")
	assert.Contains(t, text, "接着聊")
}
