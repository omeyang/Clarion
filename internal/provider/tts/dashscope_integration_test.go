//go:build integration

package tts

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/omeyang/clarion/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 集成测试：验证 DashScope TTS API 的真实连通性。
// 运行方式：CLARION_TTS_API_KEY=sk-xxx go test -tags=integration -run=Integration -v ./internal/provider/tts/

func getTestTTSKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("CLARION_TTS_API_KEY")
	if key == "" {
		t.Skip("跳过集成测试：未设置 CLARION_TTS_API_KEY")
	}
	return key
}

func TestIntegration_DashScope_Synthesize(t *testing.T) {
	apiKey := getTestTTSKey(t)

	d := NewDashScope(apiKey)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	audio, err := d.Synthesize(ctx, "你好，我是 Clarion 语音引擎。", provider.TTSConfig{
		Model:      "cosyvoice-v3-flash",
		Voice:      "longanyang",
		SampleRate: 16000,
	})
	require.NoError(t, err, "Synthesize 调用失败")
	assert.NotEmpty(t, audio, "音频数据不应为空")

	// 16kHz 16-bit PCM，1秒约 32000 字节，一句话至少应有 0.5 秒。
	assert.Greater(t, len(audio), 16000, "音频数据长度不合理（太短）")

	durationMs := len(audio) * 1000 / (16000 * 2) // 16kHz 16-bit = 32000 bytes/sec
	t.Logf("合成音频: %d 字节, 约 %d 毫秒", len(audio), durationMs)
}

func TestIntegration_DashScope_SynthesizeStream(t *testing.T) {
	apiKey := getTestTTSKey(t)

	d := NewDashScope(apiKey)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	textCh := make(chan string, 3)
	textCh <- "你好，"
	textCh <- "欢迎使用"
	textCh <- "语音服务。"
	close(textCh)

	audioCh, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{
		Model:      "cosyvoice-v3-flash",
		Voice:      "longanyang",
		SampleRate: 16000,
	})
	require.NoError(t, err, "SynthesizeStream 调用失败")

	var totalBytes int
	var chunkCount int
	for chunk := range audioCh {
		totalBytes += len(chunk)
		chunkCount++
	}

	assert.Greater(t, totalBytes, 0, "应收到音频数据")
	assert.Greater(t, chunkCount, 0, "应收到多个音频片段")

	durationMs := totalBytes * 1000 / (16000 * 2)
	t.Logf("流式合成: %d 字节, %d 个片段, 约 %d 毫秒", totalBytes, chunkCount, durationMs)
}

func TestIntegration_DashScope_Cancel(t *testing.T) {
	apiKey := getTestTTSKey(t)

	d := NewDashScope(apiKey)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 使用一个不会关闭的文本通道，模拟持续输入场景。
	textCh := make(chan string, 1)
	textCh <- "这是一段很长的文本，测试打断功能是否正常工作。"

	audioCh, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{
		Model:      "cosyvoice-v3-flash",
		Voice:      "longanyang",
		SampleRate: 16000,
	})
	require.NoError(t, err, "SynthesizeStream 调用失败")

	// 等待收到第一个音频片段后执行打断。
	select {
	case chunk, ok := <-audioCh:
		if ok {
			assert.NotEmpty(t, chunk, "首个音频片段不应为空")
			t.Logf("收到首个音频片段: %d 字节，执行打断", len(chunk))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("等待首个音频片段超时")
	}

	// 执行打断。
	err = d.Cancel()
	require.NoError(t, err, "Cancel 调用失败")

	// 验证音频通道最终关闭。
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-audioCh:
			if !ok {
				t.Log("打断成功，音频通道已关闭")
				return
			}
		case <-timeout:
			t.Fatal("打断后音频通道未关闭")
		}
	}
}
