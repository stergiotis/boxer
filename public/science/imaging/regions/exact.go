package regions

import (
	"math"
	"math/big"
)

// The predicates here decide on which side of an outline a point lies, and
// in which order a line meets two grid lines, exactly for the float64
// numbers they are given: a float64 evaluation first, accepted when its
// error bound shows the sign is certain, and an evaluation in rational
// numbers otherwise. A tie — a pixel centre on an edge, a line through a
// pixel corner — is therefore decided by the rule written for it and never
// by rounding (ADR-0399 §SD3, §SD8).

// errFactor bounds the relative error of a difference of two products of
// differences evaluated in float64: Shewchuk's ccwerrboundA, (3 + 16ε)ε.
const errFactor = (3 + 16*0x1p-53) * 0x1p-53

// signDiffProd is the sign of (a1 − a2)(b1 − b2) − (c1 − c2)(d1 − d2).
func signDiffProd(a1, a2, b1, b2, c1, c2, d1, d2 float64) int {
	p := (a1 - a2) * (b1 - b2)
	q := (c1 - c2) * (d1 - d2)
	mag := math.Abs(p) + math.Abs(q)
	// The subtractions are correctly rounded, so each factor's sign is exact;
	// a product that underflows is not, and neither is a sum near overflow.
	if mag > 0x1p-900 && mag < 0x1p900 {
		det := p - q
		if math.Abs(det) > errFactor*mag {
			return sign(det)
		}
	}
	return signDiffProdExact(a1, a2, b1, b2, c1, c2, d1, d2)
}

func signDiffProdExact(a1, a2, b1, b2, c1, c2, d1, d2 float64) int {
	var x, y, t big.Rat
	x.Mul(ratDiff(a1, a2), ratDiff(b1, b2))
	y.Mul(ratDiff(c1, c2), ratDiff(d1, d2))
	t.Sub(&x, &y)
	return t.Sign()
}

func rat(v float64) *big.Rat {
	r := new(big.Rat)
	r.SetFloat64(v)
	return r
}

func ratDiff(a, b float64) *big.Rat {
	r := rat(a)
	return r.Sub(r, rat(b))
}

