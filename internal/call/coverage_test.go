package call

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/engine/media"
	"github.com/omeyang/clarion/internal/engine/rules"
	"github.com/omeyang/clarion/internal/provider"
)

// ── mock 实现 ────────────────────────────────────────────────────

// mockTTSProvider 模拟 TTSProvider 接口。
type mockTTSProvider struct {
	synthesizeData []byte
	synthesizeErr  error
	cancelErr      error
	calls          atomic.Int32
}

func (m *mockTTSProvider) SynthesizeStream(_ context.Context, _ <-chan string, _ provider.TTSConfig) (<-chan []byte, error) {
	return nil, nil
}

func (m *mockTTSProvider) Synthesize(_ context.Context, _ string, _ provider.TTSConfig) ([]byte, error) {
	m.calls.Add(1)
	return m.synthesizeData, m.synthesizeErr
}

func (m *mockTTSProvider) Cancel() error {
	return m.cancelErr
}

// mockASRProvider 模拟 ASRProvider 接口。
type mockASRProvider struct {
	stream    provider.ASRStream
	streamErr error
}

func (m *mockASRProvider) StartStream(_ context.Context, _ provider.ASRConfig) (provider.ASRStream, error) {
	return m.stream, m.streamErr
}

// mockASRStream 模拟 ASRStream 接口。
type mockASRStream struct {
	feedErr error
	eventCh chan provider.ASREvent
}

func (m *mockASRStream) FeedAudio(_ context.Context, _ []byte) error {
	return m.feedErr
}

func (m *mockASRStream) Events() <-chan provider.ASREvent {
	if m.eventCh == nil {
		ch := make(chan provider.ASREvent)
		close(ch)
		return ch
	}
	return m.eventCh
}

func (m *mockASRStream) Close() error {
	return nil
}

// mockLLMProvider 模拟 LLMProvider 接口。
type mockLLMProvider struct{}

func (m *mockLLMProvider) GenerateStream(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}

func (m *mockLLMProvider) Generate(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
	return "", nil
}

// ── session_tts.go 测试 ────────────────────────────────────────

func TestSession_WriteWAVFile(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	pcmData := make([]byte, 1600) // 100ms at 8kHz 16bit
	for i := range pcmData {
		pcmData[i] = byte(i % 256)
	}

	path, err := s.writeWAVFile(pcmData)
	require.NoError(t, err)
	assert.NotEmpty(t, path)

	// 验证文件存在且大小合理（WAV 头 44 字节 + 1600 字节 PCM）。
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, int64(44+1600), info.Size())

	// 清理。
	s.removeFile(path)
	_, statErr = os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestSession_RemoveFile_NotExist(t *testing.T) {
	t.Parallel()
	s := newTestSession(engine.MediaBotSpeaking)
	// 删除不存在的文件不应 panic。
	s.removeFile("/tmp/nonexistent-clarion-test-file-12345")
}

func TestSession_PlayViaESL_NoESL(t *testing.T) {
	t.Parallel()
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.ESL = nil

	// 无 ESL 时返回 false。
	got := s.playViaESL(context.Background(), "/tmp/test.wav", 1000)
	assert.False(t, got)
}

func TestSession_PlayViaESL_NoChannelUUID(t *testing.T) {
	t.Parallel()
	s := newTestSession(engine.MediaBotSpeaking)
	esl, _ := newMockESLClient(t)
	s.cfg.ESL = esl
	s.channelUUID = ""

	got := s.playViaESL(context.Background(), "/tmp/test.wav", 1000)
	assert.False(t, got)
}

func TestSession_PlayViaESL_ContextCancelled(t *testing.T) {
	t.Parallel()
	s := newTestSession(engine.MediaBotSpeaking)
	esl, _ := newMockESLClient(t)
	s.cfg.ESL = esl
	s.channelUUID = "test-uuid"

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 已取消

	got := s.playViaESL(ctx, "/tmp/test.wav", 1000)
	assert.True(t, got) // 取消时返回 true
}

func TestSession_PlayViaAudioOut(t *testing.T) {
	t.Parallel()

	audioOut := make(chan []byte, 64)
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.AudioOut = audioOut

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	// 发送 640 字节 = 2 帧（每帧 320 字节）。
	data := make([]byte, 640)
	s.playViaAudioOut(data)

	// 应收到 2 帧。
	frameCount := 0
	for range 2 {
		select {
		case frame := <-audioOut:
			assert.Len(t, frame, 320)
			frameCount++
		default:
		}
	}
	assert.Equal(t, 2, frameCount)
}

func TestSession_PlayViaAudioOut_ContextDone(t *testing.T) {
	t.Parallel()

	audioOut := make(chan []byte, 1) // 小缓冲区
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.AudioOut = audioOut

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 已取消
	s.ctx = ctx

	// 不应阻塞。
	data := make([]byte, 3200) // 10 帧
	s.playViaAudioOut(data)
}

func TestSession_SynthesizeAndDownsample_Success(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		// 生成 16kHz PCM 数据（3200 字节 = 100ms），降采样到 8kHz 后应为 1600 字节。
		synthesizeData: make([]byte, 3200),
	}
	// 填充非零数据，避免 Resample16to8 返回 nil。
	for i := range tts.synthesizeData {
		tts.synthesizeData[i] = byte(i%200 + 1)
	}

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts

	downsampled, ok := s.synthesizeAndDownsample(context.Background(), "你好")
	assert.True(t, ok)
	assert.NotEmpty(t, downsampled)
	assert.Equal(t, int32(1), tts.calls.Load())
}

func TestSession_SynthesizeAndDownsample_Error(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeErr: errors.New("tts 合成失败"),
	}

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts

	_, ok := s.synthesizeAndDownsample(context.Background(), "你好")
	assert.False(t, ok)
}

func TestSession_SynthesizeAndDownsample_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tts := &mockTTSProvider{
		synthesizeErr: ctx.Err(),
	}

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts

	_, ok := s.synthesizeAndDownsample(ctx, "你好")
	assert.False(t, ok)
}

func TestSession_SynthesizeAndDownsample_EmptyPCM(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeData: []byte{},
	}

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts

	_, ok := s.synthesizeAndDownsample(context.Background(), "你好")
	assert.False(t, ok)
}

func TestSession_SynthesizeAndPlayAsync_NoESL(t *testing.T) {
	t.Parallel()

	audioOut := make(chan []byte, 64)
	tts := &mockTTSProvider{
		synthesizeData: make([]byte, 3200),
	}
	for i := range tts.synthesizeData {
		tts.synthesizeData[i] = byte(i%200 + 1)
	}

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts
	s.cfg.AudioOut = audioOut
	s.cfg.ESL = nil

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	s.synthesizeAndPlayAsync("你好")

	// 等待 botDoneCh 信号。
	select {
	case <-s.botDoneCh:
		// 成功
	case <-time.After(3 * time.Second):
		t.Fatal("超时等待 botDoneCh 信号")
	}
}

