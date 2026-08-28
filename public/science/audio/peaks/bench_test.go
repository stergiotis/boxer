package peaks_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
)

// twelveHourStereo is the shape ADR-0208 is written for: the long capture,
// as a procedural source rather than an 8 GB fixture. The signal is a gated
// tone per channel — speech-shaped, and cheap because the gate is off most
// of the time.
func twelveHourStereo(d time.Duration) (format pcm.Format, src pcm.SourceI, frames int64, err error) {
	format = pcm.Format{SampleRate: 48000, Channels: 2}
	frames = format.DurationToFrames(d)
	fn := pcm.PerChannel(
		pcm.Gate(pcm.Sine(format, 440, 0.9), 4800, 43200),
		pcm.Gate(pcm.Sine(format, 220, 0.6), 2400, 45600),
	)
	synth, err := pcm.NewSynthSourceE(format, frames, fn)
	if err != nil {
		return format, nil, 0, err
	}
	return format, synth, frames, nil
}

// TestBuildTenMinutes is the default-lane sibling of
// BenchmarkBuildTwelveHours: the same path over a tenth-hour source, so a
// regression in the build shows up without a benchmark run.
func TestBuildTenMinutes(t *testing.T) {
	if testing.Short() {
		t.Skip("folds 28.8 M frames")
	}
	format, src, frames, err := twelveHourStereo(10 * time.Minute)
	require.NoError(t, err)
	p, err := peaks.BuildE(context.Background(), src, peaks.DefaultBaseBin(), 0, nil)
	require.NoError(t, err)
	require.True(t, p.IsComplete())
	require.Equal(t, frames, p.Built())
	require.Equal(t, frames, p.Frames())
	require.Equal(t, format, p.Format())
	require.Equal(t, int64(112500), p.Bins(0))
	require.Equal(t, int32(18), p.Levels())
	require.Equal(t, int64(1), p.Bins(p.Levels()-1))
	require.Equal(t, int8(115), p.GlobalPeak(), "0.9 full scale is 114.3, rounded outward")

	// The whole file across a 1000-column pane, at the level the pane picks.
	dstMin := make([]int8, 1000)
	dstMax := make([]int8, 1000)
	require.Equal(t, 1000, p.Columns(0, frames, 0, dstMin, dstMax))
	// 28800 frames per column, so the bin of level 6 (16384 frames) is the
	// coarsest that still fits a column.
	require.Equal(t, int32(6), p.PickLevel(float64(frames)/1000))
}

// TestMemoryAccountingTwelveHoursStereo pins the resident cost of the shape
// ADR-0208 §SD2 is sized for. The arithmetic: 12 h at 48 kHz is 2.0736 G
// frames, so a 256-frame base bin gives 8.1 M bins at level 0 and
// 16 200 007 bins over all 24 levels; each bin costs one byte of minimum
// and one of maximum per channel, so two channels cost 64 800 028 bytes.
//
// That is 61.8 MiB, about 20 % above the ~52 MB the ADR estimates — the
// estimate is low, the model is the same.
func TestMemoryAccountingTwelveHoursStereo(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates ~62 MiB")
	}
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	frames := format.DurationToFrames(12 * time.Hour)
	require.Equal(t, int64(2_073_600_000), frames)
	p, err := peaks.NewPyramidE(format, frames, peaks.DefaultBaseBin())
	require.NoError(t, err)
	require.Equal(t, int64(8_100_000), p.Bins(0))
	require.Equal(t, int32(24), p.Levels())
	require.Equal(t, int64(1), p.Bins(p.Levels()-1))
	require.Equal(t, int64(32_400_000), p.Bins(0)*2*int64(format.Channels), "level 0 alone")
	require.Equal(t, int64(64_800_028), p.MemoryBytes())
	require.Greater(t, p.MemoryBytes(), int64(60)<<20)
	require.Less(t, p.MemoryBytes(), int64(63)<<20)
}

// BenchmarkFoldStereo measures the fold alone — no source, no allocation
// inside the loop — so the build benchmark's cost can be split between
// reading the source and summarising it.
func BenchmarkFoldStereo(b *testing.B) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const chunkFrames = 1 << 14
	samples := genSamples(23, chunkFrames*int(format.Channels))
	p, err := peaks.NewPyramidE(format, int64(chunkFrames)*int64(b.N), peaks.DefaultBaseBin())
	require.NoError(b, err)
	b.SetBytes(chunkFrames * int64(format.Channels) * 4)
	b.ResetTimer()
	for range b.N {
		err = p.FoldE(samples)
		if err != nil {
			b.Fatal(err)
		}
	}
	elapsed := b.Elapsed()
	b.StopTimer()
	b.ReportMetric(float64(chunkFrames)*float64(b.N)/elapsed.Seconds(), "frames/s")
}

// BenchmarkBuildTwelveHours bounds the build of the long capture: ns/op for
// the whole pass, frames per second, and the resident size of the result.
func BenchmarkBuildTwelveHours(b *testing.B) {
	_, src, frames, err := twelveHourStereo(12 * time.Hour)
	require.NoError(b, err)
	ctx := context.Background()
	memoryBytes := int64(0)
	b.ResetTimer()
	for range b.N {
		p, err := peaks.BuildE(ctx, src, peaks.DefaultBaseBin(), 0, nil)
		if err != nil {
			b.Fatal(err)
		}
		memoryBytes = p.MemoryBytes()
	}
	elapsed := b.Elapsed()
	b.StopTimer()
	b.ReportMetric(float64(frames)*float64(b.N)/elapsed.Seconds(), "frames/s")
	b.ReportMetric(float64(memoryBytes), "memoryBytes")
}
