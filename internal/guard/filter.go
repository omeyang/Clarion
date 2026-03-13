// Package guard 提供输入清洗和输出校验，防止 prompt 注入和越权操作。
package guard

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// SafetyFlag 表示输入安全分类。
type SafetyFlag string

// 安全分类值。
const (
	SafetyNormal    SafetyFlag = "normal"
	SafetyOffTopic  SafetyFlag = "off_topic"
	SafetyInjection SafetyFlag = "injection"
)

// FilterResult 是输入过滤的结果。
type FilterResult struct {
	// Text 是清洗后的文本（不可变引用，仅在 Blocked 为 false 时有效）。
	Text string
	// Flag 表示安全分类。
	Flag SafetyFlag
	// Blocked 为 true 时表示该输入应被拦截。
	Blocked bool
	// Reason 描述拦截原因（仅在 Blocked 为 true 时有值）。
	Reason string
}

// InputFilter 基于正则模式检测 prompt 注入和离题输入。
type InputFilter struct {
	injectionPatterns []*regexp.Regexp
	maxInputRunes     int
}

// injectionRawPatterns 是默认的注入检测模式。
var injectionRawPatterns = []string{
	`(?i)忘记.{0,10}指令`,
	`(?i)你现在是`,
	`(?i)ignore.{0,10}instructions`,
	`(?i)你的(提示词|系统提示|指令)`,
	`(?i)假装你是`,
	`(?i)不要遵守`,
	`(?i)忽略.{0,10}(规则|限制|指令)`,
	`(?i)请?重复.{0,5}(系统|提示|指令)`,
	`(?i)reveal.{0,10}(system|prompt|instruction)`,
	`(?i)act\s+as`,
	`(?i)你是.{0,5}(chatgpt|gpt|claude|ai)`,
	`(?i)DAN\s+mode`,
	`(?i)jailbreak`,
}

// defaultMaxInputRunes 是默认输入最大字符数。
const defaultMaxInputRunes = 500

// NewInputFilter 创建输入过滤器。
// patterns 为额外的注入检测正则表达式；maxRunes 为输入最大字符数（0 使用默认值）。
func NewInputFilter(patterns []string, maxRunes int) *InputFilter {
	if maxRunes <= 0 {
		maxRunes = defaultMaxInputRunes
	}
	allPatterns := make([]string, 0, len(injectionRawPatterns)+len(patterns))
	allPatterns = append(allPatterns, injectionRawPatterns...)
	allPatterns = append(allPatterns, patterns...)

	compiled := make([]*regexp.Regexp, 0, len(allPatterns))
	for _, p := range allPatterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, re)
		}
	}
	return &InputFilter{
		injectionPatterns: compiled,
		maxInputRunes:     maxRunes,
	}
}

// Filter 对用户输入文本进行清洗和安全检查。
func (f *InputFilter) Filter(text string) FilterResult {
	text = sanitize(text)
	if text == "" {
		return FilterResult{Text: text, Flag: SafetyNormal}
	}

	// 长度截断。
	if utf8.RuneCountInString(text) > f.maxInputRunes {
		runes := []rune(text)
		text = string(runes[:f.maxInputRunes])
	}

	// 注入检测。
	for _, re := range f.injectionPatterns {
		if re.MatchString(text) {
			return FilterResult{
				Text:    text,
				Flag:    SafetyInjection,
				Blocked: true,
				Reason:  "检测到疑似 prompt 注入",
			}
		}
	}

	return FilterResult{Text: text, Flag: SafetyNormal}
}

// sanitize 清洗输入文本：去除首尾空白，折叠连续空白，移除控制字符。
func sanitize(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	prevSpace := false
	for _, r := range text {
		// 空白字符折叠。
		if r == ' ' || r == '\n' || r == '\t' {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		// 跳过控制字符。
		if r < 32 {
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
