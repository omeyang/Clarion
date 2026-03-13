package notify

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWeChatNotifier_SendFollowUpNotification(t *testing.T) {
	tests := []struct {
		name         string
		serverStatus int
		wantErr      bool
	}{
		{
			name:         "successful notification",
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:         "server error",
			serverStatus: http.StatusInternalServerError,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var received map[string]interface{}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Errorf("decode request body: %v", err)
				}

				w.WriteHeader(tt.serverStatus)
			}))
			defer server.Close()

			notifier := NewWeChatNotifier(server.URL, slog.Default())

			notification := FollowUpNotification{
				CallID:       42,
				ContactName:  "张三",
				ContactPhone: "13800138000",
				Grade:        "A",
				Summary:      "客户有合作意向",
				CollectedFields: map[string]string{
					"company": "测试公司",
					"budget":  "10万",
				},
				NextAction: "安排线下拜访",
			}

			err := notifier.SendFollowUpNotification(context.Background(), notification)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "markdown", received["msgtype"])

			md, ok := received["markdown"].(map[string]interface{})
			require.True(t, ok)

			content, ok := md["content"].(string)
			require.True(t, ok)
			assert.Contains(t, content, "张三")
			assert.Contains(t, content, "13800138000")
			assert.Contains(t, content, "客户有合作意向")
			assert.Contains(t, content, "测试公司")
			assert.Contains(t, content, "安排线下拜访")
		})
	}
}

func TestWeChatNotifier_FormatMarkdown(t *testing.T) {
	notifier := NewWeChatNotifier("http://example.com", slog.Default())

	notif := FollowUpNotification{
		CallID:       1,
		ContactName:  "李四",
		ContactPhone: "13900139000",
		Grade:        "B",
		Summary:      "需要跟进",
		CollectedFields: map[string]string{
			"email": "test@example.com",
		},
		NextAction: "电话回访",
	}

	content := notifier.formatMarkdown(notif)

	assert.Contains(t, content, "AI外呼跟进提醒")
	assert.Contains(t, content, "李四")
	assert.Contains(t, content, "B")
	assert.Contains(t, content, "需要跟进")
	assert.Contains(t, content, "email: test@example.com")
	assert.Contains(t, content, "电话回访")
}

func TestWeChatNotifier_EmptyFields(t *testing.T) {
	notifier := NewWeChatNotifier("http://example.com", slog.Default())

	notif := FollowUpNotification{
		CallID:       1,
		ContactName:  "王五",
		ContactPhone: "13700137000",
		Grade:        "C",
	}

	content := notifier.formatMarkdown(notif)

	assert.Contains(t, content, "王五")
	assert.NotContains(t, content, "收集信息")
	assert.NotContains(t, content, "建议行动")
}

func TestWeChatNotifier_CancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWeChatNotifier(server.URL, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := notifier.SendFollowUpNotification(ctx, FollowUpNotification{})
	require.Error(t, err)
}
