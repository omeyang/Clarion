package call

import (
	"testing"
)

// BenchmarkJitterBufferPushPop 基准测试 JitterBuffer 的 Push/Pop 循环，使用 320 字节帧。
func BenchmarkJitterBufferPushPop(b *testing.B) {
	jb := NewJitterBuffer(JitterBufferConfig{Capacity: 20, Threshold: 3})

	// 320 字节 PCM 帧（8kHz 20ms）。
	frame := make([]byte, 320)
	for i := range frame {
		frame[i] = byte(i % 256)
	}

	// 预热到阈值。
	for range 3 {
		jb.Push(frame)
	}

	b.ResetTimer()
	for range b.N {
		jb.Push(frame)
		jb.Pop()
	}
}

// BenchmarkJitterBufferOverflow 基准测试溢出场景：Push 超出容量后 Pop。
func BenchmarkJitterBufferOverflow(b *testing.B) {
	frame := make([]byte, 320)
	for i := range frame {
		frame[i] = byte(i % 256)
	}

	b.ResetTimer()
	for range b.N {
		jb := NewJitterBuffer(JitterBufferConfig{Capacity: 10, Threshold: 1})
		// 推入超出容量的帧数。
		for range 15 {
			jb.Push(frame)
		}
		// 全部弹出。
		for {
			if _, ok := jb.Pop(); !ok {
				break
			}
		}
	}
}
