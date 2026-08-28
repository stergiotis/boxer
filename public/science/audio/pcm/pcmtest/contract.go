// Package pcmtest checks a [pcm.SourceI] implementation against the read
// contract. It lives outside _test.go files so decoders in other packages
// can run it; it is test support, not production code, despite the file
// extension (CODINGSTANDARDS § Testing).
package pcmtest

import (
	"context"
	"io"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

// TestingI is the slice of testing.TB the checks need. It is satisfied by
// *testing.T, *testing.B and *rapid.T alike, so a property test can run the
// contract on every generated source.
type TestingI interface {
	require.TestingT
	Helper()
}

// CheckSourceContract asserts the [pcm.SourceI] contract on src: reads are
// chunk-invariant (any chunking yields the same samples as one read),
// partial reads happen only at the end, a read at or past the end is
// (0, io.EOF), a negative offset is a non-EOF error, and the sample count
// per read is a whole number of frames.
//
// maxFrames bounds how much of the source is compared, so a long source is
// checked over its head, its tail and a window in the middle rather than in
// full.
func CheckSourceContract(t TestingI, src pcm.SourceI, maxFrames int64) {
	t.Helper()
	ctx := context.Background()
	format := src.Format()
	require.NoError(t, format.ValidateE())
	frames := src.Frames()
	require.GreaterOrEqual(t, frames, int64(0))
	ch := int(format.Channels)

	// End-of-source semantics.
	tail := make([]float32, 4*ch)
	n, err := src.ReadFramesAtE(ctx, frames, tail)
	require.Equal(t, 0, n, "read at Frames() must return no frames")
	require.ErrorIs(t, err, io.EOF, "read at Frames() must return io.EOF")
	n, err = src.ReadFramesAtE(ctx, frames+1000, tail)
	require.Equal(t, 0, n)
	require.ErrorIs(t, err, io.EOF)
	n, err = src.ReadFramesAtE(ctx, -1, tail)
	require.Equal(t, 0, n)
	require.Error(t, err)
	require.NotErrorIs(t, err, io.EOF, "a negative offset is not end-of-source")

	// A dst smaller than one frame reads nothing without error at a valid
	// offset.
	if frames > 0 && ch > 1 {
		short := make([]float32, ch-1)
		n, err = src.ReadFramesAtE(ctx, 0, short)
		require.Equal(t, 0, n)
		require.NoError(t, err)
	}

	if frames == 0 {
		return
	}

	// Windows to compare: head, middle, tail — clipped to maxFrames each.
	win := min(frames, max(maxFrames, 1))
	starts := []int64{0}
	if frames > win {
		starts = append(starts, (frames-win)/2, frames-win)
	}
	for _, start := range starts {
		reference := readWindow(t, src, start, win)
		require.Len(t, reference, int(win)*ch)

		// Chunkings that do not divide the window, including a 1-frame
		// chunk, must reproduce the reference sample for sample.
		for _, chunk := range []int64{1, 3, 7, 64, 1000, win} {
			if chunk > win {
				continue
			}
			got := make([]float32, 0, int(win)*ch)
			buf := make([]float32, int(chunk)*ch)
			for off := start; off < start+win; {
				want := min(chunk, start+win-off)
				n, err = src.ReadFramesAtE(ctx, off, buf[:int(want)*ch])
				require.NoError(t, err, "offset %d chunk %d", off, chunk)
				require.Equal(t, int(want), n, "a read inside the source is complete (offset %d chunk %d)", off, chunk)
				got = append(got, buf[:n*ch]...)
				off += int64(n)
			}
			require.Equal(t, reference, got, "chunking by %d frames from %d changed the samples", chunk, start)
		}
	}

	// A read that crosses the end is partial, not an error.
	if frames >= 2 {
		buf := make([]float32, 3*ch)
		n, err = src.ReadFramesAtE(ctx, frames-2, buf)
		require.NoError(t, err)
		require.Equal(t, 2, n, "a read crossing the end returns the frames that exist")
		expect := readWindow(t, src, frames-2, 2)
		require.Equal(t, expect, buf[:2*ch])
	}
}

func readWindow(t TestingI, src pcm.SourceI, start, frames int64) (out []float32) {
	t.Helper()
	ch := int(src.Format().Channels)
	out = make([]float32, int(frames)*ch)
	n, err := src.ReadFramesAtE(context.Background(), start, out)
	require.NoError(t, err)
	require.Equal(t, int(frames), n)
	return out
}
