package swisstopo

import (
	"math"
	"math/rand/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// EnsembleSpec configures a Monte-Carlo line-of-sight ensemble. The bearing
// fan (HalfRangeDeg / StepDeg) stays deterministic; the four geometric inputs
// each get an independent Gaussian whose standard deviation is given here. A
// zero σ pins that input to its nominal value.
type EnsembleSpec struct {
	HalfRangeDeg float64
	StepDeg      float64
	Samples      int
	Seed         uint64

	SigmaObsPosM    float64 // observer (sweep-center) horizontal position, 1σ metres
	SigmaTgtPosM    float64 // target horizontal position, 1σ metres
	SigmaObsHeightM float64 // observer height above terrain, 1σ metres
	SigmaTgtHeightM float64 // target height above terrain, 1σ metres
}

func (s EnsembleSpec) anyRandom() bool {
	return s.SigmaObsPosM > 0 || s.SigmaTgtPosM > 0 ||
		s.SigmaObsHeightM > 0 || s.SigmaTgtHeightM > 0
}

// SampledVar is one random input's realised draws (as deviations from the
// nominal, in metres), for plotting the empirical distribution. Positions are
// recorded as a radial offset (≥0); heights as a signed deviation.
type SampledVar struct {
	Name string
	Dev  []float64
}

// LOSEnsembleResult aggregates a Monte-Carlo line-of-sight ensemble to expose
// how the verdict and the height profiles vary under input uncertainty.
//
// Per relative bearing the visibility verdict is reduced to a fraction across
// samples, and per (bearing, distance) the terrain elevation is reduced to a
// min/max envelope. AngleDeg, VisProb, TerrainMin and TerrainMax are parallel
// by bearing; Distance is the shared along-ray axis. Inputs holds the realised
// draw of every randomised variable for the distribution pane.
type LOSEnsembleResult struct {
	Nominal    LOSSweepResult
	AngleDeg   []float64
	Distance   []float64
	VisProb    []float64
	TerrainMin [][]float32
	TerrainMax [][]float32
	Samples    int
	Spec       EnsembleSpec
	Inputs     []SampledVar
}

// LineOfSightSweepEnsemble evaluates the nominal polar sweep plus spec.Samples
// Monte-Carlo sweeps. Each sample independently jitters the observer position,
// target position, observer height and target height by their configured
// Gaussian σ (the bearing fan stays deterministic), runs a full sweep, and the
// ensemble is reduced to per-bearing visibility probability and a
// per-(bearing, distance) terrain envelope. The draws are recorded in
// result.Inputs. spec.Seed makes the draw reproducible.
//
// With no σ set or zero samples this collapses to the nominal sweep (VisProb is
// then the nominal binary verdict and the envelope collapses onto the nominal
// profile), so it is a strict generalisation of LineOfSightSweep.
func (inst *ElevationSampler) LineOfSightSweepEnsemble(from LV95Coord, fromHeight float64, to LV95Coord, toHeight float64, spec EnsembleSpec) (result LOSEnsembleResult, err error) {
	var nominal LOSSweepResult
	nominal, err = inst.LineOfSightSweep(from, fromHeight, to, toHeight, spec.HalfRangeDeg, spec.StepDeg)
	if err != nil {
		err = eh.Errorf("nominal sweep failed: %w", err)
		return
	}

	var members []LOSSweepResult
	var obsOff, tgtOff, obsHDev, tgtHDev []float64
	if spec.anyRandom() && spec.Samples > 0 {
		// Fixed second stream constant keeps the PCG draw a pure function of
		// the seed; the six normals are drawn in a fixed order per sample so
		// toggling one σ does not shift another variable's stream.
		rng := rand.New(rand.NewPCG(spec.Seed, 0x9e3779b97f4a7c15))
		members = make([]LOSSweepResult, 0, spec.Samples)
		obsOff = make([]float64, 0, spec.Samples)
		tgtOff = make([]float64, 0, spec.Samples)
		obsHDev = make([]float64, 0, spec.Samples)
		tgtHDev = make([]float64, 0, spec.Samples)
		for i := range spec.Samples {
			dObsE := rng.NormFloat64() * spec.SigmaObsPosM
			dObsN := rng.NormFloat64() * spec.SigmaObsPosM
			dTgtE := rng.NormFloat64() * spec.SigmaTgtPosM
			dTgtN := rng.NormFloat64() * spec.SigmaTgtPosM
			dObsH := rng.NormFloat64() * spec.SigmaObsHeightM
			dTgtH := rng.NormFloat64() * spec.SigmaTgtHeightM

			jFrom := LV95Coord{E: from.E + dObsE, N: from.N + dObsN}
			jTo := LV95Coord{E: to.E + dTgtE, N: to.N + dTgtN}

			var m LOSSweepResult
			m, err = inst.LineOfSightSweep(jFrom, fromHeight+dObsH, jTo, toHeight+dTgtH, spec.HalfRangeDeg, spec.StepDeg)
			if err != nil {
				err = eh.Errorf("ensemble sweep %d failed: %w", i, err)
				return
			}
			members = append(members, m)
			obsOff = append(obsOff, math.Hypot(dObsE, dObsN))
			tgtOff = append(tgtOff, math.Hypot(dTgtE, dTgtN))
			obsHDev = append(obsHDev, dObsH)
			tgtHDev = append(tgtHDev, dTgtH)
		}
	}

	result = aggregateEnsemble(nominal, members)
	result.Spec = spec
	// Only surface variables that were actually randomised.
	if spec.SigmaObsPosM > 0 {
		result.Inputs = append(result.Inputs, SampledVar{Name: "observer position", Dev: obsOff})
	}
	if spec.SigmaTgtPosM > 0 {
		result.Inputs = append(result.Inputs, SampledVar{Name: "target position", Dev: tgtOff})
	}
	if spec.SigmaObsHeightM > 0 {
		result.Inputs = append(result.Inputs, SampledVar{Name: "observer height", Dev: obsHDev})
	}
	if spec.SigmaTgtHeightM > 0 {
		result.Inputs = append(result.Inputs, SampledVar{Name: "target height", Dev: tgtHDev})
	}
	return
}

// aggregateEnsemble reduces a nominal sweep plus Monte-Carlo member sweeps to
// per-bearing visibility fractions and a per-(bearing, distance) terrain
// envelope. Pure: no sampler, so it is unit-testable with synthetic sweeps.
// The envelope brackets the nominal and every member; the visibility fraction
// is over members only (the nominal is the central estimate, not a random
// draw), falling back to the nominal binary verdict when there are no members.
func aggregateEnsemble(nominal LOSSweepResult, members []LOSSweepResult) (result LOSEnsembleResult) {
	result.Nominal = nominal
	result.AngleDeg = nominal.AngleDeg
	result.Samples = len(members)

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
