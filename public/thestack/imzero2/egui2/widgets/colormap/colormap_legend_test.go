package colormap

import (
	"math"
	"testing"
)

func TestConfig_RangeAndIsLog(t *testing.T) {
	cfg := NewConfig([]uint32{0x000000ff, 0xffffffff}, 5, 50)
	if mn, mx := cfg.Range(); mn != 5 || mx != 50 {
		t.Errorf("Range() = (%v,%v), want (5,50)", mn, mx)
	}
	if cfg.IsLog() {
		t.Errorf("IsLog() = true for a linear config, want false")
	}
	cfg.Scale = ScaleLogE
	if !cfg.IsLog() {
		t.Errorf("IsLog() = false after Scale=Log, want true")
	}
	cfg.Scale = ScaleDbE
	if cfg.IsLog() {
		t.Errorf("IsLog() = true for Db (linear in decibels), want false")
	}
}

func TestConfig_NormalizeLinear(t *testing.T) {
	cfg := NewConfig([]uint32{0x000000ff, 0xffffffff}, 0, 100)
	for _, tc := range []struct{ v, want float64 }{
		{0, 0}, {50, 0.5}, {100, 1}, {-10, 0}, {200, 1},
	} {
		if got := cfg.Normalize(tc.v); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("Normalize(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestConfig_NormalizeLog(t *testing.T) {
	cfg := NewConfig([]uint32{0x000000ff, 0xffffffff}, 1, 1000)
	cfg.Scale = ScaleLogE
	// log10(1)=0 → 0; log10(10)=1 over a span of log10(1000)=3 → 1/3; log10(1000)=3 → 1.
	// Non-positive values clamp to 0.
	for _, tc := range []struct{ v, want float64 }{
		{1, 0}, {10, 1.0 / 3.0}, {1000, 1}, {0, 0}, {-5, 0}, {1e9, 1},
	} {
		if got := cfg.Normalize(tc.v); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("Normalize(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestConfig_At(t *testing.T) {
	cfg := NewConfig([]uint32{0x000000ff, 0xffffffff}, 0, 100) // black → white
	if got := cfg.At(0); got != 0x000000ff {
		t.Errorf("At(0) = %#08x, want 0x000000ff", got)
	}
	if got := cfg.At(100); got != 0xffffffff {
		t.Errorf("At(100) = %#08x, want 0xffffffff", got)
	}
	// The legend interpolates between stops (smooth), unlike treemap's quantized cells.
	if got := cfg.At(50); got != 0x808080ff {
		t.Errorf("At(50) = %#08x, want 0x808080ff (interpolated mid-grey)", got)
	}
}

func TestConfig_IndexAt(t *testing.T) {
	cfg := NewConfig([]uint32{0x000000ff, 0xffffffff}, 0, 100)
	for _, tc := range []struct {
		v    float64
		n    int
		want int
	}{
		{0, 4, 0}, {100, 4, 3}, {50, 4, 1}, {-10, 4, 0}, {200, 4, 3}, {50, 1, 0},
	} {
		if got := cfg.IndexAt(tc.v, tc.n); got != tc.want {
			t.Errorf("IndexAt(%v, %d) = %d, want %d", tc.v, tc.n, got, tc.want)
		}
	}
}
