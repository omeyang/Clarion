package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	assert.Equal(t, ":8000", cfg.Server.Addr)
	assert.Equal(t, "info", cfg.Server.LogLevel)
	assert.False(t, cfg.Server.Debug)
	assert.Equal(t, 20, cfg.Database.MaxOpenConns)
	assert.Equal(t, "localhost:6379", cfg.Redis.Addr)
	assert.Equal(t, "qwen", cfg.ASR.Provider)
	assert.Equal(t, "deepseek", cfg.LLM.Provider)
	assert.Equal(t, "dashscope", cfg.TTS.Provider)
	assert.Equal(t, 2, cfg.TTS.PoolSize)
	assert.Equal(t, 5, cfg.Worker.MaxConcurrentCalls)
	assert.Equal(t, 300, cfg.CallProtection.MaxDurationSec)
	assert.True(t, cfg.AMD.Enabled)

	// LocalASR 默认值。
	assert.False(t, cfg.LocalASR.Enabled)
	assert.Equal(t, 1, cfg.LocalASR.NumThreads)

	// LocalTTS 默认值。
	assert.False(t, cfg.LocalTTS.Enabled)
	assert.Equal(t, 1, cfg.LocalTTS.NumThreads)
	assert.Equal(t, 10, cfg.LocalTTS.Threshold)
	assert.InDelta(t, 1.0, float64(cfg.LocalTTS.Speed), 0.01)

	// SileroVAD 默认值。
	assert.False(t, cfg.SileroVAD.Enabled)
	assert.InDelta(t, 0.5, float64(cfg.SileroVAD.Threshold), 0.01)
	assert.InDelta(t, 0.3, float64(cfg.SileroVAD.MinSilenceDuration), 0.01)
	assert.InDelta(t, 0.25, float64(cfg.SileroVAD.MinSpeechDuration), 0.01)
	assert.Equal(t, 16000, cfg.SileroVAD.SampleRate)
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, ":8000", cfg.Server.Addr)
}

func TestLoad_FromFile(t *testing.T) {
	content := `
[server]
addr = ":9000"
debug = true
log_level = "debug"

[database]
dsn = "postgres://test:test@localhost:5432/testdb?sslmode=disable"
max_open_conns = 10
max_idle_conns = 2

[redis]
addr = "localhost:6380"

[worker]
max_concurrent_calls = 10
`
	dir := t.TempDir()
	path := filepath.Join(dir, "clarion.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, ":9000", cfg.Server.Addr)
	assert.True(t, cfg.Server.Debug)
	assert.Equal(t, "debug", cfg.Server.LogLevel)
	assert.Equal(t, 10, cfg.Database.MaxOpenConns)
	assert.Equal(t, "localhost:6380", cfg.Redis.Addr)
	assert.Equal(t, 10, cfg.Worker.MaxConcurrentCalls)

	// Defaults still apply for unset sections.
	assert.Equal(t, "deepseek", cfg.LLM.Provider)
	assert.Equal(t, 300, cfg.CallProtection.MaxDurationSec)
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/clarion.toml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read config file")
}

func TestLoad_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	require.NoError(t, os.WriteFile(path, []byte("not [valid toml !!!"), 0o600))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read config file")
}

