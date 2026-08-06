package colormap

import (
	"image/color"
	"math"
	"testing"
)

func TestMapLinearEndpoints(t *testing.T) {
	cfg := NewConfig(Viridis8, 0.0, 1.0)
	src := []float32{0.0, 1.0}
	dst := make([]uint32, 2)
	stats := cfg.Map(src, dst)
	if stats != (ColumnStats{}) {
		t.Fatalf("endpoints should not register as bad/under/over, got %+v", stats)
	}
	if dst[0] != Viridis8[0] {
		t.Fatalf("t=0 expected %08x, got %08x", Viridis8[0], dst[0])
	}
	if dst[1] != Viridis8[len(Viridis8)-1] {
		t.Fatalf("t=1 expected %08x, got %08x", Viridis8[len(Viridis8)-1], dst[1])
	}
}

func TestMapLinearInterpolatedMidpoint(t *testing.T) {
	// Use a two-stop palette so the midpoint is an exact 50/50 blend.
	pal := []uint32{0x00000000, 0xffffffff}
	cfg := NewConfig(pal, 0.0, 1.0)
	cfg.Palette = pal
	src := []float32{0.5}
	dst := make([]uint32, 1)
	cfg.Map(src, dst)
	// 0x00 + 0xff midpoint rounds to 0x80 per lerpRGBA's +0.5 rounding.
	want := uint32(0x80808080)
	if dst[0] != want {
		t.Fatalf("t=0.5 expected %08x, got %08x", want, dst[0])
	}
}

func TestMapUnderOverBad(t *testing.T) {
	cfg := NewConfig(Viridis8, 0.0, 1.0)
	cfg.BadColor = color.NRGBA{R: 0x11, A: 0xff}
	cfg.UnderflowColor = color.NRGBA{G: 0x22, A: 0xff}
	cfg.OverflowColor = color.NRGBA{B: 0x33, A: 0xff}

	src := []float32{
		float32(math.NaN()),
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
		-0.5,
		1.5,
		0.5,
	}
	dst := make([]uint32, len(src))
	stats := cfg.Map(src, dst)

	if stats.BadSamples != 3 {
		t.Errorf("expected 3 bad samples, got %d", stats.BadSamples)
	}
	if stats.Underflow != 1 {
		t.Errorf("expected 1 underflow, got %d", stats.Underflow)
	}
	if stats.Overflow != 1 {
		t.Errorf("expected 1 overflow, got %d", stats.Overflow)
	}

	badWant := nrgbaToRGBAu32(cfg.BadColor)
	underWant := nrgbaToRGBAu32(cfg.UnderflowColor)
	overWant := nrgbaToRGBAu32(cfg.OverflowColor)
	for i := 0; i < 3; i++ {
		if dst[i] != badWant {
			t.Errorf("src[%d] bad: expected %08x, got %08x", i, badWant, dst[i])
		}
	}
	if dst[3] != underWant {
		t.Errorf("underflow: expected %08x, got %08x", underWant, dst[3])
	}
	if dst[4] != overWant {
		t.Errorf("overflow: expected %08x, got %08x", overWant, dst[4])
	}
	// dst[5] should be a valid palette colour, not any substitution.
	if dst[5] == badWant || dst[5] == underWant || dst[5] == overWant {
		t.Errorf("in-range sample should not use substitution, got %08x", dst[5])
	}
}

func TestMapLogScale(t *testing.T) {
	cfg := NewConfig(Viridis8, 1.0, 1000.0)
	cfg.Scale = ScaleLogE
	src := []float32{1.0, 10.0, 100.0, 1000.0}
	dst := make([]uint32, len(src))
	stats := cfg.Map(src, dst)
	if stats != (ColumnStats{}) {
		t.Fatalf("unexpected stats %+v", stats)
	}
	// t values under log10: 0, 1/3, 2/3, 1. Endpoints must match palette ends.
	if dst[0] != Viridis8[0] {
		t.Errorf("log t=0 expected %08x, got %08x", Viridis8[0], dst[0])
	}
	if dst[3] != Viridis8[len(Viridis8)-1] {
		t.Errorf("log t=1 expected %08x, got %08x", Viridis8[len(Viridis8)-1], dst[3])
	}
}

func TestMapLogScaleNonPositive(t *testing.T) {
	cfg := NewConfig(Viridis8, 1.0, 1000.0)
	cfg.Scale = ScaleLogE
	cfg.UnderflowColor = color.NRGBA{R: 0x77, A: 0xff}
	src := []float32{0.0, -1.0, 0.5}
	dst := make([]uint32, len(src))
	stats := cfg.Map(src, dst)
	if stats.Underflow != 3 {
		t.Errorf("expected 3 underflow (0, -1, 0.5 < DataMin), got %d", stats.Underflow)
	}
	want := nrgbaToRGBAu32(cfg.UnderflowColor)
	for i, v := range dst {
		if v != want {
			t.Errorf("dst[%d]: expected %08x, got %08x", i, want, v)
		}
	}
}

