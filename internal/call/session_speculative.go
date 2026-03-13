package call

import (
	"context"
	"log/slog"
	"strings"

	sonataprovider "github.com/omeyang/Sonata/engine/aiface"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/provider"
)

// 预推理常量。
const (
	// speculativeStableThreshold 触发预推理需要的 partial ASR 连续稳定次数。
	// 每次 partial 间隔约 100-200ms，3 次 ≈ 300-600ms 的稳定窗口。
	speculativeStableThreshold = 3

	// speculativeEarlyThreshold 句末标点场景下的加速阈值。
	// 当 partial 文本以句号/问号结尾时，用户大概率已说完，提前触发。
	speculativeEarlyThreshold = 2
)

// sentenceEndPunctuation 句末标点，用于端点检测加速。
const sentenceEndPunctuation = "。！？.!?"

// speculativeRun 持有一次预推理的状态。
type speculativeRun struct {
	inputText  string
	sentenceCh <-chan string
	commit     func()
	cancel     context.CancelFunc
}

// handlePartialASR 处理 partial ASR 结果，跟踪稳定性并在条件满足时启动预推理。
// partial ASR 稳定意味着用户可能已说完话，提前启动 LLM 可省去 500-800ms 首 Token 延迟。
func (s *Session) handlePartialASR(ctx context.Context, evt provider.ASREvent) {
	state := s.mfsm.State()
	if state != engine.MediaUserSpeaking && state != engine.MediaWaitingUser {
		return
	}

	text := evt.Text
	if text == "" {
		return
	}

	// 跟踪 partial 稳定性。
	if text == s.lastPartialText {
		s.partialStableCount++
	} else {
		s.lastPartialText = text
		s.partialStableCount = 1
		// partial 变化，取消已有的预推理。
		s.cancelSpeculative()
	}

	// 确定触发阈值：句末标点加速触发。
	threshold := speculativeStableThreshold
	if endsWithSentencePunctuation(text) {
		threshold = speculativeEarlyThreshold
	}

	if s.partialStableCount >= threshold && s.speculative == nil {
		s.startSpeculative(ctx, text)
	}
}

// startSpeculative 基于当前 partial ASR 文本启动预推理。
func (s *Session) startSpeculative(ctx context.Context, text string) {
	if s.cfg.DialogueEngine == nil {
		return
	}

	specCtx, cancel := context.WithCancel(ctx)
	sentenceCh, commit, err := s.cfg.DialogueEngine.PrepareStream(specCtx, text)
	if err != nil {
		cancel()
		s.logger.Warn("预推理启动失败", slog.String("error", err.Error()))
		return
	}

	s.speculative = &speculativeRun{
		inputText:  text,
		sentenceCh: sentenceCh,
		commit:     commit,
		cancel:     cancel,
	}
	s.logger.Info("预推理已启动", slog.String("text", text))
}

// cancelSpeculative 取消当前预推理（如果有）。
func (s *Session) cancelSpeculative() {
	if s.speculative == nil {
		return
	}
	s.speculative.cancel()
	// 消费剩余句子，避免 goroutine 泄漏。
	go func(ch <-chan string) {
		for range ch {
		}
	}(s.speculative.sentenceCh)
	s.speculative = nil
}

// resetPartialTracking 重置 partial ASR 追踪状态。
func (s *Session) resetPartialTracking() {
	s.lastPartialText = ""
	s.partialStableCount = 0
	s.cancelSpeculative()
}

// warmupProviders 预热实现了 Warmer 接口的提供者。
func (s *Session) warmupProviders(ctx context.Context) {
	providers := []any{s.cfg.ASR, s.cfg.LLM, s.cfg.TTS}
	for _, p := range providers {
		if w, ok := p.(sonataprovider.Warmer); ok {
			if err := w.Warmup(ctx); err != nil {
				s.logger.Warn("provider warmup 失败", slog.String("error", err.Error()))
			}
		}
	}
}

// endsWithSentencePunctuation 判断文本是否以句末标点结尾。
func endsWithSentencePunctuation(text string) bool {
	if text == "" {
		return false
	}
	// 获取最后一个 rune。
	for i := len(text); i > 0; {
		r, size := decodeLastRune(text, i)
		if r == 0 {
			return false
		}
		// 跳过尾部空白。
		if r == ' ' || r == '\t' || r == '\n' {
			i -= size
			continue
		}
		return strings.ContainsRune(sentenceEndPunctuation, r)
	}
	return false
}

// decodeLastRune 从 text[:end] 解码最后一个 rune。
func decodeLastRune(text string, end int) (rune, int) {
	if end <= 0 {
		return 0, 0
	}
	// UTF-8 向前回溯（最多 4 字节）。
	for start := end - 1; start >= 0 && start >= end-4; start-- {
		r := rune(text[start])
		if r < 0x80 { // ASCII
			return r, 1
		}
		if r >= 0xC0 { // UTF-8 起始字节
			runes := []rune(text[start:end])
			if len(runes) == 1 {
				return runes[0], end - start
			}
		}
	}
	return 0, 0
}
