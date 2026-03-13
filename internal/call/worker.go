package call

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/engine/rules"
	"github.com/omeyang/clarion/internal/guard"
	"github.com/omeyang/clarion/internal/observe"
	"github.com/omeyang/clarion/internal/provider"
	realtimepkg "github.com/omeyang/clarion/internal/provider/realtime"
	strategypkg "github.com/omeyang/clarion/internal/provider/strategy"
	"github.com/omeyang/clarion/internal/scheduler"
	"github.com/omeyang/clarion/internal/store"
)

// audioLink 持有会话的双向音频通道，供 WebSocket 桥接使用。
type audioLink struct {
	in  chan []byte // FreeSWITCH → Call Worker（用户音频）
	out chan []byte // Call Worker → FreeSWITCH（AI 音频）
}

// Task 是呼叫任务的内部表示。
type Task struct {
	CallID     int64  `json:"call_id"`
	ContactID  int64  `json:"contact_id"`
	TaskID     int64  `json:"task_id"`
	Phone      string `json:"phone"`
	Gateway    string `json:"gateway"`
	CallerID   string `json:"caller_id"`
	TemplateID int64  `json:"template_id"`
	// RecoveryFromCallID 非零时表示恢复呼叫，值为被中断通话的 CallID。
	RecoveryFromCallID int64 `json:"recovery_from_call_id,omitempty"`
}

// Worker 是呼叫工作进程主循环。
// 通过 Asynq 接收任务、创建会话并管理并发。
type Worker struct {
	cfg    config.Config
	rds    *store.RDS
	logger *slog.Logger

	asr provider.ASRProvider
	llm provider.LLMProvider
	tts provider.TTSProvider
	esl *ESLClient

	activeCalls atomic.Int32
	maxCalls    int

	mu         sync.Mutex
	sessions   map[string]*Session   // sessionID → session
	audioLinks map[string]*audioLink // sessionID → 音频通道

	wsServer       *http.Server
	snapshotStore  SnapshotStore
	speechDetector SpeechDetector

	// schedulerClient 用于入队恢复任务。
	schedulerClient *scheduler.Client

	// metrics OTel 度量收集器。
	metrics *observe.CallMetrics
}

// NewWorker 创建新的呼叫工作进程。
func NewWorker(cfg config.Config, rds *store.RDS, logger *slog.Logger) *Worker {
	w := &Worker{
		cfg:        cfg,
		rds:        rds,
		logger:     logger,
		maxCalls:   cfg.Worker.MaxConcurrentCalls,
		sessions:   make(map[string]*Session),
		audioLinks: make(map[string]*audioLink),
	}
	if rds != nil {
		w.snapshotStore = NewRedisSnapshotStore(rds.Client, cfg.Redis.SessionPrefix)
		w.schedulerClient = scheduler.NewClient(
			scheduler.RedisOpt(cfg.Redis),
			cfg.Scheduler.Queue,
		)
	}
	return w
}

// SetProviders 设置 AI 服务提供者。需在 Run 之前调用。
func (w *Worker) SetProviders(asr provider.ASRProvider, llm provider.LLMProvider, tts provider.TTSProvider) {
	w.asr = asr
	w.llm = llm
	w.tts = tts
}

// SetSpeechDetector 设置语音活动检测器（如 SileroVAD）。需在 Run 之前调用。
// 设置后会注入到每个 Session，替代默认的能量阈值检测。
func (w *Worker) SetSpeechDetector(sd SpeechDetector) {
	w.speechDetector = sd
}

// SetMetrics 设置 OTel 度量收集器。需在 Run 之前调用。
// 设置后每个 Session 会上报网络质量等度量。
func (w *Worker) SetMetrics(m *observe.CallMetrics) {
	w.metrics = m
}

