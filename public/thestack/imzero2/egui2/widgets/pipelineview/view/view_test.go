package view

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pipelineview"
)

// The renderer itself paints through the bindings and is verified by the
// gallery demo's tour capture. VolumeWidth is pure, and it is where the
// overlay's judgement calls live, so it is pinned here.

func layWith(volumes ...float64) *pipelineview.Layout {
	lay := &pipelineview.Layout{}
	for _, v := range volumes {
		lay.Edges = append(lay.Edges, pipelineview.EdgeLayout{Volume: v})
	}
	return lay
}

func TestVolumeWidthSpansTheBounds(t *testing.T) {
	const minW, maxW = 2.0, 8.0
	h := VolumeWidth(layWith(1, 100, 10_000), minW, maxW)
	got := make([]float32, 0, 3)
	for _, v := range []float64{1, 100, 10_000} {
		w, ok := h("", "", v)
		if !ok {
			t.Fatalf("volume %v declined", v)
		}
		got = append(got, w)
	}
	// The largest volume lands exactly on maxW, and widths increase with
	// volume — the ordering claim the overlay actually makes.
	if got[2] != maxW {
		t.Errorf("largest volume gave %v, want maxW %v", got[2], float32(maxW))
	}
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Errorf("width %d (%v) not greater than width %d (%v)", i, got[i], i-1, got[i-1])
		}
	}
	if got[0] < minW {
		t.Errorf("smallest volume gave %v, below minW %v", got[0], float32(minW))
	}
	// Square root, not linear: a hundredth of the maximum is a tenth of the
	// span, which is the whole point of the curve.
	wantMid := float32(minW) + float32(maxW-minW)*float32(math.Sqrt(0.01))
	if diff := got[1] - wantMid; diff > 1e-5 || diff < -1e-5 {
		t.Errorf("mid volume gave %v, want %v (sqrt curve)", got[1], wantMid)
	}
}

// TestVolumeWidthDeclinesUnknown pins the distinction the model draws: 0 is
// "unknown", so such an edge keeps the default width rather than becoming a
// hairline that asserts "no flow".
func TestVolumeWidthDeclinesUnknown(t *testing.T) {
	h := VolumeWidth(layWith(0, 500), 0, 0)
	if _, ok := h("", "", 0); ok {
		t.Error("a zero volume was given a width")
	}
	if _, ok := h("", "", -1); ok {
		t.Error("a negative volume was given a width")
	}
	if _, ok := h("", "", 500); !ok {
		t.Error("a positive volume was declined")
	}
}

// TestVolumeWidthNoVolumesIsInert is what keeps existing consumers (the play
// Passes tab, the tour capture) pixel-identical: with nothing to show, the
// hook declines everything.
func TestVolumeWidthNoVolumesIsInert(t *testing.T) {
	for _, lay := range []*pipelineview.Layout{layWith(), layWith(0, 0, 0), nil} {
		h := VolumeWidth(lay, 0, 0)
		if _, ok := h("", "", 42); ok {
			t.Error("a hook built from a volume-free layout returned a width")
		}
	}
}

func TestVolumeWidthBoundsDefaultAndClamp(t *testing.T) {
	h := VolumeWidth(layWith(10), 0, 0)
	w, ok := h("", "", 10)
	if !ok || w != DefaultVolumeMaxW {
		t.Errorf("zero bounds gave (%v, %v), want the default max %v", w, ok, float32(DefaultVolumeMaxW))
	}
	// An inverted range must not produce a width below the floor.
	h = VolumeWidth(layWith(10), 6, 2)
	if w, _ := h("", "", 10); w != 6 {
		t.Errorf("inverted bounds gave %v, want the floor 6", w)
	}
	// Volumes above the observed maximum clamp rather than exceeding maxW.
	h = VolumeWidth(layWith(10), 1, 5)
	if w, _ := h("", "", 1e9); w != 5 {
		t.Errorf("over-max volume gave %v, want maxW 5", w)
	}
}
