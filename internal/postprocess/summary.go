// Package postprocess 实现每通通话完成后运行的后处理工作进程：
// 生成摘要、写入结果和发送通知。
package postprocess

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/provider"
)

// Summarizer 使用 AI 生成已完成通话的摘要。
type Summarizer struct {
	llm    provider.LLMProvider
	logger *slog.Logger
}

// NewSummarizer 创建由给定 LLM 提供者支持的 Summarizer。
func NewSummarizer(llm provider.LLMProvider, logger *slog.Logger) *Summarizer {
	return &Summarizer{llm: llm, logger: logger}
}

const summaryPrompt = `你是一个通话分析助手。请根据以下通话对话记录生成一段简洁的中文总结（不超过200字），包含：
1. 客户的主要态度和意向
2. 收集到的关键信息
3. 建议的后续行动

已收集的字段：
{{FIELDS}}

对话记录：
{{TURNS}}

请直接输出总结内容，不要包含任何标题或前缀。`

// GenerateSummary 调用 LLM 生成对话轮次和收集字段的结构化摘要。
// 如果 LLM 调用失败，回退到简单的轮次内容拼接。
func (s *Summarizer) GenerateSummary(ctx context.Context, turns []dialogue.Turn, fields map[string]string) (string, error) {
	if s.llm == nil {
		return s.fallbackSummary(turns, fields), nil
	}

	prompt := buildSummaryPrompt(turns, fields)
	messages := []provider.Message{
		{Role: "user", Content: prompt},
	}

	cfg := provider.LLMConfig{
		MaxTokens:   256,
		Temperature: 0.3,
	}

	summary, err := s.llm.Generate(ctx, messages, cfg)
	if err != nil {
		s.logger.Warn("LLM summary failed, using fallback", slog.String("error", err.Error()))
		return s.fallbackSummary(turns, fields), nil
	}

	return strings.TrimSpace(summary), nil
}

func buildSummaryPrompt(turns []dialogue.Turn, fields map[string]string) string {
	var fieldLines []string
	for k, v := range fields {
		fieldLines = append(fieldLines, fmt.Sprintf("- %s: %s", k, v))
	}
	fieldStr := "（无）"
	if len(fieldLines) > 0 {
		fieldStr = strings.Join(fieldLines, "\n")
	}

	var turnLines []string
	for _, t := range turns {
		turnLines = append(turnLines, fmt.Sprintf("[%s] %s", t.Speaker, t.Content))
	}
	turnStr := strings.Join(turnLines, "\n")

	result := summaryPrompt
	result = strings.ReplaceAll(result, "{{FIELDS}}", fieldStr)
	result = strings.ReplaceAll(result, "{{TURNS}}", turnStr)
	return result
}

func (s *Summarizer) fallbackSummary(turns []dialogue.Turn, fields map[string]string) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("通话共%d轮对话。", len(turns)))

	if len(fields) > 0 {
		var fieldParts []string
		for k, v := range fields {
			fieldParts = append(fieldParts, fmt.Sprintf("%s=%s", k, v))
		}
		parts = append(parts, fmt.Sprintf("收集到字段：%s。", strings.Join(fieldParts, ", ")))
	}

	return strings.Join(parts, " ")
}
