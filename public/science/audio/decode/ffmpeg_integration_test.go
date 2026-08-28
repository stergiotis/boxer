//go:build integration

package decode

import (
	"context"
	"io"
	"math"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/pcm/pcmtest"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
)

// integrationFrames is three seconds at the fixture's rate — long enough for a
// lossy codec's edge effects to be excluded and still leave most of the file
// under comparison.
const integrationFrames int64 = 3 * 48000

// edgeFrames is how much of each end the Vorbis comparison drops: the encoder's
// lookahead and its padded tail are not the same signal as the input.
const edgeFrames int64 = 4800

// requireFfmpeg skips unless both binaries the decoder needs resolve.
func requireFfmpeg(t *testing.T) {
	t.Helper()
	if _, ok := extbin.Ffmpeg.Resolve(); !ok {
		t.Skip("ffmpeg is not available on this host")
	}
	if _, ok := extbin.Ffprobe.Resolve(); !ok {
		t.Skip("ffprobe is not available on this host")
	}
}

// encodeE transcodes src to dst with the given codec arguments.
func encodeE(t *testing.T, src string, dst string, codecArgs ...string) {
	t.Helper()
	args := make([]string, 0, 8+len(codecArgs))
	args = append(args, "-nostdin", "-v", "error", "-y", "-i", src)
	args = append(args, codecArgs...)
	args = append(args, dst)
	require.NoError(t, extbin.Ffmpeg.Run(context.Background(), extbin.Opts{}, args...))
}

// fixtureSet is the WAV reference and the two compressed encodings of it.
type fixtureSet struct {
	wav    string
	flac   string
	vorbis string
}

func newFixtureSet(t *testing.T) (set fixtureSet) {
	t.Helper()
	dir := t.TempDir()
	// 16-bit PCM, so a lossless codec round-trips the reference exactly rather
	// than through a float-to-integer conversion of the encoder's choosing.
	set.wav = writeWAVFixture(t, dir, "tone.wav", integrationFrames, wavfile.EncodingPCMInt, 16)
	set.flac = filepath.Join(dir, "tone.flac")
	set.vorbis = filepath.Join(dir, "tone.ogg")
	encodeE(t, set.wav, set.flac, "-c:a", "flac")
	encodeE(t, set.wav, set.vorbis, "-c:a", "libvorbis", "-q:a", "5")
	return set
}

// readAll reads frames [0, frames) of src sequentially in chunk-frame steps.
func readAll(t *testing.T, src pcm.SourceI, frames int64, chunk int64) (samples []float32) {
	t.Helper()
	channels := int64(src.Format().Channels)
	samples = make([]float32, 0, frames*channels)
	buf := make([]float32, chunk*channels)
	for off := int64(0); off < frames; {
		want := min(chunk, frames-off)
		n, err := src.ReadFramesAtE(context.Background(), off, buf[:want*channels])
		require.NoError(t, err)
		require.Equal(t, int(want), n)
		samples = append(samples, buf[:int64(n)*channels]...)
		off += int64(n)
	}
	return samples
}

func openFfmpegFixtureE(t *testing.T, path string) (src *FfmpegSource) {
	t.Helper()
	generic, kind, err := OpenE(context.Background(), path)
	require.NoError(t, err)
	require.Equal(t, KindFfmpeg, kind)
	src, ok := generic.(*FfmpegSource)
	require.True(t, ok, "a compressed file is decoded by FfmpegSource")
	t.Cleanup(func() { require.NoError(t, src.CloseE()) })
	require.Equal(t, testFormat, src.Format())
	return src
}

// requireLengthAgrees checks the probed length against the reference within
// two percent, and that padding — frames the codec did not deliver — stayed
// negligible.
func requireLengthAgrees(t *testing.T, src *FfmpegSource) {
	t.Helper()
	require.InEpsilon(t, integrationFrames, src.Frames(), 0.02)
	require.Equal(t, src.Frames(), src.DeclaredFrames())
}

func TestFfmpegFlacMatchesTheWAVSampleForSample(t *testing.T) {
	requireFfmpeg(t)
	set := newFixtureSet(t)

	reference, err := wavfile.OpenE(set.wav)
	require.NoError(t, err)
	defer func() { require.NoError(t, reference.CloseE()) }()
	src := openFfmpegFixtureE(t, set.flac)
	requireLengthAgrees(t, src)

	shared := min(reference.Frames(), src.Frames())
	want := readAll(t, reference, shared, 4096)
	got := readAll(t, src, shared, 4096)
	require.Len(t, got, len(want))
	for i := range want {
		require.InDelta(t, want[i], got[i], 1e-4, "sample %d", i)
	}
	require.LessOrEqual(t, src.Padded(), integrationFrames/100, "a lossless codec should deliver its declared length")
}

