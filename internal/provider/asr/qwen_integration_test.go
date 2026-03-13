//go:build integration

package asr

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"testing"
	"time"

	"github.com/omeyang/clarion/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 集成测试：验证 Qwen ASR API 的真实连通性。
// 运行方式：CLARION_ASR_API_KEY=sk-xxx go test -tags=integration -run=Integration -v ./internal/provider/asr/

func getTestASRKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("CLARION_ASR_API_KEY")
	if key == "" {
		t.Skip("跳过集成测试：未设置 CLARION_ASR_API_KEY")
	}
	return key
}

// generateSineWavePCM 生成指定频率和时长的 16kHz 16-bit PCM 正弦波。
// 用于验证 ASR 连接和音频传输通路，不期望产生有意义的识别结果。
func generateSineWavePCM(freqHz float64, durationMs int, sampleRate int) []byte {
	numSamples := sampleRate * durationMs / 1000
	buf := make([]byte, numSamples*2) // 16-bit = 2 bytes per sample
	for i := 0; i < numSamples; i++ {
		sample := int16(math.Sin(2*math.Pi*freqHz*float64(i)/float64(sampleRate)) * 16000)
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(sample))
	}
	return buf
}

func TestIntegration_Qwen_StartStreamAndConnect(t *testing.T) {
	apiKey := getTestASRKey(t)

	q := NewQwen(apiKey)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	stream, err := q.StartStream(ctx, provider.ASRConfig{
		Model:      "qwen3-asr-flash-realtime",
		SampleRate: 16000,
		Language:   "zh",
	})
	require.NoError(t, err, "StartStream 连接失败")
	defer stream.Close()

	t.Log("ASR WebSocket 连接成功，session.update 已发送")

	// 发送一段正弦波音频，验证 FeedAudio 通路。
	audio := generateSineWavePCM(440, 500, 16000) // 440Hz, 500ms
	err = stream.FeedAudio(ctx, audio)
	require.NoError(t, err, "FeedAudio 发送失败")

	t.Logf("已发送 %d 字节音频数据", len(audio))

	// 等待一小段时间，确认连接稳定（不会立即断开）。
	time.Sleep(500 * time.Millisecond)

	// 正弦波不是语音，可能不会产生事件，但连接应保持稳定。
	err = stream.Close()
	assert.NoError(t, err, "Close 应正常关闭")

	t.Log("ASR 连接已正常关闭")
}

func TestIntegration_Qwen_FeedAudioChunks(t *testing.T) {
	apiKey := getTestASRKey(t)

	q := NewQwen(apiKey)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	stream, err := q.StartStream(ctx, provider.ASRConfig{
		Model:      "qwen3-asr-flash-realtime",
		SampleRate: 16000,
		Language:   "zh",
	})
	require.NoError(t, err, "StartStream 连接失败")
	defer stream.Close()

	// 模拟真实场景：分块发送音频（每 20ms 一块，标准 RTP 包大小）。
	chunkDurationMs := 20
	chunkSize := 16000 * 2 * chunkDurationMs / 1000 // 640 bytes per 20ms at 16kHz 16-bit
	totalDurationMs := 2000                          // 发送 2 秒音频

	// 生成 2 秒 440Hz 正弦波。
	fullAudio := generateSineWavePCM(440, totalDurationMs, 16000)

	sentChunks := 0
	for offset := 0; offset+chunkSize <= len(fullAudio); offset += chunkSize {
		chunk := fullAudio[offset : offset+chunkSize]
		err := stream.FeedAudio(ctx, chunk)
		require.NoError(t, err, "FeedAudio 第 %d 块失败", sentChunks)
		sentChunks++

		// 模拟实时发送节奏。
		time.Sleep(time.Duration(chunkDurationMs) * time.Millisecond)
	}

	t.Logf("已分块发送 %d 个音频块（每块 %d 字节，共 %d 毫秒）",
		sentChunks, chunkSize, totalDurationMs)

	// 等待可能的 ASR 事件。
	timeout := time.After(3 * time.Second)
	eventCount := 0
	for {
		select {
		case evt, ok := <-stream.Events():
			if !ok {
				t.Logf("事件通道已关闭，共收到 %d 个事件", eventCount)
				return
			}
			eventCount++
			t.Logf("收到 ASR 事件: text=%q, isFinal=%v, confidence=%.2f",
				evt.Text, evt.IsFinal, evt.Confidence)
		case <-timeout:
			t.Logf("等待超时，共收到 %d 个 ASR 事件（正弦波非语音，无事件属正常）", eventCount)
			return
		}
	}
}
