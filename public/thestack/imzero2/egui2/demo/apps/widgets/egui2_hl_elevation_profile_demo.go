package widgets

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/science/geo/swisstopo"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// =============================================================================
// DEMO: elevation-profile — slippy map + terrain profile between two clicks
//
// Click two points anywhere in Switzerland; the demo reads the elevation
// profile from swissALTI3D tiles via swisstopo.ElevationSampler in a
// cancellable background goroutine and renders the result as an egui_plot
// line.
//
// Tile directory comes from $SWISSTOPO_TILES_DIR (fallback /home/spx/data/
// swisstopo); when the directory is absent, the demo still renders the map
// and the markers but shows a "tiles not available" label instead of the
// profile.
// =============================================================================

const (
	swissCenterLat       = 46.8182
	swissCenterLon       = 8.2275
	swissTilesDirEnv     = "SWISSTOPO_TILES_DIR"
	swissTilesDirDefault = "/home/spx/data/swisstopo"
	elevMapStageW        = float32(900)
	elevMapStageH        = float32(500)
	elevProfileSampleM   = 50.0
	elevMarkerIdPt1      = uint64(100)
	elevMarkerIdPt2      = uint64(200)
)

// SwissTilesDir is the swissALTI3D tile directory consumed by the
// elevation-profile demo. Empty falls back to swissTilesDirDefault.
var SwissTilesDir = env.NewPath(env.Spec{
	Name:        swissTilesDirEnv,
	Description: "directory containing swissALTI3D 2m COG tiles for the elevation-profile demo",
	Category:    env.CategoryE("swisstopo"),
})

// selectionStageE is the click-placement FSM. "Computing" vs "Done" is
// derived from result presence, not encoded here — keeps the FSM owned
// exclusively by the render goroutine.
type selectionStageE uint8

const (
	selectionStageNone selectionStageE = iota
	selectionStagePt1
	selectionStagePt2
)

// profileResult is the worker's output, published atomically under
// elevationProfileState.mu so reset/republish never races with a render
// read.
type profileResult struct {
	distances  []float64
	elevations []float32
	fromLV     swisstopo.LV95Coord
	toLV       swisstopo.LV95Coord
}

type elevationProfileState struct {
	stage  selectionStageE
	pt1Lat float64
	pt1Lon float64
	pt2Lat float64
	pt2Lon float64

	// Sticky-once override flags for SetZoom / CenterAt — see
	// doc/skills/imzero2/SKILLS.md §16.3. Flipped on the discrete event
	// that should retarget the view; cleared after one frame so the user
	// can pan/zoom freely afterwards.
	overrideZoom   float64
	overrideCenter [2]float64
	applyZoom      bool
	applyCenter    bool

	// plotEpoch is folded into the profile-plot widget id; bumping it on
	// reset gives egui_plot fresh widget state so its cached y-axis
	// transform from the previous selection doesn't dominate the new
	// data's auto-bounds fit. PlotFluid has no ResetView/AutoBoundsReset
	// hook, so widget-id rotation is the cleanest path.
	plotEpoch uint64

	// Worker state. mu serialises {epoch, cancelFn, result} writes against
	// reads in the render goroutine; epoch ensures late-arriving worker
	// results from a cancelled compute are dropped instead of overwriting
	// post-reset state.
	mu       sync.Mutex
	epoch    uint64
	cancelFn context.CancelFunc
	result   *profileResult
}

var (
	samplerOnce sync.Once
	sampler     *swisstopo.ElevationSampler
	samplerErr  error
)

// ensureSampler lazily constructs the package-shared ElevationSampler on
// first call. Uses context.Background() because sampler init must outlive
// any individual demo-instance worker context — a short-lived first caller
// shouldn't poison the sampler for the rest of the process.
func ensureSampler() (s *swisstopo.ElevationSampler, err error) {
	samplerOnce.Do(func() {
		dir := tilesDir()
		sampler, samplerErr = swisstopo.NewElevationSampler(context.Background(), dir)
		if samplerErr != nil {
			log.Error().Err(samplerErr).Str("dir", dir).Str("env", swissTilesDirEnv).Msg("elevation-profile: sampler init failed")
			sampler = nil
		}
	})
	s = sampler
	err = samplerErr
	return
}

func tilesDir() (dir string) {
	dir = SwissTilesDir.Get()
	if dir == "" {
		dir = swissTilesDirDefault
	}
	return
}

