# 第三章 实时会话运行时

---

## 1. 双层 Session 架构

Clarion 的实时会话运行时采用**双层 Session 架构**：底层是 Sonata 引擎的通用 Session，上层是 Clarion 针对电话外呼场景的特化 Session。

### 1.1 Sonata Session（通用引擎层）

Sonata 是独立的核心引擎库（`github.com/omeyang/Sonata`），提供与具体通信协议无关的实时语音会话能力。

核心概念：

| 组件 | 说明 |
|------|------|
| `Session.Config` | SessionID、ASR/LLM/TTS provider（`aiface` 接口）、Transport、DialogEngine、Transitions、Logger、Metrics |
| `ProtectionConfig` | MaxDurationSec(300)、MaxSilenceSec(15)、FirstSilenceTimeoutSec(6) |
| `Transport` 接口 | `AudioIn() <-chan []byte`、`PlayAudio`、`StopPlayback`、`Close` |
| `SpeechDetector` 接口 | 可插拔的语音检测（WebRTC VAD、Silero VAD 等） |
| 媒体 FSM | 表驱动状态机，`PhoneTransitions()`（电话生命周期）和 `AppTransitions()`（简化版） |

Sonata Session 的 `Run(ctx)` 流程：

```
warmupProviders → startDialogue(opening) → eventLoop
```

`eventLoop` 通过 `select` 监听：`ctx.Done`、`audioIn`、`silenceTimer`、`asrResults`、`botDoneCh`。

### 1.2 Clarion Session（外呼特化层）

Clarion 的 `internal/call/Session` 不直接嵌入 Sonata Session，而是复用其核心设计模式，并扩展电话外呼特有的能力。

```go
// internal/call/session.go
type Session struct {
    cfg            SessionConfig
    mfsm           *media.FSM           // Sonata 媒体 FSM（PhoneTransitions）
    asrStream      provider.ASRStream
    asrResults     chan provider.ASREvent
    botDoneCh      chan struct{}
    bargeInFrames  int
    ttsCancel      context.CancelFunc
    ttsPlaying     atomic.Bool
    speechDetector SpeechDetector
    amdDetector    *AMDDetectorTestable
    asrBreaker     *resilience.Breaker
    speculative    *speculativeRun
    resampleBuf    []byte               // 热路径复用缓冲
    vadResampleBuf []byte
    netQuality     *NetworkQuality
    metrics        *observe.CallMetrics
}
```

Clarion Session 相比 Sonata 新增的能力：

| 能力 | 文件 | 说明 |
|------|------|------|
| ESL 控制 | `session_esl.go` | FreeSWITCH 命令（originate、uuid_break、uuid_kill） |
| AMD 检测 | `session_audio.go` + `amd.go` | 能量+时序的留言机检测 |
| 填充词 | `session_filler.go` | 预合成 PCM 轮询播放，掩盖 LLM 延迟 |
| 预推理 | `session_speculative.go` | 基于 partial ASR 稳定性提前启动 LLM |
| 混合模式 | `session_hybrid.go` | Omni Realtime + Smart LLM 异步分析 |
| 会话快照 | `snapshot.go` | 意外中断时保存状态到 Redis，恢复呼叫时加载 |
| 网络质量 | `netquality.go` | 抖动、丢帧、低音量检测 |
| 安全防护 | `guard` 包集成 | InputFilter、CallBudget、DecisionValidator |
| ASR 熔断 | `resilience.Breaker` | ASR 连续失败时快速跳过 |

### 1.3 Session 的运行流程

Clarion Session 的 `Run(ctx)` 流程：

```
warmupProviders → mfsm.Handle(EvDial) → ESL originate → eventLoop
```

`eventLoop` 通过 `select` 监听五个通道：

```go
select {
case <-ctx.Done():        // 上下文取消
case frame := <-audioIn:  // FreeSWITCH 音频帧
case evt := <-eslEvents:  // ESL 事件（CHANNEL_ANSWER、CHANNEL_HANGUP 等）
case <-silenceTimer.C:    // 静默超时
case evt := <-asrResults: // ASR 最终结果
case <-botDoneCh:         // TTS 播放完成
}
```

经典模式和混合模式使用不同的 eventLoop 实现：`eventLoop`（classic）和 `eventLoopHybrid`（hybrid）。

---

## 2. 流式管道

![流式管道](./images/03-01-streaming-pipeline.png)

流式管道是实时会话运行时的核心，采用**流水线并行 + 句级分割**，最大化减少端到端延迟。

### 2.1 经典管线（Classic Pipeline）

