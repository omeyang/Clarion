package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDialogueState_String(t *testing.T) {
	tests := []struct {
		state DialogueState
		want  string
	}{
		{DialogueOpening, "OPENING"},
		{DialogueQualification, "QUALIFICATION"},
		{DialogueInformationGathering, "INFORMATION_GATHERING"},
		{DialogueObjectionHandling, "OBJECTION_HANDLING"},
		{DialogueNextAction, "NEXT_ACTION"},
		{DialogueMarkForFollowup, "MARK_FOR_FOLLOWUP"},
		{DialogueClosing, "CLOSING"},
		{DialogueState(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.state.String())
		})
	}
}

func TestParseDialogueState(t *testing.T) {
	tests := []struct {
		input string
		want  DialogueState
		ok    bool
	}{
		{"OPENING", DialogueOpening, true},
		{"QUALIFICATION", DialogueQualification, true},
		{"INFORMATION_GATHERING", DialogueInformationGathering, true},
		{"OBJECTION_HANDLING", DialogueObjectionHandling, true},
		{"NEXT_ACTION", DialogueNextAction, true},
		{"MARK_FOR_FOLLOWUP", DialogueMarkForFollowup, true},
		{"CLOSING", DialogueClosing, true},
		{"UNKNOWN", DialogueOpening, false},
		{"", DialogueOpening, false},
		{"invalid", DialogueOpening, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := ParseDialogueState(tt.input)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestParseDialogueState_RoundTrip(t *testing.T) {
	// 所有已知状态应能 String() → Parse 往返。
	states := []DialogueState{
		DialogueOpening, DialogueQualification, DialogueInformationGathering,
		DialogueObjectionHandling, DialogueNextAction, DialogueMarkForFollowup,
		DialogueClosing,
	}
	for _, s := range states {
		got, ok := ParseDialogueState(s.String())
		assert.True(t, ok, "状态 %s 应能解析", s.String())
		assert.Equal(t, s, got)
	}
}
