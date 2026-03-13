package guard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDefaultContentChecker() *ContentChecker {
	return NewContentChecker(ContentCheckerConfig{})
}

func TestContentChecker_NormalText(t *testing.T) {
	c := newDefaultContentChecker()
	tests := []struct {
		name string
		text string
	}{
		{"简短问候", "您好，请问是张先生吗？"},
		{"业务介绍", "我们公司推出了一款新产品，价格非常实惠"},
		{"确认信息", "好的，我记录一下您的需求"},
		{"空字符串", ""},
		{"纯空白", "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := c.Check(tt.text)
			assert.True(t, r.OK, "正常文本应通过: %q", tt.text)
			assert.Empty(t, r.Issues)
		})
	}
}

func TestContentChecker_JSONArtifact(t *testing.T) {
	c := newDefaultContentChecker()
	tests := []struct {
		name string
		text string
	}{
		{"完整JSON对象", `好的 {"intent": "continue", "reply": "你好"}`},
		{"键值对", `"intent": "continue" 这是回复`},
		{"布尔值", `"should_end": true`},
		{"数字值", `"confidence": 0.9`},
		{"null值", `"grade": null`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := c.Check(tt.text)
			assert.False(t, r.OK, "JSON 片段应被检出: %s", tt.text)
			assert.Contains(t, r.Issues, ContentIssueJSONArtifact)
		})
	}
}

func TestContentChecker_CodeBlock(t *testing.T) {
	c := newDefaultContentChecker()

	t.Run("三引号代码块", func(t *testing.T) {
		text := "请看代码：```go\nfmt.Println(\"hello\")\n```结束"
		r := c.Check(text)
		assert.False(t, r.OK)
		assert.Contains(t, r.Issues, ContentIssueCodeBlock)
		assert.NotContains(t, r.Text, "```")
	})

	t.Run("行内代码", func(t *testing.T) {
		text := "使用 `fmt.Println` 函数"
		r := c.Check(text)
		assert.False(t, r.OK)
		assert.Contains(t, r.Issues, ContentIssueCodeBlock)
		assert.NotContains(t, r.Text, "`")
		assert.Contains(t, r.Text, "fmt.Println")
	})
}

func TestContentChecker_Markdown(t *testing.T) {
	c := newDefaultContentChecker()
	tests := []struct {
		name     string
		text     string
		notInOut string // 清理后不应包含的内容
	}{
		{"加粗", "这是**重要**信息", "**"},
		{"标题", "# 产品介绍\n这是我们的产品", "#"},
		{"链接", "详情请见[官网](https://example.com)", "(https://"},
		{"列表", "- 第一点\n- 第二点", "- "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := c.Check(tt.text)
			assert.False(t, r.OK, "Markdown 应被检出: %s", tt.text)
			assert.Contains(t, r.Issues, ContentIssueMarkdown)
			assert.NotContains(t, r.Text, tt.notInOut)
		})
	}
}

func TestContentChecker_URL(t *testing.T) {
	c := newDefaultContentChecker()
	tests := []struct {
		name string
		text string
	}{
		{"HTTP链接", "请访问 http://example.com 了解详情"},
		{"HTTPS链接", "详情请见 https://www.example.com/path?q=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := c.Check(tt.text)
			assert.False(t, r.OK, "URL 应被检出: %s", tt.text)
			assert.Contains(t, r.Issues, ContentIssueURL)
			assert.NotContains(t, r.Text, "http")
		})
	}
}

func TestContentChecker_PII(t *testing.T) {
	c := newDefaultContentChecker()
	tests := []struct {
		name string
		text string
	}{
		{"手机号", "我的电话是13812345678"},
		{"邮箱", "请发送到 test@example.com"},
		{"身份证号", "身份证号是110101199001011234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := c.Check(tt.text)
			assert.False(t, r.OK, "PII 应被检出: %s", tt.text)
			assert.Contains(t, r.Issues, ContentIssuePII)
		})
	}
}

