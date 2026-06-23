package play

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBandColorByName_Known(t *testing.T) {
	tests := []struct {
		name string
		want styletokens.RGBA8
	}{
		{"neutral.bg.faint", styletokens.NeutralBgFaint},
		{"accent.subtle", styletokens.AccentSubtle},
		{"warning.default", styletokens.WarningDefault},
		{"error.strong", styletokens.ErrorStrong},
		{"success.subtle", styletokens.SuccessSubtle},
		{"info.default", styletokens.InfoDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := bandColorByName(tt.name)
			require.True(t, ok, "expected hit for %q", tt.name)
			want := tt.want
			want.A = bandAlpha
			assert.Equal(t, want.AsHex(), got, "alpha-overridden RGBA8 mismatch")
		})
	}
}

func TestBandColorByName_Unknown(t *testing.T) {
	for _, name := range []string{"", "rainbow", "Accent.Subtle.Extra", "tw.green-500"} {
		_, ok := bandColorByName(name)
		assert.False(t, ok, "expected miss for %q", name)
	}
}

func TestBandColorByName_NormalisesWhitespaceAndCase(t *testing.T) {
	a, ok1 := bandColorByName("  Warning.Default  ")
	b, ok2 := bandColorByName("warning.default")
	require.True(t, ok1)
	require.True(t, ok2)
	assert.Equal(t, b, a, "case + trim should be normalised")
}

func TestBandColorByName_AlphaApplied(t *testing.T) {
	packed, ok := bandColorByName("info.default")
	require.True(t, ok)
	gotAlpha := uint8(packed & 0xff)
	assert.Equal(t, bandAlpha, gotAlpha)
}

func TestSubstituteBandsRange_LiteralForm(t *testing.T) {
	// 2026-05-23T17:13:42.000Z → 1779556422000 ms epoch
	// 2026-05-23T17:14:42.000Z → 1779556482000 ms epoch
	const minMS int64 = 1779556422000
	const maxMS int64 = 1779556482000
	const sql = "SELECT * FROM bands WHERE t BETWEEN _time_data_min AND _time_data_max"

	out := substituteBandsRange(sql, minMS, maxMS)
	assert.Contains(t, out, "toDateTime64('2026-05-23 17:13:42.000', 3, 'UTC')")
	assert.Contains(t, out, "toDateTime64('2026-05-23 17:14:42.000', 3, 'UTC')")
	assert.False(t, strings.Contains(out, "_time_data_min"),
		"placeholder _time_data_min must be replaced")
	assert.False(t, strings.Contains(out, "_time_data_max"),
		"placeholder _time_data_max must be replaced")
}

func TestSubstituteBandsRange_NoPlaceholdersIsNoOp(t *testing.T) {
	const sql = "SELECT 1 AS _tl_band_from, 2 AS _tl_band_to"
	out := substituteBandsRange(sql, 0, 0)
	assert.Equal(t, sql, out)
}

func TestExtentOfEvents_Points(t *testing.T) {
	pts := []*layout.PointEvent{
		{TMS: 10}, {TMS: 30}, {TMS: 20},
	}
	mn, mx, ok := extentOfEvents(nil, pts, nil)
	require.True(t, ok)
	assert.Equal(t, int64(10), mn)
	assert.Equal(t, int64(30), mx)
}

func TestExtentOfEvents_IntervalsCoverToMS(t *testing.T) {
	ivs := []*layout.IntervalEvent{
		{FromMS: 100, ToMS: 200},
		{FromMS: 150, ToMS: 350},
	}
	mn, mx, ok := extentOfEvents(ivs, nil, nil)
	require.True(t, ok)
	assert.Equal(t, int64(100), mn)
	assert.Equal(t, int64(350), mx)
}

func TestExtentOfEvents_Annotations(t *testing.T) {
	anns := []*layout.Annotation{
		{TMS: 50}, {TMS: 60}, {TMS: 40},
	}
	mn, mx, ok := extentOfEvents(nil, nil, anns)
	require.True(t, ok)
	assert.Equal(t, int64(40), mn)
	assert.Equal(t, int64(60), mx)
}

func TestExtentOfEvents_EmptyReturnsFalse(t *testing.T) {
	_, _, ok := extentOfEvents(nil, nil, nil)
	assert.False(t, ok)
}

func TestExtentOfEvents_SkipsNils(t *testing.T) {
	pts := []*layout.PointEvent{nil, {TMS: 100}, nil}
	mn, mx, ok := extentOfEvents(nil, pts, nil)
	require.True(t, ok)
	assert.Equal(t, int64(100), mn)
	assert.Equal(t, int64(100), mx)
}

func TestBandsCacheStore_LRUEviction(t *testing.T) {
	inst := &TimelineDriver{}
	for i := range bandsCacheSize + 3 {
		key := bandsCacheKey{MinMS: int64(i), MaxMS: int64(i + 1), SQL: "x"}
		inst.bandsCacheStoreLocked(key, []layout.BackgroundBand{{FromMS: int64(i)}})
	}
	assert.Equal(t, bandsCacheSize, len(inst.bandsCache),
		"cache should be bounded to bandsCacheSize")
	// Most-recent insertion is at index 0.
	assert.Equal(t, int64(bandsCacheSize+2), inst.bandsCache[0].Key.MinMS)
}

func TestBandsCacheLookup_MoveToFront(t *testing.T) {
	inst := &TimelineDriver{}
	keys := []bandsCacheKey{
		{MinMS: 1, MaxMS: 2, SQL: "a"},
		{MinMS: 3, MaxMS: 4, SQL: "b"},
		{MinMS: 5, MaxMS: 6, SQL: "c"},
	}
	for _, k := range keys {
		inst.bandsCacheStoreLocked(k, nil)
	}
	// Most-recent first: c, b, a.
	require.Equal(t, keys[2], inst.bandsCache[0].Key)

	// Looking up `a` should move it to front.
	_, ok := inst.bandsCacheLookupLocked(keys[0])
	require.True(t, ok)
	assert.Equal(t, keys[0], inst.bandsCache[0].Key)
}

func TestBandsCacheLookup_Miss(t *testing.T) {
	inst := &TimelineDriver{}
	inst.bandsCacheStoreLocked(bandsCacheKey{MinMS: 1, MaxMS: 2, SQL: "a"}, nil)
	_, ok := inst.bandsCacheLookupLocked(bandsCacheKey{MinMS: 99, MaxMS: 100, SQL: "z"})
	assert.False(t, ok)
}