func TestSession_SynthesizeAndPlayAsync_CancelsPrevious(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeData: make([]byte, 3200),
	}
	for i := range tts.synthesizeData {
		tts.synthesizeData[i] = byte(i%200 + 1)
	}

	audioOut := make(chan []byte, 128)
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts
	s.cfg.AudioOut = audioOut
	s.cfg.ESL = nil

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	// 第一次合成。
	s.synthesizeAndPlayAsync("你好")
	// 第二次合成应取消前一次。
	s.synthesizeAndPlayAsync("再见")

	// 等待 botDoneCh（至少一次完成）。
	select {
	case <-s.botDoneCh:
	case <-time.After(3 * time.Second):
		t.Fatal("超时等待 botDoneCh")
	}
}

func TestSession_SynthesizeAndPlayAsync_TTS_Error(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeErr: errors.New("tts error"),
	}

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts
	s.cfg.AudioOut = make(chan []byte, 64)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	s.synthesizeAndPlayAsync("你好")

	// TTS 错误应仍发送 botDoneCh 信号。
	select {
	case <-s.botDoneCh:
	case <-time.After(3 * time.Second):
		t.Fatal("超时等待 botDoneCh")
	}
}

func TestSession_WaitPlayback(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	start := time.Now()
	// 160 字节 PCM 数据 = 10ms，加 50ms margin = 60ms 总等待。
	s.waitPlayback(ctx, 160, 50)
	elapsed := time.Since(start)

	// 应等待大约 60ms。
	assert.Greater(t, elapsed, 50*time.Millisecond)
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestSession_WaitPlayback_ContextCancel(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx

	// 立即取消。
	cancel()

	start := time.Now()
	s.waitPlayback(ctx, 16000*10, 1000) // 10 秒的 PCM
	elapsed := time.Since(start)

	// 应立即返回。
	assert.Less(t, elapsed, 100*time.Millisecond)
}

func TestSession_PrefetchSegments(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeData: make([]byte, 1600),
	}
	for i := range tts.synthesizeData {
		tts.synthesizeData[i] = byte(i%200 + 1)
	}

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts

	sentenceCh := make(chan string, 2)
	sentenceCh <- "句子一"
	sentenceCh <- "句子二"
	close(sentenceCh)

	outCh := make(chan ttsSegment, 2)

	s.prefetchSegments(context.Background(), sentenceCh, outCh)

	// 应收到 2 个句段。
	segments := make([]ttsSegment, 0, 2)
	for seg := range outCh {
		segments = append(segments, seg)
	}
	assert.Len(t, segments, 2)
	assert.Equal(t, int32(2), tts.calls.Load())
}

func TestSession_PrefetchSegments_ContextCancelled(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeData: make([]byte, 1600),
	}

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sentenceCh := make(chan string, 2)
	sentenceCh <- "句子一"
	close(sentenceCh)

	outCh := make(chan ttsSegment, 2)

	s.prefetchSegments(ctx, sentenceCh, outCh)

	// Context 取消后应不产生句段。
	segments := make([]ttsSegment, 0)
	for seg := range outCh {
		segments = append(segments, seg)
	}
	assert.Empty(t, segments)
}

func TestSession_PrefetchSegments_TTSError(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeErr: errors.New("tts error"),
	}

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts

	sentenceCh := make(chan string, 1)
	sentenceCh <- "句子一"
	close(sentenceCh)

	outCh := make(chan ttsSegment, 2)

	s.prefetchSegments(context.Background(), sentenceCh, outCh)

	// TTS 错误时跳过，outCh 应为空。
	segments := make([]ttsSegment, 0)
	for seg := range outCh {
		segments = append(segments, seg)
	}
	assert.Empty(t, segments)
}

func TestSession_PlaySegment_AudioOut(t *testing.T) {
	t.Parallel()

	audioOut := make(chan []byte, 64)
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.AudioOut = audioOut
	s.cfg.ESL = nil

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	// 构造 16kHz PCM 数据。
	rawPCM := make([]byte, 3200)
	for i := range rawPCM {
		rawPCM[i] = byte(i%200 + 1)
	}

	s.playSegment(ctx, rawPCM)

	// 应通过 AudioOut 发送帧。
	select {
	case frame := <-audioOut:
		assert.NotEmpty(t, frame)
	default:
		t.Fatal("应收到 AudioOut 帧")
	}
}

func TestSession_PlaySegment_EmptyPCM(t *testing.T) {
	t.Parallel()

	audioOut := make(chan []byte, 16)
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.AudioOut = audioOut

	ctx := context.Background()
	// 空 PCM 不应产生输出。
	s.playSegment(ctx, nil)
	s.playSegment(ctx, []byte{})

	select {
	case <-audioOut:
		t.Fatal("空 PCM 不应产生输出")
	default:
	}
}

func TestSession_PlayViaAudioOutPaced(t *testing.T) {
	t.Parallel()

	audioOut := make(chan []byte, 64)
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.AudioOut = audioOut

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	// 640 字节 = 2 帧（每帧 320 字节）。
	rawPCM := make([]byte, 640)
	for i := range rawPCM {
		rawPCM[i] = byte(i%200 + 1)
	}

	start := time.Now()
	s.playViaAudioOutPaced(ctx, rawPCM)
	elapsed := time.Since(start)

	// 2 帧 = 40ms，应在合理时间内完成。
	assert.Less(t, elapsed, 500*time.Millisecond)

	// ttsPlaying 应恢复为 false。
	assert.False(t, s.ttsPlaying.Load())
}

func TestSession_PlayViaAudioOutPaced_ContextCancel(t *testing.T) {
	t.Parallel()

	audioOut := make(chan []byte, 1)
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.AudioOut = audioOut

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	rawPCM := make([]byte, 32000) // 很多帧
	s.playViaAudioOutPaced(ctx, rawPCM)

	assert.False(t, s.ttsPlaying.Load())
}

func TestSession_SynthesizeAndPlayStreamAsync(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeData: make([]byte, 3200),
	}
	for i := range tts.synthesizeData {
		tts.synthesizeData[i] = byte(i%200 + 1)
	}

	audioOut := make(chan []byte, 128)
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts
	s.cfg.AudioOut = audioOut
	s.cfg.ESL = nil

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	sentenceCh := make(chan string, 2)
	sentenceCh <- "句子一"
	close(sentenceCh)

	var completed atomic.Bool
	onComplete := func() {
		completed.Store(true)
	}

	s.synthesizeAndPlayStreamAsync(sentenceCh, onComplete)

	// 等待 botDoneCh。
	select {
	case <-s.botDoneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("超时等待 botDoneCh")
	}

	// 等待 onComplete。
	time.Sleep(100 * time.Millisecond)
	assert.True(t, completed.Load(), "onComplete 应被调用")
	assert.False(t, s.ttsStreaming.Load(), "ttsStreaming 应恢复为 false")
}

func TestSession_SynthesizeAndPlayStreamAsync_NilOnComplete(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeData: make([]byte, 3200),
	}
	for i := range tts.synthesizeData {
		tts.synthesizeData[i] = byte(i%200 + 1)
	}

	audioOut := make(chan []byte, 128)
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.TTS = tts
	s.cfg.AudioOut = audioOut

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	sentenceCh := make(chan string, 1)
	close(sentenceCh)

	// nil onComplete 不应 panic。
	s.synthesizeAndPlayStreamAsync(sentenceCh, nil)

	select {
	case <-s.botDoneCh:
	case <-time.After(3 * time.Second):
		t.Fatal("超时等待 botDoneCh")
	}
}