```
AudioIn → 重采样(8kHz→16kHz) → 流式 ASR → [填充词] → LLM 流式生成 → 句级分割 → TTS 逐句合成 → AudioOut
```

关键特性：

**句级流式 TTS**：DialogEngine 的 `ProcessStream` 返回 `<-chan string`，每个完整句子独立发送给 TTS 合成。`session_tts.go` 中 `synthesizeAndPlayStreamAsync` 从 sentence channel 逐句读取，每句独立调用 TTS，实现边生成边播放。

**填充词掩盖延迟**：ASR 返回最终结果后、LLM 开始输出之前，播放预合成的填充词音频（"嗯"、"好的"），将用户感知延迟从约 2 秒降至约 200ms。

**预推理加速**：当 partial ASR 文本连续稳定 3 次（约 300-600ms），提前启动 LLM 推理。若 final ASR 结果与预推理输入一致，直接复用结果，省去 500-800ms 首 Token 延迟。

### 2.2 混合管线（Hybrid Pipeline）

```
AudioIn → Qwen3-Omni-Flash-Realtime（实时语音对话）
              ↓ 异步
          Smart LLM（策略分析、字段提取、状态决策）
```

混合模式（`PipelineHybrid`）使用 Omni Realtime 模型直接处理语音流，绕过 ASR→LLM→TTS 链路，实现更低延迟。同时异步调用 Strategy LLM 进行业务决策。

### 2.3 延迟对比

```
批处理模式：
  ASR 2s ──────→ LLM 2s ──────→ TTS 1s ──→  总延迟 ~5s

经典流式（无优化）：
  ASR 0.3s → LLM 0.5s → TTS 0.5s →  总延迟 ~1.3s

经典流式 + 填充词 + 预推理：
  填充词 0.2s → [预推理省 0.5s] → TTS 0.3s →  感知延迟 ~0.2s
```

---

## 3. 媒体状态机

![媒体状态机](./images/03-02-media-state-machine.png)

### 3.1 表驱动设计

Sonata 的媒体 FSM（`engine/mediafsm/fsm.go`）采用**表驱动设计**：

```go
// 转换表：map[transitionKey]State，O(1) 查找
type transitionKey struct {
    from  State
    event Event
}
```

Clarion 通过类型别名复用 Sonata FSM（`internal/engine/media/fsm.go`）：

```go
type FSM = smedia.FSM

func NewFSM(initial engine.MediaState, opts ...Option) *FSM {
    return smedia.NewFSM(initial, smedia.PhoneTransitions(), opts...)
}
```

### 3.2 状态与转换

完整的电话生命周期状态（`PhoneTransitions()`）：

| 状态 | 说明 |
|------|------|
| `Idle` | 等待调度 |
| `Dialing` | 已发 SIP INVITE |
| `Ringing` | 对方振铃中 |
| `AMDDetecting` | 留言机检测 |
| `BotSpeaking` | AI 播放 TTS 音频 |
| `WaitingUser` | 等待用户开口 |
| `UserSpeaking` | 用户说话中，ASR 识别中 |
| `Processing` | LLM + TTS 生成中 |
| `BargeIn` | 用户打断 AI |
| `SilenceTimeout` | 静默超时 |
| `Hangup` | 通话结束 |
| `PostProcessing` | 异步后处理 |

### 3.3 Unsynced 优化

FSM 提供 `Unsynced()` 选项，禁用内部 RWMutex。Clarion Session 的 eventLoop 是单 goroutine 事件循环，所有 FSM 操作都在同一 goroutine 中执行，无需锁同步：

```go
mfsm: media.NewFSM(engine.MediaIdle, media.Unsynced()),
```

### 3.4 媒体 FSM 与对话 FSM 的协作

- **媒体 FSM** 控制"何时说、何时听"（音频交互节奏）
- **对话 FSM**（`dialogue.Engine`）控制"说什么"（内容决策）
- 两者通过 Session 协调，独立运行、事件驱动

---

## 4. VAD 与 Barge-in

### 4.1 语音检测架构

Clarion 使用可插拔的 `SpeechDetector` 接口：

```go
type SpeechDetector interface {
    IsSpeech(frame []byte) (bool, error)
}
```

三级检测策略：

| 优先级 | 方案 | 说明 |
|--------|------|------|
| 1 | WebRTC VAD | C 库绑定，轻量高效 |
| 2 | Silero VAD | ONNX 模型，准确率高 |
| 3 | 能量阈值回退 | `pcm.EnergyDBFS(frame)` > `-35.0 dBFS` |

Worker 通过 `SetSpeechDetector()` 注入检测器，未设置时回退到能量阈值。

