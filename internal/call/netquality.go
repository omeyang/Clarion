package call

import (
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/omeyang/Sonata/engine/pcm"
)

// 网络质量事件类型。
const (
	NetEventPoorNetwork = "poor_network_detected"
	NetEventAudioGap    = "audio_gap_detected"
	NetEventLowVolume   = "low_volume_detected"
)

// NetworkQualityConfig 网络质量监控配置。
type NetworkQualityConfig struct {
	// FrameIntervalMs 预期帧间隔（毫秒），8kHz 20ms 帧 = 20。
	FrameIntervalMs float64
	// JitterThresholdMs 抖动告警阈值（毫秒），超过此值认为抖动异常。
	JitterThresholdMs float64
	// GapThresholdMs 音频间隙检测阈值（毫秒），帧间隔超过此值认为出现间隙。
	GapThresholdMs float64
	// LowVolumeDBFS 低音量阈值（dBFS），低于此值认为音量过低。
	LowVolumeDBFS float64
	// LowVolumeConsecutive 连续低音量帧数阈值，超过此值触发低音量事件。
	LowVolumeConsecutive int
	// WindowSize 滑动窗口大小，用于计算移动平均。
	WindowSize int
	// ReportIntervalFrames 报告间隔（每 N 帧输出一次摘要日志）。
	ReportIntervalFrames int
}

// DefaultNetworkQualityConfig 返回默认的网络质量监控配置。
func DefaultNetworkQualityConfig() NetworkQualityConfig {
	return NetworkQualityConfig{
		FrameIntervalMs:      20.0,
		JitterThresholdMs:    30.0,
		GapThresholdMs:       100.0,
		LowVolumeDBFS:        -55.0,
		LowVolumeConsecutive: 25, // 连续 25 帧（约 500ms）低音量触发事件。
		WindowSize:           50,
		ReportIntervalFrames: 250, // 约 5 秒汇总一次
	}
}

// NetworkQualitySnapshot 是某一时刻的网络质量快照（只读）。
type NetworkQualitySnapshot struct {
	JitterAvgMs   float64 `json:"jitter_avg_ms"`   // 平均帧间隔抖动（毫秒）。
	LossRate      float64 `json:"loss_rate"`       // 丢帧率估算（0~1）。
	GapCount      int     `json:"gap_count"`       // 累计音频间隙次数。
	LowVolumeRate float64 `json:"low_volume_rate"` // 低音量帧占比（0~1）。
	FrameCount    int     `json:"frame_count"`     // 已处理帧总数。
}

// NetworkQuality 采集音频帧到达的网络质量指标。
// 线程安全：所有方法均可在任意 goroutine 中调用。
type NetworkQuality struct {
	cfg    NetworkQualityConfig
	logger *slog.Logger

	mu                   sync.Mutex
	lastArrival          time.Time
	jitterWindow         []float64 // 最近 N 帧的抖动值（绝对偏差）。
	jitterIdx            int       // 环形缓冲区写入位置。
	jitterFull           bool      // 缓冲区是否已填满。
	frameCount           int       // 已处理帧总数。
	gapCount             int       // 累计音频间隙次数。
	lateFrames           int       // 到达间隔超出预期的帧数（用于估算丢帧率）。
	lowVolumeFrames      int       // 低音量帧计数。
	lowVolumeConsecutive int       // 连续低音量帧计数，用于触发事件。
}

// NewNetworkQuality 创建网络质量监控器。
func NewNetworkQuality(cfg NetworkQualityConfig, logger *slog.Logger) *NetworkQuality {
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = 50
	}
	if cfg.LowVolumeConsecutive <= 0 {
		cfg.LowVolumeConsecutive = 25
	}
	return &NetworkQuality{
		cfg:          cfg,
		logger:       logger,
		jitterWindow: make([]float64, cfg.WindowSize),
	}
}

