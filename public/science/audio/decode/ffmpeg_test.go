package decode

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

func TestFfmpegArgsPlaceTheSeekBeforeTheInput(t *testing.T) {
	src := &FfmpegSource{path: "/tmp/a.flac", format: testFormat}

	fromStart := src.appendArgs(nil, 0, src.path)
	require.Equal(t, []string{
		"-nostdin", "-v", "error",
		"-i", "/tmp/a.flac",
		"-map", "0:a:0",
		"-f", "f32le",
		"-acodec", "pcm_f32le",
		"-ac", "2",
		"-ar", "48000",
		"-",
	}, fromStart, "no -ss at all when starting at the beginning")

	seeked := src.appendArgs(nil, 24000, src.path)
	require.Equal(t, []string{"-nostdin", "-v", "error", "-ss", "0.500000000", "-i", "/tmp/a.flac"}, seeked[:7])
	require.Less(t, indexOf(seeked, "-ss"), indexOf(seeked, "-i"), "-ss before -i is the accurate seek")
}

func indexOf(args []string, want string) (i int) {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func TestFfmpegReadAfterCloseIsAnError(t *testing.T) {
	src := &FfmpegSource{path: "/tmp/a.flac", format: testFormat, frames: 1000}
	require.NoError(t, src.CloseE())
	require.NoError(t, src.CloseE(), "closing twice is not an error")

	n, err := src.ReadFramesAtE(context.Background(), 0, make([]float32, 8))
	require.Error(t, err)
	require.Zero(t, n)
}

func TestReadBufferStaysWithinItsBounds(t *testing.T) {
	cases := map[string]pcm.Format{
		"telephone mono": {SampleRate: 8000, Channels: 1},
		"cd stereo":      {SampleRate: 44100, Channels: 2},
		"studio 32ch":    {SampleRate: 384000, Channels: 32},
	}
	for name, format := range cases {
		t.Run(name, func(t *testing.T) {
			n := readBufferBytes(format)
			require.GreaterOrEqual(t, n, readBufferMinBytes)
			require.LessOrEqual(t, n, readBufferMaxBytes)
		})
	}
	require.Equal(t, 48000*2*4*int(readAheadMillis)/1000, readBufferBytes(testFormat))
}

func TestStderrTailKeepsTheLastBytes(t *testing.T) {
	tail := newStderrTail(16)
	_, err := tail.Write([]byte("abcdefgh"))
	require.NoError(t, err)
	require.Equal(t, "abcdefgh", tail.String())

	_, err = tail.Write([]byte("0123456789"))
	require.NoError(t, err)
	require.Equal(t, "cdefgh0123456789", tail.String())

	// One write larger than the whole tail keeps only its end.
	_, err = tail.Write(bytes.Repeat([]byte("z"), 40))
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("z", 16), tail.String())
}
