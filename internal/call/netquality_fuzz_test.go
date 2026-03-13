package call

import (
	"testing"
	"time"
)

// FuzzRecordFrame 模糊测试 RecordFrame，确保任意帧数据和时间间隔不会 panic。
func FuzzRecordFrame(f *testing.F) {
	// 种子语料：帧数据 + 间隔毫秒数。
	f.Add([]byte{0x00, 0x80, 0x00, 0x80}, int64(20))
	f.Add(make([]byte, 320), int64(20))
	f.Add([]byte{}, int64(0))
	f.Add([]byte{0xFF}, int64(1000))
	f.Add(make([]byte, 1), int64(-5))

	f.Fuzz(func(t *testing.T, frame []byte, intervalMs int64) {
		cfg := DefaultNetworkQualityConfig()
		cfg.ReportIntervalFrames = 0
		nq := NewNetworkQuality(cfg, testLogger())

		now := time.Now()

		// 发送两帧以覆盖间隔计算路径。
		nq.RecordFrame(frame, now)
		nq.RecordFrame(frame, now.Add(time.Duration(intervalMs)*time.Millisecond))

		// Snapshot 也不应 panic。
		_ = nq.Snapshot()
	})
}
