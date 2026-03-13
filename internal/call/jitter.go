package call

import (
	"sync"
)

// JitterBufferConfig 抖动缓冲配置。
type JitterBufferConfig struct {
	// Capacity 缓冲区最大帧数。超出后丢弃最旧帧。
	Capacity int
	// Threshold 开始消费所需的最小预缓冲帧数。
	// 例如 3 帧 = 60ms（20ms/帧），吸收网络抖动。
	Threshold int
}

// DefaultJitterBufferConfig 返回默认的抖动缓冲配置。
// 容量 20 帧（400ms），阈值 3 帧（60ms）。
func DefaultJitterBufferConfig() JitterBufferConfig {
	return JitterBufferConfig{
		Capacity:  20,
		Threshold: 3,
	}
}

// JitterBuffer 自适应抖动缓冲，用于 TTS 播放端预缓冲。
// 写入帧到缓冲区后，需累积到阈值帧数才开始允许读取，
// 吸收网络抖动导致的帧到达不均匀。
// 线程安全：所有方法可在任意 goroutine 中调用。
type JitterBuffer struct {
	mu        sync.Mutex
	buf       [][]byte
	capacity  int
	threshold int
	started   bool // 是否已达到阈值开始消费。
}

// NewJitterBuffer 创建抖动缓冲。
// 若 capacity <= 0 或 threshold <= 0，使用默认值。
// threshold 不会超过 capacity。
func NewJitterBuffer(cfg JitterBufferConfig) *JitterBuffer {
	if cfg.Capacity <= 0 {
		cfg.Capacity = DefaultJitterBufferConfig().Capacity
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultJitterBufferConfig().Threshold
	}
	if cfg.Threshold > cfg.Capacity {
		cfg.Threshold = cfg.Capacity
	}
	return &JitterBuffer{
		buf:       make([][]byte, 0, cfg.Capacity),
		capacity:  cfg.Capacity,
		threshold: cfg.Threshold,
	}
}

// Push 写入一帧音频到缓冲区。
// 若缓冲区已满，丢弃最旧帧为新帧腾出空间。
// 返回 true 表示缓冲区未满正常写入，false 表示发生了溢出丢弃。
func (jb *JitterBuffer) Push(frame []byte) bool {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	overflow := false
	if len(jb.buf) >= jb.capacity {
		// 丢弃最旧帧。
		jb.buf = jb.buf[1:]
		overflow = true
	}
	jb.buf = append(jb.buf, frame)

	// 检查是否达到阈值。
	if !jb.started && len(jb.buf) >= jb.threshold {
		jb.started = true
	}

	return !overflow
}

// Pop 从缓冲区取出最旧帧。
// 若尚未达到预缓冲阈值或缓冲区为空，返回 nil, false。
func (jb *JitterBuffer) Pop() ([]byte, bool) {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if !jb.started || len(jb.buf) == 0 {
		return nil, false
	}

	frame := jb.buf[0]
	jb.buf = jb.buf[1:]
	return frame, true
}

// Len 返回当前缓冲帧数。
func (jb *JitterBuffer) Len() int {
	jb.mu.Lock()
	defer jb.mu.Unlock()
	return len(jb.buf)
}

// Ready 返回缓冲是否已达到预缓冲阈值，可以开始消费。
func (jb *JitterBuffer) Ready() bool {
	jb.mu.Lock()
	defer jb.mu.Unlock()
	return jb.started
}

// Reset 重置缓冲区状态，清空所有帧并重置预缓冲标记。
func (jb *JitterBuffer) Reset() {
	jb.mu.Lock()
	defer jb.mu.Unlock()
	jb.buf = jb.buf[:0]
	jb.started = false
}
