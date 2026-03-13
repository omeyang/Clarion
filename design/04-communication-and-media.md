# 第四章 通信与媒体层

---

## 1. FreeSWITCH 集成

FreeSWITCH 是外呼系统的**电话呼叫控制中心**，Clarion 通过 ESL TCP 连接进行控制。

### 1.1 ESL 客户端架构

```go
// internal/call/esl.go
type ESLClient struct {
    cfg     config.FreeSWITCH
    conn    net.Conn
    reader  *bufio.Reader       // 65536 字节缓冲（SIP 头可能很大）
    replyCh chan eslResponse     // readLoop 向 sendRaw 传递响应
    events  chan ESLEvent        // 事件通道（缓冲 256）
}
```

架构要点：

- **单 goroutine 读取**：所有 TCP 读取集中在 `readLoop` goroutine 中，避免并发读取竞争
- **命令-响应分离**：`sendRaw` 只负责写入命令，然后从 `replyCh` 等待 `readLoop` 传回响应
- **写入互斥**：`writeMu sync.Mutex` 保护写入操作，允许多个 goroutine 安全发送命令

### 1.2 连接流程

```
TCP 连接 → FreeSWITCH 发送 "auth/request" → 发送密码认证 → 订阅事件 → 启动 readLoop
```

ESL 使用 Outbound 模式：Call Worker 主动连接 FreeSWITCH 的 ESL 端口（配置在 `config.FreeSWITCH`）。

### 1.3 Originate 命令

ESLClient 提供三种呼叫发起方式：

| 方法 | 用途 | 目标 |
|------|------|------|
| `Originate(ctx, gateway, callerID, phone, sessionID)` | 通过 SIP 网关外呼 | 真实电话号码 |
| `OriginateUser(ctx, phone, sipDomain, callerID, sessionID)` | 呼叫本地 SIP 用户 | SIP 注册终端 |
| `OriginateLoopback(ctx, phone, context, sessionID)` | 回环测试 | FreeSWITCH 内部 |

所有 originate 命令使用 `&park()` 让通话接通后暂停，由 Call Worker 接管后续控制。

### 1.4 通话控制命令

| 命令 | 用途 | 调用时机 |
|------|------|----------|
| `uuid_break` | 停止当前播放 | Barge-in 打断时 |
| `uuid_kill` | 强制结束通话 | 超时或异常挂断 |

---

## 2. 音频桥接

### 2.1 mod_audio_fork + WebSocket

FreeSWITCH 通过 `mod_audio_fork` 模块将实时音频流通过 WebSocket 桥接到 Call Worker。

```
被叫用户 (手机/座机)
    │  语音通话 (G.711)
    ▼
FreeSWITCH
    │  mod_audio_fork (WebSocket 双向)
    │  音频格式: 8kHz PCM
    ▼
Call Worker WebSocket Server
    │
    ├─→ AudioIn channel → Session 处理
    │       │
    │       ├─→ 重采样 8kHz→16kHz (pcm.Resample8to16Into)
    │       └─→ 流式 ASR
    │
    └─← AudioOut channel ← Session TTS 输出
            │
            └─→ WebSocket 回送 → FreeSWITCH → 用户听到
```

### 2.2 audioLink 双向通道

每个会话拥有一对缓冲 channel，连接 WebSocket 桥接与 Session：

```go
type audioLink struct {
    in  chan []byte  // FreeSWITCH → Call Worker（缓冲 128）
    out chan []byte  // Call Worker → FreeSWITCH（缓冲 128）
}
```

Worker 在 `executeTask` 中创建 audioLink，注册到 `audioLinks` map（以 sessionID 为键）。WebSocket handler（`handleAudioWS`）通过 sessionID 查找对应的 audioLink：

- **读取方向**：从 WebSocket 读取 binary frame → 写入 `link.in` → Session 的 `AudioIn` channel
- **写入方向**：Session 写入 `AudioOut` channel → 从 `link.out` 读取 → 通过 WebSocket 发送回 FreeSWITCH

### 2.3 音频格式矩阵

| 位置 | 采样率 | 编码 | 说明 |
|------|--------|------|------|
| 电话线路 | 8kHz | G.711 (PCMU/PCMA) | 电信标准 |
| FreeSWITCH 内部 | 8kHz | PCM16 Linear | mod_audio_fork 输出 |
| ASR 输入 | 16kHz | PCM16 Linear | `pcm.Resample8to16Into` 转换 |
| TTS 输出 | 16-24kHz | PCM16 Linear | 按 provider 不同 |
| 回送 FreeSWITCH | 8kHz | PCM16 Linear | 下采样后回送 |