func TestMapDbScale(t *testing.T) {
	cfg := NewConfig(Viridis8, -80.0, 0.0) // dB range
	cfg.Scale = ScaleDbE
	cfg.UnderflowColor = color.NRGBA{R: 0x77, A: 0xff}
	// 10*log10(1.0) = 0 dB (hits DataMax exactly → in-range, t=1)
	// 10*log10(1e-4) = -40 dB (mid-range)
	// 0 and -1 → non-positive → underflow
	// Boundary at -80 dB is fp-unstable (1e-8 rounds just below), so
	// test clearly-in-range and clearly-below values instead of DataMin.
	src := []float32{1.0, 1e-4, 0.0, -1.0}
	dst := make([]uint32, len(src))
	stats := cfg.Map(src, dst)
	if stats.Underflow != 2 {
		t.Errorf("expected 2 underflow (0 and -1), got %d", stats.Underflow)
	}
	if stats.Overflow != 0 {
		t.Errorf("expected 0 overflow, got %d", stats.Overflow)
	}
	if stats.BadSamples != 0 {
		t.Errorf("expected 0 bad, got %d", stats.BadSamples)
	}
	if dst[0] != Viridis8[len(Viridis8)-1] {
		t.Errorf("0 dB (t=1) expected %08x, got %08x", Viridis8[len(Viridis8)-1], dst[0])
	}
	underWant := nrgbaToRGBAu32(cfg.UnderflowColor)
	for _, i := range []int{2, 3} {
		if dst[i] != underWant {
			t.Errorf("dst[%d]: expected underflow %08x, got %08x", i, underWant, dst[i])
		}
	}
}

func TestColumnStatsAdd(t *testing.T) {
	a := ColumnStats{BadSamples: 1, Underflow: 2, Overflow: 3}
	b := ColumnStats{BadSamples: 10, Underflow: 20, Overflow: 30}
	a.Add(b)
	if a != (ColumnStats{BadSamples: 11, Underflow: 22, Overflow: 33}) {
		t.Fatalf("Add: got %+v", a)
	}
}

func TestNewConfigPanics(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"palette too short", func() { _ = NewConfig([]uint32{0xff0000ff}, 0, 1) }},
		{"min == max", func() { _ = NewConfig(Viridis8, 1, 1) }},
		{"min > max", func() { _ = NewConfig(Viridis8, 2, 1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic")
				}
			}()
			tc.fn()
		})
	}
}

func TestMapPanicsOnShortDst(t *testing.T) {
	cfg := NewConfig(Viridis8, 0, 1)
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic when dst shorter than src")
		}
	}()
	cfg.Map(make([]float32, 10), make([]uint32, 5))
}

func TestPaletteLerpClamps(t *testing.T) {
	pal := []uint32{0x11223344, 0x55667788}
	if got := paletteLerp(pal, -0.5); got != pal[0] {
		t.Errorf("t<0 should clamp to palette[0], got %08x", got)
	}
	if got := paletteLerp(pal, 1.5); got != pal[1] {
		t.Errorf("t>1 should clamp to palette[last], got %08x", got)
	}
	// A NaN position must land on a stop rather than reaching int(NaN) —
	// math.MinInt64 — and indexing out of range. Callers are meant to have
	// substituted before here; this is the guard that makes the helper safe
	// for the one that forgets.
	if got := paletteLerp(pal, math.NaN()); got != pal[0] {
		t.Errorf("NaN should land on palette[0], got %08x", got)
	}
}

// At is the SCALAR path, and until this test it did not share Map's
// substitution of a non-number: NaN flowed through Normalize into paletteLerp
// and panicked with index [-9223372036854775808]. A sparse heatmap — any grid
// with a cell nothing filled — reached it through implot's two heatmap routes.
func TestAtSubstitutesBadColorForNonNumbers(t *testing.T) {
	cfg := NewConfig(Viridis8, 0.0, 1.0)
	cfg.BadColor = color.NRGBA{R: 0x11, A: 0xff}
	want := nrgbaToRGBAu32(cfg.BadColor)

	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got := cfg.At(v); got != want {
			t.Errorf("At(%v) = %08x, want BadColor %08x", v, got, want)
		}
	}

	// The default BadColor is transparent, which is what makes an unfilled
	// heatmap cell read as the surface behind it rather than as a value.
	if got := NewConfig(Viridis8, 0.0, 1.0).At(math.NaN()); got != 0 {
		t.Errorf("default BadColor should be transparent, got %08x", got)
	}

	// Out-of-range still CLAMPS here rather than substituting, which is the
	// deliberate divergence from Map: At is also a gradient sampler, and the
	// substitution colours default to transparent.
	if got := cfg.At(-1); got != Viridis8[0] {
		t.Errorf("under-range should clamp to palette[0], got %08x", got)
	}
	if got := cfg.At(2); got != Viridis8[len(Viridis8)-1] {
		t.Errorf("over-range should clamp to palette[last], got %08x", got)
	}
}
