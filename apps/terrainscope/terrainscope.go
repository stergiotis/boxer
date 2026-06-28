// Package terrainscope is a keelson app for swissALTI3D terrain
// line-of-sight analysis. Two clicks on a slippy map pick an observer and
// a target; the app then interprets that sight line in polar coordinates
// about the observer and sweeps the bearing by ±range degrees, evaluating
// the terrain line-of-sight of every ray and recording all the resulting
// height profiles (swisstopo.LineOfSightSweep). A half-range of 0 reduces
// the fan to the single observer→target ray.
//
// Ported from the former imzero2 "elevation-profile" demo scene
// (ADR-0099). Tile reading is a direct filesystem read of
// $SWISSTOPO_TILES_DIR for now — the ADR-0090-style headless elevation
// service (which would make this app a zero-fs-cap bus client) is deferred
// to Phase 4.
//
// Lifecycle: Mount captures the logger; Frame renders inside the
// host-owned window; Unmount cancels any in-flight sweep worker.
package terrainscope

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/science/geo/swisstopo"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

const (
	swissCenterLat   = 46.8182
	swissCenterLon   = 8.2275
	swissTilesDirEnv = "SWISSTOPO_TILES_DIR"
	mapStageW        = float32(900)
	mapStageH        = float32(440)
	plotHeight       = float32(260)
	markerIdPt1      = uint64(100)
	markerIdPt2      = uint64(200)

	// Default analysis parameters, editable via the controls panel.
	defaultObserverH    = 1.7 // eye height, metres above terrain
	defaultTargetH      = 0.0
	defaultSweepHalfDeg = 2.0
	defaultSweepStepDeg = 0.5
)

// SwissTilesDir is the swissALTI3D tile directory consumed by the app.
// Defaults to ~/data/swisstopo (home-expanded by the env layer) when
// $SWISSTOPO_TILES_DIR is unset. Moved here from the demo package by
// ADR-0099.
var SwissTilesDir = env.NewPath(env.Spec{
	Name:        swissTilesDirEnv,
	Description: "directory containing swissALTI3D 2m COG tiles for the terrainscope app",
	Category:    env.CategoryE("swisstopo"),
	Default:     "~/data/swisstopo",
})

// ids is the package-level WidgetIdStack. Each frame's render wraps the
// body in c.IdScope(ids.PrepareSeq(inst.seed)) so two open windows
// produce disjoint Go-side widget ids even though the stack is shared.
var ids = c.NewWidgetIdStack()

// instanceCounter feeds per-instance seeds. Every newApp() increments
// and the post-increment value is the App's stable salt for the
// lifetime of that window.
var instanceCounter atomic.Uint64

// Package-shared sampler, built lazily on first request. Shared across
// windows because it is read-only after construction and the tile-index
// scan is expensive; context.Background() because sampler init must
// outlive any individual window's worker context.
var (
	samplerOnce sync.Once
	sampler     *swisstopo.ElevationSampler
	samplerErr  error
)

// selectionStageE is the click-placement FSM. "Computing" vs "Done" is
// derived from result presence, not encoded here — keeps the FSM owned
// exclusively by the render goroutine.
type selectionStageE uint8

const (
	selectionStageNone selectionStageE = iota
	selectionStagePt1
	selectionStagePt2
)

// sweepResult is the worker's output, published atomically under App.mu
// so reset/republish never races with a render read.
type sweepResult struct {
	sweep  swisstopo.LOSSweepResult
	fromLV swisstopo.LV95Coord
	toLV   swisstopo.LV95Coord
}

