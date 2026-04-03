package call

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/omeyang/Sonata/engine/pcm"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/guard"
)

// eventLoopHybrid 是 hybrid 模式的主事件循环。
// Omni-Realtime 负责实时音频对话，Smart LLM 异步分析业务决策。
func (s *Session) eventLoopHybrid(ctx context.Context) error {
	rv := s.cfg.Realtime
	if rv == nil {
		return errors.New("hybrid mode requires RealtimeVoice provider")
	}

	// 连接 Omni-Realtime。
	if err := rv.Connect(ctx, RealtimeVoiceConfig{
		Instructions: s.cfg.hybridInstructions(),
	}); err != nil {
		return fmt.Errorf("connect realtime voice: %w", err)
	}
	defer func() {
		if err := rv.Close(); err != nil {
			s.logger.Warn("关闭 RealtimeVoice", slog.String("error", err.Error()))
		}
	}()

	silenceTimer := time.NewTimer(time.Duration(s.cfg.Protection.FirstSilenceTimeoutSec) * time.Second)
	defer silenceTimer.Stop()

	// 优先使用 dispatcher 分配的专属通道，回退到全局 ESL 通道（兼容测试）。
	eslEvents := s.cfg.ESLEvents
	if eslEvents == nil && s.cfg.ESL != nil {
		eslEvents = s.cfg.ESL.Events()
	}

	// 异步决策状态。
	fields := make(map[string]string)

	hstate := &hybridLoopState{
		audioOutCh:   rv.AudioOut(),
		transcriptCh: rv.Transcripts(),
		eslEvents:    eslEvents,
		silenceTimer: silenceTimer,
		rv:           rv,
		turnNumber:   0,
		fields:       fields,
	}

	for {
		done, err := s.processHybridEvent(ctx, hstate)
		if done {
			return err
		}
	}
}

// hybridLoopState 持有 hybrid 事件循环的可变状态。
type hybridLoopState struct {
	audioOutCh   <-chan []byte
	transcriptCh <-chan TranscriptEvent
	eslEvents    <-chan ESLEvent
	silenceTimer *time.Timer
	rv           RealtimeVoice
	turnNumber   int
	fields       map[string]string
}

// processHybridEvent 处理 hybrid 事件循环中的单次 select 事件。
// 返回 done=true 表示循环应退出。
func (s *Session) processHybridEvent(ctx context.Context, h *hybridLoopState) (done bool, err error) {
	select {
	case <-ctx.Done():
		s.logger.Info("session context done", slog.String("reason", ctx.Err().Error()))
		s.handleHangup("max_duration")
		return true, fmt.Errorf("session context done: %w", ctx.Err())

	case frame, ok := <-s.cfg.AudioIn:
		if !ok {
			s.handleHangup("audio_closed")
			return true, nil
		}
		s.handleAudioFrameHybrid(ctx, frame, h.rv, h.silenceTimer)

	case audioChunk, ok := <-h.audioOutCh:
		if !ok {
			h.audioOutCh = nil
			return false, nil
		}
		s.handleOmniAudioOut(audioChunk)

	case evt, ok := <-h.transcriptCh:
		if !ok {
			h.transcriptCh = nil
			return false, nil
		}
		s.handleTranscriptEvent(ctx, evt, h.rv, &h.turnNumber, h.fields)

	case event, ok := <-h.eslEvents:
		if !ok {
			h.eslEvents = nil
			return false, nil
		}
		if eslDone := s.handleESLEvent(ctx, event, h.silenceTimer); eslDone {
			return true, nil
		}

	case <-h.silenceTimer.C:
		s.handleSilenceTimeout(h.silenceTimer)
		if s.mfsm.IsTerminal() {
			return true, nil
		}
	}
	return false, nil
}

