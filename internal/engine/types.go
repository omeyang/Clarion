// Package engine 定义引擎子系统共享的核心类型。
package engine

import (
	sonataengine "github.com/omeyang/Sonata/engine"
	"github.com/omeyang/Sonata/engine/mediafsm"
)

// ── 媒体状态机类型（Sonata 类型别名）──────────────────────────

// MediaState 表示媒体状态机中的状态（Sonata 类型别名）。
type MediaState = mediafsm.State

// 通话生命周期的媒体状态。
const (
	MediaIdle           = mediafsm.Idle
	MediaDialing        = mediafsm.Dialing
	MediaRinging        = mediafsm.Ringing
	MediaAMDDetecting   = mediafsm.AMDDetecting
	MediaBotSpeaking    = mediafsm.BotSpeaking
	MediaWaitingUser    = mediafsm.WaitingUser
	MediaUserSpeaking   = mediafsm.UserSpeaking
	MediaProcessing     = mediafsm.Processing
	MediaBargeIn        = mediafsm.BargeIn
	MediaSilenceTimeout = mediafsm.SilenceTimeout
	MediaHangup         = mediafsm.Hangup
	MediaPostProcessing = mediafsm.PostProcessing
)

// MediaEvent 触发媒体状态机中的状态转换（Sonata 类型别名）。
type MediaEvent = mediafsm.Event

// 触发状态转换的媒体事件。
const (
	EvDial              = mediafsm.EvDial
	EvRinging           = mediafsm.EvRinging
	EvDialFailed        = mediafsm.EvDialFailed
	EvAnswer            = mediafsm.EvAnswer
	EvRingTimeout       = mediafsm.EvRingTimeout
	EvAMDHuman          = mediafsm.EvAMDHuman
	EvAMDMachine        = mediafsm.EvAMDMachine
	EvBotDone           = mediafsm.EvBotDone
	EvSpeechStart       = mediafsm.EvSpeechStart
	EvSpeechEnd         = mediafsm.EvSpeechEnd
	EvBargeIn           = mediafsm.EvBargeIn
	EvBargeInDone       = mediafsm.EvBargeInDone
	EvProcessingDone    = mediafsm.EvProcessingDone
	EvSilenceTimeout    = mediafsm.EvSilenceTimeout
	EvSilencePromptDone = mediafsm.EvSilencePromptDone
	EvSecondSilence     = mediafsm.EvSecondSilence
	EvHangup            = mediafsm.EvHangup
	EvProcessingTimeout = mediafsm.EvProcessingTimeout
	EvPostDone          = mediafsm.EvPostDone
)

// ── 对话状态机类型（业务特有）─────────────────────────────────

// DialogueState 表示业务对话状态机中的状态。
type DialogueState int

// 业务对话流程的对话状态。
const (
	DialogueOpening              DialogueState = iota // 开场问候。
	DialogueQualification                             // 检查是否为目标受众。
	DialogueInformationGathering                      // 收集缺失字段。
	DialogueObjectionHandling                         // 处理用户异议。
	DialogueNextAction                                // 引导下一步行动。
	DialogueMarkForFollowup                           // 标记为人工跟进。
	DialogueClosing                                   // 结束对话。
)

func (s DialogueState) String() string {
	names := [...]string{
		"OPENING", "QUALIFICATION", "INFORMATION_GATHERING",
		"OBJECTION_HANDLING", "NEXT_ACTION", "MARK_FOR_FOLLOWUP", "CLOSING",
	}
	if int(s) < len(names) {
		return names[s]
	}
	return unknownStr
}

const unknownStr = "UNKNOWN"

// dialogueStateNames 对话状态名称到值的映射，用于从字符串解析。
var dialogueStateNames = map[string]DialogueState{
	"OPENING":               DialogueOpening,
	"QUALIFICATION":         DialogueQualification,
	"INFORMATION_GATHERING": DialogueInformationGathering,
	"OBJECTION_HANDLING":    DialogueObjectionHandling,
	"NEXT_ACTION":           DialogueNextAction,
	"MARK_FOR_FOLLOWUP":     DialogueMarkForFollowup,
	"CLOSING":               DialogueClosing,
}

