package guard

import (
	"github.com/omeyang/clarion/internal/engine"
)

// OutputValidatorConfig 配置统一输出校验器。
// 各子校验器配置为 nil 时跳过对应校验。
type OutputValidatorConfig struct {
	Decision *DecisionValidatorConfig
	Output   *OutputCheckerConfig
	Response *ResponseValidatorConfig
	Content  *ContentCheckerConfig
}

// OutputValidatorInput 是统一输出校验器的输入。
// 包含 LLM 的结构化决策和文本回复两部分。
type OutputValidatorInput struct {
	// Intent 用户意图。
	Intent engine.Intent
	// Grade 客户评级。
	Grade engine.Grade
	// Instructions 注入给 RealtimeVoice 的指令。
	Instructions string
	// ShouldEnd 是否应结束通话。
	ShouldEnd bool
	// ExtractedFields 本轮新提取的字段。
	ExtractedFields map[string]string
	// State 当前对话状态，用于状态感知校验。
	State engine.DialogueState
	// ResponseText LLM 输出的回复文本。
	ResponseText string
}

// OutputValidatorResult 是统一输出校验的结果。
type OutputValidatorResult struct {
	// Valid 为 true 表示全部校验通过。
	Valid bool
	// Intent 修正后的意图。
	Intent engine.Intent
	// Grade 修正后的评级。
	Grade engine.Grade
	// Instructions 修正后的指令。
	Instructions string
	// ShouldEnd 修正后的结束标志。
	ShouldEnd bool
	// ExtractedFields 修正后的字段。
	ExtractedFields map[string]string
	// ResponseText 修正后的回复文本。
	ResponseText string
	// Violations 所有违规项。
	Violations []string
	// ContentIssues 内容校验发现的问题。
	ContentIssues []ContentIssue
}

// OutputValidator 统一输出校验器，组合 DecisionValidator、OutputChecker、
// ResponseValidator 和 ContentChecker，提供单次调用完成所有校验。
type OutputValidator struct {
	decVal     *DecisionValidator
	outChecker *OutputChecker
	respVal    *ResponseValidator
	contentChk *ContentChecker
}

// NewOutputValidator 创建统一输出校验器。
func NewOutputValidator(cfg OutputValidatorConfig) *OutputValidator {
	v := &OutputValidator{}
	if cfg.Decision != nil {
		v.decVal = NewDecisionValidator(*cfg.Decision)
	}
	if cfg.Output != nil {
		v.outChecker = NewOutputChecker(*cfg.Output)
	}
	if cfg.Response != nil {
		v.respVal = NewResponseValidator(*cfg.Response)
	}
	if cfg.Content != nil {
		v.contentChk = NewContentChecker(*cfg.Content)
	}
	return v
}

// Validate 对 LLM 输出进行全面校验，按顺序执行：
// 1. 决策字段校验（意图、评级、字段、指令）
// 2. 状态感知校验（当前状态下意图和结束操作是否合法）
// 3. 回复文本安全校验（AI 泄露、提示泄露、长度）
// 4. 内容质量校验（JSON 片段、代码块、Markdown、URL、PII）。
func (v *OutputValidator) Validate(input OutputValidatorInput) OutputValidatorResult {
	result := OutputValidatorResult{
		Valid:           true,
		Intent:          input.Intent,
		Grade:           input.Grade,
		Instructions:    input.Instructions,
		ShouldEnd:       input.ShouldEnd,
		ExtractedFields: input.ExtractedFields,
		ResponseText:    input.ResponseText,
	}

	v.validateDecision(&result)
	v.validateStateOutput(&result, input.State)
	v.validateResponseText(&result)
	v.validateContent(&result)

	return result
}

// validateDecision 执行决策字段校验。
func (v *OutputValidator) validateDecision(r *OutputValidatorResult) {
	if v.decVal == nil {
		return
	}
	dr := v.decVal.Validate(DecisionInput{
		Intent:          r.Intent,
		Grade:           r.Grade,
		Instructions:    r.Instructions,
		ShouldEnd:       r.ShouldEnd,
		ExtractedFields: r.ExtractedFields,
	})
	if !dr.Valid {
		r.Valid = false
		r.Violations = append(r.Violations, dr.Violations...)
	}
	r.Intent = dr.Sanitized.Intent
	r.Grade = dr.Sanitized.Grade
	r.Instructions = dr.Sanitized.Instructions
	r.ShouldEnd = dr.Sanitized.ShouldEnd
	r.ExtractedFields = dr.Sanitized.ExtractedFields
}

// validateStateOutput 执行状态感知校验。
func (v *OutputValidator) validateStateOutput(r *OutputValidatorResult, state engine.DialogueState) {
	if v.outChecker == nil {
		return
	}
	or := v.outChecker.Check(OutputCheckInput{
		Intent:       r.Intent,
		Grade:        r.Grade,
		Instructions: r.Instructions,
		ShouldEnd:    r.ShouldEnd,
		State:        state,
	})
	if !or.Valid {
		r.Valid = false
		r.Violations = append(r.Violations, or.Violations...)
	}
	r.Intent = or.Sanitized.Intent
	r.ShouldEnd = or.Sanitized.ShouldEnd
	r.Instructions = or.Sanitized.Instructions
}

// validateResponseText 执行回复文本安全校验。
func (v *OutputValidator) validateResponseText(r *OutputValidatorResult) {
	if v.respVal == nil || r.ResponseText == "" {
		return
	}
	rr := v.respVal.Validate(r.ResponseText)
	if !rr.OK {
		r.Valid = false
		for i, issue := range rr.Issues {
			r.Violations = append(r.Violations, rr.Reasons[i])
			// AI 泄露或提示泄露时替换为空，由调用方决定兜底文案。
			if issue == ResponseIssueAIDisclosure || issue == ResponseIssuePromptLeak {
				r.ResponseText = ""
				return
			}
		}
	}
	r.ResponseText = rr.Text
}

// validateContent 执行内容质量校验。
func (v *OutputValidator) validateContent(r *OutputValidatorResult) {
	if v.contentChk == nil || r.ResponseText == "" {
		return
	}
	cr := v.contentChk.Check(r.ResponseText)
	if !cr.OK {
		r.Valid = false
		r.ContentIssues = append(r.ContentIssues, cr.Issues...)
		r.Violations = append(r.Violations, cr.Reasons...)
	}
	// 清洗后非空才更新，避免丢失整段回复。
	if cr.Text != "" {
		r.ResponseText = cr.Text
	}
}
