package swisstopo

import (
	"math/rand/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// LOSEnsembleResult aggregates a Monte-Carlo ensemble of polar sweeps over
// the observer (sweep-center) position to expose how the line-of-sight
// verdict and the height profiles vary under center-position uncertainty.
//
// The observer is sampled from an isotropic 2D Gaussian N(center, σ²) in the
// LV95 plane; each sample yields a full LineOfSightSweep toward the fixed
// target. Per relative bearing the visibility verdict is reduced to a
// fraction across samples, and per (bearing, distance) the terrain elevation
// is reduced to a min/max envelope. AngleDeg, VisProb, TerrainMin and
// TerrainMax are parallel by bearing (one entry per ray); Distance is the
// shared along-ray axis.
type LOSEnsembleResult struct {
	// Nominal is the sweep from the un-jittered center — the central
	// estimate, drawn bold by consumers.
	Nominal LOSSweepResult
	// AngleDeg holds each ray's bearing offset in degrees (= Nominal.AngleDeg).
	AngleDeg []float64
	// Distance is the common along-ray distance axis (metres), trimmed to
	// the shortest member so the envelope is rectangular.
	Distance []float64
	// VisProb[j] is the fraction of Monte-Carlo samples whose ray j has a
	// clear line of sight, in [0,1]. With zero samples it is the nominal
	// ray's binary verdict (0 or 1).
	VisProb []float64
	// TerrainMin[j][k] / TerrainMax[j][k] bound the terrain elevation at ray
	// j, distance Distance[k], across the nominal sweep and all samples.
	TerrainMin [][]float32
	TerrainMax [][]float32
	// Samples is the number of Monte-Carlo members evaluated (excludes the
	// nominal sweep).
	Samples int
	// SigmaM is the Gaussian standard deviation (metres) used to jitter the
	// center.
	SigmaM float64
}

// LineOfSightSweepEnsemble evaluates the nominal polar sweep plus `samples`
// Monte-Carlo sweeps whose observer is jittered by an isotropic 2D Gaussian
// of standard deviation sigmaM (metres) about `from`. The target is held
// fixed — the uncertainty models where the observer is, not where the target
// is. seed makes the draw reproducible.
//
// sigmaM <= 0 or samples <= 0 reduces to the nominal sweep alone (VisProb is
// then the nominal binary verdict and the envelope collapses onto the nominal
// profile), so this is a strict generalisation of LineOfSightSweep.
func (inst *ElevationSampler) LineOfSightSweepEnsemble(from LV95Coord, fromHeight float64, to LV95Coord, toHeight float64, halfRangeDeg float64, stepDeg float64, sigmaM float64, samples int, seed uint64) (result LOSEnsembleResult, err error) {
	var nominal LOSSweepResult
	nominal, err = inst.LineOfSightSweep(from, fromHeight, to, toHeight, halfRangeDeg, stepDeg)
	if err != nil {
		err = eh.Errorf("nominal sweep failed: %w", err)
		return
	}

	var members []LOSSweepResult
	if sigmaM > 0 && samples > 0 {
		// Fixed second stream constant keeps the PCG draw a pure function of
		// seed; the golden-ratio odd constant is the conventional choice.
		rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
		members = make([]LOSSweepResult, 0, samples)
		for i := range samples {
			jit := LV95Coord{
				E: from.E + rng.NormFloat64()*sigmaM,
				N: from.N + rng.NormFloat64()*sigmaM,
			}
			var m LOSSweepResult
			m, err = inst.LineOfSightSweep(jit, fromHeight, to, toHeight, halfRangeDeg, stepDeg)
			if err != nil {
				err = eh.Errorf("ensemble sweep %d failed: %w", i, err)
				return
			}
			members = append(members, m)
		}
	}

	result = aggregateEnsemble(nominal, members, sigmaM)
	return
}

// aggregateEnsemble reduces a nominal sweep plus Monte-Carlo member sweeps to
// per-bearing visibility fractions and a per-(bearing, distance) terrain
// envelope. Pure: no sampler, so it is unit-testable with synthetic sweeps.
// The envelope brackets the nominal and every member; the visibility fraction
// is over members only (the nominal is the central estimate, not a random
// draw), falling back to the nominal binary verdict when there are no members.
func aggregateEnsemble(nominal LOSSweepResult, members []LOSSweepResult, sigmaM float64) (result LOSEnsembleResult) {
	result.Nominal = nominal
	result.AngleDeg = nominal.AngleDeg
	result.Samples = len(members)
	result.SigmaM = sigmaM

	nRays := len(nominal.Rays)
	if nRays == 0 {
		return
	}

	// Common length: the shortest ray across the nominal and all members
	// (member ranges differ by ~σ, so point counts can differ by one).
	minLen := len(nominal.Rays[0].ProfileDist)
	for _, m := range members {
		if len(m.Rays) != nRays {
			continue // defensive: shapes should match (same half/step)
		}
		if l := len(m.Rays[0].ProfileDist); l < minLen {
			minLen = l
		}
	}
	result.Distance = append([]float64(nil), nominal.Rays[0].ProfileDist[:minLen]...)

	result.VisProb = make([]float64, nRays)
	result.TerrainMin = make([][]float32, nRays)
	result.TerrainMax = make([][]float32, nRays)

	for j := range nRays {
		// Visibility fraction over members (fallback to nominal verdict).
		if len(members) == 0 {
			if nominal.Rays[j].Visible {
				result.VisProb[j] = 1
			}
		} else {
			cnt := 0
			for _, m := range members {
				if len(m.Rays) == nRays && m.Rays[j].Visible {
					cnt++
				}
			}
			result.VisProb[j] = float64(cnt) / float64(len(members))
		}

		// Terrain envelope across nominal + members.
		mn := make([]float32, minLen)
		mx := make([]float32, minLen)
		for k := range minLen {
			lo := nominal.Rays[j].ProfileElev[k]
			hi := lo
			for _, m := range members {
				if len(m.Rays) != nRays {
					continue
				}
				v := m.Rays[j].ProfileElev[k]
				if v < lo {
					lo = v
				}
				if v > hi {
					hi = v
				}
			}
			mn[k] = lo
			mx[k] = hi
		}
		result.TerrainMin[j] = mn
		result.TerrainMax[j] = mx
	}
	return
}
