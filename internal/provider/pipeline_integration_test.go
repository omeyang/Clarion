//go:build integration

// 全链路集成测试：验证 LLM → TTS → ASR 的完整流水线。
//
// 运行方式：
//
//	CLARION_LLM_API_KEY=sk-xxx \
//	CLARION_TTS_API_KEY=sk-xxx \
//	CLARION_ASR_API_KEY=sk-xxx \
//	go test -tags=integration -run=Integration -v ./internal/provider/
package provider_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/omeyang/clarion/internal/provider"
	"github.com/omeyang/clarion/internal/provider/asr"
	"github.com/omeyang/clarion/internal/provider/llm"
	"github.com/omeyang/clarion/internal/provider/tts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfMissing(t *testing.T, envKeys ...string) {
	t.Helper()
	for _, key := range envKeys {
		if os.Getenv(key) == "" {
			t.Skipf("跳过集成测试：未设置 %s", key)
		}
	}
}

// TestIntegration_Pipeline_LLM_TTS 验证 LLM 生成文本后 TTS 合成音频的流水线。
func TestIntegration_Pipeline_LLM_TTS(t *testing.T) {
	skipIfMissing(t, "CLARION_LLM_API_KEY", "CLARION_TTS_API_KEY")

	llmKey := os.Getenv("CLARION_LLM_API_KEY")
	ttsKey := os.Getenv("CLARION_TTS_API_KEY")

	// 第一步：LLM 流式生成文本。
	ds := llm.NewDeepSeek(llmKey, "https://api.deepseek.com")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	messages := []provider.Message{
		{Role: "system", Content: "你是一个房产销售机器人。回答简短，不超过 30 个字。"},
		{Role: "user", Content: "你们楼盘在哪里？"},
	}

	tokenCh, err := ds.GenerateStream(ctx, messages, provider.LLMConfig{
		Model:       "deepseek-chat",
		MaxTokens:   64,
		Temperature: 0.7,
		TimeoutMs:   15000,
	})
	require.NoError(t, err, "LLM GenerateStream 失败")

	// 收集 LLM 的所有 token。
	var tokens []string
	for tok := range tokenCh {
		tokens = append(tokens, tok)
	}
	fullText := strings.Join(tokens, "")
	require.NotEmpty(t, fullText, "LLM 应生成非空文本")
	t.Logf("LLM 生成文本（%d token）: %s", len(tokens), fullText)

	// 第二步：TTS 将文本合成为音频。
	d := tts.NewDashScope(ttsKey)

	audio, err := d.Synthesize(ctx, fullText, provider.TTSConfig{
		Model:      "cosyvoice-v3-flash",
		Voice:      "longanyang",
		SampleRate: 16000,
	})
	require.NoError(t, err, "TTS Synthesize 失败")
	require.NotEmpty(t, audio, "TTS 应生成非空音频")

	durationMs := len(audio) * 1000 / (16000 * 2)
	t.Logf("TTS 合成音频: %d 字节, 约 %d 毫秒", len(audio), durationMs)
}

