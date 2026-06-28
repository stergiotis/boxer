// Package terrainscope is a keelson app for swissALTI3D terrain
// line-of-sight analysis. Two clicks on a slippy map pick an observer and a
// target; the app interprets that sight line in polar coordinates about the
// observer and sweeps the bearing by ±range degrees, evaluating the terrain
// line-of-sight of every ray. The observer position is treated as uncertain:
// a Monte-Carlo ensemble jitters the sweep centre by a Gaussian and the app
// records, per bearing, the visibility probability and, per (bearing,
// distance), the terrain-elevation envelope (swisstopo.LineOfSightSweepEnsemble).
//
// All analysis parameters are live controls — editing a slider recomputes
// the ensemble (coalesced so at most one worker runs at a time). A half-range
// of 0 reduces the fan to a single ray; σ or samples of 0 reduces the
// ensemble to the nominal sweep.
//
// Ported from the former imzero2 "elevation-profile" demo scene (ADR-0099).
// Tile reading is a direct filesystem read of $SWISSTOPO_TILES_DIR for now —
// the ADR-0090-style headless elevation service (which would make this app a
// zero-fs-cap bus client) is deferred to Phase 4.
//
// Lifecycle: Mount captures the logger; Frame renders inside the host-owned
// window; Unmount cancels any in-flight sweep worker.
package terrainscope

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

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
	mapStageH        = float32(380)
	plotHeight       = float32(290)
	ecdfHeight       = float32(220) // distribution-pane plot height
	plotMargin       = float32(12)  // vertical breathing room around the plot
	markerIdPt1      = uint64(100)
	markerIdPt2      = uint64(200)

	// envelopeBandAlpha keeps the per-ray uncertainty polygons faint, so the
	// overlapping bands read as a haze rather than a wall of colour.
	envelopeBandAlpha = uint8(0x10)

	// sweepSeed fixes the Monte-Carlo draw so identical parameters yield an
	// identical ensemble — the band and probabilities don't flicker between
	// recomputes of the same selection.
	sweepSeed = uint64(0x7e44a1)

	// Default analysis parameters, editable via the controls panel.
	defaultObserverH    = 1.7 // eye height, metres above terrain
	defaultTargetH      = 0.0
	defaultSweepHalfDeg = 2.0
	defaultSweepStepDeg = 0.5
	defaultSigmaObsPos  = 10.0 // observer-position uncertainty (1σ, metres)
	defaultSigmaTgtPos  = 5.0  // target-position uncertainty (1σ, metres)
	defaultSigmaObsH    = 1.0  // observer-height uncertainty (1σ, metres)
	defaultSigmaTgtH    = 1.0  // target-height uncertainty (1σ, metres)
	defaultSamples      = 16.0
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

// ids is the package-level WidgetIdStack. Each frame's render wraps the body
// in c.IdScope(ids.PrepareSeq(inst.seed)) so two open windows produce
// disjoint Go-side widget ids even though the stack is shared.
var ids = c.NewWidgetIdStack()

// instanceCounter feeds per-instance seeds.
var instanceCounter atomic.Uint64

// Package-shared sampler, built lazily on first request. Shared across
// windows because it is read-only after construction and the tile-index scan
// is expensive; context.Background() because sampler init must outlive any
// individual window's worker context.
var (
	samplerOnce sync.Once
	sampler     *swisstopo.ElevationSampler
	samplerErr  error
)

// selectionStageE is the click-placement FSM. "Computing" vs "Done" is
// derived from result/worker state, not encoded here.
type selectionStageE uint8

const (
	selectionStageNone selectionStageE = iota
	selectionStagePt1
	selectionStagePt2
)

// Dock tab ids — stable across frames (they name entries in egui_dock's
// persistent layout state). The map and the two plots open as tabs in one
// leaf; the user can drag a tab to split them into side-by-side panes.
const (
	dockTabMap   = uint64(1)
	dockTabSweep = uint64(2)
	dockTabDist  = uint64(3)
)

