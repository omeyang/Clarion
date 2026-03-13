package call

import (
	"time"

	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/engine"
)

// AMDDetector 使用能量和时序分析执行留言机检测。
//
// 算法：
//  1. 连续语音超过阈值 → 留言机（机器播放较长的问候语）。
//  2. 在窗口内检测到自然停顿 → 人类（人类说"你好"后停顿）。
//  3. 检测窗口到期未做出判断 → 未知。
type AMDDetector struct {
	cfg config.AMD

	// 内部状态。
	started            bool
	startTime          time.Time
	speechStartTime    time.Time
	isSpeaking         bool
	continuousSpeechMs int
	pauseDetected      bool
	result             engine.AnswerType
	decided            bool
}

// NewAMDDetector 使用给定的 AMD 配置创建检测器。
func NewAMDDetector(cfg config.AMD) *AMDDetector {
	return &AMDDetector{cfg: cfg}
}

// FeedFrame 处理一帧音频并更新检测状态。
// energyDBFS 是帧的能量级别（dBFS）。
// frameMs 是帧的时长（毫秒）。
func (d *AMDDetector) FeedFrame(energyDBFS float64, frameMs int) {
	if d.decided {
		return
	}

	if !d.started {
		d.started = true
		d.startTime = time.Now()
	}

	isSpeech := energyDBFS > d.cfg.EnergyThresholdDBFS

	if isSpeech {
		d.feedSpeechFrame(frameMs)
	} else {
		d.feedSilenceFrame()
	}

	if d.decided {
		return
	}

	// 检查检测窗口是否到期。
	d.checkWindowExpiry(int(time.Since(d.startTime).Milliseconds()))
}

// feedSpeechFrame 处理检测到语音的帧。
func (d *AMDDetector) feedSpeechFrame(frameMs int) {
	if !d.isSpeaking {
		d.isSpeaking = true
		d.speechStartTime = time.Now()

		// 如果之前有语音，然后静默再次语音，这是停顿模式。
		if d.pauseDetected {
			d.result = engine.AnswerHuman
			d.decided = true
			return
		}
	}

	d.continuousSpeechMs += frameMs

	// 检查连续语音阈值（留言机检测）。
	if d.continuousSpeechMs >= d.cfg.ContinuousSpeechThresholdMs {
		d.result = engine.AnswerVoicemail
		d.decided = true
	}
}

// feedSilenceFrame 处理未检测到语音的帧。
func (d *AMDDetector) feedSilenceFrame() {
	if d.isSpeaking {
		d.isSpeaking = false
		speechDuration := time.Since(d.speechStartTime).Milliseconds()

		if speechDuration > 0 && speechDuration < int64(d.cfg.ContinuousSpeechThresholdMs) {
			d.pauseDetected = true
		}
		d.continuousSpeechMs = 0
	}

	if !d.pauseDetected {
		return
	}

	silenceMs := time.Since(d.speechStartTime).Milliseconds()
	if silenceMs > int64(d.cfg.HumanPauseThresholdMs) {
		d.result = engine.AnswerHuman
		d.decided = true
	}
}

// checkWindowExpiry 在检测窗口到期时决定结果。
func (d *AMDDetector) checkWindowExpiry(elapsedMs int) {
	if elapsedMs < d.cfg.DetectionWindowMs {
		return
	}

	if d.pauseDetected {
		d.result = engine.AnswerHuman
	} else if d.continuousSpeechMs > 0 {
		d.result = engine.AnswerVoicemail
	} else {
		d.result = engine.AnswerUnknown
	}
	d.decided = true
}

// Result 返回检测结果。未决定时返回 AnswerUnknown。
func (d *AMDDetector) Result() engine.AnswerType {
	if !d.decided {
		return engine.AnswerUnknown
	}
	return d.result
}

// Decided 当检测器已得出结论时返回 true。
func (d *AMDDetector) Decided() bool {
	return d.decided
}

// Reset 清除检测器状态以便复用。
func (d *AMDDetector) Reset() {
	d.started = false
	d.isSpeaking = false
	d.continuousSpeechMs = 0
	d.pauseDetected = false
	d.result = engine.AnswerUnknown
	d.decided = false
}

// AMDDetectorTestable 是可测试版本，使用显式时间戳替代 time.Now()。
type AMDDetectorTestable struct {
	cfg config.AMD

	startMs            int
	currentMs          int
	isSpeaking         bool
	continuousSpeechMs int
	pauseDetected      bool
	lastSpeechEndMs    int
	result             engine.AnswerType
	decided            bool
	started            bool
}

// NewAMDDetectorTestable 创建确定性检测器，用于测试。
func NewAMDDetectorTestable(cfg config.AMD) *AMDDetectorTestable {
	return &AMDDetectorTestable{cfg: cfg}
}

// FeedFrame 在给定时间戳处理一帧音频。
func (d *AMDDetectorTestable) FeedFrame(energyDBFS float64, frameMs int) {
	if d.decided {
		return
	}

	if !d.started {
		d.started = true
		d.startMs = d.currentMs
	}

	isSpeech := energyDBFS > d.cfg.EnergyThresholdDBFS

	if isSpeech {
		d.feedSpeechFrame(frameMs)
	} else {
		d.feedSilenceFrame()
	}

	d.currentMs += frameMs

	if d.decided {
		return
	}

	// 检查检测窗口是否到期。
	d.checkWindowExpiry(d.currentMs - d.startMs)
}

// feedSpeechFrame 处理检测到语音的帧。
func (d *AMDDetectorTestable) feedSpeechFrame(frameMs int) {
	if !d.isSpeaking {
		d.isSpeaking = true

		// 如果之前检测到停顿且语音再次开始，则为人类。
		if d.pauseDetected {
			d.result = engine.AnswerHuman
			d.decided = true
			return
		}
	}

	d.continuousSpeechMs += frameMs

	if d.continuousSpeechMs >= d.cfg.ContinuousSpeechThresholdMs {
		d.result = engine.AnswerVoicemail
		d.decided = true
	}
}

// feedSilenceFrame 处理未检测到语音的帧。
func (d *AMDDetectorTestable) feedSilenceFrame() {
	if d.isSpeaking {
		d.isSpeaking = false
		d.lastSpeechEndMs = d.currentMs

		if d.continuousSpeechMs > 0 && d.continuousSpeechMs < d.cfg.ContinuousSpeechThresholdMs {
			d.pauseDetected = true
		}
		d.continuousSpeechMs = 0
	}

	if !d.pauseDetected || d.lastSpeechEndMs <= 0 {
		return
	}

	silenceMs := d.currentMs - d.lastSpeechEndMs
	if silenceMs >= d.cfg.HumanPauseThresholdMs {
		d.result = engine.AnswerHuman
		d.decided = true
	}
}

// checkWindowExpiry 在检测窗口到期时决定结果。
func (d *AMDDetectorTestable) checkWindowExpiry(elapsedMs int) {
	if elapsedMs < d.cfg.DetectionWindowMs {
		return
	}

	if d.pauseDetected {
		d.result = engine.AnswerHuman
	} else if d.continuousSpeechMs > 0 {
		d.result = engine.AnswerVoicemail
	} else {
		d.result = engine.AnswerUnknown
	}
	d.decided = true
}

// Result 返回检测结果。
func (d *AMDDetectorTestable) Result() engine.AnswerType {
	if !d.decided {
		return engine.AnswerUnknown
	}
	return d.result
}

// Decided 当检测器已得出结论时返回 true。
func (d *AMDDetectorTestable) Decided() bool {
	return d.decided
}
