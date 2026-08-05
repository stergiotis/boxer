package coveragebus

import (
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stretchr/testify/require"
)

func TestCBORCodec_RoundTrip(t *testing.T) {
	bm := roaring.BitmapOf(1, 99, 70000)
	orig := &covsnap.Update{
		MetaHash:        [16]byte{1, 2, 3},
		Seq:             7,
		SampledAtUnixMs: 1_700_000_000_123,
		Full:            true,
		Units:           bm,
		Status: covsnap.RunStatus{
			CoveredUnits: 3, TotalUnits: 9,
			CoveredStmts: 4, TotalStmts: 13,
			CoveredFuncs: 2, TotalFuncs: 3,
		},
		Pkgs:  []covsnap.PkgSample{{PkgIdx: 0, CoveredUnits: 2, CoveredStmts: 3, CoveredFuncs: 1}},
		Funcs: []covsnap.FuncSample{{PkgIdx: 0, FuncIdx: 1, CoveredUnits: 2}},
	}
	codec := NewCBORCodec()
	payload, err := codec.Encode(orig)
	require.NoError(t, err)
	got, err := codec.Decode(payload)
	require.NoError(t, err)
	require.Equal(t, orig.MetaHash, got.MetaHash)
	require.Equal(t, orig.Seq, got.Seq)
	require.Equal(t, orig.SampledAtUnixMs, got.SampledAtUnixMs)
	require.Equal(t, orig.Full, got.Full)
	require.Equal(t, orig.Status, got.Status)
	require.Equal(t, orig.Pkgs, got.Pkgs)
	require.Equal(t, orig.Funcs, got.Funcs)
	require.True(t, got.Units.Equals(bm), "covered set must round-trip")
}

// A heartbeat tick has an empty (or absent) covered set; the decode side
// always hands the handler a non-nil bitmap.
func TestCBORCodec_EmptyAndNilUnits(t *testing.T) {
	codec := NewCBORCodec()
	for _, units := range []*roaring.Bitmap{nil, roaring.New()} {
		payload, err := codec.Encode(&covsnap.Update{Seq: 1, Units: units})
		require.NoError(t, err)
		got, err := codec.Decode(payload)
		require.NoError(t, err)
		require.NotNil(t, got.Units)
		require.True(t, got.Units.IsEmpty())
	}
}

func TestParseInterval(t *testing.T) {
	d, on := ParseInterval("5s")
	require.True(t, on)
	require.EqualValues(t, 5_000_000_000, d)
	d, on = ParseInterval("2m")
	require.True(t, on)
	require.EqualValues(t, 120_000_000_000, d)
	_, on = ParseInterval("0")
	require.False(t, on)
	_, on = ParseInterval("-1s")
	require.False(t, on)
	// A misconfigured knob must not silently switch the lane off.
	d, on = ParseInterval("garbage")
	require.True(t, on)
	require.Equal(t, DefaultInterval, d)
}