// App is the per-window terrainscope instance.
type App struct {
	seed   uint64
	logger zerolog.Logger

	stage  selectionStageE
	pt1Lat float64
	pt1Lon float64
	pt2Lat float64
	pt2Lon float64

	// Analysis parameters, edited via the controls panel and snapshotted
	// into each worker launch.
	observerH    float64
	targetH      float64
	sweepHalfDeg float64
	sweepStepDeg float64

	// Sticky-once override flags for SetZoom / CenterAt — see
	// doc/skills/imzero2/SKILLS.md §16.3. Flipped on the discrete event
	// that should retarget the view; cleared after one frame so the user
	// can pan/zoom freely afterwards.
	overrideZoom   float64
	overrideCenter [2]float64
	applyZoom      bool
	applyCenter    bool

	// plotEpoch is folded into the profile-plot widget id; bumping it on
	// reset gives egui_plot fresh widget state so its cached axis
	// transform from the previous selection doesn't dominate the new
	// data's auto-bounds fit.
	plotEpoch uint64

	// Worker state. mu serialises {epoch, cancelFn, result} writes against
	// reads in the render goroutine; epoch ensures late-arriving worker
	// results from a cancelled compute are dropped instead of overwriting
	// post-reset state.
	mu       sync.Mutex
	epoch    uint64
	cancelFn context.CancelFunc
	result   *sweepResult
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		seed:         instanceCounter.Add(1),
		logger:       log.Logger,
		observerH:    defaultObserverH,
		targetH:      defaultTargetH,
		sweepHalfDeg: defaultSweepHalfDeg,
		sweepStepDeg: defaultSweepStepDeg,
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.logger = ctx.Log()
	return
}

// Unmount cancels any in-flight sweep worker so a closed window does not
// leave a goroutine sampling tiles into a result nobody reads.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.mu.Lock()
	cancel := inst.cancelFn
	inst.cancelFn = nil
	inst.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return
}

// Frame renders the app body. Wrapped in IdScope(seed) so per-instance
// widget ids stay disjoint across multiple open windows.
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	ids.Reset()
	for range c.IdScope(ids.PrepareSeq(inst.seed)) {
		inst.renderBody()
	}
	return
}

func (inst *App) renderBody() {
	for range c.PanelTopInside(ids.PrepareStr("controls")).Resizable(false).KeepIter() {
		inst.renderControls()
	}
	for range c.PanelCentralInside().KeepIter() {
		inst.renderMap()
		inst.renderResult()
	}
}

func (inst *App) renderControls() {
	inst.mu.Lock()
	haveResult := inst.result != nil
	inst.mu.Unlock()

	c.Label(statusLabel(inst.stage, haveResult)).Send()
	if samplerErr != nil {
		c.Label(fmt.Sprintf("tiles not available (%s=%q): %v", swissTilesDirEnv, tilesDir(), samplerErr)).Send()
	}

	for range c.Horizontal().KeepIter() {
		_ = c.SliderF64(ids.PrepareStr("obsH"), inst.observerH, 0, 100).
			Text("observer").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.observerH)
		_ = c.SliderF64(ids.PrepareStr("tgtH"), inst.targetH, 0, 100).
			Text("target").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.targetH)
	}
	for range c.Horizontal().KeepIter() {
		_ = c.SliderF64(ids.PrepareStr("half"), inst.sweepHalfDeg, 0, 30).
			Text("sweep ±").Suffix("°").FixedDecimals(1).SendRespVal(&inst.sweepHalfDeg)
		_ = c.SliderF64(ids.PrepareStr("step"), inst.sweepStepDeg, 0.1, 5).
			Text("step").Suffix("°").FixedDecimals(2).SendRespVal(&inst.sweepStepDeg)
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("reset"), c.Atoms().Text(icons.IconClose+" Reset").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.resetSelection()
		}
		// Once both points are placed, the analysis parameters can be
		// re-applied without re-picking. Recompute is the explicit apply:
		// slider edits update the params but do not spawn a worker until
		// clicked, so dragging a slider does not flood the tile sampler.
		if inst.stage == selectionStagePt2 {
			if c.Button(ids.PrepareStr("recompute"), c.Atoms().Text(icons.IconPlay+" Recompute").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.launchSweepWorker()
			}
		}
	}
}

