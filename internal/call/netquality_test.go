package call

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nqTestFrameSamples 测试用音频帧采样数（8kHz 下 20ms = 160 采样）。
const nqTestFrameSamples = 160

// makeFrameWithAmplitude 生成指定幅值的 PCM16 音频帧。
func makeFrameWithAmplitude(amplitude int16) []byte {
	frame := make([]byte, nqTestFrameSamples*2)
	for i := range nqTestFrameSamples {
		binary.LittleEndian.PutUint16(frame[i*2:], uint16(amplitude))
	}
	return frame
}

func TestNewNetworkQuality_默认配置(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()
	nq := NewNetworkQuality(cfg, testLogger())
	require.NotNil(t, nq)

	snap := nq.Snapshot()
	assert.Equal(t, 0, snap.FrameCount)
	assert.Equal(t, 0, snap.GapCount)
	assert.Equal(t, 0.0, snap.JitterAvgMs)
	assert.Equal(t, 0.0, snap.LossRate)
}

func TestNetworkQuality_均匀帧无告警(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()
	cfg.ReportIntervalFrames = 0 // 关闭日志输出。
	nq := NewNetworkQuality(cfg, testLogger())

	frame := makeFrameWithAmplitude(8000)
	now := time.Now()

	// 以 20ms 间隔发送 100 帧，不应产生任何告警。
	for i := range 100 {
		events := nq.RecordFrame(frame, now.Add(time.Duration(i)*20*time.Millisecond))
		for _, e := range events {
			assert.NotEqual(t, NetEventAudioGap, e, "均匀帧不应产生间隙事件")
		}
	}

	snap := nq.Snapshot()
	assert.Equal(t, 100, snap.FrameCount)
	assert.InDelta(t, 0.0, snap.JitterAvgMs, 1.0, "均匀帧抖动应接近 0")
	assert.Equal(t, 0.0, snap.LossRate, "均匀帧不应有丢帧")
	assert.Equal(t, 0, snap.GapCount)
}

func TestNetworkQuality_音频间隙检测(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()
	cfg.GapThresholdMs = 100.0
	cfg.ReportIntervalFrames = 0
	nq := NewNetworkQuality(cfg, testLogger())

	frame := makeFrameWithAmplitude(8000)
	now := time.Now()

	// 发送第一帧。
	nq.RecordFrame(frame, now)

	// 200ms 间隔 → 应检测到间隙。
	events := nq.RecordFrame(frame, now.Add(200*time.Millisecond))
	assert.Contains(t, events, NetEventAudioGap)

	snap := nq.Snapshot()
	assert.Equal(t, 1, snap.GapCount)
}

func TestNetworkQuality_高抖动告警(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()
	cfg.WindowSize = 10
	cfg.JitterThresholdMs = 15.0
	cfg.GapThresholdMs = 500.0 // 提高间隙阈值避免干扰。
	cfg.ReportIntervalFrames = 0
	nq := NewNetworkQuality(cfg, testLogger())

	frame := makeFrameWithAmplitude(8000)
	now := time.Now()

	// 交替 10ms 和 30ms 到达，平均抖动 = 10ms > 阈值 15ms 可能不触发，
	// 改为交替 5ms 和 55ms，平均抖动 = (15+35)/2 = 25ms > 15ms。
	var lastEvents []string
	for i := range 20 {
		var interval time.Duration
		if i%2 == 0 {
			interval = 5 * time.Millisecond
		} else {
			interval = 55 * time.Millisecond
		}
		now = now.Add(interval)
		lastEvents = nq.RecordFrame(frame, now)
	}

	// 窗口填满后应出现 poor_network 事件。
	snap := nq.Snapshot()
	assert.Greater(t, snap.JitterAvgMs, 15.0, "平均抖动应超过阈值")
	// 最后一帧的事件应包含 poor_network。
	assert.Contains(t, lastEvents, NetEventPoorNetwork)
}

func TestNetworkQuality_低音量检测(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()
	cfg.LowVolumeDBFS = -55.0
	cfg.ReportIntervalFrames = 0
	nq := NewNetworkQuality(cfg, testLogger())

	// 极低幅值帧（接近静默）。
	quietFrame := makeFrameWithAmplitude(10)
	// 正常音量帧。
	normalFrame := makeFrameWithAmplitude(8000)

	now := time.Now()

	// 发 5 个安静帧 + 5 个正常帧。
	for i := range 5 {
		nq.RecordFrame(quietFrame, now.Add(time.Duration(i)*20*time.Millisecond))
	}
	for i := range 5 {
		nq.RecordFrame(normalFrame, now.Add(time.Duration(5+i)*20*time.Millisecond))
	}

	snap := nq.Snapshot()
	assert.Equal(t, 10, snap.FrameCount)
	assert.InDelta(t, 0.5, snap.LowVolumeRate, 0.15, "约一半帧应为低音量")
}

