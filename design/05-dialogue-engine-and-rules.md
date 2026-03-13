# 05 - 对话引擎与业务规则

---

## 1. 双状态机设计

系统中运行着两个独立的状态机，各司其职，通过 Session 协调：

| 状态机 | 关注点 | 实现位置 |
|--------|--------|----------|
| **媒体 FSM** | 何时说（交互节奏） | `Sonata/engine/mediafsm`（Sonata 核心库） |
| **对话 FSM** | 说什么（对话内容） | `internal/engine/dialogue/fsm.go` |

![双状态机关系](./diagrams/05-02-dual-state-machines.mmd)

### 1.1 媒体 FSM

媒体 FSM 定义在 Sonata 核心库中，Clarion 通过类型别名引用（`internal/engine/types.go`）。状态包括：

| 状态 | 说明 |
|------|------|
| `Idle` | 空闲 |
| `Dialing` | 拨号中 |
| `Ringing` | 振铃中 |
| `AMDDetecting` | 答录机检测 |
| `BotSpeaking` | 机器人播放语音 |
| `WaitingUser` | 等待用户说话 |
| `UserSpeaking` | 用户说话中（VAD 检测到语音活动） |
| `Processing` | 处理中（ASR→LLM→规则→TTS） |
| `BargeIn` | 用户打断 |
| `SilenceTimeout` | 静默超时 |
| `Hangup` | 挂断 |
| `PostProcessing` | 异步后处理 |

媒体 FSM 由事件驱动（`EvDial`、`EvAnswer`、`EvSpeechStart`、`EvBargeIn` 等），每个事件触发确定性的状态转换。

### 1.2 对话 FSM

对话 FSM 与媒体 FSM 完全不同——它不是事件驱动，而是 **条件驱动**。每次推进时，FSM 遍历当前状态的所有 `TransitionRule`，找到第一个条件匹配的规则并执行转换。

### 1.3 两层状态机协作

```
时间线 →

媒体状态:  BOT_SPEAKING → WAITING → USER_SPEAKING → PROCESSING → BOT_SPEAKING → ...
                                                         ↓
对话状态:                                        [Opening] → [Qualification] → ...
                                                         ↑
                                                  规则引擎决策
```

关键规则：
- **对话 FSM 只在 PROCESSING 媒体状态时被驱动**——媒体 FSM 进入 Processing 状态后，ASR 结果送入对话引擎处理
- **挂断直接跳转 PostProcessing**——媒体 FSM 在任意时刻检测到挂断，直接进入后处理，不经过对话 FSM
- **打断（Barge-in）由媒体 FSM 处理**——`uuid_break` 停止播放后，进入 UserSpeaking 开始新一轮 ASR

---

## 2. 对话 FSM

### 2.1 状态总览

![业务状态机](./diagrams/05-01-business-state-machine.mmd)

对话 FSM 实现在 `internal/engine/dialogue/fsm.go`，定义了 7 个状态：

| 状态 | 职责 |
|------|------|
| **Opening** | 表明身份，确认是否方便沟通 |
| **Qualification** | 判断是否值得继续沟通，获取关键字段 |
| **InformationGathering** | 补全缺失字段，提高人工接手效率 |
| **ObjectionHandling** | 处理"没兴趣""没空"等异议 |
| **NextAction** | 引导下一步动作（加微信/预约/约时间） |
| **MarkForFollowup** | 标记需要人工跟进，触发异步通知 |
| **Closing** | 结束对话，收口并记录结果 |

### 2.2 条件驱动的状态转换

`TransitionRule` 结构体定义了转换规则：

```go
type TransitionRule struct {
    From      engine.DialogueState
    To        engine.DialogueState
    Condition func(ctx *engine.DialogueContext) bool
}
```

`FSM.Advance(ctx)` 遍历规则列表，找到第一个 `From == 当前状态 && Condition(ctx) == true` 的规则并转换。规则按组织分为 `openingRules`、`qualificationRules`、`informationGatheringRules`、`objectionHandlingRules`、`nextActionRules`、`followupRules`。

### 2.3 DialogueContext

转换条件基于 `DialogueContext` 进行判断：

