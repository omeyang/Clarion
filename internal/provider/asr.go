// Package provider 定义 AI 能力（ASR、LLM、TTS）的接口。
//
// 所有类型均为 Sonata 核心库的类型别名，确保 clarion 与 Sonata 类型一致。
// 实现位于子包（asr/、llm/、tts/）中。
package provider

import sonata "github.com/omeyang/Sonata/engine/aiface"

// ASREvent 是来自 ASR 流的识别事件（Sonata 类型别名）。
type ASREvent = sonata.ASREvent

// ASRConfig 持有 ASR 流的配置（Sonata 类型别名）。
type ASRConfig = sonata.ASRConfig

// ASRStream 是一个活跃的语音识别会话（Sonata 类型别名）。
type ASRStream = sonata.ASRStream

// ASRProvider 创建 ASR 流（Sonata 类型别名）。
type ASRProvider = sonata.ASRProvider
