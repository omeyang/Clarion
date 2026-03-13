//go:build integration

package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/omeyang/clarion/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 集成测试：验证 DeepSeek API 的真实连通性。
// 运行方式：CLARION_LLM_API_KEY=sk-xxx go test -tags=integration -run=Integration -v ./internal/provider/llm/

func getTestLLMKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("CLARION_LLM_API_KEY")
	if key == "" {
		t.Skip("跳过集成测试：未设置 CLARION_LLM_API_KEY")
	}
	return key
}

func TestIntegration_DeepSeek_Generate(t *testing.T) {
	apiKey := getTestLLMKey(t)

	ds := NewDeepSeek(apiKey, "https://api.deepseek.com")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	messages := []provider.Message{
		{Role: "system", Content: "你是一个简洁的助手，回答尽量简短。"},
		{Role: "user", Content: "1+1等于几？只回答数字。"},
	}

	result, err := ds.Generate(ctx, messages, provider.LLMConfig{
		Model:       "deepseek-chat",
		MaxTokens:   64,
		Temperature: 0.0,
		TimeoutMs:   15000,
	})
	require.NoError(t, err, "Generate 调用失败")
	assert.NotEmpty(t, result, "响应不应为空")
	assert.Contains(t, result, "2", "回答应包含数字 2")

	t.Logf("非流式响应: %s", result)
}

func TestIntegration_DeepSeek_GenerateStream(t *testing.T) {
	apiKey := getTestLLMKey(t)

	ds := NewDeepSeek(apiKey, "https://api.deepseek.com")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	messages := []provider.Message{
		{Role: "system", Content: "你是一个简洁的助手。"},
		{Role: "user", Content: "用一句话介绍你自己。"},
	}

	ch, err := ds.GenerateStream(ctx, messages, provider.LLMConfig{
		Model:       "deepseek-chat",
		MaxTokens:   128,
		Temperature: 0.7,
		TimeoutMs:   15000,
	})
	require.NoError(t, err, "GenerateStream 调用失败")

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}

	fullResponse := strings.Join(tokens, "")
	assert.NotEmpty(t, fullResponse, "流式响应不应为空")
	assert.Greater(t, len(tokens), 1, "应收到多个 token")

	t.Logf("流式响应 (共 %d 个 token): %s", len(tokens), fullResponse)
}

func TestIntegration_DeepSeek_GenerateStream_JSON(t *testing.T) {
	apiKey := getTestLLMKey(t)

	ds := NewDeepSeek(apiKey, "https://api.deepseek.com")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 测试结构化输出能力（对话引擎需要 JSON 格式的意图识别结果）。
	messages := []provider.Message{
		{Role: "system", Content: `你是一个意图分类器。用户说话后，你必须返回 JSON 格式：
{"intent": "interested|not_interested|busy|ask_detail|reject", "confidence": 0.0-1.0}
只返回 JSON，不要其他内容。`},
		{Role: "user", Content: "嗯，这个听起来不错，你说说具体怎么弄？"},
	}

	result, err := ds.Generate(ctx, messages, provider.LLMConfig{
		Model:       "deepseek-chat",
		MaxTokens:   128,
		Temperature: 0.0,
		TimeoutMs:   15000,
	})
	require.NoError(t, err, "JSON 格式生成失败")
	assert.NotEmpty(t, result)

	// 验证返回了有效的 JSON（至少包含 intent 字段）。
	assert.Contains(t, result, "intent", "响应应包含 intent 字段")

	t.Logf("意图分类结果: %s", result)
}