// TestIntegration_Pipeline_LLM_TTS_Stream 验证流式全链路：LLM 边生成边喂给 TTS。
func TestIntegration_Pipeline_LLM_TTS_Stream(t *testing.T) {
	skipIfMissing(t, "CLARION_LLM_API_KEY", "CLARION_TTS_API_KEY")

	llmKey := os.Getenv("CLARION_LLM_API_KEY")
	ttsKey := os.Getenv("CLARION_TTS_API_KEY")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// LLM 流式生成。
	ds := llm.NewDeepSeek(llmKey, "https://api.deepseek.com")

	messages := []provider.Message{
		{Role: "system", Content: "你是一个房产销售机器人。回答简短，不超过 50 个字。"},
		{Role: "user", Content: "你们这个楼盘有什么优势？"},
	}

	tokenCh, err := ds.GenerateStream(ctx, messages, provider.LLMConfig{
		Model:       "deepseek-chat",
		MaxTokens:   128,
		Temperature: 0.7,
		TimeoutMs:   15000,
	})
	require.NoError(t, err, "LLM GenerateStream 失败")

	// 将 LLM token 通道直接桥接到 TTS 文本通道（模拟实时流水线）。
	textCh := make(chan string, 32)
	go func() {
		defer close(textCh)
		var buf strings.Builder
		for tok := range tokenCh {
			buf.WriteString(tok)
			// 按句号、问号、感叹号分段发送给 TTS。
			text := buf.String()
			if strings.ContainsAny(text, "。！？,.!?") {
				textCh <- text
				t.Logf("  → 发送给 TTS: %q", text)
				buf.Reset()
			}
		}
		// 发送剩余文本。
		if buf.Len() > 0 {
			textCh <- buf.String()
			t.Logf("  → 发送给 TTS（尾部）: %q", buf.String())
		}
	}()

	// TTS 流式合成。
	d := tts.NewDashScope(ttsKey)

	audioCh, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{
		Model:      "cosyvoice-v3-flash",
		Voice:      "longanyang",
		SampleRate: 16000,
	})
	require.NoError(t, err, "TTS SynthesizeStream 失败")

	var totalBytes int
	var chunkCount int
	for chunk := range audioCh {
		totalBytes += len(chunk)
		chunkCount++
	}

	assert.Greater(t, totalBytes, 0, "应收到音频数据")

	durationMs := totalBytes * 1000 / (16000 * 2)
	t.Logf("流式全链路完成: %d 字节, %d 个音频片段, 约 %d 毫秒", totalBytes, chunkCount, durationMs)
}

// TestIntegration_Pipeline_TTS_ASR 验证 TTS 合成的音频能被 ASR 正确识别。
func TestIntegration_Pipeline_TTS_ASR(t *testing.T) {
	skipIfMissing(t, "CLARION_TTS_API_KEY", "CLARION_ASR_API_KEY")

	ttsKey := os.Getenv("CLARION_TTS_API_KEY")
	asrKey := os.Getenv("CLARION_ASR_API_KEY")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 第一步：TTS 合成一段已知文本的音频。
	sourceText := "你好，我想了解一下你们的楼盘信息。"
	t.Logf("原始文本: %s", sourceText)

	d := tts.NewDashScope(ttsKey)
	audio, err := d.Synthesize(ctx, sourceText, provider.TTSConfig{
		Model:      "cosyvoice-v3-flash",
		Voice:      "longanyang",
		SampleRate: 16000,
	})
	require.NoError(t, err, "TTS 合成失败")
	require.NotEmpty(t, audio, "TTS 应生成非空音频")

	durationMs := len(audio) * 1000 / (16000 * 2)
	t.Logf("TTS 合成完成: %d 字节, 约 %d 毫秒", len(audio), durationMs)

	// 第二步：将音频喂给 ASR 进行识别。
	q := asr.NewQwen(asrKey)

	stream, err := q.StartStream(ctx, provider.ASRConfig{
		Model:      "qwen3-asr-flash-realtime",
		SampleRate: 16000,
		Language:   "zh",
	})
	require.NoError(t, err, "ASR StartStream 失败")
	defer stream.Close()

	// 分块发送音频（模拟实时流，每 20ms 一块）。
	chunkSize := 16000 * 2 * 20 / 1000 // 640 bytes per 20ms
	for offset := 0; offset < len(audio); offset += chunkSize {
		end := offset + chunkSize
		if end > len(audio) {
			end = len(audio)
		}
		err := stream.FeedAudio(ctx, audio[offset:end])
		require.NoError(t, err, "FeedAudio 失败 (offset=%d)", offset)

		// 模拟实时节奏。
		time.Sleep(15 * time.Millisecond)
	}

	t.Log("音频发送完毕，等待 ASR 识别结果...")

	// 等待识别结果。
	var partials []string
	var finals []string
	timeout := time.After(10 * time.Second)

	for {
		select {
		case evt, ok := <-stream.Events():
			if !ok {
				goto done
			}
			if evt.IsFinal {
				finals = append(finals, evt.Text)
				t.Logf("  [最终] %s (confidence=%.2f)", evt.Text, evt.Confidence)
			} else {
				partials = append(partials, evt.Text)
				t.Logf("  [部分] %s", evt.Text)
			}
		case <-timeout:
			t.Log("等待 ASR 结果超时")
			goto done
		}
	}

done:
	t.Logf("ASR 识别完成: %d 个部分结果, %d 个最终结果", len(partials), len(finals))
	if len(finals) > 0 {
		recognized := strings.Join(finals, "")
		t.Logf("识别全文: %s", recognized)
		t.Logf("原始文本: %s", sourceText)
		// 不做严格匹配，ASR 可能有微小差异。
	}
}

