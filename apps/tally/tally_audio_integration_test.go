//go:build integration

package tally

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/science/audio/decode"
)

// TestAudioSessionOverACompressedRecording is the ffmpeg half end to end: a
// recording staged into anonymous memory out of an fs.FS, probed and decoded
// through inherited descriptors, and opened as a track whose peaks build to
// completion. The sink is whatever the host has — a device where one opens,
// the silent clock with a reason otherwise.
func TestAudioSessionOverACompressedRecording(t *testing.T) {
	if _, ok := extbin.Ffmpeg.Resolve(); !ok {
		t.Skip("ffmpeg is not available on this host")
	}
	dir := t.TempDir()
	t.Setenv(adhocdata.StoreDir.Spec().Name, filepath.Join(dir, "store"))
	path, want := writeWavFixture(t, dir, "tone.wav")
	flac := filepath.Join(dir, "tone.flac")
	require.NoError(t, extbin.Ffmpeg.Run(context.Background(), extbin.Opts{},
		"-nostdin", "-v", "error", "-y", "-i", path, "-c:a", "flac", flac))
	info, err := os.Stat(flac)
	require.NoError(t, err)

	s, err := openAudioSession(context.Background(), os.DirFS(dir), "tone.flac", info.Size(), info.ModTime())
	require.NoError(t, err)
	defer func() { require.NoError(t, s.closeE()) }()
	require.Equal(t, decode.KindFfmpeg, s.staged.kind)
	assert.Equal(t, audioFixtureFormat, s.tr.Format())
	assert.InEpsilon(t, audioFixtureFrames, s.tr.Frames(), 0.02)
	// The store dates the file, so frame 0 reads as mtime less the length.
	assert.False(t, s.tr.TimeBase().Epoch.IsZero(), "an mtime gives the readout an epoch")

	// The peaks build reads through its own decoder; it must finish rather
	// than deadlock against the sink's and the window cache's.
	deadline := time.Now().Add(30 * time.Second)
	bp := s.tr.BuildProgress()
	for !bp.Complete && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		bp = s.tr.BuildProgress()
	}
	require.True(t, bp.Complete, "the peaks build completes")
	require.NoError(t, bp.Err)

	// And the samples are the recording's own, read back through a window.
	dst := make([]float32, 1024*int64(audioFixtureFormat.Channels))
	n, err := s.tr.ReadWindowE(context.Background(), 4096, dst)
	require.NoError(t, err)
	require.Equal(t, 1024, n)
	channels := int64(audioFixtureFormat.Channels)
	for i := range dst {
		assert.InDelta(t, want[4096*channels+int64(i)], dst[i], 1e-3, "sample %d", i)
	}
}