### 2.4 重采样实现

```go
// 热路径优化：预分配缓冲区，避免每帧分配
s.resampleBuf    // ASR 重采样用
s.vadResampleBuf // VAD 重采样用（VAD 可能需要不同采样率）
```

使用 Sonata 的 `pcm.Resample8to16Into` 函数，线性插值实现 8kHz→16kHz 上采样。Session 在创建时预分配缓冲区，50 帧/秒的热路径上零分配。

---

## 3. Call Worker 架构

### 3.1 Worker 核心结构

```go
// internal/call/worker.go
type Worker struct {
    cfg    config.Config
    rds    *store.RDS
    asr    provider.ASRProvider
    llm    provider.LLMProvider
    tts    provider.TTSProvider
    esl    *ESLClient

    activeCalls atomic.Int32
    maxCalls    int

    sessions   map[string]*Session    // sessionID → session
    audioLinks map[string]*audioLink  // sessionID → 音频通道

    wsServer        *http.Server
    snapshotStore   SnapshotStore
    speechDetector  SpeechDetector
    schedulerClient *scheduler.Client
    metrics         *observe.CallMetrics
}
```

### 3.2 启动流程

`Worker.Run(ctx)` 的启动序列：

```
1. 连接 FreeSWITCH ESL（TCP）
2. 启动 WebSocket 音频服务器（接收 mod_audio_fork 连接）
3. 启动 Asynq 任务服务器（消费外呼任务队列）
4. 等待 ctx.Done() 关闭信号
5. 优雅关闭：Asynq shutdown → WS server shutdown → Scheduler client close → ESL close
```

### 3.3 任务消费

Worker 使用 **Asynq**（基于 Redis 的任务队列）消费外呼任务，而非 Redis BRPOP 轮询：

```go
srv := asynq.NewServer(
    scheduler.RedisOpt(w.cfg.Redis),
    asynq.Config{
        Concurrency: w.maxCalls,  // 并发数 = 最大通话数
        Queues:      map[string]int{w.cfg.Scheduler.Queue: 1},
    },
)
mux.HandleFunc(scheduler.TaskTypeOutboundCall, w.HandleOutboundCall)
```

Asynq 的优势：

- 自动并发控制（`Concurrency` 限制）
- 任务重试和死信队列
- 优雅关闭（等待活跃任务完成）

### 3.4 executeTask 流程

```go
func (w *Worker) executeTask(ctx context.Context, task Task) {
    // 1. 创建双向音频通道
    audioLink := &audioLink{in: make(chan []byte, 128), out: make(chan []byte, 128)}

    // 2. 构建 SessionConfig
    sessionCfg := w.buildSessionConfig(task, sessionID, audioIn, audioOut)

    // 3. 附加组件
    w.attachDialogueEngine(&sessionCfg)     // 对话引擎
    w.attachHybridProviders(&sessionCfg)    // Realtime + Strategy（hybrid 模式）
    w.attachInputFilter(&sessionCfg)        // 输入安全过滤
    w.attachGuardHybrid(&sessionCfg)        // Budget + DecisionValidator
    w.attachRecoverySnapshot(ctx, &sessionCfg, task.RecoveryFromCallID) // 恢复快照

    // 4. 创建并运行 Session
    session := NewSession(sessionCfg)
    result, err := session.Run(ctx)

    // 5. 发布完成事件到 Redis Stream
    w.publishCompletion(ctx, result)

    // 6. 意外中断时入队恢复任务
    w.scheduleRecoveryIfNeeded(ctx, result, task)
}
```

### 3.5 并发模型

```
Call Worker 进程
    ├── ESL 连接（单 goroutine readLoop）
    ├── WebSocket 服务器（per-connection goroutine）
    ├── Asynq 任务服务器
    │   ├── 通话 A（Session goroutine + eventLoop）
    │   ├── 通话 B（Session goroutine + eventLoop）
    │   └── ...
    └── 最大并发：config.Worker.MaxConcurrentCalls
```

每路通话由 Asynq 分配的 goroutine 驱动，Session 内部的 eventLoop 是单 goroutine 模型（FSM 使用 `Unsynced()`）。

---

## 4. AMD 检测

### 4.1 算法概述

