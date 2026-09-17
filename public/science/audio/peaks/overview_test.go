package peaks

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The reduced peaks of a known signal (ADR-0245 §Verification): a gated tone
// is loud where the gate is open and flat where it is shut, per channel.
func TestOverviewOfAKnownSignal(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 2}
	const frames = 8000
	// Left: a tone for the first half, then silence. Right: silence.
	left := pcm.Gate(pcm.Sine(format, 440, 0.5), frames/2, frames/2)
	src, err := pcm.NewSynthSourceE(format, frames, pcm.PerChannel(left, pcm.Silence()))
	require.NoError(t, err)

	ov, err := OverviewE(context.Background(), src, 100)
	require.NoError(t, err)
	require.Equal(t, 100, ov.Columns())
	assert.Equal(t, int64(frames), ov.Frames)
	require.Len(t, ov.Min, 2)

	for c := range 100 {
		lo, hi := ov.Min[0][c], ov.Max[0][c]
		if c < 45 {
			assert.Greater(t, hi, int8(55), "column %d: the tone reaches its amplitude", c)
			assert.Less(t, lo, int8(-55), "column %d", c)
		}
		if c > 55 {
			assert.Zero(t, hi, "column %d: the gate is shut", c)
			assert.Zero(t, lo, "column %d", c)
		}
		assert.Zero(t, ov.Min[1][c], "the right channel is silent")
		assert.Zero(t, ov.Max[1][c])
	}
	assert.InDelta(t, 63, int(ov.Peak), 2, "half scale")
}

func TestOverviewEdges(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	empty, err := pcm.NewSynthSourceE(format, 0, nil)
	require.NoError(t, err)
	ov, err := OverviewE(context.Background(), empty, 64)
	require.NoError(t, err)
	assert.Zero(t, ov.Columns(), "nothing to draw is not an error")

	short, err := pcm.NewSynthSourceE(format, 10, pcm.Sine(format, 1000, 1))
	require.NoError(t, err)
	ov, err = OverviewE(context.Background(), short, 64)
	require.NoError(t, err)
	assert.Equal(t, 10, ov.Columns(), "never more columns than frames")

	_, err = OverviewE(context.Background(), short, 0)
	assert.Error(t, err)
	_, err = OverviewE(context.Background(), nil, 8)
	assert.Error(t, err)
	var none *Overview
	assert.Zero(t, none.Columns())
}
