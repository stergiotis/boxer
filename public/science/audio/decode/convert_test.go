package decode

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// appendF32LE encodes samples the way ffmpeg's `-f f32le` output does.
func appendF32LE(dst []byte, samples []float32) (out []byte) {
	out = dst
	for _, s := range samples {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(s))
	}
	return out
}

func TestAssemblerDecodesWholeBuffer(t *testing.T) {
	want := []float32{0, 1, -1, 0.5, -0.25}
	var asm f32Assembler
	dst := make([]float32, len(want))
	n := asm.Decode(appendF32LE(nil, want), dst)
	require.Equal(t, len(want), n)
	require.Equal(t, want, dst)
	require.Zero(t, asm.Pending())
}

func TestAssemblerCarriesASplitSampleAcrossChunks(t *testing.T) {
	want := []float32{0.125, -0.5, 0.75}
	src := appendF32LE(nil, want)
	// Cut mid-sample: three bytes of the second sample arrive in the first
	// chunk, its fourth byte in the second.
	const cut = 7
	var asm f32Assembler
	dst := make([]float32, len(want))
	n := asm.Decode(src[:cut], dst)
	require.Equal(t, 1, n, "only the first sample is complete")
	require.Equal(t, 3, asm.Pending(), "the split sample's bytes are carried")
	n += asm.Decode(src[cut:], dst[n:])
	require.Equal(t, len(want), n)
	require.Equal(t, want, dst)
	require.Zero(t, asm.Pending())
}

func TestAssemblerCarriesSamplesTheDestinationCannotTake(t *testing.T) {
	want := []float32{1, 2, 3, 4}
	src := appendF32LE(nil, want)
	var asm f32Assembler
	head := make([]float32, 2)
	require.Equal(t, 2, asm.Decode(src, head))
	require.Equal(t, want[:2], head)
	require.Equal(t, 2*bytesPerSample, asm.Pending())

	tail := make([]float32, 2)
	require.Equal(t, 2, asm.Decode(nil, tail), "the carry is drained without new input")
	require.Equal(t, want[2:], tail)
	require.Zero(t, asm.Pending())
}

func TestAssemblerResetDropsTheCarry(t *testing.T) {
	var asm f32Assembler
	dst := make([]float32, 1)
	require.Zero(t, asm.Decode([]byte{1, 2, 3}, dst))
	require.Equal(t, 3, asm.Pending())
	asm.Reset()
	require.Zero(t, asm.Pending())
	// A restarted process's bytes must not be prefixed by its predecessor's
	// tail.
	require.Equal(t, 1, asm.Decode(appendF32LE(nil, []float32{0.5}), dst))
	require.Equal(t, float32(0.5), dst[0])
}

func TestAssemblerIsChunkingInvariant(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		samples := rapid.SliceOfN(rapid.Float32Range(-1, 1), 0, 64).Draw(t, "samples")
		src := appendF32LE(nil, samples)
		cuts := rapid.SliceOfN(rapid.IntRange(0, len(src)), 0, 8).Draw(t, "cuts")

		var asm f32Assembler
		dst := make([]float32, len(samples))
		filled := 0
		prev := 0
		for _, cut := range append(cuts, len(src)) {
			if cut < prev {
				continue
			}
			filled += asm.Decode(src[prev:cut], dst[filled:])
			prev = cut
		}
		filled += asm.Decode(nil, dst[filled:])
		require.Equal(t, len(samples), filled)
		require.Equal(t, append([]float32{}, samples...), dst)
	})
}
