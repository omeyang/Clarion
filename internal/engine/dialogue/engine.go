package dialogue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/rules"
	"github.com/omeyang/clarion/internal/guard"
	"github.com/omeyang/clarion/internal/provider"
)

// stageDirectionRe 匹配 LLM 生成的舞台指令，如"（停顿）""（语气温和）"。
// 这些文本不应发送给 TTS 合成。
var stageDirectionRe = regexp.MustCompile(`[（(][^）)]*[）)]`)

// Turn 表示单个对话轮次。
type Turn struct {
	Number       int                  `json:"turn_number"`
	Speaker      string               `json:"speaker"`
	Content      string               `json:"content"`
	StateBefore  engine.DialogueState `json:"state_before"`
	StateAfter   engine.DialogueState `json:"state_after"`
	ASRLatencyMs int                  `json:"asr_latency_ms"`
	LLMLatencyMs int                  `json:"llm_latency_ms"`
	TTSLatencyMs int                  `json:"tts_latency_ms"`
	Interrupted  bool                 `json:"is_interrupted"`
}

// SessionResult 是对话会话的最终结果。
type SessionResult struct {
	Grade           engine.Grade      `json:"grade"`
	CollectedFields map[string]string `json:"collected_fields"`
	MissingFields   []string          `json:"missing_fields"`
	NextAction      string            `json:"next_action"`
	TurnCount       int               `json:"turn_count"`
	Turns           []Turn            `json:"turns"`
	ShouldNotify    bool              `json:"should_notify"`
}

// TemplateData 传递给 Go text/template 用于提示词渲染。
type TemplateData struct {
	CurrentState    string
	CollectedFields map[string]string
	MissingFields   []string
	RecentTurns     []Turn
	UserInput       string
	TurnCount       int
}

// Engine 编排对话循环：LLM → 规则 → FSM → 回复。
type Engine struct {
	fsm          *FSM
	ruleEngine   *rules.Engine
	llm          provider.LLMProvider
	dctx         *engine.DialogueContext
	turns        []Turn
	logger       *slog.Logger
	prompts      map[string]*template.Template
	systemPrompt string
	maxHistory   int
	budget       *guard.CallBudget
	respVal      *guard.ResponseValidator
	decVal       *guard.DecisionValidator
	outChecker   *guard.OutputChecker
	offTopic     *guard.OffTopicTracker
	contentChk   *guard.ContentChecker
}

// EngineConfig 配置对话引擎。
type EngineConfig struct {
	TemplateConfig       rules.TemplateConfig
	LLM                  provider.LLMProvider
	Logger               *slog.Logger
	PromptTemplates      map[string]string
	SystemPrompt         string
	MaxHistory           int
	BudgetConfig         *guard.BudgetConfig
	ResponseValidatorCfg *guard.ResponseValidatorConfig
	DecisionValidatorCfg *guard.DecisionValidatorConfig
	OutputCheckerCfg     *guard.OutputCheckerConfig
	OffTopicCfg          *guard.OffTopicConfig
	ContentCheckerCfg    *guard.ContentCheckerConfig
}

