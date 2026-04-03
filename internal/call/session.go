package call

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/engine/media"
	"github.com/omeyang/clarion/internal/guard"
	"github.com/omeyang/clarion/internal/observe"
	"github.com/omeyang/clarion/internal/provider"
	"github.com/omeyang/clarion/internal/resilience"
)

// SpeechDetector 检测音频帧是否包含人声。
// frame 为 PCM16 LE 单声道数据。
type SpeechDetector interface {
	IsSpeech(frame []byte) (bool, error)
}

// PipelineMode 指定呼叫管线模式。
type PipelineMode string

// 管线模式常量。
const (
	PipelineClassic PipelineMode = "classic" // 经典 ASR→LLM→TTS 管线。
	PipelineHybrid  PipelineMode = "hybrid"  // Omni 实时语音 + Smart LLM 异步分析。
)

// SessionConfig 持有呼叫会话的所有依赖。
type SessionConfig struct {
	CallID    int64
	ContactID int64
	TaskID    int64
	SessionID string
	Phone     string
	Gateway   string
	CallerID  string
	SIPDomain string

	Protection config.CallProtection
	AMDConfig  config.AMD
	ASRConfig  provider.ASRConfig
	LLMConfig  provider.LLMConfig
	TTSConfig  provider.TTSConfig

	ASR            provider.ASRProvider
	LLM            provider.LLMProvider
	TTS            provider.TTSProvider
	ESL            *ESLClient
	ESLEvents      <-chan ESLEvent // 专属事件通道（由 dispatcher 分配），为 nil 时回退到 ESL.Events()
	SpeechDetector SpeechDetector

	DialogueEngine *dialogue.Engine
	Logger         *slog.Logger

	// AudioWSURL 是 FreeSWITCH mod_audio_fork 连接 Call Worker 的 WebSocket 地址。
	AudioWSURL string

	// AudioIn 接收来自 WebSocket 桥接的音频帧。
	AudioIn <-chan []byte
	// AudioOut 发送音频帧到 WebSocket 桥接进行播放。
	AudioOut chan<- []byte

	// FillerAudios 填充词音频列表（预合成的 8kHz PCM16 数据）。
	// ASR 返回最终结果后，在 LLM 处理期间随机播放一个填充词，
	// 将感知延迟从 ~2s 降至 ~200ms。
	// 为空时不播放填充词。
	FillerAudios [][]byte

	// PipelineMode 指定管线模式。
	PipelineMode PipelineMode
	// Realtime 是实时语音提供者（hybrid 模式下使用）。
	Realtime RealtimeVoice
	// Strategy 是异步业务决策器（hybrid 模式下使用）。
	Strategy DialogueStrategy

	// InputFilter 输入安全过滤器，为 nil 时不过滤。
	InputFilter *guard.InputFilter
	// Budget 通话预算控制器（hybrid 模式下使用，classic 模式由 DialogueEngine 管理）。
	Budget *guard.CallBudget
	// DecisionValidator 决策校验器（hybrid 模式下校验 Strategy 决策字段格式）。
	DecisionValidator *guard.DecisionValidator

	// SnapshotStore 会话快照存储，为 nil 时不保存快照。
	SnapshotStore SnapshotStore
	// SnapshotTTL 快照过期时间，默认 10 分钟。
	SnapshotTTL time.Duration

	// RestoredSnapshot 非 nil 时表示这是一次恢复呼叫，包含中断前的会话快照。
	// 用于恢复对话状态、使用恢复开场白。
	RestoredSnapshot *SessionSnapshot

	// Metrics OTel 度量收集器，为 nil 时不上报度量。
	Metrics *observe.CallMetrics
}

// SessionResult 是已完成呼叫会话的结果。
type SessionResult struct {
	CallID     int64                  `json:"call_id"`
	SessionID  string                 `json:"session_id"`
	Status     engine.CallStatus      `json:"status"`
	AnswerType engine.AnswerType      `json:"answer_type"`
	Duration   int                    `json:"duration_sec"`
	Grade      engine.Grade           `json:"grade"`
	Turns      []dialogue.Turn        `json:"turns"`
	Events     []engine.RecordedEvent `json:"events"`
	Fields     map[string]string      `json:"collected_fields"`
	// NetQuality 通话期间的网络质量汇总快照。
	NetQuality *NetworkQualitySnapshot `json:"net_quality,omitempty"`
}

