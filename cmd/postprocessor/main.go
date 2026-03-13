// Command postprocessor 启动后处理工作进程，从 Redis Stream 消费
// 通话完成事件，执行摘要生成、结果持久化和跟进通知。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/notify"
	"github.com/omeyang/clarion/internal/postprocess"
	"github.com/omeyang/clarion/internal/provider/llm"
	"github.com/omeyang/clarion/internal/store"
)

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	_ = lv.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}

func main() {
	app := &cli.Command{
		Name:  "clarion-postprocessor",
		Usage: "Post-Processing Worker",
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
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg.Server.LogLevel)
	logger.Info("starting clarion post-processor", slog.String("config", cfg.String()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := store.NewDB(ctx, cfg.Database, logger)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer db.Close()

	rds, err := store.NewRDS(ctx, cfg.Redis, logger)
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer func() {
		if err := rds.Close(); err != nil {
			logger.Warn("redis close error", slog.String("error", err.Error()))
		}
	}()

	// 初始化 LLM 提供者用于摘要生成和商机提取。
	llmProvider := initLLMProvider(cfg, logger)

	summarizer := postprocess.NewSummarizer(llmProvider, logger)
	writer := postprocess.NewWriter(db.Pool, logger)

	var notifier notify.Notifier
	if cfg.Notification.Enabled && cfg.Notification.WeChatWebhookURL != "" {
		notifier = notify.NewWeChatNotifier(cfg.Notification.WeChatWebhookURL, logger)
		logger.Info("wechat notification enabled")
	}

	workerCfg := postprocess.WorkerConfig{
		StreamKey:     cfg.Redis.EventStreamKey,
		ConsumerGroup: cfg.PostProcessor.ConsumerGroup,
		ConsumerName:  cfg.PostProcessor.ConsumerName,
		BatchSize:     cfg.PostProcessor.BatchSize,
		BlockMs:       cfg.PostProcessor.BlockMs,
	}

	w := postprocess.NewWorker(workerCfg, rds, summarizer, writer, notifier, logger)

	// 设置商机提取器（LLM 可用时使用 LLM 提取，否则回退到规则提取）。
	extractor := postprocess.NewOpportunityExtractor(llmProvider, logger)
	w.SetOpportunityExtractor(extractor)

	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Run(ctx)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("received signal, shutting down", slog.String("signal", sig.String()))
		cancel()
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("worker error: %w", err)
		}
	}

	logger.Info("post-processor stopped gracefully")
	return nil
}

// initLLMProvider 根据配置初始化 LLM 提供者。
// API Key 未配置时返回 nil，后续的 Summarizer 和 OpportunityExtractor 会回退到规则模式。
func initLLMProvider(cfg *config.Config, logger *slog.Logger) *llm.DeepSeek {
	if cfg.LLM.APIKey == "" {
		logger.Warn("LLM API Key 未配置，摘要生成和商机提取将使用规则模式")
		return nil
	}
	logger.Info("LLM 提供者已初始化",
		slog.String("provider", cfg.LLM.Provider),
		slog.String("model", cfg.LLM.Model),
	)
	return llm.NewDeepSeek(cfg.LLM.APIKey, cfg.LLM.BaseURL)
}