// Hover-help for each control. Shown as a tooltip via c.HoverText.
const (
	tipObserverH = "Observer eye height above the terrain (metres) — the near end of every sight line (e.g. 1.7 for a standing person)."
	tipTargetH   = "Target height above the terrain (metres) — the height that must be visible at the far end."
	tipSweepHalf = "Half-range of the bearing fan (degrees): rays span ±this either side of the observer→target line."
	tipSweepStep = "Angular spacing between adjacent rays in the fan (degrees). Fewer degrees = denser fan = more rays."
	tipSigObsPos = "1σ Gaussian uncertainty of the observer's horizontal position (metres). 0 pins the observer."
	tipSigTgtPos = "1σ Gaussian uncertainty of the target's horizontal position (metres). 0 pins the target."
	tipSigObsH   = "1σ Gaussian uncertainty of the observer height (metres). 0 pins the height."
	tipSigTgtH   = "1σ Gaussian uncertainty of the target height (metres). 0 pins the height."
	tipSamples   = "Number of Monte-Carlo ensemble members drawn from the input distributions (0 = nominal sweep only)."
	tipSimTime   = "Wall-clock time of the last ensemble simulation (nominal sweep + all Monte-Carlo members)."
)

// sepStr separates inline metric chips in a summary row.
const sepStr = "  ·  "

// sweepParams is the comparable snapshot of every input that affects the
// result. The reactive recompute fires whenever it changes.
type sweepParams struct {
	fromLat     float64
	fromLon     float64
	toLat       float64
	toLon       float64
	observer    float64
	target      float64
	halfDeg     float64
	stepDeg     float64
	sigmaObsPos float64
	sigmaTgtPos float64
	sigmaObsH   float64
	sigmaTgtH   float64
	samples     int64
}

// sweepResult is the worker's output, published atomically under App.mu.
type sweepResult struct {
	ens        swisstopo.LOSEnsembleResult
	fromLV     swisstopo.LV95Coord
	toLV       swisstopo.LV95Coord
	computeDur time.Duration // wall-clock of the ensemble simulation
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

	// Analysis parameters, edited via the controls panel.
	observerH    float64
	targetH      float64
	sweepHalfDeg float64
	sweepStepDeg float64
	sigmaObsPos  float64
	sigmaTgtPos  float64
	sigmaObsH    float64
	sigmaTgtH    float64
	samples      float64

	// Reactive-recompute bookkeeping. lastLaunched is the params snapshot of
	// the in-flight/last worker; the frame loop relaunches (coalesced) when
	// the current params diverge.
	haveLaunched bool
	lastLaunched sweepParams

	// Sticky-once override flags for SetZoom / CenterAt — see
	// doc/skills/imzero2/SKILLS.md §16.3.
	overrideZoom   float64
	overrideCenter [2]float64
	applyZoom      bool
	applyCenter    bool

	// plotEpoch is folded into the plot widget id; bumping it on reset gives
	// egui_plot fresh widget state so its cached axis transform doesn't
	// dominate the next selection's auto-bounds fit.
	plotEpoch uint64

	// Worker state. mu serialises {epoch, cancelFn, inFlight, result} writes
	// against render-goroutine reads; epoch drops late results from a
	// superseded/cancelled compute.
	mu       sync.Mutex
	epoch    uint64
	cancelFn context.CancelFunc
	inFlight bool
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
		sigmaObsPos:  defaultSigmaObsPos,
		sigmaTgtPos:  defaultSigmaTgtPos,
		sigmaObsH:    defaultSigmaObsH,
		sigmaTgtH:    defaultSigmaTgtH,
		samples:      defaultSamples,
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.logger = ctx.Log()
	return
}

// Unmount cancels any in-flight worker so a closed window does not leave a
// goroutine sampling tiles into a result nobody reads.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.mu.Lock()
	cancel := inst.cancelFn
	inst.cancelFn = nil
	inst.inFlight = false
	inst.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return
}

// Frame renders the app body. Wrapped in IdScope(seed) so per-instance widget
// ids stay disjoint across multiple open windows.
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
		for dock := range c.DockArea(ids.PrepareStr("ts-dock")) {
			// Map + the two plots open as tabs in one leaf; drag a tab to
			// split them into side-by-side panes.
			dock.InitRoot(dockTabMap, dockTabSweep, dockTabDist)
			for range dock.Tab(dockTabMap, "Map") {
				inst.renderMap()
			}
			for range dock.Tab(dockTabSweep, "Sweep") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderSweepTab()
				}
			}
			for range dock.Tab(dockTabDist, "Distributions") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderDistTab()
				}
			}
		}
	}
	// Single launch site: react to control edits / a fresh selection after
	// the controls and click have been processed this frame.
	inst.maybeRecompute()
}

