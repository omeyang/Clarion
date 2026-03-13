// Package config 加载和验证统一的 TOML 配置。
//
// 优先级：默认值 → clarion.toml → 环境变量 (CLARION_{SECTION}_{KEY})。
// 配置在启动时加载一次，运行时不可变。
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// Config 是顶层配置结构体。
// 所有节与 clarion.toml 中的 [section] 块一一对应。
type Config struct {
	Server         Server         `toml:"server"          koanf:"server"`
	Database       Database       `toml:"database"        koanf:"database"`
	Redis          Redis          `toml:"redis"           koanf:"redis"`
	ASR            ASR            `toml:"asr"             koanf:"asr"`
	LLM            LLM            `toml:"llm"             koanf:"llm"`
	TTS            TTS            `toml:"tts"             koanf:"tts"`
	FreeSWITCH     FreeSWITCH     `toml:"freeswitch"      koanf:"freeswitch"`
	CallProtection CallProtection `toml:"call_protection"  koanf:"call_protection"`
	AMD            AMD            `toml:"amd"             koanf:"amd"`
	OSS            OSS            `toml:"oss"             koanf:"oss"`
	Worker         Worker         `toml:"worker"          koanf:"worker"`
	Notification   Notification   `toml:"notification"    koanf:"notification"`
	PostProcessor  PostProcessor  `toml:"postprocessor"   koanf:"postprocessor"`
	Pipeline       Pipeline       `toml:"pipeline"        koanf:"pipeline"`
	Realtime       Realtime       `toml:"realtime"        koanf:"realtime"`
	SmartLLM       SmartLLM       `toml:"smart_llm"       koanf:"smart_llm"`
	Budget         Budget         `toml:"budget"          koanf:"budget"`
	Guard          Guard          `toml:"guard"           koanf:"guard"`
	OffTopic       OffTopic       `toml:"off_topic"       koanf:"off_topic"`
	LocalASR       LocalASR       `toml:"local_asr"       koanf:"local_asr"`
	LocalTTS       LocalTTS       `toml:"local_tts"       koanf:"local_tts"`
	Scheduler      Scheduler      `toml:"scheduler"       koanf:"scheduler"`
	Snapshot       Snapshot       `toml:"snapshot"        koanf:"snapshot"`
	SileroVAD      SileroVAD      `toml:"silero_vad"      koanf:"silero_vad"`
	Observe        Observe        `toml:"observe"         koanf:"observe"`
}

// Observe 配置可观测性功能。
type Observe struct {
	// EBPF 配置 eBPF 内核级观测（可选，需要 Linux ≥ 5.8 + CAP_BPF）。
	EBPF EBPFConfig `toml:"ebpf" koanf:"ebpf"`
}

// EBPFConfig 配置 eBPF 观测探针。
type EBPFConfig struct {
	// Enabled 是否启用 eBPF 观测（默认 false，需要 CAP_BPF + CAP_PERFMON）。
	Enabled bool `toml:"enabled" koanf:"enabled"`
	// TCPTrace 是否启用 TCP 延迟追踪（追踪连接延迟和重传）。
	TCPTrace bool `toml:"tcp_trace" koanf:"tcp_trace"`
	// SchedLatency 是否启用 Go 调度延迟观测（实验性）。
	SchedLatency bool `toml:"sched_latency" koanf:"sched_latency"`
}

// Server 配置 HTTP API 服务器。
type Server struct {
	Addr     string `toml:"addr"      koanf:"addr"`
	Debug    bool   `toml:"debug"     koanf:"debug"`
	LogLevel string `toml:"log_level" koanf:"log_level"`
}

// Database 配置 PostgreSQL 连接池。
type Database struct {
	DSN          string `toml:"dsn"            koanf:"dsn"`
	MaxOpenConns int    `toml:"max_open_conns" koanf:"max_open_conns"`
	MaxIdleConns int    `toml:"max_idle_conns" koanf:"max_idle_conns"`
}

// Redis 配置 Redis 连接和队列键。
type Redis struct {
	Addr           string `toml:"addr"             koanf:"addr"`
	Password       string `toml:"password"         koanf:"password"`
	DB             int    `toml:"db"               koanf:"db"`
	TaskQueueKey   string `toml:"task_queue_key"   koanf:"task_queue_key"`
	EventStreamKey string `toml:"event_stream_key" koanf:"event_stream_key"`
	SessionPrefix  string `toml:"session_prefix"   koanf:"session_prefix"`
}