func sign(v float64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// atOrRightOfEdge reports whether a point (x, y) on a row lies at or to the
// right of where the edge from (x0, y0) to (x1, y1), y0 < y1, crosses that
// row: x ≥ x0 + (y − y0)(x1 − x0)/(y1 − y0), decided exactly.
func atOrRightOfEdge(x0, y0, x1, y1, x, y float64) bool {
	// (x − x0)(y1 − y0) − (x1 − x0)(y − y0) ≥ 0, with y1 − y0 > 0.
	return signDiffProd(x, x0, y1, y0, x1, x0, y, y0) >= 0
}

// ellipseGeom is an ellipse as a quadratic form, for the computations that
// do not decide a tie: Q(x, y) = A·dx² + B·dx·dy + C·dy² ≤ 1 with
// (dx, dy) the offset from the centre.
type ellipseGeom struct {
	cx, cy, rx, ry float64
	s, c           float64
	a, b, cc       float64
	// hx and hy are the half extents along x and y; dyR is the row offset
	// from the centre of the rightmost point, −dyR that of the leftmost.
	hx, hy, dyR float64
	// exact holds the parameters as rational numbers once a tie needs them.
	exact *ellipseExact
}

func newEllipseGeom(sh Shape) (e ellipseGeom) {
	e.cx, e.cy, e.rx, e.ry = sh.a.X, sh.a.Y, sh.b.X, sh.b.Y
	e.s, e.c = sincosDeg(sh.angle)
	irx2, iry2 := 1/(e.rx*e.rx), 1/(e.ry*e.ry)
	e.a = e.c*e.c*irx2 + e.s*e.s*iry2
	e.b = 2 * e.c * e.s * (irx2 - iry2)
	e.cc = e.s*e.s*irx2 + e.c*e.c*iry2
	e.hx = math.Hypot(e.rx*e.c, e.ry*e.s)
	e.hy = math.Hypot(e.rx*e.s, e.ry*e.c)
	if e.hx > 0 {
		e.dyR = e.s * e.c * (e.rx*e.rx - e.ry*e.ry) / e.hx
	}
	return e
}

// chord is where the ellipse meets the row line y, clamped so that a line
// within rounding of a tangent meets it in one point; ok is false for a
// line that misses it.
func (inst *ellipseGeom) chord(y float64) (xl, xr float64, ok bool) {
	dy := y - inst.cy
	if math.Abs(dy) > inst.hy {
		return 0, 0, false
	}
	// The discriminant is 4A − 4dy²/(rx²·ry²), since 4AC − B² = 4/(rx²·ry²).
	q := dy / (inst.rx * inst.ry)
	disc := max(4*(inst.a-q*q), 0)
	sq := math.Sqrt(disc)
	mid := -inst.b * dy
	return inst.cx + (mid-sq)/(2*inst.a), inst.cx + (mid+sq)/(2*inst.a), true
}

// ellipseExact is an ellipse's parameters as rational numbers: the centre,
// the radii, and the sine and cosine of its angle as [sincosDeg] gives them
// in float64 — exact at multiples of 90°, the float64 nearest otherwise.
type ellipseExact struct {
	cx, cy, rx2, ry2, s, c big.Rat
}

func (inst *ellipseGeom) rational() *ellipseExact {
	if inst.exact == nil {
		x := &ellipseExact{}
		x.cx.SetFloat64(inst.cx)
		x.cy.SetFloat64(inst.cy)
		rx, ry := rat(inst.rx), rat(inst.ry)
		x.rx2.Mul(rx, rx)
		x.ry2.Mul(ry, ry)
		x.s.SetFloat64(inst.s)
		x.c.SetFloat64(inst.c)
		inst.exact = x
	}
	return inst.exact
}

// side classifies (x, y) against the ellipse: q is the sign of Q − 1
// (−1 inside, 0 on the outline, +1 outside), and d the sign of ∂Q/∂x —
// negative where Q falls as x grows, left of the chord's middle.
func (inst *ellipseGeom) side(x, y float64) (q, d int) {
	dx, dy := x-inst.cx, y-inst.cy
	u := inst.c*dx + inst.s*dy
	v := -inst.s*dx + inst.c*dy
	rx2, ry2 := inst.rx*inst.rx, inst.ry*inst.ry
	l := u*u*ry2 + v*v*rx2
	r := rx2 * ry2
	um := math.Abs(inst.c*dx) + math.Abs(inst.s*dy)
	vm := math.Abs(inst.s*dx) + math.Abs(inst.c*dy)
	lm := um*um*ry2 + vm*vm*rx2
	dd := u*inst.c*ry2 - v*inst.s*rx2
	dm := um*math.Abs(inst.c)*ry2 + vm*math.Abs(inst.s)*rx2
	// The float64 evaluation carries a relative error of a few dozen ε of
	// the magnitudes; 1e-13 leaves a wide margin above it.
	const rel = 1e-13
	sure := r > 0x1p-900 && lm+r < 0x1p900
	qSure := sure && math.Abs(l-r) > rel*(lm+r)
	dSure := sure && math.Abs(dd) > rel*dm
	if qSure && dSure {
		return sign(l - r), sign(dd)
	}
	x2 := inst.rational()
	var rdx, rdy, ru, rv, t1, t2 big.Rat
	rdx.Sub(rat(x), &x2.cx)
	rdy.Sub(rat(y), &x2.cy)
	ru.Add(t1.Mul(&x2.c, &rdx), t2.Mul(&x2.s, &rdy))
	rv.Sub(t1.Mul(&x2.c, &rdy), t2.Mul(&x2.s, &rdx))
	if qSure {
		q = sign(l - r)
	} else {
		var lq, rq, a, b big.Rat
		a.Mul(&ru, &ru)
		a.Mul(&a, &x2.ry2)
		b.Mul(&rv, &rv)
		b.Mul(&b, &x2.rx2)
		lq.Add(&a, &b)
		rq.Mul(&x2.rx2, &x2.ry2)
		q = lq.Cmp(&rq)
	}
	if dSure {
		d = sign(dd)
	} else {
		var a, b big.Rat
		a.Mul(&ru, &x2.c)
		a.Mul(&a, &x2.ry2)
		b.Mul(&rv, &x2.s)
		b.Mul(&b, &x2.rx2)
		d = a.Cmp(&b)
	}
	return q, d
}

// atOrRightOfLeft reports whether x on the row line y lies at or to the
// right of the left end of the ellipse's chord on it; false where the line
// misses the ellipse and x is left of the middle.
func (inst *ellipseGeom) atOrRightOfLeft(x, y float64) bool {
	q, d := inst.side(x, y)
	return q <= 0 || d > 0
}

// leftOfRight reports whether x on the row line y lies strictly left of the
// right end of the ellipse's chord on it; at a tangent point, where the two
// ends are one point, it is false.
func (inst *ellipseGeom) leftOfRight(x, y float64) bool {
	q, d := inst.side(x, y)
	return q < 0 || d < 0
}