// handleAudioFrameHybrid 在 hybrid 模式下处理音频帧。
// AMD 检测阶段使用本地 VAD，检测为人类后将音频流转发给 Omni。
func (s *Session) handleAudioFrameHybrid(ctx context.Context, frame []byte, rv RealtimeVoice, silenceTimer *time.Timer) {
	state := s.mfsm.State()

	switch state {
	case engine.MediaAMDDetecting:
		s.handleAMDFrame(frame)

	case engine.MediaWaitingUser, engine.MediaUserSpeaking, engine.MediaBotSpeaking:
		// hybrid 模式下，所有音频都转发给 Omni（由 server_vad 管理语音检测）。
		s.feedOmniResampled(ctx, frame, rv)
		// 有音频活动时重置静默计时器。
		silenceTimer.Reset(time.Duration(s.cfg.Protection.MaxSilenceSec) * time.Second)

	case engine.MediaProcessing:
		// Omni 正在生成回复，继续转发音频以支持 barge-in。
		s.feedOmniResampled(ctx, frame, rv)

	case engine.MediaIdle, engine.MediaDialing, engine.MediaRinging,
		engine.MediaBargeIn, engine.MediaSilenceTimeout,
		engine.MediaHangup, engine.MediaPostProcessing:
		// 这些状态下不处理音频。
	}
}

// feedOmniResampled 将 8kHz 音频重采样为 16kHz 后送入 Omni。
func (s *Session) feedOmniResampled(ctx context.Context, frame []byte, rv RealtimeVoice) {
	upsampled := pcm.Resample8to16(frame)
	if upsampled == nil {
		return
	}
	if err := rv.FeedAudio(ctx, upsampled); err != nil {
		s.logger.Warn("omni feed audio 失败", slog.String("error", err.Error()))
	}
}

// handleOmniAudioOut 处理 Omni 输出的音频帧。
// 将 24kHz PCM 降采样到 8kHz 后通过 AudioOut 发送给 FreeSWITCH。
func (s *Session) handleOmniAudioOut(chunk []byte) {
	// Omni 输出 24kHz 16bit mono PCM，需降采样到 8kHz。
	downsampled := resample24to8(chunk)
	if downsampled == nil {
		return
	}

	// 如果未在 BOT_SPEAKING 状态，转换过去。
	if s.mfsm.CanHandle(engine.EvProcessingDone) {
		s.tryHandleFSMEvent(engine.EvProcessingDone, "hybrid: processing done")
		s.recordEvent(engine.EventBotSpeakStart, nil)
	}

	// 按 320 字节（8kHz 20ms）帧发送。
	const frameSize = 320
	for i := 0; i < len(downsampled); i += frameSize {
		end := min(i+frameSize, len(downsampled))
		frame := downsampled[i:end]
		select {
		case s.cfg.AudioOut <- frame:
		case <-s.ctx.Done():
			return
		}
	}
}

// handleTranscriptEvent 处理 Omni 的文本转录事件。
func (s *Session) handleTranscriptEvent(
	ctx context.Context,
	evt TranscriptEvent,
	rv RealtimeVoice,
	turnNumber *int,
	fields map[string]string,
) {
	if !evt.IsFinal {
		return
	}

	switch evt.Role {
	case "user":
		s.handleHybridUserTranscript(ctx, evt, rv, turnNumber)

	case "assistant":
		s.handleHybridAssistantTranscript(ctx, evt, rv, turnNumber, fields)
	}
}

// hybridInjectionFallback 是 hybrid 模式注入被拦截时注入给 Omni 的指令。
// 指示 Omni 用安全回复替代，引导对话回到正题。
const hybridInjectionFallback = "用户刚才的话题偏离了正事，请你用一句话礼貌地引导回正题，比如说：好的，我们继续聊正事吧。"

// hybridBudgetDegradeInstruction 是预算紧张时注入给 Omni 的指令。
// 指示 Omni 缩短回复，尽快收束对话。
const hybridBudgetDegradeInstruction = "通话时间快到了，请尽量简短回复，抓紧收集关键信息，准备礼貌结束通话。"

