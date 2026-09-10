package graphview

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Donut is a ring of proportional slices drawn around a node (ADR-0224
// §SD9): one slice per positive value, in declaration order, clockwise from
// the top. Colors pairs with Values by index; a missing colour takes the
// design system's qualitative cycle. Total, when larger than the sum of
// Values, leaves the remainder as a muted track, which turns the ring into
// a progress or share display. The zero value draws nothing.
//
// The slices are the caller's: the widget reads them during Render and
// keeps no copy past the frame, so a caller may reuse its buffers.
type Donut struct {
	Values []float32
	Colors color.Colors
	Total  float32
}

// IsEmpty reports whether the donut draws nothing.
func (inst Donut) IsEmpty() bool {
	for _, v := range inst.Values {
		if v > 0 {
			return false
		}
	}
	return true
}

// donutArc is one slice resolved to angles, radians, screen orientation
// (y down, so increasing angle is clockwise), starting at the top.
type donutArc struct {
	a0, a1 float32
	col    color.Color
	track  bool
}

// donutStart is where the first slice begins: straight up.
const donutStart = -math.Pi / 2

// donutArcs resolves a donut into arcs, appending to dst. Non-positive
// values are skipped; the denominator is the larger of Total and the sum of
// the values; a remainder becomes one track arc.
func donutArcs(d Donut, track color.Color, dst []donutArc) []donutArc {
	var sum float32
	for _, v := range d.Values {
		if v > 0 {
			sum += v
		}
	}
	if sum <= 0 {
		return dst
	}
	denom := max(sum, d.Total)
	a := float32(donutStart)
	for i, v := range d.Values {
		if v <= 0 {
			continue
		}
		span := float32(2*math.Pi) * v / denom
		col := color.Color{}
		if i < len(d.Colors) {
			col = color.Hex(d.Colors[i])
		}
		if isUnset(col) {
			col = color.Hex(styletokens.QualitativeCycle(i).AsHex())
		}
		dst = append(dst, donutArc{a0: a, a1: a + span, col: col})
		a += span
	}
	if denom > sum {
		dst = append(dst, donutArc{a0: a, a1: float32(donutStart + 2*math.Pi), col: track, track: true})
	}
	return dst
}

// ringSectorArcPx is the target spacing of the sampled arc, in pixels.
const ringSectorArcPx = 4

// ringSector appends the closed outline of a ring sector — the outer arc
// from a0 to a1, then the inner arc back — to xs and ys. It is a concave
// polygon, so it goes through the concave fill. Callers split spans over
// half a turn: a full ring would otherwise be a self-touching outline.
func ringSector(cx, cy, rIn, rOut, a0, a1 float32, xs, ys []float32) ([]float32, []float32) {
	span := a1 - a0
	n := int(math.Ceil(float64(span * rOut / ringSectorArcPx)))
	n = min(max(n, 2), 64)
	for i := 0; i <= n; i++ {
		a := a0 + span*float32(i)/float32(n)
		s, c := math.Sincos(float64(a))
		xs = append(xs, cx+rOut*float32(c))
		ys = append(ys, cy+rOut*float32(s))
	}
	for i := n; i >= 0; i-- {
		a := a0 + span*float32(i)/float32(n)
		s, c := math.Sincos(float64(a))
		xs = append(xs, cx+rIn*float32(c))
		ys = append(ys, cy+rIn*float32(s))
	}
	return xs, ys
}

// donutMinInnerPx is the node radius on screen below which its donut is not
// drawn: the node is a dot at that size, the ring around it would dwarf it,
// and each slice costs a polygon.
const donutMinInnerPx = 2
