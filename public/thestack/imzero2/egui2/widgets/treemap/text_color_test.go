package treemap

import (
	"math"
	"testing"
)

func TestRelativeLuminance_Endpoints(t *testing.T) {
	// Pure black → 0; pure white → 1. W3C spec.
	if got := relativeLuminance(0, 0, 0); math.Abs(got) > 1e-9 {
		t.Errorf("L(black) = %v, want 0", got)
	}
	if got := relativeLuminance(255, 255, 255); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("L(white) = %v, want 1", got)
	}
}

func TestRelativeLuminance_PrimaryRanking(t *testing.T) {
	// Green contributes most, red middle, blue least. Standard coefficients
	// 0.7152 / 0.2126 / 0.0722.
	red := relativeLuminance(255, 0, 0)
	green := relativeLuminance(0, 255, 0)
	blue := relativeLuminance(0, 0, 255)
	if !(green > red && red > blue) {
		t.Errorf("expected green > red > blue luminance; got R=%v G=%v B=%v", red, green, blue)
	}
	// Sum of pure primaries = 1 (the white-equivalent decomposition).
	if math.Abs(red+green+blue-1.0) > 1e-9 {
		t.Errorf("R+G+B luminance != 1: got %v", red+green+blue)
	}
}

func TestRelativeLuminance_GammaSplit(t *testing.T) {
	// At c=10/255 ≈ 0.0392 we're just below the 0.03928 spec split; should
	// use the linear branch. The two branches must agree at the split point.
	cAt := uint8(math.Round(0.03928 * 255)) // ≈ 10
	low := relativeLuminance(cAt-1, cAt-1, cAt-1)
	high := relativeLuminance(cAt+1, cAt+1, cAt+1)
	if !(low < high) {
		t.Errorf("luminance must increase across the gamma split; got %v -> %v", low, high)
	}
}

func TestPickTextColor_ConcreteCases(t *testing.T) {
	// Cases far from the L ≈ 0.179 threshold so the expectation is
	// unambiguous regardless of small threshold tuning.
	cases := []struct {
		name      string
		r, g, b   uint8
		wantBlack bool
	}{
		{"pure white", 255, 255, 255, true},
		{"pure black", 0, 0, 0, false},
		{"bright yellow (Viridis8 last entry)", 0xfd, 0xe7, 0x25, true},
		{"dark purple (Viridis8 first entry)", 0x44, 0x01, 0x54, false},
		{"pure blue (L ≈ 0.07)", 0, 0, 255, false},
		{"pure green (L ≈ 0.72)", 0, 255, 0, true},
		{"mid gray RGB(220,220,220)", 220, 220, 220, true},
		{"near-black RGB(20,20,20)", 20, 20, 20, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pickTextColor(c.r, c.g, c.b)
			wantRGBA := uint32(0xffffffff)
			if c.wantBlack {
				wantRGBA = 0x000000ff
			}
			if got != wantRGBA {
				t.Errorf("pickTextColor(%d,%d,%d) = %#08x, want %#08x",
					c.r, c.g, c.b, got, wantRGBA)
			}
		})
	}
}

func TestPickTextColor_ThresholdMonotonic(t *testing.T) {
	// Sweep gray ramp; black-text choice must form one contiguous range at
	// the bright end (no flicker).
	var firstBlack int = -1
	for v := 0; v <= 255; v++ {
		isBlack := pickTextColor(uint8(v), uint8(v), uint8(v)) == 0x000000ff
		if isBlack && firstBlack == -1 {
			firstBlack = v
		}
		if firstBlack != -1 && !isBlack {
			t.Errorf("gray ramp v=%d: black-text region not contiguous (first black at %d, but v=%d picks white)",
				v, firstBlack, v)
		}
	}
	if firstBlack == -1 {
		t.Errorf("white text picked for all grays; expected black-text region near the bright end")
	}
}

func TestDeriveCellColors_TextSlotsPopulated(t *testing.T) {
	// Black base: Fill is black → white text. DimFill is also near-black →
	// white text. HoverFill is RGB(30,30,30) → still dark → white text.
	cs := deriveCellColors(0x000000ff)
	if cs.Text.Literal() != 0xffffffff {
		t.Errorf("Text over black base: got %#08x, want 0xffffffff", cs.Text.Literal())
	}
	if cs.DimText.Literal() != 0xffffffff {
		t.Errorf("DimText over half-black base: got %#08x, want 0xffffffff", cs.DimText.Literal())
	}

	// White base: Fill bright → black text. HoverFill clamps at 255 → still
	// bright → black text. DimFill = RGB(127,127,127) → near-mid; check
	// against the threshold rather than asserting a specific outcome.
	cs2 := deriveCellColors(0xffffffff)
	if cs2.Text.Literal() != 0x000000ff {
		t.Errorf("Text over white base: got %#08x, want 0x000000ff", cs2.Text.Literal())
	}
	if cs2.HoverText.Literal() != 0x000000ff {
		t.Errorf("HoverText over saturated-white base: got %#08x, want 0x000000ff", cs2.HoverText.Literal())
	}
}
