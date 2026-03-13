// Command audiotest 执行端到端语音管道测试。
//
// 两种模式：
//
//  1. gen-audio: 使用 TTS 生成测试音频文件
//     go run ./cmd/audiotest -c deploy/local/clarion-local.toml gen-audio -text "喂，你好" -o test.wav
//
//  2. pipeline: 音频文件 → ASR → LLM → TTS → 输出音频（完整管道测试）
//     go run ./cmd/audiotest -c deploy/local/clarion-local.toml pipeline -i test.wav -o reply.wav
//
// 此工具不需要 FreeSWITCH，直接测试 AI 管道的质量与延迟。
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/provider"
	"github.com/omeyang/clarion/internal/provider/asr"
	"github.com/omeyang/clarion/internal/provider/llm"
	"github.com/omeyang/clarion/internal/provider/tts"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	app := &cli.Command{
		Name:  "audiotest",
		Usage: "AI 语音管道测试工具",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Value:   "clarion.toml",
				Usage:   "配置文件路径",
			},
		},
		Commands: []*cli.Command{
			genAudioCmd(logger),
			pipelineCmd(logger),
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "audiotest: %v\n", err)
		os.Exit(1)
	}
}

// genAudioCmd 使用 TTS 合成测试音频文件。
func genAudioCmd(logger *slog.Logger) *cli.Command {
	return &cli.Command{
		Name:  "gen-audio",
		Usage: "使用 TTS 生成测试 WAV 文件",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "text",
				Aliases: []string{"t"},
				Value:   "喂，你好，我是张先生，请问有什么事吗？",
				Usage:   "要合成的文本",
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "deploy/local/test-audio/user-speech.wav",
				Usage:   "输出 WAV 文件路径",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := config.Load(cmd.Root().String("config"))
			if err != nil {
				return fmt.Errorf("加载配置: %w", err)
			}

			text := cmd.String("text")
			output := cmd.String("output")

			logger.Info("开始 TTS 合成", slog.String("text", text), slog.String("output", output))

			ttsProv := tts.NewDashScope(cfg.TTS.APIKey, tts.WithDashScopeLogger(logger))
			ttsCfg := provider.TTSConfig{
				Model:      cfg.TTS.Model,
				Voice:      cfg.TTS.Voice,
				SampleRate: cfg.TTS.SampleRate,
			}

			start := time.Now()
			audioData, err := ttsProv.Synthesize(ctx, text, ttsCfg)
			if err != nil {
				return fmt.Errorf("TTS 合成失败: %w", err)
			}

			elapsed := time.Since(start)
			logger.Info("TTS 合成完成",
				slog.Duration("耗时", elapsed),
				slog.Int("音频字节数", len(audioData)),
			)

			// 写入 WAV 文件。
			if err := writeWAV(output, audioData, cfg.TTS.SampleRate); err != nil {
				return fmt.Errorf("写入 WAV: %w", err)
			}

			durationMs := len(audioData) / 2 * 1000 / cfg.TTS.SampleRate
			fmt.Printf("生成完成: %s (%d ms 音频, TTS 耗时 %v)\n", output, durationMs, elapsed.Round(time.Millisecond))
			return nil
		},
	}
}

// pipelineCmd 执行完整的 ASR → LLM → TTS 管道测试。
func pipelineCmd(logger *slog.Logger) *cli.Command {
	return &cli.Command{
		Name:  "pipeline",
		Usage: "端到端管道测试: WAV → ASR → LLM → TTS → WAV",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "input",
				Aliases: []string{"i"},
				Value:   "deploy/local/test-audio/user-speech.wav",
				Usage:   "输入 WAV 文件（模拟用户语音）",
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "deploy/local/test-audio/bot-reply.wav",
				Usage:   "输出 WAV 文件（AI 回复语音）",
			},
			&cli.StringFlag{
				Name:  "system-prompt",
				Value: "你是一个友好的 AI 外呼助手，正在进行房产推荐电话。请用简短的中文回复。",
				Usage: "LLM 系统提示词",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := config.Load(cmd.Root().String("config"))
			if err != nil {
				return fmt.Errorf("加载配置: %w", err)
			}

			inputFile := cmd.String("input")
			outputFile := cmd.String("output")
			systemPrompt := cmd.String("system-prompt")

			return runPipeline(ctx, cfg, logger, inputFile, outputFile, systemPrompt)
		},
	}
}

// asrResult 持有 ASR 阶段的结果。
type asrResult struct {
	text    string
	elapsed time.Duration
}

// llmResult 持有 LLM 阶段的结果。
type llmResult struct {
	reply        string
	elapsed      time.Duration
	firstTokenMs int64
}