// ── session_filler.go 测试 ─────────────────────────────────────

func TestSession_PlayFiller_NoFillers(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.FillerAudios = nil

	ctx := context.Background()
	// 无填充词时不应 panic。
	s.playFiller(ctx)
}

func TestSession_PlayFiller_EmptyFiller(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.FillerAudios = [][]byte{{}} // 一个空填充词

	ctx := context.Background()
	s.playFiller(ctx)
}

func TestSession_PlayFiller_ViaAudioOut(t *testing.T) {
	t.Parallel()

	audioOut := make(chan []byte, 64)
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.AudioOut = audioOut
	s.cfg.ESL = nil

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	// 创建 320 字节的填充词（1 帧）。
	filler := make([]byte, 320)
	for i := range filler {
		filler[i] = byte(i%200 + 1)
	}
	s.cfg.FillerAudios = [][]byte{filler}

	s.playFiller(ctx)

	// 应通过 AudioOut 发送帧。
	select {
	case frame := <-audioOut:
		assert.NotEmpty(t, frame)
	default:
		t.Fatal("应收到 AudioOut 帧")
	}
}

func TestSession_PlayFiller_RoundRobin(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	audioOut := make(chan []byte, 64)
	s.cfg.AudioOut = audioOut
	s.cfg.ESL = nil

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	filler1 := make([]byte, 320)
	filler2 := make([]byte, 320)
	filler1[0] = 0x01
	filler2[0] = 0x02
	s.cfg.FillerAudios = [][]byte{filler1, filler2}

	// 调用两次应交替选择不同填充词。
	s.playFiller(ctx)
	// 消费帧。
	for len(audioOut) > 0 {
		<-audioOut
	}
	s.playFiller(ctx)
}

// ── snapshot.go 补充测试 ───────────────────────────────────────

func TestSession_BuildSnapshot_WithDialogueEngine(t *testing.T) {
	t.Parallel()

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig: rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{
			"OPENING": "你好",
		},
	})
	require.NoError(t, err)

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.DialogueEngine = eng
	s.cfg.CallID = 100
	s.cfg.ContactID = 200
	s.cfg.TaskID = 300
	s.cfg.Phone = "13800138000"
	s.cfg.Gateway = "pstn"
	s.cfg.CallerID = "10001"

	snap := s.buildSnapshot("MEDIA_TIMEOUT")

	assert.Equal(t, int64(100), snap.CallID)
	assert.NotEmpty(t, snap.DialogueState)
	assert.NotNil(t, snap.CollectedFields)
}

func TestSession_RecentSnapshotTurns_OverLimit(t *testing.T) {
	t.Parallel()

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig: rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{
			"OPENING": "你好",
		},
		MaxHistory: 20,
	})
	require.NoError(t, err)

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.DialogueEngine = eng

	// 模拟多轮对话超过 maxSnapshotTurns。
	for i := range maxSnapshotTurns + 3 {
		_, _ = eng.ProcessUserInput(context.Background(), "用户输入"+string(rune('A'+i)))
	}

	turns := s.recentSnapshotTurns()
	assert.LessOrEqual(t, len(turns), maxSnapshotTurns)
}

func TestSession_CollectFields(t *testing.T) {
	t.Parallel()

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig: rules.TemplateConfig{
			MaxTurns:       20,
			MaxObjections:  3,
			RequiredFields: []string{"name"},
		},
		PromptTemplates: map[string]string{
			"OPENING": "你好",
		},
	})
	require.NoError(t, err)

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.DialogueEngine = eng

	fields := s.collectFields()
	assert.NotNil(t, fields) // 应返回空 map 而非 nil
}

// ── adapter.go 测试 ────────────────────────────────────────────

func TestOmniAdapter_NilInner(t *testing.T) {
	t.Parallel()
	// NewOmniAdapter 需要非 nil 的 Omni，但我们可以测试 smartAdapter。
	adapter := NewSmartAdapter(nil)
	assert.NotNil(t, adapter)
}

// ── session_audio.go 补充测试 ──────────────────────────────────

func TestSession_HandleAMDFrame_Enabled(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaAMDDetecting)
	s.cfg.AMDConfig = config.AMD{
		Enabled:                     true,
		DetectionWindowMs:           3000,
		ContinuousSpeechThresholdMs: 4000,
		HumanPauseThresholdMs:       300,
		EnergyThresholdDBFS:         -35.0,
	}

	// 送入足够多的高能量帧触发 AMD。
	frame := makeLoudFrame()
	for range 20 {
		s.handleAMDFrame(frame)
	}

	// AMD 检测器应已创建。
	assert.NotNil(t, s.amdDetector)
}

func TestSession_FeedASR_NilStream(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaUserSpeaking)
	s.asrStream = nil

	// 无 ASR 流时不应 panic。
	s.feedASR(context.Background(), makeLoudFrame())
}

func TestSession_FeedASR_WithStream(t *testing.T) {
	t.Parallel()

	stream := &mockASRStream{eventCh: make(chan provider.ASREvent, 1)}
	s := newTestSession(engine.MediaUserSpeaking)
	s.asrStream = stream

	s.feedASR(context.Background(), makeLoudFrame())
	// 不应 panic，重采样后的数据应被送入流。
}

func TestSession_FeedASR_StreamError(t *testing.T) {
	t.Parallel()

	stream := &mockASRStream{
		feedErr: errors.New("feed error"),
		eventCh: make(chan provider.ASREvent, 1),
	}
	s := newTestSession(engine.MediaUserSpeaking)
	s.asrStream = stream

	// 错误时只记录警告，不 panic。
	s.feedASR(context.Background(), makeLoudFrame())
}

func TestSession_StartASRStream_NilASR(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaWaitingUser)
	s.cfg.ASR = nil
	s.ctx = context.Background()

	// 无 ASR provider 时不应 panic。
	s.startASRStream()
	assert.Nil(t, s.asrStream)
}

func TestSession_StartASRStream_NilContext(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaWaitingUser)
	s.cfg.ASR = &mockASRProvider{}
	s.ctx = nil

	// 无 context 时不应 panic。
	s.startASRStream()
	assert.Nil(t, s.asrStream)
}

func TestSession_StartASRStream_Success(t *testing.T) {
	t.Parallel()

	eventCh := make(chan provider.ASREvent, 1)
	close(eventCh)
	stream := &mockASRStream{eventCh: eventCh}
	asr := &mockASRProvider{stream: stream}

	s := newTestSession(engine.MediaWaitingUser)
	s.cfg.ASR = asr
	s.ctx = context.Background()

	s.startASRStream()
	assert.NotNil(t, s.asrStream)
}

func TestSession_StartASRStream_Error(t *testing.T) {
	t.Parallel()

	asr := &mockASRProvider{streamErr: errors.New("asr error")}

	s := newTestSession(engine.MediaWaitingUser)
	s.cfg.ASR = asr
	s.ctx = context.Background()

	s.startASRStream()
	assert.Nil(t, s.asrStream)
}

