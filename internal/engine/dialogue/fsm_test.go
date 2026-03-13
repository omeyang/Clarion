package dialogue

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/engine"
)

func baseContext() *engine.DialogueContext {
	return &engine.DialogueContext{
		Intent:          engine.IntentContinue,
		CollectedFields: make(map[string]string),
		RequiredFields:  []string{"name", "age", "budget"},
		MaxObjections:   2,
		MaxTurns:        20,
	}
}

func TestDialogueFSM_Transitions(t *testing.T) {
	tests := []struct {
		name    string
		from    engine.DialogueState
		ctx     *engine.DialogueContext
		want    engine.DialogueState
		wantErr bool
	}{
		{
			name: "opening to qualification on continue",
			from: engine.DialogueOpening,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentContinue
				return c
			}(),
			want: engine.DialogueQualification,
		},
		{
			name: "opening to closing on reject",
			from: engine.DialogueOpening,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentReject
				return c
			}(),
			want: engine.DialogueClosing,
		},
		{
			name: "opening to objection on busy",
			from: engine.DialogueOpening,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentBusy
				return c
			}(),
			want: engine.DialogueObjectionHandling,
		},
		{
			name: "qualification to info gathering when missing fields",
			from: engine.DialogueQualification,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentContinue
				return c
			}(),
			want: engine.DialogueInformationGathering,
		},
		{
			name: "qualification to next action when all fields collected",
			from: engine.DialogueQualification,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentContinue
				c.CollectedFields = map[string]string{"name": "张先生", "age": "35", "budget": "500万"}
				return c
			}(),
			want: engine.DialogueNextAction,
		},
		{
			name: "qualification to objection on hesitate",
			from: engine.DialogueQualification,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentHesitate
				return c
			}(),
			want: engine.DialogueObjectionHandling,
		},
		{
			name: "qualification to closing on reject",
			from: engine.DialogueQualification,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentReject
				return c
			}(),
			want: engine.DialogueClosing,
		},
		{
			name: "info gathering self-loop when still missing fields",
			from: engine.DialogueInformationGathering,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentConfirm
				c.CollectedFields = map[string]string{"name": "张先生"}
				return c
			}(),
			want: engine.DialogueInformationGathering,
		},
		{
			name: "info gathering to next action when all fields",
			from: engine.DialogueInformationGathering,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentContinue
				c.CollectedFields = map[string]string{"name": "张先生", "age": "35", "budget": "500万"}
				return c
			}(),
			want: engine.DialogueNextAction,
		},
		{
			name: "info gathering to objection on busy",
			from: engine.DialogueInformationGathering,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentBusy
				return c
			}(),
			want: engine.DialogueObjectionHandling,
		},
		{
			name: "info gathering to closing on reject",
			from: engine.DialogueInformationGathering,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentReject
				return c
			}(),
			want: engine.DialogueClosing,
		},
		{
			name: "objection to info gathering on resolved",
			from: engine.DialogueObjectionHandling,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentContinue
				c.ObjectionCount = 1
				return c
			}(),
			want: engine.DialogueInformationGathering,
		},
		{
			name: "objection to follow-up when high value and max objections",
			from: engine.DialogueObjectionHandling,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentHesitate
				c.ObjectionCount = 2
				c.HighValue = true
				return c
			}(),
			want: engine.DialogueMarkForFollowup,
		},
		{
			name: "objection to closing when max objections",
			from: engine.DialogueObjectionHandling,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentHesitate
				c.ObjectionCount = 2
				return c
			}(),
			want: engine.DialogueClosing,
		},
		{
			name: "objection to closing on reject",
			from: engine.DialogueObjectionHandling,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentReject
				c.ObjectionCount = 0
				return c
			}(),
			want: engine.DialogueClosing,
		},
		{
			name: "next action to follow-up when high value",
			from: engine.DialogueNextAction,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.HighValue = true
				return c
			}(),
			want: engine.DialogueMarkForFollowup,
		},
		{
			name: "next action to follow-up on schedule",
			from: engine.DialogueNextAction,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentSchedule
				return c
			}(),
			want: engine.DialogueMarkForFollowup,
		},
		{
			name: "next action to closing default",
			from: engine.DialogueNextAction,
			ctx: func() *engine.DialogueContext {
				c := baseContext()
				c.Intent = engine.IntentConfirm
				return c
			}(),
			want: engine.DialogueClosing,
		},
		{
			name: "mark for followup to closing",
			from: engine.DialogueMarkForFollowup,
			ctx:  baseContext(),
			want: engine.DialogueClosing,
		},
		{
			name:    "closing has no transitions",
			from:    engine.DialogueClosing,
			ctx:     baseContext(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsm := NewFSM(tt.from, DefaultRules())

			got, err := fsm.Advance(tt.ctx)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidTransition)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want, fsm.State())
		})
	}
}

