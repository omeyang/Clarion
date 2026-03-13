package call

import (
	"context"
	"log/slog"
	"time"

	"github.com/omeyang/Sonata/engine/pcm"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/provider"
)

// injectionFallbackReply 是检测到注入攻击时的安全回复。
const injectionFallbackReply = "好的，我们继续聊正事吧。"

// handleAudioFrame 处理传入的音频用于 ASR 或 AMD。
func (s *Session) handleAudioFrame(ctx context.Context, frame []byte, silenceTimer *time.Timer) {
	// 网络质量埋点：记录每帧到达并处理质量事件。
	s.recordNetworkQuality(frame)

	state := s.mfsm.State()

	switch state {
	case engine.MediaAMDDetecting:
		s.handleAMDFrame(frame)
	case engine.MediaWaitingUser:
		s.feedASR(ctx, frame)
		s.handleWaitingUserFrame(frame, silenceTimer)
	case engine.MediaUserSpeaking:
		s.feedASR(ctx, frame)
	case engine.MediaBotSpeaking:
		s.handleBargeInFrame(ctx, frame)
	case engine.MediaIdle, engine.MediaDialing, engine.MediaRinging,
		engine.MediaProcessing, engine.MediaBargeIn,
		engine.MediaSilenceTimeout, engine.MediaHangup, engine.MediaPostProcessing:
		// 这些状态下不处理音频。
	}
}

// handleAMDFrame 在 AMD 检测期间处理音频帧。
func (s *Session) handleAMDFrame(frame []byte) {
	if !s.cfg.AMDConfig.Enabled {
		s.transitionToHuman()
		return
	}

	energy := pcm.EnergyDBFS(frame)
	frameMs := pcm.FrameDuration(frame, 8000)

	// 懒初始化：确保同一次 AMD 检测期间累积所有帧数据。
	if s.amdDetector == nil {
		s.amdDetector = NewAMDDetectorTestable(s.cfg.AMDConfig)
	}
	s.amdDetector.FeedFrame(energy, frameMs)

	if !s.amdDetector.Decided() {
		return
	}

	s.ansType = s.amdDetector.Result()
	if s.ansType == engine.AnswerHuman {
		s.handleFSMEvent(engine.EvAMDHuman, "handle AMD human event")
		s.answered = true
		s.status = engine.CallInProgress
		s.recordEvent(engine.EventAMDResult, map[string]string{"result": "human"})
		s.startDialogue()
		return
	}

	s.handleFSMEvent(engine.EvAMDMachine, "handle AMD machine event")
	s.status = engine.CallVoicemail
	s.recordEvent(engine.EventAMDResult, map[string]string{"result": "voicemail"})
}

// transitionToHuman 跳过 AMD 并将应答视为人类。
func (s *Session) transitionToHuman() {
	s.handleFSMEvent(engine.EvAMDHuman, "handle AMD human event")
	s.ansType = engine.AnswerHuman
	s.answered = true
	s.status = engine.CallInProgress
	s.startDialogue()
}

// handleWaitingUserFrame 在等待用户说话时检测语音起始。
// 使用 VAD 检测人声，避免环境噪音误触发。
func (s *Session) handleWaitingUserFrame(frame []byte, silenceTimer *time.Timer) {
	if !s.isSpeechFrame(frame) {
		return
	}

	s.handleFSMEvent(engine.EvSpeechStart, "handle speech start event")
	s.recordEvent(engine.EventUserSpeechStart, nil)
	silenceTimer.Reset(time.Duration(s.cfg.Protection.MaxSilenceSec) * time.Second)
}

// bargeInThreshold 是触发 barge-in 需要的连续活跃帧数（约 200ms）。
// 较高的值可防止环境噪音误触发打断。
const bargeInThreshold = 10

// handleBargeInFrame 在机器人说话时检测打断。
// 仅在 TTS 音频实际播放中才检测，防止合成阶段误触发。
// 使用 WebRTC VAD 检测人声，而非纯能量阈值，避免环境噪音误触发。
// 需要连续多帧 VAD 判定为人声才触发。
func (s *Session) handleBargeInFrame(ctx context.Context, frame []byte) {
	// TTS 未在播放时忽略（合成阶段不触发 barge-in）。
	if !s.ttsPlaying.Load() {
		return
	}

	if !s.isSpeechFrame(frame) {
		s.bargeInFrames = 0
		return
	}

	s.bargeInFrames++
	if s.bargeInFrames < bargeInThreshold {
		return
	}
	s.bargeInFrames = 0

	s.logger.Info("barge-in 触发（VAD 检测到人声）")
	s.ttsPlaying.Store(false)
	s.handleFSMEvent(engine.EvBargeIn, "handle barge-in event")
	s.recordEvent(engine.EventBargeIn, nil)

	// 取消进行中的 TTS 合成。
	if s.ttsCancel != nil {
		s.ttsCancel()
		s.ttsCancel = nil
	}

	// 停止 FreeSWITCH 当前播放。
	if s.cfg.ESL != nil && s.channelUUID != "" {
		if err := s.cfg.ESL.Break(ctx, s.channelUUID); err != nil {
			s.logger.Warn("break playback", slog.String("error", err.Error()))
		}
	}

	s.handleFSMEvent(engine.EvBargeInDone, "handle barge-in done event")
	s.recordEvent(engine.EventUserSpeechStart, nil)
}

