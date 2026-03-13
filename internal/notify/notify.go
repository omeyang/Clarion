// Package notify 定义后处理工作进程用于通知人员已完成通话的
// 通知接口和数据类型。
package notify

import "context"

// FollowUpNotification 包含发送跟进提醒所需的数据。
type FollowUpNotification struct {
	CallID          int64             `json:"call_id"`
	ContactName     string            `json:"contact_name"`
	ContactPhone    string            `json:"contact_phone"`
	Grade           string            `json:"grade"`
	Summary         string            `json:"summary"`
	CollectedFields map[string]string `json:"collected_fields"`
	NextAction      string            `json:"next_action"`
}

// Notifier 通过外部渠道发送跟进通知。
type Notifier interface {
	SendFollowUpNotification(ctx context.Context, notification FollowUpNotification) error
}