// runPipeline 执行完整的 ASR → LLM → TTS 管道。
func runPipeline(ctx context.Context, cfg *config.Config, logger *slog.Logger,
	inputFile, outputFile, systemPrompt string) error {
	totalStart := time.Now()

	fmt.Println("=== Clarion 语音管道测试 ===")
	fmt.Println()

	audioData, sampleRate, err := readWAV(inputFile)
	if err != nil {
		return fmt.Errorf("读取 WAV: %w", err)
	}
	audioDurationMs := len(audioData) / 2 * 1000 / sampleRate
	fmt.Printf("输入: %s (%d ms, %d Hz)\n", inputFile, audioDurationMs, sampleRate)

	aResult, err := runASR(ctx, cfg, logger, audioData)
	if err != nil {
		return err
	}

	lResult, err := runLLM(ctx, cfg, logger, aResult.text, systemPrompt)
	if err != nil {
		return err
	}

	ttsElapsed, err := runTTS(ctx, cfg, logger, lResult.reply, outputFile)
	if err != nil {
		return err
	}

	printSummary(aResult.elapsed, lResult.elapsed, lResult.firstTokenMs, ttsElapsed, time.Since(totalStart))
	return nil
}

// runASR 执行 ASR 阶段：将音频文件送入流式 ASR 并获取识别文本。
func runASR(ctx context.Context, cfg *config.Config, logger *slog.Logger, audioData []byte) (asrResult, error) {
	fmt.Println()
	fmt.Print("ASR 识别中...")

	asrProv := asr.NewQwen(cfg.ASR.APIKey, asr.WithQwenLogger(logger))
	asrCfg := provider.ASRConfig{
		Model:      cfg.ASR.Model,
		SampleRate: cfg.ASR.SampleRate,
	}

	start := time.Now()
	stream, err := asrProv.StartStream(ctx, asrCfg)
	if err != nil {
		return asrResult{}, fmt.Errorf("ASR 启动失败: %w", err)
	}

	// 将音频按帧发送（每帧 20ms）。
	frameSize := cfg.ASR.SampleRate * 2 * 20 / 1000
	for i := 0; i < len(audioData); i += frameSize {
		end := min(i+frameSize, len(audioData))
		if feedErr := stream.FeedAudio(ctx, audioData[i:end]); feedErr != nil {
			logger.Warn("ASR FeedAudio 失败", slog.String("error", feedErr.Error()))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if closeErr := stream.Close(); closeErr != nil {
		logger.Warn("ASR Close", slog.String("error", closeErr.Error()))
	}

	var recognizedText string
	for evt := range stream.Events() {
		if evt.IsFinal && evt.Text != "" {
			recognizedText = evt.Text
		}
	}

	elapsed := time.Since(start)
	fmt.Printf(" 完成 (%v)\n", elapsed.Round(time.Millisecond))
	fmt.Printf("识别结果: \"%s\"\n", recognizedText)

	if recognizedText == "" {
		return asrResult{}, errors.New("ASR 未返回有效识别结果，请检查音频文件和 API Key")
	}

	return asrResult{text: recognizedText, elapsed: elapsed}, nil
}

// runLLM 执行 LLM 阶段：将 ASR 文本送入 LLM 并获取流式回复。
func runLLM(ctx context.Context, cfg *config.Config, logger *slog.Logger, recognizedText, systemPrompt string) (llmResult, error) {
	fmt.Println()
	fmt.Print("LLM 生成中...")

	llmProv := llm.NewDeepSeek(cfg.LLM.APIKey, cfg.LLM.BaseURL, llm.WithLogger(logger))
	llmCfg := provider.LLMConfig{
		Model:       cfg.LLM.Model,
		MaxTokens:   cfg.LLM.MaxTokens,
		Temperature: cfg.LLM.Temperature,
		TimeoutMs:   cfg.LLM.TimeoutMs,
	}

	messages := []provider.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: recognizedText},
	}

	start := time.Now()
	tokenCh, err := llmProv.GenerateStream(ctx, messages, llmCfg)
	if err != nil {
		return llmResult{}, fmt.Errorf("LLM 生成失败: %w", err)
	}

	var sb strings.Builder
	firstToken := true
	var firstTokenMs int64
	for token := range tokenCh {
		if firstToken {
			firstTokenMs = time.Since(start).Milliseconds()
			firstToken = false
		}
		sb.WriteString(token)
	}
	reply := sb.String()
	elapsed := time.Since(start)

	fmt.Printf(" 完成 (%v, 首 token %dms)\n", elapsed.Round(time.Millisecond), firstTokenMs)
	fmt.Printf("LLM 回复: \"%s\"\n", reply)

	if reply == "" {
		return llmResult{}, errors.New("LLM 未返回回复，请检查 API Key")
	}

	return llmResult{reply: reply, elapsed: elapsed, firstTokenMs: firstTokenMs}, nil
}

