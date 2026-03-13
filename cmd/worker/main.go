// Command worker 启动外呼工作进程。
//
// 它连接 Redis 进行任务排队、连接 FreeSWITCH 进行通话控制，
// 并为每通电话运行流式 ASR→LLM→TTS 管线。
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	sonataprovider "github.com/omeyang/Sonata/engine/aiface"
	"github.com/omeyang/Sonata/sherpa"
	"github.com/omeyang/clarion/internal/call"
	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/observe"
	observeebpf "github.com/omeyang/clarion/internal/observe/ebpf"
	"github.com/omeyang/clarion/internal/provider"
	"github.com/omeyang/clarion/internal/provider/asr"
	"github.com/omeyang/clarion/internal/provider/llm"
	"github.com/omeyang/clarion/internal/provider/tts"
	"github.com/omeyang/clarion/internal/store"
)

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	_ = lv.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}

func main() {
	app := &cli.Command{
		Name:  "clarion-worker",
		Usage: "Call Worker process",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Value:   "clarion.toml",
				Usage:   "path to config file",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return run(cmd.String("config"))
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "clarion-worker: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg.Server.LogLevel)
	logger.Info("clarion-worker starting", slog.String("config", cfg.String()))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	rds, err := store.NewRDS(ctx, cfg.Redis, logger)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer func() {
		if closeErr := rds.Close(); closeErr != nil {
			logger.Warn("redis close error", slog.String("error", closeErr.Error()))
		}
	}()

	worker := call.NewWorker(*cfg, rds, logger)

	// 初始化 OTel 度量收集器（网络质量、延迟、计数器等）。
	metrics, err := observe.NewCallMetrics()
	if err != nil {
		return fmt.Errorf("init call metrics: %w", err)
	}
	worker.SetMetrics(metrics)
	logger.Info("OTel 度量收集器已初始化")

	// 初始化 Silero VAD（ML 级语音活动检测）。
	if cfg.SileroVAD.Enabled {
		vad, vadErr := initSileroVAD(cfg, logger)
		if vadErr != nil {
			return fmt.Errorf("init Silero VAD: %w", vadErr)
		}
		defer func() {
			if closeErr := vad.Close(); closeErr != nil {
				logger.Warn("Silero VAD close error", slog.String("error", closeErr.Error()))
			}
		}()
		worker.SetSpeechDetector(vad)
	}

	// 初始化 eBPF 内核级观测（可选）。
	if cfg.Observe.EBPF.Enabled {
		if stopFn := startEBPFProbes(ctx, cfg, logger); stopFn != nil {
			defer stopFn()
		}
	}

	// 初始化 AI 服务提供者。
	providerCleanup, err := setupProviders(ctx, cfg, worker, logger)
	if err != nil {
		return fmt.Errorf("setup providers: %w", err)
	}
	defer providerCleanup()

	logger.Info("clarion-worker running")

	if err := worker.Run(ctx); err != nil {
		return fmt.Errorf("worker: %w", err)
	}

	logger.Info("clarion-worker stopped")
	return nil
}

// setupProviders 初始化 AI 服务提供者并注入到 Worker。
// 返回的 cleanup 函数用于释放提供者资源，调用方须在退出时 defer 调用。
// ASR API Key 未配置时跳过初始化并返回空操作 cleanup。
func setupProviders(ctx context.Context, cfg *config.Config, worker *call.Worker, logger *slog.Logger) (cleanup func(), retErr error) {
	noop := func() {}
	if cfg.ASR.APIKey == "" {
		logger.Warn("ASR API Key 未配置，AI 功能不可用")
		return noop, nil
	}

	asrProvider, ttsProvider, localCloser, err := initProviders(cfg, logger)
	if err != nil {
		return noop, fmt.Errorf("init providers: %w", err)
	}

	llmProvider := llm.NewDeepSeek(cfg.LLM.APIKey, cfg.LLM.BaseURL, llm.WithLogger(logger))
	worker.SetProviders(asrProvider, llmProvider, ttsProvider)

	// 预热连接池，避免首次调用时的冷启动延迟。
	warmupProviders(ctx, logger, llmProvider, ttsProvider)

	logger.Info("AI providers 已初始化",
		slog.String("asr", cfg.ASR.Provider),
		slog.String("llm", cfg.LLM.Provider),
		slog.String("tts", cfg.TTS.Provider),
		slog.Bool("racing_asr", cfg.LocalASR.Enabled),
		slog.Bool("tiered_tts", cfg.LocalTTS.Enabled),
	)

	cleanup = func() {
		// 关闭本地模型资源（释放 sherpa-onnx C 内存）。
		closeIfNotNil(localCloser)
		// 关闭 TTS 连接池（如已启用）。
		if closer, ok := ttsProvider.(interface{ Close() error }); ok {
			closeWithLog(logger, "TTS provider", closer)
		}
	}
	return cleanup, nil
}

// initProviders 初始化 ASR 和 TTS 提供者。
// 启用本地模型时，使用 RacingASR（本地+云端竞速）和 TieredTTS（短文本本地、长文本云端）。
// 返回的 io.Closer 用于释放本地模型的 C 资源，调用方须在退出时 defer Close()。
func initProviders(cfg *config.Config, logger *slog.Logger) (provider.ASRProvider, provider.TTSProvider, io.Closer, error) {
	cloudASR := asr.NewQwen(cfg.ASR.APIKey)
	cloudTTS := tts.NewDashScope(cfg.TTS.APIKey,
		tts.WithDashScopePoolSize(cfg.TTS.PoolSize),
		tts.WithDashScopeLogger(logger))

	asrProvider, asrCloser, err := buildASRProvider(cfg, cloudASR, logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build ASR provider: %w", err)
	}

	ttsProvider, ttsCloser, err := buildTTSProvider(cfg, cloudTTS, logger)
	if err != nil {
		// 构建 TTS 失败时，清理已创建的 ASR 本地资源。
		closeIfNotNil(asrCloser)
		return nil, nil, nil, fmt.Errorf("build TTS provider: %w", err)
	}

	closer := combineClosers(asrCloser, ttsCloser)
	return asrProvider, ttsProvider, closer, nil
}

// buildASRProvider 根据配置构建 ASR 提供者。
// 启用本地 ASR 时返回 RacingASR，否则返回云端 ASR。
// 返回的 io.Closer 用于释放本地 ASR 的 C 资源，未启用本地时为 nil。
func buildASRProvider(cfg *config.Config, cloudASR provider.ASRProvider, logger *slog.Logger) (provider.ASRProvider, io.Closer, error) {
	if !cfg.LocalASR.Enabled {
		return cloudASR, nil, nil
	}

	localASR, err := sherpa.NewLocalASR(sherpa.LocalASRConfig{
		EncoderPath: cfg.LocalASR.EncoderPath,
		DecoderPath: cfg.LocalASR.DecoderPath,
		TokensPath:  cfg.LocalASR.TokensPath,
		NumThreads:  cfg.LocalASR.NumThreads,
		SampleRate:  cfg.ASR.SampleRate,
		Logger:      logger,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create local ASR: %w", err)
	}

	racing, err := sherpa.NewRacingASR(sherpa.RacingASRConfig{
		Local:  localASR,
		Cloud:  cloudASR,
		Logger: logger,
	})
	if err != nil {
		closeIfNotNil(localASR)
		return nil, nil, fmt.Errorf("create racing ASR: %w", err)
	}

	logger.Info("RacingASR 已启用（本地 Paraformer + 云端竞速）")
	return racing, localASR, nil
}

// buildTTSProvider 根据配置构建 TTS 提供者。
// 启用本地 TTS 时返回 TieredTTS，否则返回云端 TTS。
// 返回的 io.Closer 用于释放本地 TTS 的 C 资源，未启用本地时为 nil。
func buildTTSProvider(cfg *config.Config, cloudTTS provider.TTSProvider, logger *slog.Logger) (provider.TTSProvider, io.Closer, error) {
	if !cfg.LocalTTS.Enabled {
		return cloudTTS, nil, nil
	}

	localTTS, err := sherpa.NewLocalTTS(sherpa.LocalTTSConfig{
		ModelPath:   cfg.LocalTTS.ModelPath,
		TokensPath:  cfg.LocalTTS.TokensPath,
		DataDir:     cfg.LocalTTS.DataDir,
		DictDir:     cfg.LocalTTS.DictDir,
		LexiconPath: cfg.LocalTTS.LexiconPath,
		RuleFsts:    cfg.LocalTTS.RuleFsts,
		RuleFars:    cfg.LocalTTS.RuleFars,
		NumThreads:  cfg.LocalTTS.NumThreads,
		SpeakerID:   cfg.LocalTTS.SpeakerID,
		Speed:       cfg.LocalTTS.Speed,
		SampleRate:  cfg.TTS.SampleRate,
		Logger:      logger,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create local TTS: %w", err)
	}

	tiered, err := sherpa.NewTieredTTS(sherpa.TieredTTSConfig{
		Local:     localTTS,
		Cloud:     cloudTTS,
		Threshold: cfg.LocalTTS.Threshold,
		Logger:    logger,
	})
	if err != nil {
		closeIfNotNil(localTTS)
		return nil, nil, fmt.Errorf("create tiered TTS: %w", err)
	}

	logger.Info("TieredTTS 已启用（短文本本地、长文本云端）",
		slog.Int("threshold", cfg.LocalTTS.Threshold),
	)
	return tiered, localTTS, nil
}

// warmupProviders 预热实现了 Warmer 接口的提供者。
// 预热失败不影响启动，仅记录警告日志。
func warmupProviders(ctx context.Context, logger *slog.Logger, providers ...any) {
	for _, p := range providers {
		w, ok := p.(sonataprovider.Warmer)
		if !ok {
			continue
		}
		if err := w.Warmup(ctx); err != nil {
			logger.Warn("provider warmup failed", slog.String("error", err.Error()))
		}
	}
}

// initSileroVAD 初始化 Silero VAD 语音活动检测器。
func initSileroVAD(cfg *config.Config, logger *slog.Logger) (*sherpa.SileroVAD, error) {
	vad, err := sherpa.NewSileroVAD(sherpa.SileroVADConfig{
		ModelPath:          cfg.SileroVAD.ModelPath,
		Threshold:          cfg.SileroVAD.Threshold,
		MinSilenceDuration: cfg.SileroVAD.MinSilenceDuration,
		MinSpeechDuration:  cfg.SileroVAD.MinSpeechDuration,
		SampleRate:         cfg.SileroVAD.SampleRate,
	})
	if err != nil {
		return nil, fmt.Errorf("create Silero VAD: %w", err)
	}

	logger.Info("Silero VAD 已启用",
		slog.String("model", cfg.SileroVAD.ModelPath),
		slog.Float64("threshold", float64(cfg.SileroVAD.Threshold)),
	)
	return vad, nil
}

// closeWithLog 关闭资源并在失败时记录警告日志。
func closeWithLog(logger *slog.Logger, name string, c io.Closer) {
	if err := c.Close(); err != nil {
		logger.Warn(name+" close error", slog.String("error", err.Error()))
	}
}

// closeIfNotNil 安全关闭可能为 nil 的 io.Closer。
func closeIfNotNil(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}

// combineClosers 将多个 io.Closer 合并为一个，任一为 nil 时跳过。
// 全部为 nil 时返回 nil。
func combineClosers(closers ...io.Closer) io.Closer {
	var valid []io.Closer
	for _, c := range closers {
		if c != nil {
			valid = append(valid, c)
		}
	}
	if len(valid) == 0 {
		return nil
	}
	return multiCloser(valid)
}

// multiCloser 实现 io.Closer，按顺序关闭多个资源并聚合错误。
type multiCloser []io.Closer

func (mc multiCloser) Close() error {
	var errs []error
	for _, c := range mc {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// startEBPFProbes 初始化并启动 eBPF 探针，返回 stop 清理函数。
// 初始化或启动失败时仅记录警告并返回 nil（不阻塞主流程）。
func startEBPFProbes(ctx context.Context, cfg *config.Config, logger *slog.Logger) func() {
	probes, err := observeebpf.NewProbeManager(observeebpf.Config{
		Enabled:      cfg.Observe.EBPF.Enabled,
		TCPTrace:     cfg.Observe.EBPF.TCPTrace,
		SchedLatency: cfg.Observe.EBPF.SchedLatency,
	}, logger)
	if err != nil {
		logger.Warn("eBPF 探针初始化失败，跳过内核级观测", slog.String("error", err.Error()))
		return nil
	}
	if probes == nil {
		return nil
	}
	if startErr := probes.Start(ctx); startErr != nil {
		logger.Warn("eBPF 探针启动失败", slog.String("error", startErr.Error()))
		return nil
	}
	logger.Info("eBPF 内核级观测已启动")
	return func() {
		if stopErr := probes.Stop(); stopErr != nil {
			logger.Warn("eBPF 探针停止失败", slog.String("error", stopErr.Error()))
		}
	}
}