func TestDialogueFSM_FullConversation(t *testing.T) {
	fsm := NewFSM(engine.DialogueOpening, DefaultRules())

	ctx := baseContext()
	ctx.Intent = engine.IntentContinue
	next, err := fsm.Advance(ctx)
	require.NoError(t, err)
	assert.Equal(t, engine.DialogueQualification, next)

	ctx.Intent = engine.IntentConfirm
	ctx.CollectedFields["name"] = "张先生"
	next, err = fsm.Advance(ctx)
	require.NoError(t, err)
	assert.Equal(t, engine.DialogueInformationGathering, next)

	ctx.Intent = engine.IntentConfirm
	ctx.CollectedFields["age"] = "35"
	next, err = fsm.Advance(ctx)
	require.NoError(t, err)
	assert.Equal(t, engine.DialogueInformationGathering, next)

	ctx.Intent = engine.IntentConfirm
	ctx.CollectedFields["budget"] = "500万"
	next, err = fsm.Advance(ctx)
	require.NoError(t, err)
	assert.Equal(t, engine.DialogueNextAction, next)

	ctx.Intent = engine.IntentSchedule
	next, err = fsm.Advance(ctx)
	require.NoError(t, err)
	assert.Equal(t, engine.DialogueMarkForFollowup, next)

	next, err = fsm.Advance(ctx)
	require.NoError(t, err)
	assert.Equal(t, engine.DialogueClosing, next)
	assert.True(t, fsm.IsTerminal())
}

func TestContext_MissingFields(t *testing.T) {
	ctx := &engine.DialogueContext{
		RequiredFields:  []string{"name", "age", "budget"},
		CollectedFields: map[string]string{"name": "张先生"},
	}
	missing := ctx.MissingFields()
	assert.Equal(t, []string{"age", "budget"}, missing)
	assert.False(t, ctx.HasAllRequiredFields())
}

func TestContext_HasAllRequiredFields(t *testing.T) {
	ctx := &engine.DialogueContext{
		RequiredFields:  []string{"name"},
		CollectedFields: map[string]string{"name": "张先生"},
	}
	assert.True(t, ctx.HasAllRequiredFields())
}

func TestDialogueFSM_ForceState(t *testing.T) {
	fsm := NewFSM(engine.DialogueOpening, DefaultRules())
	fsm.ForceState(engine.DialogueClosing)
	assert.Equal(t, engine.DialogueClosing, fsm.State())
	assert.True(t, fsm.IsTerminal())
}

func TestDialogueState_String(t *testing.T) {
	assert.Equal(t, "OPENING", engine.DialogueOpening.String())
	assert.Equal(t, "CLOSING", engine.DialogueClosing.String())
	assert.Equal(t, "INFORMATION_GATHERING", engine.DialogueInformationGathering.String())
}

func BenchmarkDialogueFSM_Advance(b *testing.B) {
	rules := DefaultRules()
	for b.Loop() {
		fsm := NewFSM(engine.DialogueOpening, rules)
		ctx := &engine.DialogueContext{
			Intent:          engine.IntentContinue,
			CollectedFields: map[string]string{"name": "张先生", "age": "35", "budget": "500万"},
			RequiredFields:  []string{"name", "age", "budget"},
			MaxObjections:   2,
		}
		_, _ = fsm.Advance(ctx)
		_, _ = fsm.Advance(ctx)
	}
}
