package provider

import sonata "github.com/omeyang/Sonata/engine/aiface"

// Message 表示发送给 LLM 的对话消息（Sonata 类型别名）。
type Message = sonata.Message

// LLMConfig 持有 LLM 调用的配置（Sonata 类型别名）。
type LLMConfig = sonata.LLMConfig

// LLMProvider 生成流式文本响应（Sonata 类型别名）。
type LLMProvider = sonata.LLMProvider