### 4.2 音频帧处理

`handleAudioFrame` 根据当前媒体状态分发音频帧：

| 状态 | 处理逻辑 |
|------|----------|
| `AMDDetecting` | 送入 AMD 检测器 |
| `WaitingUser` | 送入 ASR + 检测语音开始 |
| `UserSpeaking` | 持续送入 ASR |
| `BotSpeaking` | 检测 barge-in |
| 其他状态 | 不处理 |

### 4.3 Barge-in 检测

在 `BotSpeaking` 状态下，`handleBargeInFrame` 持续检测用户是否打断：

- 条件：连续 **10 帧**语音检测为 true（每帧 20ms，共约 **200ms**）
- 防误触发：必须 `ttsPlaying` 为 true（TTS 确实在播放时才允许打断）
- 触发后：
  1. 取消 TTS context（`ttsCancel()`）
  2. 停止 FreeSWITCH 播放（`uuid_break`）
  3. FSM 转换 `BotSpeaking → BargeIn → UserSpeaking`
  4. 重置 ASR 缓冲，开始新一轮识别

### 4.4 音频重采样

FreeSWITCH 输出 8kHz PCM，ASR 引擎需要 16kHz PCM：

```go
// 使用 Sonata 的 pcm.Resample8to16Into，复用缓冲区避免热路径分配
pcm.Resample8to16Into(frame, s.resampleBuf)
```

`resampleBuf` 和 `vadResampleBuf` 在 Session 创建时预分配，避免每帧（50 帧/秒）的内存分配开销。

---

## 5. 保护机制

### 5.1 通话保护参数

```go
type CallProtection struct {
    MaxDurationSec       int  // 默认 300（5 分钟）
    MaxSilenceSec        int  // 默认 15
    FirstSilenceTimeoutSec int // 默认 6
    MaxTurns             int  // 最大对话轮次
}
```

### 5.2 静默超时策略

静默超时采用**两级递进**：

| 阶段 | 超时时间 | 处理方式 |
|------|----------|----------|
| 首次静默 | 6 秒（`FirstSilenceTimeoutSec`） | 播放提醒话术，FSM → `SilenceTimeout → BotSpeaking` |
| 二次静默 | 15 秒（`MaxSilenceSec`） | 礼貌挂断，FSM → `SilenceTimeout → Hangup` |

Session 内部通过 `silenceCount` 字段追踪静默次数。

### 5.3 ASR 熔断器

```go
asrBreaker: resilience.NewBreaker(resilience.BreakerConfig{
    // ASR 流启动连续失败时快速跳过，避免阻塞热路径
})
```

`resilience.Breaker` 是简单的熔断器：连续失败次数达到阈值后进入 Open 状态，后续请求直接跳过；经过冷却期后进入 Half-Open 状态尝试恢复。

### 5.4 通话最大时长

`MaxDurationSec` 默认 300 秒（5 分钟）。eventLoop 在每次循环中检查通话时长，超时后播放结束话术并挂断。

---

## 6. 填充词

### 6.1 工作原理

填充词（filler audio）是预合成的短音频片段（如"嗯"、"好的"），在 ASR 返回最终结果后、LLM 开始输出前播放。

```
用户说完 → ASR final → [播放填充词 ~200ms] → LLM 处理 → TTS 首句播放
                                                       用户感知延迟 ≈ 200ms
```

### 6.2 实现细节

```go
// session_filler.go
func (s *Session) playFiller(ttsCtx context.Context) {
    idx := int(fillerIndex.Add(1)-1) % len(s.cfg.FillerAudios)
    filler := s.cfg.FillerAudios[idx]
    // 填充词已是 8kHz PCM16，无需重采样
    // 优先通过 ESL 播放 WAV 文件，退回 AudioOut 直写
}
```

- **轮询选择**：全局 `atomic.Int64` 计数器取模，避免连续播放相同填充词
- **格式**：预合成 8kHz PCM16 数据，与 FreeSWITCH 音频格式一致
- **播放方式**：优先写入临时 WAV 文件通过 ESL 播放，无 ESL 时退回 AudioOut channel

---

## 7. 预推理

### 7.1 原理

预推理（speculative execution）基于一个观察：partial ASR 在用户接近说完时趋于稳定。当 partial 文本连续不变达到阈值，可以高概率认为这就是最终结果，提前启动 LLM 推理。

### 7.2 触发条件

```go
const (
    speculativeStableThreshold = 3  // 普通场景：连续 3 次 partial 相同
    speculativeEarlyThreshold  = 2  // 句末标点场景：连续 2 次即触发
)
```

