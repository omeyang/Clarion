package call

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/omeyang/Sonata/engine/pcm"
)

// audioCacheDir 是 TTS 临时 WAV 文件的存储目录。
const audioCacheDir = "/tmp/clarion-audio"

// writeWAVFile 将 PCM 音频数据写入临时 WAV 文件，返回文件路径。
// 调用方负责在使用完毕后删除该文件。
func (s *Session) writeWAVFile(downsampled []byte) (string, error) {
	if mkErr := os.MkdirAll(audioCacheDir, 0o750); mkErr != nil {
		return "", fmt.Errorf("创建音频缓存目录: %w", mkErr)
	}

	tmpFile, fileErr := os.CreateTemp(audioCacheDir, "tts-*.wav")
	if fileErr != nil {
		return "", fmt.Errorf("创建 TTS 临时文件: %w", fileErr)
	}
	tmpPath := tmpFile.Name()

	wavHeader := pcm.BuildWAVHeader(len(downsampled), 8000, 1, 16)
	if _, wErr := tmpFile.Write(wavHeader); wErr != nil {
		if cErr := tmpFile.Close(); cErr != nil {
			s.logger.Warn("关闭临时文件失败", slog.String("error", cErr.Error()))
		}
		return "", fmt.Errorf("写入 WAV 头部: %w", wErr)
	}
	if _, wErr := tmpFile.Write(downsampled); wErr != nil {
		if cErr := tmpFile.Close(); cErr != nil {
			s.logger.Warn("关闭临时文件失败", slog.String("error", cErr.Error()))
		}
		return "", fmt.Errorf("写入 TTS 音频数据: %w", wErr)
	}
	if cErr := tmpFile.Close(); cErr != nil {
		return "", fmt.Errorf("关闭临时文件: %w", cErr)
	}

	return tmpPath, nil
}

// removeFile 删除文件，失败时记录警告。
func (s *Session) removeFile(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		s.logger.Warn("删除临时文件失败", slog.String("path", path), slog.String("error", err.Error()))
	}
}

// playViaESL 通过 ESL 让 FreeSWITCH 播放 WAV 文件。
// 返回 true 表示通过 ESL 播放（PLAYBACK_STOP 触发 botDone），false 表示未使用 ESL。
func (s *Session) playViaESL(ttsCtx context.Context, tmpPath string, pcmLen int) bool {
	if s.cfg.ESL == nil || s.channelUUID == "" {
		return false
	}

	if ttsCtx.Err() != nil {
		return true
	}

	playCmd := fmt.Sprintf("uuid_broadcast %s %s aleg", s.channelUUID, tmpPath)
	reply, playErr := s.cfg.ESL.SendCommand(ttsCtx, playCmd)
	if playErr != nil {
		if ttsCtx.Err() == nil {
			s.logger.Error("ESL 播放命令失败", slog.String("error", playErr.Error()))
		}
		return true
	}
	s.ttsPlaying.Store(true)
	s.logger.Info("TTS 播放已启动",
		slog.String("file", tmpPath),
		slog.Int("pcm_bytes", pcmLen),
		slog.String("reply", reply),
	)
	return true
}

// playViaAudioOut 通过 AudioOut 通道发送音频帧（无 ESL 时退回方案）。
func (s *Session) playViaAudioOut(downsampled []byte) {
	const frameSize = 320
	for i := 0; i < len(downsampled); i += frameSize {
		end := min(i+frameSize, len(downsampled))
		frame := downsampled[i:end]

		select {
		case <-s.ctx.Done():
			return
		case s.cfg.AudioOut <- frame:
		}
	}
}

