package call

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// fillerIndex 用于轮询选择填充词，避免使用随机数（gosec G404）。
var fillerIndex atomic.Int64

// playFiller 播放填充词音频，掩盖 LLM 处理延迟。
// 填充词为预合成的 8kHz PCM16 数据，通过 ESL 播放或退回 AudioOut。
// 典型填充词（"嗯"、"好的"）约 200-300ms，将感知延迟从 ~2s 降至 ~200ms。
// 多个填充词通过轮询方式选择，避免重复感。
func (s *Session) playFiller(ttsCtx context.Context) {
	if len(s.cfg.FillerAudios) == 0 {
		return
	}

	idx := int(fillerIndex.Add(1)-1) % len(s.cfg.FillerAudios)
	filler := s.cfg.FillerAudios[idx]
	if len(filler) == 0 {
		return
	}

	s.logger.Info("播放填充词", slog.Int("audio_bytes", len(filler)))

	// 填充词已经是 8kHz PCM16，无需重采样。
	tmpPath, err := s.writeWAVFile(filler)
	if err != nil {
		s.logger.Warn("填充词 WAV 写入失败", slog.String("error", err.Error()))
		return
	}
	defer s.removeFile(tmpPath)

	if s.playViaESL(ttsCtx, tmpPath, len(filler)) {
		// 填充词较短，用较小的等待余量。
		s.waitPlayback(ttsCtx, len(filler), 50)
		return
	}

	// 无 ESL 时退回 AudioOut。
	s.playViaAudioOut(filler)
}