// RecordFrame 记录一帧音频到达，更新网络质量指标。
// frame 为 PCM16 LE 单声道原始数据，now 为帧到达时刻。
// 返回本帧触发的事件列表（可能为空）。
func (nq *NetworkQuality) RecordFrame(frame []byte, now time.Time) []string {
	nq.mu.Lock()
	defer nq.mu.Unlock()

	nq.frameCount++
	var events []string

	// 计算帧间隔抖动。
	if !nq.lastArrival.IsZero() {
		intervalMs := float64(now.Sub(nq.lastArrival).Microseconds()) / 1000.0
		jitter := math.Abs(intervalMs - nq.cfg.FrameIntervalMs)

		// 写入环形缓冲区。
		nq.jitterWindow[nq.jitterIdx] = jitter
		nq.jitterIdx = (nq.jitterIdx + 1) % nq.cfg.WindowSize
		if nq.jitterIdx == 0 {
			nq.jitterFull = true
		}

		// 帧间隔过大 → 可能丢帧。
		if intervalMs > nq.cfg.FrameIntervalMs*1.5 {
			nq.lateFrames++
		}

		// 音频间隙检测。
		if intervalMs > nq.cfg.GapThresholdMs {
			nq.gapCount++
			events = append(events, NetEventAudioGap)
		}
	}
	nq.lastArrival = now

	// 低音量检测：连续低音量帧超过阈值时触发事件。
	if len(frame) > 0 && nq.checkLowVolume(frame) {
		events = append(events, NetEventLowVolume)
	}

	// 窗口已满时检查抖动告警。
	if nq.jitterFull {
		avg := nq.jitterAvgLocked()
		if avg > nq.cfg.JitterThresholdMs {
			events = append(events, NetEventPoorNetwork)
		}
	}

	// 定期输出摘要日志。
	if nq.cfg.ReportIntervalFrames > 0 && nq.frameCount%nq.cfg.ReportIntervalFrames == 0 {
		nq.logSummary()
	}

	return events
}

// Snapshot 返回当前网络质量快照。
func (nq *NetworkQuality) Snapshot() NetworkQualitySnapshot {
	nq.mu.Lock()
	defer nq.mu.Unlock()

	snap := NetworkQualitySnapshot{
		FrameCount: nq.frameCount,
		GapCount:   nq.gapCount,
	}

	if nq.jitterFull || nq.jitterIdx > 0 {
		snap.JitterAvgMs = nq.jitterAvgLocked()
	}

	if nq.frameCount > 1 {
		// 丢帧率 = 延迟到达帧数 / 总帧数。
		snap.LossRate = float64(nq.lateFrames) / float64(nq.frameCount)
	}

	if nq.frameCount > 0 {
		snap.LowVolumeRate = float64(nq.lowVolumeFrames) / float64(nq.frameCount)
	}

	return snap
}

// jitterAvgLocked 计算滑动窗口内的平均抖动（需持锁调用）。
func (nq *NetworkQuality) jitterAvgLocked() float64 {
	n := nq.jitterIdx
	if nq.jitterFull {
		n = nq.cfg.WindowSize
	}
	if n == 0 {
		return 0
	}
	var sum float64
	for i := range n {
		sum += nq.jitterWindow[i]
	}
	return sum / float64(n)
}

// checkLowVolume 检测低音量并更新连续计数（需持锁调用）。
// 连续低音量帧达到阈值时返回 true，表示应触发低音量事件。
func (nq *NetworkQuality) checkLowVolume(frame []byte) bool {
	energy := pcm.EnergyDBFS(frame)
	if energy >= nq.cfg.LowVolumeDBFS {
		nq.lowVolumeConsecutive = 0
		return false
	}
	nq.lowVolumeFrames++
	nq.lowVolumeConsecutive++
	return nq.lowVolumeConsecutive == nq.cfg.LowVolumeConsecutive
}

// logSummary 输出质量摘要日志（需持锁调用）。
func (nq *NetworkQuality) logSummary() {
	snap := NetworkQualitySnapshot{
		FrameCount: nq.frameCount,
		GapCount:   nq.gapCount,
	}
	if nq.jitterFull || nq.jitterIdx > 0 {
		snap.JitterAvgMs = nq.jitterAvgLocked()
	}
	if nq.frameCount > 1 {
		snap.LossRate = float64(nq.lateFrames) / float64(nq.frameCount)
	}
	if nq.frameCount > 0 {
		snap.LowVolumeRate = float64(nq.lowVolumeFrames) / float64(nq.frameCount)
	}

	nq.logger.Info("网络质量摘要",
		slog.Int("frames", snap.FrameCount),
		slog.Float64("jitter_avg_ms", math.Round(snap.JitterAvgMs*100)/100),
		slog.Float64("loss_rate", math.Round(snap.LossRate*1000)/1000),
		slog.Int("gaps", snap.GapCount),
		slog.Float64("low_volume_rate", math.Round(snap.LowVolumeRate*1000)/1000),
	)
}