func (inst *App) renderMap() {
	inst.mu.Lock()
	res := inst.result
	inst.mu.Unlock()

	sm := c.CurrentApplicationState.StateManager

	// Capture our own WalkersMap widget id so we can ignore clicks on
	// sibling walkers maps. PrepareStr + Derive consumes the stack frame;
	// we re-prepare immediately before the WalkersMap.Send().
	ids.PrepareStr("ts-map")
	mapId := ids.Derive()

	cam := sm.GetWalkersCamera()
	if cam.Found && cam.Clicked && cam.HoverValid && cam.MapId == mapId {
		inst.handleClick(cam.HoverLat, cam.HoverLon)
	}

	// Overlays — must be emitted BEFORE the WalkersMap opcode so they drain
	// into this frame's map render. See SKILLS.md §16.2.
	if inst.stage >= selectionStagePt1 {
		c.MapMarker(markerIdPt1, inst.pt1Lat, inst.pt1Lon).
			Label(fmt.Sprintf("observer (%.5f, %.5f)", inst.pt1Lat, inst.pt1Lon)).
			Color(color.Hex(0x44ff44ff)).Radius(8).Send()
	}
	if inst.stage >= selectionStagePt2 {
		c.MapMarker(markerIdPt2, inst.pt2Lat, inst.pt2Lon).
			Label(fmt.Sprintf("target (%.5f, %.5f)", inst.pt2Lat, inst.pt2Lon)).
			Color(color.Hex(0xff4444ff)).Radius(8).Send()
	}
	// The swept fan: one polyline per ray, observer → rotated target.
	if res != nil {
		inst.renderFanOverlay(res)
	}

	// Map. Sticky overrides are applied via the apply-once flag pattern
	// (SKILLS.md §16.3); unconditional per-frame SetZoom/CenterAt would
	// lock the user out of pan/zoom after the second click.
	ids.PrepareStr("ts-map")
	mw := c.WalkersMap(ids, swissCenterLat, swissCenterLon, false).
		Width(mapStageW).Height(mapStageH)
	if inst.applyZoom {
		mw = mw.SetZoom(inst.overrideZoom)
		inst.applyZoom = false
	}
	if inst.applyCenter {
		mw = mw.CenterAt(inst.overrideCenter[0], inst.overrideCenter[1])
		inst.applyCenter = false
	}
	mw.Send()
}

// renderFanOverlay draws the swept rays as map polylines from the observer
// to each ray's rotated target. The centre ray (offset 0) is drawn strong;
// the rest are hairlines coloured along a blue→red ramp by bearing.
func (inst *App) renderFanOverlay(res *sweepResult) {
	n := len(res.sweep.Targets)
	for i := range n {
		tgtWGS := swisstopo.LV95ToWGS84(res.sweep.Targets[i])
		isCenter := math.Abs(res.sweep.AngleDeg[i]) < 1e-9
		col := rayColorAt(i, n)
		width := styletokens.StrokeHair
		if isCenter {
			col = color.Hex(0xffee44ff)
			width = styletokens.StrokeStrong
		}
		c.MapPolyline(
			[]float64{inst.pt1Lat, tgtWGS.Lat},
			[]float64{inst.pt1Lon, tgtWGS.Lon},
		).Stroke(col, width).Send()
	}
}

func (inst *App) renderResult() {
	inst.mu.Lock()
	res := inst.result
	inst.mu.Unlock()

	switch {
	case inst.stage == selectionStagePt2 && res != nil && len(res.sweep.Rays) > 0:
		inst.renderSweepPanel(res)
	case inst.stage == selectionStagePt2 && res == nil:
		c.Label("Computing line-of-sight sweep… this may take a moment.").Send()
	case inst.stage == selectionStagePt1:
		c.Label(fmt.Sprintf("Observer: (%.6f, %.6f)", inst.pt1Lat, inst.pt1Lon)).Send()
		c.Label("Click a second point to set the target bearing, then the sweep runs.").Send()
	}
}