func (inst *App) renderControls() {
	inst.mu.Lock()
	inFlight := inst.inFlight
	var dur time.Duration
	var samples int
	if inst.result != nil {
		dur = inst.result.computeDur
		samples = inst.result.ens.Samples
	}
	inst.mu.Unlock()

	for range c.Horizontal().KeepIter() {
		c.Label(statusLabel(inst.stage, inFlight)).Send()
		if dur > 0 {
			c.AddSpace(plotMargin)
			metric(fmt.Sprintf("simulation: %s (%d samples)", fmtDur(dur), samples), tipSimTime)
		}
	}
	if samplerErr != nil {
		c.Label(fmt.Sprintf("tiles not available (%s=%q): %v", swissTilesDirEnv, tilesDir(), samplerErr)).Send()
	}

	for range c.Horizontal().KeepIter() {
		for range c.HoverText(tipObserverH).KeepIter() {
			_ = c.SliderF64(ids.PrepareStr("obsH"), inst.observerH, 0, 100).
				Text("observer").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.observerH)
		}
		for range c.HoverText(tipTargetH).KeepIter() {
			_ = c.SliderF64(ids.PrepareStr("tgtH"), inst.targetH, 0, 100).
				Text("target").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.targetH)
		}
	}
	for range c.Horizontal().KeepIter() {
		for range c.HoverText(tipSweepHalf).KeepIter() {
			_ = c.SliderF64(ids.PrepareStr("half"), inst.sweepHalfDeg, 0, 30).
				Text("sweep ±").Suffix("°").FixedDecimals(1).SendRespVal(&inst.sweepHalfDeg)
		}
		for range c.HoverText(tipSweepStep).KeepIter() {
			_ = c.SliderF64(ids.PrepareStr("step"), inst.sweepStepDeg, 0.1, 5).
				Text("step").Suffix("°").FixedDecimals(2).SendRespVal(&inst.sweepStepDeg)
		}
	}
	// Per-input uncertainty (1σ Gaussian); 0 pins that input. The bearing
	// fan stays deterministic.
	for range c.Horizontal().KeepIter() {
		for range c.HoverText(tipSigObsPos).KeepIter() {
			_ = c.SliderF64(ids.PrepareStr("sigObsPos"), inst.sigmaObsPos, 0, 50).
				Text("σ obs pos").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.sigmaObsPos)
		}
		for range c.HoverText(tipSigTgtPos).KeepIter() {
			_ = c.SliderF64(ids.PrepareStr("sigTgtPos"), inst.sigmaTgtPos, 0, 50).
				Text("σ tgt pos").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.sigmaTgtPos)
		}
	}
	for range c.Horizontal().KeepIter() {
		for range c.HoverText(tipSigObsH).KeepIter() {
			_ = c.SliderF64(ids.PrepareStr("sigObsH"), inst.sigmaObsH, 0, 30).
				Text("σ obs ht").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.sigmaObsH)
		}
		for range c.HoverText(tipSigTgtH).KeepIter() {
			_ = c.SliderF64(ids.PrepareStr("sigTgtH"), inst.sigmaTgtH, 0, 30).
				Text("σ tgt ht").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.sigmaTgtH)
		}
	}
	for range c.Horizontal().KeepIter() {
		for range c.HoverText(tipSamples).KeepIter() {
			_ = c.SliderF64(ids.PrepareStr("samples"), inst.samples, 0, 64).
				Text("samples").FixedDecimals(0).SendRespVal(&inst.samples)
		}
	}
	if c.Button(ids.PrepareStr("reset"), c.Atoms().Text(icons.IconClose+" Reset").Keep()).
		SendResp().HasPrimaryClicked() {
		inst.resetSelection()
	}
}

// maybeRecompute is the reactive, coalesced launch. While a worker is in
// flight it keeps frames flowing (so the result paints when ready) and never
// starts a second; once idle it (re)launches if the live params have diverged
// from the last launch — leading + trailing coalescing, at most one worker.
func (inst *App) maybeRecompute() {
	if inst.stage != selectionStagePt2 {
		return
	}
	inst.mu.Lock()
	inFlight := inst.inFlight
	inst.mu.Unlock()

	if inFlight {
		c.RequestRepaint()
		return
	}
	cur := inst.currentParams()
	if !inst.haveLaunched || cur != inst.lastLaunched {
		inst.launchSweepWorker(cur)
		inst.lastLaunched = cur
		inst.haveLaunched = true
		c.RequestRepaint()
	}
}