// ASR 配置自动语音识别提供者。
type ASR struct {
	Provider   string `toml:"provider"    koanf:"provider"`
	APIKey     string `toml:"api_key"     koanf:"api_key"`
	Model      string `toml:"model"       koanf:"model"`
	SampleRate int    `toml:"sample_rate" koanf:"sample_rate"`
}

// LLM 配置大语言模型提供者。
type LLM struct {
	Provider    string  `toml:"provider"    koanf:"provider"`
	APIKey      string  `toml:"api_key"     koanf:"api_key"`
	BaseURL     string  `toml:"base_url"    koanf:"base_url"`
	Model       string  `toml:"model"       koanf:"model"`
	MaxTokens   int     `toml:"max_tokens"  koanf:"max_tokens"`
	Temperature float64 `toml:"temperature" koanf:"temperature"`
	TimeoutMs   int     `toml:"timeout_ms"  koanf:"timeout_ms"`
}

// TTS 配置文本转语音提供者。
type TTS struct {
	Provider   string `toml:"provider"    koanf:"provider"`
	APIKey     string `toml:"api_key"     koanf:"api_key"`
	Model      string `toml:"model"       koanf:"model"`
	Voice      string `toml:"voice"       koanf:"voice"`
	SampleRate int    `toml:"sample_rate" koanf:"sample_rate"`
	// PoolSize WebSocket 连接池大小。启用后预建连接减少建连延迟（约 100ms）。
	// 0 表示禁用连接池（默认），推荐值 2。
	PoolSize int `toml:"pool_size" koanf:"pool_size"`
}

// FreeSWITCH 配置 ESL 连接和音频 WebSocket。
type FreeSWITCH struct {
	ESLHost     string `toml:"esl_host"      koanf:"esl_host"`
	ESLPort     int    `toml:"esl_port"      koanf:"esl_port"`
	ESLPassword string `toml:"esl_password"  koanf:"esl_password"`
	AudioWSAddr string `toml:"audio_ws_addr" koanf:"audio_ws_addr"`
	// AudioWSHost 是 FreeSWITCH 用于回连 Call Worker WebSocket 的地址。
	// 当 FreeSWITCH 在容器中运行时，通常设为 "host.docker.internal"。
	AudioWSHost string `toml:"audio_ws_host" koanf:"audio_ws_host"`
	// SIPDomain 是 FreeSWITCH 的 SIP 域名（local_ip_v4），
	// 用于 user/ 端点呼叫本地注册的 SIP 用户。
	SIPDomain string `toml:"sip_domain" koanf:"sip_domain"`
}

// CallProtection 配置外呼安全限制。
type CallProtection struct {
	MaxDurationSec         int `toml:"max_duration_sec"          koanf:"max_duration_sec"`
	MaxSilenceSec          int `toml:"max_silence_sec"           koanf:"max_silence_sec"`
	RingTimeoutSec         int `toml:"ring_timeout_sec"          koanf:"ring_timeout_sec"`
	FirstSilenceTimeoutSec int `toml:"first_silence_timeout_sec" koanf:"first_silence_timeout_sec"`
	MaxASRRetries          int `toml:"max_asr_retries"           koanf:"max_asr_retries"`
	MaxConsecutiveErrors   int `toml:"max_consecutive_errors"    koanf:"max_consecutive_errors"`
	MaxTurns               int `toml:"max_turns"                 koanf:"max_turns"`
}

// AMD 配置留言机检测参数。
type AMD struct {
	Enabled                     bool    `toml:"enabled"                        koanf:"enabled"`
	DetectionWindowMs           int     `toml:"detection_window_ms"            koanf:"detection_window_ms"`
	ContinuousSpeechThresholdMs int     `toml:"continuous_speech_threshold_ms" koanf:"continuous_speech_threshold_ms"`
	HumanPauseThresholdMs       int     `toml:"human_pause_threshold_ms"       koanf:"human_pause_threshold_ms"`
	EnergyThresholdDBFS         float64 `toml:"energy_threshold_dbfs"          koanf:"energy_threshold_dbfs"`
}