func TestContentChecker_NormalNotFalsePositive(t *testing.T) {
	c := newDefaultContentChecker()
	tests := []struct {
		name string
		text string
	}{
		{"包含数字但不是手机号", "我们的产品编号是12345"},
		{"包含@但不是邮箱", "价格是100元@天"},
		{"包含引号", `他说"你好"就走了`},
		{"普通花括号", "营业时间{周一到周五}"},
		{"短数字", "价格是999元"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := c.Check(tt.text)
			assert.True(t, r.OK, "正常文本不应误判: %s", tt.text)
		})
	}
}

func TestContentChecker_MultipleIssues(t *testing.T) {
	c := newDefaultContentChecker()
	text := "```代码```请访问 https://example.com"
	r := c.Check(text)
	assert.False(t, r.OK)
	assert.GreaterOrEqual(t, len(r.Issues), 2)
}

func TestContentChecker_CustomPIIPatterns(t *testing.T) {
	c := NewContentChecker(ContentCheckerConfig{
		ExtraPIIPatterns: []string{`(?i)银行卡.{0,5}\d{16,19}`},
	})
	r := c.Check("银行卡号1234567890123456")
	assert.False(t, r.OK)
	assert.Contains(t, r.Issues, ContentIssuePII)
}

func TestContentChecker_InvalidCustomPatternIgnored(t *testing.T) {
	// 无效正则不应导致 panic。
	c := NewContentChecker(ContentCheckerConfig{
		ExtraPIIPatterns: []string{`(?P<invalid`},
	})
	r := c.Check("正常文本")
	assert.True(t, r.OK)
}

func TestContentChecker_CodeBlockRemoval(t *testing.T) {
	c := newDefaultContentChecker()
	text := "前面的话```json\n{\"key\": \"value\"}\n```后面的话"
	r := c.Check(text)
	assert.Contains(t, r.Text, "前面的话")
	assert.Contains(t, r.Text, "后面的话")
	assert.NotContains(t, r.Text, "```")
}

func TestContentChecker_MarkdownLinkPreservesText(t *testing.T) {
	c := newDefaultContentChecker()
	text := "请查看[产品手册](https://example.com/docs)"
	r := c.Check(text)
	assert.Contains(t, r.Text, "产品手册")
	assert.NotContains(t, r.Text, "https://")
}

func TestContentChecker_URLRemovalNormalizesSpaces(t *testing.T) {
	c := newDefaultContentChecker()
	text := "请访问  https://example.com  了解详情"
	r := c.Check(text)
	assert.NotContains(t, r.Text, "  ")
}

func TestContentIssue_Values(t *testing.T) {
	// 确保枚举值不重复。
	issues := []ContentIssue{
		ContentIssueJSONArtifact,
		ContentIssueCodeBlock,
		ContentIssueMarkdown,
		ContentIssueURL,
		ContentIssuePII,
	}
	seen := make(map[ContentIssue]bool, len(issues))
	for _, issue := range issues {
		assert.False(t, seen[issue], "重复的问题类型: %s", issue)
		seen[issue] = true
	}
}

func TestCleanMarkdown(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"去除加粗", "这是**重要**信息", "这是重要信息"},
		{"去除标题", "# 标题\n内容", "标题\n内容"},
		{"去除链接保留文本", "[官网](https://example.com)", "官网"},
		{"去除列表标记", "- 项目一\n- 项目二", "项目一\n项目二"},
		{"普通文本不变", "这是普通文本", "这是普通文本"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanMarkdown(tt.text)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeSpaces(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"连续空格", "你好  世界", "你好 世界"},
		{"首尾空白", "  你好  ", "你好"},
		{"tab和换行", "你好\t\n世界", "你好 世界"},
		{"正常文本", "你好世界", "你好世界"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSpaces(tt.text)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContentChecker_EmptyAfterClean(t *testing.T) {
	c := newDefaultContentChecker()
	// 代码块移除后只剩空白。
	r := c.Check("```\nsome code\n```")
	require.False(t, r.OK)
	assert.Equal(t, "", r.Text)
}
