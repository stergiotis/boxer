package decode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/pcm/pcmtest"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
)

// testFormat and testSignal are the fixture every lane of these tests writes:
// a gated tone, which has both silence and full-scale excursions in it.
var testFormat = pcm.Format{SampleRate: 48000, Channels: 2}

func testSignal() (fn pcm.SampleFunc) {
	return pcm.Gate(pcm.PerChannel(
		pcm.Sine(testFormat, 440, 0.8),
		pcm.Sine(testFormat, 660, 0.5),
	), 12000, 4000)
}

// writeWAVFixture writes frames of the gated tone to a WAVE file in dir and
// returns its path.
func writeWAVFixture(t *testing.T, dir string, name string, frames int64, enc wavfile.EncodingE, bits uint16) (path string) {
	t.Helper()
	src, err := pcm.NewSynthSourceE(testFormat, frames, testSignal())
	require.NoError(t, err)
	path = filepath.Join(dir, name)
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, wavfile.WriteE(context.Background(), f, testFormat, enc, bits, src))
	require.NoError(t, f.Close())
	return path
}

func TestOpenWAVUsesTheNativeReader(t *testing.T) {
	const frames int64 = 20000
	path := writeWAVFixture(t, t.TempDir(), "tone.wav", frames, wavfile.EncodingIEEEFloat, 32)

	src, kind, err := OpenE(context.Background(), path)
	require.NoError(t, err)
	require.Equal(t, KindWAV, kind)
	defer func() { require.NoError(t, src.CloseE()) }()

	require.Equal(t, testFormat, src.Format())
	require.Equal(t, frames, src.Frames())
	pcmtest.CheckSourceContract(t, src, 2000)
}

func TestReopenerGivesIndependentSources(t *testing.T) {
	const frames int64 = 8000
	path := writeWAVFixture(t, t.TempDir(), "tone.wav", frames, wavfile.EncodingIEEEFloat, 32)
	reopen := Reopener(path)

	first, err := reopen(context.Background())
	require.NoError(t, err)
	defer func() { require.NoError(t, first.CloseE()) }()
	second, err := reopen(context.Background())
	require.NoError(t, err)
	defer func() { require.NoError(t, second.CloseE()) }()
	require.NotSame(t, first, second)

	channels := int(testFormat.Channels)
	head := make([]float32, 64*channels)
	tail := make([]float32, 64*channels)
	n, err := first.ReadFramesAtE(context.Background(), 0, head)
	require.NoError(t, err)
	require.Equal(t, 64, n)
	n, err = second.ReadFramesAtE(context.Background(), frames-64, tail)
	require.NoError(t, err)
	require.Equal(t, 64, n)

	// The second source's position did not disturb the first's.
	again := make([]float32, 64*channels)
	n, err = first.ReadFramesAtE(context.Background(), 64, again)
	require.NoError(t, err)
	require.Equal(t, 64, n)
	require.NotEqual(t, head, again)
}

func TestOpenMissingFileErrors(t *testing.T) {
	_, kind, err := OpenE(context.Background(), filepath.Join(t.TempDir(), "absent.wav"))
	require.Error(t, err)
	require.Equal(t, KindUnknown, kind)
}
