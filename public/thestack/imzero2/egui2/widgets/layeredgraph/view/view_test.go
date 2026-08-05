package view

import (
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// The renderer itself paints through the bindings and is verified by the
// gallery demo's tour capture. WeightWidth is pure, and it is where the
// magnitude overlay's judgement calls live (ADR-0167 §SD2), so it is pinned
// here.

func layWith(weights ...float64) *layeredgraph.Layout {
	lay := &layeredgraph.Layout{}
	for _, w := range weights {
		lay.Edges = append(lay.Edges, layeredgraph.EdgeLayout{Weight: w})
	}
	return lay
}

func TestWeightWidthSpansTheBounds(t *testing.T) {
	const minW, maxW = 2.0, 8.0
	h := WeightWidth(layWith(1, 100, 10_000), minW, maxW)

	got := make([]float32, 0, 3)
	for _, w := range []float64{1, 100, 10_000} {
		width, ok := h("", "", w)
		if !ok {
			t.Fatalf("weight %v declined", w)
		}
		got = append(got, width)
	}
	// The heaviest edge takes the full width; every carrying edge is at least
	// the minimum; and the curve is monotone in between.
	if got[2] != maxW {
		t.Errorf("heaviest edge got %v, want %v", got[2], maxW)
	}
	for i, w := range got {
		if w < minW || w > maxW {
			t.Errorf("width %d = %v, outside [%v,%v]", i, w, minW, maxW)
		}
	}
	if !(got[0] < got[1] && got[1] < got[2]) {
		t.Errorf("not monotone: %v", got)
	}
	// Square root, not linear: a weight 1/100th of the maximum must land well
	// above the floor, which is the whole reason for the curve. Linear would
	// put it at minW + 0.01*(maxW-minW) = 2.06.
	if got[1] <= 2.5 {
		t.Errorf("mid weight %v is near the floor — the curve reads as linear", got[1])
	}
}

// The model documents 0 as unknown rather than none, so such an edge keeps the
// default width instead of becoming a hairline that asserts "no flow".
func TestWeightWidthDeclinesUnknown(t *testing.T) {
	h := WeightWidth(layWith(0, 500), 0, 0)
	if _, ok := h("", "", 0); ok {
		t.Error("a zero weight was given a width")
	}
	if _, ok := h("", "", -1); ok {
		t.Error("a negative weight was given a width")
	}
	if _, ok := h("", "", 500); !ok {
		t.Error("a positive weight was declined")
	}
}

// ADR-0167 C1: a graph carrying no weights renders exactly as it did before.
// The hook declines universally, so the caller never overrides Style.EdgeStrokeW
// — the property, not a golden image.
func TestWeightWidthDeclinesEverythingWithoutWeights(t *testing.T) {
	for _, lay := range []*layeredgraph.Layout{layWith(), layWith(0, 0, 0), nil} {
		h := WeightWidth(lay, 0, 0)
		for _, w := range []float64{0, 1, 1e9} {
			if _, ok := h("a", "b", w); ok {
				t.Fatalf("a weightless layout gave weight %v a width", w)
			}
		}
	}
}

// Bounds are normalised rather than trusted: a caller passing them backwards
// gets a flat mapping, not an inverted one.
func TestWeightWidthNormalisesBounds(t *testing.T) {
	h := WeightWidth(layWith(10), 9, 3)
	w, ok := h("", "", 10)
	if !ok {
		t.Fatal("declined")
	}
	if w != 9 {
		t.Errorf("got %v, want the min after the swap collapsed the range", w)
	}
}