func (inst *App) renderSweepPanel(res *sweepResult) {
	c.Separator().Send()

	rays := res.sweep.Rays
	centerIdx := centerRayIndex(res.sweep.AngleDeg)
	center := rays[centerIdx]
	totalDist := center.ProfileDist[len(center.ProfileDist)-1]

	visible := 0
	for _, ray := range rays {
		if ray.Visible {
			visible++
		}
	}

	var minE, maxE float32 = math.MaxFloat32, -math.MaxFloat32
	for _, e := range center.ProfileElev {
		if e < minE {
			minE = e
		}
		if e > maxE {
			maxE = e
		}
	}

	c.Label(fmt.Sprintf("Observer %s (+%.1f m)  →  target %s (+%.1f m)",
		res.fromLV.String(), inst.observerH, res.toLV.String(), inst.targetH)).Send()
	c.Label(fmt.Sprintf("Range: %.0f m  |  rays: %d (±%.1f° @ %.2f°)  |  visible: %d  |  obstructed: %d",
		totalDist, len(rays), inst.sweepHalfDeg, inst.sweepStepDeg, visible, len(rays)-visible)).Send()
	c.Label(fmt.Sprintf("Centre ray min elev: %.0f m  |  max elev: %.0f m", minE, maxE)).Send()

	// Overlay every ray's terrain profile (the recorded height profiles),
	// the centre sight-line, and a scatter of obstruction points.
	for i, ray := range rays {
		isCenter := i == centerIdx
		col := rayColorAt(i, len(rays))
		width := float32(1.0)
		if isCenter {
			col = color.Hex(0xffee44ff)
			width = 2.5
		}
		c.PlotLine(fmt.Sprintf("%+.1f°", res.sweep.AngleDeg[i]), ray.ProfileDist, f32sToF64(ray.ProfileElev)).
			Color(col).Width(width).Highlight(isCenter).Send()
	}
	c.PlotLine("sight line", center.ProfileDist, f32sToF64(center.LOSElev)).
		Color(color.Hex(0xff8800ff)).Width(1.5).Send()

	var obsX []float64
	var obsY []float64
	for _, ray := range rays {
		if !ray.Visible {
			obsX = append(obsX, ray.ObstructionDist)
			obsY = append(obsY, float64(ray.ObstructionElev))
		}
	}
	if len(obsX) > 0 {
		c.PlotScatter("obstructions", obsX, obsY).
			Color(color.Hex(0xff2222ff)).Radius(4.0).Shape(2).Filled(true).Send()
	}

	c.Plot(ids.PrepareStr(fmt.Sprintf("sweep-plot-%d", inst.plotEpoch))).
		Width(mapStageW).Height(plotHeight).
		XAxisLabel("Distance along ray (m)").YAxisLabel("Elevation (m a.s.l.)").
		Legend().AllowZoom(true).AllowDrag(true).Send()
}

func statusLabel(stage selectionStageE, haveResult bool) (label string) {
	switch {
	case stage == selectionStageNone:
		label = "Selection: 0/2 — click the observer point on the map"
	case stage == selectionStagePt1:
		label = "Selection: 1/2 — click the target point on the map"
	case stage == selectionStagePt2 && !haveResult:
		label = "Computing line-of-sight sweep…"
	default:
		label = "Selection: 2/2 — sweep computed (adjust sliders + Recompute)"
	}
	return
}

func (inst *App) handleClick(lat float64, lon float64) {
	switch inst.stage {
	case selectionStageNone:
		inst.pt1Lat, inst.pt1Lon = lat, lon
		inst.stage = selectionStagePt1
	case selectionStagePt1:
		inst.pt2Lat, inst.pt2Lon = lat, lon
		inst.stage = selectionStagePt2
		inst.armRecenter()
		inst.launchSweepWorker()
	}
}

func (inst *App) resetSelection() {
	inst.mu.Lock()
	inst.epoch++ // late-arriving worker writes from before the bump are dropped
	prev := inst.cancelFn
	inst.cancelFn = nil
	inst.result = nil
	inst.mu.Unlock()
	if prev != nil {
		prev()
	}
	inst.stage = selectionStageNone
	inst.plotEpoch++ // force egui_plot to allocate fresh widget state next render
}

func (inst *App) launchSweepWorker() {
	ctx, cancel := context.WithCancel(context.Background())
	inst.mu.Lock()
	inst.epoch++
	myEpoch := inst.epoch
	prev := inst.cancelFn
	inst.cancelFn = cancel
	inst.result = nil
	inst.mu.Unlock()
	if prev != nil {
		prev()
	}

	params := sweepParams{
		fromLat:  inst.pt1Lat,
		fromLon:  inst.pt1Lon,
		toLat:    inst.pt2Lat,
		toLon:    inst.pt2Lon,
		observer: inst.observerH,
		target:   inst.targetH,
		halfDeg:  inst.sweepHalfDeg,
		stepDeg:  inst.sweepStepDeg,
	}
	go inst.runSweepWorker(ctx, myEpoch, params)
}

