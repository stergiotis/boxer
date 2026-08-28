package track_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/decode"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/track"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
)

// A WAV on disk opens as a background-built track whose peaks land in the
// cache directory, and opens from that cache the second time.
func TestOpenFileWAVBuildsThenLoadsFromCache(t *testing.T) {
	dir := t.TempDir()
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	frames := format.DurationToFrames(3 * time.Second)
	src, err := pcm.NewSynthSourceE(format, frames, pcm.Sine(format, 220, 0.5))
	require.NoError(t, err)
	path := filepath.Join(dir, "tone.wav")
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, wavfile.WriteE(context.Background(), f, format, wavfile.EncodingPCMInt, 16, src))
	require.NoError(t, f.Close())

	cacheDir := filepath.Join(dir, "peaks")
	tr, kind, err := track.OpenFileE(context.Background(), path, track.Options{CacheDir: cacheDir})
	require.NoError(t, err)
	require.Equal(t, decode.KindWAV, kind)
	require.Equal(t, frames, tr.Frames())
	deadline := time.Now().Add(10 * time.Second)
	for !tr.BuildProgress().Complete && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	bp := tr.BuildProgress()
	require.True(t, bp.Complete, "background build did not complete")
	require.NoError(t, bp.Err)
	require.NoError(t, bp.CacheErr)
	require.False(t, bp.FromCache)
	require.NoError(t, tr.CloseE())

	entries, err := os.ReadDir(cacheDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "one peaks file written")

	tr2, _, err := track.OpenFileE(context.Background(), path, track.Options{CacheDir: cacheDir})
	require.NoError(t, err)
	defer func() { require.NoError(t, tr2.CloseE()) }()
	bp2 := tr2.BuildProgress()
	require.True(t, bp2.Complete)
	require.True(t, bp2.FromCache, "the second open must come from the cache")
	require.True(t, tr2.Peaks().IsComplete())

	// The window cache serves the raw path through its own decoder.
	var raw []float32
	ok := false
	for deadline := time.Now().Add(5 * time.Second); !ok && time.Now().Before(deadline); {
		raw, ok = tr2.Window(1000, 1400)
		if !ok {
			time.Sleep(5 * time.Millisecond)
		}
	}
	require.True(t, ok, "window did not arrive")
	require.Len(t, raw, 400)
}

func TestOpenFileMissingPathFails(t *testing.T) {
	_, _, err := track.OpenFileE(context.Background(), filepath.Join(t.TempDir(), "absent.wav"), track.Options{NoCache: true})
	require.Error(t, err)
}