// isSpeechFrame 判断音频帧是否包含人声。
// 优先使用注入的 SpeechDetector，不可用时退回能量阈值检测。
// FreeSWITCH 输出 8kHz PCM，SpeechDetector（如 SileroVAD）需要 16kHz，因此先重采样。
func (s *Session) isSpeechFrame(frame []byte) bool {
	if s.speechDetector != nil {
		s.vadResampleBuf = pcm.Resample8to16Into(s.vadResampleBuf, frame)
		if s.vadResampleBuf == nil {
			s.logger.Debug("重采样失败，退回能量检测", slog.Int("frame_size", len(frame)))
			return pcm.EnergyDBFS(frame) > s.cfg.AMDConfig.EnergyThresholdDBFS
		}
		speech, err := s.speechDetector.IsSpeech(s.vadResampleBuf)
		if err == nil {
			return speech
		}
		s.logger.Debug("SpeechDetector 处理失败，退回能量检测", slog.String("error", err.Error()))
	}
	return pcm.EnergyDBFS(frame) > s.cfg.AMDConfig.EnergyThresholdDBFS
}

// feedASR 将音频帧重采样后送入流式 ASR。
// FreeSWITCH 输出 8 kHz PCM，ASR 需要 16 kHz。
func (s *Session) feedASR(ctx context.Context, frame []byte) {
	if s.asrStream == nil {
		return
	}
	s.resampleBuf = pcm.Resample8to16Into(s.resampleBuf, frame)
	if s.resampleBuf == nil {
		return
	}
	if err := s.asrStream.FeedAudio(ctx, s.resampleBuf); err != nil {
		s.logger.Warn("ASR feed 失败", slog.String("error", err.Error()))
	}
}

// startASRStream 初始化流式 ASR 会话，并启动 goroutine 将事件转发到 asrResults 通道。
func (s *Session) startASRStream() {
	if s.cfg.ASR == nil || s.ctx == nil {
		return
	}

	if !s.asrBreaker.Allow() {
		s.logger.Warn("ASR 熔断器打开，跳过 ASR 流启动")
		return
	}

	stream, err := s.cfg.ASR.StartStream(s.ctx, s.cfg.ASRConfig)
	if err != nil {
		s.asrBreaker.RecordFailure()
		s.logger.Error("ASR 流启动失败", slog.String("error", err.Error()))
		return
	}
	s.asrBreaker.RecordSuccess()
	s.asrStream = stream

	go func() {
		for evt := range stream.Events() {
			select {
			case s.asrResults <- evt:
			default:
				s.logger.Warn("ASR 事件通道已满，丢弃事件")
			}
		}
	}()
}

// filterInput 通过 InputFilter 过滤用户输入。
// 注入攻击被拦截时返回空字符串，并用安全回复代替正常对话。
// 未启用过滤器时原样返回。
func (s *Session) filterInput(text string) string {
	if s.cfg.InputFilter == nil {
		return text
	}
	result := s.cfg.InputFilter.Filter(text)
	if result.Blocked {
		s.logger.Warn("输入被安全过滤器拦截",
			slog.String("flag", string(result.Flag)),
			slog.String("reason", result.Reason),
		)
		s.recordEvent(engine.EventUserSpeechEnd, map[string]string{
			"text":   text,
			"safety": string(result.Flag),
		})
		s.handleInjectionFallback()
		return ""
	}
	return result.Text
}

// handleInjectionFallback 在注入被拦截时播放安全回复。
func (s *Session) handleInjectionFallback() {
	s.tryHandleFSMEvent(engine.EvSpeechEnd, "handle speech end (injection blocked)")
	s.recordEvent(engine.EventBotSpeakStart, map[string]string{"text": injectionFallbackReply})

	if s.cfg.TTS != nil && s.ctx != nil {
		s.synthesizeAndPlayAsync(injectionFallbackReply)
		return
	}

	// 无 TTS 时直接完成。
	s.tryHandleFSMEvent(engine.EvProcessingDone, "handle processing done (injection)")
	s.tryHandleFSMEvent(engine.EvBotDone, "handle bot done (injection)")
	s.recordEvent(engine.EventBotSpeakEnd, nil)
}

