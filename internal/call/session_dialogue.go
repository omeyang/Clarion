package call

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
)

// startDialogue 发送开场文本以开始对话。
// 如果 TTS 提供者可用，异步合成并播放开场白。
// 恢复呼叫时自动恢复对话状态并使用恢复开场白。
func (s *Session) startDialogue() {
	if s.cfg.DialogueEngine == nil {
		return
	}

	// 恢复呼叫：从快照恢复对话状态，使用恢复开场白。
	if snap := s.cfg.RestoredSnapshot; snap != nil {
		s.cfg.DialogueEngine.RestoreFromSnapshot(dialogue.SnapshotData{
			DialogueState:   snap.DialogueState,
			Turns:           snap.Turns,
			CollectedFields: snap.CollectedFields,
		})
		s.logger.Info("从快照恢复对话状态",
			slog.Int64("original_call_id", snap.CallID),
			slog.String("state", snap.DialogueState),
			slog.String("cause", snap.InterruptCause),
		)
	}

	opening := s.openingText()
	s.logger.Info("对话开始", slog.String("text", opening))
	s.recordEvent(engine.EventBotSpeakStart, map[string]string{"text": opening})

	// 启动 ASR 流（用于后续用户语音识别）。
	s.startASRStream()

	// 如果有 TTS 提供者，异步合成并播放；否则直接跳过。
	if s.cfg.TTS != nil && s.ctx != nil {
		s.synthesizeAndPlayAsync(opening)
		return
	}

	// 无 TTS 时直接发出完成信号（测试兼容）。
	s.recordEvent(engine.EventBotSpeakEnd, nil)
	if s.mfsm.CanHandle(engine.EvBotDone) {
		if err := s.mfsm.Handle(engine.EvBotDone); err != nil {
			s.logger.Warn("handle bot done event", slog.String("error", err.Error()))
		}
	}
}

// openingText 返回本次通话的开场白文本。
// 恢复呼叫时使用恢复开场白，否则使用对话引擎的默认开场白。
func (s *Session) openingText() string {
	if s.cfg.RestoredSnapshot != nil {
		return s.cfg.DialogueEngine.GetRecoveryOpeningText()
	}
	return s.cfg.DialogueEngine.GetOpeningText()
}

// handleSilenceTimeout 处理静默超时事件。
func (s *Session) handleSilenceTimeout(silenceTimer *time.Timer) {
	s.silenceCount++
	s.recordEvent(engine.EventSilenceTimeout, map[string]string{
		"count": strconv.Itoa(s.silenceCount),
	})

	s.tryHandleFSMEvent(engine.EvSilenceTimeout, "handle silence timeout event")

	if s.silenceCount >= 2 {
		s.tryHandleFSMEvent(engine.EvSecondSilence, "handle second silence event")
		s.status = engine.CallCompleted
		return
	}

	// 播放静默提示音，然后重置计时器。
	s.tryHandleFSMEvent(engine.EvSilencePromptDone, "handle silence prompt done event")
	silenceTimer.Reset(time.Duration(s.cfg.Protection.MaxSilenceSec) * time.Second)
}