// Run 启动工作循环。阻塞直到上下文取消。
func (w *Worker) Run(ctx context.Context) error {
	w.logger.Info("worker starting",
		slog.Int("max_concurrent", w.maxCalls),
		slog.String("queue", w.cfg.Scheduler.Queue),
	)

	// 连接 FreeSWITCH ESL。
	w.esl = NewESLClient(w.cfg.FreeSWITCH, w.logger)
	if err := w.esl.Connect(ctx); err != nil {
		return fmt.Errorf("ESL connect: %w", err)
	}
	defer func() {
		if err := w.esl.Close(); err != nil {
			w.logger.Warn("close ESL connection", slog.String("error", err.Error()))
		}
	}()

	// 启动音频 WebSocket 服务器。
	w.startWSServer()

	// 启动 Asynq 任务服务器，替代 BRPOP 轮询。
	srv := w.newAsynqServer()
	mux := asynq.NewServeMux()
	mux.HandleFunc(scheduler.TaskTypeOutboundCall, w.HandleOutboundCall)

	if err := srv.Start(mux); err != nil {
		return fmt.Errorf("start asynq server: %w", err)
	}

	// 等待关闭信号。
	<-ctx.Done()
	w.logger.Info("worker shutting down, waiting for active calls")

	// 优雅关闭 Asynq 服务器（等待活跃任务完成）。
	srv.Shutdown()
	w.shutdownWSServer()
	w.closeSchedulerClient()

	w.logger.Info("worker stopped")
	return nil
}

// closeSchedulerClient 关闭调度客户端。
func (w *Worker) closeSchedulerClient() {
	if w.schedulerClient == nil {
		return
	}
	if err := w.schedulerClient.Close(); err != nil {
		w.logger.Warn("关闭调度客户端", slog.String("error", err.Error()))
	}
}

// HandleOutboundCall 处理 Asynq 外呼任务。
func (w *Worker) HandleOutboundCall(ctx context.Context, t *asynq.Task) error {
	payload, err := scheduler.ParseOutboundCallPayload(t)
	if err != nil {
		return fmt.Errorf("parse outbound call payload: %w", err)
	}

	task := Task{
		CallID:             payload.CallID,
		ContactID:          payload.ContactID,
		TaskID:             payload.TaskID,
		Phone:              payload.Phone,
		Gateway:            payload.Gateway,
		CallerID:           payload.CallerID,
		TemplateID:         payload.TemplateID,
		RecoveryFromCallID: payload.RecoveryFromCallID,
	}

	w.logger.Info("task received",
		slog.Int64("call_id", task.CallID),
		slog.String("phone", task.Phone),
	)

	w.executeTask(ctx, task)
	return nil
}

// newAsynqServer 创建 Asynq 服务器实例。
func (w *Worker) newAsynqServer() *asynq.Server {
	return asynq.NewServer(
		scheduler.RedisOpt(w.cfg.Redis),
		asynq.Config{
			Concurrency:     w.maxCalls,
			Queues:          map[string]int{w.cfg.Scheduler.Queue: 1},
			Logger:          &slogAdapter{l: w.logger.With(slog.String("component", "asynq"))},
			ShutdownTimeout: 10 * time.Second,
		},
	)
}

