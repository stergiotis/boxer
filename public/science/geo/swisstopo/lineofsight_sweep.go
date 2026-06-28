package swisstopo

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// LOSSweepResult is a polar fan of line-of-sight rays about a common
// observer. Interpreting the observer→target line in polar coordinates,
// the bearing is swept by a symmetric range and LineOfSight is evaluated
// per ray. AngleDeg, Targets and Rays are parallel slices (one entry per
// ray), ordered by ascending angle offset.
//
// Because the range (observer→target distance) and the 2 m sampling step
// are identical for every ray, all rays share one distance axis — i.e.
// Rays[i].ProfileDist is the same for every i — so the result reads as a
// dense (angle × distance → elevation) field.
type LOSSweepResult struct {
	// AngleDeg holds each ray's bearing offset in degrees, ascending and
	// symmetric about 0 (the original observer→target ray).
	AngleDeg []float64
	// Targets holds each ray's rotated endpoint (the observer→target
	// vector rotated about the observer by AngleDeg[i]), parallel to Rays.
	Targets []LV95Coord
	// Rays holds the per-ray line-of-sight result, parallel to AngleDeg.
	Rays []LOSResult
}

// LineOfSightSweep interprets the observer (from) → target (to) sight
// line in polar coordinates about the observer and sweeps the bearing by
// ±halfRangeDeg in stepDeg increments, evaluating LineOfSight per ray.
// The range (|to-from|) is held constant, so the target traces an arc;
// only the bearing changes.
//
// halfRangeDeg == 0 yields a single ray (the original line), making this
// a strict generalisation of LineOfSight. stepDeg must be > 0 whenever
// halfRangeDeg > 0. The swept offsets are integer multiples of stepDeg
// in [-halfRangeDeg, +halfRangeDeg]; when halfRangeDeg is not an exact
// multiple of stepDeg the realised extent is floor(halfRangeDeg/stepDeg)
// * stepDeg.
//
// fromHeight / toHeight are metres above terrain (e.g. 1.7 for eye
// height), forwarded unchanged to each ray's LineOfSight.
func (inst *ElevationSampler) LineOfSightSweep(from LV95Coord, fromHeight float64, to LV95Coord, toHeight float64, halfRangeDeg float64, stepDeg float64) (result LOSSweepResult, err error) {
	if halfRangeDeg < 0 {
		err = eh.Errorf("halfRangeDeg must be >= 0, got %g", halfRangeDeg)
		return
	}
	if halfRangeDeg > 0 && stepDeg <= 0 {
		err = eh.Errorf("stepDeg must be > 0 when halfRangeDeg > 0, got %g", stepDeg)
		return
	}

	offsets := sweepAngleOffsets(halfRangeDeg, stepDeg)
	result.AngleDeg = offsets
	result.Targets = make([]LV95Coord, 0, len(offsets))
	result.Rays = make([]LOSResult, 0, len(offsets))

	for _, deg := range offsets {
		target := rotateTargetAbout(from, to, deg)
		var ray LOSResult
		ray, err = inst.LineOfSight(from, fromHeight, target, toHeight)
		if err != nil {
			err = eh.Errorf("line-of-sight at sweep offset %g deg failed: %w", deg, err)
			return
		}
		result.Targets = append(result.Targets, target)
		result.Rays = append(result.Rays, ray)
	}

	// Normalise to a common distance axis. The range is mathematically
	// identical for every ray, but floating-point rounding of the rotated
	// endpoint can make ceil(totalDist/step) differ by one, so ray lengths
	// vary by ±1 sample. Trim every ray to the shortest so the result is a
	// rectangular (angle × distance) field. The dropped sample is one step
	// at the far end; LineOfSight never treats an endpoint as an
	// obstruction candidate, so the per-ray visibility verdict is
	// unaffected.
	minLen := math.MaxInt
	for _, ray := range result.Rays {
		if len(ray.ProfileDist) < minLen {
			minLen = len(ray.ProfileDist)
		}
	}
	for i := range result.Rays {
		result.Rays[i] = truncateProfile(result.Rays[i], minLen)
	}
	return
}

// truncateProfile trims a ray's parallel profile arrays to n samples,
// leaving the scalar LOS verdict (Visible, Obstruction*) untouched —
// those describe the full ray and never depend on the endpoint sample.
func truncateProfile(r LOSResult, n int) (out LOSResult) {
	out = r
	if len(out.ProfileDist) <= n {
		return
	}
	out.ProfileDist = out.ProfileDist[:n]
	out.ProfileElev = out.ProfileElev[:n]
	out.LOSElev = out.LOSElev[:n]
	return
}

// sweepAngleOffsets returns the ascending, symmetric list of degree
// offsets for a ±halfRangeDeg sweep at stepDeg spacing, always including
// 0. A non-positive halfRangeDeg or stepDeg collapses to a single {0}
// ray. Offsets are integer multiples of stepDeg, so the realised extent
// is floor(halfRangeDeg/stepDeg) * stepDeg.
func sweepAngleOffsets(halfRangeDeg float64, stepDeg float64) (offsets []float64) {
	if halfRangeDeg <= 0 || stepDeg <= 0 {
		return []float64{0}
	}
	// +1e-9 so an exact ratio (e.g. 2.0/0.5) floors to 4, not 3, despite
	// binary rounding.
	n := int(math.Floor(halfRangeDeg/stepDeg + 1e-9))
	offsets = make([]float64, 0, 2*n+1)
	for i := -n; i <= n; i++ {
		offsets = append(offsets, float64(i)*stepDeg)
	}
	return
}

// rotateTargetAbout rotates the from→to vector about `from` by deg
// degrees (counter-clockwise in the LV95 E/N plane) and returns the new
// target, preserving the range |to-from|. deg == 0 reproduces `to`
// exactly. LV95 is a conformal projection, so over the few-kilometre /
// few-degree span of a sweep this grid-space rotation is
// indistinguishable from a true geodesic azimuth change (ADR-0099).
func rotateTargetAbout(from LV95Coord, to LV95Coord, deg float64) (target LV95Coord) {
	rad := deg * math.Pi / 180.0
	dE := to.E - from.E
	dN := to.N - from.N
	cos := math.Cos(rad)
	sin := math.Sin(rad)
	target = LV95Coord{
		E: from.E + dE*cos - dN*sin,
		N: from.N + dE*sin + dN*cos,
	}
	return
}
