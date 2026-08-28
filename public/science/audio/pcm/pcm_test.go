package pcm_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/pcm/pcmtest"
)

func TestFormatDurationRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		format := pcm.Format{
			SampleRate: rapid.Uint32Range(1, 384000).Draw(t, "rate"),
			Channels:   rapid.Uint16Range(1, 8).Draw(t, "channels"),
		}
		// Up to a week at the drawn rate: inside time.Duration's ~292-year
		// range and far inside int64 for the frame arithmetic.
		frames := rapid.Int64Range(0, 7*24*3600*int64(format.SampleRate)).Draw(t, "frames")
		d := format.FramesToDuration(frames)
		back := format.DurationToFrames(d)
		// Nanosecond truncation loses at most one frame.
		require.LessOrEqual(t, frames-back, int64(1))
		require.GreaterOrEqual(t, frames-back, int64(0))
	})
}

func TestFormatTwelveHoursDoesNotOverflow(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	frames := format.DurationToFrames(12 * time.Hour)
	require.Equal(t, int64(12*3600*48000), frames)
	require.Equal(t, 12*time.Hour, format.FramesToDuration(frames))
}

func TestFormatValidate(t *testing.T) {
	require.Error(t, pcm.Format{}.ValidateE())
	require.Error(t, pcm.Format{SampleRate: 48000}.ValidateE())
	require.NoError(t, pcm.Format{SampleRate: 48000, Channels: 1}.ValidateE())
}

func TestMemSourceContract(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		format := pcm.Format{
			SampleRate: 8000,
			Channels:   rapid.Uint16Range(1, 3).Draw(t, "channels"),
		}
		frames := rapid.IntRange(0, 300).Draw(t, "frames")
		samples := rapid.SliceOfN(rapid.Float32Range(-1, 1), frames*int(format.Channels), frames*int(format.Channels)).Draw(t, "samples")
		src, err := pcm.NewMemSourceE(format, samples)
		require.NoError(t, err)
		pcmtest.CheckSourceContract(t, src, 300)
	})
}

func TestMemSourceRejectsRaggedFrames(t *testing.T) {
	_, err := pcm.NewMemSourceE(pcm.Format{SampleRate: 8000, Channels: 2}, make([]float32, 3))
	require.Error(t, err)
}

func TestSynthSourceContract(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames = 48000
	fn := pcm.PerChannel(
		pcm.Gate(pcm.Sine(format, 440, 0.8), 4800, 4800),
		pcm.Chirp(format, frames, 100, 4000, 0.5),
	)
	src, err := pcm.NewSynthSourceE(format, frames, fn)
	require.NoError(t, err)
	pcmtest.CheckSourceContract(t, src, 2000)
}

func TestSynthSourceLongLengthIsFree(t *testing.T) {
	// Twelve hours of stereo: constructing and reading the tail costs
	// nothing proportional to the length.
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	frames := format.DurationToFrames(12 * time.Hour)
	src, err := pcm.NewSynthSourceE(format, frames, pcm.Sine(format, 1000, 0.5))
	require.NoError(t, err)
	buf := make([]float32, 8)
	n, err := src.ReadFramesAtE(context.Background(), frames-2, buf)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	pcmtest.CheckSourceContract(t, src, 500)
}

func TestGateShape(t *testing.T) {
	one := func(int64, int) float32 { return 1 }
	fn := pcm.Gate(one, 2, 3)
	got := make([]float32, 0, 10)
	for f := range int64(10) {
		got = append(got, fn(f, 0))
	}
	require.Equal(t, []float32{1, 1, 0, 0, 0, 1, 1, 0, 0, 0}, got)
}