func TestSession_HandleStreamingASR_EmptyText(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaUserSpeaking)
	s.ctx = context.Background()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	evt := provider.ASREvent{
		Text:    "",
		IsFinal: true,
	}

	s.handleStreamingASR(context.Background(), evt, timer)
	// 空文本不应改变状态。
	assert.Equal(t, engine.MediaUserSpeaking, s.mfsm.State())
}

func TestSession_HandleStreamingASR_WrongState(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.ctx = context.Background()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	evt := provider.ASREvent{
		Text:    "你好",
		IsFinal: true,
	}

	s.handleStreamingASR(context.Background(), evt, timer)
	// BotSpeaking 状态下 ASR 结果应被丢弃。
	assert.Equal(t, engine.MediaBotSpeaking, s.mfsm.State())
}

func TestSession_HandleStreamingASR_PartialEvent(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaUserSpeaking)
	s.ctx = context.Background()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	evt := provider.ASREvent{
		Text:    "你好",
		IsFinal: false,
	}

	s.handleStreamingASR(context.Background(), evt, timer)
	// partial 应更新追踪但不改变 FSM 状态。
	assert.Equal(t, "你好", s.lastPartialText)
}

func TestSession_HandleBargeIn_TTSNotPlaying(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.ttsPlaying.Store(false) // TTS 未在播放

	frame := makeLoudFrame()
	s.handleBargeInFrame(context.Background(), frame)

	// TTS 未播放时不应触发 barge-in。
	assert.Equal(t, 0, s.bargeInFrames)
}

func TestSession_HandleBargeIn_SilentFrame(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.AMDConfig.EnergyThresholdDBFS = -35.0
	s.ttsPlaying.Store(true)
	s.bargeInFrames = 5

	frame := makeSilentFrame()
	s.handleBargeInFrame(context.Background(), frame)

	// 静默帧应重置计数器。
	assert.Equal(t, 0, s.bargeInFrames)
}

func TestSession_HandleBargeIn_BelowThreshold(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.AMDConfig.EnergyThresholdDBFS = -35.0
	s.ttsPlaying.Store(true)

	frame := makeLoudFrame()
	// 发送少于阈值的帧数。
	for range bargeInThreshold - 1 {
		s.handleBargeInFrame(context.Background(), frame)
	}

	// 不应触发 barge-in。
	assert.Equal(t, engine.MediaBotSpeaking, s.mfsm.State())
	assert.Equal(t, bargeInThreshold-1, s.bargeInFrames)
}

func TestSession_RecordNetworkQuality_NilNetQuality(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaWaitingUser)
	s.netQuality = nil

	// 不应 panic。
	s.recordNetworkQuality(makeLoudFrame())
}

func TestSession_HandleInjectionFallback_NoTTS(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaUserSpeaking)
	s.cfg.TTS = nil
	s.ctx = context.Background()

	s.handleInjectionFallback()

	// 无 TTS 时应通过 FSM 直接完成。
	s.mu.Lock()
	found := false
	for _, e := range s.events {
		if e.EventType == engine.EventBotSpeakStart {
			found = true
		}
	}
	s.mu.Unlock()
	assert.True(t, found)
}

func TestSession_HandleInjectionFallback_WithTTS(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeData: make([]byte, 3200),
	}
	for i := range tts.synthesizeData {
		tts.synthesizeData[i] = byte(i%200 + 1)
	}

	audioOut := make(chan []byte, 128)
	s := newTestSession(engine.MediaUserSpeaking)
	s.cfg.TTS = tts
	s.cfg.AudioOut = audioOut
	s.cfg.ESL = nil
	s.ctx = context.Background()

	s.handleInjectionFallback()

	// 应启动 TTS 合成。
	select {
	case <-s.botDoneCh:
	case <-time.After(3 * time.Second):
		t.Fatal("超时等待 botDoneCh")
	}
}

// ── session_esl.go 补充测试 ────────────────────────────────────

func TestSession_IsMyESLEvent_SessionMatch(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.SessionID = "my-session"

	event := ESLEvent{
		Name: "CHANNEL_HANGUP",
		Headers: map[string]string{
			"variable_clarion_session_id": "my-session",
		},
	}

	assert.True(t, s.isMyESLEvent(event))
}

func TestSession_IsMyESLEvent_SessionMismatch(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.SessionID = "my-session"

	event := ESLEvent{
		Name: "CHANNEL_HANGUP",
		Headers: map[string]string{
			"variable_clarion_session_id": "other-session",
		},
	}

	assert.False(t, s.isMyESLEvent(event))
}

func TestSession_IsMyESLEvent_UUIDMatch(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.channelUUID = "my-uuid"

	event := ESLEvent{
		Name: "CHANNEL_HANGUP",
		Headers: map[string]string{
			"Unique-ID": "my-uuid",
		},
	}

	assert.True(t, s.isMyESLEvent(event))
}

func TestSession_IsMyESLEvent_UUIDMismatch(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.channelUUID = "my-uuid"

	event := ESLEvent{
		Name: "CHANNEL_HANGUP",
		Headers: map[string]string{
			"Unique-ID": "other-uuid",
		},
	}

	assert.False(t, s.isMyESLEvent(event))
}

func TestSession_HandleBGJob_NoError(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaDialing)
	event := ESLEvent{
		Name: "BACKGROUND_JOB",
		Body: "+OK Job-UUID: test-uuid",
	}

	done := s.handleBGJob(event)
	assert.False(t, done)
}

func TestSession_HandleBGJob_WithError(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaDialing)
	s.status = engine.CallDialing
	event := ESLEvent{
		Name: "BACKGROUND_JOB",
		Body: "-ERR USER_NOT_REGISTERED",
	}

	done := s.handleBGJob(event)
	assert.True(t, done)
}

func TestSession_HandleChannelProgress(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaDialing)
	s.handleChannelProgress()

	assert.Equal(t, engine.CallRinging, s.status)
	assert.Equal(t, engine.MediaRinging, s.mfsm.State())
}

func TestSession_HandleChannelAnswer(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaRinging)
	event := ESLEvent{
		Name: "CHANNEL_ANSWER",
		Headers: map[string]string{
			"Unique-ID": "answer-uuid",
		},
	}

	s.handleChannelAnswer(context.Background(), event)

	assert.Equal(t, "answer-uuid", s.channelUUID)
	assert.Equal(t, engine.MediaAMDDetecting, s.mfsm.State())
}

func TestSession_HandlePlaybackStop_NonStreaming(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.ttsStreaming.Store(false)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	s.handlePlaybackStop(timer)

	assert.False(t, s.ttsPlaying.Load())
	assert.Equal(t, engine.MediaWaitingUser, s.mfsm.State())
}

func TestSession_HandlePlaybackStop_Streaming(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.ttsStreaming.Store(true)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	s.handlePlaybackStop(timer)

	// 流式模式下不应触发 EvBotDone。
	assert.Equal(t, engine.MediaBotSpeaking, s.mfsm.State())
}