// ParseDialogueState 从字符串解析对话状态。
// 无法识别时返回 DialogueOpening 和 false。
func ParseDialogueState(s string) (DialogueState, bool) {
	state, ok := dialogueStateNames[s]
	return state, ok
}

// ── 业务枚举类型 ─────────────────────────────────────────────

// Intent 表示 LLM 提取的用户意图。
type Intent string

// LLM 提取的用户意图值。
const (
	IntentContinue      Intent = "continue"
	IntentReject        Intent = "reject"
	IntentNotInterested Intent = "not_interested"
	IntentBusy          Intent = "busy"
	IntentAskDetail     Intent = "ask_detail"
	IntentInterested    Intent = "interested"
	IntentHesitate      Intent = "hesitate"
	IntentConfirm       Intent = "confirm"
	IntentSchedule      Intent = "schedule"
	IntentUnknown       Intent = "unknown"
)

// Grade 表示联系人评级结果。
type Grade string

// 联系人评级结果。
const (
	GradeA Grade = "A" // 高意向。
	GradeB Grade = "B" // 有一定意向，需跟进。
	GradeC Grade = "C" // 低意向。
	GradeD Grade = "D" // 无意向。
	GradeX Grade = "X" // 无效（未接听、语音信箱等）。
)

// CallStatus 表示通话记录的状态。
type CallStatus string

// 通话状态值。
const (
	CallPending     CallStatus = "pending"
	CallDialing     CallStatus = "dialing"
	CallRinging     CallStatus = "ringing"
	CallInProgress  CallStatus = "in_progress"
	CallCompleted   CallStatus = "completed"
	CallFailed      CallStatus = "failed"
	CallNoAnswer    CallStatus = "no_answer"
	CallBusy        CallStatus = "busy"
	CallVoicemail   CallStatus = "voicemail"
	CallInterrupted CallStatus = "interrupted"
)

// AnswerType 表示通话接听方类型。
type AnswerType string

// 接听类型值。
const (
	AnswerHuman     AnswerType = "human"
	AnswerVoicemail AnswerType = "voicemail"
	AnswerIVR       AnswerType = "ivr"
	AnswerUnknown   AnswerType = "unknown"
)

// TaskStatus 表示外呼任务的生命周期状态。
type TaskStatus string

// 任务状态值。
const (
	TaskDraft     TaskStatus = "draft"
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskPaused    TaskStatus = "paused"
	TaskCompleted TaskStatus = "completed"
	TaskCancelled TaskStatus = "cancelled"
)

// TemplateStatus 表示场景模板的生命周期状态。
type TemplateStatus string

// 模板状态值。
const (
	TemplateDraft     TemplateStatus = "draft"
	TemplateActive    TemplateStatus = "active"
	TemplatePublished TemplateStatus = "published"
	TemplateArchived  TemplateStatus = "archived"
)

// ── 事件类型（Sonata 类型别名 + Clarion 扩展）────────────────

// EventType 表示 call_events 表中的通话事件类型（Sonata 类型别名）。
type EventType = sonataengine.EventType

// Sonata 通用事件类型。
const (
	EventUserSpeechStart = sonataengine.EventUserSpeechStart
	EventUserSpeechEnd   = sonataengine.EventUserSpeechEnd
	EventBotSpeakStart   = sonataengine.EventBotSpeakStart
	EventBotSpeakEnd     = sonataengine.EventBotSpeakEnd
	EventBargeIn         = sonataengine.EventBargeIn
	EventSilenceTimeout  = sonataengine.EventSilenceTimeout
	EventASRError        = sonataengine.EventASRError
	EventLLMTimeout      = sonataengine.EventLLMTimeout
	EventTTSError        = sonataengine.EventTTSError
)

// Clarion 电话场景专有事件类型。
const (
	EventHangupByUser   EventType = "hangup_by_user"
	EventHangupBySystem EventType = "hangup_by_system"
	EventAMDResult      EventType = "amd_result"

	// 网络质量事件。
	EventPoorNetwork EventType = "poor_network_detected"
	EventAudioGap    EventType = "audio_gap_detected"
	EventLowVolume   EventType = "low_volume_detected"

	// 意外中断事件。
	EventUnexpectedDisconnect EventType = "unexpected_disconnect"
)

// RecordedEvent 记录带时间戳的媒体层事件（Sonata 类型别名）。
type RecordedEvent = sonataengine.RecordedEvent