// OSS 配置阿里云对象存储服务。
type OSS struct {
	Enabled         bool   `toml:"enabled"           koanf:"enabled"`
	Endpoint        string `toml:"endpoint"          koanf:"endpoint"`
	Bucket          string `toml:"bucket"            koanf:"bucket"`
	AccessKeyID     string `toml:"access_key_id"     koanf:"access_key_id"`
	AccessKeySecret string `toml:"access_key_secret" koanf:"access_key_secret"`
}

// Worker 配置呼叫工作进程并发。
type Worker struct {
	MaxConcurrentCalls int `toml:"max_concurrent_calls" koanf:"max_concurrent_calls"`
}

// Notification 配置跟进通知设置。
type Notification struct {
	WeChatWebhookURL string `toml:"wechat_webhook_url" koanf:"wechat_webhook_url"`
	Enabled          bool   `toml:"enabled"            koanf:"enabled"`
}

// PostProcessor 配置后处理工作进程。
type PostProcessor struct {
	ConsumerGroup string `toml:"consumer_group"  koanf:"consumer_group"`
	ConsumerName  string `toml:"consumer_name"   koanf:"consumer_name"`
	BatchSize     int64  `toml:"batch_size"      koanf:"batch_size"`
	BlockMs       int64  `toml:"block_ms"        koanf:"block_ms"`
	AudioCacheDir string `toml:"audio_cache_dir" koanf:"audio_cache_dir"`
}

// Pipeline 配置通话管线模式。
type Pipeline struct {
	// Mode 指定管线模式："classic"（ASR→LLM→TTS 串行）或 "hybrid"（Omni 实时 + Smart LLM 异步决策）。
	Mode string `toml:"mode" koanf:"mode"`
}

// Realtime 配置实时全模态语音提供者（hybrid 模式下使用）。
type Realtime struct {
	Provider          string  `toml:"provider"            koanf:"provider"`
	APIKey            string  `toml:"api_key"             koanf:"api_key"`
	Model             string  `toml:"model"               koanf:"model"`
	Voice             string  `toml:"voice"               koanf:"voice"`
	Instructions      string  `toml:"instructions"        koanf:"instructions"`
	OutputSampleRate  int     `toml:"output_sample_rate"  koanf:"output_sample_rate"`
	VADEnabled        bool    `toml:"vad_enabled"         koanf:"vad_enabled"`
	VADThreshold      float64 `toml:"vad_threshold"       koanf:"vad_threshold"`
	SilenceDurationMs int     `toml:"silence_duration_ms" koanf:"silence_duration_ms"`
}

// SmartLLM 配置异步业务决策 LLM（hybrid 模式下使用）。
type SmartLLM struct {
	Provider    string  `toml:"provider"    koanf:"provider"`
	APIKey      string  `toml:"api_key"     koanf:"api_key"`
	BaseURL     string  `toml:"base_url"    koanf:"base_url"`
	Model       string  `toml:"model"       koanf:"model"`
	MaxTokens   int     `toml:"max_tokens"  koanf:"max_tokens"`
	Temperature float64 `toml:"temperature" koanf:"temperature"`
}

// Budget 配置单通电话的成本预算控制。
type Budget struct {
	// Enabled 是否启用预算控制。
	Enabled bool `toml:"enabled" koanf:"enabled"`
	// MaxTokens 整通电话 token 上限（输入+输出合计）。
	MaxTokens int `toml:"max_tokens" koanf:"max_tokens"`
	// MaxTurns 最大对话轮次。
	MaxTurns int `toml:"max_turns" koanf:"max_turns"`
	// MaxDurationSec 最长通话时长（秒）。
	MaxDurationSec int `toml:"max_duration_sec" koanf:"max_duration_sec"`
	// DegradeThreshold 降级阈值比例（0-1），达到此比例后切换为模板回复。
	DegradeThreshold float64 `toml:"degrade_threshold" koanf:"degrade_threshold"`
}

// Duration 返回 MaxDurationSec 对应的 time.Duration。
func (b Budget) Duration() time.Duration {
	return time.Duration(b.MaxDurationSec) * time.Second
}

