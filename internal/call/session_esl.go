package call

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/omeyang/clarion/internal/engine"
)

// handleESLEvent 处理 FreeSWITCH 事件。
// 通过 clarion_session_id 通道变量或 Unique-ID 过滤，只处理属于本会话的事件。
func (s *Session) handleESLEvent(ctx context.Context, event ESLEvent, silenceTimer *time.Timer) bool {
	if !s.isMyESLEvent(event) {
		return false
	}

	switch event.Name {
	case "BACKGROUND_JOB":
		return s.handleBGJob(event)
	case "CHANNEL_PROGRESS":
		s.handleChannelProgress()
	case "CHANNEL_ANSWER":
		s.handleChannelAnswer(ctx, event)
	case "CHANNEL_HANGUP":
		s.handleHangup(event.Header("Hangup-Cause"))
		return true
	case "CHANNEL_HANGUP_COMPLETE":
		return true
	case "PLAYBACK_STOP":
		s.handlePlaybackStop(silenceTimer)
	case "DETECTED_SPEECH":
		s.handleASRResult(ctx, event)
	}

	return false
}

// isMyESLEvent 过滤事件，只保留属于本会话的事件。
func (s *Session) isMyESLEvent(event ESLEvent) bool {
	eventSessionID := event.Header("variable_clarion_session_id")
	eventUUID := event.UUID()

	s.logger.Debug("ESL event filtering",
		slog.String("event", event.Name),
		slog.String("event_session_id", eventSessionID),
		slog.String("my_session_id", s.cfg.SessionID),
		slog.String("event_uuid", eventUUID),
		slog.String("my_channel_uuid", s.channelUUID),
	)

	if eventSessionID != "" && eventSessionID != s.cfg.SessionID {
		s.logger.Debug("ESL event filtered out: session mismatch")
		return false
	}
	if eventSessionID == "" && s.channelUUID != "" && eventUUID != "" && eventUUID != s.channelUUID {
		s.logger.Debug("ESL event filtered out: UUID mismatch")
		return false
	}
	return true
}

// handleBGJob 处理 bgapi 异步结果。返回 true 表示会话应终止。
func (s *Session) handleBGJob(event ESLEvent) bool {
	if !strings.Contains(event.Body, "-ERR") {
		return false
	}
	cause := strings.TrimSpace(strings.TrimPrefix(event.Body, "-ERR "))
	s.logger.Error("originate 失败", slog.String("cause", cause))
	s.handleHangup(cause)
	return true
}

// handleChannelProgress 处理 CHANNEL_PROGRESS 事件。
func (s *Session) handleChannelProgress() {
	s.status = engine.CallRinging
	if err := s.mfsm.Handle(engine.EvRinging); err != nil {
		s.logger.Warn("handle ringing event", slog.String("error", err.Error()))
	}
}

// handleChannelAnswer 处理 CHANNEL_ANSWER 事件。
func (s *Session) handleChannelAnswer(ctx context.Context, event ESLEvent) {
	s.channelUUID = event.UUID()
	s.tryHandleFSMEvent(engine.EvAnswer, "handle answer event")

	// 启动 mod_audio_fork 将通话音频流式传输到 WebSocket。
	if s.cfg.ESL != nil && s.channelUUID != "" && s.cfg.AudioWSURL != "" {
		wsURL := fmt.Sprintf("%s?session_id=%s", s.cfg.AudioWSURL, s.cfg.SessionID)
		if forkErr := s.cfg.ESL.AudioForkStart(ctx, s.channelUUID, wsURL); forkErr != nil {
			s.logger.Error("audio fork start 失败", slog.String("error", forkErr.Error()))
		} else {
			s.logger.Info("audio fork 已启动", slog.String("ws_url", wsURL))
		}
	}
}

// handlePlaybackStop 处理 PLAYBACK_STOP 事件。
func (s *Session) handlePlaybackStop(silenceTimer *time.Timer) {
	s.ttsPlaying.Store(false)
	// 流式 TTS 模式下，由 synthesizeAndPlayStreamAsync goroutine 管理 botDone，
	// 此处仅标记播放停止，不触发 FSM 转换。
	if s.ttsStreaming.Load() {
		return
	}
	if s.mfsm.CanHandle(engine.EvBotDone) {
		if err := s.mfsm.Handle(engine.EvBotDone); err != nil {
			s.logger.Warn("handle bot done event", slog.String("error", err.Error()))
		}
		s.recordEvent(engine.EventBotSpeakEnd, nil)
		silenceTimer.Reset(time.Duration(s.cfg.Protection.MaxSilenceSec) * time.Second)
	}
}

// handleASRResult 处理语音识别结果。
func (s *Session) handleASRResult(ctx context.Context, event ESLEvent) {
	text := event.Body
	if text == "" {
		return
	}

	s.recordEvent(engine.EventUserSpeechEnd, map[string]string{"text": text})

	if s.mfsm.CanHandle(engine.EvSpeechEnd) {
		if err := s.mfsm.Handle(engine.EvSpeechEnd); err != nil {
			s.logger.Warn("handle speech end event", slog.String("error", err.Error()))
		}
	}

	// 通过对话引擎处理。
	if s.cfg.DialogueEngine == nil {
		if s.mfsm.CanHandle(engine.EvProcessingDone) {
			if err := s.mfsm.Handle(engine.EvProcessingDone); err != nil {
				s.logger.Warn("handle processing done event", slog.String("error", err.Error()))
			}
		}
		return
	}

	reply, err := s.cfg.DialogueEngine.ProcessUserInput(ctx, text)
	if err != nil {
		s.logger.Error("dialogue processing failed", slog.String("error", err.Error()))
	}

	if s.cfg.DialogueEngine.IsFinished() {
		s.handleHangup("dialogue_complete")
		return
	}

	s.recordEvent(engine.EventBotSpeakStart, map[string]string{"text": reply})

	if s.mfsm.CanHandle(engine.EvProcessingDone) {
		if err := s.mfsm.Handle(engine.EvProcessingDone); err != nil {
			s.logger.Warn("handle processing done event", slog.String("error", err.Error()))
		}
	}

	// 如果有 TTS 提供者，异步合成并播放回复。
	if s.cfg.TTS != nil && s.ctx != nil && reply != "" {
		s.synthesizeAndPlayAsync(reply)
	}
}