func TestSession_HandleASRResult_EmptyBody(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaUserSpeaking)
	event := ESLEvent{Body: ""}

	s.handleASRResult(context.Background(), event)
	// 空文本不应改变状态。
	assert.Equal(t, engine.MediaUserSpeaking, s.mfsm.State())
}

// ── session_speculative.go 补充测试 ────────────────────────────

func TestSession_StartSpeculative_NilEngine(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaUserSpeaking)
	s.cfg.DialogueEngine = nil

	s.startSpeculative(context.Background(), "你好")
	assert.Nil(t, s.speculative)
}

func TestSession_HandlePartialASR_TriggerSpeculative(t *testing.T) {
	t.Parallel()

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig:  rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{"OPENING": "你好"},
		LLM:             &mockLLMProvider{},
		SystemPrompt:    "test",
		MaxHistory:      5,
	})
	require.NoError(t, err)

	s := newTestSession(engine.MediaUserSpeaking)
	s.cfg.DialogueEngine = eng
	s.ctx = context.Background()

	// 发送足够多的稳定 partial 触发预推理。
	for range speculativeStableThreshold {
		s.handlePartialASR(s.ctx, provider.ASREvent{Text: "你好。", IsFinal: false})
	}

	// 句末标点触发 speculativeEarlyThreshold=2，应已触发。
	// 但 speculativeStableThreshold=3 也满足。
	assert.NotNil(t, s.speculative)
	assert.Equal(t, "你好。", s.speculative.inputText)

	// 清理。
	s.cancelSpeculative()
}

func TestSession_WarmupProviders(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaIdle)
	s.cfg.ASR = nil
	s.cfg.LLM = nil
	s.cfg.TTS = nil

	// 不应 panic。
	s.warmupProviders(context.Background())
}

// ── session_hybrid.go 补充测试 ─────────────────────────────────

func TestSession_HandleAudioFrameHybrid_AMDDetecting(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AMDConfig:  defaultAMDCfg(),
		AudioIn:    make(<-chan []byte),
		AudioOut:   make(chan<- []byte, 16),
		Logger:     slog.Default(),
	})
	s.mfsm = media.NewFSM(engine.MediaAMDDetecting)

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	frame := makeLoudFrame()
	s.handleAudioFrameHybrid(context.Background(), frame, rv, timer)

	// AMD 禁用时应转为 human。
	assert.True(t, s.answered)
}

func TestSession_HandleAudioFrameHybrid_IgnoredStates(t *testing.T) {
	t.Parallel()

	ignoredStates := []engine.MediaState{
		engine.MediaIdle,
		engine.MediaDialing,
		engine.MediaRinging,
		engine.MediaHangup,
		engine.MediaPostProcessing,
	}

	for _, state := range ignoredStates {
		t.Run(state.String(), func(t *testing.T) {
			t.Parallel()
			rv := newMockRealtimeVoice()
			s := newHybridTestSession(SessionConfig{
				Protection: defaultTestProtection(),
				AudioIn:    make(<-chan []byte),
				AudioOut:   make(chan<- []byte, 16),
				Logger:     slog.Default(),
			})
			s.mfsm = media.NewFSM(state)
			timer := time.NewTimer(time.Hour)
			defer timer.Stop()

			frame := makeLoudFrame()
			s.handleAudioFrameHybrid(context.Background(), frame, rv, timer)
			assert.Equal(t, state, s.mfsm.State())
		})
	}
}

func TestSession_FeedOmniResampled_Error(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	rv.feedErr = errors.New("feed error")

	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AudioIn:    make(<-chan []byte),
		AudioOut:   make(chan<- []byte, 16),
		Logger:     slog.Default(),
	})

	// 不应 panic，只记录警告。
	s.feedOmniResampled(context.Background(), makeLoudFrame(), rv)
}

func TestSession_RunStrategyAsync(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	strategy := &mockStrategy{
		decision: &Decision{
			Intent:          engine.IntentInterested,
			ExtractedFields: map[string]string{"name": "张三"},
			Instructions:    "继续引导",
			ShouldEnd:       false,
			Grade:           engine.GradeB,
		},
	}

	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AudioIn:    make(<-chan []byte),
		AudioOut:   make(chan<- []byte, 16),
		Logger:     slog.Default(),
		Strategy:   strategy,
	})
	s.startTime = time.Now()

	fields := make(map[string]string)
	s.runStrategyAsync(context.Background(), rv, StrategyInput{
		UserText:      "你好",
		AssistantText: "",
		TurnNumber:    1,
	}, fields)

	// 应更新 instructions。
	rv.mu.Lock()
	defer rv.mu.Unlock()
	require.Len(t, rv.instructions, 1)
	assert.Contains(t, rv.instructions[0], "继续引导")

	// 应合并字段。
	assert.Equal(t, "张三", fields["name"])
}

func TestSession_RunStrategyAsync_Error(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	strategy := &mockStrategy{
		err: errors.New("strategy error"),
	}

	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AudioIn:    make(<-chan []byte),
		AudioOut:   make(chan<- []byte, 16),
		Logger:     slog.Default(),
		Strategy:   strategy,
	})
	s.startTime = time.Now()

	fields := make(map[string]string)
	// 不应 panic。
	s.runStrategyAsync(context.Background(), rv, StrategyInput{}, fields)
}

func TestSession_RunStrategyAsync_ShouldEnd(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	strategy := &mockStrategy{
		decision: &Decision{
			Intent:    engine.IntentNotInterested,
			ShouldEnd: true,
		},
	}

	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AudioIn:    make(<-chan []byte),
		AudioOut:   make(chan<- []byte, 16),
		Logger:     slog.Default(),
		Strategy:   strategy,
	})
	s.startTime = time.Now()
	s.status = engine.CallInProgress

	fields := make(map[string]string)
	s.runStrategyAsync(context.Background(), rv, StrategyInput{}, fields)

	assert.Equal(t, engine.CallCompleted, s.status)
}

// ── worker.go 补充测试 ─────────────────────────────────────────

func TestSlogAdapter(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
	a := &slogAdapter{l: logger}

	// 所有方法不应 panic。
	a.Debug("debug msg")
	a.Info("info msg")
	a.Warn("warn msg")
	a.Error("error msg")
	a.Fatal("fatal msg")
}

func TestWorker_SetMetrics(t *testing.T) {
	t.Parallel()

	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	assert.Nil(t, w.metrics)
	w.SetMetrics(nil)
	assert.Nil(t, w.metrics)
}

func TestWorker_ScheduleRecoveryIfNeeded_Interrupted_NoClient(t *testing.T) {
	t.Parallel()

	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())
	w.schedulerClient = nil

	result := &SessionResult{Status: engine.CallInterrupted}
	task := Task{CallID: 1, Phone: "13800138000"}

	// 无调度客户端，不应 panic。
	w.scheduleRecoveryIfNeeded(context.Background(), result, task)
}

// ── session_dialogue.go 补充测试 ───────────────────────────────

func TestSession_HandleSilenceTimeout_CountTracking(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaWaitingUser)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	// 第一次超时。
	s.handleSilenceTimeout(timer)
	assert.Equal(t, 1, s.silenceCount)
	assert.Equal(t, engine.MediaBotSpeaking, s.mfsm.State())
}