// Scheduler 配置 Asynq 任务调度器。
type Scheduler struct {
	// Queue 是外呼任务队列名称。
	Queue string `toml:"queue" koanf:"queue"`
	// Retry 配置重试策略。
	Retry Retry `toml:"retry" koanf:"retry"`
}

// Retry 配置呼叫重试策略。
type Retry struct {
	// StartHour 允许重试的每日起始小时（含），默认 9。
	StartHour int `toml:"start_hour" koanf:"start_hour"`
	// EndHour 允许重试的每日截止小时（不含），默认 20。
	EndHour int `toml:"end_hour" koanf:"end_hour"`
	// WeekdaysOnly 是否仅在工作日重试，默认 true。
	WeekdaysOnly bool `toml:"weekdays_only" koanf:"weekdays_only"`
	// MinIntervalMin 同一号码两次呼叫的最小间隔（分钟），默认 120。
	MinIntervalMin int `toml:"min_interval_minutes" koanf:"min_interval_minutes"`
}

// MinInterval 返回 MinIntervalMin 对应的 time.Duration。
func (r Retry) MinInterval() time.Duration {
	return time.Duration(r.MinIntervalMin) * time.Minute
}

// Snapshot 配置会话快照（意外中断恢复）。
type Snapshot struct {
	// TTLSec 快照在 Redis 中的过期时间（秒），默认 600（10 分钟）。
	TTLSec int `toml:"ttl_sec" koanf:"ttl_sec"`
}

// TTL 返回 TTLSec 对应的 time.Duration。
func (s Snapshot) TTL() time.Duration {
	return time.Duration(s.TTLSec) * time.Second
}

// LocalASR 配置本地 ASR（sherpa-onnx Paraformer）。
// 启用后与云端 ASR 组成 RacingASR 竞速模式。
type LocalASR struct {
	// Enabled 是否启用本地 ASR。
	Enabled bool `toml:"enabled" koanf:"enabled"`
	// EncoderPath Paraformer 编码器 ONNX 模型路径。
	EncoderPath string `toml:"encoder_path" koanf:"encoder_path"`
	// DecoderPath Paraformer 解码器 ONNX 模型路径。
	DecoderPath string `toml:"decoder_path" koanf:"decoder_path"`
	// TokensPath tokens 文件路径。
	TokensPath string `toml:"tokens_path" koanf:"tokens_path"`
	// NumThreads 推理线程数，默认 1。
	NumThreads int `toml:"num_threads" koanf:"num_threads"`
}

// LocalTTS 配置本地 TTS（sherpa-onnx VITS）。
// 启用后与云端 TTS 组成 TieredTTS 分层模式（短文本本地、长文本云端）。
type LocalTTS struct {
	// Enabled 是否启用本地 TTS。
	Enabled bool `toml:"enabled" koanf:"enabled"`
	// ModelPath VITS 模型 ONNX 文件路径。
	ModelPath string `toml:"model_path" koanf:"model_path"`
	// TokensPath tokens 文件路径。
	TokensPath string `toml:"tokens_path" koanf:"tokens_path"`
	// DataDir 模型数据目录（如 espeak-ng-data）。
	DataDir string `toml:"data_dir" koanf:"data_dir"`
	// DictDir 词典目录路径（可选）。
	DictDir string `toml:"dict_dir" koanf:"dict_dir"`
	// LexiconPath 词典文件路径（可选）。
	LexiconPath string `toml:"lexicon_path" koanf:"lexicon_path"`
	// RuleFsts 文本规范化 FST 路径（可选）。
	RuleFsts string `toml:"rule_fsts" koanf:"rule_fsts"`
	// RuleFars 文本规范化 FAR 路径（可选）。
	RuleFars string `toml:"rule_fars" koanf:"rule_fars"`
	// NumThreads 推理线程数，默认 1。
	NumThreads int `toml:"num_threads" koanf:"num_threads"`
	// SpeakerID 说话人 ID，多说话人模型中使用，默认 0。
	SpeakerID int `toml:"speaker_id" koanf:"speaker_id"`
	// Speed 语速倍率，默认 1.0。
	Speed float32 `toml:"speed" koanf:"speed"`
	// Threshold 文本字符数阈值，不超过此值走本地 TTS，超过走云端。默认 10。
	Threshold int `toml:"threshold" koanf:"threshold"`
}

