package terrainscope

// terrainscope_tour.go enrols a synthetic line-of-sight polar sweep into the
// imzero2 demo registry (ADR-0057) so the central TestDriver captures it in
// the widgets tour. The live App is a windowed AppI that needs two map clicks,
// a slippy-map basemap, and swissALTI3D tiles — none of which fit the
// deterministic, network-free tour stage. So this scene seeds a hand-built
// LOSEnsembleResult (a valley with a central ridge: the centre rays are
// blocked, the outer rays stay clear, and a Monte-Carlo-style envelope widens
// with distance) and renders the panel-free sweep view — the overlaid height
// profiles, the per-ray uncertainty bands, the centre sight-line, and the
// obstruction scatter. Screenshot scaffolding only; the live app opens from
// the carousel and reads real tiles.

import (
	"math"

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
		Description: "Synthetic ±2° line-of-sight sweep (0.5° step) under observer-position " +
			"uncertainty: a fan of terrain height profiles, each wrapped in a Monte-Carlo " +
			"elevation envelope that widens with distance, plus per-ray visibility percentages, " +
			"the centre sight-line, and obstruction points. A valley with a central ridge blocks " +
			"the centre rays while the outer rays stay clear. Screenshot scaffolding — the live " +
			"app reads swissALTI3D tiles on a slippy map.",
		Init:           terrainTourInit,
		RenderStateful: terrainTourRender,
		SourceFunc:     (*App).renderSweepPanel,
	})
}

func terrainTourInit(_ *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.stage = selectionStagePt2
	inst.result = synthSweepResult()
	return inst
}

func terrainTourRender(_ *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	ids.Reset()
	for range c.IdScope(ids.PrepareSeq(inst.seed)) {
		inst.renderSweepPanel(inst.result)
	}
}

// synthSweepResult builds an illustrative ensemble: a valley (terrain dips
// below the near-flat sight line) with a Gaussian ridge at mid-range whose
// height grows toward the centre bearing, so the centre rays are obstructed
// and the outer rays stay visible. A distance-widening band stands in for the
// Monte-Carlo elevation envelope, and the visibility fraction ramps from the
// blocked centre to the clear edges.
func synthSweepResult() *sweepResult {
	const (
		nRays = 9
		nPts  = 31
		stepM = 40.0
		half  = 2.0
		stepD = 0.5
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

	nominal := swisstopo.LOSSweepResult{AngleDeg: angles, Rays: rays}
	return &sweepResult{
		ens: swisstopo.LOSEnsembleResult{
			Nominal:    nominal,
			AngleDeg:   angles,
			Distance:   dist,
			VisProb:    visProb,
			TerrainMin: tMin,
			TerrainMax: tMax,
			Samples:    24,
			SigmaM:     12,
		},
		fromLV: swisstopo.LV95Coord{E: 2_600_000, N: 1_200_000},
		toLV:   swisstopo.LV95Coord{E: 2_600_849, N: 1_200_849},
	}
}