| 字段 | 说明 |
|------|------|
| `CurrentState` | 当前对话状态 |
| `Intent` | LLM 识别的用户意图 |
| `TurnCount` | 当前轮次数 |
| `ObjectionCount` | 累计异议次数 |
| `MaxObjections` | 异议次数上限（来自模板配置） |
| `MaxTurns` | 最大对话轮次（来自模板配置） |
| `HighValue` | 是否为高价值线索 |
| `CollectedFields` | 已收集的结构化字段 |
| `RequiredFields` | 必填字段列表（来自模板配置） |

转换条件示例：Opening 状态下，若意图为 `continue`/`confirm`/`interested`/`ask_detail`，转入 Qualification；若意图为 `reject`/`not_interested`，直接 Closing。

### 2.4 终止与恢复

- `IsTerminal()` 检查当前状态是否为 `Closing`
- `ForceState(s)` 强制设置状态，用于从快照恢复

---

## 3. 规则引擎

### 3.1 LLMOutput → Decision 流程

规则引擎（`internal/engine/rules/engine.go`）位于 LLM 输出和对话 FSM 之间，做出确定性的业务决策。

![LLM与规则引擎边界](./diagrams/05-03-llm-rule-boundary.mmd)

**输入：LLMOutput**

```go
type LLMOutput struct {
    Intent          engine.Intent     // 用户意图
    ExtractedFields map[string]string // 提取的字段
    ObjectionType   string            // 异议类型
    SuggestedReply  string            // LLM 建议回复
    Confidence      float64           // 置信度
}
```

**输出：Decision**

```go
type Decision struct {
    NextState     engine.DialogueState // 建议下一状态
    ReplyStrategy ReplyStrategy        // 回复策略
    ReplyText     string               // 回复文本
    ShouldEnd     bool                 // 是否结束通话
    ShouldNotify  bool                 // 是否发送通知
    Grade         engine.Grade         // 评级（可选）
}
```

### 3.2 Evaluate 处理流程

`Evaluate(llmOut, dctx)` 的执行步骤：

1. **合并字段**——将 LLM 提取的字段合并到 `CollectedFields`
2. **跟踪异议**——`busy`/`not_interested`/`hesitate` 意图累加 `ObjectionCount`
3. **递增轮次**——`TurnCount++`
4. **判断高价值**——基于意图和字段数量判定 `HighValue`
5. **检查轮次上限**——达到 `MaxTurns` 时强制 Closing + 模板回复
6. **选择回复策略**——根据状态、置信度和 LLM 建议回复选择策略

### 3.3 回复策略

| 策略 | 触发条件 | 说明 |
|------|----------|------|
| `ReplyTemplate` | Closing 状态 / 低置信度（< 0.5）/ confirm 意图 | 使用预定义模板 |
| `ReplyLLM` | 高置信度（>= 0.5）且有建议回复 | 使用 LLM 草稿 |
| `ReplyPrecompiled` | 预编译音频场景 | 直接播放音频，无需文本 |

### 3.4 分级系统（A/B/C/D/X）

`GradeCall(dctx, callStatus)` 在通话结束后生成评级：

| 等级 | 判定条件 |
|------|----------|
| **X** | 通话状态为无效（no_answer / voicemail 等） |
| **D** | 意图为拒绝（reject / not_interested） |
| **A** | 高意图信号（interested / schedule 等）+ 收集字段数 >= AMinFields |
| **B** | 收集字段数 >= BMinFields + 轮次数 >= BMinTurns + 非拒绝意图 |
| **C** | 犹豫意图或有异议记录 |

分级规则由 `GradingRules` 配置，各阈值通过 `TemplateConfig` 传入，不同行业场景可独立配置。

---

## 4. LLM 集成

### 4.1 双模式设计

对话引擎同时支持两种 LLM 调用模式，分别用于不同场景：

| 模式 | 用途 | 调用方法 | 输出格式 |
|------|------|----------|----------|
| **结构化输出** | 意图识别 + 字段提取 | `callLLM` → `llm.Generate` | JSON（含 intent, extracted_fields, suggested_reply, confidence） |
| **流式自然文本** | TTS 语音合成 | `ProcessUserInputStream` → `llm.GenerateStream` | 纯文本，无 JSON，无格式标记 |

### 4.2 结构化输出模式

`callLLM` 通过 `buildMessages` 构建消息，系统提示词要求 LLM 返回 JSON：

```
你必须返回 JSON 格式，包含以下字段：
{
  "intent": "意图",
  "extracted_fields": {},
  "suggested_reply": "你的回复内容",
  "confidence": 0.8
}
```

