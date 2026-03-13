// Package media 提供媒体状态机（MSM）。
//
// MSM 管理通话的音频交互生命周期：
// IDLE → DIALING → RINGING → AMD → BOT_SPEAKING ↔ USER_SPEAKING → HANGUP。
//
// 底层实现委托给 Sonata 核心库的通用 FSM，
// 本包提供电话场景的便捷构造函数。
package media

import (
	smedia "github.com/omeyang/Sonata/engine/mediafsm"

	"github.com/omeyang/clarion/internal/engine"
)

// FSM 是 Sonata 媒体状态机的类型别名。
type FSM = smedia.FSM

// Callback 在状态转换成功后被调用（Sonata 类型别名）。
type Callback = smedia.Callback

// ErrInvalidTransition 在当前状态无法处理事件时返回。
var ErrInvalidTransition = smedia.ErrInvalidTransition

// Option 是 Sonata 媒体 FSM 选项的类型别名。
type Option = smedia.Option

// Unsynced 禁用 FSM 的 RWMutex 同步，适用于单 goroutine 事件循环。
var Unsynced = smedia.Unsynced

// NewFSM 创建电话场景的媒体 FSM。
// 使用 Sonata 的 PhoneTransitions 作为默认转换规则。
func NewFSM(initial engine.MediaState, opts ...Option) *FSM {
	return smedia.NewFSM(initial, smedia.PhoneTransitions(), opts...)
}
