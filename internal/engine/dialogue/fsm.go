// Package dialogue 实现业务对话状态机和对话引擎。
//
// 对话 FSM 控制"说什么"，媒体 FSM 控制"何时说"。
// FSM 由规则引擎基于 LLM 输出（意图、提取字段）驱动。
package dialogue

import (
	"errors"
	"fmt"
	"sync"

	"github.com/omeyang/clarion/internal/engine"
)

// ErrInvalidTransition 在转换无效时返回。
var ErrInvalidTransition = errors.New("invalid dialogue transition")

// TransitionRule 定义基于条件的状态转换规则。
type TransitionRule struct {
	From      engine.DialogueState
	To        engine.DialogueState
	Condition func(ctx *engine.DialogueContext) bool
}

// DefaultRules 返回与设计文档第 2 节对应的标准转换规则。
func DefaultRules() []TransitionRule {
	var rules []TransitionRule
	rules = append(rules, openingRules()...)
	rules = append(rules, qualificationRules()...)
	rules = append(rules, informationGatheringRules()...)
	rules = append(rules, objectionHandlingRules()...)
	rules = append(rules, nextActionRules()...)
	rules = append(rules, followupRules()...)
	return rules
}

// intentIn 当意图匹配给定意图之一时返回 true。
func intentIn(intent engine.Intent, intents ...engine.Intent) bool {
	for _, i := range intents {
		if intent == i {
			return true
		}
	}
	return false
}

// openingRules 返回开场状态的转换规则。
func openingRules() []TransitionRule {
	return []TransitionRule{
		{
			From: engine.DialogueOpening,
			To:   engine.DialogueQualification,
			Condition: func(ctx *engine.DialogueContext) bool {
				return intentIn(ctx.Intent,
					engine.IntentContinue, engine.IntentConfirm,
					engine.IntentInterested, engine.IntentAskDetail)
			},
		},
		{
			From: engine.DialogueOpening,
			To:   engine.DialogueClosing,
			Condition: func(ctx *engine.DialogueContext) bool {
				return intentIn(ctx.Intent, engine.IntentReject, engine.IntentNotInterested)
			},
		},
		{
			From: engine.DialogueOpening,
			To:   engine.DialogueObjectionHandling,
			Condition: func(ctx *engine.DialogueContext) bool {
				return ctx.Intent == engine.IntentBusy
			},
		},
	}
}

// qualificationRules 返回资质确认状态的转换规则。
func qualificationRules() []TransitionRule {
	return []TransitionRule{
		{
			From: engine.DialogueQualification,
			To:   engine.DialogueInformationGathering,
			Condition: func(ctx *engine.DialogueContext) bool {
				return !ctx.HasAllRequiredFields() &&
					intentIn(ctx.Intent, engine.IntentContinue, engine.IntentConfirm, engine.IntentInterested)
			},
		},
		{
			From: engine.DialogueQualification,
			To:   engine.DialogueNextAction,
			Condition: func(ctx *engine.DialogueContext) bool {
				return ctx.HasAllRequiredFields() &&
					!intentIn(ctx.Intent, engine.IntentReject, engine.IntentNotInterested)
			},
		},
		{
			From: engine.DialogueQualification,
			To:   engine.DialogueObjectionHandling,
			Condition: func(ctx *engine.DialogueContext) bool {
				return intentIn(ctx.Intent, engine.IntentBusy, engine.IntentHesitate, engine.IntentNotInterested)
			},
		},
		{
			From: engine.DialogueQualification,
			To:   engine.DialogueClosing,
			Condition: func(ctx *engine.DialogueContext) bool {
				return ctx.Intent == engine.IntentReject
			},
		},
	}
}

// informationGatheringRules 返回信息收集状态的转换规则。
func informationGatheringRules() []TransitionRule {
	return []TransitionRule{
		{
			From: engine.DialogueInformationGathering,
			To:   engine.DialogueInformationGathering,
			Condition: func(ctx *engine.DialogueContext) bool {
				return !ctx.HasAllRequiredFields() &&
					!intentIn(ctx.Intent,
						engine.IntentReject, engine.IntentNotInterested,
						engine.IntentBusy, engine.IntentHesitate)
			},
		},
		{
			From: engine.DialogueInformationGathering,
			To:   engine.DialogueNextAction,
			Condition: func(ctx *engine.DialogueContext) bool {
				return ctx.HasAllRequiredFields()
			},
		},
		{
			From: engine.DialogueInformationGathering,
			To:   engine.DialogueObjectionHandling,
			Condition: func(ctx *engine.DialogueContext) bool {
				return intentIn(ctx.Intent, engine.IntentBusy, engine.IntentHesitate, engine.IntentNotInterested)
			},
		},
		{
			From: engine.DialogueInformationGathering,
			To:   engine.DialogueClosing,
			Condition: func(ctx *engine.DialogueContext) bool {
				return ctx.Intent == engine.IntentReject
			},
		},
	}
}