系统提示词包含：当前对话状态、轮次计数、合法意图枚举。JSON 解析失败时降级为 `IntentUnknown` + 原始文本作为 `SuggestedReply`。

### 4.3 流式自然文本模式

`buildStreamMessages` 构建的系统提示词明确要求：

```
请直接用自然语言回复用户，简短、口语化，像打电话一样自然。
不要返回 JSON，不要加任何格式标记。
禁止使用括号描述动作或情绪，如"（停顿）""（语气温和）"等。
```

### 4.4 ProcessUserInput 完整流程（非流式）

```
budgetCheck → recordUserTurn → callLLM（JSON）→ validateDecision
→ validateOutput → checkOffTopic → ruleEngine.Evaluate → fsm.Advance
→ validateResponse → cleanContent → recordBudget → recordBotTurn
```

### 4.5 上下文管理

- 保留最近 `maxHistory` 轮（默认 5）对话，通过 `recentTurns()` 截取
- 已提取字段通过 `DialogueContext` 传递，不重复原始对话
- LLM 温度 0.7，最大 512 token

---

## 5. Guard 安全防护链

`internal/guard/` 包提供 7 层安全防护，在对话引擎中按需组合使用：

### 5.1 CallBudget — 成本预算控制

跟踪单通电话的 token、轮次、时长消耗，三个维度任一达标即触发：

| 配置项 | 说明 |
|--------|------|
| `MaxTokens` | token 上限（输入+输出合计），0 不限制 |
| `MaxTurns` | 最大对话轮次，0 不限制 |
| `MaxDuration` | 最长通话时长，0 不限制 |
| `DegradeThreshold` | 降级阈值比例（默认 0.8），达到后切换模板回复 |

三级操作：`BudgetOK` → `BudgetDegrade`（模板回复）→ `BudgetEnd`（结束通话）。

Token 估算：中文按 1.5 字符/token 粗略计算，`EstimateTokens` 函数实现。

### 5.2 ResponseValidator — 响应安全校验

检测 LLM 回复中的安全问题：

- **AI 身份泄露**——检测"我是 AI""我是语言模型"等表述
- **提示词泄露**——检测 LLM 泄露系统提示词内容
- **超长文本**——超过最大字符数时截断

检测到 AI 身份泄露或提示泄露时，替换为安全回复 `"好的，我了解了。"`

### 5.3 ContentChecker — 内容清洗

清洗 LLM 输出中不适合 TTS 合成的内容：

- 移除 JSON 片段、代码块、Markdown 格式、URL
- PII 检测（仅记录日志，不阻塞发送）
- 清洗后文本为空时保留原始文本，避免丢失整段回复

### 5.4 OffTopicTracker — 离题检测

跟踪连续离题轮次（默认 `unknown` 意图视为离题）：

| 阈值 | 默认值 | 操作 |
|------|--------|------|
| `ConvergeAfter` | 2 | 收束话术拉回正轨 |
| `EndAfter` | 4 | 礼貌结束通话 |

回到正轨（非离题意图）时计数器自动重置。

### 5.5 OutputChecker — 输出状态校验

校验 LLM 输出的意图在当前对话状态下是否合法：

- 防止在 Opening 状态就输出 Closing 意图
- 检测不合理的结束通话请求
- 返回修正后的意图

### 5.6 DecisionValidator — 决策校验

校验 LLM 结构化输出（意图、字段等）的合法性，返回 `Sanitized` 版本。

### 5.7 InputFilter — 输入过滤

基于正则模式检测 prompt 注入：

- 内置 13 种注入模式（如"忘记指令""ignore instructions""DAN mode"等）
- 最大输入字符数限制（默认 500），超长截断
- 输入文本清洗：去除首尾空白、折叠连续空白、移除控制字符

### 5.8 防护链在引擎中的执行顺序

**ProcessUserInput（非流式）：**

```
budgetCheck → callLLM → validateDecision → validateOutput
→ checkOffTopic → ruleEngine.Evaluate → fsm.Advance
→ validateResponse → cleanContent
```

**ProcessUserInputStream（流式）：**

```
budgetCheck → buildStreamMessages → GenerateStream
→ streamTokensToSentences（每句 validateResponse + cleanContent）
```

---

## 6. 流式分句

### 6.1 两级分句策略

`streamTokensToSentences` 将 LLM token 流按标点切分为句段，逐句送入 TTS：