// runTTS 执行 TTS 阶段：将 LLM 回复合成为语音并写入 WAV 文件。
func runTTS(ctx context.Context, cfg *config.Config, logger *slog.Logger, text, outputFile string) (time.Duration, error) {
	fmt.Println()
	fmt.Print("TTS 合成中...")

	ttsProv := tts.NewDashScope(cfg.TTS.APIKey, tts.WithDashScopeLogger(logger))
	ttsCfg := provider.TTSConfig{
		Model:      cfg.TTS.Model,
		Voice:      cfg.TTS.Voice,
		SampleRate: cfg.TTS.SampleRate,
	}

	start := time.Now()
	replyAudio, err := ttsProv.Synthesize(ctx, text, ttsCfg)
	if err != nil {
		return 0, fmt.Errorf("TTS 合成失败: %w", err)
	}
	elapsed := time.Since(start)

	fmt.Printf(" 完成 (%v)\n", elapsed.Round(time.Millisecond))

	if writeErr := writeWAV(outputFile, replyAudio, cfg.TTS.SampleRate); writeErr != nil {
		return 0, fmt.Errorf("写入 WAV: %w", writeErr)
	}

	replyDurationMs := len(replyAudio) / 2 * 1000 / cfg.TTS.SampleRate
	fmt.Printf("输出: %s (%d ms 音频)\n", outputFile, replyDurationMs)

	_ = logger // 保持签名一致。
	return elapsed, nil
}

// printSummary 输出延迟汇总。
func printSummary(asrElapsed, llmElapsed time.Duration, llmFirstTokenMs int64, ttsElapsed, totalElapsed time.Duration) {
	fmt.Println()
	fmt.Println("=== 延迟汇总 ===")
	fmt.Printf("ASR 识别:     %v\n", asrElapsed.Round(time.Millisecond))
	fmt.Printf("LLM 首 token: %d ms\n", llmFirstTokenMs)
	fmt.Printf("LLM 完整:     %v\n", llmElapsed.Round(time.Millisecond))
	fmt.Printf("TTS 合成:     %v\n", ttsElapsed.Round(time.Millisecond))
	fmt.Printf("端到端总计:   %v\n", totalElapsed.Round(time.Millisecond))
	fmt.Println()

	estimatedFirstResponse := asrElapsed + time.Duration(llmFirstTokenMs)*time.Millisecond + 300*time.Millisecond
	fmt.Printf("预估流式首响应: ~%v （ASR + LLM首Token + TTS首音频块）\n",
		estimatedFirstResponse.Round(time.Millisecond))

	if estimatedFirstResponse <= 1500*time.Millisecond {
		fmt.Println("状态: 达标 (< 1.5s)")
	} else {
		fmt.Printf("状态: 超标 (目标 < 1.5s, 实际 ~%v)\n",
			estimatedFirstResponse.Round(time.Millisecond))
	}
}

// writeWAV 将 PCM16 LE 数据写入 WAV 文件。
func writeWAV(path string, pcm []byte, sampleRate int) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create WAV file: %w", err)
	}

	dataSize := uint32(len(pcm))
	fileSize := 36 + dataSize

	// WAV 文件头（44 字节）。
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], fileSize)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)                   // PCM 子块大小
	binary.LittleEndian.PutUint16(header[20:22], 1)                    // PCM 格式
	binary.LittleEndian.PutUint16(header[22:24], 1)                    // 单声道
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))   // 采样率
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*2)) // 字节率
	binary.LittleEndian.PutUint16(header[32:34], 2)                    // 块对齐
	binary.LittleEndian.PutUint16(header[34:36], 16)                   // 位深度
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataSize)

	if _, writeErr := f.Write(header); writeErr != nil {
		closeErr := f.Close()
		return errors.Join(fmt.Errorf("write WAV header: %w", writeErr), closeErr)
	}
	if _, writeErr := f.Write(pcm); writeErr != nil {
		closeErr := f.Close()
		return errors.Join(fmt.Errorf("write WAV data: %w", writeErr), closeErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		return fmt.Errorf("close WAV file: %w", closeErr)
	}
	return nil
}

// readWAV 从 WAV 文件读取 PCM16 LE 音频数据。
// 返回 PCM 数据、采样率和错误。
func readWAV(path string) ([]byte, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read WAV file: %w", err)
	}

	if len(data) < 44 {
		return nil, 0, errors.New("文件过小，不是有效的 WAV")
	}

	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, errors.New("不是有效的 WAV 文件")
	}

	sampleRate := int(binary.LittleEndian.Uint32(data[24:28]))

	// 查找 data 子块。
	offset := 12
	for offset < len(data)-8 {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		if chunkID == "data" {
			start := offset + 8
			end := start + chunkSize
			if end > len(data) {
				end = len(data)
			}
			return data[start:end], sampleRate, nil
		}
		offset += 8 + chunkSize
		// 保持偶数对齐。
		if offset%2 != 0 {
			offset++
		}
	}

	return nil, 0, errors.New("未找到 data 子块")
}
