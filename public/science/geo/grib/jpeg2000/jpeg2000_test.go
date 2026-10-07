package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func readTestdata(t *testing.T, name string) (b []byte) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return
}

func readSamples(t *testing.T, name string) (v []int32) {
	t.Helper()
	b := readTestdata(t, name)
	v = make([]int32, len(b)/4)
	for i := range v {
		v[i] = int32(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return
}

// TestOpenJPEGCodestreamsInsideTheProfileDecodeExactly: a 47×39 16-bit
// image encoded by OpenJPEG under every option the profile accepts — the
// defaults, context reset, vertically causal contexts, predictable
// termination, segmentation symbols, SOP and EPH markers, a different
// progression, no decomposition, smaller code-blocks, a 16×16 precinct
// partition, the JP2 wrapper — and an 8-bit one, each decoded to the
// source samples.
func TestOpenJPEGCodestreamsInsideTheProfileDecodeExactly(t *testing.T) {
	want16 := readSamples(t, "src16.i32")
	want8 := readSamples(t, "src8.i32")
	entries, err := os.ReadDir("testdata")
	require.NoError(t, err)
	n := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "ok_") {
			continue
		}
		n++
		img, err := Decode(readTestdata(t, name))
		require.NoError(t, err, name)
		require.Equal(t, 47, img.Width, name)
		require.Equal(t, 39, img.Height, name)
		want := want16
		if strings.Contains(name, "8bit") {
			want = want8
			require.EqualValues(t, 8, img.Precision, name)
		} else {
			require.EqualValues(t, 16, img.Precision, name)
		}
		require.Equal(t, want, img.Samples, name)
	}
	require.GreaterOrEqual(t, n, 11)
}

// TestOutsideTheProfileIsRefusedByName holds the refusal contract on
// codestreams OpenJPEG wrote with the features the decoder does not do.
func TestOutsideTheProfileIsRefusedByName(t *testing.T) {
	for name, feature := range map[string]string{
		"no_97.j2k":      "9/7 irreversible wavelet",
		"no_layers.j2k":  "2 quality layers",
		"no_tiles.j2k":   "4 tiles",
		"no_bypass.j2k":  "arithmetic coder bypass",
		"no_termall.j2k": "termination on each coding pass",
	} {
		_, err := Decode(readTestdata(t, name))
		require.ErrorIs(t, err, ErrUnsupported, name)
		got, ok := UnsupportedFeature(err)
		require.True(t, ok, name)
		require.Equal(t, feature, got, name)
	}
}

// TestTagTreeSpecExample decodes the worked example of B.10.2: the leaves
// q3(0,0) = 1, q3(1,0) = 3 and q3(2,0) = 2 of a 3-wide array coded as
// 01111, 001 and 101.
func TestTagTreeSpecExample(t *testing.T) {
	// Bits: 01111 001 101 = 0111 1001 101x xxxx
	br := headerBits{b: []byte{0x79, 0xa0}}
	tree := newTagTree(6, 3)
	require.EqualValues(t, 1, tree.decodeFull(&br, 0, 0))
	require.EqualValues(t, 3, tree.decodeFull(&br, 1, 0))
	require.EqualValues(t, 2, tree.decodeFull(&br, 2, 0))
	require.False(t, br.bad)
}

// TestPassCountCodewords is Table B.4.
func TestPassCountCodewords(t *testing.T) {
	for _, tc := range []struct {
		bits string
		want int
	}{{"0", 1}, {"10", 2}, {"1100", 3}, {"1101", 4}, {"1110", 5}, {"111100000", 6}, {"111111110", 36}, {"1111111110000000", 37}, {"1111111111111111", 164}} {
		// Pack with the encoder's stuffing rule: a byte of 0xFF is
		// followed by a byte whose top bit is a stuffed zero.
		var buf []byte
		var cur byte
		n := 0
		limit := 8
		for _, c := range tc.bits {
			cur = cur<<1 | byte(c-'0')
			n++
			if n == limit {
				buf = append(buf, cur)
				limit = 8
				if cur == 0xff {
					limit = 7
				}
				cur, n = 0, 0
			}
		}
		if n > 0 {
			buf = append(buf, cur<<(limit-n))
		}
		br := headerBits{b: buf}
		require.Equal(t, tc.want, readPassCount(&br), tc.bits)
	}
}

// forward53 is the analysis half of F.4.8.2 (for the test only): the
// inverse must undo it for any signal and any coordinate range.
func forward53(sig []int32, i0 int32) (out []int32) {
	n := len(sig)
	out = make([]int32, n)
	copy(out, sig)
	if n == 1 {
		if i0&1 == 1 {
			out[0] *= 2
		}
		return
	}
	ext := func(k int) int32 {
		period := 2 * (n - 1)
		idx := k % period
		if idx < 0 {
			idx += period
		}
		if idx >= n {
			idx = period - idx
		}
		return sig[idx]
	}
	// Odd coordinates first (high-pass), then even (low-pass), each
	// referring to the extended input and the already computed highs.
	high := func(k int) int32 { // k relative, coordinate i0+k odd
		return ext(k) - (ext(k-1)+ext(k+1))>>1
	}
	for k := 0; k < n; k++ {
		if (i0+int32(k))&1 == 1 {
			out[k] = high(k)
		}
	}
	highAt := func(k int) int32 {
		// symmetric extension of the high-pass signal about the ends
		period := 2 * (n - 1)
		idx := k % period
		if idx < 0 {
			idx += period
		}
		if idx >= n {
			idx = period - idx
		}
		if (i0+int32(idx))&1 == 1 {
			return out[idx]
		}
		return high(idx)
	}
	for k := 0; k < n; k++ {
		if (i0+int32(k))&1 == 0 {
			out[k] = sig[k] + (highAt(k-1)+highAt(k+1)+2)>>2
		}
	}
	return
}

func TestInverseLiftingUndoesTheForwardTransform(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 40).Draw(rt, "n")
		i0 := int32(rapid.IntRange(0, 9).Draw(rt, "i0"))
		sig := make([]int32, n)
		for i := range sig {
			sig[i] = int32(rapid.IntRange(-3000, 3000).Draw(rt, "v"))
		}
		coeffs := forward53(sig, i0)
		got := make([]int32, n)
		copy(got, coeffs)
		scratch := make([]int32, n+8)
		sr1D(got, i0, i0+int32(n), scratch, nil)
		require.Equal(t, sig, got)
	})
}

// TestDamagedCodestreamsNeverPanic mutates the fixtures and asks only that
// the decoder return one of its two errors or an image.
func TestDamagedCodestreamsNeverPanic(t *testing.T) {
	originals := [][]byte{readTestdata(t, "ok_default.j2k"), readTestdata(t, "ok_sop_eph.j2k"), readTestdata(t, "ok_jp2.jp2"), readTestdata(t, "ok_3levels_cb16.j2k")}
	rapid.Check(t, func(rt *rapid.T) {
		buf := bytes.Clone(rapid.SampledFrom(originals).Draw(rt, "file"))
		if rapid.Bool().Draw(rt, "truncate") {
			buf = buf[:rapid.IntRange(0, len(buf)).Draw(rt, "length")]
		}
		for range rapid.IntRange(0, 16).Draw(rt, "flips") {
			if len(buf) == 0 {
				break
			}
			buf[rapid.IntRange(0, len(buf)-1).Draw(rt, "at")] = rapid.Byte().Draw(rt, "byte")
		}
		_, err := Decode(buf)
		if err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrUnsupported) {
			rt.Fatalf("unexpected error class: %v", err)
		}
	})
}
