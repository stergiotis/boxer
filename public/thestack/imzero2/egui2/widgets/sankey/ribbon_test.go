package sankey

import (
	"math"
	"testing"
)

func TestSampleEndpointsAreExact(t *testing.T) {
	lay := mustCompute(t, testEnergy(), Options{})
	var r Ribbon
	for i := range lay.Links {
		l := lay.Links[i]
		l.Sample(DefaultSamples, &r)
		n := len(r.Xs)
		// A drift here would show as a hairline gap where a ribbon meets its
		// node bar, so it is pinned exactly rather than within eps.
		if r.Xs[0] != l.SX || r.Top[0] != l.SY1 || r.Bot[0] != l.SY0 {
			t.Errorf("link %d source end (%v,%v,%v), want (%v,%v,%v)",
				i, r.Xs[0], r.Bot[0], r.Top[0], l.SX, l.SY0, l.SY1)
		}
		if r.Xs[n-1] != l.TX || r.Top[n-1] != l.TY1 || r.Bot[n-1] != l.TY0 {
			t.Errorf("link %d target end (%v,%v,%v), want (%v,%v,%v)",
				i, r.Xs[n-1], r.Bot[n-1], r.Top[n-1], l.TX, l.TY0, l.TY1)
		}
	}
}

// TestSampleIsASimpleRing pins the two properties the ear-clipping fill
// depends on (ADR-0159 SD2): x ascends, so the outline never doubles back,
// and the two edges are a constant distance apart, so they cannot cross.
func TestSampleIsASimpleRing(t *testing.T) {
	lay := mustCompute(t, testEnergy(), Options{})
	var r Ribbon
	for i := range lay.Links {
		lay.Links[i].Sample(DefaultSamples, &r)
		for k := 1; k < len(r.Xs); k++ {
			if r.Xs[k] < r.Xs[k-1] {
				t.Fatalf("link %d: Xs not ascending at %d (%.9f < %.9f)", i, k, r.Xs[k], r.Xs[k-1])
			}
		}
		for k := range r.Xs {
			if got := r.Top[k] - r.Bot[k]; math.Abs(got-r.Thickness) > eps {
				t.Fatalf("link %d: thickness %.9f at %d, want %.9f", i, got, k, r.Thickness)
			}
			if r.Top[k] < r.Bot[k] {
				t.Fatalf("link %d: edges crossed at %d", i, k)
			}
		}
	}
}

// TestContainsMatchesTheDrawnEdge is the reason draw and hit test share one
// sampler: the region tested has to be the region drawn.
func TestContainsMatchesTheDrawnEdge(t *testing.T) {
	lay := mustCompute(t, testEnergy(), Options{})
	var r Ribbon
	const inset = 1e-6
	for i := range lay.Links {
		lay.Links[i].Sample(DefaultSamples, &r)
		for k := range r.Xs {
			x := r.Xs[k]
			mid := (r.Top[k] + r.Bot[k]) / 2
			if !r.Contains(x, mid) {
				t.Errorf("link %d: midline (%.6f,%.6f) not contained", i, x, mid)
			}
			if !r.Contains(x, r.Top[k]-inset) {
				t.Errorf("link %d: just inside the top edge not contained at x=%.6f", i, x)
			}
			if !r.Contains(x, r.Bot[k]+inset) {
				t.Errorf("link %d: just inside the bottom edge not contained at x=%.6f", i, x)
			}
			// Outside, with slack for the linear interpolation between two
			// samples of a curved edge.
			if r.Contains(x, r.Top[k]+r.Thickness) {
				t.Errorf("link %d: a point a full thickness above the top is contained at x=%.6f", i, x)
			}
			if r.Contains(x, r.Bot[k]-r.Thickness) {
				t.Errorf("link %d: a point a full thickness below the bottom is contained at x=%.6f", i, x)
			}
		}
		n := len(r.Xs)
		if r.Contains(r.Xs[0]-0.01, (r.Top[0]+r.Bot[0])/2) {
			t.Errorf("link %d: contained left of the source face", i)
		}
		if r.Contains(r.Xs[n-1]+0.01, (r.Top[n-1]+r.Bot[n-1])/2) {
			t.Errorf("link %d: contained right of the target face", i)
		}
	}
}

func TestSampleReusesBuffers(t *testing.T) {
	lay := mustCompute(t, testEnergy(), Options{})
	r := lay.Links[0].Sample(DefaultSamples, nil)
	before := cap(r.Xs)
	allocs := testing.AllocsPerRun(20, func() {
		for i := range lay.Links {
			lay.Links[i].Sample(DefaultSamples, r)
		}
	})
	if allocs != 0 {
		t.Errorf("Sample into a warm Ribbon allocated %.1f times per run", allocs)
	}
	if cap(r.Xs) != before {
		t.Errorf("buffer grew from %d to %d", before, cap(r.Xs))
	}
}

func TestSampleNilAndDegenerate(t *testing.T) {
	lay := mustCompute(t, testEnergy(), Options{})
	if got := lay.Links[0].Sample(0, nil); len(got.Xs) != DefaultSamples+1 {
		t.Errorf("samples 0 gave %d points, want %d", len(got.Xs), DefaultSamples+1)
	}
	if got := lay.Links[0].Sample(1, nil); len(got.Xs) != 2 {
		t.Errorf("samples 1 gave %d points, want 2", len(got.Xs))
	}
}

func TestNodeAt(t *testing.T) {
	lay := mustCompute(t, testEnergy(), Options{})
	for i := range lay.Nodes {
		n := &lay.Nodes[i]
		cx, cy := (n.X0+n.X1)/2, (n.Y0+n.Y1)/2
		if got := lay.NodeAt(cx, cy); got != i {
			t.Errorf("NodeAt(centre of %s) = %d, want %d", n.ID, got, i)
		}
	}
	// The gap between two stages belongs to no node.
	if got := lay.NodeAt(0.5, 0.5); got != -1 {
		t.Errorf("NodeAt in the gap = %d, want -1", got)
	}
}

func TestLinkAtPicksTheTopmost(t *testing.T) {
	lay := mustCompute(t, testEnergy(), Options{})
	var scratch Ribbon
	for i := range lay.Links {
		l := lay.Links[i]
		l.Sample(DefaultSamples, &scratch)
		// Probe at the source face, where ribbons are guaranteed not to
		// overlap because they tile the bar.
		x := l.SX + 1e-9
		y := (l.SY0 + l.SY1) / 2
		if got := lay.LinkAt(x, y, DefaultSamples, &scratch); got != i {
			t.Errorf("LinkAt at link %d's source face = %d", i, got)
		}
	}
	if got := lay.LinkAt(-1, 0.5, DefaultSamples, &scratch); got != -1 {
		t.Errorf("LinkAt outside the box = %d, want -1", got)
	}
}
