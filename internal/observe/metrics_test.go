package observe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCallMetrics_创建成功 验证所有度量实例均被正确初始化。
func TestNewCallMetrics_创建成功(t *testing.T) {
	t.Parallel()

	m, err := NewCallMetrics()
	require.NoError(t, err)
	require.NotNil(t, m)

	// 验证所有直方图已创建。
	assert.NotNil(t, m.asrLatency)
	assert.NotNil(t, m.llmFirstToken)
	assert.NotNil(t, m.ttsFirstChunk)
	assert.NotNil(t, m.turnLatency)

	// 验证所有计数器已创建。
	assert.NotNil(t, m.bargeInTotal)
	assert.NotNil(t, m.silenceTimeout)
	assert.NotNil(t, m.providerErrors)
	assert.NotNil(t, m.callsCompleted)

	// 验证网络质量度量已创建。
	assert.NotNil(t, m.audioGapTotal)
	assert.NotNil(t, m.poorNetworkTotal)
	assert.NotNil(t, m.lowVolumeTotal)
	assert.NotNil(t, m.jitterAvg)
	assert.NotNil(t, m.lossRate)
	assert.NotNil(t, m.lowVolumeRate)

	// 验证仪表盘已创建。
	assert.NotNil(t, m.activeCalls)
}

// TestCallMetrics_方法无panic 调用所有方法，确保不会 panic。
func TestCallMetrics_方法无panic(t *testing.T) {
	t.Parallel()

	m, err := NewCallMetrics()
	require.NoError(t, err)

	// 延迟记录方法。
	assert.NotPanics(t, func() { m.RecordASRLatency(100 * time.Millisecond) })
	assert.NotPanics(t, func() { m.RecordLLMFirstToken(200 * time.Millisecond) })
	assert.NotPanics(t, func() { m.RecordTTSFirstChunk(50 * time.Millisecond) })
	assert.NotPanics(t, func() { m.RecordTurnLatency(500 * time.Millisecond) })

	// 计数器方法。
	assert.NotPanics(t, func() { m.IncBargeIn() })
	assert.NotPanics(t, func() { m.IncSilenceTimeout() })
	assert.NotPanics(t, func() { m.IncProviderError("test-provider") })
	assert.NotPanics(t, func() { m.IncCallCompleted() })

	// 网络质量方法。
	assert.NotPanics(t, func() { m.IncAudioGap() })
	assert.NotPanics(t, func() { m.IncPoorNetwork() })
	assert.NotPanics(t, func() { m.IncLowVolume() })
	assert.NotPanics(t, func() { m.RecordJitterAvg(5.0) })
	assert.NotPanics(t, func() { m.RecordLossRate(0.05) })
	assert.NotPanics(t, func() { m.RecordLowVolumeRate(0.1) })

	// 仪表盘方法。
	assert.NotPanics(t, func() { m.SetCallActive(1) })
	assert.NotPanics(t, func() { m.SetCallActive(-1) })
}
