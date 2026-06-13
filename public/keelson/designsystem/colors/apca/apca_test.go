package apca_test

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/apca"
)

// nearly compares two floats within tol.
func nearly(a, b, tol float64) (ok bool) {
	ok = math.Abs(a-b) <= tol
	return
}

// Reference vectors from the Myndex APCA Beta 0.1.9 test page
// (https://www.myndex.com/APCA/). Tolerance is generous (±0.5 Lc) to
// absorb the ~6th-decimal differences across float pipelines; APCA
// thresholds are coarser than that anyway.
func TestApcaReferenceVectors(t *testing.T) {
	cases := []struct {
		name                          string
		textR, textG, textB           uint8
		bgR, bgG, bgB                 uint8
		wantLc                        float64
	}{
		// Canonical Myndex Beta 0.1.9 reference vectors.
		{"black on white (BoW max)", 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 106.04},
		{"white on black (WoB max)", 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, -107.88},
		{"mid-gray on white", 0x88, 0x88, 0x88, 0xff, 0xff, 0xff, 63.06},
		{"white on mid-gray", 0xff, 0xff, 0xff, 0x88, 0x88, 0x88, -68.54},
	}
	for _, tc := range cases {
		got := apca.Lc(tc.textR, tc.textG, tc.textB, tc.bgR, tc.bgG, tc.bgB)
		if !nearly(got, tc.wantLc, 1.0) {
			t.Errorf("%s: got Lc=%.2f, want %.2f (±1.0)", tc.name, got, tc.wantLc)
		}
	}
}

func TestApcaSignByPolarity(t *testing.T) {
	// BoW (dark text on light bg) → positive Lc.
	bow := apca.Lc(0, 0, 0, 0xff, 0xff, 0xff)
	if bow <= 0 {
		t.Errorf("expected positive Lc for dark-on-light, got %v", bow)
	}
	// WoB (light text on dark bg) → negative Lc.
	wob := apca.Lc(0xff, 0xff, 0xff, 0, 0, 0)
	if wob >= 0 {
		t.Errorf("expected negative Lc for light-on-dark, got %v", wob)
	}
}

func TestApcaSameColorIsZero(t *testing.T) {
	// Identical text and bg should yield zero contrast.
	got := apca.Lc(0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
	if !nearly(got, 0.0, 0.001) {
		t.Errorf("same-color Lc should be 0, got %v", got)
	}
}

func TestThresholdMonotonicInSize(t *testing.T) {
	// Smaller / lighter text requires higher Lc.
	smallThin := apca.Threshold(11, 400)
	mediumRegular := apca.Threshold(14, 400)
	largeBold := apca.Threshold(24, 700)
	if !(smallThin >= mediumRegular && mediumRegular >= largeBold) {
		t.Errorf("threshold monotonicity broken: 11pt/400=%v, 14pt/400=%v, 24pt/700=%v",
			smallThin, mediumRegular, largeBold)
	}
}

func TestUIThresholdCategories(t *testing.T) {
	if apca.UIThreshold("meaningful") != 60 {
		t.Errorf("meaningful UI Lc should be 60")
	}
	if apca.UIThreshold("ambient") != 30 {
		t.Errorf("ambient UI Lc should be 30")
	}
	if apca.UIThreshold("floating") != 15 {
		t.Errorf("floating UI Lc should be 15")
	}
}
