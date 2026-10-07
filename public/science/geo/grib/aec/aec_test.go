package aec

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestParamsOutsideTheStandardAreRefused(t *testing.T) {
	good := Params{Bits: 12, BlockSize: 32, ReferenceInterval: 128, Preprocess: true}
	require.NoError(t, good.checkE())
	for _, bad := range []Params{
		{Bits: 0, BlockSize: 32, ReferenceInterval: 128},
		{Bits: 33, BlockSize: 32, ReferenceInterval: 128},
		{Bits: 12, BlockSize: 12, ReferenceInterval: 128},
		{Bits: 12, BlockSize: 32, ReferenceInterval: 0},
		{Bits: 12, BlockSize: 32, ReferenceInterval: 4097},
		{Bits: 5, BlockSize: 32, ReferenceInterval: 1, Restricted: true},
	} {
		_, err := Decode(nil, nil, 1, bad)
		require.ErrorIs(t, err, ErrParams, "%+v", bad)
	}
}

func TestIdentifierLengthFollowsTable51(t *testing.T) {
	for _, tc := range []struct {
		bits       uint8
		restricted bool
		want       uint8
	}{{1, true, 1}, {2, true, 1}, {3, true, 2}, {4, true, 2}, {1, false, 3}, {8, false, 3}, {9, false, 4}, {16, false, 4}, {17, false, 5}, {32, false, 5}} {
		require.Equal(t, tc.want, Params{Bits: tc.bits, Restricted: tc.restricted}.idBits(), "%d bits restricted=%t", tc.bits, tc.restricted)
	}
}

// TestFundamentalSequence reads the codewords of table 3-1 back.
func TestFundamentalSequence(t *testing.T) {
	// 1, 01, 001, 0001 packed MSB first: 1 01 001 0001 = 1010 0100 01xx xxxx
	r := reader{b: []byte{0xa4, 0x40}}
	for want := uint64(0); want < 4; want++ {
		require.Equal(t, want, r.fs(64))
	}
	require.False(t, r.bad)
	// A run past the limit is corrupt, not a huge value.
	r = reader{b: []byte{0, 0, 0, 0}}
	r.fs(10)
	require.True(t, r.bad)
}

// TestSecondExtensionInverse holds the pair transform's inverse over the
// whole small range: every (a, b) maps to γ and back.
func TestSecondExtensionInverse(t *testing.T) {
	for a := uint64(0); a < 40; a++ {
		for b := uint64(0); b < 40; b++ {
			gamma := (a+b)*(a+b+1)/2 + b
			s := (isqrt(8*gamma+1) - 1) / 2
			for s*(s+1)/2 > gamma {
				s--
			}
			for (s+1)*(s+2)/2 <= gamma {
				s++
			}
			gotB := gamma - s*(s+1)/2
			require.Equal(t, b, gotB, "a=%d b=%d", a, b)
			require.Equal(t, a, s-gotB, "a=%d b=%d", a, b)
		}
	}
}

func TestIntegerSquareRoot(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		v := rapid.Uint64Range(0, 1<<62).Draw(rt, "v")
		r := isqrt(v)
		require.LessOrEqual(t, r*r, v)
		require.Greater(t, (r+1)*(r+1), v)
	})
}

// TestMapperInverse: mapping a prediction error and inverting it returns
// the sample, for every predictor and sample of a small unsigned and a
// small signed range (§4.4).
func TestMapperInverse(t *testing.T) {
	mapErr := func(x, pred, xmin, xmax int64) (delta uint64) {
		theta := min(pred-xmin, xmax-pred)
		e := x - pred
		switch {
		case e >= 0 && e <= theta:
			delta = uint64(2 * e)
		case e < 0 && -e <= theta:
			delta = uint64(2*(-e) - 1)
		default:
			if e > 0 {
				delta = uint64(theta + e)
			} else {
				delta = uint64(theta - e)
			}
		}
		return
	}
	for _, signed := range []bool{false, true} {
		d := decoder{p: Params{Bits: 4, Signed: signed}, sampleMax: 15}
		xmin, xmax := int64(0), int64(15)
		if signed {
			xmin, xmax = -8, 7
		}
		for pred := xmin; pred <= xmax; pred++ {
			for x := xmin; x <= xmax; x++ {
				predU := uint64(pred)
				if pred < 0 {
					predU = uint64(pred + 16)
				}
				got, err := d.unmap(mapErr(x, pred, xmin, xmax), predU)
				require.NoError(t, err)
				want := uint64(x)
				if x < 0 {
					want = uint64(x + 16)
				}
				require.Equal(t, want, got, "signed=%t pred=%d x=%d", signed, pred, x)
			}
		}
	}
}

// TestGarbageNeverPanics: random bytes under random parameters either
// decode or fail with ErrCorrupt.
func TestGarbageNeverPanics(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := Params{
			Bits:              uint8(rapid.IntRange(1, 32).Draw(rt, "bits")),
			BlockSize:         uint8(rapid.SampledFrom([]int{8, 16, 32, 64}).Draw(rt, "block")),
			ReferenceInterval: uint16(rapid.IntRange(1, 200).Draw(rt, "rsi")),
			Preprocess:        rapid.Bool().Draw(rt, "pre"),
			Signed:            rapid.Bool().Draw(rt, "signed"),
			PadInterval:       rapid.Bool().Draw(rt, "pad"),
		}
		if p.Bits <= 4 {
			p.Restricted = rapid.Bool().Draw(rt, "restricted")
		}
		data := rapid.SliceOfN(rapid.Byte(), 0, 512).Draw(rt, "data")
		count := rapid.IntRange(0, 2000).Draw(rt, "count")
		_, err := Decode(nil, data, count, p)
		if err != nil && !errors.Is(err, ErrCorrupt) {
			rt.Fatalf("unexpected error class: %v", err)
		}
	})
}
