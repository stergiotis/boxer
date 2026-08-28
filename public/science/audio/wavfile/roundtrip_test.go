package wavfile

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/pcm/pcmtest"
)

// sampleFormat pairs an encoding with a width the writer and the reader both
// implement.
type sampleFormat struct {
	encoding EncodingE
	bits     uint16
}

var allSampleFormats = []sampleFormat{
	{EncodingPCMInt, 8},
	{EncodingPCMInt, 16},
	{EncodingPCMInt, 24},
	{EncodingPCMInt, 32},
	{EncodingIEEEFloat, 32},
	{EncodingIEEEFloat, 64},
}

// roundTripTolerance is the largest absolute error a write/read cycle can
// introduce at a given width.
func roundTripTolerance(sf sampleFormat) (tol float64) {
	if sf.encoding == EncodingIEEEFloat {
		// binary32 is the transport format, and binary64 holds it exactly.
		return 0
	}
	switch sf.bits {
	case 8:
		return 1.0 / 128
	case 16:
		return 1.0 / 32768
	}
	// 24-bit quantisation, and — for 32-bit codes — the 24-bit mantissa of
	// the float32 the code is read back into, which is the coarser of the
	// two.
	return 1.0 / 8388608
}

func TestWriteReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	rapid.Check(t, func(rt *rapid.T) {
		format := pcm.Format{
			SampleRate: rapid.Uint32Range(1, 384000).Draw(rt, "sampleRate"),
			Channels:   rapid.Uint16Range(1, 8).Draw(rt, "channels"),
		}
		sf := rapid.SampledFrom(allSampleFormats).Draw(rt, "sampleFormat")
		frames := rapid.IntRange(0, 2000).Draw(rt, "frames")
		ch := int(format.Channels)
		total := frames * ch
		want := rapid.SliceOfN(rapid.Float32Range(-1, 1), total, total).Draw(rt, "samples")

		src, err := pcm.NewMemSourceE(format, want)
		require.NoError(rt, err)
		var buf bytes.Buffer
		err = WriteE(ctx, &buf, format, sf.encoding, sf.bits, src)
		require.NoError(rt, err)

		raw := buf.Bytes()
		file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
		require.NoError(rt, err)
		require.Equal(rt, format, file.Format())
		require.Equal(rt, int64(frames), file.Frames())
		require.Equal(rt, sf.encoding, file.Encoding())
		require.Equal(rt, sf.bits, file.BitsPerSample())
		require.Equal(rt, sf.bits, file.ValidBitsPerSample())
		require.False(rt, file.IsRF64())
		require.False(rt, file.IsTruncated())

		got := make([]float32, total)
		if total > 0 {
			var n int
			n, err = file.ReadFramesAtE(ctx, 0, got)
			require.NoError(rt, err)
			require.Equal(rt, frames, n)
		}
		tol := roundTripTolerance(sf)
		if tol == 0 {
			require.Equal(rt, want, got)
		} else {
			for i := range want {
				require.InDelta(rt, float64(want[i]), float64(got[i]), tol, "sample %d", i)
			}
		}
		pcmtest.CheckSourceContract(rt, file, 2000)
		require.NoError(rt, file.CloseE())
	})
}

// A zero-frame stream is a legal WAVE and the read contract has to hold on
// it, which is where every off-by-one in the bounds arithmetic shows up.
func TestWriteReadZeroFrames(t *testing.T) {
	ctx := context.Background()
	for _, sf := range allSampleFormats {
		t.Run(fmt.Sprintf("%s%d", sf.encoding, sf.bits), func(t *testing.T) {
			format := pcm.Format{SampleRate: 44100, Channels: 2}
			src, err := pcm.NewMemSourceE(format, nil)
			require.NoError(t, err)
			var buf bytes.Buffer
			require.NoError(t, WriteE(ctx, &buf, format, sf.encoding, sf.bits, src))

			raw := buf.Bytes()
			file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
			require.NoError(t, err)
			require.Equal(t, int64(0), file.Frames())
			require.Equal(t, format, file.Format())
			require.Zero(t, format.FramesToDuration(file.Frames()))
			pcmtest.CheckSourceContract(t, file, 2000)
		})
	}
}

// An odd data-chunk size needs the pad byte, or the reader walks into the
// samples looking for a chunk header.
func TestWriteOddDataSizePadsTheChunk(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	src, err := pcm.NewMemSourceE(format, []float32{0.5, -0.5, 0.25})
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, WriteE(context.Background(), &buf, format, EncodingPCMInt, 8, src))
	require.Equal(t, int(riffHeaderSize+chunkHeaderSize+fmtChunkSize+chunkHeaderSize)+3+1, buf.Len())

	raw := buf.Bytes()
	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	require.Equal(t, int64(3), file.Frames())
}

// The scratch buffer is a struct field so a long sequential read allocates
// once; a shrinking request must reuse it rather than trim capacity.
func TestReadFramesReusesScratch(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 2}
	samples := make([]float32, 2*512)
	for i := range samples {
		samples[i] = float32(i%97)/97 - 0.5
	}
	src, err := pcm.NewMemSourceE(format, samples)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, WriteE(context.Background(), &buf, format, EncodingPCMInt, 24, src))
	raw := buf.Bytes()
	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)

	big := make([]float32, 2*256)
	_, err = file.ReadFramesAtE(context.Background(), 0, big)
	require.NoError(t, err)
	grown := cap(file.scratch)
	require.Equal(t, 256*2*3, grown)
	small := make([]float32, 2*4)
	_, err = file.ReadFramesAtE(context.Background(), 100, small)
	require.NoError(t, err)
	require.Equal(t, grown, cap(file.scratch))
	_, err = file.ReadFramesAtE(context.Background(), 0, big)
	require.NoError(t, err)
	require.Equal(t, grown, cap(file.scratch))
}

func TestReadFramesHonoursContextCancellation(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	src, err := pcm.NewMemSourceE(format, []float32{0, 0.5, -0.5, 1})
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, WriteE(context.Background(), &buf, format, EncodingPCMInt, 16, src))
	raw := buf.Bytes()
	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dst := make([]float32, 4)
	n, err := file.ReadFramesAtE(ctx, 0, dst)
	require.Equal(t, 0, n)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWriteRejects(t *testing.T) {
	ctx := context.Background()
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	src, err := pcm.NewMemSourceE(format, []float32{0, 1})
	require.NoError(t, err)

	require.Error(t, WriteE(ctx, nil, format, EncodingPCMInt, 16, src))
	require.Error(t, WriteE(ctx, &bytes.Buffer{}, format, EncodingPCMInt, 16, nil))
	require.Error(t, WriteE(ctx, &bytes.Buffer{}, format, EncodingPCMInt, 12, src))
	require.Error(t, WriteE(ctx, &bytes.Buffer{}, pcm.Format{}, EncodingPCMInt, 16, src))
}

func TestEncodingString(t *testing.T) {
	require.Equal(t, "pcmInt", EncodingPCMInt.String())
	require.Equal(t, "ieeeFloat", EncodingIEEEFloat.String())
	require.Equal(t, "unknown", EncodingUnknown.String())
	require.Len(t, AllEncodings, 2)
}