// recordNetworkQuality 记录音频帧的网络质量指标，并将质量事件写入会话事件日志和 OTel 度量。
func (s *Session) recordNetworkQuality(frame []byte) {
	if s.netQuality == nil {
		return
	}
	events := s.netQuality.RecordFrame(frame, time.Now())
	for _, evt := range events {
		switch evt {
		case NetEventPoorNetwork:
			s.recordEvent(engine.EventPoorNetwork, nil)
			if s.metrics != nil {
				s.metrics.IncPoorNetwork()
			}
		case NetEventAudioGap:
			s.recordEvent(engine.EventAudioGap, nil)
			if s.metrics != nil {
				s.metrics.IncAudioGap()
			}
		case NetEventLowVolume:
			s.recordEvent(engine.EventLowVolume, nil)
			if s.metrics != nil {
				s.metrics.IncLowVolume()
			}
		}
	}
}

// handleStreamingASR 处理流式 ASR 事件。
// partial 结果用于预推理（提前启动 LLM），final 结果触发对话管线。
// 在 BOT_SPEAKING 或 PROCESSING 状态下忽略 ASR 结果，防止打断正在进行的 TTS。
func (s *Session) handleStreamingASR(ctx context.Context, evt provider.ASREvent, silenceTimer *time.Timer) {
	// partial 结果：跟踪稳定性，条件满足时启动预推理。
	if !evt.IsFinal {
		s.handlePartialASR(ctx, evt)
		return
	}

	text := evt.Text
	if text == "" {
		return
	}

	// 重置 partial 追踪。
	defer s.resetPartialTracking()

	// 仅在等待用户或用户说话状态下处理 ASR 结果。
	state := s.mfsm.State()
	if state != engine.MediaWaitingUser && state != engine.MediaUserSpeaking {
		s.logger.Debug("ASR 结果丢弃（非用户状态）",
			slog.String("text", text),
			slog.String("state", state.String()),
		)
		return
	}

	s.logger.Info("ASR 最终结果", slog.String("text", text), slog.Float64("confidence", evt.Confidence))

	// 输入安全过滤：拦截注入攻击，截断过长输入。
	text = s.filterInput(text)
	if text == "" {
		return
	}

	s.recordEvent(engine.EventUserSpeechEnd, map[string]string{"text": text})
	s.tryHandleFSMEvent(engine.EvSpeechEnd, "handle speech end (streaming ASR)")

	if s.cfg.DialogueEngine == nil {
		s.tryHandleFSMEvent(engine.EvProcessingDone, "handle processing done (no engine)")
		return
	}

	// 尝试复用预推理结果，否则走正常流式管道。
	var sentenceCh <-chan string
	var onComplete func()

	if s.speculative != nil && s.speculative.inputText == text {
		// 预推理命中：复用已在运行的 LLM 结果。
		s.logger.Info("预推理命中", slog.String("text", text))
		sentenceCh = s.speculative.sentenceCh
		onComplete = s.speculative.commit
		s.speculative = nil
	} else {
		// 预推理未命中或不存在：走正常流式管道。
		if s.speculative != nil {
			s.logger.Info("预推理未命中",
				slog.String("speculative", s.speculative.inputText),
				slog.String("final", text),
			)
		}
		s.cancelSpeculative()

		var err error
		sentenceCh, err = s.cfg.DialogueEngine.ProcessUserInputStream(ctx, text)
		if err != nil {
			s.logger.Error("对话流式处理失败", slog.String("error", err.Error()))
			s.tryHandleFSMEvent(engine.EvProcessingDone, "handle processing done (stream error)")
			s.tryHandleFSMEvent(engine.EvBotDone, "handle bot done (stream error)")
			return
		}
	}

	s.recordEvent(engine.EventBotSpeakStart, map[string]string{"text": "(streaming)"})
	s.tryHandleFSMEvent(engine.EvProcessingDone, "handle processing done (stream)")

	if s.cfg.TTS != nil {
		s.synthesizeAndPlayStreamAsync(sentenceCh, onComplete)
	} else {
		// 无 TTS 时消费完通道后直接完成。
		go func() {
			for range sentenceCh {
			}
			if onComplete != nil {
				onComplete()
			}
		}()
		s.tryHandleFSMEvent(engine.EvBotDone, "handle bot done (no TTS)")
		s.recordEvent(engine.EventBotSpeakEnd, nil)
		silenceTimer.Reset(time.Duration(s.cfg.Protection.MaxSilenceSec) * time.Second)
	}
}