func TestLoad_ValidationError(t *testing.T) {
	content := `
[server]
addr = ""

[database]
dsn = "postgres://x@localhost/db"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.addr must not be empty")
}

func TestValidate_AllRules(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:    "空 server.addr",
			mutate:  func(c *Config) { c.Server.Addr = "" },
			wantErr: "server.addr must not be empty",
		},
		{
			name:    "空 database.dsn",
			mutate:  func(c *Config) { c.Database.DSN = "" },
			wantErr: "database.dsn must not be empty",
		},
		{
			name:    "空 redis.addr",
			mutate:  func(c *Config) { c.Redis.Addr = "" },
			wantErr: "redis.addr must not be empty",
		},
		{
			name:    "database.max_open_conns 为零",
			mutate:  func(c *Config) { c.Database.MaxOpenConns = 0 },
			wantErr: "database.max_open_conns must be positive",
		},
		{
			name:    "call_protection.max_duration_sec 为负",
			mutate:  func(c *Config) { c.CallProtection.MaxDurationSec = -1 },
			wantErr: "call_protection.max_duration_sec must be positive",
		},
		{
			name:    "worker.max_concurrent_calls 为零",
			mutate:  func(c *Config) { c.Worker.MaxConcurrentCalls = 0 },
			wantErr: "worker.max_concurrent_calls must be positive",
		},
		{
			name: "silero_vad 启用但缺少 model_path",
			mutate: func(c *Config) {
				c.SileroVAD.Enabled = true
				c.SileroVAD.ModelPath = ""
			},
			wantErr: "silero_vad.model_path must not be empty when silero_vad is enabled",
		},
		{
			name: "local_asr 启用但缺少 encoder_path",
			mutate: func(c *Config) {
				c.LocalASR.Enabled = true
				c.LocalASR.DecoderPath = "/model/decoder.onnx"
				c.LocalASR.TokensPath = "/model/tokens.txt"
			},
			wantErr: "local_asr.encoder_path must not be empty when local_asr is enabled",
		},
		{
			name: "local_asr 启用但缺少 decoder_path",
			mutate: func(c *Config) {
				c.LocalASR.Enabled = true
				c.LocalASR.EncoderPath = "/model/encoder.onnx"
				c.LocalASR.TokensPath = "/model/tokens.txt"
			},
			wantErr: "local_asr.decoder_path must not be empty when local_asr is enabled",
		},
		{
			name: "local_asr 启用但缺少 tokens_path",
			mutate: func(c *Config) {
				c.LocalASR.Enabled = true
				c.LocalASR.EncoderPath = "/model/encoder.onnx"
				c.LocalASR.DecoderPath = "/model/decoder.onnx"
			},
			wantErr: "local_asr.tokens_path must not be empty when local_asr is enabled",
		},
		{
			name: "local_tts 启用但缺少 model_path",
			mutate: func(c *Config) {
				c.LocalTTS.Enabled = true
				c.LocalTTS.ModelPath = ""
			},
			wantErr: "local_tts.model_path must not be empty when local_tts is enabled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)
			err := cfg.validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := Defaults()
	cfg.Server.Addr = ""
	cfg.Database.DSN = ""
	cfg.Redis.Addr = ""

	err := cfg.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.addr")
	assert.Contains(t, err.Error(), "database.dsn")
	assert.Contains(t, err.Error(), "redis.addr")
}

func TestValidate_Pass(t *testing.T) {
	cfg := validConfig()
	require.NoError(t, cfg.validate())
}

// validConfig 返回一个能通过所有校验的配置。
func validConfig() Config {
	cfg := Defaults()
	cfg.Database.DSN = "postgres://user:pass@localhost:5432/testdb"
	cfg.Redis.Addr = "localhost:6379"
	return cfg
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("CLARION_LLM_API_KEY", "sk-test-key")
	t.Setenv("CLARION_DATABASE_MAX_OPEN_CONNS", "50")
	t.Setenv("CLARION_SERVER_DEBUG", "true")
	t.Setenv("CLARION_AMD_ENERGY_THRESHOLD_DBFS", "-40.5")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, "sk-test-key", cfg.LLM.APIKey)
	assert.Equal(t, 50, cfg.Database.MaxOpenConns)
	assert.True(t, cfg.Server.Debug)
	assert.InDelta(t, -40.5, cfg.AMD.EnergyThresholdDBFS, 0.01)
}

func TestEnvOverrides_Priority(t *testing.T) {
	content := `
[llm]
api_key = "from-file"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "clarion.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	t.Setenv("CLARION_LLM_API_KEY", "from-env")

	cfg, err := Load(path)
	require.NoError(t, err)

	// Env var overrides file value.
	assert.Equal(t, "from-env", cfg.LLM.APIKey)
}

func TestLoad_SileroVAD_FromFile(t *testing.T) {
	content := `
[silero_vad]
enabled = true
model_path = "/opt/models/silero_vad.onnx"
threshold = 0.45
min_silence_duration = 0.2
`
	dir := t.TempDir()
	path := filepath.Join(dir, "clarion.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.SileroVAD.Enabled)
	assert.Equal(t, "/opt/models/silero_vad.onnx", cfg.SileroVAD.ModelPath)
	assert.InDelta(t, 0.45, float64(cfg.SileroVAD.Threshold), 0.01)
	assert.InDelta(t, 0.2, float64(cfg.SileroVAD.MinSilenceDuration), 0.01)
	// 未设置的字段保留默认值。
	assert.InDelta(t, 0.25, float64(cfg.SileroVAD.MinSpeechDuration), 0.01)
	assert.Equal(t, 16000, cfg.SileroVAD.SampleRate)
}

func TestEnvOverrides_SileroVAD(t *testing.T) {
	t.Setenv("CLARION_SILERO_VAD_ENABLED", "true")
	t.Setenv("CLARION_SILERO_VAD_MODEL_PATH", "/tmp/vad.onnx")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.True(t, cfg.SileroVAD.Enabled)
	assert.Equal(t, "/tmp/vad.onnx", cfg.SileroVAD.ModelPath)
}

func TestConfig_String_MasksSensitive(t *testing.T) {
	cfg := Defaults()
	cfg.Database.DSN = "postgres://user:secret@localhost:5432/db"

	s := cfg.String()
	assert.NotContains(t, s, "secret")
	assert.Contains(t, s, "***@localhost:5432/db")
}

func TestMaskDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"with credentials", "postgres://user:pass@host/db", "***@host/db"},
		{"no at sign", "host/db", "***"},
		{"empty", "", "***"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, maskDSN(tt.dsn))
		})
	}
}