// Session 编排单个外呼通话。
// 管理媒体 FSM、对话引擎和流式 ASR→LLM→TTS 管线。
type Session struct {
	cfg    SessionConfig
	mfsm   *media.FSM
	logger *slog.Logger
	ctx    context.Context //nolint:containedctx // Run() 传入，供异步方法使用

	mu        sync.Mutex
	events    []engine.RecordedEvent
	startTime time.Time
	answered  bool
	ansType   engine.AnswerType
	status    engine.CallStatus

	silenceCount int

	// 通话中的通道 UUID（从 CHANNEL_ANSWER 事件获取）。
	channelUUID string
	// 流式 ASR 会话。
	asrStream provider.ASRStream
	// ASR 最终结果通道。
	asrResults chan provider.ASREvent
	// TTS 播放完成信号。
	botDoneCh chan struct{}

	// barge-in 连续活跃帧计数，防止噪音误触发。
	bargeInFrames int
	// 取消当前 TTS 合成/播放的函数。
	ttsCancel context.CancelFunc
	// ttsPlaying 标记 TTS 音频是否正在 FreeSWITCH 中播放。
	// 仅在实际播放时才允许 barge-in。
	ttsPlaying atomic.Bool
	// ttsStreaming 标记是否处于流式 TTS 模式（逐句合成播放）。
	// 流式模式下 PLAYBACK_STOP 不触发 EvBotDone，由 goroutine 统一管理。
	ttsStreaming atomic.Bool

	// speechDetector 检测人声，由 SessionConfig.SpeechDetector 注入。
	speechDetector SpeechDetector

	// amdDetector AMD 检测器，在 AMD 阶段累积帧数据判断人/机。
	amdDetector *AMDDetectorTestable

	// asrBreaker ASR 流启动的熔断器，连续失败时快速跳过。
	asrBreaker *resilience.Breaker

	// 预推理状态：基于 partial ASR 稳定性提前启动 LLM。
	lastPartialText    string
	partialStableCount int
	speculative        *speculativeRun

	// 重采样缓冲区复用，避免每帧分配（50 帧/秒热路径）。
	resampleBuf    []byte
	vadResampleBuf []byte

	// 网络质量监控。
	netQuality *NetworkQuality
	// OTel 度量收集器。
	metrics *observe.CallMetrics
}

// NewSession 创建新的呼叫会话。
func NewSession(cfg SessionConfig) *Session {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With(
		slog.String("session_id", cfg.SessionID),
		slog.Int64("call_id", cfg.CallID),
	)

	s := &Session{
		cfg:            cfg,
		mfsm:           media.NewFSM(engine.MediaIdle, media.Unsynced()),
		logger:         logger,
		status:         engine.CallPending,
		asrResults:     make(chan provider.ASREvent, 16),
		botDoneCh:      make(chan struct{}, 1),
		speechDetector: cfg.SpeechDetector,
		asrBreaker: resilience.NewBreaker(resilience.BreakerConfig{
			FailureThreshold: 3,
			ResetTimeout:     10 * time.Second,
		}),
		netQuality: NewNetworkQuality(DefaultNetworkQualityConfig(), logger),
		metrics:    cfg.Metrics,
	}

	return s
}