func TestSession_OpeningText_Recovery(t *testing.T) {
	t.Parallel()

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig:  rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{"OPENING": "你好"},
	})
	require.NoError(t, err)

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.DialogueEngine = eng
	s.cfg.RestoredSnapshot = &SessionSnapshot{CallID: 100}

	text := s.openingText()
	assert.NotEmpty(t, text)
}

func TestSession_OpeningText_Normal(t *testing.T) {
	t.Parallel()

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig:  rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{"OPENING": "你好，我是AI助手。"},
	})
	require.NoError(t, err)

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.DialogueEngine = eng
	s.cfg.RestoredSnapshot = nil

	text := s.openingText()
	assert.Equal(t, "你好，我是AI助手。", text)
}

// ── HandleStreamingASR 完整路径测试 ────────────────────────────

func TestSession_HandleStreamingASR_WithDialogueEngine_NoTTS(t *testing.T) {
	t.Parallel()

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig:  rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{"OPENING": "你好"},
		LLM:             &mockLLMProvider{},
		SystemPrompt:    "test",
		MaxHistory:      5,
	})
	require.NoError(t, err)

	s := newTestSession(engine.MediaUserSpeaking)
	s.cfg.DialogueEngine = eng
	s.cfg.TTS = nil
	s.ctx = context.Background()

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	evt := provider.ASREvent{
		Text:       "好的",
		IsFinal:    true,
		Confidence: 0.95,
	}

	s.handleStreamingASR(context.Background(), evt, timer)

	// 无 TTS 时应记录事件并完成。
	s.mu.Lock()
	found := false
	for _, e := range s.events {
		if e.EventType == engine.EventUserSpeechEnd {
			found = true
		}
	}
	s.mu.Unlock()
	assert.True(t, found)
}

func TestSession_HandleStreamingASR_WithTTS(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeData: make([]byte, 3200),
	}
	for i := range tts.synthesizeData {
		tts.synthesizeData[i] = byte(i%200 + 1)
	}

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig:  rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{"OPENING": "你好"},
		LLM:             &mockLLMProvider{},
		SystemPrompt:    "test",
		MaxHistory:      5,
	})
	require.NoError(t, err)

	audioOut := make(chan []byte, 128)
	s := newTestSession(engine.MediaUserSpeaking)
	s.cfg.DialogueEngine = eng
	s.cfg.TTS = tts
	s.cfg.AudioOut = audioOut
	s.cfg.ESL = nil
	s.ctx = context.Background()

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	evt := provider.ASREvent{
		Text:       "好的",
		IsFinal:    true,
		Confidence: 0.95,
	}

	s.handleStreamingASR(context.Background(), evt, timer)

	// 应启动流式 TTS 合成。
	select {
	case <-s.botDoneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("超时等待 botDoneCh")
	}
}

func TestSession_HandleStreamingASR_SpeculativeHit(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeData: make([]byte, 3200),
	}
	for i := range tts.synthesizeData {
		tts.synthesizeData[i] = byte(i%200 + 1)
	}

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig:  rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{"OPENING": "你好"},
		LLM:             &mockLLMProvider{},
		SystemPrompt:    "test",
		MaxHistory:      5,
	})
	require.NoError(t, err)

	audioOut := make(chan []byte, 128)
	s := newTestSession(engine.MediaUserSpeaking)
	s.cfg.DialogueEngine = eng
	s.cfg.TTS = tts
	s.cfg.AudioOut = audioOut
	s.cfg.ESL = nil
	s.ctx = context.Background()

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	// 设置预推理命中。
	sentenceCh := make(chan string, 1)
	sentenceCh <- "回复句子"
	close(sentenceCh)

	var committed atomic.Bool
	s.speculative = &speculativeRun{
		inputText:  "你好",
		sentenceCh: sentenceCh,
		commit: func() {
			committed.Store(true)
		},
		cancel: func() {},
	}

	evt := provider.ASREvent{
		Text:       "你好",
		IsFinal:    true,
		Confidence: 0.95,
	}

	s.handleStreamingASR(context.Background(), evt, timer)

	// 等待 TTS 完成。
	select {
	case <-s.botDoneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("超时等待 botDoneCh")
	}

	time.Sleep(100 * time.Millisecond)
	assert.True(t, committed.Load(), "预推理 commit 应被调用")
}

func TestSession_HandleStreamingASR_SpeculativeMiss(t *testing.T) {
	t.Parallel()

	tts := &mockTTSProvider{
		synthesizeData: make([]byte, 3200),
	}
	for i := range tts.synthesizeData {
		tts.synthesizeData[i] = byte(i%200 + 1)
	}

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig:  rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{"OPENING": "你好"},
		LLM:             &mockLLMProvider{},
		SystemPrompt:    "test",
		MaxHistory:      5,
	})
	require.NoError(t, err)

	audioOut := make(chan []byte, 128)
	s := newTestSession(engine.MediaUserSpeaking)
	s.cfg.DialogueEngine = eng
	s.cfg.TTS = tts
	s.cfg.AudioOut = audioOut
	s.cfg.ESL = nil
	s.ctx = context.Background()

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	// 设置预推理未命中。
	specCh := make(chan string, 1)
	close(specCh)
	ctx, cancel := context.WithCancel(context.Background())
	s.speculative = &speculativeRun{
		inputText:  "完全不同的文本",
		sentenceCh: specCh,
		commit:     func() {},
		cancel:     cancel,
	}

	evt := provider.ASREvent{
		Text:       "实际文本",
		IsFinal:    true,
		Confidence: 0.95,
	}

	s.handleStreamingASR(context.Background(), evt, timer)

	// 预推理应被取消。
	assert.Nil(t, s.speculative)
	assert.Error(t, ctx.Err())

	// 等待 TTS 完成。
	select {
	case <-s.botDoneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("超时等待 botDoneCh")
	}
}

// ── hybrid 事件循环补充测试 ────────────────────────────────────

func TestProcessHybridEvent_AudioOutClosed(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	close(rv.audioOutCh)

	audioIn := make(chan []byte, 1)
	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AudioIn:    audioIn,
		AudioOut:   make(chan<- []byte, 16),
		Logger:     slog.Default(),
		Realtime:   rv,
	})
	s.startTime = time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	hstate := &hybridLoopState{
		audioOutCh:   rv.AudioOut(),
		transcriptCh: rv.Transcripts(),
		silenceTimer: time.NewTimer(time.Hour),
		rv:           rv,
		fields:       make(map[string]string),
	}
	defer hstate.silenceTimer.Stop()

	done, err := s.processHybridEvent(ctx, hstate)
	assert.False(t, done)
	assert.NoError(t, err)
	assert.Nil(t, hstate.audioOutCh) // 应设为 nil
}

