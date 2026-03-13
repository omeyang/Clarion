package guard

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// ResponseIssue 描述响应文本校验中发现的问题类型。
type ResponseIssue string

// 响应校验问题类型。
const (
	// ResponseIssueTooLong 响应文本超过长度上限。
	ResponseIssueTooLong ResponseIssue = "too_long"
	// ResponseIssueAIDisclosure 响应文本包含 AI 身份泄露。
	ResponseIssueAIDisclosure ResponseIssue = "ai_disclosure"
	// ResponseIssuePromptLeak 响应文本包含系统提示泄露。
	ResponseIssuePromptLeak ResponseIssue = "prompt_leak"
	// ResponseIssueEmpty 响应文本为空。
	ResponseIssueEmpty ResponseIssue = "empty"
)

// ResponseCheckResult 是响应文本校验的结果。
type ResponseCheckResult struct {
	// OK 为 true 表示通过全部校验。
	OK bool
	// Text 是校验/修正后的文本。
	Text string
	// Issues 记录所有发现的问题。
	Issues []ResponseIssue
	// Reasons 对应每个问题的描述。
	Reasons []string
}

// ResponseValidatorConfig 配置响应文本校验器。
type ResponseValidatorConfig struct {
	// MaxResponseRunes 单条响应最大字符数。0 使用默认值。
	MaxResponseRunes int
	// ExtraAIPatterns 额外的 AI 身份泄露检测正则。
	ExtraAIPatterns []string
	// ExtraLeakPatterns 额外的系统提示泄露检测正则。
	ExtraLeakPatterns []string
}

// 默认配置值。
const (
	defaultMaxResponseRunes = 100
)

// aiDisclosurePatterns 检测 AI 身份泄露。
var aiDisclosurePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)我是.{0,5}(AI|人工智能|机器人|语音助手|虚拟助手)`),
	regexp.MustCompile(`(?i)作为.{0,5}(AI|人工智能|机器人)`),
	regexp.MustCompile(`(?i)I'?\s*am\s+(an?\s+)?(AI|artificial|robot|bot|virtual)`),
	regexp.MustCompile(`(?i)我(不是人|没有感情|是程序)`),
	regexp.MustCompile(`(?i)(ChatGPT|GPT|Claude|DeepSeek|大语言模型|LLM)`),
}

// promptLeakPatterns 检测系统提示/指令泄露。
var promptLeakPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)系统提示`),
	regexp.MustCompile(`(?i)system\s*prompt`),
	regexp.MustCompile(`(?i)我的指令(是|要求)`),
	regexp.MustCompile(`(?i)我被(要求|设定|编程)`),
	regexp.MustCompile(`(?i)(prompt|instruction).{0,10}(say|tell|ask)`),
	regexp.MustCompile(`(?i)严格限制`),
}

// ResponseValidator 校验 LLM 响应文本是否适合在电话中播放。
// 检查长度、AI 身份泄露、系统提示泄露等。
type ResponseValidator struct {
	maxResponseRunes int
	aiPatterns       []*regexp.Regexp
	leakPatterns     []*regexp.Regexp
}

// NewResponseValidator 创建响应文本校验器。
func NewResponseValidator(cfg ResponseValidatorConfig) *ResponseValidator {
	maxRunes := cfg.MaxResponseRunes
	if maxRunes <= 0 {
		maxRunes = defaultMaxResponseRunes
	}

	aiPats := compileAndMerge(aiDisclosurePatterns, cfg.ExtraAIPatterns)
	leakPats := compileAndMerge(promptLeakPatterns, cfg.ExtraLeakPatterns)

	return &ResponseValidator{
		maxResponseRunes: maxRunes,
		aiPatterns:       aiPats,
		leakPatterns:     leakPats,
	}
}

// Validate 校验响应文本。返回校验结果，包含修正后的文本。
func (v *ResponseValidator) Validate(text string) ResponseCheckResult {
	result := ResponseCheckResult{OK: true, Text: text}

	text = strings.TrimSpace(text)
	result.Text = text

	if text == "" {
		result.addIssue(ResponseIssueEmpty, "响应文本为空")
		return result
	}

	v.checkLength(&result)
	v.checkAIDisclosure(&result)
	v.checkPromptLeak(&result)

	return result
}

// checkLength 检查并截断过长的响应文本。
func (v *ResponseValidator) checkLength(r *ResponseCheckResult) {
	if utf8.RuneCountInString(r.Text) <= v.maxResponseRunes {
		return
	}
	runes := []rune(r.Text)
	r.Text = string(runes[:v.maxResponseRunes])
	r.addIssue(ResponseIssueTooLong, "响应超过长度上限，已截断")
}

// checkAIDisclosure 检测 AI 身份泄露。
func (v *ResponseValidator) checkAIDisclosure(r *ResponseCheckResult) {
	for _, re := range v.aiPatterns {
		if re.MatchString(r.Text) {
			r.addIssue(ResponseIssueAIDisclosure, "响应包含 AI 身份泄露")
			return
		}
	}
}

// checkPromptLeak 检测系统提示泄露。
func (v *ResponseValidator) checkPromptLeak(r *ResponseCheckResult) {
	for _, re := range v.leakPatterns {
		if re.MatchString(r.Text) {
			r.addIssue(ResponseIssuePromptLeak, "响应包含系统提示泄露")
			return
		}
	}
}

// addIssue 添加问题记录并标记为未通过。
func (r *ResponseCheckResult) addIssue(issue ResponseIssue, reason string) {
	r.OK = false
	r.Issues = append(r.Issues, issue)
	r.Reasons = append(r.Reasons, reason)
}

// compileAndMerge 合并预编译正则和额外正则字符串。
func compileAndMerge(base []*regexp.Regexp, extra []string) []*regexp.Regexp {
	result := make([]*regexp.Regexp, len(base), len(base)+len(extra))
	copy(result, base)
	for _, p := range extra {
		if re, err := regexp.Compile(p); err == nil {
			result = append(result, re)
		}
	}
	return result
}
