package wavfile

// Fuzz targets for the WAVE reader and writer:
//
//	FuzzNewReader           — NewReaderE is total on arbitrary bytes: no
//	                          panic, and header parsing allocates in
//	                          proportion to the stream, never to a size or
//	                          count a field declares. An accepted stream's
//	                          frames lie inside it and satisfy the
//	                          pcm.SourceI read contract.
//	FuzzWriteReadRoundTrip  — WriteE∘NewReaderE∘ReadFramesAtE returns every
//	                          sample, in both the RIFF and the RF64 form:
//	                          float containers bit-for-bit (NaN as NaN),
//	                          integer containers within one quantisation step
//	                          of the sample clamped to the container's range,
//	                          with NaN as silence.
//
// Run e.g.:
//
//	go test -run xxx -fuzz FuzzNewReader -fuzztime 60s ./public/science/audio/wavfile/

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/pcm/pcmtest"
)

// fuzzMaxInput keeps per-exec cost low; nothing in the container format
// needs more than a few hundred bytes to reach.
const fuzzMaxInput = 1 << 16

// fuzzAllocSlack is the fixed allocation headroom NewReaderE gets on top of
// the stream-proportional part: the File itself and error construction.
const fuzzAllocSlack = 64 << 10

func fuzzWritten(f *testing.F, spec headerSpec, samples []float32) {
	src, err := pcm.NewMemSourceE(spec.format, samples)
	if err != nil {
		f.Fatal(err)
	}
	spec.frames = src.Frames()
	var buf bytes.Buffer
	err = writeSpecE(context.Background(), &buf, spec, src)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(buf.Bytes())
}

func FuzzNewReader(f *testing.F) {
	stereo := pcm.Format{SampleRate: 48000, Channels: 2}
	samples := []float32{0, 0.5, -0.5, 1, -1, 0.25}
	for _, sf := range allSampleFormats {
		fuzzWritten(f, headerSpec{format: stereo, encoding: sf.encoding, bits: sf.bits}, samples)
	}
	fuzzWritten(f, headerSpec{format: stereo, encoding: EncodingPCMInt, bits: 16, rf64: true}, samples)
	fuzzWritten(f, headerSpec{format: pcm.Format{SampleRate: 8000, Channels: 1}, encoding: EncodingPCMInt, bits: 24}, []float32{0.1})

	chunks := appendChunk(nil, "fmt ", fmtBodyExtensible(2, 48000, 24, 20, subFormatGUID(formatTagPCM)))
	chunks = appendChunk(chunks, "JUNK", []byte("odd"))
	chunks = appendChunk(chunks, "data", pcm24Body([]int32{1, -1, 4096, -4096}))
	f.Add(riffContainer(chunks))

	table := binary.LittleEndian.AppendUint64([]byte("JUNK"), 8)
	chunks = appendChunk(nil, "ds64", ds64Body(0, 1<<40, 2, table))
	chunks = ds64EscapedChunk(chunks, "JUNK", []byte("metadata"))
	chunks = appendChunk(chunks, "fmt ", fmtBody(formatTagPCM, 2, 48000, 16))
	chunks = ds64EscapedChunk(chunks, "data", pcm16Body([]int16{7, -7, 9, -9}))
	f.Add(container("BW64", maxUint32, chunks))
	f.Add(ds64TableOverflowFixture())

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzMaxInput {
			t.Skip()
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		file, err := NewReaderE(bytes.NewReader(data), int64(len(data)))
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc
		if limit := uint64(4*len(data) + fuzzAllocSlack); allocated > limit {
			t.Fatalf("header parse allocated %d bytes for a %d-byte stream (limit %d)", allocated, len(data), limit)
		}
		if err != nil {
			return // rejection is fine; panics and runaway allocation are not
		}

		require.NoError(t, file.Format().ValidateE())
		require.GreaterOrEqual(t, file.Frames(), int64(0))
		end := file.dataOff + file.Frames()*int64(file.blockAlign)
		require.LessOrEqual(t, end, int64(len(data)), "frames extend past the stream")
		pcmtest.CheckSourceContract(t, file, 64)
	})
}

func FuzzWriteReadRoundTrip(f *testing.F) {
	f.Add(uint8(1), uint16(2), uint32(48000), false, []byte{0, 0, 0, 0, 0, 0, 0x80, 0x3f})
	f.Add(uint8(4), uint16(1), uint32(44100), true, []byte{0, 0, 0xc0, 0x7f, 0, 0, 0x80, 0xff})
	f.Add(uint8(0), uint16(3), uint32(8000), false, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
	f.Add(uint8(3), uint16(1), uint32(1), true, []byte{0xff, 0xff, 0xff, 0x7f})

	ctx := context.Background()
	f.Fuzz(func(t *testing.T, formatIdx uint8, channels uint16, rate uint32, rf64 bool, raw []byte) {
		if len(raw) > fuzzMaxInput {
			t.Skip()
		}
		sf := allSampleFormats[int(formatIdx)%len(allSampleFormats)]
		format := pcm.Format{SampleRate: max(rate, 1), Channels: channels%16 + 1}
		ch := int(format.Channels)
		samples := make([]float32, len(raw)/4/ch*ch)
		for i := range samples {
			samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}

		src, err := pcm.NewMemSourceE(format, samples)
		require.NoError(t, err)
		var buf bytes.Buffer
		spec := headerSpec{format: format, frames: src.Frames(), encoding: sf.encoding, bits: sf.bits, rf64: rf64}
		require.NoError(t, writeSpecE(ctx, &buf, spec, src))

		written := buf.Bytes()
		file, err := NewReaderE(bytes.NewReader(written), int64(len(written)))
		require.NoError(t, err)
		require.Equal(t, format, file.Format())
		require.Equal(t, src.Frames(), file.Frames())
		require.Equal(t, rf64, file.IsRF64())
		require.False(t, file.IsTruncated())

		got := make([]float32, len(samples))
		if len(got) > 0 {
			n, rerr := file.ReadFramesAtE(ctx, 0, got)
			require.NoError(t, rerr)
			require.Equal(t, int(src.Frames()), n)
		}
		if sf.encoding == EncodingIEEEFloat {
			for i, s := range samples {
				if math.IsNaN(float64(s)) {
					require.True(t, math.IsNaN(float64(got[i])), "sample %d: NaN read back as %v", i, got[i])
					continue
				}
				require.Equal(t, math.Float32bits(s), math.Float32bits(got[i]), "sample %d", i)
			}
			return
		}
		// Integer codes span [-scale, scale-1], so the largest positive
		// sample the container holds is just under 1.
		scale := float64(int64(1) << (sf.bits - 1))
		tol := roundTripTolerance(sf)
		for i, s := range samples {
			want := float64(s)
			if math.IsNaN(want) {
				want = 0
			}
			want = math.Max(-1, math.Min(want, (scale-1)/scale))
			require.InDelta(t, want, float64(got[i]), tol, "sample %d (%v)", i, s)
		}
	})
}
