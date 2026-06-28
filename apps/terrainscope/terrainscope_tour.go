package terrainscope

// terrainscope_tour.go enrols two synthetic scenes into the imzero2 demo
// registry (ADR-0057) so the central TestDriver captures them in the widgets
// tour: the line-of-sight polar sweep, and the input-distribution pane. The
// live App is a windowed AppI that needs two map clicks, a slippy-map basemap,
// and swissALTI3D tiles — none of which fit the deterministic, network-free
// tour stage. So both scenes share a hand-built LOSEnsembleResult (a valley
// with a central ridge; a Monte-Carlo elevation envelope that widens with
// distance; and recorded draws for four randomised inputs) and render the
// panel-free views directly. Screenshot scaffolding only.

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/science/geo/swisstopo"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "terrainscope-sweep",
		Category: "Science",
		Title:    icons.PhMountains + " terrainscope — LOS polar sweep",
		Stage:    [2]float32{1040, 560},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "Synthetic ±2° line-of-sight sweep (0.5° step) under input uncertainty: a " +
			"fan of terrain height profiles, each wrapped in a Monte-Carlo elevation envelope that " +
			"widens with distance, plus per-ray visibility percentages, the centre sight-line, and " +
			"obstruction points. Screenshot scaffolding — the live app reads swissALTI3D tiles.",
		Init:           terrainTourInit,
		RenderStateful: terrainTourRenderSweep,
		SourceFunc:     (*App).renderSweepPanel,
	})
	registry.Register(registry.Demo{
		Name:     "terrainscope-distributions",
		Category: "Science",
		Title:    icons.IconChartLine + " terrainscope — input distributions",
		Stage:    [2]float32{1040, 420},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindUX,
		Description: "The distribution pane: each randomised input (observer/target position and " +
			"height) is drawn from its own Gaussian; the empirical CDF of the realised draws " +
			"(deviation from nominal, metres) is shown as one step-line per variable. The sweep " +
			"angle stays deterministic. Screenshot scaffolding for the live terrainscope app.",
		Init:           terrainTourInit,
		RenderStateful: terrainTourRenderDist,
		SourceFunc:     (*App).renderDistPane,
	})
}

func terrainTourInit(_ *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.stage = selectionStagePt2
	inst.result = synthSweepResult()
	return inst
}

func terrainTourRenderSweep(_ *c.WidgetIdStack, state any) {
	if inst, ok := state.(*App); ok && inst != nil {
		ids.Reset()
		for range c.IdScope(ids.PrepareSeq(inst.seed)) {
			inst.renderSweepPanel(inst.result)
		}
	}
}

func terrainTourRenderDist(_ *c.WidgetIdStack, state any) {
	if inst, ok := state.(*App); ok && inst != nil {
		ids.Reset()
		for range c.IdScope(ids.PrepareSeq(inst.seed)) {
			inst.renderDistPane(inst.result)
		}
	}
}

// synthSweepResult builds an illustrative ensemble: a valley (terrain dips
// below the near-flat sight line) with a Gaussian ridge at mid-range whose
// height grows toward the centre bearing, so the centre rays are obstructed and
// the outer rays stay visible. A distance-widening band stands in for the
// Monte-Carlo elevation envelope, the visibility fraction ramps from the
// blocked centre to the clear edges, and four randomised inputs are drawn from
// their own Gaussians (seeded, so the scene is deterministic).
func synthSweepResult() *sweepResult {
	const (
		nRays   = 9
		nPts    = 31
		stepM   = 40.0
		half    = 2.0
		stepD   = 0.5
		samples = 24
	)
	total := float64(nPts-1) * stepM // 1200 m
	fromLOS := 601.7                 // observer 600 m + 1.7 m eye height
	toLOS := 600.0                   // target 600 m + 0 m

	dist := make([]float64, nPts)
	los := make([]float32, nPts)
	for j := range nPts {
		d := float64(j) * stepM
		dist[j] = d
		t := d / total
		los[j] = float32(fromLOS*(1-t) + toLOS*t)
	}

	angles := make([]float64, nRays)
	rays := make([]swisstopo.LOSResult, nRays)
	visProb := make([]float64, nRays)
	tMin := make([][]float32, nRays)
	tMax := make([][]float32, nRays)

	for i := range nRays {
		deg := -half + float64(i)*stepD
		angles[i] = deg
		frac := 1.0 - math.Abs(deg)/half // 1 at centre, 0 at edges
		ridge := 8.0 + frac*40.0         // 8 m (edge) … 48 m (centre)

		terrain := make([]float32, nPts)
		mn := make([]float32, nPts)
		mx := make([]float32, nPts)
		visible := true
		var obsDist float64
		var obsElev float32
		for j := range nPts {
			d := dist[j]
			valley := 600.0 - 20.0*math.Sin(math.Pi*d/total)
			bump := ridge * math.Exp(-math.Pow((d-total*0.5)/150.0, 2))
			e := float32(valley + bump)
			terrain[j] = e
			band := float32(2.0 + 10.0*(d/total)) // ±2 m near … ±12 m far
			mn[j] = e - band
			mx[j] = e + band
			if j > 0 && j < nPts-1 && visible && e > los[j] {
				visible = false
				obsDist = d
				obsElev = e
			}
		}
		rays[i] = swisstopo.LOSResult{
			Visible:         visible,
			FromElev:        600,
			ToElev:          600,
			ObstructionDist: obsDist,
			ObstructionElev: obsElev,
			ProfileDist:     dist,
			ProfileElev:     terrain,
			LOSElev:         los,
		}
		visProb[i] = 0.95 - 0.9*frac // edges ~0.95, centre ~0.05
		tMin[i] = mn
		tMax[i] = mx
	}

	spec := swisstopo.EnsembleSpec{
		HalfRangeDeg: half, StepDeg: stepD, Samples: samples, Seed: 1,
		SigmaObsPosM: 12, SigmaTgtPosM: 6, SigmaObsHeightM: 1.5, SigmaTgtHeightM: 1,
	}
	rng := rand.New(rand.NewPCG(1, 0x9e3779b97f4a7c15))
	mkDev := func(sigma float64, radial bool) []float64 {
		d := make([]float64, samples)
		for i := range d {
			if radial {
				d[i] = math.Hypot(rng.NormFloat64()*sigma, rng.NormFloat64()*sigma)
			} else {
				d[i] = rng.NormFloat64() * sigma
			}
		}
		return d
	}
	inputs := []swisstopo.SampledVar{
		{Name: "observer position", Dev: mkDev(spec.SigmaObsPosM, true)},
		{Name: "target position", Dev: mkDev(spec.SigmaTgtPosM, true)},
		{Name: "observer height", Dev: mkDev(spec.SigmaObsHeightM, false)},
		{Name: "target height", Dev: mkDev(spec.SigmaTgtHeightM, false)},
	}

	return &sweepResult{
		ens: swisstopo.LOSEnsembleResult{
			Nominal:    swisstopo.LOSSweepResult{AngleDeg: angles, Rays: rays},
			AngleDeg:   angles,
			Distance:   dist,
			VisProb:    visProb,
			TerrainMin: tMin,
			TerrainMax: tMax,
			Samples:    samples,
			Spec:       spec,
			Inputs:     inputs,
		},
		fromLV:     swisstopo.LV95Coord{E: 2_600_000, N: 1_200_000},
		toLV:       swisstopo.LV95Coord{E: 2_600_849, N: 1_200_849},
		computeDur: 18500 * time.Microsecond, // illustrative simulation time
	}
}
