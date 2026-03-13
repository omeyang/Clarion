package call

import (
	"context"
	"fmt"

	"github.com/omeyang/clarion/internal/provider/realtime"
	"github.com/omeyang/clarion/internal/provider/strategy"
)

// omniAdapter 将 realtime.Omni 适配为 RealtimeVoice 接口。
type omniAdapter struct {
	inner *realtime.Omni
}

// NewOmniAdapter 创建 Omni 到 RealtimeVoice 的适配器。
func NewOmniAdapter(o *realtime.Omni) RealtimeVoice {
	return &omniAdapter{inner: o}
}

func (a *omniAdapter) Connect(ctx context.Context, cfg RealtimeVoiceConfig) error {
	if err := a.inner.Connect(ctx, realtime.ConnectConfig{
		Model:             cfg.Model,
		Voice:             cfg.Voice,
		Instructions:      cfg.Instructions,
		InputSampleRate:   cfg.InputSampleRate,
		OutputSampleRate:  cfg.OutputSampleRate,
		VADEnabled:        cfg.VADEnabled,
		VADThreshold:      cfg.VADThreshold,
		SilenceDurationMs: cfg.SilenceDurationMs,
	}); err != nil {
		return fmt.Errorf("connect realtime: %w", err)
	}
	return nil
}

func (a *omniAdapter) FeedAudio(ctx context.Context, frame []byte) error {
	if err := a.inner.FeedAudio(ctx, frame); err != nil {
		return fmt.Errorf("feed audio: %w", err)
	}
	return nil
}

func (a *omniAdapter) AudioOut() <-chan []byte {
	return a.inner.AudioOut()
}

func (a *omniAdapter) Transcripts() <-chan TranscriptEvent {
	// 将 realtime.TranscriptEvent 转换为 call.TranscriptEvent。
	rtCh := a.inner.Transcripts()
	ch := make(chan TranscriptEvent, cap(rtCh))
	go func() {
		defer close(ch)
		for evt := range rtCh {
			ch <- TranscriptEvent{
				Role:      evt.Role,
				Text:      evt.Text,
				IsFinal:   evt.IsFinal,
				Timestamp: evt.Timestamp,
			}
		}
	}()
	return ch
}

func (a *omniAdapter) UpdateInstructions(ctx context.Context, instructions string) error {
	if err := a.inner.UpdateInstructions(ctx, instructions); err != nil {
		return fmt.Errorf("update instructions: %w", err)
	}
	return nil
}

func (a *omniAdapter) Interrupt(ctx context.Context) error {
	if err := a.inner.Interrupt(ctx); err != nil {
		return fmt.Errorf("interrupt: %w", err)
	}
	return nil
}

func (a *omniAdapter) Close() error {
	if err := a.inner.Close(); err != nil {
		return fmt.Errorf("close realtime: %w", err)
	}
	return nil
}

// smartAdapter 将 strategy.Smart 适配为 DialogueStrategy 接口。
type smartAdapter struct {
	inner *strategy.Smart
}

// NewSmartAdapter 创建 Smart 到 DialogueStrategy 的适配器。
func NewSmartAdapter(s *strategy.Smart) DialogueStrategy {
	return &smartAdapter{inner: s}
}

func (a *smartAdapter) Analyze(ctx context.Context, input StrategyInput) (*Decision, error) {
	d, err := a.inner.Analyze(ctx, strategy.Input{
		UserText:      input.UserText,
		AssistantText: input.AssistantText,
		TurnNumber:    input.TurnNumber,
		CurrentFields: input.CurrentFields,
	})
	if err != nil {
		return nil, fmt.Errorf("strategy analyze: %w", err)
	}
	return &Decision{
		Intent:          d.Intent,
		ExtractedFields: d.ExtractedFields,
		Instructions:    d.Instructions,
		ShouldEnd:       d.ShouldEnd,
		Grade:           d.Grade,
	}, nil
}
