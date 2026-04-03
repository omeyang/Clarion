// Command clarion 启动 API 服务器或运行文本模拟模式。
//
// 用法：
//
//	clarion serve --config clarion.toml   # 启动 API 服务器（默认）
//	clarion simulate --config clarion.toml # 启动文本模拟
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/omeyang/clarion/internal/api"
	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/service"
	"github.com/omeyang/clarion/internal/simulate"
	"github.com/omeyang/clarion/internal/store"
)

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	_ = lv.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}

func main() {
	app := &cli.Command{
		Name:  "clarion",
		Usage: "AI Outbound Voice Engine",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Value:   "clarion.toml",
				Usage:   "path to config file",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runServe(cmd.String("config"))
		},
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "Start the API server",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runServe(cmd.Root().String("config"))
				},
			},
			adminCommands(),
			{
				Name:  "simulate",
				Usage: "Start text simulation mode",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "config",
						Aliases: []string{"c"},
						Usage:   "path to config file (optional)",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfgPath := cmd.String("config")
					if cfgPath == "" {
						cfgPath = cmd.Root().String("config")
					}
					return runSimulate(cfgPath)
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runServe(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg.Server.LogLevel)
	logger.Info("starting clarion API server", slog.String("config", cfg.String()))

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

	services := &api.Services{
		Contacts:  service.NewContactSvc(store.NewPgContactStore(db.Pool)),
		Templates: service.NewTemplateSvc(store.NewPgTemplateStore(db.Pool)),
		Tasks:     service.NewTaskSvc(store.NewPgTaskStore(db.Pool)),
		Calls:     service.NewCallSvc(store.NewPgCallStore(db.Pool)),
	}
	handler := api.Router(logger, services)

	if cfg.Server.Debug {
		logger.Info("debug mode enabled, pprof available at /debug/pprof/")
		mux := http.NewServeMux()
		mux.Handle("/debug/pprof/", http.DefaultServeMux)
		mux.Handle("/", handler)
		handler = mux
	}

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", cfg.Server.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("received signal, shutting down", slog.String("signal", sig.String()))
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	logger.Info("server stopped gracefully")
	return nil
}

func runSimulate(configPath string) error {
	logger := newLogger("debug")

	if configPath != "" {
		_, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	sim, err := simulate.NewSimulator(simulate.SimulatorConfig{
		LLM:    nil,
		Logger: logger,
		RequiredFields: []string{
			"company_name", "contact_person", "phone",
		},
		MaxTurns: 20,
		Input:    os.Stdin,
		Output:   os.Stdout,
	})
	if err != nil {
		return fmt.Errorf("create simulator: %w", err)
	}

	if err := sim.Run(ctx); err != nil {
		return fmt.Errorf("run simulator: %w", err)
	}
	return nil
}