| 层级 | 分隔符 | 触发条件 |
|------|--------|----------|
| **句级** | `。！？；.!?;` | 始终触发 |
| **子句级** | `，、,：:` | 累积字符数 >= 6（`minClauseRunes`）时触发 |

子句级切分的目的：将首段 TTS 延迟从约 300ms 降至约 100ms。`minClauseRunes = 6` 避免产生过短片段（如"嗯，"），确保 TTS 合成质量。

### 6.2 舞台指令移除

LLM 流式输出可能包含"（停顿）""（语气温和）"等舞台指令。通过正则 `[（(][^）)]*[）)]` 匹配并移除，避免 TTS 合成无意义文本。

### 6.3 逐句安全校验

每个分句在发送到通道前，经过两层校验：
1. `validateResponse` — AI 身份泄露、提示泄露、长度
2. `cleanContent` — JSON 片段、代码块、Markdown、URL

### 6.4 剩余文本处理

token 流结束后，缓冲区中剩余文本（未遇到分隔符的尾部）同样经过舞台指令移除和安全校验后发送。

---

## 7. 预推理（Speculative Execution）

### 7.1 PrepareStream

`PrepareStream(ctx, userText)` 用于基于 partial ASR 结果的预推理——在 ASR 尚未出 Final 时提前启动 LLM，降低端到端延迟。

返回三个值：
- `<-chan string` — 句子通道，与 `ProcessUserInputStream` 相同的分句逻辑
- `func()` — 确认函数（commitFn），在所有句段消费完成后调用
- `error` — 错误

### 7.2 延迟提交

关键设计：**不立即产生副作用**。

- 流式生成期间不记录对话轮次、不推进 FSM、不记录预算消耗
- 调用方消费完句段后调用 `commitFn`，此时才执行：
  1. 等待流式处理完成（`<-done` 确保 `replyText` 已写入）
  2. 记录 user turn 和 bot turn
  3. 推进 FSM（`recordStreamTurn`）
  4. 记录预算消耗
- 若预推理被取消（ctx 取消），不会产生任何副作用

---

## 8. 会话恢复

### 8.1 RestoreFromSnapshot

当通话因网络问题或系统故障中断后恢复时，通过 `RestoreFromSnapshot(data SnapshotData)` 还原对话引擎状态。

`SnapshotData` 包含：

| 字段 | 说明 |
|------|------|
| `DialogueState` | 中断时的对话状态名称（如 `"INFORMATION_GATHERING"`） |
| `Turns` | 中断前的最近对话轮次 |
| `CollectedFields` | 已收集的结构化字段 |

恢复步骤：
1. **FSM 状态恢复**——通过 `ParseDialogueState` 解析状态名称，`ForceState` 设置
2. **对话轮次恢复**——复制 Turns 到引擎，`TurnCount = len(Turns) / 2`（每轮含 user + bot）
3. **字段恢复**——`maps.Copy` 合并到 `CollectedFields`

### 8.2 恢复开场白

恢复呼叫时使用专门的开场白：

```
"您好，刚才电话好像断了，我们接着聊。"
```

通过 `GetRecoveryOpeningText()` 获取，区别于正常开场白 `GetOpeningText()`。

---

## 附录

### A. 意图类型

| 意图 | 说明 |
|------|------|
| `continue` | 愿意继续 |
| `confirm` | 确认/同意 |
| `interested` | 感兴趣 |
| `ask_detail` | 追问细节 |
| `hesitate` | 犹豫 |
| `busy` | 忙/没时间 |
| `not_interested` | 不感兴趣 |
| `reject` | 明确拒绝 |
| `schedule` | 想预约时间 |
| `unknown` | 无法识别 |

### B. 代码索引

| 组件 | 文件路径 |
|------|----------|
| 对话引擎 | `internal/engine/dialogue/engine.go` |
| 对话 FSM | `internal/engine/dialogue/fsm.go` |
| 规则引擎 | `internal/engine/rules/engine.go` |
| 对话上下文 | `internal/engine/context.go` |
| 类型定义 | `internal/engine/types.go` |
| 预算控制 | `internal/guard/budget.go` |
| 响应校验 | `internal/guard/response.go` |
| 内容清洗 | `internal/guard/content.go` |
| 离题检测 | `internal/guard/offtopic.go` |
| 输出校验 | `internal/guard/output.go` |
| 决策校验 | `internal/guard/output_validator.go` |
| 输入过滤 | `internal/guard/filter.go` |
