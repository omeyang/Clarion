package simulate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLM 用于测试的 LLM 模拟实现。每次调用 Generate 按顺序返回预设响应。
type mockLLM struct {
	responses []string
	callCount int
}

func (m *mockLLM) Generate(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
	if m.callCount >= len(m.responses) {
		return `{"intent":"unknown","confidence":0.1}`, nil
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func (m *mockLLM) GenerateStream(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (<-chan string, error) {
	return nil, errors.New("not implemented")
}

// llmResponse 构造 LLM JSON 响应的辅助函数。
func llmResponse(intent engine.Intent, fields map[string]string, reply string) string {
	out := map[string]any{
		"intent":           intent,
		"extracted_fields": fields,
		"suggested_reply":  reply,
		"confidence":       0.9,
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func TestNewSimulator(t *testing.T) {
	tests := []struct {
		name    string
		cfg     SimulatorConfig
		wantErr bool
	}{
		{
			name: "default config",
			cfg: SimulatorConfig{
				Logger: slog.Default(),
				Input:  strings.NewReader(""),
				Output: &bytes.Buffer{},
			},
		},
		{
			name: "with required fields",
			cfg: SimulatorConfig{
				Logger:         slog.Default(),
				RequiredFields: []string{"name", "phone"},
				Input:          strings.NewReader(""),
				Output:         &bytes.Buffer{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sim, err := NewSimulator(tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, sim)
		})
	}
}

func TestSimulator_RunEOF(t *testing.T) {
	var out bytes.Buffer

	sim, err := NewSimulator(SimulatorConfig{
		Logger: slog.Default(),
		Input:  strings.NewReader(""), // Empty input → EOF immediately.
		Output: &out,
	})
	require.NoError(t, err)

	err = sim.Run(context.Background())
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "Clarion")
	assert.Contains(t, output, "输入结束")
}

func TestSimulator_RunWithInput(t *testing.T) {
	input := "你好\n我想了解一下\n/quit\n"
	var out bytes.Buffer

	sim, err := NewSimulator(SimulatorConfig{
		Logger:         slog.Default(),
		RequiredFields: []string{"company_name"},
		Input:          strings.NewReader(input),
		Output:         &out,
	})
	require.NoError(t, err)

	err = sim.Run(context.Background())
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "机器人:")
	assert.Contains(t, output, "退出模拟")
}

func TestSimulator_StateCommand(t *testing.T) {
	input := "/state\n/quit\n"
	var out bytes.Buffer

	sim, err := NewSimulator(SimulatorConfig{
		Logger: slog.Default(),
		Input:  strings.NewReader(input),
		Output: &out,
	})
	require.NoError(t, err)

	err = sim.Run(context.Background())
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "状态")
}

func TestSimulator_EmptyInputSkipped(t *testing.T) {
	input := "\n\n/quit\n"
	var out bytes.Buffer

	sim, err := NewSimulator(SimulatorConfig{
		Logger: slog.Default(),
		Input:  strings.NewReader(input),
		Output: &out,
	})
	require.NoError(t, err)

	err = sim.Run(context.Background())
	require.NoError(t, err)

	// Should not crash on empty lines.
	assert.Contains(t, out.String(), "退出模拟")
}

func TestSimulator_ContextCancellation(t *testing.T) {
	// Use a reader that blocks, but cancel the context.
	pr, _ := io.Pipe() // Never writes.
	defer pr.Close()

	var out bytes.Buffer

	sim, err := NewSimulator(SimulatorConfig{
		Logger: slog.Default(),
		Input:  pr,
		Output: &out,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err = sim.Run(ctx)
	require.NoError(t, err)

	assert.Contains(t, out.String(), "模拟已取消")
}

func TestSimulator_ExitCommand(t *testing.T) {
	input := "/exit\n"
	var out bytes.Buffer

	sim, err := NewSimulator(SimulatorConfig{
		Logger: slog.Default(),
		Input:  strings.NewReader(input),
		Output: &out,
	})
	require.NoError(t, err)

	err = sim.Run(context.Background())
	require.NoError(t, err)

	assert.Contains(t, out.String(), "退出模拟")
}

func TestNewSimulator_CustomMaxTurns(t *testing.T) {
	sim, err := NewSimulator(SimulatorConfig{
		MaxTurns: 5,
		Input:    strings.NewReader(""),
		Output:   &bytes.Buffer{},
	})
	require.NoError(t, err)
	assert.NotNil(t, sim)
}

func TestNewSimulator_WithPromptTemplates(t *testing.T) {
	sim, err := NewSimulator(SimulatorConfig{
		PromptTemplates: map[string]string{
			"OPENING": "自定义开场白",
		},
		Input:  strings.NewReader(""),
		Output: &bytes.Buffer{},
	})
	require.NoError(t, err)
	assert.NotNil(t, sim)
}

func TestSimulator_RunWithCollectedFields(t *testing.T) {
	// 模拟 LLM 返回带字段提取的响应，验证 printState 显示已收集字段和缺失字段。
	mock := &mockLLM{
		responses: []string{
			llmResponse(engine.IntentInterested, map[string]string{"name": "张三"}, "请问您的电话号码？"),
		},
	}

	input := "你好，我叫张三\n/state\n/quit\n"
	var out bytes.Buffer

	sim, err := NewSimulator(SimulatorConfig{
		LLM:            mock,
		RequiredFields: []string{"name", "phone"},
		Input:          strings.NewReader(input),
		Output:         &out,
	})
	require.NoError(t, err)

	err = sim.Run(context.Background())
	require.NoError(t, err)

	output := out.String()
	// 验证已收集字段显示。
	assert.Contains(t, output, "name=张三")
	// 验证缺失字段显示。
	assert.Contains(t, output, "缺失")
	assert.Contains(t, output, "phone")
}

func TestSimulator_RunRejectEndsDialogue(t *testing.T) {
	// 模拟用户拒绝，使 FSM 转移到 Closing 状态，验证 IsFinished 分支和 printResult 输出。
	mock := &mockLLM{
		responses: []string{
			llmResponse(engine.IntentReject, nil, "好的，打扰了。"),
		},
	}

	input := "不需要\n"
	var out bytes.Buffer

	sim, err := NewSimulator(SimulatorConfig{
		LLM:            mock,
		RequiredFields: []string{"name"},
		Input:          strings.NewReader(input),
		Output:         &out,
	})
	require.NoError(t, err)

	err = sim.Run(context.Background())
	require.NoError(t, err)

	output := out.String()
	// 对话应自然结束，显示结果面板。
	assert.Contains(t, output, "对话结果")
	assert.Contains(t, output, "评级")
	assert.Contains(t, output, "轮次")
	// 验证缺失字段在结果中显示。
	assert.Contains(t, output, "缺失字段")
}

func TestSimulator_PrintResultWithCollectedFields(t *testing.T) {
	// 先收集字段再拒绝，验证 printResult 中收集字段的显示。
	mock := &mockLLM{
		responses: []string{
			llmResponse(engine.IntentInterested, map[string]string{"name": "李四"}, "好的"),
			llmResponse(engine.IntentReject, nil, "再见"),
		},
	}

	input := "我叫李四\n不用了\n"
	var out bytes.Buffer

	sim, err := NewSimulator(SimulatorConfig{
		LLM:            mock,
		RequiredFields: []string{"name", "phone"},
		Input:          strings.NewReader(input),
		Output:         &out,
	})
	require.NoError(t, err)

	err = sim.Run(context.Background())
	require.NoError(t, err)

	output := out.String()
	// 验证结果面板中显示已收集字段。
	assert.Contains(t, output, "对话结果")
	assert.Contains(t, output, "收集字段")
	assert.Contains(t, output, "name")
	assert.Contains(t, output, "李四")
}