// shutdownWSServer 优雅关闭 WebSocket 音频服务器。
func (w *Worker) shutdownWSServer() {
	if w.wsServer == nil {
		return
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.wsServer.Shutdown(shutdownCtx); err != nil {
		w.logger.Warn("WS server shutdown", slog.String("error", err.Error()))
	}
}

// executeTask 运行单个呼叫会话。
func (w *Worker) executeTask(ctx context.Context, task Task) {
	w.activeCalls.Add(1)
	defer w.activeCalls.Add(-1)

	sessionID := fmt.Sprintf("call-%d-%d", task.CallID, time.Now().UnixMilli())

	audioIn := make(chan []byte, 128)
	audioOut := make(chan []byte, 128)

	// 注册音频通道供 WebSocket 桥接使用。
	link := &audioLink{in: audioIn, out: audioOut}
	w.mu.Lock()
	w.audioLinks[sessionID] = link

	sessionCfg := w.buildSessionConfig(task, sessionID, audioIn, audioOut)
	w.attachDialogueEngine(&sessionCfg)
	w.attachHybridProviders(&sessionCfg)
	w.attachInputFilter(&sessionCfg)
	w.attachGuardHybrid(&sessionCfg)
	w.attachRecoverySnapshot(ctx, &sessionCfg, task.RecoveryFromCallID)

	session := NewSession(sessionCfg)
	w.sessions[sessionID] = session
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		delete(w.sessions, sessionID)
		delete(w.audioLinks, sessionID)
		w.mu.Unlock()
		close(audioOut)
	}()

	result, err := session.Run(ctx)
	if err != nil {
		w.logger.Error("session error",
			slog.String("session_id", sessionID),
			slog.String("error", err.Error()),
		)
	}

	// 发布完成事件到 Redis Stream。
	if result != nil {
		w.publishCompletion(ctx, result)
	}

	// 通话意外中断时入队恢复任务。
	if result != nil {
		w.scheduleRecoveryIfNeeded(ctx, result, task)
	}
}

// buildSessionConfig 根据任务信息构建会话配置。
func (w *Worker) buildSessionConfig(task Task, sessionID string, audioIn <-chan []byte, audioOut chan<- []byte) SessionConfig {
	return SessionConfig{
		CallID:    task.CallID,
		ContactID: task.ContactID,
		TaskID:    task.TaskID,
		SessionID: sessionID,
		Phone:     task.Phone,
		Gateway:   task.Gateway,
		CallerID:  task.CallerID,
		SIPDomain: w.cfg.FreeSWITCH.SIPDomain,

		Protection: w.cfg.CallProtection,
		AMDConfig:  w.cfg.AMD,
		ASRConfig: provider.ASRConfig{
			Model:      w.cfg.ASR.Model,
			SampleRate: w.cfg.ASR.SampleRate,
		},
		LLMConfig: provider.LLMConfig{
			Model:       w.cfg.LLM.Model,
			MaxTokens:   w.cfg.LLM.MaxTokens,
			Temperature: w.cfg.LLM.Temperature,
			TimeoutMs:   w.cfg.LLM.TimeoutMs,
		},
		TTSConfig: provider.TTSConfig{
			Model:      w.cfg.TTS.Model,
			Voice:      w.cfg.TTS.Voice,
			SampleRate: w.cfg.TTS.SampleRate,
		},

		ASR: w.asr,
		LLM: w.llm,
		TTS: w.tts,
		ESL: w.esl,

		AudioWSURL: fmt.Sprintf("ws://%s/audio", w.cfg.FreeSWITCH.AudioWSHost),

		PipelineMode: PipelineMode(w.cfg.Pipeline.Mode),

		SpeechDetector: w.speechDetector,

		Logger:        w.logger,
		AudioIn:       audioIn,
		AudioOut:      audioOut,
		SnapshotStore: w.snapshotStore,
		SnapshotTTL:   w.cfg.Snapshot.TTL(),
		Metrics:       w.metrics,
	}
}

