// Package rules 实现业务决策的规则引擎。
//
// 规则引擎位于 LLM 输出和对话 FSM 之间。
// LLM 提供结构化输出（意图、提取字段、建议回复）。
// 规则引擎做出确定性决策：是否继续、转换到哪个状态、
// 使用什么回复策略、以及分配什么评级。
package rules

import (
	"slices"

	"github.com/omeyang/clarion/internal/engine"
)

// LLMOutput 是 LLM 的结构化输出。
type LLMOutput struct {
	Intent          engine.Intent     `json:"intent"`
	ExtractedFields map[string]string `json:"extracted_fields"`
	ObjectionType   string            `json:"objection_type,omitempty"`
	SuggestedReply  string            `json:"suggested_reply"`
	Confidence      float64           `json:"confidence"`
}

// Decision 是规则引擎的输出。
type Decision struct {
	NextState     engine.DialogueState `json:"next_state"`
	ReplyStrategy ReplyStrategy        `json:"reply_strategy"`
	ReplyText     string               `json:"reply_text"`
	ShouldEnd     bool                 `json:"should_end"`
	ShouldNotify  bool                 `json:"should_notify"`
	Grade         engine.Grade         `json:"grade,omitempty"`
}

// ReplyStrategy 决定回复的生成方式。
type ReplyStrategy int

// 回复策略值。
const (
	ReplyTemplate    ReplyStrategy = iota // 使用预定义模板。
	ReplyLLM                              // 使用 LLM 建议的回复。
	ReplyPrecompiled                      // 使用预编译音频。
)

func (s ReplyStrategy) String() string {
	names := [...]string{"TEMPLATE", "LLM", "PRECOMPILED"}
	if int(s) < len(names) {
		return names[s]
	}
	return "UNKNOWN"
}

// TemplateConfig 持有与规则评估相关的场景模板配置。
type TemplateConfig struct {
	RequiredFields []string
	MaxObjections  int
	MaxTurns       int
	GradingRules   GradingRules
	Templates      map[string]string // state → reply template
}

// GradingRules 定义各评级等级的条件。
type GradingRules struct {
	AIntents        []engine.Intent
	AMinFields      int
	BMinFields      int
	BMinTurns       int
	RejectIntents   []engine.Intent
	InvalidStatuses []engine.CallStatus
}

// Engine 根据 LLM 输出和会话上下文评估规则。
type Engine struct {
	config TemplateConfig
}

// NewEngine 使用给定的模板配置创建规则引擎。
func NewEngine(config TemplateConfig) *Engine {
	return &Engine{config: config}
}

// Evaluate 处理 LLM 输出和会话上下文以产生 Decision。
func (e *Engine) Evaluate(llmOut LLMOutput, dctx *engine.DialogueContext) Decision {
	// 将提取的字段合并到已收集字段中。
	for k, v := range llmOut.ExtractedFields {
		if v != "" {
			dctx.CollectedFields[k] = v
		}
	}

	// 跟踪异议次数。
	if isObjection(llmOut.Intent) {
		dctx.ObjectionCount++
	}

	dctx.TurnCount++
	dctx.Intent = llmOut.Intent
	dctx.RequiredFields = e.config.RequiredFields
	dctx.MaxObjections = e.config.MaxObjections
	dctx.MaxTurns = e.config.MaxTurns

	// 判断是否为高价值线索。
	dctx.HighValue = e.isHighValue(llmOut, dctx)

	decision := Decision{}

	// 检查轮次上限。
	if dctx.TurnCount >= dctx.MaxTurns {
		decision.ShouldEnd = true
		decision.NextState = engine.DialogueClosing
		decision.ReplyStrategy = ReplyTemplate
		decision.ReplyText = e.templateFor(engine.DialogueClosing)
		return decision
	}

	// 决定回复策略。
	decision.ReplyStrategy = e.chooseReplyStrategy(llmOut, dctx)

	switch decision.ReplyStrategy {
	case ReplyLLM:
		decision.ReplyText = llmOut.SuggestedReply
	case ReplyTemplate:
		decision.ReplyText = e.templateFor(dctx.CurrentState)
	case ReplyPrecompiled:
		decision.ReplyText = "" // 预编译音频，无需文本。
	}

	// 设置下一状态（后续由 FSM.Advance 更新）。
	decision.NextState = dctx.CurrentState

	// 检查是否需要通知跟进。
	if dctx.HighValue && dctx.CurrentState == engine.DialogueMarkForFollowup {
		decision.ShouldNotify = true
	}

	// 检查是否进入结束状态。
	if dctx.CurrentState == engine.DialogueClosing {
		decision.ShouldEnd = true
	}

	return decision
}

// GradeCall 根据收集到的数据为通话生成最终评级。
func (e *Engine) GradeCall(dctx *engine.DialogueContext, callResult engine.CallStatus) engine.Grade {
	// 无效通话状态。
	if slices.Contains(e.config.GradingRules.InvalidStatuses, callResult) {
		return engine.GradeX
	}

	// 检查拒绝意图。
	if slices.Contains(e.config.GradingRules.RejectIntents, dctx.Intent) {
		return engine.GradeD
	}

	// A 级：高意图信号。
	collectedCount := len(dctx.CollectedFields)
	if slices.Contains(e.config.GradingRules.AIntents, dctx.Intent) &&
		collectedCount >= e.config.GradingRules.AMinFields {
		return engine.GradeA
	}

	// B 级：有一定兴趣。
	if collectedCount >= e.config.GradingRules.BMinFields &&
		dctx.TurnCount >= e.config.GradingRules.BMinTurns &&
		!isReject(dctx.Intent) {
		return engine.GradeB
	}

	// C 级：犹豫或低参与度。
	if dctx.Intent == engine.IntentHesitate || dctx.ObjectionCount > 0 {
		return engine.GradeC
	}

	return engine.GradeD
}

func (e *Engine) chooseReplyStrategy(llmOut LLMOutput, dctx *engine.DialogueContext) ReplyStrategy {
	if dctx.CurrentState == engine.DialogueClosing {
		return ReplyTemplate
	}
	// 有 LLM 建议回复且置信度足够时，优先使用 LLM 回复。
	if llmOut.SuggestedReply != "" && llmOut.Confidence >= 0.5 {
		return ReplyLLM
	}
	if llmOut.Intent == engine.IntentConfirm {
		return ReplyTemplate
	}
	if llmOut.Confidence < 0.5 {
		return ReplyTemplate
	}
	return ReplyLLM
}

func (e *Engine) templateFor(state engine.DialogueState) string {
	if tmpl, ok := e.config.Templates[state.String()]; ok {
		return tmpl
	}
	return "好的，感谢您的时间，再见！"
}

func (e *Engine) isHighValue(llmOut LLMOutput, dctx *engine.DialogueContext) bool {
	if slices.Contains(e.config.GradingRules.AIntents, llmOut.Intent) {
		return true
	}
	return len(dctx.CollectedFields) >= e.config.GradingRules.AMinFields
}

func isObjection(intent engine.Intent) bool {
	return intent == engine.IntentBusy ||
		intent == engine.IntentNotInterested ||
		intent == engine.IntentHesitate
}

func isReject(intent engine.Intent) bool {
	return intent == engine.IntentReject || intent == engine.IntentNotInterested
}
