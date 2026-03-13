package guard

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/omeyang/clarion/internal/engine"
)

// DecisionInput 是待校验的 LLM 决策输出。
// 与 strategy.Decision 解耦，调用方负责转换。
type DecisionInput struct {
	Intent          engine.Intent
	Grade           engine.Grade
	Instructions    string
	ShouldEnd       bool
	ExtractedFields map[string]string
}

// ValidationResult 是决策校验的结果。
type ValidationResult struct {
	// Valid 为 true 表示决策通过全部校验。
	Valid bool
	// Violations 记录所有违规项。
	Violations []string
	// Sanitized 是经过修正后的决策（截断过长字段、替换非法值）。
	Sanitized DecisionInput
}

// DecisionValidatorConfig 配置决策校验器。
type DecisionValidatorConfig struct {
	// AllowedIntents 允许的意图列表。空列表表示使用默认意图集。
	AllowedIntents []engine.Intent
	// AllowedGrades 允许的评级列表。空列表表示使用默认评级集。
	AllowedGrades []engine.Grade
	// MaxInstructionRunes 指令最大字符数。0 使用默认值。
	MaxInstructionRunes int
	// MaxFieldValueRunes 单个字段值最大字符数。0 使用默认值。
	MaxFieldValueRunes int
	// MaxFields 最大字段数量。0 使用默认值。
	MaxFields int
}

const (
	defaultMaxInstructionRunes = 200
	defaultMaxFieldValueRunes  = 100
	defaultMaxFields           = 20
)

// defaultIntents 是默认允许的意图集合。
var defaultIntents = []engine.Intent{
	engine.IntentContinue,
	engine.IntentReject,
	engine.IntentNotInterested,
	engine.IntentBusy,
	engine.IntentAskDetail,
	engine.IntentInterested,
	engine.IntentHesitate,
	engine.IntentConfirm,
	engine.IntentSchedule,
	engine.IntentUnknown,
}

// defaultGrades 是默认允许的评级集合。
var defaultGrades = []engine.Grade{
	engine.GradeA,
	engine.GradeB,
	engine.GradeC,
	engine.GradeD,
	engine.GradeX,
}