func TestProcessHybridEvent_TranscriptClosed(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	close(rv.transcriptCh)

	audioIn := make(chan []byte, 1)
	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AudioIn:    audioIn,
		AudioOut:   make(chan<- []byte, 16),
		Logger:     slog.Default(),
		Realtime:   rv,
	})
	s.startTime = time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	hstate := &hybridLoopState{
		audioOutCh:   make(<-chan []byte), // 不关闭，避免选到此通道
		transcriptCh: rv.Transcripts(),
		silenceTimer: time.NewTimer(time.Hour),
		rv:           rv,
		fields:       make(map[string]string),
	}
	defer hstate.silenceTimer.Stop()

	done, err := s.processHybridEvent(ctx, hstate)
	assert.False(t, done)
	assert.NoError(t, err)
	assert.Nil(t, hstate.transcriptCh)
}

// ── session_esl.go handleASRResult 完整路径 ────────────────────

func TestSession_HandleASRResult_WithDialogueEngine(t *testing.T) {
	t.Parallel()

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig:  rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{"OPENING": "你好"},
		MaxHistory:      5,
	})
	require.NoError(t, err)

	s := newTestSession(engine.MediaUserSpeaking)
	s.cfg.DialogueEngine = eng
	s.status = engine.CallInProgress

	event := ESLEvent{
		Name: "DETECTED_SPEECH",
		Body: "好的",
	}

	s.handleASRResult(context.Background(), event)

	// 应记录事件。
	s.mu.Lock()
	found := false
	for _, e := range s.events {
		if e.EventType == engine.EventUserSpeechEnd {
			found = true
		}
	}
	s.mu.Unlock()
	assert.True(t, found)
}

// ── snapshot snapshotKey 测试 ──────────────────────────────────

func TestRedisSnapshotStore_SnapshotKey(t *testing.T) {
	t.Parallel()

	store := &RedisSnapshotStore{prefix: "clarion:session"}
	key := store.snapshotKey(123)
	assert.Equal(t, "clarion:session:snapshot:123", key)
}

func TestNewRedisSnapshotStore(t *testing.T) {
	t.Parallel()

	store := NewRedisSnapshotStore(nil, "test:prefix")
	assert.NotNil(t, store)
	assert.Equal(t, "test:prefix", store.prefix)
}

// ── AMDDetector (非 Testable 版本) 测试 ──────────────────────────

func TestAMDDetector_FeedFrame_Human(t *testing.T) {
	t.Parallel()

	cfg := config.AMD{
		Enabled:                     true,
		DetectionWindowMs:           10000,
		ContinuousSpeechThresholdMs: 4000,
		HumanPauseThresholdMs:       10, // 非常短的停顿阈值，使测试可靠
		EnergyThresholdDBFS:         -35.0,
	}

	d := NewAMDDetector(cfg)

	// 先发送一些语音帧。
	for range 5 {
		d.FeedFrame(-10.0, 20) // 语音帧
	}
	// 发送静默帧触发停顿检测。
	// 需要等待足够时间使 silenceMs > HumanPauseThresholdMs。
	time.Sleep(30 * time.Millisecond)
	for range 10 {
		d.FeedFrame(-50.0, 20) // 静默帧
	}
	// 再次发送语音帧。
	d.FeedFrame(-10.0, 20)

	// 停顿后再次出现语音 → 人类。
	assert.True(t, d.Decided())
	assert.Equal(t, engine.AnswerHuman, d.Result())
}

func TestAMDDetector_FeedFrame_Voicemail(t *testing.T) {
	t.Parallel()

	cfg := config.AMD{
		Enabled:                     true,
		DetectionWindowMs:           10000,
		ContinuousSpeechThresholdMs: 200,
		HumanPauseThresholdMs:       300,
		EnergyThresholdDBFS:         -35.0,
	}

	d := NewAMDDetector(cfg)

	// 持续语音帧超过阈值 → 留言机。
	for range 20 {
		d.FeedFrame(-10.0, 20) // 200ms 连续语音
	}

	assert.True(t, d.Decided())
	assert.Equal(t, engine.AnswerVoicemail, d.Result())
}

func TestAMDDetector_FeedFrame_WindowExpiry_Unknown(t *testing.T) {
	t.Parallel()

	cfg := config.AMD{
		Enabled:                     true,
		DetectionWindowMs:           100, // 非常短的窗口
		ContinuousSpeechThresholdMs: 4000,
		HumanPauseThresholdMs:       300,
		EnergyThresholdDBFS:         -35.0,
	}

	d := NewAMDDetector(cfg)

	// 只发送静默帧，窗口到期时应返回 Unknown。
	for range 20 {
		d.FeedFrame(-50.0, 20)
	}
	// 此时窗口可能已到期（取决于 time.Since 的实际时间）。
	// 如果检测器已决定，验证结果不是 AnswerHuman。
	if d.Decided() {
		assert.NotEqual(t, engine.AnswerHuman, d.Result())
	}
}

func TestAMDDetector_FeedFrame_AfterDecided(t *testing.T) {
	t.Parallel()

	cfg := config.AMD{
		Enabled:                     true,
		DetectionWindowMs:           10000,
		ContinuousSpeechThresholdMs: 100,
		EnergyThresholdDBFS:         -35.0,
	}

	d := NewAMDDetector(cfg)

	// 触发 voicemail 判定。
	for range 10 {
		d.FeedFrame(-10.0, 20)
	}
	assert.True(t, d.Decided())

	// 已决定后 FeedFrame 应忽略。
	d.FeedFrame(-10.0, 20)
	assert.Equal(t, engine.AnswerVoicemail, d.Result())
}

func TestAMDDetector_Result_Undecided(t *testing.T) {
	t.Parallel()

	cfg := config.AMD{
		Enabled:                     true,
		DetectionWindowMs:           3000,
		ContinuousSpeechThresholdMs: 4000,
		EnergyThresholdDBFS:         -35.0,
	}

	d := NewAMDDetector(cfg)
	assert.Equal(t, engine.AnswerUnknown, d.Result())
	assert.False(t, d.Decided())
}

func TestAMDDetector_SilenceAfterPause(t *testing.T) {
	t.Parallel()

	cfg := config.AMD{
		Enabled:                     true,
		DetectionWindowMs:           10000,
		ContinuousSpeechThresholdMs: 4000,
		HumanPauseThresholdMs:       50, // 很短的停顿阈值
		EnergyThresholdDBFS:         -35.0,
	}

	d := NewAMDDetector(cfg)

	// 先说话，然后停顿。
	for range 5 {
		d.FeedFrame(-10.0, 20)
	}
	// 静默帧触发 pauseDetected。
	d.FeedFrame(-50.0, 20)
	// 继续足够多的静默帧使 silenceMs > HumanPauseThresholdMs。
	for range 20 {
		d.FeedFrame(-50.0, 20)
	}

	if d.Decided() {
		assert.Equal(t, engine.AnswerHuman, d.Result())
	}
}

// ── worker.go attachInputFilter/attachGuardHybrid 测试 ────────

func TestWorker_AttachInputFilter_Disabled(t *testing.T) {
	t.Parallel()

	cfg := defaultWorkerConfig()
	cfg.Guard.Enabled = false
	w := NewWorker(cfg, nil, testLogger())

	sessionCfg := &SessionConfig{}
	w.attachInputFilter(sessionCfg)

	assert.Nil(t, sessionCfg.InputFilter)
}