// handleHybridUserTranscript 处理 hybrid 模式下的用户转录事件。
// 通过 InputFilter 过滤注入攻击，记录预算消耗并检查预算状态。
func (s *Session) handleHybridUserTranscript(ctx context.Context, evt TranscriptEvent, rv RealtimeVoice, turnNumber *int) {
	text := evt.Text

	// 输入安全过滤：拦截注入攻击。
	if s.cfg.InputFilter != nil {
		result := s.cfg.InputFilter.Filter(text)
		if result.Blocked {
			s.logger.Warn("hybrid: 用户输入被安全过滤器拦截",
				slog.String("flag", string(result.Flag)),
				slog.String("reason", result.Reason),
			)
			s.recordEvent(engine.EventUserSpeechEnd, map[string]string{
				"text":   text,
				"safety": string(result.Flag),
			})
			// 注入安全回复指令给 Omni，避免用户侧出现沉默。
			if err := rv.UpdateInstructions(ctx, hybridInjectionFallback); err != nil {
				s.logger.Warn("hybrid: 注入安全回复指令失败", slog.String("error", err.Error()))
			}
			return
		}
		text = result.Text
	}

	s.logger.Info("hybrid: 用户转录", slog.String("text", text))
	s.recordEvent(engine.EventUserSpeechEnd, map[string]string{"text": text})
	*turnNumber++

	// 记录用户输入的 token 消耗。
	if s.cfg.Budget != nil {
		s.cfg.Budget.RecordTokens(guard.EstimateTokens(text))
	}

	// 记录预算轮次并检查是否需要降级或结束通话。
	s.checkHybridBudget(ctx, rv)
}

// handleHybridAssistantTranscript 处理 hybrid 模式下的助手转录事件。
func (s *Session) handleHybridAssistantTranscript(
	ctx context.Context,
	evt TranscriptEvent,
	rv RealtimeVoice,
	turnNumber *int,
	fields map[string]string,
) {
	s.logger.Info("hybrid: 助手转录", slog.String("text", evt.Text))
	s.recordEvent(engine.EventBotSpeakEnd, map[string]string{"text": evt.Text})

	// 记录预算 token 消耗。
	if s.cfg.Budget != nil {
		tokens := guard.EstimateTokens(evt.Text)
		s.cfg.Budget.RecordTokens(tokens)
	}

	// 触发异步 Smart LLM 决策（不阻塞音频流）。
	if s.cfg.Strategy != nil {
		go s.runStrategyAsync(ctx, rv, StrategyInput{
			UserText:      "",
			AssistantText: evt.Text,
			TurnNumber:    *turnNumber,
			CurrentFields: copyFields(fields),
		}, fields)
	}
}

// checkHybridBudget 检查 hybrid 模式下的预算状态。
// 预算耗尽时结束通话，预算紧张时指示 Omni 缩短回复。
func (s *Session) checkHybridBudget(ctx context.Context, rv RealtimeVoice) {
	if s.cfg.Budget == nil {
		return
	}
	s.cfg.Budget.RecordTurn()
	action := s.cfg.Budget.Check()
	switch action {
	case guard.BudgetOK:
		// 预算充裕，正常处理。
	case guard.BudgetEnd:
		s.logger.Warn("hybrid: 预算耗尽，结束通话",
			slog.Int("used_tokens", s.cfg.Budget.UsedTokens()),
			slog.Int("used_turns", s.cfg.Budget.UsedTurns()),
		)
		s.handleHangup("budget_exhausted")
	case guard.BudgetDegrade:
		s.logger.Info("hybrid: 预算紧张，指示 Omni 缩短回复",
			slog.Int("used_tokens", s.cfg.Budget.UsedTokens()),
			slog.Int("used_turns", s.cfg.Budget.UsedTurns()),
		)
		// 指示 Omni 缩短回复并尽快收束对话。
		if err := rv.UpdateInstructions(ctx, hybridBudgetDegradeInstruction); err != nil {
			s.logger.Warn("hybrid: 注入预算降级指令失败", slog.String("error", err.Error()))
		}
	}
}

