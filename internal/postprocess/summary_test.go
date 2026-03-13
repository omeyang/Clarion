package postprocess

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/provider"
)

// mockLLM implements provider.LLMProvider for testing.
type mockLLM struct {
	generateFn func(ctx context.Context, messages []provider.Message, cfg provider.LLMConfig) (string, error)
}

func (m *mockLLM) GenerateStream(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (<-chan string, error) {
	return nil, errors.New("not implemented")
}

func (m *mockLLM) Generate(ctx context.Context, messages []provider.Message, cfg provider.LLMConfig) (string, error) {
	if m.generateFn != nil {
		return m.generateFn(ctx, messages, cfg)
	}
	return "test summary", nil
}

func TestSummarizer_GenerateSummary(t *testing.T) {
	turns := []dialogue.Turn{
		{Number: 1, Speaker: "bot", Content: "你好", StateBefore: engine.DialogueOpening, StateAfter: engine.DialogueOpening},
		{Number: 2, Speaker: "user", Content: "你好，请说", StateBefore: engine.DialogueOpening, StateAfter: engine.DialogueQualification},
	}
	fields := map[string]string{"company_name": "测试公司"}

	tests := []struct {
		name     string
		llm      provider.LLMProvider
		wantErr  bool
		contains string
	}{
		{
			name:     "nil LLM falls back",
			llm:      nil,
			contains: "通话共2轮对话",
		},
		{
			name: "successful LLM call",
			llm: &mockLLM{generateFn: func(_ context.Context, msgs []provider.Message, _ provider.LLMConfig) (string, error) {
				assert.Len(t, msgs, 1)
				return "客户表示有兴趣", nil
			}},
			contains: "客户表示有兴趣",
		},
		{
			name: "LLM error falls back",
			llm: &mockLLM{generateFn: func(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
				return "", errors.New("llm timeout")
			}},
			contains: "通话共2轮对话",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSummarizer(tt.llm, slog.Default())
			result, err := s.GenerateSummary(context.Background(), turns, fields)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestBuildSummaryPrompt(t *testing.T) {
	turns := []dialogue.Turn{
		{Speaker: "bot", Content: "hello"},
		{Speaker: "user", Content: "hi"},
	}
	fields := map[string]string{"name": "Alice"}

	prompt := buildSummaryPrompt(turns, fields)

	assert.Contains(t, prompt, "[bot] hello")
	assert.Contains(t, prompt, "[user] hi")
	assert.Contains(t, prompt, "name: Alice")
}

func TestFallbackSummary_NoFields(t *testing.T) {
	s := NewSummarizer(nil, slog.Default())
	result := s.fallbackSummary([]dialogue.Turn{{}, {}, {}}, nil)
	assert.Contains(t, result, "通话共3轮对话")
	assert.NotContains(t, result, "收集到字段")
}

func TestFallbackSummary_WithFields(t *testing.T) {
	s := NewSummarizer(nil, slog.Default())
	result := s.fallbackSummary([]dialogue.Turn{{}}, map[string]string{"k": "v"})
	assert.Contains(t, result, "收集到字段")
	assert.Contains(t, result, "k=v")
}