func TestFfmpegVorbisIsCloseToTheWAVAwayFromTheEdges(t *testing.T) {
	requireFfmpeg(t)
	set := newFixtureSet(t)

	reference, err := wavfile.OpenE(set.wav)
	require.NoError(t, err)
	defer func() { require.NoError(t, reference.CloseE()) }()
	src := openFfmpegFixtureE(t, set.vorbis)
	requireLengthAgrees(t, src)

	shared := min(reference.Frames(), src.Frames())
	want := readAll(t, reference, shared, 4096)
	got := readAll(t, src, shared, 4096)
	require.Len(t, got, len(want))

	channels := int64(testFormat.Channels)
	from := edgeFrames * channels
	to := (shared - edgeFrames) * channels
	require.Greater(t, to, from)
	sum := 0.0
	for i := from; i < to; i++ {
		d := float64(want[i] - got[i])
		sum += d * d
	}
	rms := math.Sqrt(sum / float64(to-from))
	require.Less(t, rms, 0.05, "vorbis at -q:a 5 should track the input closely")
	require.LessOrEqual(t, src.Padded(), integrationFrames/100)
}

func TestFfmpegSourceContractAndRestarts(t *testing.T) {
	requireFfmpeg(t)
	set := newFixtureSet(t)

	src := openFfmpegFixtureE(t, set.flac)
	pcmtest.CheckSourceContract(t, src, 2000)
	require.Positive(t, src.Restarts(), "the contract's positioned reads restart the process")

	// A fresh source read strictly forwards never restarts: the sequential
	// peaks build is the access pattern the streaming decoder is shaped for.
	sequential := openFfmpegFixtureE(t, set.flac)
	readAll(t, sequential, sequential.Frames(), 4096)
	require.Zero(t, sequential.Restarts())
}

func TestFfmpegReadHonoursACancelledContext(t *testing.T) {
	requireFfmpeg(t)
	set := newFixtureSet(t)
	src := openFfmpegFixtureE(t, set.flac)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	buf := make([]float32, 4096*int(testFormat.Channels))
	n, err := src.ReadFramesAtE(ctx, 0, buf)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, n)

	// The source is still usable with a live context.
	n, err = src.ReadFramesAtE(context.Background(), 0, buf)
	require.NoError(t, err)
	require.Equal(t, 4096, n)
}

func TestFfmpegReopenerSourcesReadConcurrently(t *testing.T) {
	requireFfmpeg(t)
	set := newFixtureSet(t)
	reopen := Reopener(set.flac)

	reference, err := wavfile.OpenE(set.wav)
	require.NoError(t, err)
	defer func() { require.NoError(t, reference.CloseE()) }()
	shared := min(reference.Frames(), integrationFrames)
	want := readAll(t, reference, shared, 4096)

	var wg sync.WaitGroup
	results := make([][]float32, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			src, oerr := reopen(context.Background())
			require.NoError(t, oerr)
			defer func() { require.NoError(t, src.CloseE()) }()
			results[i] = readAll(t, src, shared, 4096)
		}()
	}
	wg.Wait()

	for i, got := range results {
		require.Len(t, got, len(want), "source %d", i)
		for j := range want {
			require.InDelta(t, want[j], got[j], 1e-4, "source %d sample %d", i, j)
		}
	}
}

func TestFfmpegPadsWhenTheCodecFallsShortOfTheDeclaredLength(t *testing.T) {
	requireFfmpeg(t)
	set := newFixtureSet(t)
	src := openFfmpegFixtureE(t, set.flac)

	// The pad rule keeps a consumer's preallocation valid when a codec's
	// output is shorter than its container's duration. These fixtures decode
	// to exactly their declared length, so the shortfall is staged here by
	// declaring more frames than the file holds — reaching into the source's
	// own field, because no encoder reliably produces the mismatch on demand.
	const shortfall int64 = 1000
	const delivered int64 = 8
	declared := src.frames + shortfall
	src.frames = declared

	channels := int(testFormat.Channels)
	tail := make([]float32, (shortfall+delivered)*int64(channels))
	n, err := src.ReadFramesAtE(context.Background(), declared-shortfall-delivered, tail)
	require.NoError(t, err)
	require.Equal(t, int(shortfall+delivered), n, "the declared length is delivered in full")
	require.Equal(t, shortfall, src.Padded())
	for i := int(delivered) * channels; i < len(tail); i++ {
		require.Zero(t, tail[i], "a padded frame reads as silence")
	}

	// And the source still ends where it declared it would.
	_, err = src.ReadFramesAtE(context.Background(), declared, tail)
	require.ErrorIs(t, err, io.EOF)
}