// attachDialogueEngine 创建对话引擎并绑定到会话配置。
func (w *Worker) attachDialogueEngine(cfg *SessionConfig) {
	if w.llm == nil {
		return
	}
	engCfg := dialogue.EngineConfig{
		TemplateConfig: rules.TemplateConfig{
			MaxTurns:      w.cfg.CallProtection.MaxTurns,
			MaxObjections: 3,
			Templates: map[string]string{
				"OPENING": "你好，请问你能听到我说话吗？",
				"CLOSING": "感谢你的时间，再见！",
			},
		},
		LLM:             w.llm,
		Logger:          w.logger,
		PromptTemplates: map[string]string{},
		SystemPrompt:    "你是一个友好的AI电话助手，正在和用户进行电话对话。用简短的中文口语回复，像真人一样自然。",
		MaxHistory:      5,
	}

	// 启用预算控制时注入 BudgetConfig。
	if w.cfg.Budget.Enabled {
		engCfg.BudgetConfig = &guard.BudgetConfig{
			MaxTokens:        w.cfg.Budget.MaxTokens,
			MaxTurns:         w.cfg.Budget.MaxTurns,
			MaxDuration:      w.cfg.Budget.Duration(),
			DegradeThreshold: w.cfg.Budget.DegradeThreshold,
		}
	}

	// 启用 guard 时注入响应校验器、决策校验器、输出校验器和内容校验器。
	if w.cfg.Guard.Enabled {
		engCfg.ResponseValidatorCfg = &guard.ResponseValidatorConfig{
			MaxResponseRunes:  w.cfg.Guard.MaxResponseRunes,
			ExtraAIPatterns:   w.cfg.Guard.ExtraAIPatterns,
			ExtraLeakPatterns: w.cfg.Guard.ExtraLeakPatterns,
		}
		engCfg.DecisionValidatorCfg = &guard.DecisionValidatorConfig{}
		engCfg.OutputCheckerCfg = &guard.OutputCheckerConfig{}
		engCfg.ContentCheckerCfg = &guard.ContentCheckerConfig{}
	}

	// 启用离题检测时注入离题计数器。
	if w.cfg.OffTopic.Enabled {
		engCfg.OffTopicCfg = &guard.OffTopicConfig{
			ConvergeAfter: w.cfg.OffTopic.ConvergeAfter,
			EndAfter:      w.cfg.OffTopic.EndAfter,
		}
	}

	eng, err := dialogue.NewEngine(engCfg)
	if err != nil {
		w.logger.Error("创建对话引擎失败", slog.String("error", err.Error()))
		return
	}
	cfg.DialogueEngine = eng
}

// attachHybridProviders 在 hybrid 管线模式下创建并绑定 RealtimeVoice 和 DialogueStrategy。
func (w *Worker) attachHybridProviders(cfg *SessionConfig) {
	if w.cfg.Pipeline.Mode != config.PipelineHybrid {
		return
	}

	rtCfg := w.cfg.Realtime
	omni := realtimepkg.NewOmni(realtimepkg.OmniConfig{
		APIKey: rtCfg.APIKey,
		Logger: w.logger,
	})
	cfg.Realtime = NewOmniAdapter(omni)

	// Smart LLM 策略分析器（可选）。
	if w.llm != nil {
		smartCfg := w.cfg.SmartLLM
		smart := strategypkg.NewSmart(strategypkg.SmartConfig{
			LLM: w.llm,
			LLMConfig: provider.LLMConfig{
				Model:       smartCfg.Model,
				MaxTokens:   smartCfg.MaxTokens,
				Temperature: smartCfg.Temperature,
			},
			Logger: w.logger,
		})
		cfg.Strategy = NewSmartAdapter(smart)
	}
}

// attachInputFilter 在启用 guard 时创建输入过滤器并绑定到会话配置。
func (w *Worker) attachInputFilter(cfg *SessionConfig) {
	if !w.cfg.Guard.Enabled {
		return
	}
	cfg.InputFilter = guard.NewInputFilter(w.cfg.Guard.ExtraPatterns, w.cfg.Guard.MaxInputRunes)
}

// attachGuardHybrid 在 hybrid 模式启用 guard/budget 时创建独立的预算和决策校验器。
// classic 模式由 DialogueEngine 内部管理，hybrid 模式需要 Session 直接持有。
func (w *Worker) attachGuardHybrid(cfg *SessionConfig) {
	if w.cfg.Pipeline.Mode != config.PipelineHybrid {
		return
	}
	// hybrid 模式下独立的预算控制。
	if w.cfg.Budget.Enabled {
		cfg.Budget = guard.NewCallBudget(guard.BudgetConfig{
			MaxTokens:        w.cfg.Budget.MaxTokens,
			MaxTurns:         w.cfg.Budget.MaxTurns,
			MaxDuration:      w.cfg.Budget.Duration(),
			DegradeThreshold: w.cfg.Budget.DegradeThreshold,
		})
	}
	// hybrid 模式下独立的决策校验器。
	if w.cfg.Guard.Enabled {
		cfg.DecisionValidator = guard.NewDecisionValidator(guard.DecisionValidatorConfig{})
	}
}

