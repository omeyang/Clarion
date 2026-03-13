package scheduler

import (
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	redisOpt := asynq.RedisClientOpt{Addr: "localhost:6379"}
	c := NewClient(redisOpt, "outbound")

	assert.NotNil(t, c.c)
	assert.Equal(t, "outbound", c.queue)
}
