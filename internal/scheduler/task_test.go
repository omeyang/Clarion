package scheduler

import (
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/config"
)

func TestNewOutboundCallTask(t *testing.T) {
	p := OutboundCallPayload{
		CallID:     42,
		ContactID:  100,
		TaskID:     7,
		Phone:      "13800138000",
		Gateway:    "pstn",
		CallerID:   "10001",
		TemplateID: 3,
		AttemptNo:  1,
	}

	task, err := NewOutboundCallTask(p)
	require.NoError(t, err)
	assert.Equal(t, TaskTypeOutboundCall, task.Type())
	assert.NotEmpty(t, task.Payload())
}

func TestParseOutboundCallPayload_RoundTrip(t *testing.T) {
	original := OutboundCallPayload{
		CallID:     42,
		ContactID:  100,
		TaskID:     7,
		Phone:      "13800138000",
		Gateway:    "pstn",
		CallerID:   "10001",
		TemplateID: 3,
		AttemptNo:  2,
	}

	task, err := NewOutboundCallTask(original)
	require.NoError(t, err)

	parsed, err := ParseOutboundCallPayload(task)
	require.NoError(t, err)

	assert.Equal(t, original, parsed)
}

func TestParseOutboundCallPayload_InvalidJSON(t *testing.T) {
	task := asynq.NewTask(TaskTypeOutboundCall, []byte("not json"))
	_, err := ParseOutboundCallPayload(task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal outbound call payload")
}

func TestUniqueKey(t *testing.T) {
	p := OutboundCallPayload{TaskID: 7, ContactID: 100, AttemptNo: 2}
	assert.Equal(t, "outbound:call:7:100:2", p.UniqueKey())
}

func TestUniqueKey_DifferentAttempts(t *testing.T) {
	p1 := OutboundCallPayload{TaskID: 7, ContactID: 100, AttemptNo: 1}
	p2 := OutboundCallPayload{TaskID: 7, ContactID: 100, AttemptNo: 2}
	assert.NotEqual(t, p1.UniqueKey(), p2.UniqueKey())
}

func TestRedisOpt(t *testing.T) {
	cfg := config.Redis{
		Addr:     "redis.example.com:6379",
		Password: "secret",
		DB:       2,
	}

	opt := RedisOpt(cfg)
	assert.Equal(t, "redis.example.com:6379", opt.Addr)
	assert.Equal(t, "secret", opt.Password)
	assert.Equal(t, 2, opt.DB)
}
