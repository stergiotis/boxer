package implot

import (
	"math"
	"testing"
)

func TestRelativeLuminanceAnchors(t *testing.T) {
	cases := []struct {
		name string
		rgba uint32
		want float64
	}{
		{"black", 0x000000ff, 0},
		{"white", 0xffffffff, 1},
		// The Rec.709 primaries, which are where the weights come from.
		{"red", 0xff0000ff, 0.2126},
		{"green", 0x00ff00ff, 0.7152},
		{"blue", 0x0000ffff, 0.0722},
		// Mid grey is well under half: that is the transfer function, and the
		// reason a Rec.601-on-bytes estimate reads brighter than this one.
		{"mid grey", 0x808080ff, 0.21586},
		// Inside the linear segment, below the 0.04045 knee.
		{"near black", 0x080808ff, 0.00242},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RelativeLuminance(tc.rgba); math.Abs(got-tc.want) > 1e-4 {
				t.Errorf("RelativeLuminance(%08x) = %v, want %v", tc.rgba, got, tc.want)
			}
		})
	}
	// Alpha is not part of it: a fill composites against something this
	// function cannot see.
	if RelativeLuminance(0x336699ff) != RelativeLuminance(0x33669900) {
		t.Error("alpha changed the luminance")
	}
	// Monotone in each channel, which is what makes it usable as a threshold.
	prev := -1.0
	for v := 0; v <= 255; v++ {
		g := RelativeLuminance(uint32(v)<<24 | uint32(v)<<16 | uint32(v)<<8 | 0xff)
		if g < prev {
			t.Fatalf("luminance fell at grey %d: %v after %v", v, g, prev)
		}
		prev = g
	}
}

func TestContrastRatioAnchors(t *testing.T) {
	if got := ContrastRatio(0x000000ff, 0xffffffff); math.Abs(got-21) > 1e-9 {
		t.Errorf("black on white = %v, want 21", got)
	}
	if got := ContrastRatio(0x336699ff, 0x336699ff); got != 1 {
		t.Errorf("a colour against itself = %v, want 1", got)
	}
	// Symmetric: the ratio does not care which way round it is asked.
	a, b := uint32(0xe69f00ff), uint32(0x101214ff)
	if ContrastRatio(a, b) != ContrastRatio(b, a) {
		t.Error("ContrastRatio is not symmetric")
	}
	// The identity the ink pickers rely on: at the luminance where two inks
	// contrast equally, they really do.
	ld, ll := RelativeLuminance(0x101214ff), RelativeLuminance(0xe5e8ebff)
	bal := math.Sqrt((ld+0.05)*(ll+0.05)) - 0.05
	toDark := (bal + 0.05) / (ld + 0.05)
	toLite := (ll + 0.05) / (bal + 0.05)
	if math.Abs(toDark-toLite) > 1e-9 {
		t.Errorf("the equal-contrast point is not equal: %v vs %v", toDark, toLite)
	}
}

// contrastText keeps upstream's Rec.601 rule rather than the accurate one, so
// a ported chart keeps upstream's ink. That is a deliberate divergence and
// this is where it is recorded — if contrastText is ever moved onto
// RelativeLuminance, this test is the thing that should have to change.
func TestContrastTextDivergesFromRelativeLuminance(t *testing.T) {
	// A fill the two rules disagree about: Rec.601 on the bytes reads it as
	// light (167 of 255) while its true luminance is 0.222, well under the
	// 0.262 that byte threshold corresponds to.
	const fill = 0xc07881ff
	rec601 := 0.299*float64(fill>>24&0xff) + 0.587*float64(fill>>16&0xff) + 0.114*float64(fill>>8&0xff)
	if rec601 <= 140 {
		t.Fatalf("the fixture stopped being a disagreement: rec601 = %v", rec601)
	}
	if l := RelativeLuminance(fill); l > 0.262251 {
		t.Fatalf("the fixture stopped being a disagreement: luminance = %v", l)
	}
	if got := contrastText(fill); got != colContrastDark {
		t.Errorf("contrastText(%08x) = %#x, want the upstream rule's dark ink %#x", fill, got, colContrastDark)
	}
}