// objectionHandlingRules 返回异议处理状态的转换规则。
func objectionHandlingRules() []TransitionRule {
	return []TransitionRule{
		{
			From: engine.DialogueObjectionHandling,
			To:   engine.DialogueInformationGathering,
			Condition: func(ctx *engine.DialogueContext) bool {
				return !ctx.HasAllRequiredFields() &&
					ctx.ObjectionCount < ctx.MaxObjections &&
					intentIn(ctx.Intent, engine.IntentContinue, engine.IntentConfirm)
			},
		},
		{
			From: engine.DialogueObjectionHandling,
			To:   engine.DialogueNextAction,
			Condition: func(ctx *engine.DialogueContext) bool {
				return ctx.HasAllRequiredFields() &&
					ctx.ObjectionCount < ctx.MaxObjections &&
					intentIn(ctx.Intent, engine.IntentContinue, engine.IntentConfirm)
			},
		},
		{
			From: engine.DialogueObjectionHandling,
			To:   engine.DialogueMarkForFollowup,
			Condition: func(ctx *engine.DialogueContext) bool {
				return ctx.HighValue && ctx.ObjectionCount >= ctx.MaxObjections
			},
		},
		{
			From: engine.DialogueObjectionHandling,
			To:   engine.DialogueClosing,
			Condition: func(ctx *engine.DialogueContext) bool {
				return ctx.Intent == engine.IntentReject ||
					ctx.ObjectionCount >= ctx.MaxObjections
			},
		},
	}
}

// nextActionRules 返回下一步行动状态的转换规则。
func nextActionRules() []TransitionRule {
	return []TransitionRule{
		{
			From: engine.DialogueNextAction,
			To:   engine.DialogueMarkForFollowup,
			Condition: func(ctx *engine.DialogueContext) bool {
				return ctx.HighValue || ctx.Intent == engine.IntentSchedule
			},
		},
		{
			From: engine.DialogueNextAction,
			To:   engine.DialogueClosing,
			Condition: func(_ *engine.DialogueContext) bool {
				return true // 兜底规则
			},
		},
	}
}

// followupRules 返回标记跟进状态的转换规则。
func followupRules() []TransitionRule {
	return []TransitionRule{
		{
			From: engine.DialogueMarkForFollowup,
			To:   engine.DialogueClosing,
			Condition: func(_ *engine.DialogueContext) bool {
				return true
			},
		},
	}
}

// FSM 是业务对话状态机。
type FSM struct {
	mu    sync.RWMutex
	state engine.DialogueState
	rules []TransitionRule
}

// NewFSM 创建以给定初始状态和规则的对话 FSM。
func NewFSM(initial engine.DialogueState, rules []TransitionRule) *FSM {
	return &FSM{
		state: initial,
		rules: rules,
	}
}

// State 返回当前对话状态。
func (f *FSM) State() engine.DialogueState {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.state
}

// Advance 根据上下文评估规则并转换到下一状态。
// 找到匹配规则时返回新状态和 nil，否则返回错误。
func (f *FSM) Advance(ctx *engine.DialogueContext) (engine.DialogueState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ctx.CurrentState = f.state

	for _, rule := range f.rules {
		if rule.From == f.state && rule.Condition(ctx) {
			f.state = rule.To
			return f.state, nil
		}
	}

	return f.state, fmt.Errorf("%w: no matching rule for state %s with intent %s",
		ErrInvalidTransition, f.state, ctx.Intent)
}

// ForceState 直接设置状态（用于测试或错误恢复）。
func (f *FSM) ForceState(s engine.DialogueState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = s
}

// IsTerminal 当对话处于结束状态时返回 true。
func (f *FSM) IsTerminal() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.state == engine.DialogueClosing
}
