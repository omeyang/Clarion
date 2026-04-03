package postprocess

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/provider"
)

// Opportunity 表示从通话中提取的结构化商机信息。
type Opportunity struct {
	TenantID         string     `json:"tenant_id"`
	CallID           int64      `json:"call_id"`
	ContactID        int64      `json:"contact_id"`
	TaskID           int64      `json:"task_id"`
	Score            int        `json:"score"`
	IntentType       string     `json:"intent_type"`
	BudgetSignal     string     `json:"budget_signal"`
	TimelineSignal   string     `json:"timeline_signal"`
	ContactRole      string     `json:"contact_role"`
	PainPoints       []string   `json:"pain_points"`
	FollowupAction   string     `json:"followup_action"`
	FollowupDate     *time.Time `json:"followup_date"`
	NeedsHumanReview bool       `json:"needs_human_review"`
}

// OpportunityExtractor 使用 LLM 从通话记录中提取商机信息。
type OpportunityExtractor struct {
	llm    provider.LLMProvider
	logger *slog.Logger
}

// NewOpportunityExtractor 创建由给定 LLM 提供者支持的商机提取器。
func NewOpportunityExtractor(llm provider.LLMProvider, logger *slog.Logger) *OpportunityExtractor {
	return &OpportunityExtractor{llm: llm, logger: logger}
}

// llmOpportunity 是 LLM 返回的商机 JSON 结构。
// 与 Opportunity 分离，避免 LLM 输出直接写入业务字段。
type llmOpportunity struct {
	Score          int      `json:"score"`
	IntentType     string   `json:"intent_type"`
	BudgetSignal   string   `json:"budget_signal"`
	TimelineSignal string   `json:"timeline_signal"`
	ContactRole    string   `json:"contact_role"`
	PainPoints     []string `json:"pain_points"`
	FollowupAction string   `json:"followup_action"`
	FollowupDate   string   `json:"followup_date"`
}

const opportunityPrompt = `你是一个销售商机分析助手。请根据以下通话记录和已收集字段，提取结构化商机信息。

已收集的字段：
{{FIELDS}}

通话评级：{{GRADE}}

对话记录：
{{TURNS}}

请严格按以下 JSON 格式输出，不要包含任何其他内容：
{
  "score": <0-100整数，综合意向分>,
  "intent_type": "<interested/not_interested/needs_info/callback/unknown>",
  "budget_signal": "<has_budget/no_budget/not_mentioned>",
  "timeline_signal": "<urgent/soon/later/not_mentioned>",
  "contact_role": "<decision_maker/user/receptionist/unknown>",
  "pain_points": ["痛点1", "痛点2"],
  "followup_action": "<follow_up/abandon/transfer_human/schedule_callback>",
  "followup_date": "<YYYY-MM-DD格式或空字符串>"
}`

// Extract 从通话事件中提取商机信息。
// 如果 LLM 不可用或调用失败，回退到基于规则的提取。
func (e *OpportunityExtractor) Extract(
	ctx context.Context,
	event *CallCompletionEvent,
) (*Opportunity, error) {
	opp := &Opportunity{
		CallID:    event.CallID,
		ContactID: event.ContactID,
		TaskID:    event.TaskID,
	}

	if e.llm == nil {
		e.fillFromRules(opp, event)
		return opp, nil
	}

	prompt := buildOpportunityPrompt(event.Turns, event.CollectedFields, event.Grade)
	messages := []provider.Message{
		{Role: "user", Content: prompt},
	}
	cfg := provider.LLMConfig{
		MaxTokens:   512,
		Temperature: 0.1,
	}

	resp, err := e.llm.Generate(ctx, messages, cfg)
	if err != nil {
		e.logger.Warn("LLM 商机提取失败，回退到规则提取",
			slog.String("error", err.Error()))
		e.fillFromRules(opp, event)
		return opp, nil
	}

	if parseErr := e.parseResponse(resp, opp); parseErr != nil {
		e.logger.Warn("LLM 商机响应解析失败，回退到规则提取",
			slog.String("error", parseErr.Error()))
		e.fillFromRules(opp, event)
		return opp, nil
	}

	opp.NeedsHumanReview = opp.Score >= 70 || opp.FollowupAction == "transfer_human"

	return opp, nil
}

