// Package precompile 实现场景模板的 TTS 音频预编译。
// 预合成音频用于确定性提示（如开场问候），以减少运行时延迟。
package precompile

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/omeyang/clarion/internal/provider"
)

// Synthesizer 通过 TTS 提供者将文本提示预编译为音频字节。
type Synthesizer struct {
	tts    provider.TTSProvider
	logger *slog.Logger
}

// NewSynthesizer 创建由给定 TTS 提供者支持的 Synthesizer。
func NewSynthesizer(tts provider.TTSProvider, logger *slog.Logger) *Synthesizer {
	return &Synthesizer{tts: tts, logger: logger}
}

// PrecompileAudios 为名称到文本映射中的每个条目合成音频，
// 并返回名称到音频字节的映射。如果任何一个合成失败，
// 返回错误且丢弃部分结果。
func (s *Synthesizer) PrecompileAudios(ctx context.Context, audios map[string]string) (map[string][]byte, error) {
	if len(audios) == 0 {
		return map[string][]byte{}, nil
	}

	results := make(map[string][]byte, len(audios))

	for name, text := range audios {
		s.logger.Info("precompiling audio", slog.String("name", name), slog.Int("text_len", len(text)))

		data, err := s.tts.Synthesize(ctx, text, provider.TTSConfig{})
		if err != nil {
			return nil, fmt.Errorf("synthesize %q: %w", name, err)
		}

		results[name] = data
		s.logger.Info("precompiled audio",
			slog.String("name", name),
			slog.Int("audio_bytes", len(data)),
		)
	}

	return results, nil
}