func demoElevationProfile(ids *c.WidgetIdStack, st *elevationProfileState) {
	sm := c.CurrentApplicationState.StateManager

	// ── Controls ────────────────────────────────────────────────────────
	if c.Button(ids.PrepareStr("reset"), c.Atoms().Text(icons.IconClose+" Reset").Keep()).
		SendResp().HasPrimaryClicked() {
		resetSelection(st)
	}

	// Snapshot worker output before we render. Holding mu across status
	// computation keeps the "Computing…" vs "Done" decision consistent
	// with the result we'll render below.
	st.mu.Lock()
	res := st.result
	st.mu.Unlock()

	c.Label(statusLabel(st.stage, res != nil)).Send()
	if samplerErr != nil {
		c.Label(fmt.Sprintf("tiles not available (%s=%q): %v", swissTilesDirEnv, tilesDir(), samplerErr)).Send()
	}

	// Capture our own WalkersMap widget id so we can ignore clicks on
	// sibling walkers maps in other demos sharing the dock host. PrepareStr
	// + Derive consumes the stack frame; we re-prepare immediately before
	// the WalkersMap.Send() call below.
	ids.PrepareStr("elev-map")
	mapId := ids.Derive()

	cam := sm.GetWalkersCamera()
	if cam.Found && cam.Clicked && cam.HoverValid && cam.MapId == mapId {
		handleClick(st, cam.HoverLat, cam.HoverLon)
	}

	// ── Overlays — must be emitted BEFORE the WalkersMap opcode so they
	//    drain into this frame's map render. See SKILLS.md §16.2.
	if st.stage >= selectionStagePt1 {
		c.MapMarker(elevMarkerIdPt1, st.pt1Lat, st.pt1Lon).
			Label(fmt.Sprintf("pt1 (%.5f, %.5f)", st.pt1Lat, st.pt1Lon)).
			Color(color.Hex(0x44ff44ff)).Radius(8).Send()
	}
	if st.stage >= selectionStagePt2 {
		c.MapMarker(elevMarkerIdPt2, st.pt2Lat, st.pt2Lon).
			Label(fmt.Sprintf("pt2 (%.5f, %.5f)", st.pt2Lat, st.pt2Lon)).
			Color(color.Hex(0xff4444ff)).Radius(8).Send()
		c.MapPolyline(
			[]float64{st.pt1Lat, st.pt2Lat},
			[]float64{st.pt1Lon, st.pt2Lon},
		).Stroke(color.Hex(0xffff00ff), styletokens.StrokeStrong).Send()
	}

	// ── Map. Sticky overrides are applied via the apply-once flag pattern
	//    (SKILLS.md §16.3); unconditional per-frame SetZoom/CenterAt would
	//    lock the user out of pan/zoom after the second click.
	ids.PrepareStr("elev-map")
	mw := c.WalkersMap(ids, swissCenterLat, swissCenterLon, false).
		Width(elevMapStageW).Height(elevMapStageH)
	if st.applyZoom {
		mw = mw.SetZoom(st.overrideZoom)
		st.applyZoom = false
	}
	if st.applyCenter {
		mw = mw.CenterAt(st.overrideCenter[0], st.overrideCenter[1])
		st.applyCenter = false
	}
	mw.Send()

	// ── Info panel + profile plot ───────────────────────────────────────
	switch {
	case st.stage == selectionStagePt2 && res != nil && len(res.distances) > 1:
		renderProfilePanel(ids, st, res)
	case st.stage == selectionStagePt2 && res == nil:
		c.Label("Loading tiles and computing profile… this may take a moment.").Send()
	case st.stage == selectionStagePt1:
		c.Label(fmt.Sprintf("Pt1: (%.6f, %.6f)", st.pt1Lat, st.pt1Lon)).Send()
		c.Label("Click a second point on the map to compute the profile.").Send()
	}
}

func statusLabel(stage selectionStageE, haveResult bool) (label string) {
	switch {
	case stage == selectionStageNone:
		label = "Selection: 0/2 — click a point on the map"
	case stage == selectionStagePt1:
		label = "Selection: 1/2 — click a second point on the map"
	case stage == selectionStagePt2 && !haveResult:
		label = "Computing elevation profile…"
	default:
		label = "Selection: 2/2 — profile computed"
	}
	return
}

func handleClick(st *elevationProfileState, lat float64, lon float64) {
	switch st.stage {
	case selectionStageNone:
		st.pt1Lat, st.pt1Lon = lat, lon
		st.stage = selectionStagePt1
	case selectionStagePt1:
		st.pt2Lat, st.pt2Lon = lat, lon
		st.stage = selectionStagePt2
		armRecenter(st)
		launchProfileWorker(st)
	}
}

func resetSelection(st *elevationProfileState) {
	st.mu.Lock()
	st.epoch++ // late-arriving worker writes from before the bump are dropped
	prev := st.cancelFn
	st.cancelFn = nil
	st.result = nil
	st.mu.Unlock()
	if prev != nil {
		prev()
	}
	st.stage = selectionStageNone
	st.plotEpoch++ // force egui_plot to allocate fresh widget state next render
}

