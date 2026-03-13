package precompile

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/provider"
)

// mockTTS implements provider.TTSProvider for testing.
type mockTTS struct {
	synthesizeFn func(ctx context.Context, text string, cfg provider.TTSConfig) ([]byte, error)
}

func (m *mockTTS) SynthesizeStream(_ context.Context, _ <-chan string, _ provider.TTSConfig) (<-chan []byte, error) {
	return nil, errors.New("not implemented")
}

func (m *mockTTS) Synthesize(ctx context.Context, text string, cfg provider.TTSConfig) ([]byte, error) {
	if m.synthesizeFn != nil {
		return m.synthesizeFn(ctx, text, cfg)
	}
	return []byte("audio:" + text), nil
}

func (m *mockTTS) Cancel() error { return nil }

func TestSynthesizer_PrecompileAudios(t *testing.T) {
	tests := []struct {
		name    string
		audios  map[string]string
		tts     *mockTTS
		wantLen int
		wantErr bool
	}{
		{
			name:    "empty map",
			audios:  map[string]string{},
			tts:     &mockTTS{},
			wantLen: 0,
		},
		{
			name: "single audio",
			audios: map[string]string{
				"greeting": "你好",
			},
			tts:     &mockTTS{},
			wantLen: 1,
		},
		{
			name: "multiple audios",
			audios: map[string]string{
				"greeting": "你好",
				"farewell": "再见",
			},
			tts:     &mockTTS{},
			wantLen: 2,
		},
		{
			name: "TTS error",
			audios: map[string]string{
				"greeting": "你好",
			},
			tts: &mockTTS{synthesizeFn: func(_ context.Context, _ string, _ provider.TTSConfig) ([]byte, error) {
				return nil, errors.New("tts error")
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSynthesizer(tt.tts, slog.Default())
			results, err := s.PrecompileAudios(context.Background(), tt.audios)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, results, tt.wantLen)

			for name := range tt.audios {
				data, ok := results[name]
				assert.True(t, ok, "missing result for %s", name)
				assert.NotEmpty(t, data)
			}
		})
	}
}

func TestSynthesizer_PrecompileAudios_ContextCancellation(t *testing.T) {
	tts := &mockTTS{synthesizeFn: func(ctx context.Context, _ string, _ provider.TTSConfig) ([]byte, error) {
		return nil, ctx.Err()
	}}

	s := NewSynthesizer(tts, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.PrecompileAudios(ctx, map[string]string{"test": "hello"})
	require.Error(t, err)
}