// parseResponse 解析 LLM 响应 JSON 并填充 Opportunity。
func (e *OpportunityExtractor) parseResponse(resp string, opp *Opportunity) error {
	cleaned := extractJSON(resp)

	var raw llmOpportunity
	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		return fmt.Errorf("解析商机 JSON: %w", err)
	}

	opp.Score = clampScore(raw.Score)
	opp.IntentType = normalizeEnum(raw.IntentType, validIntentTypes)
	opp.BudgetSignal = normalizeEnum(raw.BudgetSignal, validBudgetSignals)
	opp.TimelineSignal = normalizeEnum(raw.TimelineSignal, validTimelineSignals)
	opp.ContactRole = normalizeEnum(raw.ContactRole, validContactRoles)
	opp.PainPoints = raw.PainPoints
	opp.FollowupAction = normalizeEnum(raw.FollowupAction, validFollowupActions)

	if raw.FollowupDate != "" {
		t, err := time.Parse(time.DateOnly, raw.FollowupDate)
		if err == nil {
			opp.FollowupDate = &t
		}
	}

	return nil
}

// fillFromRules 基于通话评级和已收集字段进行规则提取。
func (e *OpportunityExtractor) fillFromRules(opp *Opportunity, event *CallCompletionEvent) {
	opp.IntentType = gradeToIntentType(event.Grade)
	opp.Score = gradeToScore(event.Grade)
	opp.BudgetSignal = "not_mentioned"
	opp.TimelineSignal = "not_mentioned"
	opp.ContactRole = "unknown"
	opp.FollowupAction = gradeToFollowupAction(event.Grade)
	opp.NeedsHumanReview = event.Grade == engine.GradeA || event.Grade == engine.GradeB
}

func buildOpportunityPrompt(turns []dialogue.Turn, fields map[string]string, grade engine.Grade) string {
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

	result := opportunityPrompt
	result = strings.ReplaceAll(result, "{{FIELDS}}", fieldStr)
	result = strings.ReplaceAll(result, "{{GRADE}}", string(grade))
	result = strings.ReplaceAll(result, "{{TURNS}}", turnStr)
	return result
}

// extractJSON 从 LLM 响应中提取 JSON 块。
// 处理 LLM 在 JSON 前后附加文本的情况。
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

func clampScore(score int) int {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

var (
	validIntentTypes     = map[string]bool{"interested": true, "not_interested": true, "needs_info": true, "callback": true, "unknown": true}
	validBudgetSignals   = map[string]bool{"has_budget": true, "no_budget": true, "not_mentioned": true}
	validTimelineSignals = map[string]bool{"urgent": true, "soon": true, "later": true, "not_mentioned": true}
	validContactRoles    = map[string]bool{"decision_maker": true, "user": true, "receptionist": true, "unknown": true}
	validFollowupActions = map[string]bool{"follow_up": true, "abandon": true, "transfer_human": true, "schedule_callback": true}
)

// normalizeEnum 校验枚举值是否在允许范围内，不在则返回 map 中的第一个"未知"默认值。
func normalizeEnum(val string, allowed map[string]bool) string {
	if allowed[val] {
		return val
	}
	// 返回合理的默认值。
	for k := range allowed {
		if strings.Contains(k, "unknown") || strings.Contains(k, "not_mentioned") {
			return k
		}
	}
	// 兜底：返回传入值（不应到达此处）。
	return val
}

func gradeToScore(grade engine.Grade) int {
	switch grade {
	case engine.GradeA:
		return 85
	case engine.GradeB:
		return 60
	case engine.GradeC:
		return 30
	case engine.GradeD:
		return 10
	default:
		return 0
	}
}

func gradeToIntentType(grade engine.Grade) string {
	switch grade {
	case engine.GradeA:
		return "interested"
	case engine.GradeB:
		return "needs_info"
	case engine.GradeC, engine.GradeD:
		return "not_interested"
	default:
		return "unknown"
	}
}

func gradeToFollowupAction(grade engine.Grade) string {
	switch grade {
	case engine.GradeA:
		return "transfer_human"
	case engine.GradeB:
		return "follow_up"
	case engine.GradeC:
		return "follow_up"
	case engine.GradeD:
		return "abandon"
	default:
		return "abandon"
	}
}