func (inst *App) currentParams() (p sweepParams) {
	return sweepParams{
		fromLat:     inst.pt1Lat,
		fromLon:     inst.pt1Lon,
		toLat:       inst.pt2Lat,
		toLon:       inst.pt2Lon,
		observer:    inst.observerH,
		target:      inst.targetH,
		halfDeg:     inst.sweepHalfDeg,
		stepDeg:     inst.sweepStepDeg,
		sigmaObsPos: inst.sigmaObsPos,
		sigmaTgtPos: inst.sigmaTgtPos,
		sigmaObsH:   inst.sigmaObsH,
		sigmaTgtH:   inst.sigmaTgtH,
		samples:     int64(inst.samples + 0.5),
	}
}

func (inst *App) renderMap() {
	inst.mu.Lock()
	res := inst.result
	inst.mu.Unlock()

	sm := c.CurrentApplicationState.StateManager

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
	if res != nil {
		inst.renderFanOverlay(res)
	}

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

// renderFanOverlay draws the swept rays as map polylines from the observer to
// each ray's nominal rotated target, coloured by visibility probability
// (green = clear from every sampled centre, red = blocked from all). The
// centre ray is drawn strong.
func (inst *App) renderFanOverlay(res *sweepResult) {
	tgts := res.ens.Nominal.Targets
	for i := range tgts {
		tgtWGS := swisstopo.LV95ToWGS84(tgts[i])
		isCenter := math.Abs(res.ens.AngleDeg[i]) < 1e-9
		col := visProbColor(res.ens.VisProb[i])
		width := styletokens.StrokeHair
		if isCenter {
			width = styletokens.StrokeStrong
		}
		c.MapPolyline(
			[]float64{inst.pt1Lat, tgtWGS.Lat},
			[]float64{inst.pt1Lon, tgtWGS.Lon},
		).Stroke(col, width).Send()
	}
}

func (inst *App) renderSweepTab() {
	inst.mu.Lock()
	res := inst.result
	inst.mu.Unlock()
	if res != nil && len(res.ens.Nominal.Rays) > 0 {
		inst.renderSweepPanel(res)
		return
	}
	inst.renderPending()
}

func (inst *App) renderDistTab() {
	inst.mu.Lock()
	res := inst.result
	inst.mu.Unlock()
	if res != nil && len(res.ens.Nominal.Rays) > 0 {
		inst.renderDistPane(res)
		return
	}
	inst.renderPending()
}

// renderPending is the placeholder the plot tabs show before a sweep exists.
func (inst *App) renderPending() {
	switch inst.stage {
	case selectionStageNone:
		c.Label("Click the observer point on the Map tab.").Send()
	case selectionStagePt1:
		c.Label("Click the target point on the Map tab to run the sweep.").Send()
	default:
		c.Label("Computing line-of-sight sweep ensemble…").Send()
	}
}

func (inst *App) renderSweepPanel(res *sweepResult) {
	c.Separator().Send()

	ens := res.ens
	rays := ens.Nominal.Rays
	centerIdx := centerRayIndex(ens.AngleDeg)
	center := rays[centerIdx]
	totalDist := center.ProfileDist[len(center.ProfileDist)-1]

	visible := 0
	meanProb := 0.0
	for i, ray := range rays {
		if ray.Visible {
			visible++
		}
		meanProb += ens.VisProb[i]
	}
	if len(rays) > 0 {
		meanProb /= float64(len(rays))
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

	for range c.Horizontal().KeepIter() {
		metric(fmt.Sprintf("observer %s (+%.1f m)", res.fromLV.String(), inst.observerH),
			"Observer position (LV95 grid E/N) and eye height above terrain — the near end of every sight line.")
		c.Label("  →  ").Send()
		metric(fmt.Sprintf("target %s (+%.1f m)", res.toLV.String(), inst.targetH),
			"Target position (LV95 grid E/N) and height above terrain — the far end of the nominal bearing.")
	}
	for range c.Horizontal().KeepIter() {
		metric(fmt.Sprintf("range %.0f m", totalDist),
			"Observer→target ground distance (m). Every swept ray spans this same range.")
		c.Label(sepStr).Send()
		metric(fmt.Sprintf("rays %d", len(rays)),
			fmt.Sprintf("Bearings in the deterministic fan: ±%.1f° at %.2f° spacing.", inst.sweepHalfDeg, inst.sweepStepDeg))
		c.Label(sepStr).Send()
		metric(fmt.Sprintf("%d samples", ens.Samples), tipSamples)
		c.Label(sepStr).Send()
		metric(fmt.Sprintf("σ pos %.0f/%.0f m", ens.Spec.SigmaObsPosM, ens.Spec.SigmaTgtPosM),
			"1σ horizontal position uncertainty, observer / target (m).")
		c.Label(sepStr).Send()
		metric(fmt.Sprintf("σ ht %.0f/%.0f m", ens.Spec.SigmaObsHeightM, ens.Spec.SigmaTgtHeightM),
			"1σ height uncertainty, observer / target (m).")
	}
	for range c.Horizontal().KeepIter() {
		metric(fmt.Sprintf("visible %d / obstructed %d", visible, len(rays)-visible),
			"Nominal (un-jittered) line-of-sight verdict per ray: clear vs blocked by terrain.")
		c.Label(sepStr).Send()
		metric(fmt.Sprintf("mean vis %.0f%%", meanProb*100),
			"Mean over all rays of the fraction of ensemble samples with a clear sight line — overall robustness to input uncertainty.")
		c.Label(sepStr).Send()
		metric(fmt.Sprintf("centre %.0f%%", ens.VisProb[centerIdx]*100),
			"Visibility probability of the centre ray (the observer→target bearing) across the ensemble.")
		c.Label(sepStr).Send()
		metric(fmt.Sprintf("compute %s", fmtDur(res.computeDur)), tipSimTime)
	}

	// Elevation envelope bands behind the profiles (only meaningful with a
	// non-degenerate ensemble). All bands share one legend entry to keep it
	// readable; each is a closed max-forward / min-backward polygon.
	if ens.Samples > 0 && len(ens.Distance) > 1 {
		for j := range rays {
			xs, ys := envelopePolygon(ens.Distance, ens.TerrainMax[j], ens.TerrainMin[j])
			c.PlotPolygon("± uncertainty", xs, ys, rayFillRGBA(j, len(rays), envelopeBandAlpha), 0x00000000, 0).Send()
		}
	}

	// Nominal terrain profiles (bearing-ramp coloured, centre highlighted),
	// the centre sight-line, then the obstruction scatter — drawn last so the
	// markers sit on top.
	for i, ray := range rays {
		isCenter := i == centerIdx
		col := rayColorAt(i, len(rays))
		width := float32(1.0)
		if isCenter {
			col = color.Hex(0xffee44ff)
			width = 2.5
		}
		name := fmt.Sprintf("%+.1f°", ens.AngleDeg[i])
		if ens.Samples > 0 {
			name = fmt.Sprintf("%+.1f° %.0f%%", ens.AngleDeg[i], ens.VisProb[i]*100)
		}
		c.PlotLine(name, ray.ProfileDist, f32sToF64(ray.ProfileElev)).
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

	// Margin around the plot so a legend taller than the plot body (many
	// rays → many entries) has room to overflow without colliding with the
	// labels above or the panel edge below.
	c.AddSpace(plotMargin)
	c.Plot(ids.PrepareStr(fmt.Sprintf("sweep-plot-%d", inst.plotEpoch))).
		Width(mapStageW).Height(plotHeight).
		XAxisLabel("Distance along ray (m)").YAxisLabel("Elevation (m a.s.l.)").
		Legend().AllowZoom(true).AllowDrag(true).AllowScroll(false).Send()
	c.AddSpace(plotMargin)
}

// renderDistPane shows the empirical CDF of every randomised input variable —
// the realised draws (deviation from nominal, metres) of each Gaussian we
// sampled. One step-line per variable on a shared [0,1] axis so the different
// distributions read side by side.
func (inst *App) renderDistPane(res *sweepResult) {
	ens := res.ens
	c.Separator().Send()
	if len(ens.Inputs) == 0 {
		c.Label("Input distributions — raise a σ above to randomise observer/target position or height.").Send()
		return
	}
	metric(fmt.Sprintf("Input distributions — empirical CDF of %d samples (deviation from nominal)", ens.Samples),
		"Each randomised input is drawn from its own Gaussian; this is the empirical CDF (fraction of samples ≤ x) of the realised "+
			"deviations from the nominal value, in metres. Position deviations are radial offsets (≥0); height deviations are signed.")
	for i, in := range ens.Inputs {
		xs, ys := ecdfStep(in.Dev)
		c.PlotLine(in.Name, xs, ys).Color(distColorAt(i)).Width(1.8).Send()
	}
	c.AddSpace(plotMargin)
	c.Plot(ids.PrepareStr(fmt.Sprintf("dist-plot-%d", inst.plotEpoch))).
		Width(mapStageW).Height(ecdfHeight).
		XAxisLabel("deviation from nominal (m)").YAxisLabel("cumulative probability").
		Legend().AllowZoom(true).AllowDrag(true).AllowScroll(false).
		IncludeY(0).IncludeY(1).Send()
	c.AddSpace(plotMargin)
}

// ecdfStep returns the staircase (xs, ys) of the empirical CDF of vals: a copy
// is sorted, and F jumps by 1/n at each sample. Connecting the points with
// straight segments yields the conventional step plot.
func ecdfStep(vals []float64) (xs []float64, ys []float64) {
	n := len(vals)
	if n == 0 {
		return
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	xs = make([]float64, 0, 2*n)
	ys = make([]float64, 0, 2*n)
	for i, x := range s {
		xs = append(xs, x)
		ys = append(ys, float64(i)/float64(n))
		xs = append(xs, x)
		ys = append(ys, float64(i+1)/float64(n))
	}
	return
}

// metric renders a label carrying a hover tooltip — used for each measured /
// displayed quantity so the reader can learn what every number means.
func metric(text string, tip string) {
	for range c.HoverText(tip).KeepIter() {
		c.Label(text).Send()
	}
}

// distColorAt is a small categorical palette for the per-variable ECDF lines.
func distColorAt(i int) (col color.Color) {
	palette := []uint32{0x4488ffff, 0xff8844ff, 0x44cc66ff, 0xcc55ccff, 0xddcc44ff}
	return color.Hex(palette[i%len(palette)])
}

func statusLabel(stage selectionStageE, computing bool) (label string) {
	switch {
	case stage == selectionStageNone:
		label = "Selection: 0/2 — click the observer point on the map"
	case stage == selectionStagePt1:
		label = "Selection: 1/2 — click the target point on the map"
	case stage == selectionStagePt2 && computing:
		label = "Computing line-of-sight sweep ensemble…"
	default:
		label = "Selection: 2/2 — drag the sliders to re-run live"
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
		// The reactive loop (maybeRecompute) launches the first sweep this
		// frame now that the stage is Pt2.
	}
}

func (inst *App) resetSelection() {
	inst.mu.Lock()
	inst.epoch++ // late worker writes from before the bump are dropped
	prev := inst.cancelFn
	inst.cancelFn = nil
	inst.inFlight = false
	inst.result = nil
	inst.mu.Unlock()
	if prev != nil {
		prev()
	}
	inst.stage = selectionStageNone
	inst.haveLaunched = false
	inst.plotEpoch++ // fresh egui_plot widget state next render
}

func (inst *App) launchSweepWorker(p sweepParams) {
	ctx, cancel := context.WithCancel(context.Background())
	inst.mu.Lock()
	inst.epoch++
	myEpoch := inst.epoch
	prev := inst.cancelFn
	inst.cancelFn = cancel
	inst.inFlight = true
	inst.result = nil
	inst.mu.Unlock()
	if prev != nil {
		prev()
	}
	go inst.runSweepWorker(ctx, myEpoch, p)
}

func (inst *App) runSweepWorker(ctx context.Context, myEpoch uint64, p sweepParams) {
	s, err := ensureSampler()
	if err != nil {
		inst.finishWorker(myEpoch, nil)
		return
	}
	if ctx.Err() != nil {
		inst.finishWorker(myEpoch, nil)
		return
	}
	fromLV := swisstopo.WGS84ToLV95(swisstopo.WGS84Coord{Lat: p.fromLat, Lon: p.fromLon})
	toLV := swisstopo.WGS84ToLV95(swisstopo.WGS84Coord{Lat: p.toLat, Lon: p.toLon})

	spec := swisstopo.EnsembleSpec{
		HalfRangeDeg:    p.halfDeg,
		StepDeg:         p.stepDeg,
		Samples:         int(p.samples),
		Seed:            sweepSeed,
		SigmaObsPosM:    p.sigmaObsPos,
		SigmaTgtPosM:    p.sigmaTgtPos,
		SigmaObsHeightM: p.sigmaObsH,
		SigmaTgtHeightM: p.sigmaTgtH,
	}
	t0 := time.Now()
	ens, err := s.LineOfSightSweepEnsemble(fromLV, p.observer, toLV, p.target, spec)
	computeDur := time.Since(t0)
	if err != nil {
		inst.logger.Error().Err(err).
			Float64("fromLat", p.fromLat).Float64("fromLon", p.fromLon).
			Float64("toLat", p.toLat).Float64("toLon", p.toLon).
			Int64("samples", p.samples).
			Msg("terrainscope: line-of-sight sweep ensemble failed")
		inst.finishWorker(myEpoch, nil)
		return
	}
	inst.finishWorker(myEpoch, &sweepResult{ens: ens, fromLV: fromLV, toLV: toLV, computeDur: computeDur})
}

// fmtDur renders a simulation wall-clock as milliseconds (microsecond
// resolution), or an em dash when unmeasured.
func fmtDur(d time.Duration) (s string) {
	if d <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f ms", float64(d.Microseconds())/1000.0)
}

// finishWorker publishes the result and clears the in-flight flag, but only
// when this worker is still the current one (epoch match) — a superseded or
// reset worker drops its result and leaves the live worker's flag alone.
func (inst *App) finishWorker(myEpoch uint64, res *sweepResult) {
	inst.mu.Lock()
	if inst.epoch == myEpoch {
		inst.inFlight = false
		if res != nil {
			inst.result = res
		}
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
// observer→target bearing) — the nearest-to-zero offset, robust to any future
// asymmetry.
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

// envelopePolygon returns the closed (xs, ys) ring of an elevation band:
// max curve left→right, then min curve right→left.
func envelopePolygon(dist []float64, hi []float32, lo []float32) (xs []float64, ys []float64) {
	n := len(dist)
	xs = make([]float64, 0, 2*n)
	ys = make([]float64, 0, 2*n)
	for k := range n {
		xs = append(xs, dist[k])
		ys = append(ys, float64(hi[k]))
	}
	for k := n - 1; k >= 0; k-- {
		xs = append(xs, dist[k])
		ys = append(ys, float64(lo[k]))
	}
	return
}

// rayColorAt returns a blue→red ramp colour for ray i of n (the profile-line
// colour, so bearings stay distinguishable).
func rayColorAt(i int, n int) (col color.Color) {
	if n <= 1 {
		return color.Hex(0xffee44ff)
	}
	frac := float64(i) / float64(n-1)
	r := lerpByte(0x33, 0xff, frac)
	b := lerpByte(0xff, 0x33, frac)
	return color.RGBA(r, 0x55, b, 0xcc)
}

// rayFillRGBA is rayColorAt as a packed 0xRRGGBBAA with an explicit alpha,
// for the translucent envelope polygons.
func rayFillRGBA(i int, n int, alpha uint8) (rgba uint32) {
	var r, b uint8 = 0xee, 0x44
	if n > 1 {
		frac := float64(i) / float64(n-1)
		r = lerpByte(0x33, 0xff, frac)
		b = lerpByte(0xff, 0x33, frac)
	}
	return uint32(r)<<24 | uint32(0x55)<<16 | uint32(b)<<8 | uint32(alpha)
}

// visProbColor maps a visibility probability in [0,1] to a red→amber→green
// ramp for the map fan (0 = blocked from every sampled centre, 1 = clear).
func visProbColor(p float64) (col color.Color) {
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	r := lerpByte(0xdd, 0x33, p)
	g := lerpByte(0x33, 0xdd, p)
	return color.RGBA(r, g, 0x44, 0xee)
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