// synthesizeAndPlayAsync 异步合成 TTS 并通过 ESL 播放音频文件。
// 完成后向 botDoneCh 发送信号。
// 每次调用会取消前一次的 TTS 合成，确保同时只有一个 TTS 运行。
func (s *Session) synthesizeAndPlayAsync(text string) {
	// 取消前一次进行中的 TTS。
	if s.ttsCancel != nil {
		s.ttsCancel()
	}
	ttsCtx, cancel := context.WithCancel(s.ctx)
	s.ttsCancel = cancel

	go func() {
		// sendBotDone 标记是否在 goroutine 结束时发送 botDoneCh 信号。
		// ESL 路径由 PLAYBACK_STOP 事件触发，被取消时也不应触发。
		sendBotDone := true

		defer func() {
			if sendBotDone {
				select {
				case s.botDoneCh <- struct{}{}:
				default:
				}
			}
		}()

		downsampled, ok := s.synthesizeAndDownsample(ttsCtx, text)
		if !ok {
			if ttsCtx.Err() != nil {
				sendBotDone = false
			}
			return
		}

		tmpPath, err := s.writeWAVFile(downsampled)
		if err != nil {
			s.logger.Error("写入 WAV 文件失败", slog.String("error", err.Error()))
			return
		}
		defer s.removeFile(tmpPath)

		if s.playViaESL(ttsCtx, tmpPath, len(downsampled)) {
			// ESL 路径由 PLAYBACK_STOP 事件触发 botDone。
			sendBotDone = false
			// 等待文件播放完成后才能删除文件。
			// 估算播放时长：8000 Hz × 2 字节/采样 = 16000 字节/秒。
			s.waitPlayback(ttsCtx, len(downsampled), 1000)
			return
		}

		// 无 ESL 时退回到通过 AudioOut 发送（测试兼容）。
		s.playViaAudioOut(downsampled)
	}()
}

// synthesizeAndDownsample 合成 TTS 并降采样为 8 kHz。
// 返回降采样后的 PCM 数据和是否成功。
func (s *Session) synthesizeAndDownsample(ttsCtx context.Context, text string) ([]byte, bool) {
	s.logger.Info("TTS 合成开始", slog.String("text", text))
	pcmData, err := s.cfg.TTS.Synthesize(ttsCtx, text, s.cfg.TTSConfig)
	if err != nil {
		if ttsCtx.Err() != nil {
			s.logger.Info("TTS 合成被取消", slog.String("text", text))
		} else {
			s.logger.Error("TTS 合成失败", slog.String("error", err.Error()))
		}
		return nil, false
	}
	s.logger.Info("TTS 合成完成", slog.Int("audio_bytes", len(pcmData)))

	if len(pcmData) == 0 {
		return nil, false
	}

	downsampled := pcm.Resample16to8(pcmData)
	if downsampled == nil {
		s.logger.Warn("TTS 重采样结果为空", slog.Int("pcm_bytes", len(pcmData)))
		return nil, false
	}
	return downsampled, true
}

// waitPlayback 等待 ESL 播放完成或被取消。
// marginMs 是额外等待的毫秒数（留余量确保播放完成）。
func (s *Session) waitPlayback(ttsCtx context.Context, pcmLen int, marginMs int) {
	durationMs := pcmLen * 1000 / 16000
	select {
	case <-ttsCtx.Done():
	case <-s.ctx.Done():
	case <-time.After(time.Duration(durationMs+marginMs) * time.Millisecond):
	}
}

// synthesizeAndPlayStreamAsync 流式合成 TTS 并逐句播放。
// 从 sentenceCh 接收句子，每句独立合成为 WAV 并播放。
// 在播放当前句时预合成下一句，减少句间停顿。
// onComplete 在所有句段播放完成后调用（可为 nil），用于预推理轮次确认。
func (s *Session) synthesizeAndPlayStreamAsync(sentenceCh <-chan string, onComplete func()) {
	// 取消前一次进行中的 TTS。
	if s.ttsCancel != nil {
		s.ttsCancel()
	}
	ttsCtx, cancel := context.WithCancel(s.ctx)
	s.ttsCancel = cancel
	s.ttsStreaming.Store(true)

	go func() {
		defer func() {
			s.ttsStreaming.Store(false)
			s.ttsPlaying.Store(false)
			if onComplete != nil {
				onComplete()
			}
			select {
			case s.botDoneCh <- struct{}{}:
			default:
			}
		}()

		// 播放填充词，掩盖 LLM 处理延迟。
		s.playFiller(ttsCtx)

		// 预合成段：播放当前句时异步合成下一句。
		prefetchCh := make(chan ttsSegment, 2)

		// 生产者：逐句调用 TTS.Synthesize。
		go s.prefetchSegments(ttsCtx, sentenceCh, prefetchCh)

		// 消费者：逐句播放。
		for seg := range prefetchCh {
			if ttsCtx.Err() != nil {
				return
			}
			s.playSegment(ttsCtx, seg.pcm)
		}
	}()
}

