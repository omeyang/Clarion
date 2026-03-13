package postprocess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/notify"
	"github.com/omeyang/clarion/internal/store"
)

// CallCompletionEvent 是通话结束时发布到 Redis 流的载荷。
// 包含后处理所需的全部信息。
type CallCompletionEvent struct {
	CallID          int64                  `json:"call_id"`
	ContactID       int64                  `json:"contact_id"`
	TaskID          int64                  `json:"task_id"`
	Grade           engine.Grade           `json:"grade"`
	CollectedFields map[string]string      `json:"collected_fields"`
	Turns           []dialogue.Turn        `json:"turns"`
	Events          []engine.RecordedEvent `json:"events"`
	Summary         string                 `json:"summary"`
	NextAction      string                 `json:"next_action"`
	DurationSec     int                    `json:"duration_sec"`
	ShouldNotify    bool                   `json:"should_notify"`
	ContactName     string                 `json:"contact_name"`
	ContactPhone    string                 `json:"contact_phone"`
}

// WorkerConfig 持有后处理工作进程的配置。
type WorkerConfig struct {
	StreamKey     string
	ConsumerGroup string
	ConsumerName  string
	BatchSize     int64
	BlockMs       int64
}

// Worker 从 Redis Stream 消费通话完成事件并执行
// 后处理：生成摘要、提取商机、持久化结果和发送通知。
type Worker struct {
	cfg        WorkerConfig
	rds        *store.RDS
	summarizer *Summarizer
	extractor  *OpportunityExtractor
	writer     *Writer
	notifier   notify.Notifier
	logger     *slog.Logger
}

// NewWorker 创建后处理工作进程。
func NewWorker(cfg WorkerConfig, rds *store.RDS, summarizer *Summarizer, writer *Writer, notifier notify.Notifier, logger *slog.Logger) *Worker {
	return &Worker{
		cfg:        cfg,
		rds:        rds,
		summarizer: summarizer,
		writer:     writer,
		notifier:   notifier,
		logger:     logger,
	}
}

// SetOpportunityExtractor 设置商机提取器。可选配置，未设置时跳过商机提取。
func (w *Worker) SetOpportunityExtractor(ext *OpportunityExtractor) {
	w.extractor = ext
}

// Run 启动消费循环。阻塞直到 ctx 取消，优雅关闭时返回 nil。
func (w *Worker) Run(ctx context.Context) error {
	if err := w.ensureConsumerGroup(ctx); err != nil {
		return fmt.Errorf("ensure consumer group: %w", err)
	}

	w.logger.Info("post-processing worker started",
		slog.String("stream", w.cfg.StreamKey),
		slog.String("group", w.cfg.ConsumerGroup),
		slog.String("consumer", w.cfg.ConsumerName),
	)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("post-processing worker shutting down")
			return nil
		default:
		}

		streams, err := w.rds.Client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    w.cfg.ConsumerGroup,
			Consumer: w.cfg.ConsumerName,
			Streams:  []string{w.cfg.StreamKey, ">"},
			Count:    w.cfg.BatchSize,
			Block:    time.Duration(w.cfg.BlockMs) * time.Millisecond,
		}).Result()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			if errors.Is(err, redis.Nil) {
				continue
			}
			w.logger.Error("xreadgroup failed", slog.String("error", err.Error()))
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				w.processMessage(ctx, msg)
			}
		}
	}
}

func (w *Worker) ensureConsumerGroup(ctx context.Context) error {
	err := w.rds.Client.XGroupCreateMkStream(ctx, w.cfg.StreamKey, w.cfg.ConsumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("create consumer group: %w", err)
	}
	return nil
}

func (w *Worker) processMessage(ctx context.Context, msg redis.XMessage) {
	logger := w.logger.With(slog.String("msg_id", msg.ID))

	payload, ok := msg.Values["data"]
	if !ok {
		logger.Warn("message missing data field")
		w.ack(ctx, msg.ID)
		return
	}

	payloadStr, ok := payload.(string)
	if !ok {
		logger.Warn("message data is not a string")
		w.ack(ctx, msg.ID)
		return
	}

	var event CallCompletionEvent
	if err := json.Unmarshal([]byte(payloadStr), &event); err != nil {
		logger.Error("unmarshal event failed", slog.String("error", err.Error()))
		w.ack(ctx, msg.ID)
		return
	}

	logger = logger.With(slog.Int64("call_id", event.CallID))

	// 如果尚未提供摘要则生成。
	if event.Summary == "" && w.summarizer != nil {
		summary, err := w.summarizer.GenerateSummary(ctx, event.Turns, event.CollectedFields)
		if err != nil {
			logger.Error("generate summary failed", slog.String("error", err.Error()))
		} else {
			event.Summary = summary
		}
	}

	// 提取商机信息。
	opp := w.extractOpportunity(ctx, logger, &event)

	// 将结果写入数据库。
	w.writeResults(ctx, logger, &event, opp)

	// 如需发送通知。
	w.sendNotification(ctx, logger, &event)

	w.ack(ctx, msg.ID)
	logger.Info("post-processing completed")
}

func (w *Worker) extractOpportunity(ctx context.Context, logger *slog.Logger, event *CallCompletionEvent) *Opportunity {
	if w.extractor == nil {
		return nil
	}
	opp, err := w.extractor.Extract(ctx, event)
	if err != nil {
		logger.Error("extract opportunity failed", slog.String("error", err.Error()))
		return nil
	}
	return opp
}

func (w *Worker) writeResults(ctx context.Context, logger *slog.Logger, event *CallCompletionEvent, opp *Opportunity) {
	if w.writer == nil {
		return
	}
	if err := w.writer.WriteCallResult(ctx, event); err != nil {
		logger.Error("write call result failed", slog.String("error", err.Error()))
	}
	if err := w.writer.WriteTurns(ctx, event.CallID, event.Turns); err != nil {
		logger.Error("write turns failed", slog.String("error", err.Error()))
	}
	if err := w.writer.WriteEvents(ctx, event.CallID, event.Events); err != nil {
		logger.Error("write events failed", slog.String("error", err.Error()))
	}
	if opp != nil {
		if err := w.writer.WriteOpportunity(ctx, opp); err != nil {
			logger.Error("write opportunity failed", slog.String("error", err.Error()))
		}
	}
}

func (w *Worker) sendNotification(ctx context.Context, logger *slog.Logger, event *CallCompletionEvent) {
	if !event.ShouldNotify || w.notifier == nil {
		return
	}
	notification := notify.FollowUpNotification{
		CallID:          event.CallID,
		ContactName:     event.ContactName,
		ContactPhone:    event.ContactPhone,
		Grade:           string(event.Grade),
		Summary:         event.Summary,
		CollectedFields: event.CollectedFields,
		NextAction:      event.NextAction,
	}
	if err := w.notifier.SendFollowUpNotification(ctx, notification); err != nil {
		logger.Error("send notification failed", slog.String("error", err.Error()))
	}
}

func (w *Worker) ack(ctx context.Context, msgID string) {
	if err := w.rds.Client.XAck(ctx, w.cfg.StreamKey, w.cfg.ConsumerGroup, msgID).Err(); err != nil {
		w.logger.Error("xack failed",
			slog.String("msg_id", msgID),
			slog.String("error", err.Error()),
		)
	}
}