// Run 执行完整的呼叫生命周期。阻塞直到呼叫完成。
func (s *Session) Run(ctx context.Context) (*SessionResult, error) {
	s.startTime = time.Now()

	// 设置最大通话时长超时。
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.Protection.MaxDurationSec)*time.Second)
	defer cancel()

	s.ctx = ctx

	// 预热提供者连接，避免首次调用冷启动延迟。
	s.warmupProviders(ctx)

	s.mfsm.OnTransition(media.Callback(func(from, to engine.MediaState, event engine.MediaEvent) {
		s.logger.Info("media transition",
			slog.String("from", from.String()),
			slog.String("to", to.String()),
			slog.String("event", event.String()),
		)
	}))

	// 发起呼叫。
	s.status = engine.CallDialing
	if err := s.mfsm.Handle(engine.EvDial); err != nil {
		return s.buildResult(engine.CallFailed), fmt.Errorf("dial: %w", err)
	}

	s.recordEvent(engine.EventBotSpeakStart, nil)

	if s.cfg.ESL != nil {
		var originateErr error
		switch s.cfg.Gateway {
		case "":
			// 无网关时使用 loopback 端点（本地回环测试）。
			_, originateErr = s.cfg.ESL.OriginateLoopback(ctx, s.cfg.Phone, "default", s.cfg.SessionID)
		case "local":
			// 呼叫本地注册的 SIP 用户（SIP 软电话测试）。
			_, originateErr = s.cfg.ESL.OriginateUser(ctx, s.cfg.Phone, s.cfg.SIPDomain, s.cfg.CallerID, s.cfg.SessionID)
		default:
			_, originateErr = s.cfg.ESL.Originate(ctx, s.cfg.Gateway, s.cfg.CallerID, s.cfg.Phone, s.cfg.SessionID)
		}
		if originateErr != nil {
			if handleErr := s.mfsm.Handle(engine.EvDialFailed); handleErr != nil {
				s.logger.Warn("handle dial-failed event", slog.String("error", handleErr.Error()))
			}
			return s.buildResult(engine.CallFailed), fmt.Errorf("originate: %w", originateErr)
		}
	}

	// 主事件循环：根据管线模式选择不同的事件循环。
	var err error
	if s.cfg.PipelineMode == PipelineHybrid && s.cfg.Realtime != nil {
		err = s.eventLoopHybrid(ctx)
	} else {
		err = s.eventLoop(ctx)
	}

	result := s.buildResult(s.status)
	if err != nil && s.status == engine.CallPending {
		result.Status = engine.CallFailed
	}

	return result, err
}

// eventLoop 处理音频和 ESL 事件直到通话结束。
func (s *Session) eventLoop(ctx context.Context) error {
	silenceTimer := time.NewTimer(time.Duration(s.cfg.Protection.FirstSilenceTimeoutSec) * time.Second)
	defer silenceTimer.Stop()

	// 优先使用 dispatcher 分配的专属通道，回退到全局 ESL 通道（兼容测试）。
	eslEvents := s.cfg.ESLEvents
	if eslEvents == nil && s.cfg.ESL != nil {
		eslEvents = s.cfg.ESL.Events()
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("session context done", slog.String("reason", ctx.Err().Error()))
			s.handleHangup("max_duration")
			return fmt.Errorf("session context done: %w", ctx.Err())

		case frame, ok := <-s.cfg.AudioIn:
			if !ok {
				s.handleHangup("audio_closed")
				return nil
			}
			s.handleAudioFrame(ctx, frame, silenceTimer)

		case event, ok := <-eslEvents:
			if !ok {
				eslEvents = nil
				continue
			}
			if done := s.handleESLEvent(ctx, event, silenceTimer); done {
				return nil
			}

		case <-silenceTimer.C:
			s.handleSilenceTimeout(silenceTimer)
			if s.mfsm.IsTerminal() {
				return nil
			}

		case asrEvt := <-s.asrResults:
			s.handleStreamingASR(ctx, asrEvt, silenceTimer)
			if s.mfsm.IsTerminal() {
				return nil
			}

		case <-s.botDoneCh:
			s.tryHandleFSMEvent(engine.EvBotDone, "handle bot done (TTS playback)")
			s.recordEvent(engine.EventBotSpeakEnd, nil)
			silenceTimer.Reset(time.Duration(s.cfg.Protection.MaxSilenceSec) * time.Second)
		}
	}
}

// handleFSMEvent 向媒体 FSM 发送事件，出错时记录警告。
func (s *Session) handleFSMEvent(event engine.MediaEvent, msg string) {
	if err := s.mfsm.Handle(event); err != nil {
		s.logger.Warn(msg, slog.String("error", err.Error()))
	}
}

// tryHandleFSMEvent 仅在事件可处理时向媒体 FSM 发送事件。
func (s *Session) tryHandleFSMEvent(event engine.MediaEvent, msg string) {
	if !s.mfsm.CanHandle(event) {
		return
	}
	if err := s.mfsm.Handle(event); err != nil {
		s.logger.Warn(msg, slog.String("error", err.Error()))
	}
}