// instructionDenyPatterns 检测指令中的可疑内容。
var instructionDenyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)忘记.{0,10}指令`),
	regexp.MustCompile(`(?i)忽略.{0,10}(规则|限制|指令)`),
	regexp.MustCompile(`(?i)你现在是`),
	regexp.MustCompile(`(?i)ignore.{0,10}instructions`),
	regexp.MustCompile(`(?i)system\s*prompt`),
	regexp.MustCompile(`(?i)override`),
	regexp.MustCompile(`(?i)jailbreak`),
}

// DecisionValidator 校验 LLM 策略决策输出是否在许可范围内。
type DecisionValidator struct {
	allowedIntents      map[engine.Intent]bool
	allowedGrades       map[engine.Grade]bool
	maxInstructionRunes int
	maxFieldValueRunes  int
	maxFields           int
}

// NewDecisionValidator 创建决策校验器。
func NewDecisionValidator(cfg DecisionValidatorConfig) *DecisionValidator {
	intents := cfg.AllowedIntents
	if len(intents) == 0 {
		intents = defaultIntents
	}
	grades := cfg.AllowedGrades
	if len(grades) == 0 {
		grades = defaultGrades
	}

	im := make(map[engine.Intent]bool, len(intents))
	for _, v := range intents {
		im[v] = true
	}
	gm := make(map[engine.Grade]bool, len(grades))
	for _, v := range grades {
		gm[v] = true
	}

	maxInst := cfg.MaxInstructionRunes
	if maxInst <= 0 {
		maxInst = defaultMaxInstructionRunes
	}
	maxFV := cfg.MaxFieldValueRunes
	if maxFV <= 0 {
		maxFV = defaultMaxFieldValueRunes
	}
	maxF := cfg.MaxFields
	if maxF <= 0 {
		maxF = defaultMaxFields
	}

	return &DecisionValidator{
		allowedIntents:      im,
		allowedGrades:       gm,
		maxInstructionRunes: maxInst,
		maxFieldValueRunes:  maxFV,
		maxFields:           maxF,
	}
}

// Validate 校验决策并返回结果。
// 即使存在违规也会返回修正后的 Sanitized 决策，调用方可选择使用。
func (v *DecisionValidator) Validate(d DecisionInput) ValidationResult {
	result := ValidationResult{
		Valid:     true,
		Sanitized: d,
	}

	v.validateIntent(&result)
	v.validateGrade(&result)
	v.validateInstructions(&result)
	v.validateFields(&result)

	return result
}

// validateIntent 校验意图是否在允许列表中。
func (v *DecisionValidator) validateIntent(r *ValidationResult) {
	intent := r.Sanitized.Intent
	if intent == "" {
		r.Sanitized.Intent = engine.IntentUnknown
		r.addViolation("意图为空，已替换为 unknown")
		return
	}
	if !v.allowedIntents[intent] {
		r.addViolation(fmt.Sprintf("意图 %q 不在允许列表中，已替换为 unknown", intent))
		r.Sanitized.Intent = engine.IntentUnknown
	}
}

// validateGrade 校验评级是否合法。
func (v *DecisionValidator) validateGrade(r *ValidationResult) {
	grade := r.Sanitized.Grade
	if grade == "" {
		// 空评级允许（某些轮次不更新评级）。
		return
	}
	if !v.allowedGrades[grade] {
		r.addViolation(fmt.Sprintf("评级 %q 不合法，已替换为 C", grade))
		r.Sanitized.Grade = engine.GradeC
	}
}

// validateInstructions 校验指令长度和内容。
func (v *DecisionValidator) validateInstructions(r *ValidationResult) {
	inst := r.Sanitized.Instructions

	// 长度截断。
	if utf8.RuneCountInString(inst) > v.maxInstructionRunes {
		runes := []rune(inst)
		r.Sanitized.Instructions = string(runes[:v.maxInstructionRunes])
		r.addViolation(fmt.Sprintf("指令超过 %d 字符，已截断", v.maxInstructionRunes))
	}

	// 可疑内容检测。
	for _, re := range instructionDenyPatterns {
		if re.MatchString(inst) {
			r.Sanitized.Instructions = ""
			r.addViolation("指令包含可疑内容，已清除")
			return
		}
	}
}

// validateFields 校验提取字段的数量和值长度。
func (v *DecisionValidator) validateFields(r *ValidationResult) {
	fields := r.Sanitized.ExtractedFields
	if len(fields) == 0 {
		return
	}

	if len(fields) > v.maxFields {
		r.addViolation(fmt.Sprintf("字段数量 %d 超过上限 %d，已截断", len(fields), v.maxFields))
		trimmed := make(map[string]string, v.maxFields)
		count := 0
		for k, val := range fields {
			if count >= v.maxFields {
				break
			}
			trimmed[k] = val
			count++
		}
		fields = trimmed
		r.Sanitized.ExtractedFields = fields
	}

	// 逐字段校验值长度。
	for k, val := range fields {
		if utf8.RuneCountInString(val) > v.maxFieldValueRunes {
			runes := []rune(val)
			fields[k] = string(runes[:v.maxFieldValueRunes])
			r.addViolation(fmt.Sprintf("字段 %q 值超过 %d 字符，已截断", k, v.maxFieldValueRunes))
		}
		// 字段名不允许包含控制字符或过长。
		if strings.ContainsFunc(k, func(r rune) bool { return r < 32 }) {
			r.addViolation(fmt.Sprintf("字段名 %q 包含控制字符", k))
		}
	}
}

// addViolation 添加违规记录并标记为无效。
func (r *ValidationResult) addViolation(msg string) {
	r.Valid = false
	r.Violations = append(r.Violations, msg)
}