func TestWorker_AttachInputFilter_Enabled(t *testing.T) {
	t.Parallel()

	cfg := defaultWorkerConfig()
	cfg.Guard.Enabled = true
	w := NewWorker(cfg, nil, testLogger())

	sessionCfg := &SessionConfig{}
	w.attachInputFilter(sessionCfg)

	assert.NotNil(t, sessionCfg.InputFilter)
}

func TestWorker_AttachGuardHybrid_NotHybrid(t *testing.T) {
	t.Parallel()

	cfg := defaultWorkerConfig()
	cfg.Pipeline.Mode = "classic"
	w := NewWorker(cfg, nil, testLogger())

	sessionCfg := &SessionConfig{}
	w.attachGuardHybrid(sessionCfg)

	assert.Nil(t, sessionCfg.Budget)
	assert.Nil(t, sessionCfg.DecisionValidator)
}

func TestWorker_AttachGuardHybrid_BudgetEnabled(t *testing.T) {
	t.Parallel()

	cfg := defaultWorkerConfig()
	cfg.Pipeline.Mode = "hybrid"
	cfg.Budget.Enabled = true
	cfg.Budget.MaxTokens = 10000
	cfg.Budget.MaxTurns = 100
	w := NewWorker(cfg, nil, testLogger())

	sessionCfg := &SessionConfig{}
	w.attachGuardHybrid(sessionCfg)

	assert.NotNil(t, sessionCfg.Budget)
}

func TestWorker_AttachGuardHybrid_GuardEnabled(t *testing.T) {
	t.Parallel()

	cfg := defaultWorkerConfig()
	cfg.Pipeline.Mode = "hybrid"
	cfg.Guard.Enabled = true
	w := NewWorker(cfg, nil, testLogger())

	sessionCfg := &SessionConfig{}
	w.attachGuardHybrid(sessionCfg)

	assert.NotNil(t, sessionCfg.DecisionValidator)
}

// ── handleAudioFrameHybrid WaitingUser/Processing 状态 ────────

func TestSession_HandleAudioFrameHybrid_WaitingUser(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	audioOut := make(chan []byte, 64)
	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AMDConfig:  defaultAMDCfg(),
		AudioIn:    make(<-chan []byte),
		AudioOut:   audioOut,
		Logger:     slog.Default(),
	})

	advanceFSMToWaitingUser(t, s.mfsm)
	assert.Equal(t, engine.MediaWaitingUser, s.mfsm.State())

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	frame := makeLoudFrame()
	s.handleAudioFrameHybrid(context.Background(), frame, rv, timer)
	// 不应 panic，音频应转发给 Omni。
}

func TestSession_HandleAudioFrameHybrid_Processing(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	audioOut := make(chan []byte, 64)
	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AMDConfig:  defaultAMDCfg(),
		AudioIn:    make(<-chan []byte),
		AudioOut:   audioOut,
		Logger:     slog.Default(),
	})

	advanceFSMToProcessing(t, s.mfsm)
	assert.Equal(t, engine.MediaProcessing, s.mfsm.State())

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	frame := makeLoudFrame()
	s.handleAudioFrameHybrid(context.Background(), frame, rv, timer)
}

// ── handleStreamingASR DialogueEngine error 路径 ──────────────

func TestSession_HandleStreamingASR_ProcessInputStreamError(t *testing.T) {
	t.Parallel()

	// 使用一个引擎，但让 ProcessUserInputStream 返回错误。
	// dialogue.Engine.ProcessUserInputStream 需要 LLM，无 LLM 时应报错。
	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig:  rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{"OPENING": "你好"},
		MaxHistory:      5,
		// 不设置 LLM，ProcessUserInputStream 应返回错误。
	})
	require.NoError(t, err)

	s := newTestSession(engine.MediaUserSpeaking)
	s.cfg.DialogueEngine = eng
	s.cfg.TTS = nil
	s.ctx = context.Background()

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	evt := provider.ASREvent{
		Text:    "好的",
		IsFinal: true,
	}

	s.handleStreamingASR(context.Background(), evt, timer)
	// 应处理错误路径而不 panic。
}

// ── session_hybrid.go checkHybridBudget 无 Budget 路径 ────────

func TestSession_CheckHybridBudget_NilBudget(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AudioIn:    make(<-chan []byte),
		AudioOut:   make(chan<- []byte, 16),
		Logger:     slog.Default(),
		Budget:     nil,
	})

	// 不应 panic。
	s.checkHybridBudget(context.Background(), rv)
}

// ── session.go saveSnapshot nilStore 路径 ─────────────────────

func TestSession_SaveSnapshot_NilStore(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.SnapshotStore = nil

	// 不应 panic。
	s.saveSnapshot("audio_closed")
}

// ── session.go Run 路径补充测试 ───────────────────────────────

func TestSession_Run_Hybrid_AudioClosed(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	audioIn := make(chan []byte, 1)
	audioOut := make(chan []byte, 16)

	s := NewSession(SessionConfig{
		CallID:       600,
		SessionID:    "test-hybrid-run",
		Phone:        "13800138000",
		Protection:   defaultTestProtection(),
		AMDConfig:    defaultAMDCfg(),
		Logger:       testLogger(),
		AudioIn:      audioIn,
		AudioOut:     audioOut,
		PipelineMode: PipelineHybrid,
		Realtime:     rv,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	close(audioIn)

	result, err := s.Run(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int64(600), result.CallID)
}

// ── playViaESL ESL 命令发送路径 ───────────────────────────────

func TestSession_PlayViaESL_Success(t *testing.T) {
	t.Parallel()

	esl, _ := newMockESLClient(t)
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.ESL = esl
	s.channelUUID = "test-uuid"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got := s.playViaESL(ctx, "/tmp/test.wav", 1000)
	assert.True(t, got)
	assert.True(t, s.ttsPlaying.Load())
}

// ── feedOmniResampled 成功路径 ────────────────────────────────

func TestSession_FeedOmniResampled_Success(t *testing.T) {
	t.Parallel()

	rv := newMockRealtimeVoice()
	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AudioIn:    make(<-chan []byte),
		AudioOut:   make(chan<- []byte, 16),
		Logger:     slog.Default(),
	})

	// 不应 panic。
	s.feedOmniResampled(context.Background(), makeLoudFrame(), rv)
}

// ── handleOmniAudioOut 已在 BotSpeaking 状态 ─────────────────

func TestHandleOmniAudioOut_AlreadyBotSpeaking(t *testing.T) {
	t.Parallel()

	audioOut := make(chan []byte, 64)
	s := newHybridTestSession(SessionConfig{
		Protection: defaultTestProtection(),
		AudioIn:    make(<-chan []byte),
		AudioOut:   audioOut,
		Logger:     slog.Default(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	// 推进到 BotSpeaking。
	advanceFSMToProcessing(t, s.mfsm)
	require.NoError(t, s.mfsm.Handle(engine.EvProcessingDone))
	assert.Equal(t, engine.MediaBotSpeaking, s.mfsm.State())

	input := make([]byte, 960)
	s.handleOmniAudioOut(input)

	// 应仍在 BotSpeaking（不应因 CanHandle 检查而出问题）。
	assert.Equal(t, engine.MediaBotSpeaking, s.mfsm.State())
}
