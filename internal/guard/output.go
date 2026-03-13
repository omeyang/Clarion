package guard

import (
	"fmt"
	"unicode/utf8"

	"github.com/omeyang/clarion/internal/engine"
)

// OutputCheckInput 是待校验的策略决策输出。
// 调用方负责从 call.Decision 或其他类型转换。
type OutputCheckInput struct {
	Intent       engine.Intent
	Grade        engine.Grade
	Instructions string
	ShouldEnd    bool
	// State 当前对话状态，用于状态感知校验。
	State engine.DialogueState
}

// OutputCheckResult 是输出校验的结果。
type OutputCheckResult struct {
	// Valid 为 true 表示输出通过全部校验。
	Valid bool
	// Violations 记录所有违规项。
	Violations []string
	// Sanitized 是修正后的输出。
	Sanitized OutputCheckInput
}

// OutputCheckerConfig 配置状态感知输出校验器。
type OutputCheckerConfig struct {
	// StateIntents 每个对话状态允许的意图集合。
	// 空 map 表示使用 defaultStateIntents。
	StateIntents map[engine.DialogueState][]engine.Intent
	// EndableStates 允许触发结束通话的对话状态。
	// 空切片表示使用 defaultEndableStates。
	EndableStates []engine.DialogueState
	// MaxInstructionRunes 指令最大字符数。0 使用默认值。
	MaxInstructionRunes int
}

// 默认指令最大字符数。
const defaultOutputMaxInstructionRunes = 200

// defaultStateIntents 每个对话状态默认允许的意图。
// 限制各状态下可接受的意图，防止 LLM 输出越权动作。
var defaultStateIntents = map[engine.DialogueState][]engine.Intent{
	engine.DialogueOpening: {
		engine.IntentContinue, engine.IntentReject, engine.IntentBusy,
		engine.IntentUnknown,
	},
	engine.DialogueQualification: {
		engine.IntentContinue, engine.IntentReject, engine.IntentNotInterested,
		engine.IntentBusy, engine.IntentInterested, engine.IntentUnknown,
	},
	engine.DialogueInformationGathering: {
		engine.IntentContinue, engine.IntentReject, engine.IntentNotInterested,
		engine.IntentBusy, engine.IntentAskDetail, engine.IntentInterested,
		engine.IntentHesitate, engine.IntentConfirm, engine.IntentSchedule,
		engine.IntentUnknown,
	},
	engine.DialogueObjectionHandling: {
		engine.IntentContinue, engine.IntentReject, engine.IntentNotInterested,
		engine.IntentInterested, engine.IntentHesitate, engine.IntentUnknown,
	},
	engine.DialogueNextAction: {
		engine.IntentContinue, engine.IntentReject, engine.IntentConfirm,
		engine.IntentSchedule, engine.IntentInterested, engine.IntentUnknown,
	},
	engine.DialogueMarkForFollowup: {
		engine.IntentContinue, engine.IntentConfirm, engine.IntentUnknown,
	},
	engine.DialogueClosing: {
		engine.IntentContinue, engine.IntentConfirm, engine.IntentUnknown,
	},
}

// defaultEndableStates 默认允许结束通话的对话状态。
var defaultEndableStates = []engine.DialogueState{
	engine.DialogueNextAction,
	engine.DialogueMarkForFollowup,
	engine.DialogueClosing,
}

// OutputChecker 校验策略决策输出是否在当前对话状态许可范围内。
// 与 DecisionValidator 互补：DecisionValidator 校验字段格式和注入，
// OutputChecker 校验业务逻辑（状态机许可范围内的意图和动作）。
type OutputChecker struct {
	stateIntents        map[engine.DialogueState]map[engine.Intent]bool
	endableStates       map[engine.DialogueState]bool
	maxInstructionRunes int
}

// NewOutputChecker 创建状态感知输出校验器。
func NewOutputChecker(cfg OutputCheckerConfig) *OutputChecker {
	si := buildStateIntents(cfg.StateIntents)
	es := buildEndableStates(cfg.EndableStates)

	maxInst := cfg.MaxInstructionRunes
	if maxInst <= 0 {
		maxInst = defaultOutputMaxInstructionRunes
	}

	return &OutputChecker{
		stateIntents:        si,
		endableStates:       es,
		maxInstructionRunes: maxInst,
	}
}

