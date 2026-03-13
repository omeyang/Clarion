package strategy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLM 是用于测试的 LLMProvider 模拟实现。
type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) GenerateStream(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (<-chan string, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan string, 1)
	ch <- m.response
	close(ch)
	return ch, nil
}

func (m *mockLLM) Generate(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
	return m.response, m.err
}

// ── parseDecision 测试 ──────────────────────────────────────────

func TestParseDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    *Decision
		wantErr bool
	}{
		{
			name: "有效JSON返回正确的Decision",
			raw:  `{"intent":"interested","extracted_fields":{"name":"张三"},"instructions":"继续跟进","should_end":false,"grade":"A"}`,
			want: &Decision{
				Intent:          engine.IntentInterested,
				ExtractedFields: map[string]string{"name": "张三"},
				Instructions:    "继续跟进",
				ShouldEnd:       false,
				Grade:           engine.GradeA,
			},
		},
		{
			name: "JSON被额外文字包裹仍能解析",
			raw:  `分析结果: {"intent":"continue","extracted_fields":{},"instructions":"询问更多","should_end":false,"grade":"C"} 以上`,
			want: &Decision{
				Intent:          engine.IntentContinue,
				ExtractedFields: map[string]string{},
				Instructions:    "询问更多",
				ShouldEnd:       false,
				Grade:           engine.GradeC,
			},
		},
		{
			name:    "没有JSON内容返回错误",
			raw:     "这是一段没有JSON的纯文本",
			wantErr: true,
		},
		{
			name:    "无效JSON返回错误",
			raw:     `{"intent": "continue", "grade":}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseDecision(tt.raw)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ── formatFields 测试 ───────────────────────────────────────────

func TestFormatFields(t *testing.T) {
	t.Parallel()

	t.Run("空map返回无", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "无", formatFields(nil))
		assert.Equal(t, "无", formatFields(map[string]string{}))
	})

	t.Run("非空map包含所有键值对", func(t *testing.T) {
		t.Parallel()
		fields := map[string]string{
			"name":  "张三",
			"phone": "13800000000",
		}
		result := formatFields(fields)

		// map 遍历顺序不确定，逐个检查。
		assert.Contains(t, result, "name=张三")
		assert.Contains(t, result, "phone=13800000000")
		assert.Contains(t, result, ", ")
	})
}

// ── NewSmart 测试 ───────────────────────────────────────────────

func TestNewSmart(t *testing.T) {
	t.Parallel()

	t.Run("空SystemPrompt使用默认值", func(t *testing.T) {
		t.Parallel()
		s := NewSmart(SmartConfig{
			LLM:          &mockLLM{},
			SystemPrompt: "",
		})
		assert.Equal(t, defaultStrategyPrompt, s.sysPrompt)
	})

	t.Run("自定义SystemPrompt被保留", func(t *testing.T) {
		t.Parallel()
		custom := "你是一个自定义助手"
		s := NewSmart(SmartConfig{
			LLM:          &mockLLM{},
			SystemPrompt: custom,
		})
		assert.Equal(t, custom, s.sysPrompt)
	})
}

// ── Analyze 测试 ────────────────────────────────────────────────

func TestAnalyze(t *testing.T) {
	t.Parallel()

	validJSON := `{"intent":"interested","extracted_fields":{"name":"李四"},"instructions":"安排回访","should_end":false,"grade":"A"}`

	t.Run("有效JSON响应返回正确Decision", func(t *testing.T) {
		t.Parallel()
		s := NewSmart(SmartConfig{
			LLM: &mockLLM{response: validJSON},
		})

		got, err := s.Analyze(context.Background(), Input{
			UserText:      "我很感兴趣",
			AssistantText: "好的，请问您的姓名？",
			TurnNumber:    2,
			CurrentFields: map[string]string{"phone": "13800000000"},
		})

		require.NoError(t, err)
		assert.Equal(t, engine.IntentInterested, got.Intent)
		assert.Equal(t, engine.GradeA, got.Grade)
		assert.Equal(t, "安排回访", got.Instructions)
		assert.Equal(t, map[string]string{"name": "李四"}, got.ExtractedFields)
		assert.False(t, got.ShouldEnd)
	})

	t.Run("无法解析的响应返回兜底Decision", func(t *testing.T) {
		t.Parallel()
		s := NewSmart(SmartConfig{
			LLM: &mockLLM{response: "我不知道怎么回复"},
		})

		got, err := s.Analyze(context.Background(), Input{
			UserText:   "随便说点什么",
			TurnNumber: 1,
		})

		require.NoError(t, err)
		assert.Equal(t, engine.IntentContinue, got.Intent)
		assert.Equal(t, engine.GradeC, got.Grade)
	})

	t.Run("LLM返回错误时透传错误", func(t *testing.T) {
		t.Parallel()
		llmErr := errors.New("服务不可用")
		s := NewSmart(SmartConfig{
			LLM: &mockLLM{err: llmErr},
		})

		_, err := s.Analyze(context.Background(), Input{
			UserText:   "你好",
			TurnNumber: 1,
		})

		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "服务不可用"))
	})

	t.Run("上下文取消时返回错误", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // 立即取消。

		// 模拟 LLM 在上下文取消后返回错误。
		s := NewSmart(SmartConfig{
			LLM: &mockLLM{err: ctx.Err()},
		})

		_, err := s.Analyze(ctx, Input{
			UserText:   "你好",
			TurnNumber: 1,
		})

		require.Error(t, err)
	})
}
