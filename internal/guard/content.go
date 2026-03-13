package guard

import (
	"regexp"
	"strings"
	"unicode"
)

// ContentIssue 描述文本内容校验中发现的问题类型。
type ContentIssue string

// 内容校验问题类型。
const (
	// ContentIssueJSONArtifact 文本包含 JSON 片段。
	ContentIssueJSONArtifact ContentIssue = "json_artifact"
	// ContentIssueCodeBlock 文本包含代码块标记。
	ContentIssueCodeBlock ContentIssue = "code_block"
	// ContentIssueMarkdown 文本包含 Markdown 格式标记。
	ContentIssueMarkdown ContentIssue = "markdown"
	// ContentIssueURL 文本包含 URL 链接。
	ContentIssueURL ContentIssue = "url"
	// ContentIssuePII 文本包含疑似个人敏感信息。
	ContentIssuePII ContentIssue = "pii"
)

// ContentCheckResult 是内容校验的结果。
type ContentCheckResult struct {
	// OK 为 true 表示通过全部校验。
	OK bool
	// Text 是清洗后的文本。
	Text string
	// Issues 记录所有发现的问题。
	Issues []ContentIssue
	// Reasons 对应每个问题的描述。
	Reasons []string
}

// ContentCheckerConfig 配置内容校验器。
type ContentCheckerConfig struct {
	// ExtraPIIPatterns 额外的 PII 检测正则。
	ExtraPIIPatterns []string
}

// jsonArtifactPatterns 检测 JSON 片段。
var jsonArtifactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\{[^{}]*"[a-z_]+"[^{}]*:[^{}]*\}`),   // {"key": value}
	regexp.MustCompile(`"[a-z_]+":\s*"[^"]*"`),               // "key": "value"
	regexp.MustCompile(`"[a-z_]+":\s*(true|false|null|\d+)`), // "key": true/false/null/数字
}

// codeBlockRe 检测代码块标记。
var codeBlockRe = regexp.MustCompile("(?s)```[\\s\\S]*?```")

// inlineCodeRe 检测行内代码标记。
var inlineCodeRe = regexp.MustCompile("`[^`]+`")

// markdownPatterns 检测 Markdown 格式标记。
var markdownPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\*\*[^*]+\*\*`),          // **加粗**
	regexp.MustCompile(`#{1,6}\s+\S`),            // # 标题
	regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`), // [text](url)
	regexp.MustCompile(`^\s*[-*]\s+`),            // - 列表项
}

// urlRe 检测 URL。
var urlRe = regexp.MustCompile(`https?://[^\s，。！？；、）\)]+`)

// piiPatterns 检测个人敏感信息。
var piiPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b1[3-9]\d{9}\b`),                                                      // 中国手机号
	regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`),                       // 邮箱
	regexp.MustCompile(`\b\d{6}(19|20)\d{2}(0[1-9]|1[0-2])(0[1-9]|[12]\d|3[01])\d{3}[\dXx]\b`), // 身份证号
}

// ContentChecker 校验 LLM 输出文本的内容质量，确保适合 TTS 合成。
// 与 ResponseValidator 互补：ResponseValidator 校验安全问题（AI 泄露、提示泄露），
// ContentChecker 校验格式问题（JSON 片段、代码块、Markdown、URL、PII）。
type ContentChecker struct {
	piiPatterns []*regexp.Regexp
}

// NewContentChecker 创建内容校验器。
func NewContentChecker(cfg ContentCheckerConfig) *ContentChecker {
	pii := compileAndMerge(piiPatterns, cfg.ExtraPIIPatterns)
	return &ContentChecker{piiPatterns: pii}
}

// Check 校验并清洗文本内容，返回适合 TTS 合成的文本。
func (c *ContentChecker) Check(text string) ContentCheckResult {
	result := ContentCheckResult{OK: true, Text: text}

	text = strings.TrimSpace(text)
	result.Text = text
	if text == "" {
		return result
	}

	c.checkCodeBlock(&result)
	c.checkJSONArtifact(&result)
	c.checkMarkdown(&result)
	c.checkURL(&result)
	c.checkPII(&result)

	return result
}

// checkCodeBlock 检测并移除代码块。
func (c *ContentChecker) checkCodeBlock(r *ContentCheckResult) {
	if codeBlockRe.MatchString(r.Text) {
		r.Text = codeBlockRe.ReplaceAllString(r.Text, "")
		r.Text = strings.TrimSpace(r.Text)
		r.addContentIssue(ContentIssueCodeBlock, "文本包含代码块，已移除")
	}
	if inlineCodeRe.MatchString(r.Text) {
		// 保留行内代码的内容，仅去掉反引号。
		r.Text = strings.ReplaceAll(r.Text, "`", "")
		r.addContentIssue(ContentIssueCodeBlock, "文本包含行内代码标记，已移除反引号")
	}
}

// checkJSONArtifact 检测 JSON 片段。
func (c *ContentChecker) checkJSONArtifact(r *ContentCheckResult) {
	for _, re := range jsonArtifactPatterns {
		if re.MatchString(r.Text) {
			r.addContentIssue(ContentIssueJSONArtifact, "文本包含 JSON 片段")
			return
		}
	}
}

// checkMarkdown 检测并清理 Markdown 格式标记。
func (c *ContentChecker) checkMarkdown(r *ContentCheckResult) {
	for _, re := range markdownPatterns {
		if re.MatchString(r.Text) {
			r.Text = cleanMarkdown(r.Text)
			r.addContentIssue(ContentIssueMarkdown, "文本包含 Markdown 格式，已清理")
			return
		}
	}
}

// checkURL 检测并移除 URL。
func (c *ContentChecker) checkURL(r *ContentCheckResult) {
	if urlRe.MatchString(r.Text) {
		r.Text = urlRe.ReplaceAllString(r.Text, "")
		r.Text = normalizeSpaces(r.Text)
		r.addContentIssue(ContentIssueURL, "文本包含 URL，已移除")
	}
}

// checkPII 检测个人敏感信息。
func (c *ContentChecker) checkPII(r *ContentCheckResult) {
	for _, re := range c.piiPatterns {
		if re.MatchString(r.Text) {
			r.addContentIssue(ContentIssuePII, "文本包含疑似个人敏感信息")
			return
		}
	}
}

// addContentIssue 添加问题记录并标记为未通过。
func (r *ContentCheckResult) addContentIssue(issue ContentIssue, reason string) {
	r.OK = false
	r.Issues = append(r.Issues, issue)
	r.Reasons = append(r.Reasons, reason)
}

// cleanMarkdown 去除 Markdown 格式标记，保留纯文本内容。
func cleanMarkdown(text string) string {
	// 去除加粗标记。
	text = strings.ReplaceAll(text, "**", "")
	// 去除标题标记。
	headingRe := regexp.MustCompile(`#{1,6}\s+`)
	text = headingRe.ReplaceAllString(text, "")
	// 去除链接，保留文本。
	linkRe := regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`)
	text = linkRe.ReplaceAllString(text, "$1")
	// 去除列表标记。
	listRe := regexp.MustCompile(`(?m)^\s*[-*]\s+`)
	text = listRe.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

// normalizeSpaces 折叠连续空白为单个空格并去除首尾空白。
func normalizeSpaces(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	prevSpace := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