AMD（Answering Machine Detection）使用**能量+时序分析**，在 Clarion 侧实现，不依赖 FreeSWITCH 的 mod_amd。

```go
// internal/call/amd.go
type AMDDetector struct {
    cfg                config.AMD
    continuousSpeechMs int
    pauseDetected      bool
    result             engine.AnswerType
    decided            bool
}
```

### 4.2 检测逻辑

```
接通后开始 AMD 检测窗口
    │
    ├─ 连续语音 ≥ ContinuousSpeechThresholdMs
    │    → 留言机（连续播放固定欢迎语）
    │    → result = AnswerVoicemail → FSM → Hangup
    │
    ├─ 检测到自然停顿（语音→静默→再次语音）
    │  且静默 ≥ HumanPauseThresholdMs
    │    → 真人（有自然节奏和停顿）
    │    → result = AnswerHuman → FSM → BotSpeaking
    │
    └─ 检测窗口到期（DetectionWindowMs）
         ├─ 有停顿 → AnswerHuman
         ├─ 有语音但无停顿 → AnswerVoicemail
         └─ 无语音 → AnswerUnknown
```

### 4.3 帧处理

每帧音频通过 `FeedFrame(energyDBFS, frameMs)` 送入检测器：

- `energyDBFS`：帧能量（dBFS），通过 `pcm.EnergyDBFS(frame)` 计算
- `frameMs`：帧时长，通过 `pcm.FrameDuration(frame, 8000)` 计算
- 语音判定：`energyDBFS > cfg.EnergyThresholdDBFS`

### 4.4 可测试版本

`AMDDetectorTestable` 使用显式时间戳（`currentMs`）替代 `time.Now()`，用于确定性测试。Session 实际使用 Testable 版本。

---

## 5. ESL 事件处理

### 5.1 事件结构

```go
type ESLEvent struct {
    Name    string
    Headers map[string]string
    Body    string
}
```

事件通过 `events chan ESLEvent`（缓冲 256）从 `readLoop` 分发到 Session 的 eventLoop。

### 5.2 核心事件处理

| 事件 | Session 处理 |
|------|-------------|
| `CHANNEL_ANSWER` | 记录 channelUUID、标记 `answered=true`、启动 mod_audio_fork、FSM → `AMDDetecting`（或直接 `BotSpeaking`） |
| `CHANNEL_HANGUP` | 提取挂断原因、判断是否意外中断、FSM → `Hangup`、保存快照（如需要） |
| `PLAYBACK_STOP` | 非流式 TTS 模式下触发 `EvBotDone`；流式模式下由 goroutine 统一管理 |

### 5.3 挂断原因处理

Session 根据挂断原因（`Hangup-Cause` header）进行不同处理：

| 挂断原因 | 分类 | 处理 |
|----------|------|------|
| `NORMAL_CLEARING` | 正常 | 正常记录通话完成 |
| `USER_BUSY` | 用户忙 | 标记忙线 |
| `NO_ANSWER` | 无人接听 | 标记未接 |
| `audio_closed` | 意外中断 | 保存快照、入队恢复任务 |
| `MEDIA_TIMEOUT` | 意外中断 | 保存快照、入队恢复任务 |
| `DESTINATION_OUT_OF_ORDER` | 意外中断 | 保存快照、入队恢复任务 |

---

## 6. 完整数据流

```
Asynq 任务队列
    │ HandleOutboundCall
    ▼
Worker.executeTask
    │ buildSessionConfig + attach*
    ▼
Session.Run(ctx)
    │ warmup → ESL originate
    │
    │  ┌─────────── FreeSWITCH ───────────┐
    │  │ CHANNEL_ANSWER → mod_audio_fork  │
    │  │        ↕ WebSocket ↕              │
    │  └──────────────────────────────────┘
    │        ↕
    │  audioLink (in/out channels)
    │        ↕
    │  eventLoop
    │  ├── audioIn → handleAudioFrame
    │  │     ├── AMD → BotSpeaking
    │  │     ├── WaitingUser → feedASR + detectSpeech
    │  │     ├── UserSpeaking → feedASR
    │  │     └── BotSpeaking → bargeIn
    │  ├── asrResults → dialogue → TTS → AudioOut
    │  ├── silenceTimer → 提醒/挂断
    │  ├── eslEvents → HANGUP 处理
    │  └── botDoneCh → WaitingUser
    │
    ▼
SessionResult → Redis Stream → Post-Processing Worker
```
