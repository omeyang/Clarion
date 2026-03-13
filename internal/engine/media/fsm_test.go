package media

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/engine"
)

func TestFSM_Transition(t *testing.T) {
	tests := []struct {
		name    string
		from    engine.MediaState
		event   engine.MediaEvent
		want    engine.MediaState
		wantErr bool
	}{
		// Happy path: full call lifecycle.
		{"idle to dialing", engine.MediaIdle, engine.EvDial, engine.MediaDialing, false},
		{"dialing to ringing", engine.MediaDialing, engine.EvRinging, engine.MediaRinging, false},
		{"ringing to amd", engine.MediaRinging, engine.EvAnswer, engine.MediaAMDDetecting, false},
		{"amd to bot speaking", engine.MediaAMDDetecting, engine.EvAMDHuman, engine.MediaBotSpeaking, false},
		{"bot done to waiting", engine.MediaBotSpeaking, engine.EvBotDone, engine.MediaWaitingUser, false},
		{"waiting to user speaking", engine.MediaWaitingUser, engine.EvSpeechStart, engine.MediaUserSpeaking, false},
		{"user done to processing", engine.MediaUserSpeaking, engine.EvSpeechEnd, engine.MediaProcessing, false},
		{"processing done to bot", engine.MediaProcessing, engine.EvProcessingDone, engine.MediaBotSpeaking, false},
		{"hangup to post", engine.MediaHangup, engine.EvPostDone, engine.MediaPostProcessing, false},

		// Barge-in flow.
		{"bot to barge-in", engine.MediaBotSpeaking, engine.EvBargeIn, engine.MediaBargeIn, false},
		{"barge-in to user speaking", engine.MediaBargeIn, engine.EvBargeInDone, engine.MediaUserSpeaking, false},

		// Silence timeout flow.
		{"waiting to silence", engine.MediaWaitingUser, engine.EvSilenceTimeout, engine.MediaSilenceTimeout, false},
		{"silence to bot (prompt)", engine.MediaSilenceTimeout, engine.EvSilencePromptDone, engine.MediaBotSpeaking, false},
		{"silence to hangup", engine.MediaSilenceTimeout, engine.EvSecondSilence, engine.MediaHangup, false},

		// Failure paths.
		{"dial failed", engine.MediaDialing, engine.EvDialFailed, engine.MediaHangup, false},
		{"ring timeout", engine.MediaRinging, engine.EvRingTimeout, engine.MediaHangup, false},
		{"amd machine", engine.MediaAMDDetecting, engine.EvAMDMachine, engine.MediaHangup, false},
		{"processing timeout", engine.MediaProcessing, engine.EvProcessingTimeout, engine.MediaHangup, false},

		// Direct answer (skip ringing).
		{"dialing direct answer", engine.MediaDialing, engine.EvAnswer, engine.MediaAMDDetecting, false},

		// Hangup from any active state.
		{"ringing hangup", engine.MediaRinging, engine.EvHangup, engine.MediaHangup, false},
		{"amd hangup", engine.MediaAMDDetecting, engine.EvHangup, engine.MediaHangup, false},
		{"bot hangup", engine.MediaBotSpeaking, engine.EvHangup, engine.MediaHangup, false},
		{"waiting hangup", engine.MediaWaitingUser, engine.EvHangup, engine.MediaHangup, false},
		{"user hangup", engine.MediaUserSpeaking, engine.EvHangup, engine.MediaHangup, false},
		{"processing hangup", engine.MediaProcessing, engine.EvHangup, engine.MediaHangup, false},
		{"barge-in hangup", engine.MediaBargeIn, engine.EvHangup, engine.MediaHangup, false},
		{"silence hangup", engine.MediaSilenceTimeout, engine.EvHangup, engine.MediaHangup, false},

		// Invalid transitions.
		{"idle barge-in invalid", engine.MediaIdle, engine.EvBargeIn, engine.MediaIdle, true},
		{"idle speech invalid", engine.MediaIdle, engine.EvSpeechStart, engine.MediaIdle, true},
		{"dialing speech invalid", engine.MediaDialing, engine.EvSpeechStart, engine.MediaDialing, true},
		{"hangup dial invalid", engine.MediaHangup, engine.EvDial, engine.MediaHangup, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsm := NewFSM(tt.from)

			err := fsm.Handle(tt.event)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidTransition)
				assert.Equal(t, tt.from, fsm.State(), "state should not change on error")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, fsm.State())
		})
	}
}

func TestFSM_FullCallLifecycle(t *testing.T) {
	fsm := NewFSM(engine.MediaIdle)

	steps := []struct {
		event engine.MediaEvent
		want  engine.MediaState
	}{
		{engine.EvDial, engine.MediaDialing},
		{engine.EvRinging, engine.MediaRinging},
		{engine.EvAnswer, engine.MediaAMDDetecting},
		{engine.EvAMDHuman, engine.MediaBotSpeaking},
		{engine.EvBotDone, engine.MediaWaitingUser},
		{engine.EvSpeechStart, engine.MediaUserSpeaking},
		{engine.EvSpeechEnd, engine.MediaProcessing},
		{engine.EvProcessingDone, engine.MediaBotSpeaking},
		// Second turn with barge-in.
		{engine.EvBargeIn, engine.MediaBargeIn},
		{engine.EvBargeInDone, engine.MediaUserSpeaking},
		{engine.EvSpeechEnd, engine.MediaProcessing},
		{engine.EvProcessingDone, engine.MediaBotSpeaking},
		{engine.EvBotDone, engine.MediaWaitingUser},
		// User hangs up.
		{engine.EvHangup, engine.MediaHangup},
		{engine.EvPostDone, engine.MediaPostProcessing},
	}

	for _, step := range steps {
		require.NoError(t, fsm.Handle(step.event), "event %s", step.event)
		assert.Equal(t, step.want, fsm.State(), "after %s", step.event)
	}

	assert.True(t, fsm.IsTerminal())
}