func TestNetworkQuality_连续低音量触发事件(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()
	cfg.LowVolumeDBFS = -55.0
	cfg.LowVolumeConsecutive = 5 // 连续 5 帧触发。
	cfg.ReportIntervalFrames = 0
	nq := NewNetworkQuality(cfg, testLogger())

	quietFrame := makeFrameWithAmplitude(10)
	now := time.Now()

	// 前 4 帧不应触发低音量事件。
	for i := range 4 {
		events := nq.RecordFrame(quietFrame, now.Add(time.Duration(i)*20*time.Millisecond))
		assert.NotContains(t, events, NetEventLowVolume, "未达到连续阈值不应触发")
	}

	// 第 5 帧应触发。
	events := nq.RecordFrame(quietFrame, now.Add(4*20*time.Millisecond))
	assert.Contains(t, events, NetEventLowVolume, "达到连续阈值应触发低音量事件")
}

func TestNetworkQuality_正常帧打断连续低音量计数(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()
	cfg.LowVolumeDBFS = -55.0
	cfg.LowVolumeConsecutive = 5
	cfg.ReportIntervalFrames = 0
	nq := NewNetworkQuality(cfg, testLogger())

	quietFrame := makeFrameWithAmplitude(10)
	normalFrame := makeFrameWithAmplitude(8000)
	now := time.Now()

	// 发 3 个安静帧。
	for i := range 3 {
		nq.RecordFrame(quietFrame, now.Add(time.Duration(i)*20*time.Millisecond))
	}
	// 插入 1 个正常帧，重置连续计数。
	nq.RecordFrame(normalFrame, now.Add(3*20*time.Millisecond))
	// 再发 4 个安静帧，不应触发（连续计数重置后只有 4 帧）。
	for i := range 4 {
		events := nq.RecordFrame(quietFrame, now.Add(time.Duration(4+i)*20*time.Millisecond))
		assert.NotContains(t, events, NetEventLowVolume)
	}

	// 第 5 个安静帧触发。
	events := nq.RecordFrame(quietFrame, now.Add(8*20*time.Millisecond))
	assert.Contains(t, events, NetEventLowVolume)
}

func TestNetworkQuality_丢帧率估算(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()
	cfg.FrameIntervalMs = 20.0
	cfg.GapThresholdMs = 500.0 // 提高间隙阈值，只测丢帧率。
	cfg.ReportIntervalFrames = 0
	nq := NewNetworkQuality(cfg, testLogger())

	frame := makeFrameWithAmplitude(8000)
	now := time.Now()

	// 每隔一帧延迟 40ms（是预期的 2 倍，超过 1.5x 阈值）。
	for i := range 20 {
		var interval time.Duration
		if i%2 == 0 {
			interval = 20 * time.Millisecond
		} else {
			interval = 40 * time.Millisecond
		}
		now = now.Add(interval)
		nq.RecordFrame(frame, now)
	}

	snap := nq.Snapshot()
	assert.Greater(t, snap.LossRate, 0.0, "应检测到丢帧")
}

func TestNetworkQuality_Snapshot_并发安全(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()
	cfg.ReportIntervalFrames = 0
	nq := NewNetworkQuality(cfg, testLogger())

	frame := makeFrameWithAmplitude(8000)
	done := make(chan struct{})

	go func() {
		now := time.Now()
		for i := range 500 {
			nq.RecordFrame(frame, now.Add(time.Duration(i)*20*time.Millisecond))
		}
		close(done)
	}()

	// 并发读取快照，不应 panic。
	for range 100 {
		_ = nq.Snapshot()
	}

	<-done
	snap := nq.Snapshot()
	assert.Equal(t, 500, snap.FrameCount)
}

func TestNetworkQuality_WindowSize为零时使用默认值(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()
	cfg.WindowSize = 0
	nq := NewNetworkQuality(cfg, testLogger())

	// 不应 panic。
	frame := makeFrameWithAmplitude(8000)
	now := time.Now()
	nq.RecordFrame(frame, now)
	nq.RecordFrame(frame, now.Add(20*time.Millisecond))

	snap := nq.Snapshot()
	assert.Equal(t, 2, snap.FrameCount)
}

func TestNetworkQuality_定期报告日志(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()
	cfg.ReportIntervalFrames = 5
	nq := NewNetworkQuality(cfg, testLogger())

	frame := makeFrameWithAmplitude(8000)
	now := time.Now()

	// 发送 10 帧，应触发 2 次日志（第 5 帧和第 10 帧），不应 panic。
	for i := range 10 {
		nq.RecordFrame(frame, now.Add(time.Duration(i)*20*time.Millisecond))
	}

	snap := nq.Snapshot()
	assert.Equal(t, 10, snap.FrameCount)
}

func TestDefaultNetworkQualityConfig_值合理(t *testing.T) {
	t.Parallel()
	cfg := DefaultNetworkQualityConfig()

	assert.Equal(t, 20.0, cfg.FrameIntervalMs)
	assert.Equal(t, 30.0, cfg.JitterThresholdMs)
	assert.Equal(t, 100.0, cfg.GapThresholdMs)
	assert.Equal(t, -55.0, cfg.LowVolumeDBFS)
	assert.Equal(t, 25, cfg.LowVolumeConsecutive)
	assert.Equal(t, 50, cfg.WindowSize)
	assert.Equal(t, 250, cfg.ReportIntervalFrames)
}