// runStrategyAsync 异步执行 Smart LLM 策略分析，将决策注入 Omni。
func (s *Session) runStrategyAsync(
	ctx context.Context,
	rv RealtimeVoice,
	input StrategyInput,
	fields map[string]string,
) {
	decision, err := s.cfg.Strategy.Analyze(ctx, input)
	if err != nil {
		s.logger.Warn("strategy 分析失败", slog.String("error", err.Error()))
		return
	}

	// 校验策略决策的字段格式和意图合法性。
	decision = s.validateStrategyDecision(decision)

	s.logger.Info("strategy 决策",
		slog.String("intent", string(decision.Intent)),
		slog.String("grade", string(decision.Grade)),
		slog.Bool("should_end", decision.ShouldEnd),
	)

	// 合并新提取的字段。
	maps.Copy(fields, decision.ExtractedFields)

	// 注入更新的指令给 Omni。
	if decision.Instructions != "" {
		if err := rv.UpdateInstructions(ctx, decision.Instructions); err != nil {
			s.logger.Warn("更新 Omni 指令失败", slog.String("error", err.Error()))
		}
	}

	// 如果策略决定结束通话。
	if decision.ShouldEnd {
		s.handleHangup("strategy_end")
	}
}

// validateStrategyDecision 校验 Strategy 决策输出的意图、字段格式等。
// 未配置 DecisionValidator 时原样返回。
func (s *Session) validateStrategyDecision(d *Decision) *Decision {
	if s.cfg.DecisionValidator == nil || d == nil {
		return d
	}
	input := guard.DecisionInput{
		Intent:          d.Intent,
		Grade:           d.Grade,
		Instructions:    d.Instructions,
		ShouldEnd:       d.ShouldEnd,
		ExtractedFields: d.ExtractedFields,
	}
	result := s.cfg.DecisionValidator.Validate(input)
	if !result.Valid {
		for _, v := range result.Violations {
			s.logger.Warn("hybrid: 策略决策校验违规", slog.String("violation", v))
		}
	}
	d.Intent = result.Sanitized.Intent
	d.Grade = result.Sanitized.Grade
	d.Instructions = result.Sanitized.Instructions
	d.ExtractedFields = result.Sanitized.ExtractedFields
	return d
}

// hybridInstructions 返回 hybrid 模式下 Omni 的初始系统指令。
func (cfg *SessionConfig) hybridInstructions() string {
	// 优先使用配置中的 instructions，否则使用默认值。
	if cfg.DialogueEngine != nil {
		opening := cfg.DialogueEngine.GetOpeningText()
		return "你是一个友好的AI电话助手。用简短的中文口语回复，像真人一样自然。接通后先说：" + opening
	}
	return "你是一个友好的AI电话助手。用简短的中文口语回复，像真人一样自然。"
}

// resample24to8 将 24kHz PCM 降采样到 8kHz（3:1 简单抽样）。
func resample24to8(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	// 24kHz 16bit mono → 8kHz 16bit mono，每 3 个采样取 1 个。
	const sampleBytes = 2 // 16bit
	const ratio = 3       // 24000 / 8000
	srcSamples := len(data) / sampleBytes
	dstSamples := srcSamples / ratio
	if dstSamples == 0 {
		return nil
	}

	out := make([]byte, dstSamples*sampleBytes)
	for i := range dstSamples {
		srcOffset := i * ratio * sampleBytes
		copy(out[i*sampleBytes:(i+1)*sampleBytes], data[srcOffset:srcOffset+sampleBytes])
	}
	return out
}

// copyFields 复制字段 map（防止并发修改）。
func copyFields(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	maps.Copy(dst, src)
	return dst
}
