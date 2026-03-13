package call

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestParseESLEvent_Simple(t *testing.T) {
	raw := "Event-Name: CHANNEL_ANSWER\nUnique-ID: abc-123\nChannel-Name: sofia/internal/1001@example.com"

	event := ParseESLEventForTest(raw)

	assert.Equal(t, "CHANNEL_ANSWER", event.Name)
	assert.Equal(t, "abc-123", event.UUID())
	assert.Equal(t, "sofia/internal/1001@example.com", event.Header("Channel-Name"))
}

func TestParseESLEvent_WithBody(t *testing.T) {
	raw := "Event-Name: CHANNEL_EXECUTE\nUnique-ID: def-456\nContent-Length: 11\n\nhello world"

	event := ParseESLEventForTest(raw)

	assert.Equal(t, "CHANNEL_EXECUTE", event.Name)
	assert.Equal(t, "def-456", event.UUID())
	assert.Equal(t, "hello world", event.Body)
}

func TestParseESLEvent_Empty(t *testing.T) {
	event := ParseESLEventForTest("")

	assert.Equal(t, "", event.Name)
	assert.NotNil(t, event.Headers)
	assert.Equal(t, "", event.Body)
}

func TestParseESLEvent_HangupEvent(t *testing.T) {
	raw := "Event-Name: CHANNEL_HANGUP\nUnique-ID: ghi-789\nHangup-Cause: NORMAL_CLEARING\nChannel-State: CS_HANGUP"

	event := ParseESLEventForTest(raw)

	assert.Equal(t, "CHANNEL_HANGUP", event.Name)
	assert.Equal(t, "ghi-789", event.UUID())
	assert.Equal(t, "NORMAL_CLEARING", event.Header("Hangup-Cause"))
	assert.Equal(t, "CS_HANGUP", event.Header("Channel-State"))
}

func TestParseESLEvent_CarriageReturn(t *testing.T) {
	raw := "Event-Name: HEARTBEAT\r\nUnique-ID: jkl-012\r\nEvent-Info: keep-alive\r"

	event := ParseESLEventForTest(raw)

	assert.Equal(t, "HEARTBEAT", event.Name)
	assert.Equal(t, "jkl-012", event.UUID())
}

func TestESLEvent_MissingHeader(t *testing.T) {
	event := ESLEvent{
		Headers: map[string]string{"Event-Name": "TEST"},
	}

	assert.Equal(t, "", event.Header("Nonexistent"))
	assert.Equal(t, "", event.UUID())
}

func TestFormatOriginateCmd(t *testing.T) {
	tests := []struct {
		name     string
		gateway  string
		callerID string
		callee   string
		expected string
	}{
		{
			name:     "standard call",
			gateway:  "pstn-gw",
			callerID: "10001",
			callee:   "13800138000",
			expected: "bgapi originate {origination_caller_id_number=10001}sofia/gateway/pstn-gw/13800138000 &park()",
		},
		{
			name:     "different gateway",
			gateway:  "voip-trunk",
			callerID: "88888888",
			callee:   "02112345678",
			expected: "bgapi originate {origination_caller_id_number=88888888}sofia/gateway/voip-trunk/02112345678 &park()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := FormatOriginateCmd(tt.gateway, tt.callerID, tt.callee)
			assert.Equal(t, tt.expected, cmd)
		})
	}
}

func TestNewESLClient(t *testing.T) {
	cfg := config.FreeSWITCH{
		ESLHost:     "127.0.0.1",
		ESLPort:     8021,
		ESLPassword: "ClueCon",
	}
	client := NewESLClient(cfg, testLogger())
	require.NotNil(t, client)
	assert.NotNil(t, client.Events())
}