// SileroVAD 配置 Silero VAD（ML 级语音活动检测）。
// 启用后替代 WebRTC VAD，对复杂环境（背景音乐、多人、街道噪音）判断更精准。
type SileroVAD struct {
	// Enabled 是否启用 Silero VAD。
	Enabled bool `toml:"enabled" koanf:"enabled"`
	// ModelPath silero_vad.onnx 模型文件路径。
	ModelPath string `toml:"model_path" koanf:"model_path"`
	// Threshold 语音检测概率阈值，范围 (0, 1)，默认 0.5。
	Threshold float32 `toml:"threshold" koanf:"threshold"`
	// MinSilenceDuration 标记语音结束所需的最小静默时长（秒），默认 0.3。
	// 电话场景推荐 0.3，比默认值 0.5 更灵敏。
	MinSilenceDuration float32 `toml:"min_silence_duration" koanf:"min_silence_duration"`
	// MinSpeechDuration 最短语音段时长（秒），更短的会被忽略，默认 0.25。
	MinSpeechDuration float32 `toml:"min_speech_duration" koanf:"min_speech_duration"`
	// SampleRate 音频采样率（Hz），Silero VAD 要求 16000。
	SampleRate int `toml:"sample_rate" koanf:"sample_rate"`
}

// OffTopic 配置离题检测与收束。
type OffTopic struct {
	// Enabled 是否启用离题检测。
	Enabled bool `toml:"enabled" koanf:"enabled"`
	// ConvergeAfter 连续离题达到此轮次后触发收束。0 使用默认值（2）。
	ConvergeAfter int `toml:"converge_after" koanf:"converge_after"`
	// EndAfter 连续离题达到此轮次后触发结束。0 使用默认值（4）。
	EndAfter int `toml:"end_after" koanf:"end_after"`
}

// Guard 配置对话安全防护（输入过滤 + 输出校验）。
type Guard struct {
	// Enabled 是否启用输入过滤。
	Enabled bool `toml:"enabled" koanf:"enabled"`
	// MaxInputRunes 用户输入最大字符数，超出截断。0 使用默认值（500）。
	MaxInputRunes int `toml:"max_input_runes" koanf:"max_input_runes"`
	// ExtraPatterns 额外的注入检测正则表达式（叠加内置规则）。
	ExtraPatterns []string `toml:"extra_patterns" koanf:"extra_patterns"`
	// MaxResponseRunes 单条 LLM 响应最大字符数，超出截断。0 使用默认值（100）。
	MaxResponseRunes int `toml:"max_response_runes" koanf:"max_response_runes"`
	// ExtraAIPatterns 额外的 AI 身份泄露检测正则（叠加内置规则）。
	ExtraAIPatterns []string `toml:"extra_ai_patterns" koanf:"extra_ai_patterns"`
	// ExtraLeakPatterns 额外的系统提示泄露检测正则（叠加内置规则）。
	ExtraLeakPatterns []string `toml:"extra_leak_patterns" koanf:"extra_leak_patterns"`
}

// PipelineClassic 是经典 ASR→LLM→TTS 串行管线模式。
const PipelineClassic = "classic"

// PipelineHybrid 是 Omni 实时 + Smart LLM 异步决策混合模式。
const PipelineHybrid = "hybrid"

// Defaults 返回具有合理默认值的 Config。
func Defaults() Config {
	cfg := Config{
		Server: Server{
			Addr:     ":8000",
			Debug:    false,
			LogLevel: "info",
		},
		Database: Database{
			DSN:          "postgres://clarion:clarion@localhost:5432/clarion?sslmode=disable",
			MaxOpenConns: 20,
			MaxIdleConns: 5,
		},
		Redis: Redis{
			Addr:           "localhost:6379",
			DB:             0,
			TaskQueueKey:   "clarion:task_queue",
			EventStreamKey: "clarion:call_completed",
			SessionPrefix:  "clarion:session",
		},
		ASR: ASR{
			Provider:   "qwen",
			Model:      "qwen3-asr-flash-realtime",
			SampleRate: 16000,
		},
		LLM: LLM{
			Provider:    "deepseek",
			BaseURL:     "https://api.deepseek.com",
			Model:       "deepseek-chat",
			MaxTokens:   512,
			Temperature: 0.7,
			TimeoutMs:   5000,
		},
		TTS: TTS{
			Provider:   "dashscope",
			Model:      "cosyvoice-v3.5-plus",
			Voice:      "longanyang",
			SampleRate: 16000,
			PoolSize:   2,
		},
		FreeSWITCH: FreeSWITCH{
			ESLHost:     "127.0.0.1",
			ESLPort:     8021,
			ESLPassword: "ClueCon",
			AudioWSAddr: ":8765",
			AudioWSHost: "127.0.0.1",
		},
	}
	cfg.setCallDefaults()
	return cfg
}

