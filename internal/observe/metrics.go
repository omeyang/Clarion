// Package observe 提供可观测性基础设施（度量、追踪）。
package observe

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// CallMetrics 收集呼叫管线的运行时度量。
type CallMetrics struct {
	// 延迟直方图。
	asrLatency    metric.Float64Histogram
	llmFirstToken metric.Float64Histogram
	ttsFirstChunk metric.Float64Histogram
	turnLatency   metric.Float64Histogram

	// 计数器。
	bargeInTotal    metric.Int64Counter
	silenceTimeout  metric.Int64Counter
	providerErrors  metric.Int64Counter
	callsCompleted  metric.Int64Counter
	fillerPlayed    metric.Int64Counter
	speculativeHit  metric.Int64Counter
	speculativeMiss metric.Int64Counter

	// 网络质量计数器。
	audioGapTotal    metric.Int64Counter
	poorNetworkTotal metric.Int64Counter
	lowVolumeTotal   metric.Int64Counter

	// 网络质量直方图。
	jitterAvg     metric.Float64Histogram
	lossRate      metric.Float64Histogram
	lowVolumeRate metric.Float64Histogram

	// 仪表盘。
	activeCalls metric.Int64UpDownCounter
}

// NewCallMetrics 创建并注册 OTel 度量。
func NewCallMetrics() (*CallMetrics, error) {
	meter := otel.Meter("clarion.call")

	m := &CallMetrics{}

	if err := m.registerHistograms(meter); err != nil {
		return nil, err
	}
	if err := m.registerCounters(meter); err != nil {
		return nil, err
	}
	if err := m.registerNetworkQuality(meter); err != nil {
		return nil, err
	}

	return m, nil
}

// registerHistograms 注册延迟直方图度量。
func (m *CallMetrics) registerHistograms(meter metric.Meter) error {
	var err error

	m.asrLatency, err = meter.Float64Histogram("clarion.asr.latency",
		metric.WithDescription("ASR 识别延迟（秒）"),
		metric.WithUnit("s"))
	if err != nil {
		return fmt.Errorf("create asr latency histogram: %w", err)
	}

	m.llmFirstToken, err = meter.Float64Histogram("clarion.llm.first_token",
		metric.WithDescription("LLM 首 token 延迟（秒）"),
		metric.WithUnit("s"))
	if err != nil {
		return fmt.Errorf("create llm first token histogram: %w", err)
	}

	m.ttsFirstChunk, err = meter.Float64Histogram("clarion.tts.first_chunk",
		metric.WithDescription("TTS 首包延迟（秒）"),
		metric.WithUnit("s"))
	if err != nil {
		return fmt.Errorf("create tts first chunk histogram: %w", err)
	}

	m.turnLatency, err = meter.Float64Histogram("clarion.turn.latency",
		metric.WithDescription("完整轮次延迟（秒）"),
		metric.WithUnit("s"))
	if err != nil {
		return fmt.Errorf("create turn latency histogram: %w", err)
	}

	return nil
}

// registerCounters 注册计数器和仪表盘度量。
func (m *CallMetrics) registerCounters(meter metric.Meter) error {
	var err error

	m.bargeInTotal, err = meter.Int64Counter("clarion.barge_in.total",
		metric.WithDescription("Barge-in 事件总数"))
	if err != nil {
		return fmt.Errorf("create barge-in counter: %w", err)
	}

	m.silenceTimeout, err = meter.Int64Counter("clarion.silence_timeout.total",
		metric.WithDescription("静默超时事件总数"))
	if err != nil {
		return fmt.Errorf("create silence timeout counter: %w", err)
	}

	m.providerErrors, err = meter.Int64Counter("clarion.provider.errors.total",
		metric.WithDescription("提供者错误总数"))
	if err != nil {
		return fmt.Errorf("create provider errors counter: %w", err)
	}

	m.callsCompleted, err = meter.Int64Counter("clarion.calls.completed.total",
		metric.WithDescription("完成的呼叫总数"))
	if err != nil {
		return fmt.Errorf("create calls completed counter: %w", err)
	}

	m.activeCalls, err = meter.Int64UpDownCounter("clarion.calls.active",
		metric.WithDescription("当前活跃呼叫数"))
	if err != nil {
		return fmt.Errorf("create active calls gauge: %w", err)
	}

	m.fillerPlayed, err = meter.Int64Counter("clarion.filler.played.total",
		metric.WithDescription("填充词播放总数"))
	if err != nil {
		return fmt.Errorf("create filler played counter: %w", err)
	}

	m.speculativeHit, err = meter.Int64Counter("clarion.speculative.hit.total",
		metric.WithDescription("预推理命中总数"))
	if err != nil {
		return fmt.Errorf("create speculative hit counter: %w", err)
	}

	m.speculativeMiss, err = meter.Int64Counter("clarion.speculative.miss.total",
		metric.WithDescription("预推理未命中总数"))
	if err != nil {
		return fmt.Errorf("create speculative miss counter: %w", err)
	}

	return nil
}

