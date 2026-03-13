// Package strategy 实现异步业务决策提供者。
package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/provider"
)

// SmartConfig 配置 Smart LLM 策略分析器。
type SmartConfig struct {
	LLM          provider.LLMProvider
	LLMConfig    provider.LLMConfig
	SystemPrompt string
	Logger       *slog.Logger
}

// Input 是传递给策略分析器的输入。
type Input struct {
	UserText      string
	AssistantText string
	TurnNumber    int
	CurrentFields map[string]string
}

// Decision 是策略分析器的输出。
type Decision struct {
	Intent          engine.Intent     `json:"intent"`
	ExtractedFields map[string]string `json:"extracted_fields"`
	Instructions    string            `json:"instructions"`
	ShouldEnd       bool              `json:"should_end"`
	Grade           engine.Grade      `json:"grade"`
}

// Smart 使用文本 LLM 异步分析对话内容，输出业务决策。
// 运行在独立 goroutine 中，不阻塞实时语音管线。
type Smart struct {
	llm       provider.LLMProvider
	llmCfg    provider.LLMConfig
	sysPrompt string
	logger    *slog.Logger
}

// NewSmart 创建新的 Smart LLM 策略分析器。
func NewSmart(cfg SmartConfig) *Smart {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	sysPrompt := cfg.SystemPrompt
	if sysPrompt == "" {
		sysPrompt = defaultStrategyPrompt
	}
	return &Smart{
		llm:       cfg.LLM,
		llmCfg:    cfg.LLMConfig,
		sysPrompt: sysPrompt,
		logger:    logger,
	}
}

// Analyze 分析用户输入和助手回复，返回业务决策。
func (s *Smart) Analyze(ctx context.Context, input Input) (*Decision, error) {
	userMsg := fmt.Sprintf(
		"轮次 %d\n用户说: %s\n助手回复: %s\n已采集字段: %s",
		input.TurnNumber,
		input.UserText,
		input.AssistantText,
		formatFields(input.CurrentFields),
	)

	messages := []provider.Message{
		{Role: "system", Content: s.sysPrompt},
		{Role: "user", Content: userMsg},
	}

	tokenCh, err := s.llm.GenerateStream(ctx, messages, s.llmCfg)
	if err != nil {
		return nil, fmt.Errorf("strategy: generate stream: %w", err)
	}

	// 收集完整响应。
	var sb strings.Builder
	for token := range tokenCh {
		sb.WriteString(token)
	}

	// 解析 JSON 决策。
	result := sb.String()
	s.logger.Debug("strategy: LLM 原始输出", "result", result)

	decision, err := parseDecision(result)
	if err != nil {
		s.logger.Warn("strategy: 解析决策失败，使用默认值", "error", err)
		return &Decision{
			Intent: engine.IntentContinue,
			Grade:  engine.GradeC,
		}, nil
	}

	return decision, nil
}

// parseDecision 从 LLM 输出中提取 JSON 决策。
func parseDecision(raw string) (*Decision, error) {
	// 尝试提取 JSON 块（LLM 可能在 JSON 外包裹文字）。
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < 0 || end <= start {
		return nil, errors.New("no JSON object found in response")
	}

	var d Decision
	if err := json.Unmarshal([]byte(raw[start:end+1]), &d); err != nil {
		return nil, fmt.Errorf("unmarshal decision: %w", err)
	}
	return &d, nil
}

// formatFields 将字段 map 格式化为可读字符串。
func formatFields(fields map[string]string) string {
	if len(fields) == 0 {
		return "无"
	}
	var parts []string
	for k, v := range fields {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ", ")
}

const defaultStrategyPrompt = `你是一个外呼通话的业务分析助手。根据对话内容分析用户意图，输出 JSON 格式的决策。

输出格式（严格 JSON，不要额外文字）：
{
  "intent": "continue|reject|not_interested|interested|confirm|unknown",
  "extracted_fields": {"字段名": "值"},
  "instructions": "给语音助手的下一轮指令（中文，简短）",
  "should_end": false,
  "grade": "A|B|C|D"
}

评级标准：
- A: 明确表示有兴趣
- B: 有一定意向，需要更多信息
- C: 态度模糊
- D: 明确拒绝`