func TestFSM_Callback(t *testing.T) {
	fsm := NewFSM(engine.MediaIdle)

	var called bool
	var cbFrom, cbTo engine.MediaState
	var cbEvent engine.MediaEvent

	fsm.OnTransition(Callback(func(from, to engine.MediaState, event engine.MediaEvent) {
		called = true
		cbFrom = from
		cbTo = to
		cbEvent = event
	}))

	require.NoError(t, fsm.Handle(engine.EvDial))

	assert.True(t, called)
	assert.Equal(t, engine.MediaIdle, cbFrom)
	assert.Equal(t, engine.MediaDialing, cbTo)
	assert.Equal(t, engine.EvDial, cbEvent)
}

func TestFSM_CallbackNotCalledOnError(t *testing.T) {
	fsm := NewFSM(engine.MediaIdle)

	var called bool
	fsm.OnTransition(Callback(func(_, _ engine.MediaState, _ engine.MediaEvent) {
		called = true
	}))

	err := fsm.Handle(engine.EvBargeIn)
	require.Error(t, err)
	assert.False(t, called)
}

func TestFSM_CanHandle(t *testing.T) {
	fsm := NewFSM(engine.MediaIdle)

	assert.True(t, fsm.CanHandle(engine.EvDial))
	assert.False(t, fsm.CanHandle(engine.EvBargeIn))
	assert.False(t, fsm.CanHandle(engine.EvSpeechStart))
}

func TestFSM_IsTerminal(t *testing.T) {
	tests := []struct {
		state    engine.MediaState
		terminal bool
	}{
		{engine.MediaIdle, false},
		{engine.MediaDialing, false},
		{engine.MediaBotSpeaking, false},
		{engine.MediaUserSpeaking, false},
		{engine.MediaHangup, true},
		{engine.MediaPostProcessing, true},
	}

	for _, tt := range tests {
		fsm := NewFSM(tt.state)
		assert.Equal(t, tt.terminal, fsm.IsTerminal(), "state %s", tt.state)
	}
}

func TestFSM_ConcurrentSafe(t *testing.T) {
	fsm := NewFSM(engine.MediaIdle)
	require.NoError(t, fsm.Handle(engine.EvDial))

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = fsm.State()
			_ = fsm.CanHandle(engine.EvRinging)
			_ = fsm.IsTerminal()
		}()
	}
	wg.Wait()
}

func TestMediaState_String(t *testing.T) {
	assert.Equal(t, "IDLE", engine.MediaIdle.String())
	assert.Equal(t, "HANGUP", engine.MediaHangup.String())
	assert.Equal(t, "POST_PROCESSING", engine.MediaPostProcessing.String())
}

func TestMediaEvent_String(t *testing.T) {
	assert.Equal(t, "DIAL", engine.EvDial.String())
	assert.Equal(t, "HANGUP", engine.EvHangup.String())
	assert.Equal(t, "BARGE_IN", engine.EvBargeIn.String())
}

func BenchmarkFSM_Handle(b *testing.B) {
	for b.Loop() {
		fsm := NewFSM(engine.MediaIdle)
		_ = fsm.Handle(engine.EvDial)
		_ = fsm.Handle(engine.EvRinging)
		_ = fsm.Handle(engine.EvAnswer)
		_ = fsm.Handle(engine.EvAMDHuman)
		_ = fsm.Handle(engine.EvBotDone)
		_ = fsm.Handle(engine.EvSpeechStart)
		_ = fsm.Handle(engine.EvSpeechEnd)
		_ = fsm.Handle(engine.EvProcessingDone)
		_ = fsm.Handle(engine.EvHangup)
	}
}

// BenchmarkFSM_Handle_Unsynced 同 BenchmarkFSM_Handle，但使用 Unsynced 选项禁用互斥锁。
// 用于衡量单 goroutine 场景下去除锁开销后的性能提升。
func BenchmarkFSM_Handle_Unsynced(b *testing.B) {
	for b.Loop() {
		fsm := NewFSM(engine.MediaIdle, Unsynced())
		_ = fsm.Handle(engine.EvDial)
		_ = fsm.Handle(engine.EvRinging)
		_ = fsm.Handle(engine.EvAnswer)
		_ = fsm.Handle(engine.EvAMDHuman)
		_ = fsm.Handle(engine.EvBotDone)
		_ = fsm.Handle(engine.EvSpeechStart)
		_ = fsm.Handle(engine.EvSpeechEnd)
		_ = fsm.Handle(engine.EvProcessingDone)
		_ = fsm.Handle(engine.EvHangup)
	}
}