// attachRecoverySnapshot 在恢复呼叫时从 Redis 加载会话快照并绑定到会话配置。
// recoveryFromCallID 为 0 时不做任何操作。
func (w *Worker) attachRecoverySnapshot(ctx context.Context, cfg *SessionConfig, recoveryFromCallID int64) {
	if recoveryFromCallID == 0 || w.snapshotStore == nil {
		return
	}

	snap, err := w.snapshotStore.Load(ctx, recoveryFromCallID)
	if err != nil {
		w.logger.Error("加载恢复快照失败",
			slog.Int64("recovery_from", recoveryFromCallID),
			slog.String("error", err.Error()),
		)
		return
	}
	if snap == nil {
		w.logger.Info("恢复快照已过期，当作新通话处理",
			slog.Int64("recovery_from", recoveryFromCallID),
		)
		return
	}

	cfg.RestoredSnapshot = snap
	w.logger.Info("已加载恢复快照",
		slog.Int64("recovery_from", recoveryFromCallID),
		slog.String("state", snap.DialogueState),
	)

	// 快照已使用，删除避免重复恢复。
	if delErr := w.snapshotStore.Delete(ctx, recoveryFromCallID); delErr != nil {
		w.logger.Warn("删除已使用的快照失败", slog.String("error", delErr.Error()))
	}
}

// recoveryDelay 恢复呼叫的默认延迟。
const recoveryDelay = 30 * time.Second

// scheduleRecoveryIfNeeded 在通话意外中断时入队恢复呼叫任务。
func (w *Worker) scheduleRecoveryIfNeeded(ctx context.Context, result *SessionResult, task Task) {
	if result.Status != engine.CallInterrupted {
		return
	}
	if w.schedulerClient == nil {
		w.logger.Warn("通话中断但无调度客户端，无法入队恢复任务")
		return
	}
	// 已经是恢复呼叫的不再二次恢复，避免无限循环。
	if task.RecoveryFromCallID != 0 {
		w.logger.Info("恢复呼叫再次中断，不再入队恢复任务",
			slog.Int64("call_id", task.CallID),
		)
		return
	}

	payload := scheduler.OutboundCallPayload{
		CallID:             task.CallID,
		ContactID:          task.ContactID,
		TaskID:             task.TaskID,
		Phone:              task.Phone,
		Gateway:            task.Gateway,
		CallerID:           task.CallerID,
		TemplateID:         task.TemplateID,
		RecoveryFromCallID: task.CallID,
	}

	_, err := w.schedulerClient.EnqueueOutboundCall(ctx, payload, asynq.ProcessIn(recoveryDelay))
	if err != nil {
		w.logger.Error("入队恢复任务失败",
			slog.Int64("call_id", task.CallID),
			slog.String("error", err.Error()),
		)
		return
	}

	w.logger.Info("已入队恢复任务",
		slog.Int64("call_id", task.CallID),
		slog.Duration("delay", recoveryDelay),
	)
}

// publishCompletion 将呼叫结果发送到 Redis 完成流。
func (w *Worker) publishCompletion(ctx context.Context, result *SessionResult) {
	data, err := SessionResultJSON(result)
	if err != nil {
		w.logger.Error("marshal result", slog.String("error", err.Error()))
		return
	}

	err = w.rds.Client.XAdd(ctx, &redis.XAddArgs{
		Stream: w.cfg.Redis.EventStreamKey,
		ID:     "*",
		Values: map[string]any{
			"call_id":    result.CallID,
			"session_id": result.SessionID,
			"status":     string(result.Status),
			"data":       string(data),
		},
	}).Err()

	if err != nil {
		w.logger.Error("publish completion", slog.String("error", err.Error()))
	}
}