// registerNetworkQuality 注册网络质量相关度量。
func (m *CallMetrics) registerNetworkQuality(meter metric.Meter) error {
	var err error

	m.audioGapTotal, err = meter.Int64Counter("clarion.network.audio_gap.total",
		metric.WithDescription("音频间隙事件总数"))
	if err != nil {
		return fmt.Errorf("create audio gap counter: %w", err)
	}

	m.poorNetworkTotal, err = meter.Int64Counter("clarion.network.poor_network.total",
		metric.WithDescription("弱网检测事件总数"))
	if err != nil {
		return fmt.Errorf("create poor network counter: %w", err)
	}

	m.jitterAvg, err = meter.Float64Histogram("clarion.network.jitter_avg",
		metric.WithDescription("帧到达抖动平均值（毫秒）"),
		metric.WithUnit("ms"))
	if err != nil {
		return fmt.Errorf("create jitter avg histogram: %w", err)
	}

	m.lowVolumeTotal, err = meter.Int64Counter("clarion.network.low_volume.total",
		metric.WithDescription("低音量事件总数"))
	if err != nil {
		return fmt.Errorf("create low volume counter: %w", err)
	}

	m.lossRate, err = meter.Float64Histogram("clarion.network.loss_rate",
		metric.WithDescription("通话结束时的丢帧率（0~1）"))
	if err != nil {
		return fmt.Errorf("create loss rate histogram: %w", err)
	}

	m.lowVolumeRate, err = meter.Float64Histogram("clarion.network.low_volume_rate",
		metric.WithDescription("通话结束时的低音量帧占比（0~1）"))
	if err != nil {
		return fmt.Errorf("create low volume rate histogram: %w", err)
	}

	return nil
}

// IncAudioGap 记录音频间隙事件。
func (m *CallMetrics) IncAudioGap() {
	m.audioGapTotal.Add(context.Background(), 1)
}

// IncPoorNetwork 记录弱网检测事件。
func (m *CallMetrics) IncPoorNetwork() {
	m.poorNetworkTotal.Add(context.Background(), 1)
}

// RecordJitterAvg 记录帧到达抖动平均值。
func (m *CallMetrics) RecordJitterAvg(ms float64) {
	m.jitterAvg.Record(context.Background(), ms)
}

// IncLowVolume 记录低音量事件。
func (m *CallMetrics) IncLowVolume() {
	m.lowVolumeTotal.Add(context.Background(), 1)
}

// RecordLossRate 记录通话结束时的丢帧率。
func (m *CallMetrics) RecordLossRate(rate float64) {
	m.lossRate.Record(context.Background(), rate)
}

// RecordLowVolumeRate 记录通话结束时的低音量帧占比。
func (m *CallMetrics) RecordLowVolumeRate(rate float64) {
	m.lowVolumeRate.Record(context.Background(), rate)
}

// RecordASRLatency 记录 ASR 延迟。
func (m *CallMetrics) RecordASRLatency(d time.Duration) {
	m.asrLatency.Record(context.Background(), d.Seconds())
}

// RecordLLMFirstToken 记录 LLM 首 token 延迟。
func (m *CallMetrics) RecordLLMFirstToken(d time.Duration) {
	m.llmFirstToken.Record(context.Background(), d.Seconds())
}

// RecordTTSFirstChunk 记录 TTS 首包延迟。
func (m *CallMetrics) RecordTTSFirstChunk(d time.Duration) {
	m.ttsFirstChunk.Record(context.Background(), d.Seconds())
}

// RecordTurnLatency 记录完整轮次延迟。
func (m *CallMetrics) RecordTurnLatency(d time.Duration) {
	m.turnLatency.Record(context.Background(), d.Seconds())
}

// IncBargeIn 记录 barge-in 事件。
func (m *CallMetrics) IncBargeIn() {
	m.bargeInTotal.Add(context.Background(), 1)
}

// IncSilenceTimeout 记录静默超时。
func (m *CallMetrics) IncSilenceTimeout() {
	m.silenceTimeout.Add(context.Background(), 1)
}

// IncProviderError 记录提供者错误。
func (m *CallMetrics) IncProviderError(provider string) {
	m.providerErrors.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("provider", provider)))
}

// IncCallCompleted 记录完成的呼叫。
func (m *CallMetrics) IncCallCompleted() {
	m.callsCompleted.Add(context.Background(), 1)
}

// SetCallActive 调整活跃通话数。
func (m *CallMetrics) SetCallActive(delta int) {
	m.activeCalls.Add(context.Background(), int64(delta))
}

// IncFillerPlayed 记录填充词播放事件。
func (m *CallMetrics) IncFillerPlayed() {
	m.fillerPlayed.Add(context.Background(), 1)
}

// IncSpeculativeHit 记录预推理命中。
func (m *CallMetrics) IncSpeculativeHit() {
	m.speculativeHit.Add(context.Background(), 1)
}

// IncSpeculativeMiss 记录预推理未命中。
func (m *CallMetrics) IncSpeculativeMiss() {
	m.speculativeMiss.Add(context.Background(), 1)
}