// setCallDefaults 设置通话相关配置的默认值。
func (c *Config) setCallDefaults() {
	c.CallProtection = CallProtection{
		MaxDurationSec:         300,
		MaxSilenceSec:          15,
		RingTimeoutSec:         30,
		FirstSilenceTimeoutSec: 6,
		MaxASRRetries:          2,
		MaxConsecutiveErrors:   3,
		MaxTurns:               20,
	}
	c.AMD = AMD{
		Enabled:                     true,
		DetectionWindowMs:           3000,
		ContinuousSpeechThresholdMs: 4000,
		HumanPauseThresholdMs:       300,
		EnergyThresholdDBFS:         -35.0,
	}
	c.OSS = OSS{Enabled: false, Bucket: "clarion-recordings"}
	c.Worker = Worker{MaxConcurrentCalls: 5}
	c.Notification = Notification{Enabled: false}
	c.PostProcessor = PostProcessor{
		ConsumerGroup: "clarion-postprocessor",
		ConsumerName:  "worker-1",
		BatchSize:     10,
		BlockMs:       5000,
		AudioCacheDir: "/tmp/clarion-audio-cache",
	}
	c.Pipeline = Pipeline{Mode: PipelineClassic}
	c.Realtime = Realtime{
		Provider:          "omni",
		Model:             "qwen3-omni-flash-realtime",
		Voice:             "Cherry",
		OutputSampleRate:  24000,
		VADEnabled:        true,
		VADThreshold:      0.5,
		SilenceDurationMs: 500,
	}
	c.SmartLLM = SmartLLM{
		Provider:    "deepseek",
		BaseURL:     "https://api.deepseek.com",
		Model:       "deepseek-chat",
		MaxTokens:   1024,
		Temperature: 0.3,
	}
	c.Budget = Budget{
		Enabled:          false,
		MaxTokens:        2000,
		MaxTurns:         20,
		MaxDurationSec:   300,
		DegradeThreshold: 0.8,
	}
	c.Guard = Guard{
		Enabled:       false,
		MaxInputRunes: 0,
	}
	c.OffTopic = OffTopic{
		Enabled:       false,
		ConvergeAfter: 2,
		EndAfter:      4,
	}
	c.setLocalDefaults()
}

// setLocalDefaults 设置本地推理和调度相关配置的默认值。
func (c *Config) setLocalDefaults() {
	c.LocalASR = LocalASR{
		Enabled:    false,
		NumThreads: 1,
	}
	c.LocalTTS = LocalTTS{
		Enabled:    false,
		NumThreads: 1,
		Speed:      1.0,
		Threshold:  10,
	}
	c.Scheduler = Scheduler{
		Queue: "outbound",
		Retry: Retry{
			StartHour:      9,
			EndHour:        20,
			WeekdaysOnly:   true,
			MinIntervalMin: 120,
		},
	}
	c.Snapshot = Snapshot{
		TTLSec: 600, // 10 分钟。
	}
	c.SileroVAD = SileroVAD{
		Enabled:            false,
		Threshold:          0.5,
		MinSilenceDuration: 0.3,
		MinSpeechDuration:  0.25,
		SampleRate:         16000,
	}
}

// knownSections 列出配置节名称，按最长优先排序用于前缀匹配。
var knownSections = []string{
	"call_protection", "postprocessor", "notification",
	"freeswitch", "database", "smart_llm", "realtime",
	"local_asr", "local_tts", "silero_vad", "observe",
	"pipeline", "scheduler", "snapshot", "budget", "server", "worker",
	"off_topic", "guard", "redis", "asr", "llm", "tts", "amd", "oss",
}

