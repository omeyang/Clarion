package call

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/engine"
)

func defaultAMDConfig() config.AMD {
	return config.AMD{
		Enabled:                     true,
		DetectionWindowMs:           3000,
		ContinuousSpeechThresholdMs: 4000,
		HumanPauseThresholdMs:       300,
		EnergyThresholdDBFS:         -35.0,
	}
}

func TestAMD_HumanDetection(t *testing.T) {
	// Human pattern: short speech → pause → speech.
	cfg := defaultAMDConfig()
	d := NewAMDDetectorTestable(cfg)

	frameMs := 20

	// 500ms of speech (25 frames).
	for range 25 {
		d.FeedFrame(-20.0, frameMs)
		if d.Decided() {
			break
		}
	}
	assert.False(t, d.Decided(), "should not decide during initial speech")

	// 400ms of silence (20 frames) — exceeds HumanPauseThresholdMs (300ms).
	for range 20 {
		d.FeedFrame(-50.0, frameMs)
		if d.Decided() {
			break
		}
	}

	// If pause alone triggered it, check.
	if d.Decided() {
		assert.Equal(t, engine.AnswerHuman, d.Result())
		return
	}

	// Speech again should trigger human detection.
	d.FeedFrame(-20.0, frameMs)
	assert.True(t, d.Decided(), "should decide after speech-pause-speech")
	assert.Equal(t, engine.AnswerHuman, d.Result())
}

func TestAMD_VoicemailDetection(t *testing.T) {
	// Voicemail pattern: continuous speech exceeding threshold.
	cfg := defaultAMDConfig()
	d := NewAMDDetectorTestable(cfg)

	frameMs := 20
	// 4000ms continuous speech = 200 frames.
	for range 200 {
		d.FeedFrame(-20.0, frameMs)
		if d.Decided() {
			break
		}
	}

	assert.True(t, d.Decided())
	assert.Equal(t, engine.AnswerVoicemail, d.Result())
}

func TestAMD_UnknownOnSilence(t *testing.T) {
	// No speech during detection window → unknown.
	cfg := defaultAMDConfig()
	d := NewAMDDetectorTestable(cfg)

	frameMs := 20
	// 3000ms silence = 150 frames.
	for range 150 {
		d.FeedFrame(-50.0, frameMs)
		if d.Decided() {
			break
		}
	}

	assert.True(t, d.Decided())
	assert.Equal(t, engine.AnswerUnknown, d.Result())
}

func TestAMD_WindowExpiry_WithSpeech(t *testing.T) {
	// Speech at start, then silence until window expires.
	cfg := defaultAMDConfig()
	cfg.DetectionWindowMs = 1000
	d := NewAMDDetectorTestable(cfg)

	frameMs := 20

	// 200ms of speech.
	for range 10 {
		d.FeedFrame(-20.0, frameMs)
		if d.Decided() {
			break
		}
	}

	// 400ms of silence — triggers pause detection.
	for range 20 {
		d.FeedFrame(-50.0, frameMs)
		if d.Decided() {
			break
		}
	}

	if d.Decided() {
		assert.Equal(t, engine.AnswerHuman, d.Result())
		return
	}

	// Fill rest of window with silence.
	for range 30 {
		d.FeedFrame(-50.0, frameMs)
		if d.Decided() {
			break
		}
	}

	assert.True(t, d.Decided())
	assert.Equal(t, engine.AnswerHuman, d.Result())
}

func TestAMD_NoDecisionAfterDecided(t *testing.T) {
	cfg := defaultAMDConfig()
	d := NewAMDDetectorTestable(cfg)

	// Force voicemail.
	for range 200 {
		d.FeedFrame(-20.0, 20)
		if d.Decided() {
			break
		}
	}

	assert.True(t, d.Decided())
	result := d.Result()

	// Feed more frames — should not change result.
	d.FeedFrame(-50.0, 20)
	assert.Equal(t, result, d.Result())
}

func TestAMD_RealWorldDetector(t *testing.T) {
	// Test the time.Now()-based detector initialization.
	cfg := defaultAMDConfig()
	d := NewAMDDetector(cfg)

	assert.False(t, d.Decided())
	assert.Equal(t, engine.AnswerUnknown, d.Result())

	d.Reset()
	assert.False(t, d.Decided())
}
