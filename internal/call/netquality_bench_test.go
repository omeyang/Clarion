package call

import (
	"testing"
	"time"
)

// BenchmarkRecordFrame 基准测试 NetworkQuality.RecordFrame，使用 320 字节 PCM 帧。
func BenchmarkRecordFrame(b *testing.B) {
	cfg := DefaultNetworkQualityConfig()
	cfg.ReportIntervalFrames = 0 // 关闭日志输出避免干扰。
	nq := NewNetworkQuality(cfg, testLogger())

	// 320 字节 = 160 采样 × 2 字节/采样，对应 8kHz 下 20ms 帧。
	frame := makeFrameWithAmplitude(8000)
	now := time.Now()

	b.ResetTimer()
	for i := range b.N {
		nq.RecordFrame(frame, now.Add(time.Duration(i)*20*time.Millisecond))
	}
}

// BenchmarkSnapshot 基准测试 NetworkQuality.Snapshot，预先写入 100 帧预热。
func BenchmarkSnapshot(b *testing.B) {
	cfg := DefaultNetworkQualityConfig()
	cfg.ReportIntervalFrames = 0
	nq := NewNetworkQuality(cfg, testLogger())

	frame := makeFrameWithAmplitude(8000)
	now := time.Now()
	for i := range 100 {
		nq.RecordFrame(frame, now.Add(time.Duration(i)*20*time.Millisecond))
	}

	b.ResetTimer()
	for range b.N {
		_ = nq.Snapshot()
	}
}