func launchProfileWorker(st *elevationProfileState) {
	ctx, cancel := context.WithCancel(context.Background())
	st.mu.Lock()
	st.epoch++
	myEpoch := st.epoch
	prev := st.cancelFn
	st.cancelFn = cancel
	st.result = nil
	st.mu.Unlock()
	if prev != nil {
		prev()
	}

	fromLat, fromLon := st.pt1Lat, st.pt1Lon
	toLat, toLon := st.pt2Lat, st.pt2Lon

	go runProfileWorker(ctx, st, myEpoch, fromLat, fromLon, toLat, toLon)
}

func runProfileWorker(ctx context.Context, st *elevationProfileState, myEpoch uint64, fromLat float64, fromLon float64, toLat float64, toLon float64) {
	s, err := ensureSampler()
	if err != nil {
		return
	}
	if ctx.Err() != nil {
		return
	}
	fromLV := swisstopo.WGS84ToLV95(swisstopo.WGS84Coord{Lat: fromLat, Lon: fromLon})
	toLV := swisstopo.WGS84ToLV95(swisstopo.WGS84Coord{Lat: toLat, Lon: toLon})
	var dists []float64
	var elevs []float32
	dists, elevs, err = s.SampleProfile(fromLV, toLV, elevProfileSampleM)
	if err != nil {
		log.Error().Err(err).Float64("fromLat", fromLat).Float64("fromLon", fromLon).Float64("toLat", toLat).Float64("toLon", toLon).Msg("elevation-profile: profile sample failed")
		return
	}
	st.mu.Lock()
	if st.epoch == myEpoch && ctx.Err() == nil {
		st.result = &profileResult{
			distances:  dists,
			elevations: elevs,
			fromLV:     fromLV,
			toLV:       toLV,
		}
	}
	st.mu.Unlock()
}

func armRecenter(st *elevationProfileState) {
	midLat := (st.pt1Lat + st.pt2Lat) / 2
	midLon := (st.pt1Lon + st.pt2Lon) / 2
	distDeg := math.Sqrt(
		(st.pt2Lat-st.pt1Lat)*(st.pt2Lat-st.pt1Lat) +
			(st.pt2Lon-st.pt1Lon)*(st.pt2Lon-st.pt1Lon))
	switch {
	case distDeg < 0.05: // ~5 km diagonal at Swiss latitudes
		st.overrideZoom = 14
	case distDeg < 0.2: // ~20 km
		st.overrideZoom = 12
	default:
		st.overrideZoom = 10
	}
	st.overrideCenter = [2]float64{midLat, midLon}
	st.applyZoom = true
	st.applyCenter = true
}

func renderProfilePanel(ids *c.WidgetIdStack, st *elevationProfileState, res *profileResult) {
	c.Separator().Send()
	totalDist := res.distances[len(res.distances)-1]
	var minE, maxE float32 = math.MaxFloat32, -math.MaxFloat32
	for _, e := range res.elevations {
		if e < minE {
			minE = e
		}
		if e > maxE {
			maxE = e
		}
	}
	c.Label(fmt.Sprintf("Pt1: (%.6f, %.6f) → %s", st.pt1Lat, st.pt1Lon, res.fromLV.String())).Send()
	c.Label(fmt.Sprintf("Pt2: (%.6f, %.6f) → %s", st.pt2Lat, st.pt2Lon, res.toLV.String())).Send()
	c.Label(fmt.Sprintf("Distance: %.0f m  |  Min elev: %.0f m  |  Max elev: %.0f m",
		totalDist, minE, maxE)).Send()

	elevs64 := make([]float64, len(res.elevations))
	for i, e := range res.elevations {
		elevs64[i] = float64(e)
	}
	c.PlotLine("terrain elevation", res.distances, elevs64).
		Color(color.Hex(0x4488ffff)).Width(2.0).Send()
	c.PlotScatter("start", []float64{res.distances[0]},
		[]float64{float64(res.elevations[0])}).
		Color(color.Hex(0x44ff44ff)).Radius(5.0).Shape(1).Filled(true).Send()
	c.PlotScatter("end", []float64{res.distances[len(res.distances)-1]},
		[]float64{float64(res.elevations[len(res.elevations)-1])}).
		Color(color.Hex(0xff4444ff)).Radius(5.0).Shape(1).Filled(true).Send()
	c.Plot(ids.PrepareStr(fmt.Sprintf("profile-plot-%d", st.plotEpoch))).
		Width(elevMapStageW).Height(250).
		XAxisLabel("Distance along line (m)").YAxisLabel("Elevation (m a.s.l.)").
		Legend().AllowZoom(true).AllowDrag(true).Send()
}