// handleHangup 转换到挂断状态并记录事件。
// 区分正常挂断和意外中断，意外中断时保存会话快照用于后续恢复。
func (s *Session) handleHangup(cause string) {
	s.logger.Info("call hangup", slog.String("cause", cause))

	if s.mfsm.CanHandle(engine.EvHangup) {
		if err := s.mfsm.Handle(engine.EvHangup); err != nil {
			s.logger.Warn("handle hangup event", slog.String("error", err.Error()))
		}
	}

	if s.status == engine.CallInProgress || s.status == engine.CallRinging {
		s.status = engine.CallCompleted
	}

	switch cause {
	case "NO_ANSWER":
		s.status = engine.CallNoAnswer
	case "USER_BUSY":
		s.status = engine.CallBusy
	case "NORMAL_CLEARING", "dialogue_complete", "max_duration":
		s.status = engine.CallCompleted
	}

	// 意外中断检测：已接听的通话因网络/媒体异常断开。
	if s.isUnexpectedDisconnect(cause) {
		s.status = engine.CallInterrupted
		s.recordEvent(engine.EventUnexpectedDisconnect, map[string]string{"cause": cause})
		s.saveSnapshot(cause)
	}

	s.recordEvent(engine.EventHangupBySystem, map[string]string{"cause": cause})
}

// defaultSnapshotTTL 快照默认过期时间。
const defaultSnapshotTTL = 10 * time.Minute

// saveSnapshot 保存会话快照到存储。
func (s *Session) saveSnapshot(cause string) {
	if s.cfg.SnapshotStore == nil {
		return
	}

	snap := s.buildSnapshot(cause)
	ttl := s.cfg.SnapshotTTL
	if ttl <= 0 {
		ttl = defaultSnapshotTTL
	}

	// 使用独立的超时上下文，避免主 context 已取消导致保存失败。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.cfg.SnapshotStore.Save(ctx, snap, ttl); err != nil {
		s.logger.Error("保存会话快照失败", slog.String("error", err.Error()))
		return
	}

	s.logger.Info("会话快照已保存",
		slog.Int64("call_id", snap.CallID),
		slog.String("cause", cause),
		slog.Duration("ttl", ttl),
	)
}

// recordEvent 将事件追加到会话事件日志。
func (s *Session) recordEvent(eventType engine.EventType, metadata map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, engine.RecordedEvent{
		EventType:   eventType,
		TimestampMs: time.Since(s.startTime).Milliseconds(),
		Metadata:    metadata,
	})
}

// buildResult 构造最终会话结果。
func (s *Session) buildResult(status engine.CallStatus) *SessionResult {
	s.mu.Lock()
	events := make([]engine.RecordedEvent, len(s.events))
	copy(events, s.events)
	s.mu.Unlock()

	duration := int(time.Since(s.startTime).Seconds())

	result := &SessionResult{
		CallID:     s.cfg.CallID,
		SessionID:  s.cfg.SessionID,
		Status:     status,
		AnswerType: s.ansType,
		Duration:   duration,
		Events:     events,
	}

	if s.cfg.DialogueEngine != nil {
		dr := s.cfg.DialogueEngine.Result(status)
		result.Grade = dr.Grade
		result.Turns = dr.Turns
		result.Fields = dr.CollectedFields
	}

	// 附加网络质量快照并上报 OTel 度量。
	s.attachNetQualityResult(result)

	return result
}

// attachNetQualityResult 将网络质量快照附加到结果，并上报最终 OTel 度量。
func (s *Session) attachNetQualityResult(result *SessionResult) {
	if s.netQuality == nil {
		return
	}
	snap := s.netQuality.Snapshot()
	result.NetQuality = &snap

	if s.metrics == nil {
		return
	}
	s.metrics.RecordJitterAvg(snap.JitterAvgMs)
	s.metrics.RecordLossRate(snap.LossRate)
	s.metrics.RecordLowVolumeRate(snap.LowVolumeRate)
}

// SessionResultJSON 将 SessionResult 序列化为 JSON。
func SessionResultJSON(r *SessionResult) ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("marshal session result: %w", err)
	}
	return data, nil
}
