package call

import (
	"testing"

	"github.com/omeyang/Sonata/engine/pcm"
)

// BenchmarkIsSpeechFrame_Energy 基准测试基于能量的语音检测。
// Session.isSpeechFrame 在无 VAD 注入时退回到 pcm.EnergyDBFS，
// 此处直接测试底层函数以避免构造完整 Session 的开销。
func BenchmarkIsSpeechFrame_Energy(b *testing.B) {
	// 320 字节 PCM 帧（8kHz 20ms，160 采样）。
	frame := makeFrameWithAmplitude(8000)
	threshold := -35.0

	b.ResetTimer()
	for range b.N {
		_ = pcm.EnergyDBFS(frame) > threshold
	}
}