// ttsSegment 是预合成的 TTS 句段。
type ttsSegment struct {
	pcm  []byte
	text string
}

// prefetchSegments 逐句调用 TTS 合成，将结果发送到 outCh。
func (s *Session) prefetchSegments(ttsCtx context.Context, sentenceCh <-chan string, outCh chan<- ttsSegment) {
	defer close(outCh)
	for sentence := range sentenceCh {
		if ttsCtx.Err() != nil {
			return
		}
		s.logger.Info("TTS 句段合成开始", slog.String("text", sentence))
		pcmData, err := s.cfg.TTS.Synthesize(ttsCtx, sentence, s.cfg.TTSConfig)
		if err != nil {
			if ttsCtx.Err() != nil {
				return
			}
			s.logger.Error("TTS 句段合成失败", slog.String("error", err.Error()))
			continue
		}
		s.logger.Info("TTS 句段合成完成",
			slog.String("text", sentence),
			slog.Int("audio_bytes", len(pcmData)),
		)
		select {
		case outCh <- ttsSegment{pcm: pcmData, text: sentence}:
		case <-ttsCtx.Done():
			return
		}
	}
}

// playSegment 将一段 PCM 音频降采样并播放。
// 优先使用直接帧发送（避免文件 I/O），ESL 路径作为退回方案。
func (s *Session) playSegment(ttsCtx context.Context, rawPCM []byte) {
	downsampled := pcm.Resample16to8(rawPCM)
	if downsampled == nil {
		s.logger.Warn("TTS 句段重采样为空", slog.Int("pcm_bytes", len(rawPCM)))
		return
	}

	// 优先使用 AudioOut 直接帧发送（避免临时文件 I/O）。
	if s.cfg.AudioOut != nil {
		s.playViaAudioOutPaced(ttsCtx, downsampled)
		return
	}

	// 退回到 ESL 文件路径。
	tmpPath, err := s.writeWAVFile(downsampled)
	if err != nil {
		s.logger.Error("写入 WAV 文件失败", slog.String("error", err.Error()))
		return
	}
	defer s.removeFile(tmpPath)

	if s.playViaESL(ttsCtx, tmpPath, len(downsampled)) {
		s.waitPlayback(ttsCtx, len(downsampled), 100)
	}
}

// playViaAudioOutPaced 通过 AudioOut 通道发送音频帧，带节奏控制和抖动缓冲。
// 先将帧写入 JitterBuffer 预缓冲，达到阈值后按 20ms 间隔消费发送，
// 吸收网络抖动导致的帧到达不均匀。
func (s *Session) playViaAudioOutPaced(ctx context.Context, rawPCM []byte) {
	const (
		frameSize     = 320 // 320 bytes = 20ms at 8kHz/16bit。
		frameDuration = 20 * time.Millisecond
	)

	s.ttsPlaying.Store(true)
	defer s.ttsPlaying.Store(false)

	jb := NewJitterBuffer(DefaultJitterBufferConfig())

	// 将所有帧写入缓冲区。
	for i := 0; i < len(rawPCM); i += frameSize {
		end := min(i+frameSize, len(rawPCM))
		jb.Push(rawPCM[i:end])
	}

	// 按节奏从缓冲区消费并发送。复用单个 timer 避免每帧分配。
	timer := time.NewTimer(0)
	<-timer.C
	defer timer.Stop()

	start := time.Now()
	frameNum := 0
	for {
		if ctx.Err() != nil {
			return
		}

		frame, ok := jb.Pop()
		if !ok {
			return
		}

		select {
		case <-ctx.Done():
			return
		case s.cfg.AudioOut <- frame:
		}

		frameNum++
		// 节奏控制：确保累计发送时长不超前于实际音频时长。
		expectedElapsed := time.Duration(frameNum) * frameDuration
		if sleep := expectedElapsed - time.Since(start); sleep > 0 {
			timer.Reset(sleep)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
		}
	}
}
