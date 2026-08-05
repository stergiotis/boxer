package layeredgraph

import "testing"

// WeightFontSize is pure and carries the same three judgements as
// view.WeightWidth (ADR-0167 §SD2/§SD3), so it is pinned the same way.

func modelWith(weights ...float64) GraphModel {
	m := GraphModel{}
	for i, w := range weights {
		m.Nodes = append(m.Nodes, Node{ID: string(rune('a' + i)), Weight: w})
	}
	return m
}

func TestWeightFontSizeSpansTheBounds(t *testing.T) {
	const minPt, maxPt = 10.0, 30.0
	h := WeightFontSize(modelWith(1, 100, 10_000), minPt, maxPt)

	got := make([]float64, 0, 3)
	for _, w := range []float64{1, 100, 10_000} {
		pt, ok := h(w)
		if !ok {
			t.Fatalf("weight %v declined", w)
		}
		got = append(got, pt)
	}
	if got[2] != maxPt {
		t.Errorf("heaviest node got %v, want %v", got[2], maxPt)
	}
	for i, pt := range got {
		if pt < minPt || pt > maxPt {
			t.Errorf("size %d = %v, outside [%v,%v]", i, pt, minPt, maxPt)
		}
	}
	if !(got[0] < got[1] && got[1] < got[2]) {
		t.Errorf("not monotone: %v", got)
	}
	// Square root, not linear. A weight 1/100th of the maximum is where the
	// two curves differ most visibly: sqrt lifts it to a tenth of the range,
	// linear leaves it at a hundredth, all but on the floor.
	linear := minPt + (maxPt-minPt)*(100.0/10_000.0)
	if got[1] <= linear {
		t.Errorf("mid weight %v is at or below the linear mapping's %v — the curve is not a square root", got[1], linear)
	}
}

// 0 is unknown rather than none, so such a node keeps the layout-wide size.
func TestWeightFontSizeDeclinesUnknown(t *testing.T) {
	h := WeightFontSize(modelWith(0, 500), 0, 0)
	if _, ok := h(0); ok {
		t.Error("a zero weight was given a size")
	}
	if _, ok := h(-1); ok {
		t.Error("a negative weight was given a size")
	}
	if _, ok := h(500); !ok {
		t.Error("a positive weight was declined")
	}
}

// ADR-0167 C1: with nothing to normalise against, the mapping declines every
// node, so an unweighted graph lays out exactly as it did before.
func TestWeightFontSizeDeclinesEverythingWithoutWeights(t *testing.T) {
	for _, m := range []GraphModel{{}, modelWith(), modelWith(0, 0, 0)} {
		h := WeightFontSize(m, 0, 0)
		for _, w := range []float64{0, 1, 1e9} {
			if _, ok := h(w); ok {
				t.Fatalf("a weightless model gave weight %v a size", w)
			}
		}
	}
}

// The floor is the Graphviz default, so the lightest carrying node is the size
// an ordinary node would have been — magnitude only ever grows a node.
func TestWeightFontSizeNeverShrinksBelowTheDefault(t *testing.T) {
	h := WeightFontSize(modelWith(1, 1e9), 0, 0)
	pt, ok := h(1)
	if !ok {
		t.Fatal("declined")
	}
	if pt < DefaultWeightMinPt {
		t.Errorf("lightest node got %v, below the default %v", pt, DefaultWeightMinPt)
	}
}

func TestWeightFontSizeNormalisesBounds(t *testing.T) {
	h := WeightFontSize(modelWith(10), 30, 12)
	pt, ok := h(10)
	if !ok {
		t.Fatal("declined")
	}
	if pt != 30 {
		t.Errorf("got %v, want the min after the swap collapsed the range", pt)
	}
}