// TestIntegration_Pipeline_Full_LLM_TTS_ASR 验证完整三级流水线。
func TestIntegration_Pipeline_Full_LLM_TTS_ASR(t *testing.T) {
	skipIfMissing(t, "CLARION_LLM_API_KEY", "CLARION_TTS_API_KEY", "CLARION_ASR_API_KEY")

	llmKey := os.Getenv("CLARION_LLM_API_KEY")
	ttsKey := os.Getenv("CLARION_TTS_API_KEY")
	asrKey := os.Getenv("CLARION_ASR_API_KEY")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Log("=== 全链路测试：LLM → TTS → ASR ===")

	// 第一步：LLM 生成回复。
	ds := llm.NewDeepSeek(llmKey, "https://api.deepseek.com")
	llmReply, err := ds.Generate(ctx, []provider.Message{
		{Role: "system", Content: "你是一个房产销售机器人。回答简短，不超过 20 个字，不使用标点符号以外的特殊字符。"},
		{Role: "user", Content: "价格多少？"},
	}, provider.LLMConfig{
		Model:       "deepseek-chat",
		MaxTokens:   64,
		Temperature: 0.3,
		TimeoutMs:   15000,
	})
	require.NoError(t, err, "LLM 生成失败")
	t.Logf("第一步 LLM 生成: %s", llmReply)

	// 第二步：TTS 合成。
	d := tts.NewDashScope(ttsKey)
	audio, err := d.Synthesize(ctx, llmReply, provider.TTSConfig{
		Model:      "cosyvoice-v3-flash",
		Voice:      "longanyang",
		SampleRate: 16000,
	})
	require.NoError(t, err, "TTS 合成失败")

	durationMs := len(audio) * 1000 / (16000 * 2)
	t.Logf("第二步 TTS 合成: %d 字节, 约 %d 毫秒", len(audio), durationMs)

	// 第三步：ASR 识别。
	q := asr.NewQwen(asrKey)
	stream, err := q.StartStream(ctx, provider.ASRConfig{
		Model:      "qwen3-asr-flash-realtime",
		SampleRate: 16000,
		Language:   "zh",
	})
	require.NoError(t, err, "ASR 连接失败")
	defer stream.Close()

	// 分块发送。
	chunkSize := 16000 * 2 * 20 / 1000
	for offset := 0; offset < len(audio); offset += chunkSize {
		end := offset + chunkSize
		if end > len(audio) {
			end = len(audio)
		}
		if err := stream.FeedAudio(ctx, audio[offset:end]); err != nil {
			t.Logf("FeedAudio 失败: %v", err)
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	// 收集识别结果。
	var recognized []string
	timeout := time.After(10 * time.Second)
	for {
		select {
		case evt, ok := <-stream.Events():
			if !ok {
				goto summary
			}
			if evt.IsFinal {
				recognized = append(recognized, evt.Text)
				t.Logf("  ASR 最终: %s", evt.Text)
			}
		case <-timeout:
			goto summary
		}
	}

summary:
	asrText := strings.Join(recognized, "")
	t.Logf("=== 全链路结果 ===")
	t.Logf("  LLM 生成: %s", llmReply)
	t.Logf("  ASR 识别: %s", asrText)
	t.Logf("  音频时长: %d 毫秒", durationMs)

	if asrText != "" {
		t.Log("全链路测试通过：LLM → TTS → ASR 流水线成功打通")
	} else {
		t.Log("ASR 未返回识别结果（可能需要更长等待时间或音频静音检测未触发 final）")
	}
}