句末标点加速：当 partial 文本以 `。！？.!?` 结尾时，用户大概率已说完，降低触发阈值。

### 7.3 执行流程

```
partial ASR "你好" → partial "你好" → partial "你好" (稳定 3 次)
                                         ↓
                              startSpeculative("你好")
                              DialogueEngine.PrepareStream(ctx, "你好")
                                         ↓
                              返回 sentenceCh, commit, cancel
                                         ↓
                     ASR final "你好" → 文本匹配 → commit() → 复用预推理结果
                     ASR final "你好吗" → 文本不匹配 → cancel() → 正常流程
```

### 7.4 状态管理

```go
type speculativeRun struct {
    inputText  string           // 触发预推理的 partial 文本
    sentenceCh <-chan string    // LLM 输出的句子 channel
    commit     func()           // 确认使用预推理结果
    cancel     context.CancelFunc // 取消预推理
}
```

当 partial 文本发生变化时（用户继续说话），已有的预推理立即取消，并消费剩余 channel 数据防止 goroutine 泄漏。

---

## 8. 会话快照与恢复

### 8.1 意外中断检测

以下挂断原因被判定为意外中断（而非用户主动挂断）：

| 原因 | 说明 |
|------|------|
| `audio_closed` | WebSocket 异常关闭 |
| `DESTINATION_OUT_OF_ORDER` | 目标端点故障 |
| `MEDIA_TIMEOUT` | RTP 媒体流超时 |
| `RECOVERY_ON_TIMER_EXPIRE` | 信令恢复超时 |
| `LOSE_RACE` | 竞态导致断线 |

前提条件：通话已接听（`answered=true`）时才触发恢复。

### 8.2 快照内容

```go
type SessionSnapshot struct {
    CallID          int64
    ContactID       int64
    TaskID          int64
    Phone, Gateway, CallerID string
    DialogueState   string             // 对话 FSM 当前状态
    Turns           []dialogue.Turn    // 最近 6 轮对话
    CollectedFields map[string]string  // 已收集的业务字段
    InterruptCause  string
    CreatedAt       time.Time
}
```

### 8.3 存储与恢复

- **存储**：`RedisSnapshotStore` 将快照 JSON 序列化后存入 Redis，键为 `{prefix}:snapshot:{callID}`，设置 TTL（默认 10 分钟）
- **恢复触发**：Worker 的 `scheduleRecoveryIfNeeded` 检测到意外中断后，通过 Scheduler Client 入队恢复任务
- **恢复流程**：恢复呼叫的 `SessionConfig.RestoredSnapshot` 非 nil，Session 加载历史对话、恢复对话状态、使用恢复开场白

---

## 9. 网络质量监控

### 9.1 监控指标

`NetworkQuality`（`netquality.go`）对每一帧音频进行实时质量分析：

| 指标 | 检测方式 | 阈值 |
|------|----------|------|
| 抖动（Jitter） | 帧间隔偏差的滑动窗口均值 | > 30ms |
| 音频间隙 | 连续帧间隔 | > 100ms |
| 低音量 | 连续帧能量低于阈值 | < -55 dBFS 连续 25 帧（约 500ms） |

### 9.2 事件类型

```go
const (
    NetEventPoorNetwork = "poor_network_detected"  // 抖动超标
    NetEventAudioGap    = "audio_gap_detected"      // 音频间隙
    NetEventLowVolume   = "low_volume_detected"     // 低音量
)
```

### 9.3 汇报

- 每 250 帧（约 5 秒）输出一次摘要日志
- 通话结束后，`NetworkQualitySnapshot` 随 `SessionResult` 一起发布到 Redis Stream

---

## 10. 双管线模式

### 10.1 经典模式（Classic）

```
AudioIn → 重采样 → ASR → [填充词] → DialogueEngine → 句级 TTS → AudioOut
```

传统的 ASR→LLM→TTS 管道，句级流式、填充词、预推理三重优化。

### 10.2 混合模式（Hybrid）

```
AudioIn → Qwen3-Omni-Flash-Realtime（实时语音对话）→ AudioOut
              ↓ 异步
          Strategy LLM（字段提取、状态决策）
```

- **Realtime 层**：Omni 模型直接处理语音流，绕过 ASR+TTS，延迟更低
- **Strategy 层**：异步 LLM 分析对话内容，提取业务字段、判断对话状态
- **Guard 系统**：`InputFilter`（注入攻击检测）、`CallBudget`（Token/时间预算）、`DecisionValidator`（决策格式校验）

模式选择通过 `SessionConfig.PipelineMode` 配置，Worker 在 `executeTask` 中根据配置自动切换 eventLoop。