// NewEngine 创建对话引擎。
func NewEngine(cfg EngineConfig) (*Engine, error) {
	fsm := NewFSM(engine.DialogueOpening, DefaultRules())
	ruleEng := rules.NewEngine(cfg.TemplateConfig)

	prompts := make(map[string]*template.Template)
	for name, tmplStr := range cfg.PromptTemplates {
		t, err := template.New(name).Parse(tmplStr)
		if err != nil {
			return nil, fmt.Errorf("parse prompt template %s: %w", name, err)
		}
		prompts[name] = t
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	maxHistory := cfg.MaxHistory
	if maxHistory <= 0 {
		maxHistory = 5
	}

	var budget *guard.CallBudget
	if cfg.BudgetConfig != nil {
		budget = guard.NewCallBudget(*cfg.BudgetConfig)
	}

	var respVal *guard.ResponseValidator
	if cfg.ResponseValidatorCfg != nil {
		respVal = guard.NewResponseValidator(*cfg.ResponseValidatorCfg)
	}

	var decVal *guard.DecisionValidator
	if cfg.DecisionValidatorCfg != nil {
		decVal = guard.NewDecisionValidator(*cfg.DecisionValidatorCfg)
	}

	var outChecker *guard.OutputChecker
	if cfg.OutputCheckerCfg != nil {
		outChecker = guard.NewOutputChecker(*cfg.OutputCheckerCfg)
	}

	var offTopic *guard.OffTopicTracker
	if cfg.OffTopicCfg != nil {
		offTopic = guard.NewOffTopicTracker(*cfg.OffTopicCfg)
	}

	var contentChk *guard.ContentChecker
	if cfg.ContentCheckerCfg != nil {
		contentChk = guard.NewContentChecker(*cfg.ContentCheckerCfg)
	}

	return &Engine{
		fsm:        fsm,
		ruleEngine: ruleEng,
		llm:        cfg.LLM,
		dctx: &engine.DialogueContext{
			CollectedFields: make(map[string]string),
			RequiredFields:  cfg.TemplateConfig.RequiredFields,
			MaxObjections:   cfg.TemplateConfig.MaxObjections,
			MaxTurns:        cfg.TemplateConfig.MaxTurns,
		},
		logger:       logger,
		prompts:      prompts,
		systemPrompt: cfg.SystemPrompt,
		maxHistory:   maxHistory,
		budget:       budget,
		respVal:      respVal,
		decVal:       decVal,
		outChecker:   outChecker,
		offTopic:     offTopic,
		contentChk:   contentChk,
	}, nil
}

// budgetFallbackReply 是预算耗尽时的结束话术。
const budgetFallbackReply = "非常感谢您的时间，后续如有需要我们再联系您，祝您生活愉快，再见！"

// budgetDegradeReply 是预算降级时的模板回复。
const budgetDegradeReply = "好的，我了解了。感谢您的反馈，后续有需要可以随时联系我们。"

// responseSafetyFallback 是响应校验检测到安全问题时的替代回复。
const responseSafetyFallback = "好的，我了解了。"

// validateResponse 校验 LLM 响应文本的安全性。
// 检测 AI 身份泄露、系统提示泄露和超长文本。
// 返回安全的响应文本（可能被截断或替换为安全回复）。
func (e *Engine) validateResponse(text string) string {
	if e.respVal == nil {
		return text
	}
	result := e.respVal.Validate(text)
	if result.OK {
		return result.Text
	}
	for i, issue := range result.Issues {
		e.logger.Warn("响应校验问题",
			slog.String("issue", string(issue)),
			slog.String("reason", result.Reasons[i]),
		)
	}
	// 长度截断是可接受的修正，直接使用截断后的文本。
	// AI 身份泄露或提示泄露需要替换为安全回复。
	for _, issue := range result.Issues {
		if issue == guard.ResponseIssueAIDisclosure || issue == guard.ResponseIssuePromptLeak {
			return responseSafetyFallback
		}
	}
	return result.Text
}

// cleanContent 清洗 LLM 输出文本中不适合 TTS 合成的内容。
// 移除 JSON 片段、代码块、Markdown 格式、URL 等，确保文本适合语音播放。
// 检测到 PII 时仅记录日志，不阻塞发送。
func (e *Engine) cleanContent(text string) string {
	if e.contentChk == nil {
		return text
	}
	result := e.contentChk.Check(text)
	if result.OK {
		return result.Text
	}
	for i, issue := range result.Issues {
		e.logger.Warn("内容校验问题",
			slog.String("issue", string(issue)),
			slog.String("reason", result.Reasons[i]),
		)
	}
	// 清洗后文本为空时返回原始文本，避免丢失整段回复。
	if result.Text == "" {
		return text
	}
	return result.Text
}

// validateDecision 校验 LLM 结构化输出的意图和字段。
// 返回修正后的输出。
func (e *Engine) validateDecision(out rules.LLMOutput) rules.LLMOutput {
	if e.decVal == nil {
		return out
	}
	input := guard.DecisionInput{
		Intent:          out.Intent,
		Instructions:    out.SuggestedReply,
		ExtractedFields: out.ExtractedFields,
	}
	result := e.decVal.Validate(input)
	if !result.Valid {
		for _, v := range result.Violations {
			e.logger.Warn("决策校验违规", slog.String("violation", v))
		}
	}
	out.Intent = result.Sanitized.Intent
	out.ExtractedFields = result.Sanitized.ExtractedFields
	return out
}

// validateOutput 校验 LLM 输出在当前对话状态下是否合法。
// 检测当前状态下不允许的意图和不合理的结束通话请求，返回修正后的输出。
func (e *Engine) validateOutput(out rules.LLMOutput) rules.LLMOutput {
	if e.outChecker == nil {
		return out
	}
	input := guard.OutputCheckInput{
		Intent:       out.Intent,
		Instructions: out.SuggestedReply,
		State:        e.fsm.State(),
	}
	result := e.outChecker.Check(input)
	if !result.Valid {
		for _, v := range result.Violations {
			e.logger.Warn("输出状态校验违规", slog.String("violation", v))
		}
	}
	out.Intent = result.Sanitized.Intent
	return out
}

// offTopicConvergeReply 是连续离题时的收束话术。
const offTopicConvergeReply = "我们还是回到正题吧，请问您对我们的方案有什么想法？"

// offTopicEndReply 是离题达到上限时的结束话术。
const offTopicEndReply = "感谢您的时间，后续如有需要我们再联系您，再见！"

// checkOffTopic 记录本轮意图并检查离题状态，返回是否需要拦截及对应回复文本。
func (e *Engine) checkOffTopic(intent engine.Intent) (bool, string) {
	if e.offTopic == nil {
		return false, ""
	}
	action := e.offTopic.Record(intent)
	switch action {
	case guard.OffTopicEnd:
		e.logger.Warn("连续离题达到上限，结束通话",
			slog.Int("consecutive", e.offTopic.Consecutive()),
		)
		return true, offTopicEndReply
	case guard.OffTopicConverge:
		e.logger.Info("连续离题，收束对话",
			slog.Int("consecutive", e.offTopic.Consecutive()),
		)
		return true, offTopicConvergeReply
	default:
		return false, ""
	}
}

// recordOffTopicTurn 记录离题拦截时的对话轮次。
func (e *Engine) recordOffTopicTurn(stateBefore engine.DialogueState, userText, reply string, llmLatencyMs int) {
	e.recordBudget(userText, reply)
	botTurn := Turn{
		Number:       len(e.turns) + 1,
		Speaker:      "bot",
		Content:      reply,
		StateBefore:  stateBefore,
		StateAfter:   e.fsm.State(),
		LLMLatencyMs: llmLatencyMs,
	}
	e.turns = append(e.turns, botTurn)
}

// checkBudget 检查预算状态，返回是否需要拦截及对应回复文本。
// 返回 (应拦截, 回复文本)。
func (e *Engine) checkBudget() (bool, string) {
	if e.budget == nil {
		return false, ""
	}
	action := e.budget.Check()
	switch action {
	case guard.BudgetEnd:
		e.logger.Warn("预算耗尽，结束通话",
			slog.Int("used_tokens", e.budget.UsedTokens()),
			slog.Int("used_turns", e.budget.UsedTurns()),
		)
		return true, budgetFallbackReply
	case guard.BudgetDegrade:
		e.logger.Info("预算紧张，降级为模板回复",
			slog.Int("used_tokens", e.budget.UsedTokens()),
			slog.Int("used_turns", e.budget.UsedTurns()),
		)
		return true, budgetDegradeReply
	default:
		return false, ""
	}
}

// recordBudget 在 LLM 调用后记录 token 消耗和轮次。
func (e *Engine) recordBudget(userText, replyText string) {
	if e.budget == nil {
		return
	}
	tokens := guard.EstimateTokens(userText) + guard.EstimateTokens(replyText)
	e.budget.RecordTokens(tokens)
	e.budget.RecordTurn()
}

// ProcessUserInput 处理一轮用户语音输入。
func (e *Engine) ProcessUserInput(ctx context.Context, userText string) (string, error) {
	stateBefore := e.fsm.State()

	// 预算检查：耗尽或降级时跳过 LLM 调用。
	if intercept, reply := e.checkBudget(); intercept {
		e.recordBudgetTurn(stateBefore, userText, reply)
		return reply, nil
	}

	userTurn := Turn{
		Number:      len(e.turns) + 1,
		Speaker:     "user",
		Content:     userText,
		StateBefore: stateBefore,
		StateAfter:  stateBefore,
	}
	e.turns = append(e.turns, userTurn)

	llmStart := time.Now()
	llmOut, err := e.callLLM(ctx, userText)
	llmLatency := int(time.Since(llmStart).Milliseconds())

	if err != nil {
		e.logger.Error("LLM call failed, using fallback", slog.String("error", err.Error()))
		llmOut = rules.LLMOutput{
			Intent:     engine.IntentUnknown,
			Confidence: 0.0,
		}
	}

	// 校验 LLM 结构化输出（意图、字段等）。
	llmOut = e.validateDecision(llmOut)
	// 校验意图在当前对话状态下是否合法。
	llmOut = e.validateOutput(llmOut)

	// 离题检测：连续离题时收束或结束。
	if intercept, reply := e.checkOffTopic(llmOut.Intent); intercept {
		e.recordOffTopicTurn(stateBefore, userText, reply, llmLatency)
		return reply, nil
	}

	decision := e.ruleEngine.Evaluate(llmOut, e.dctx)

	_, fsmErr := e.fsm.Advance(e.dctx)
	if fsmErr != nil {
		e.logger.Warn("FSM advance failed",
			slog.String("error", fsmErr.Error()),
			slog.String("state", e.fsm.State().String()),
		)
	}

	replyText := decision.ReplyText
	if replyText == "" && llmOut.SuggestedReply != "" {
		replyText = llmOut.SuggestedReply
	}
	if replyText == "" {
		replyText = "好的，我了解了。"
	}

	// 校验响应文本安全性（AI 身份泄露、提示泄露、长度）。
	replyText = e.validateResponse(replyText)
	// 清洗不适合 TTS 合成的内容（JSON 片段、代码块、Markdown、URL、PII）。
	replyText = e.cleanContent(replyText)

	// 记录预算消耗。
	e.recordBudget(userText, replyText)

	botTurn := Turn{
		Number:       len(e.turns) + 1,
		Speaker:      "bot",
		Content:      replyText,
		StateBefore:  stateBefore,
		StateAfter:   e.fsm.State(),
		LLMLatencyMs: llmLatency,
	}
	e.turns = append(e.turns, botTurn)

	e.logger.Info("dialogue turn",
		slog.Int("turn", botTurn.Number),
		slog.String("state", fmt.Sprintf("%s→%s", stateBefore, e.fsm.State())),
		slog.String("intent", string(llmOut.Intent)),
		slog.String("reply_strategy", decision.ReplyStrategy.String()),
	)

	return replyText, nil
}

// recordBudgetTurn 记录预算拦截时的对话轮次。
func (e *Engine) recordBudgetTurn(stateBefore engine.DialogueState, userText, replyText string) {
	userTurn := Turn{
		Number:      len(e.turns) + 1,
		Speaker:     "user",
		Content:     userText,
		StateBefore: stateBefore,
		StateAfter:  stateBefore,
	}
	e.turns = append(e.turns, userTurn)

	e.recordBudget(userText, replyText)

	botTurn := Turn{
		Number:      len(e.turns) + 1,
		Speaker:     "bot",
		Content:     replyText,
		StateBefore: stateBefore,
		StateAfter:  e.fsm.State(),
	}
	e.turns = append(e.turns, botTurn)

	e.logger.Info("预算拦截轮次",
		slog.Int("turn", botTurn.Number),
		slog.String("reply", replyText),
	)
}

// GetOpeningText 返回开场问候语文本。
func (e *Engine) GetOpeningText() string {
	if tmpl, ok := e.prompts["OPENING"]; ok {
		var buf strings.Builder
		data := TemplateData{CurrentState: "OPENING"}
		if err := tmpl.Execute(&buf, data); err == nil {
			return buf.String()
		}
	}
	return "您好，打扰您一分钟。"
}

// State 返回当前对话状态。
func (e *Engine) State() engine.DialogueState {
	return e.fsm.State()
}

// IsFinished 当对话已结束时返回 true。
func (e *Engine) IsFinished() bool {
	return e.fsm.IsTerminal()
}

// Result 生成最终会话结果。
func (e *Engine) Result(callStatus engine.CallStatus) SessionResult {
	grade := e.ruleEngine.GradeCall(e.dctx, callStatus)

	return SessionResult{
		Grade:           grade,
		CollectedFields: e.dctx.CollectedFields,
		MissingFields:   e.dctx.MissingFields(),
		TurnCount:       e.dctx.TurnCount,
		Turns:           e.turns,
		ShouldNotify:    e.dctx.HighValue,
	}
}

// Turns 返回所有已记录的对话轮次。
func (e *Engine) Turns() []Turn {
	return e.turns
}

// Budget 返回预算跟踪器，未配置时返回 nil。
func (e *Engine) Budget() *guard.CallBudget {
	return e.budget
}

// SnapshotData 持有从快照恢复对话引擎所需的最小数据。
type SnapshotData struct {
	// DialogueState 中断时的对话状态名称（如 "OPENING"、"INFORMATION_GATHERING"）。
	DialogueState string
	// Turns 中断前的最近对话轮次。
	Turns []Turn
	// CollectedFields 已收集的字段。
	CollectedFields map[string]string
}

// RestoreFromSnapshot 从快照数据恢复对话引擎状态。
// 恢复 FSM 状态、对话轮次和已收集字段，使后续对话从中断处继续。
func (e *Engine) RestoreFromSnapshot(data SnapshotData) {
	// 恢复 FSM 状态。
	if state, ok := engine.ParseDialogueState(data.DialogueState); ok {
		e.fsm.ForceState(state)
	}

	// 恢复对话轮次。
	if len(data.Turns) > 0 {
		e.turns = make([]Turn, len(data.Turns))
		copy(e.turns, data.Turns)
		e.dctx.TurnCount = len(data.Turns) / 2 // 每轮包含 user+bot 两条。
	}

	// 恢复已收集字段。
	maps.Copy(e.dctx.CollectedFields, data.CollectedFields)
}

// recoveryOpeningText 恢复呼叫的开场白。
const recoveryOpeningText = "您好，刚才电话好像断了，我们接着聊。"

// GetRecoveryOpeningText 返回恢复呼叫的开场白。
func (e *Engine) GetRecoveryOpeningText() string {
	return recoveryOpeningText
}

func (e *Engine) callLLM(ctx context.Context, userText string) (rules.LLMOutput, error) {
	if e.llm == nil {
		return rules.LLMOutput{Intent: engine.IntentUnknown}, nil
	}

	messages := e.buildMessages(userText)

	cfg := provider.LLMConfig{
		MaxTokens:   512,
		Temperature: 0.7,
	}

	response, err := e.llm.Generate(ctx, messages, cfg)
	if err != nil {
		return rules.LLMOutput{}, fmt.Errorf("llm generate: %w", err)
	}

	var out rules.LLMOutput
	if parseErr := json.Unmarshal([]byte(response), &out); parseErr != nil {
		// 优雅降级：JSON 解析失败时使用 LLM 原始响应。
		out = rules.LLMOutput{
			Intent:         engine.IntentUnknown,
			SuggestedReply: response,
			Confidence:     0.3,
		}
	}

	return out, nil
}

func (e *Engine) buildMessages(userText string) []provider.Message {
	messages := []provider.Message{
		{Role: "system", Content: e.buildSystemPrompt()},
	}

	for _, turn := range e.recentTurns() {
		role := "user"
		if turn.Speaker == "bot" {
			role = "assistant"
		}
		messages = append(messages, provider.Message{Role: role, Content: turn.Content})
	}

	messages = append(messages, provider.Message{Role: "user", Content: userText})
	return messages
}

func (e *Engine) buildSystemPrompt() string {
	state := e.fsm.State().String()

	// 基础系统提示词。
	base := e.systemPrompt
	if base == "" {
		base = "你是一个友好的AI电话助手。"
	}

	// 拼接状态上下文和结构化输出指令。
	return fmt.Sprintf(`%s

当前对话状态: %s
轮次: %d

你必须返回 JSON 格式，包含以下字段：
{
  "intent": "意图",
  "extracted_fields": {},
  "suggested_reply": "你的回复内容",
  "confidence": 0.8
}

intent 必须是以下之一：
- "confirm" — 用户确认/同意
- "continue" — 用户愿意继续
- "interested" — 用户感兴趣
- "ask_detail" — 用户追问细节
- "hesitate" — 用户犹豫
- "busy" — 用户说忙/没时间
- "not_interested" — 用户不感兴趣
- "reject" — 用户明确拒绝
- "schedule" — 用户想预约时间

suggested_reply 是你回复用户的自然语言内容，要简短、口语化。
只返回 JSON，不要返回其他内容。`, base, state, e.dctx.TurnCount)
}

// ProcessUserInputStream 流式处理用户输入，按句返回回复文本。
// 使用 LLM 流式生成，按中文标点分句后逐句发送到返回的通道。
// 通道关闭表示所有句子已发送。调用方应从通道读取句子并逐句合成 TTS。
func (e *Engine) ProcessUserInputStream(ctx context.Context, userText string) (<-chan string, error) {
	if e.llm == nil {
		ch := make(chan string, 1)
		ch <- "好的，我了解了。"
		close(ch)
		return ch, nil
	}

	// 预算检查：耗尽或降级时跳过 LLM 调用，直接返回模板回复。
	if intercept, reply := e.checkBudget(); intercept {
		stateBefore := e.fsm.State()
		e.recordBudgetTurn(stateBefore, userText, reply)
		ch := make(chan string, 1)
		ch <- reply
		close(ch)
		return ch, nil
	}

	stateBefore := e.fsm.State()

	userTurn := Turn{
		Number:      len(e.turns) + 1,
		Speaker:     "user",
		Content:     userText,
		StateBefore: stateBefore,
		StateAfter:  stateBefore,
	}
	e.turns = append(e.turns, userTurn)

	llmStart := time.Now()
	messages := e.buildStreamMessages(userText)

	cfg := provider.LLMConfig{
		MaxTokens:   512,
		Temperature: 0.7,
	}

	tokenCh, err := e.llm.GenerateStream(ctx, messages, cfg)
	if err != nil {
		return nil, fmt.Errorf("llm stream: %w", err)
	}

	sentenceCh := make(chan string, 8)
	go func() {
		defer close(sentenceCh)

		replyText := e.streamTokensToSentences(ctx, tokenCh, sentenceCh)
		e.recordStreamTurn(stateBefore, replyText, llmStart)
		// 记录预算消耗。
		e.recordBudget(userText, replyText)
	}()

	return sentenceCh, nil
}

// 分句常量。
const (
	// sentenceBreakers 句级分隔符，始终触发分句。
	sentenceBreakers = "。！？；.!?;"
	// clauseBreakers 子句级分隔符，累积足够长度后触发。
	// 子句级切分将首段 TTS 延迟从 ~300ms 降至 ~100ms。
	clauseBreakers = "，、,：:"
	// minClauseRunes 子句级分隔触发的最小字符数。
	// 避免产生过短片段（如"嗯，"），确保 TTS 合成质量。
	minClauseRunes = 6
)

// streamTokensToSentences 将 LLM token 流按标点分段后发送到 sentenceCh。
// 使用两级切分策略：句级标点（。！？）始终触发；子句级标点（，、）在
// 累积足够长度后触发，使 TTS 尽早开始合成，降低首段音频延迟。
// 返回完整的回复文本。
func (e *Engine) streamTokensToSentences(ctx context.Context, tokenCh <-chan string, sentenceCh chan<- string) string {
	var buf strings.Builder
	var fullText strings.Builder
	var runeCount int

	for tok := range tokenCh {
		buf.WriteString(tok)
		fullText.WriteString(tok)
		runeCount += utf8.RuneCountInString(tok)

		text := buf.String()

		// 两级切分：句级标点始终触发，子句级标点需累积足够长度。
		shouldSplit := strings.ContainsAny(text, sentenceBreakers) ||
			(runeCount >= minClauseRunes && strings.ContainsAny(text, clauseBreakers))
		if !shouldSplit {
			continue
		}

		// 过滤舞台指令（如"（停顿）""（语气温和）"），避免 TTS 合成无意义文本。
		sentence := strings.TrimSpace(stageDirectionRe.ReplaceAllString(text, ""))
		if sentence != "" {
			// 校验响应安全性（AI 身份泄露、提示泄露、长度）。
			sentence = e.validateResponse(sentence)
			// 清洗不适合 TTS 合成的内容。
			sentence = e.cleanContent(sentence)
			select {
			case sentenceCh <- sentence:
			case <-ctx.Done():
				return fullText.String()
			}
		}
		buf.Reset()
		runeCount = 0
	}

	// 发送剩余文本。
	if remaining := strings.TrimSpace(stageDirectionRe.ReplaceAllString(buf.String(), "")); remaining != "" {
		// 校验响应安全性。
		remaining = e.validateResponse(remaining)
		// 清洗不适合 TTS 合成的内容。
		remaining = e.cleanContent(remaining)
		select {
		case sentenceCh <- remaining:
		case <-ctx.Done():
		}
	}

	return fullText.String()
}

// recordStreamTurn 记录流式对话轮次并推进 FSM。
func (e *Engine) recordStreamTurn(stateBefore engine.DialogueState, replyText string, llmStart time.Time) {
	llmLatency := int(time.Since(llmStart).Milliseconds())

	e.dctx.TurnCount++
	if _, fsmErr := e.fsm.Advance(e.dctx); fsmErr != nil {
		e.logger.Warn("FSM advance failed (stream)",
			slog.String("error", fsmErr.Error()),
			slog.String("state", e.fsm.State().String()),
		)
	}

	botTurn := Turn{
		Number:       len(e.turns) + 1,
		Speaker:      "bot",
		Content:      replyText,
		StateBefore:  stateBefore,
		StateAfter:   e.fsm.State(),
		LLMLatencyMs: llmLatency,
	}
	e.turns = append(e.turns, botTurn)

	e.logger.Info("dialogue stream turn",
		slog.Int("turn", botTurn.Number),
		slog.String("state", fmt.Sprintf("%s→%s", stateBefore, e.fsm.State())),
		slog.Int("llm_ms", llmLatency),
	)
}

// PrepareStream 预准备流式处理，不记录对话轮次。
// 用于基于 partial ASR 的预推理：在 ASR 尚未出 Final 时提前启动 LLM。
// 返回句子通道和确认函数。确认函数在所有句段消费完成后调用，
// 补充记录对话轮次和推进 FSM。若预推理被取消（ctx 取消），不会产生副作用。
func (e *Engine) PrepareStream(ctx context.Context, userText string) (<-chan string, func(), error) {
	if e.llm == nil {
		ch := make(chan string, 1)
		ch <- "好的，我了解了。"
		close(ch)
		return ch, func() {}, nil
	}

	// 预算检查：耗尽或降级时跳过 LLM 调用。
	if intercept, reply := e.checkBudget(); intercept {
		stateBefore := e.fsm.State()
		ch := make(chan string, 1)
		ch <- reply
		close(ch)
		commitFn := func() {
			e.recordBudgetTurn(stateBefore, userText, reply)
		}
		return ch, commitFn, nil
	}

	stateBefore := e.fsm.State()
	llmStart := time.Now()

	messages := e.buildStreamMessages(userText)
	cfg := provider.LLMConfig{MaxTokens: 512, Temperature: 0.7}

	tokenCh, err := e.llm.GenerateStream(ctx, messages, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("llm stream (speculative): %w", err)
	}

	out := make(chan string, 8)
	var replyText string
	done := make(chan struct{})

	go func() {
		defer close(out)
		defer close(done)
		replyText = e.streamTokensToSentences(ctx, tokenCh, out)
	}()

	commitFn := func() {
		// 等待流式处理完成，确保 replyText 已写入。
		<-done

		userTurn := Turn{
			Number:      len(e.turns) + 1,
			Speaker:     "user",
			Content:     userText,
			StateBefore: stateBefore,
			StateAfter:  stateBefore,
		}
		e.turns = append(e.turns, userTurn)
		e.recordStreamTurn(stateBefore, replyText, llmStart)
		// 记录预算消耗。
		e.recordBudget(userText, replyText)
	}

	return out, commitFn, nil
}

// buildStreamMessages 构建流式 LLM 的消息列表。
// 与 buildMessages 不同的是系统提示词要求纯文本回复，不要 JSON。
func (e *Engine) buildStreamMessages(userText string) []provider.Message {
	state := e.fsm.State().String()

	base := e.systemPrompt
	if base == "" {
		base = "你是一个友好的AI电话助手。"
	}

	systemPrompt := fmt.Sprintf(`%s

当前对话状态: %s
轮次: %d

请直接用自然语言回复用户，简短、口语化，像打电话一样自然。
不要返回 JSON，不要加任何格式标记。
禁止使用括号描述动作或情绪，如"（停顿）""（语气温和）"等。只输出要说的话。`, base, state, e.dctx.TurnCount)

	messages := []provider.Message{
		{Role: "system", Content: systemPrompt},
	}

	for _, turn := range e.recentTurns() {
		role := "user"
		if turn.Speaker == "bot" {
			role = "assistant"
		}
		messages = append(messages, provider.Message{Role: role, Content: turn.Content})
	}

	messages = append(messages, provider.Message{Role: "user", Content: userText})
	return messages
}

func (e *Engine) recentTurns() []Turn {
	if len(e.turns) <= e.maxHistory*2 {
		return e.turns
	}
	return e.turns[len(e.turns)-e.maxHistory*2:]
}
