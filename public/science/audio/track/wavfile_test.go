package track_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/track"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
)

// TestOpenOverAWavFile is the whole ADR-0208 M1 path in one test: a WAV
// written to disk, decoded by wavfile, summarised by peaks and read back
// through a track's raw-window path (§SD3), with the file descriptor's
// lifetime owned by the track.
func TestOpenOverAWavFile(t *testing.T) {
	ctx := context.Background()
	format := pcm.Format{SampleRate: 44100, Channels: 2}
	const frames int64 = 44100 / 2
	ch := int(format.Channels)
	epoch := time.Date(2026, time.August, 28, 11, 0, 0, 0, time.UTC)

	fn := pcm.PerChannel(
		pcm.Gate(pcm.Sine(format, 440, 0.9), 4410, 2205),
		pcm.Chirp(format, frames, 50, 8000, 0.7),
	)
	written, err := pcm.NewSynthSourceE(format, frames, fn)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "clip.wav")
	f, err := os.Create(path)
	require.NoError(t, err)
	// 32-bit IEEE float is what the source already holds, so the comparison
	// below is exact rather than within a quantisation step.
	err = wavfile.WriteE(ctx, f, format, wavfile.EncodingIEEEFloat, 32, written)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	file, err := wavfile.OpenE(path)
	require.NoError(t, err)
	require.Equal(t, frames, file.Frames())

	clock := sink.NewManualClock(time.Unix(0, 0))
	tr, err := track.OpenE(ctx, file, track.Options{
		BaseBin: 128,
		Epoch:   epoch,
		NewSink: func(src pcm.SourceI) sink.SinkI { return sink.NewNull(src, clock) },
	})
	require.NoError(t, err)

	require.Equal(t, format, tr.Format())
	require.Equal(t, frames, tr.Frames())
	require.Equal(t, 500*time.Millisecond, tr.Duration())
	require.True(t, tr.Peaks().IsComplete())
	require.Equal(t, frames, tr.Peaks().Built())
	require.Positive(t, tr.Peaks().GlobalPeak())
	require.True(t, tr.TimeBase().IsAbsolute())

	// The raw window is the samples the file holds, byte for byte.
	const window = 2048
	got := make([]float32, window*ch)
	n, err := tr.ReadWindowE(ctx, 1000, got)
	require.NoError(t, err)
	require.Equal(t, window, n)
	want := make([]float32, window*ch)
	_, err = written.ReadFramesAtE(ctx, 1000, want)
	require.NoError(t, err)
	require.Equal(t, want, got)

	// The transport runs off the same file without a second open.
	tr.Sink().Play()
	clock.Advance(250 * time.Millisecond)
	require.Equal(t, frames/2, tr.Sink().Position())
	at, ok := tr.TimeBase().FrameToTime(tr.Sink().Position())
	require.True(t, ok)
	require.True(t, at.Equal(epoch.Add(250*time.Millisecond)))

	// CloseE owns the descriptor the track was handed.
	require.NoError(t, tr.CloseE())
	n, err = tr.ReadWindowE(ctx, 0, got)
	require.Error(t, err)
	require.Zero(t, n)
	require.NoError(t, tr.CloseE())
}