type sweepParams struct {
	fromLat  float64
	fromLon  float64
	toLat    float64
	toLon    float64
	observer float64
	target   float64
	halfDeg  float64
	stepDeg  float64
}

func (inst *App) runSweepWorker(ctx context.Context, myEpoch uint64, p sweepParams) {
	s, err := ensureSampler()
	if err != nil {
		return
	}
	if ctx.Err() != nil {
		return
	}
	fromLV := swisstopo.WGS84ToLV95(swisstopo.WGS84Coord{Lat: p.fromLat, Lon: p.fromLon})
	toLV := swisstopo.WGS84ToLV95(swisstopo.WGS84Coord{Lat: p.toLat, Lon: p.toLon})

	sweep, err := s.LineOfSightSweep(fromLV, p.observer, toLV, p.target, p.halfDeg, p.stepDeg)
	if err != nil {
		inst.logger.Error().Err(err).
			Float64("fromLat", p.fromLat).Float64("fromLon", p.fromLon).
			Float64("toLat", p.toLat).Float64("toLon", p.toLon).
			Float64("halfDeg", p.halfDeg).Float64("stepDeg", p.stepDeg).
			Msg("terrainscope: line-of-sight sweep failed")
		return
	}

	inst.mu.Lock()
	if inst.epoch == myEpoch && ctx.Err() == nil {
		inst.result = &sweepResult{sweep: sweep, fromLV: fromLV, toLV: toLV}
	}
	inst.mu.Unlock()
}

func (inst *App) armRecenter() {
	midLat := (inst.pt1Lat + inst.pt2Lat) / 2
	midLon := (inst.pt1Lon + inst.pt2Lon) / 2
	distDeg := math.Sqrt(
		(inst.pt2Lat-inst.pt1Lat)*(inst.pt2Lat-inst.pt1Lat) +
			(inst.pt2Lon-inst.pt1Lon)*(inst.pt2Lon-inst.pt1Lon))
	switch {
	case distDeg < 0.05: // ~5 km diagonal at Swiss latitudes
		inst.overrideZoom = 14
	case distDeg < 0.2: // ~20 km
		inst.overrideZoom = 12
	default:
		inst.overrideZoom = 10
	}
	inst.overrideCenter = [2]float64{midLat, midLon}
	inst.applyZoom = true
	inst.applyCenter = true
}

// centerRayIndex returns the index of the offset-0 ray (the original
// observer→target bearing). Offsets are symmetric and ascending, so this
// is the middle index, but searching for the zero crossing keeps it robust
// to any future asymmetry.
func centerRayIndex(angleDeg []float64) (idx int) {
	best := math.MaxFloat64
	for i, a := range angleDeg {
		if abs := math.Abs(a); abs < best {
			best = abs
			idx = i
		}
	}
	return
}

// rayColorAt returns a blue→red ramp colour for ray i of n, used for the
// non-centre rays in both the map fan and the profile overlay.
func rayColorAt(i int, n int) (col color.Color) {
	if n <= 1 {
		return color.Hex(0xffee44ff)
	}
	frac := float64(i) / float64(n-1) // 0 (most negative) … 1 (most positive)
	r := lerpByte(0x33, 0xff, frac)
	b := lerpByte(0xff, 0x33, frac)
	return color.RGBA(r, 0x55, b, 0xcc)
}

func lerpByte(lo float64, hi float64, frac float64) (out uint8) {
	v := lo + frac*(hi-lo)
	return uint8(min(255, max(0, int(v+0.5))))
}

func f32sToF64(in []float32) (out []float64) {
	out = make([]float64, len(in))
	for i, v := range in {
		out[i] = float64(v)
	}
	return
}

func tilesDir() (dir string) {
	return SwissTilesDir.Get()
}

func ensureSampler() (s *swisstopo.ElevationSampler, err error) {
	samplerOnce.Do(func() {
		dir := tilesDir()
		sampler, samplerErr = swisstopo.NewElevationSampler(context.Background(), dir)
		if samplerErr != nil {
			log.Error().Err(samplerErr).Str("dir", dir).Str("env", swissTilesDirEnv).Msg("terrainscope: sampler init failed")
			sampler = nil
		}
	})
	s = sampler
	err = samplerErr
	return
}