// Load 从给定的 TOML 文件路径读取配置，然后应用环境变量覆盖。
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	// 1. 加载结构体默认值。
	if err := k.Load(structs.Provider(Defaults(), "koanf"), nil); err != nil {
		return nil, fmt.Errorf("load defaults: %w", err)
	}

	// 2. 如提供则加载 TOML 文件。
	if path != "" {
		if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
			return nil, fmt.Errorf("read config file %s: %w", path, err)
		}
	}

	// 3. 加载环境变量：CLARION_LLM_API_KEY -> llm.api_key
	if err := k.Load(env.ProviderWithValue("CLARION_", ".", func(key, val string) (string, any) {
		key = strings.ToLower(strings.TrimPrefix(key, "CLARION_"))
		for _, section := range knownSections {
			prefix := strings.ReplaceAll(section, ".", "_") + "_"
			if rest, ok := strings.CutPrefix(key, prefix); ok {
				return section + "." + rest, val
			}
		}
		return key, val
	}), nil); err != nil {
		return nil, fmt.Errorf("load env overrides: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}
	return &cfg, nil
}

// validate 检查必填字段和值约束。
func (c *Config) validate() error {
	var errs []error
	if c.Server.Addr == "" {
		errs = append(errs, errors.New("server.addr must not be empty"))
	}
	if c.Database.DSN == "" {
		errs = append(errs, errors.New("database.dsn must not be empty"))
	}
	if c.Redis.Addr == "" {
		errs = append(errs, errors.New("redis.addr must not be empty"))
	}
	if c.Database.MaxOpenConns <= 0 {
		errs = append(errs, errors.New("database.max_open_conns must be positive"))
	}
	if c.CallProtection.MaxDurationSec <= 0 {
		errs = append(errs, errors.New("call_protection.max_duration_sec must be positive"))
	}
	if c.Worker.MaxConcurrentCalls <= 0 {
		errs = append(errs, errors.New("worker.max_concurrent_calls must be positive"))
	}
	switch c.Pipeline.Mode {
	case PipelineClassic, PipelineHybrid:
		// 合法值。
	default:
		errs = append(errs, fmt.Errorf("pipeline.mode must be %q or %q, got %q",
			PipelineClassic, PipelineHybrid, c.Pipeline.Mode))
	}
	errs = append(errs, c.validateLocalModels()...)
	return errors.Join(errs...)
}

// validateLocalModels 校验本地推理模型（SileroVAD / LocalASR / LocalTTS）的必填路径。
func (c *Config) validateLocalModels() []error {
	var errs []error
	if c.SileroVAD.Enabled && c.SileroVAD.ModelPath == "" {
		errs = append(errs, errors.New("silero_vad.model_path must not be empty when silero_vad is enabled"))
	}
	if c.LocalASR.Enabled {
		if c.LocalASR.EncoderPath == "" {
			errs = append(errs, errors.New("local_asr.encoder_path must not be empty when local_asr is enabled"))
		}
		if c.LocalASR.DecoderPath == "" {
			errs = append(errs, errors.New("local_asr.decoder_path must not be empty when local_asr is enabled"))
		}
		if c.LocalASR.TokensPath == "" {
			errs = append(errs, errors.New("local_asr.tokens_path must not be empty when local_asr is enabled"))
		}
	}
	if c.LocalTTS.Enabled && c.LocalTTS.ModelPath == "" {
		errs = append(errs, errors.New("local_tts.model_path must not be empty when local_tts is enabled"))
	}
	return errs
}

// String 实现 fmt.Stringer，对敏感字段进行脱敏。
func (c Config) String() string {
	return fmt.Sprintf(
		"Config{server=%s, db=%s, redis=%s, asr=%s, llm=%s/%s, tts=%s, pipeline=%s}",
		c.Server.Addr,
		maskDSN(c.Database.DSN),
		c.Redis.Addr,
		c.ASR.Provider,
		c.LLM.Provider, c.LLM.Model,
		c.TTS.Provider,
		c.Pipeline.Mode,
	)
}

func maskDSN(dsn string) string {
	if idx := strings.Index(dsn, "@"); idx > 0 {
		return "***@" + dsn[idx+1:]
	}
	return "***"
}
