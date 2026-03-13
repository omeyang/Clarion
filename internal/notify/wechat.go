package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// WeChatNotifier 向企业微信 webhook 发送通知。
type WeChatNotifier struct {
	webhookURL string
	client     *http.Client
	logger     *slog.Logger
}

// NewWeChatNotifier 创建向指定企业微信 webhook URL 发送
// Markdown 消息的通知器。
func NewWeChatNotifier(webhookURL string, logger *slog.Logger) *WeChatNotifier {
	return &WeChatNotifier{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

type wechatMessage struct {
	MsgType  string         `json:"msgtype"`
	Markdown wechatMarkdown `json:"markdown"`
}

type wechatMarkdown struct {
	Content string `json:"content"`
}

// SendFollowUpNotification 向已配置的企业微信 webhook 发送
// Markdown 格式的跟进通知。
func (n *WeChatNotifier) SendFollowUpNotification(ctx context.Context, notification FollowUpNotification) error {
	content := n.formatMarkdown(notification)

	msg := wechatMessage{
		MsgType:  "markdown",
		Markdown: wechatMarkdown{Content: content},
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal wechat message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create wechat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send wechat notification: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			n.logger.Error("close response body", slog.String("error", err.Error()))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wechat webhook returned status %d", resp.StatusCode)
	}

	n.logger.Info("wechat notification sent",
		slog.Int64("call_id", notification.CallID),
		slog.String("grade", notification.Grade),
	)

	return nil
}

func (n *WeChatNotifier) formatMarkdown(notif FollowUpNotification) string {
	var b strings.Builder
	b.WriteString("## AI外呼跟进提醒\n\n")
	fmt.Fprintf(&b, "> **客户**: %s (%s)\n", notif.ContactName, notif.ContactPhone)
	fmt.Fprintf(&b, "> **评级**: %s\n", notif.Grade)
	fmt.Fprintf(&b, "> **通话ID**: %d\n\n", notif.CallID)

	if notif.Summary != "" {
		fmt.Fprintf(&b, "**摘要**: %s\n\n", notif.Summary)
	}

	if len(notif.CollectedFields) > 0 {
		b.WriteString("**收集信息**:\n")
		keys := make([]string, 0, len(notif.CollectedFields))
		for k := range notif.CollectedFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "- %s: %s\n", k, notif.CollectedFields[k])
		}
		b.WriteString("\n")
	}

	if notif.NextAction != "" {
		fmt.Fprintf(&b, "**建议行动**: %s\n", notif.NextAction)
	}

	return b.String()
}