// ActiveCalls 返回当前活跃呼叫数。
func (w *Worker) ActiveCalls() int {
	return int(w.activeCalls.Load())
}

// startWSServer 启动 WebSocket 音频服务器。
func (w *Worker) startWSServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/audio", w.handleAudioWS)

	w.wsServer = &http.Server{
		Addr:              w.cfg.FreeSWITCH.AudioWSAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		w.logger.Info("audio WS server starting", slog.String("addr", w.cfg.FreeSWITCH.AudioWSAddr))
		if err := w.wsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			w.logger.Error("WS server error", slog.String("error", err.Error()))
		}
	}()
}

// handleAudioWS 处理来自 FreeSWITCH mod_audio_fork 的 WebSocket 音频流。
// 将接收到的音频帧转发到会话的 AudioIn 通道，
// 将会话的 AudioOut 通道中的音频帧发送回 FreeSWITCH 播放。
func (w *Worker) handleAudioWS(rw http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(rw, "missing session_id", http.StatusBadRequest)
		return
	}

	w.mu.Lock()
	link, ok := w.audioLinks[sessionID]
	w.mu.Unlock()

	if !ok {
		http.Error(rw, "session not found", http.StatusNotFound)
		return
	}

	conn, err := websocket.Accept(rw, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		w.logger.Error("WebSocket 升级失败", slog.String("error", err.Error()))
		return
	}
	defer func() {
		if closeErr := conn.CloseNow(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			w.logger.Warn("close WebSocket", slog.String("error", closeErr.Error()))
		}
	}()

	w.logger.Info("音频 WebSocket 已连接", slog.String("session_id", sessionID))

	ctx := r.Context()
	readDone := w.wsReadLoop(ctx, conn, link.in)
	w.wsWriteLoop(ctx, conn, link.out, readDone, sessionID)
}

// wsReadLoop 启动 goroutine 读取 WebSocket 音频帧并转发到 audioIn 通道。
// 返回的 channel 在读取结束时关闭。
func (w *Worker) wsReadLoop(ctx context.Context, conn *websocket.Conn, audioIn chan<- []byte) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			msgType, data, readErr := conn.Read(ctx)
			if readErr != nil {
				return
			}
			// mod_audio_fork 发送二进制帧（PCM 音频）和文本帧（JSON 元数据）。
			// 仅转发二进制音频帧。
			if msgType != websocket.MessageBinary {
				continue
			}
			select {
			case audioIn <- data:
			default:
				// 缓冲区满，丢弃帧。
			}
		}
	}()
	return done
}

// wsWriteLoop 从 audioOut 通道读取音频帧并通过 WebSocket 发送。
// readDone 关闭时停止写入。
func (w *Worker) wsWriteLoop(ctx context.Context, conn *websocket.Conn, audioOut <-chan []byte, readDone <-chan struct{}, sessionID string) {
	for {
		select {
		case <-readDone:
			w.logger.Info("音频 WebSocket 读取结束", slog.String("session_id", sessionID))
			return
		case <-ctx.Done():
			return
		case frame, chanOpen := <-audioOut:
			if !chanOpen {
				return
			}
			if writeErr := conn.Write(ctx, websocket.MessageBinary, frame); writeErr != nil {
				w.logger.Warn("WebSocket 写入失败", slog.String("error", writeErr.Error()))
				return
			}
		}
	}
}

// slogAdapter 将 slog.Logger 适配为 Asynq Logger 接口。
type slogAdapter struct {
	l *slog.Logger
}

func (a *slogAdapter) Debug(args ...any) { a.l.Debug(fmt.Sprint(args...)) }
func (a *slogAdapter) Info(args ...any)  { a.l.Info(fmt.Sprint(args...)) }
func (a *slogAdapter) Warn(args ...any)  { a.l.Warn(fmt.Sprint(args...)) }
func (a *slogAdapter) Error(args ...any) { a.l.Error(fmt.Sprint(args...)) }
func (a *slogAdapter) Fatal(args ...any) { a.l.Error(fmt.Sprint(args...)) }
