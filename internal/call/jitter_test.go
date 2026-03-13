package call

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJitterBuffer_默认配置(t *testing.T) {
	t.Parallel()
	jb := NewJitterBuffer(DefaultJitterBufferConfig())
	require.NotNil(t, jb)
	assert.Equal(t, 0, jb.Len())
	assert.False(t, jb.Ready())
}

func TestNewJitterBuffer_零值使用默认(t *testing.T) {
	t.Parallel()
	jb := NewJitterBuffer(JitterBufferConfig{})
	require.NotNil(t, jb)
	assert.Equal(t, 0, jb.Len())
}

func TestNewJitterBuffer_阈值不超过容量(t *testing.T) {
	t.Parallel()
	jb := NewJitterBuffer(JitterBufferConfig{Capacity: 5, Threshold: 10})
	// 阈值被截断为容量值。
	// 推入 5 帧后应 Ready。
	for range 5 {
		jb.Push([]byte{1})
	}
	assert.True(t, jb.Ready())
}

func TestJitterBuffer_预缓冲阈值(t *testing.T) {
	t.Parallel()
	jb := NewJitterBuffer(JitterBufferConfig{Capacity: 10, Threshold: 3})

	// 未达阈值时 Pop 返回 nil。
	frame, ok := jb.Pop()
	assert.Nil(t, frame)
	assert.False(t, ok)

	// 推入 2 帧，仍未达阈值。
	jb.Push([]byte{1})
	jb.Push([]byte{2})
	assert.False(t, jb.Ready())
	frame, ok = jb.Pop()
	assert.Nil(t, frame)
	assert.False(t, ok)

	// 推入第 3 帧，达到阈值。
	jb.Push([]byte{3})
	assert.True(t, jb.Ready())
	assert.Equal(t, 3, jb.Len())

	// 现在可以 Pop。
	frame, ok = jb.Pop()
	assert.True(t, ok)
	assert.Equal(t, []byte{1}, frame)
}

func TestJitterBuffer_FIFO顺序(t *testing.T) {
	t.Parallel()
	jb := NewJitterBuffer(JitterBufferConfig{Capacity: 10, Threshold: 1})

	for i := range 5 {
		jb.Push([]byte{byte(i)})
	}

	for i := range 5 {
		frame, ok := jb.Pop()
		require.True(t, ok)
		assert.Equal(t, []byte{byte(i)}, frame)
	}

	// 缓冲区已空。
	frame, ok := jb.Pop()
	assert.Nil(t, frame)
	assert.False(t, ok)
}

func TestJitterBuffer_溢出丢弃最旧帧(t *testing.T) {
	t.Parallel()
	jb := NewJitterBuffer(JitterBufferConfig{Capacity: 3, Threshold: 1})

	// 推入 5 帧（容量 3），前 2 帧应被丢弃。
	ok1 := jb.Push([]byte{1})
	assert.True(t, ok1, "第 1 帧不应溢出")

	ok2 := jb.Push([]byte{2})
	assert.True(t, ok2)

	ok3 := jb.Push([]byte{3})
	assert.True(t, ok3)

	ok4 := jb.Push([]byte{4})
	assert.False(t, ok4, "第 4 帧应触发溢出")

	ok5 := jb.Push([]byte{5})
	assert.False(t, ok5, "第 5 帧应触发溢出")

	assert.Equal(t, 3, jb.Len())

	// 剩余帧应为 3, 4, 5。
	frame, ok := jb.Pop()
	require.True(t, ok)
	assert.Equal(t, []byte{3}, frame)

	frame, ok = jb.Pop()
	require.True(t, ok)
	assert.Equal(t, []byte{4}, frame)

	frame, ok = jb.Pop()
	require.True(t, ok)
	assert.Equal(t, []byte{5}, frame)
}

func TestJitterBuffer_Reset(t *testing.T) {
	t.Parallel()
	jb := NewJitterBuffer(JitterBufferConfig{Capacity: 10, Threshold: 2})

	jb.Push([]byte{1})
	jb.Push([]byte{2})
	jb.Push([]byte{3})
	assert.True(t, jb.Ready())
	assert.Equal(t, 3, jb.Len())

	jb.Reset()
	assert.False(t, jb.Ready())
	assert.Equal(t, 0, jb.Len())

	// Reset 后需要重新达到阈值。
	jb.Push([]byte{10})
	assert.False(t, jb.Ready())
	_, ok := jb.Pop()
	assert.False(t, ok)

	jb.Push([]byte{20})
	assert.True(t, jb.Ready())
	frame, ok := jb.Pop()
	require.True(t, ok)
	assert.Equal(t, []byte{10}, frame)
}

func TestJitterBuffer_空缓冲Pop(t *testing.T) {
	t.Parallel()
	jb := NewJitterBuffer(JitterBufferConfig{Capacity: 5, Threshold: 1})

	// 推入后全部 Pop 完。
	jb.Push([]byte{1})
	frame, ok := jb.Pop()
	require.True(t, ok)
	assert.Equal(t, []byte{1}, frame)

	// 缓冲区为空但已 started，Pop 返回 nil, false。
	frame, ok = jb.Pop()
	assert.Nil(t, frame)
	assert.False(t, ok)
}

func TestJitterBuffer_并发安全(t *testing.T) {
	t.Parallel()
	jb := NewJitterBuffer(JitterBufferConfig{Capacity: 100, Threshold: 5})

	done := make(chan struct{})
	go func() {
		for range 200 {
			jb.Push([]byte{42})
		}
		close(done)
	}()

	// 并发读取，不应 panic。
	for range 100 {
		jb.Pop()
		jb.Len()
		jb.Ready()
	}

	<-done
}

func TestDefaultJitterBufferConfig_值合理(t *testing.T) {
	t.Parallel()
	cfg := DefaultJitterBufferConfig()
	assert.Equal(t, 20, cfg.Capacity)
	assert.Equal(t, 3, cfg.Threshold)
	assert.Greater(t, cfg.Capacity, cfg.Threshold)
}
