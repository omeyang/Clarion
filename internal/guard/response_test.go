package guard

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDefaultResponseValidator() *ResponseValidator {
	return NewResponseValidator(ResponseValidatorConfig{})
}

func TestResponseValidator_NormalText(t *testing.T) {
	v := newDefaultResponseValidator()
	tests := []struct {
		name string
		text string
	}{
		{"简短问候", "您好，请问是张先生吗？"},
		{"业务介绍", "我们公司最近推出了一款新产品"},
		{"确认信息", "好的，我记录一下"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Validate(tt.text)
			assert.True(t, r.OK, "正常文本应通过: %s", tt.text)
			assert.Empty(t, r.Issues)
			assert.Equal(t, tt.text, r.Text)
		})
	}
}

func TestResponseValidator_EmptyText(t *testing.T) {
	v := newDefaultResponseValidator()
	tests := []struct {
		name string
		text string
	}{
		{"空字符串", ""},
		{"仅空白", "   \t\n  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Validate(tt.text)
			assert.False(t, r.OK)
			require.Len(t, r.Issues, 1)
			assert.Equal(t, ResponseIssueEmpty, r.Issues[0])
		})
	}
}

func TestResponseValidator_TooLong(t *testing.T) {
	v := NewResponseValidator(ResponseValidatorConfig{MaxResponseRunes: 10})
	r := v.Validate("一二三四五六七八九十额外内容")
	assert.False(t, r.OK)
	assert.Contains(t, r.Issues, ResponseIssueTooLong)
	assert.Len(t, []rune(r.Text), 10)
}

func TestResponseValidator_DefaultMaxResponseRunes(t *testing.T) {
	v := newDefaultResponseValidator()
	long := strings.Repeat("字", defaultMaxResponseRunes+50)
	r := v.Validate(long)
	assert.False(t, r.OK)
	assert.Contains(t, r.Issues, ResponseIssueTooLong)
	assert.Len(t, []rune(r.Text), defaultMaxResponseRunes)
}

func TestResponseValidator_AIDisclosure(t *testing.T) {
	v := newDefaultResponseValidator()
	tests := []struct {
		name string
		text string
	}{
		{"我是AI", "我是AI助手，有什么可以帮您"},
		{"我是人工智能", "我是人工智能"},
		{"我是机器人", "其实我是机器人"},
		{"作为AI", "作为一个AI，我无法判断"},
		{"I am an AI", "I am an AI assistant"},
		{"我不是人", "对不起我不是人"},
		{"我是程序", "我是程序生成的"},
		{"提到ChatGPT", "这个问题可以问ChatGPT"},
		{"提到大语言模型", "我是大语言模型"},
		{"提到DeepSeek", "我使用DeepSeek技术"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Validate(tt.text)
			assert.False(t, r.OK, "AI 泄露应被检出: %s", tt.text)
			assert.Contains(t, r.Issues, ResponseIssueAIDisclosure)
		})
	}
}

func TestResponseValidator_PromptLeak(t *testing.T) {
	v := newDefaultResponseValidator()
	tests := []struct {
		name string
		text string
	}{
		{"系统提示", "我的系统提示里写了..."},
		{"system prompt", "my system prompt says"},
		{"我的指令是", "我的指令是要推销产品"},
		{"我被要求", "我被要求这样说的"},
		{"我被设定", "我被设定为销售角色"},
		{"严格限制", "我有严格限制不能说"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Validate(tt.text)
			assert.False(t, r.OK, "提示泄露应被检出: %s", tt.text)
			assert.Contains(t, r.Issues, ResponseIssuePromptLeak)
		})
	}
}

func TestResponseValidator_NormalNotFalsePositive(t *testing.T) {
	v := newDefaultResponseValidator()
	// 确保正常业务文本不会误判。
	tests := []struct {
		name string
		text string
	}{
		{"问候语", "您好，我是某公司的客服小王"},
		{"产品介绍", "这款产品使用了最新的技术"},
		{"确认身份", "请问您是李先生本人吗"},
		{"报价信息", "这个套餐每月只需要99元"},
		{"约时间", "请问您明天下午方便吗"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Validate(tt.text)
			assert.True(t, r.OK, "正常文本不应误判: %s", tt.text)
		})
	}
}

func TestResponseValidator_MultipleIssues(t *testing.T) {
	v := NewResponseValidator(ResponseValidatorConfig{MaxResponseRunes: 15})
	// 既超长又包含 AI 泄露。
	r := v.Validate("你好我是人工智能助手请问有什么可以帮到您的吗")
	assert.False(t, r.OK)
	assert.GreaterOrEqual(t, len(r.Issues), 2)
}

func TestResponseValidator_CustomExtraPatterns(t *testing.T) {
	v := NewResponseValidator(ResponseValidatorConfig{
		ExtraAIPatterns:   []string{`(?i)智能客服`},
		ExtraLeakPatterns: []string{`(?i)配置文件`},
	})

	r := v.Validate("我是智能客服")
	assert.False(t, r.OK)
	assert.Contains(t, r.Issues, ResponseIssueAIDisclosure)

	r = v.Validate("根据配置文件的设定")
	assert.False(t, r.OK)
	assert.Contains(t, r.Issues, ResponseIssuePromptLeak)
}

func TestResponseValidator_InvalidExtraPatternIgnored(t *testing.T) {
	// 无效正则不应导致 panic。
	v := NewResponseValidator(ResponseValidatorConfig{
		ExtraAIPatterns: []string{`(?P<invalid`},
	})
	r := v.Validate("正常文本")
	assert.True(t, r.OK)
}

func TestResponseValidator_TrimWhitespace(t *testing.T) {
	v := newDefaultResponseValidator()
	r := v.Validate("  你好  ")
	assert.True(t, r.OK)
	assert.Equal(t, "你好", r.Text)
}

func TestResponseIssue_Values(t *testing.T) {
	// 确保枚举值不重复。
	issues := []ResponseIssue{
		ResponseIssueTooLong,
		ResponseIssueAIDisclosure,
		ResponseIssuePromptLeak,
		ResponseIssueEmpty,
	}
	seen := make(map[ResponseIssue]bool, len(issues))
	for _, issue := range issues {
		assert.False(t, seen[issue], "重复的问题类型: %s", issue)
		seen[issue] = true
	}
}
