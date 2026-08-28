package wavfile

import (
	"context"
	"encoding/binary"
	"io"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

// writeChunkFrames is how many frames one read/encode/write cycle moves. It
// bounds the two scratch buffers rather than the file, so writing a
// twelve-hour recording costs the same memory as writing a second of one.
const writeChunkFrames int64 = 4096

// WriteE writes every frame of src to w as a canonical little-endian WAVE
// stream: a 16-byte fmt chunk followed by the data chunk. When the byte
// counts would not fit RIFF's 32-bit size fields the RF64 form with a ds64
// chunk is written instead, so a recording past 4 GiB needs no decision from
// the caller.
//
// src.Frames() is the length written and must not change during the call; w
// is not closed. Supported widths are 8, 16, 24 and 32 bits for
// [EncodingPCMInt] and 32 or 64 for [EncodingIEEEFloat].
func WriteE(ctx context.Context, w io.Writer, format pcm.Format, enc EncodingE, bits uint16, src pcm.SourceI) (err error) {
	if w == nil {
		return eh.New("nil writer")
	}
	if src == nil {
		return eh.New("nil source")
	}
	return writeSpecE(ctx, w, headerSpec{
		format:   format,
		frames:   src.Frames(),
		encoding: enc,
		bits:     bits,
	}, src)
}

// writeSpecE is WriteE over an explicit header spec, so a test can ask for
// the RF64 form at a size that fits in RIFF.
func writeSpecE(ctx context.Context, w io.Writer, spec headerSpec, src pcm.SourceI) (err error) {
	header, _, err := appendHeader(make([]byte, 0, 64), spec)
	if err != nil {
		return err
	}
	_, err = w.Write(header)
	if err != nil {
		return eh.Errorf("write wave header: %w", err)
	}

	ch := int(spec.format.Channels)
	width := int(spec.bits / 8)
	samples := make([]float32, int(writeChunkFrames)*ch)
	raw := make([]byte, 0, int(writeChunkFrames)*ch*width)
	remaining := spec.frames
	offset := int64(0)
	for remaining > 0 {
		want := min(writeChunkFrames, remaining)
		var n int
		n, err = src.ReadFramesAtE(ctx, offset, samples[:int(want)*ch])
		if err != nil {
			return eb.Build().Int64("frameOffset", offset).Errorf("read source frames: %w", err)
		}
		if n <= 0 {
			return eb.Build().
				Int64("frameOffset", offset).
				Int64("frames", spec.frames).
				Errorf("source ended before the frame count it declared")
		}
		raw = appendSamples(raw[:0], samples[:n*ch], spec.encoding, spec.bits)
		_, err = w.Write(raw)
		if err != nil {
			return eh.Errorf("write samples: %w", err)
		}
		offset += int64(n)
		remaining -= int64(n)
	}
	if spec.dataSize()&1 == 1 {
		_, err = w.Write([]byte{0})
		if err != nil {
			return eh.Errorf("write chunk pad byte: %w", err)
		}
	}
	return nil
}

// appendSamples encodes interleaved float32 into the container width, the
// inverse of [File.decode].
func appendSamples(dst []byte, samples []float32, enc EncodingE, bits uint16) (out []byte) {
	out = dst
	switch enc {
	case EncodingPCMInt:
		switch bits {
		case 8:
			for _, s := range samples {
				out = append(out, byte(quantise(s, 128, -128, 127)+128))
			}
		case 16:
			for _, s := range samples {
				out = binary.LittleEndian.AppendUint16(out, uint16(int16(quantise(s, 32768, -32768, 32767))))
			}
		case 24:
			for _, s := range samples {
				v := quantise(s, 8388608, -8388608, 8388607)
				out = append(out, byte(v), byte(v>>8), byte(v>>16))
			}
		case 32:
			for _, s := range samples {
				out = binary.LittleEndian.AppendUint32(out, uint32(int32(quantise(s, 2147483648, -2147483648, 2147483647))))
			}
		}
	case EncodingIEEEFloat:
		switch bits {
		case 32:
			for _, s := range samples {
				out = binary.LittleEndian.AppendUint32(out, math.Float32bits(s))
			}
		case 64:
			for _, s := range samples {
				out = binary.LittleEndian.AppendUint64(out, math.Float64bits(float64(s)))
			}
		}
	}
	return out
}

// quantise scales a sample to an integer code, rounding to nearest and
// clamping to the container's range. A NaN becomes silence rather than an
// arbitrary code.
func quantise(s float32, scale float64, lo int64, hi int64) (v int64) {
	x := math.Round(float64(s) * scale)
	if math.IsNaN(x) {
		return 0
	}
	if x <= float64(lo) {
		return lo
	}
	if x >= float64(hi) {
		return hi
	}
	return int64(x)
}