// Check 校验输出并返回结果。
// 即使存在违规也会返回修正后的 Sanitized 输出。
func (c *OutputChecker) Check(input OutputCheckInput) OutputCheckResult {
	result := OutputCheckResult{
		Valid:     true,
		Sanitized: input,
	}

	c.checkStateIntent(&result)
	c.checkShouldEnd(&result)
	c.checkInstructionLength(&result)
	c.checkInstructionContent(&result)

	return result
}

// checkStateIntent 校验意图在当前对话状态下是否允许。
func (c *OutputChecker) checkStateIntent(r *OutputCheckResult) {
	intent := r.Sanitized.Intent
	state := r.Sanitized.State

	// 空意图替换为 unknown。
	if intent == "" {
		r.Sanitized.Intent = engine.IntentUnknown
		addOutputViolation(r, "意图为空，已替换为 unknown")
		return
	}

	allowed, hasState := c.stateIntents[state]
	if !hasState {
		// 未配置的状态默认放行。
		return
	}
	if !allowed[intent] {
		addOutputViolation(r, fmt.Sprintf(
			"状态 %s 下不允许意图 %q，已替换为 unknown", state, intent,
		))
		r.Sanitized.Intent = engine.IntentUnknown
	}
}

// checkShouldEnd 校验结束通话请求在当前状态下是否合理。
func (c *OutputChecker) checkShouldEnd(r *OutputCheckResult) {
	if !r.Sanitized.ShouldEnd {
		return
	}
	if !c.endableStates[r.Sanitized.State] {
		addOutputViolation(r, fmt.Sprintf(
			"状态 %s 下不允许结束通话，已忽略", r.Sanitized.State,
		))
		r.Sanitized.ShouldEnd = false
	}
}

// checkInstructionLength 校验指令长度。
func (c *OutputChecker) checkInstructionLength(r *OutputCheckResult) {
	inst := r.Sanitized.Instructions
	if utf8.RuneCountInString(inst) <= c.maxInstructionRunes {
		return
	}
	runes := []rune(inst)
	r.Sanitized.Instructions = string(runes[:c.maxInstructionRunes])
	addOutputViolation(r, fmt.Sprintf(
		"指令超过 %d 字符，已截断", c.maxInstructionRunes,
	))
}

// checkInstructionContent 检测指令中的可疑注入内容。
func (c *OutputChecker) checkInstructionContent(r *OutputCheckResult) {
	inst := r.Sanitized.Instructions
	if inst == "" {
		return
	}
	for _, re := range instructionDenyPatterns {
		if re.MatchString(inst) {
			r.Sanitized.Instructions = ""
			addOutputViolation(r, "指令包含可疑注入内容，已清除")
			return
		}
	}
}

// addOutputViolation 添加违规记录并标记为无效。
func addOutputViolation(r *OutputCheckResult, msg string) {
	r.Valid = false
	r.Violations = append(r.Violations, msg)
}

// buildStateIntents 构建状态→意图集合映射。
func buildStateIntents(custom map[engine.DialogueState][]engine.Intent) map[engine.DialogueState]map[engine.Intent]bool {
	src := defaultStateIntents
	if len(custom) > 0 {
		src = custom
	}
	result := make(map[engine.DialogueState]map[engine.Intent]bool, len(src))
	for state, intents := range src {
		m := make(map[engine.Intent]bool, len(intents))
		for _, intent := range intents {
			m[intent] = true
		}
		result[state] = m
	}
	return result
}

// buildEndableStates 构建允许结束通话的状态集合。
func buildEndableStates(custom []engine.DialogueState) map[engine.DialogueState]bool {
	src := defaultEndableStates
	if len(custom) > 0 {
		src = custom
	}
	result := make(map[engine.DialogueState]bool, len(src))
	for _, s := range src {
		result[s] = true
	}
	return result
}
