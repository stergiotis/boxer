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
// Lifecycle: Mount captures the logger and the task api; Frame takes the
// sweep runner's result and renders inside the host-owned window; Unmount
// cancels any in-flight sweep.
package terrainscope

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/geo/swisstopo"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
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

	// rayLineAlpha and visFanAlpha are the opacities the ray lines and the map
	// fan carried before their hues moved onto the IDS ramp, kept as they were:
	// this change is about WHERE the colour comes from, not about how much of
	// the terrain underneath it should show through.
	rayLineAlpha = uint8(0xcc)
	visFanAlpha  = uint8(0xee)

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
	tipRetry     = "Run the sweep again with the same parameters. Changing any parameter also relaunches it."
	tipAdvanced  = "Target height, the angular spacing of the fan, and the 1σ uncertainty of each input. The defaults are a standing observer, a ground-level target and metre-scale position noise."
)

// sweepKind and sweepTitle name the keelson task each sweep runs as.
const (
	sweepKind  = "terrainscope.sweep"
	sweepTitle = "line-of-sight sweep"
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

// sweepResult is the sweep runner's output; the frame takes it once.
type sweepResult struct {
	ens        swisstopo.LOSEnsembleResult
	fromLV     swisstopo.LV95Coord
	toLV       swisstopo.LV95Coord
	computeDur time.Duration // wall-clock of the ensemble simulation
}

// App is the per-window terrainscope instance.
type App struct {
	// ids is the per-instance WidgetIdStack. The host pre-pushes a
	// window-unique salt onto it before every Frame() call (ADR-0026
	// §SD9, windowhost.renderWindowBody), so every widget id the app
	// derives is unique across all concurrently open instances. Captured
	// from ctx.Ids() in Mount; the app must NOT Reset() it.
	ids *c.WidgetIdStack

	logger zerolog.Logger
	// tasks runs each sweep as a keelson task owned by this window, so it
	// has a row and a cancel on the bus; nil on hosts without a runtime
	// (the tour stage), where the runner still works and only the row is
	// missing.
	tasks task.TaskApiI

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
	pm             *portolan.Map // the map widget, created on first renderMap
	tiles          *basemap.Tiles

	// plotEpoch is folded into the plot widget id; bumping it on reset gives
	// egui_plot fresh widget state so its cached axis transform doesn't
	// dominate the next selection's auto-bounds fit.
	plotEpoch uint64

	// sweep is the one-shot runner the ensemble computes on (W2): at most
	// one sweep in flight, a stale result dropped by token, cancel through
	// the row. result and sweepErr are the render goroutine's copies, taken
	// from the runner at the top of Frame; exactly one of them is meaningful
	// once a sweep has finished. sweepNote is the last outcome that was not
	// a result — a cancel — shown in the status row until the next launch.
	sweep     bgjob.Runner[sweepResult]
	result    *sweepResult
	sweepErr  error
	sweepNote string
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids:          c.NewWidgetIdStack(),
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
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.tiles = basemap.NewTiles(ctx.Bus(), "terrainscope: basemap")
	inst.tasks = task.ForApp(ctx)
	return
}

// Unmount cancels any in-flight sweep so a closed window does not leave a
// goroutine sampling tiles into a result nobody reads. The ensemble itself
// takes no context, so the cancel lands between the sampler's init and the
// sweep, or the goroutine runs the sweep out and drops the result.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.sweep.Invalidate()
	return
}

// Frame renders the app body. The host has already pre-pushed a window-unique
// salt onto inst.ids via c.IdScope (windowhost.renderWindowBody, ADR-0026
// §SD9), so widget ids derived from inst.ids are unique across all
// concurrently open instances — the app must not Reset() the stack or wrap the
// body in its own instance salt (doing so discards the host salt and collides
// with sibling apps that share a label string).
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.takeResults()
	inst.renderBody()
	return
}

// takeResults moves the runner's outcome onto the instance once, at the top
// of the frame, so every pane below reads one stable value (jackstay's
// shape). A failure is copied out and the runner reset to idle, so the
// error stays on the instance until a parameter change or Retry relaunches
// (F4: a sweep that failed says so; it never shows as computing forever).
func (inst *App) takeResults() {
	if res, _, ok := inst.sweep.TakeResult(); ok {
		inst.result = res
		inst.sweepErr = nil
		return
	}
	if snap := inst.sweep.Snapshot(); snap.State == bgjob.StateFailed {
		inst.sweepErr = snap.Err
		inst.result = nil
		inst.sweep.Invalidate()
	}
}

func (inst *App) renderBody() {
	for range c.PanelTopInside(inst.ids.PrepareStr("controls")).Resizable(false).KeepIter() {
		inst.renderControls()
	}
	for range c.PanelCentralInside().KeepIter() {
		for dock := range c.DockArea(inst.ids.PrepareStr("ts-dock")) {
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
	inst.renderStatusRow()

	// The window opens on what the method needs (L5): eye height, the fan's
	// half-range and the ensemble size. The rest sits under one fold.
	for range c.Horizontal().KeepIter() {
		for range c.HoverText(tipObserverH).KeepIter() {
			_ = c.SliderF64(inst.ids.PrepareStr("obsH"), inst.observerH, 0, 100).
				Text("observer").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.observerH)
		}
		for range c.HoverText(tipSweepHalf).KeepIter() {
			_ = c.SliderF64(inst.ids.PrepareStr("half"), inst.sweepHalfDeg, 0, 30).
				Text("sweep ±").Suffix("°").FixedDecimals(1).SendRespVal(&inst.sweepHalfDeg)
		}
		for range c.HoverText(tipSamples).KeepIter() {
			_ = c.SliderF64(inst.ids.PrepareStr("samples"), inst.samples, 0, 64).
				Text("samples").FixedDecimals(0).SendRespVal(&inst.samples)
		}
	}
	for range c.HoverText(tipAdvanced).KeepIter() {
		for range c.CollapsingHeader(inst.ids.PrepareStr("advanced"),
			c.WidgetText().Text("Advanced — target height, ray spacing, input uncertainties").Keep()).KeepIter() {
			inst.renderAdvancedControls()
		}
	}
	// Reset clears the two picked points; it is not the view's lead action
	// (the sweep runs on its own as the parameters move), so it carries no
	// kind.
	if c.Button(inst.ids.PrepareStr("reset"), c.Atoms().Text(icons.IconClose+" Reset").Keep()).
		SendResp().HasPrimaryClicked() {
		inst.resetSelection()
	}
}

// renderStatusRow is the window's instrument panel (L7): the selection
// phase, the running sweep's row with its cancel, the last outcome — a
// failure as a toned badge with its reason and a Retry, a cancel as a note —
// and the simulation time once there is a result.
func (inst *App) renderStatusRow() {
	snap := inst.sweep.Snapshot()
	running := snap.State == bgjob.StateRunning
	for range c.Horizontal().KeepIter() {
		c.Label(statusLabel(inst.stage, running)).Send()
		if running {
			if jobprogress.Render(jobprogress.Input{
				Ids:      inst.ids,
				ScopeKey: "sweep-row",
				Fraction: snap.Fraction,
				EtaMs:    snap.EtaMs,
				Note:     snap.Note,
				Cancel:   true,
				Inline:   true,
			}).CancelClicked {
				inst.sweep.Cancel()
				inst.sweepNote = "sweep cancelled"
			}
		}
		if inst.result != nil && inst.result.computeDur > 0 {
			c.AddSpace(plotMargin)
			metric(fmt.Sprintf("simulation: %s (%d samples)", fmtDur(inst.result.computeDur), inst.result.ens.Samples), tipSimTime)
		}
	}
	if samplerErr != nil {
		c.Label(fmt.Sprintf("tiles not available (%s=%q): %v", swissTilesDirEnv, tilesDir(), samplerErr)).Send()
	}
	// A sweep that ended without a result — failed or cancelled — leaves a
	// toned badge with the reason and a Retry. Retry is the one action that
	// state leads with (L2), and the only primary the window ever shows,
	// since the sweep otherwise runs on its own.
	if running || (inst.sweepErr == nil && inst.sweepNote == "") {
		return
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(inst.ids.PrepareStr("retry"), c.Atoms().Text("Retry").Keep()).
			Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked() {
			inst.haveLaunched = false
		}
		if inst.sweepErr != nil {
			badge.New(inst.ids.PrepareStr("sweep-err"), "sweep failed: "+inst.sweepErr.Error()).
				Tone(badge.ToneError).Variant(badge.VariantSoft).Tooltip(tipRetry).Send()
		} else {
			badge.New(inst.ids.PrepareStr("sweep-note"), inst.sweepNote).
				Tone(badge.ToneNeutral).Variant(badge.VariantSoft).Tooltip(tipRetry).Send()
		}
	}
}

// renderAdvancedControls is the fold's body: the parameters a first sweep
// does not need. Per-input uncertainty is 1σ Gaussian; 0 pins that input.
// The bearing fan stays deterministic.
func (inst *App) renderAdvancedControls() {
	for range c.Horizontal().KeepIter() {
		for range c.HoverText(tipTargetH).KeepIter() {
			_ = c.SliderF64(inst.ids.PrepareStr("tgtH"), inst.targetH, 0, 100).
				Text("target").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.targetH)
		}
		for range c.HoverText(tipSweepStep).KeepIter() {
			_ = c.SliderF64(inst.ids.PrepareStr("step"), inst.sweepStepDeg, 0.1, 5).
				Text("step").Suffix("°").FixedDecimals(2).SendRespVal(&inst.sweepStepDeg)
		}
	}
	for range c.Horizontal().KeepIter() {
		for range c.HoverText(tipSigObsPos).KeepIter() {
			_ = c.SliderF64(inst.ids.PrepareStr("sigObsPos"), inst.sigmaObsPos, 0, 50).
				Text("σ obs pos").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.sigmaObsPos)
		}
		for range c.HoverText(tipSigTgtPos).KeepIter() {
			_ = c.SliderF64(inst.ids.PrepareStr("sigTgtPos"), inst.sigmaTgtPos, 0, 50).
				Text("σ tgt pos").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.sigmaTgtPos)
		}
	}
	for range c.Horizontal().KeepIter() {
		for range c.HoverText(tipSigObsH).KeepIter() {
			_ = c.SliderF64(inst.ids.PrepareStr("sigObsH"), inst.sigmaObsH, 0, 30).
				Text("σ obs ht").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.sigmaObsH)
		}
		for range c.HoverText(tipSigTgtH).KeepIter() {
			_ = c.SliderF64(inst.ids.PrepareStr("sigTgtH"), inst.sigmaTgtH, 0, 30).
				Text("σ tgt ht").Suffix(" m").FixedDecimals(1).SendRespVal(&inst.sigmaTgtH)
		}
	}
}

// maybeRecompute is the reactive, coalesced launch. While a sweep is in
// flight it keeps frames flowing (so the row advances and the result paints
// when ready, W7) and never starts a second; once idle it (re)launches if the
// live params have diverged from the last launch — leading + trailing
// coalescing, at most one sweep. Retry clears haveLaunched, which is the
// same relaunch with the same params.
func (inst *App) maybeRecompute() {
	if inst.stage != selectionStagePt2 {
		return
	}
	if inst.sweep.Running() {
		c.RequestRepaint()
		return
	}
	cur := inst.currentParams()
	if !inst.haveLaunched || cur != inst.lastLaunched {
		inst.launchSweep(cur)
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
	res := inst.result

	// The map is created on first render; it owns its view Go-side
	// (ADR-0204). Zoom 8 shows the whole country to pick the points on.
	if inst.pm == nil {
		inst.pm = portolan.New(inst.ids, "terrain-map", portolan.Options{
			Source: basemap.PortolanSource(),
			Loader: basemap.PortolanLoader(inst.tiles),
			Center: portolan.LL(swissCenterLat, swissCenterLon),
			Zoom:   8,
		})
	}
	// A primary click on the map — last frame's, like every register — places
	// the observer, then the target.
	if ll, ok := inst.pm.Clicked(); ok {
		inst.handleClick(ll.Lat, ll.Lng)
	}
	// Sticky-once overrides from the sweep: zoom, then centre.
	if inst.applyZoom {
		inst.pm.View().SetZoom(inst.overrideZoom)
		inst.applyZoom = false
	}
	if inst.applyCenter {
		inst.pm.View().PanTo(portolan.LL(inst.overrideCenter[0], inst.overrideCenter[1]))
		inst.applyCenter = false
	}
	inst.pm.Render(mapStageW, mapStageH, func(p portolan.Projector) {
		if res != nil {
			inst.renderFanOverlay(p, res)
		}
		if inst.stage >= selectionStagePt1 {
			pt := portolan.LL(inst.pt1Lat, inst.pt1Lon)
			p.Marker(pt, 8, colorObserver)
			p.Label(pt, 11, -2, 0, 2, fmt.Sprintf("observer (%.5f, %.5f)", inst.pt1Lat, inst.pt1Lon), 12, colorMapLabel)
		}
		if inst.stage >= selectionStagePt2 {
			pt := portolan.LL(inst.pt2Lat, inst.pt2Lon)
			p.Marker(pt, 8, colorTarget)
			p.Label(pt, 11, -2, 0, 2, fmt.Sprintf("target (%.5f, %.5f)", inst.pt2Lat, inst.pt2Lon), 12, colorMapLabel)
		}
	})
}

// renderFanOverlay draws the swept rays as map polylines from the observer to
// each ray's nominal rotated target, coloured by visibility probability
// (green = clear from every sampled centre, red = blocked from all). The
// centre ray is drawn strong.
func (inst *App) renderFanOverlay(p portolan.Projector, res *sweepResult) {
	tgts := res.ens.Nominal.Targets
	for i := range tgts {
		tgtWGS := swisstopo.LV95ToWGS84(tgts[i])
		isCenter := math.Abs(res.ens.AngleDeg[i]) < 1e-9
		col := visProbColor(res.ens.VisProb[i])
		width := styletokens.StrokeHair
		if isCenter {
			width = styletokens.StrokeStrong
		}
		p.Polyline(
			[]float64{inst.pt1Lat, tgtWGS.Lat},
			[]float64{inst.pt1Lon, tgtWGS.Lon},
			col, width)
	}
}

func (inst *App) renderSweepTab() {
	if res := inst.result; res != nil && len(res.ens.Nominal.Rays) > 0 {
		inst.renderSweepPanel(res)
		return
	}
	inst.renderPending()
}

func (inst *App) renderDistTab() {
	if res := inst.result; res != nil && len(res.ens.Nominal.Rays) > 0 {
		inst.renderDistPane(res)
		return
	}
	inst.renderPending()
}

// renderPending is the placeholder the plot tabs show when there is no sweep
// to draw: which of the states that is — no points yet, one point, a sweep
// running, a sweep that failed, or one that was cancelled — is a different
// sentence with a different remedy (L4). The running row, the reason and
// Retry live in the status row above the tabs.
func (inst *App) renderPending() {
	switch {
	case inst.stage == selectionStageNone:
		c.Label("Click the observer point on the Map tab.").Send()
	case inst.stage == selectionStagePt1:
		c.Label("Click the target point on the Map tab to run the sweep.").Send()
	case inst.sweep.Running():
		c.Label("Computing the line-of-sight sweep ensemble — the status row above shows its progress.").Send()
	case inst.sweepErr != nil:
		c.Label("The sweep failed; the reason and Retry are in the status row above.").Send()
	default:
		c.Label("No sweep to show — change a parameter or press Retry in the status row above.").Send()
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

	// The sweep plot renders through the implot port (ADR-0149 SD7);
	// bumping plotEpoch still resets the view by changing the plot's
	// identity. Margins keep a legend taller than the plot body from
	// colliding with the labels above or the panel edge below.
	c.AddSpace(plotMargin)
	p := implot.Begin(inst.ids, fmt.Sprintf("##sweep-plot-%d", inst.plotEpoch), mapStageW, plotHeight)
	p.SetupAxes("Distance along ray (m)", "Elevation (m a.s.l.)", implot.AxisFlagsNone, implot.AxisFlagsNone)

	// Elevation envelope bands behind the profiles (only meaningful with a
	// non-degenerate ensemble). All bands share one label — and therefore
	// one legend entry — while each keeps its own bearing-ramp fill.
	if ens.Samples > 0 && len(ens.Distance) > 1 {
		for j := range rays {
			p.SetNextColor(rayFillRGBA(j, len(rays), envelopeBandAlpha))
			p.ShadedBetween("± uncertainty", ens.Distance,
				f32sToF64(ens.TerrainMax[j]), f32sToF64(ens.TerrainMin[j]))
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
			col = colorCenterRay
			width = 2.5
		}
		name := fmt.Sprintf("%+.1f°", ens.AngleDeg[i])
		if ens.Samples > 0 {
			name = fmt.Sprintf("%+.1f° %.0f%%", ens.AngleDeg[i], ens.VisProb[i]*100)
		}
		p.SetNextColor(col.Literal()).SetNextWeight(width)
		p.Line(name, ray.ProfileDist, f32sToF64(ray.ProfileElev))
	}
	p.SetNextColor(colorSightLine.Literal()).SetNextWeight(1.5)
	p.Line("sight line", center.ProfileDist, f32sToF64(center.LOSElev))

	var obsX []float64
	var obsY []float64
	for _, ray := range rays {
		if !ray.Visible {
			obsX = append(obsX, ray.ObstructionDist)
			obsY = append(obsY, float64(ray.ObstructionElev))
		}
	}
	if len(obsX) > 0 {
		p.SetNextColor(colorObstruction.Literal())
		p.Scatter("obstructions", obsX, obsY, implot.MarkerSquare, 4.0)
	}
	p.End()
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
	c.AddSpace(plotMargin)
	p := implot.Begin(inst.ids, fmt.Sprintf("##dist-plot-%d", inst.plotEpoch), mapStageW, ecdfHeight)
	p.SetupAxes("deviation from nominal (m)", "cumulative probability",
		implot.AxisFlagsNone, implot.AxisFlagsNone)
	p.IncludeY(0)
	p.IncludeY(1)
	for i, in := range ens.Inputs {
		xs, ys := ecdfStep(in.Dev)
		p.SetNextColor(distColorAt(i).Literal()).SetNextWeight(1.8)
		p.Line(in.Name, xs, ys)
	}
	p.End()
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

// distColorAt colours the i-th per-variable ECDF line. The variables are
// categories with no order, so the encoding is the IDS qualitative cycle
// (ADR-0156); at most four inputs are ever randomised, well inside the
// cycle's seven entries.
func distColorAt(i int) (col color.Color) {
	return roleColor(styletokens.QualitativeCycle(i))
}

// Colours of the marks that are not data encodings: the two picked points,
// their labels, the centre ray, the sight line and the obstruction markers.
// Every one is a styletokens role or ramp sample (L1), so the theme chosen
// at launch reaches them; the fan and the ray profiles stay on Batlow
// (rayTone, visProbColor). The observer and the target are two unordered
// categories, so they take the first two entries of the qualitative cycle
// rather than a status tone they do not carry.
var (
	colorObserver    = roleColor(styletokens.QualitativeCycle(0))
	colorTarget      = roleColor(styletokens.QualitativeCycle(1))
	colorMapLabel    = roleColor(styletokens.NeutralTextExtreme)
	colorCenterRay   = roleColor(styletokens.NeutralTextExtreme) // the highlighted one, legible over any Batlow sample
	colorSightLine   = roleColor(styletokens.AccentDefault)      // the reference line the profiles are read against
	colorObstruction = roleColor(styletokens.ErrorDefault)       // a blocked sight line is the defect the method reports
)

// roleColor is a styletokens role or ramp sample as a widget colour.
// RGBA8.AsHex packs 0xRRGGBBAA, which is what color.Hex consumes.
func roleColor(t styletokens.RGBA8) (col color.Color) {
	return color.Hex(t.AsHex())
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
	inst.sweep.Invalidate() // an in-flight sweep's result would describe the old points
	inst.result = nil
	inst.sweepErr = nil
	inst.sweepNote = ""
	inst.stage = selectionStageNone
	inst.haveLaunched = false
	inst.plotEpoch++ // fresh egui_plot widget state next render
}

// launchSweep starts one sweep on the runner as a cancellable keelson task
// (W2, W5). The previous result is dropped at once: it described other
// parameters, and the plot tabs say the sweep is running until the new one
// lands. The compute callback never touches imzero2.
//
// Progress is indeterminate with a phase note. The ensemble is one call that
// runs its Monte-Carlo members internally and takes neither a context nor a
// progress hook, so a fraction from the samples done would need one in
// swisstopo. deferred: a per-sample report from LineOfSightSweepEnsemble.
func (inst *App) launchSweep(p sweepParams) {
	inst.result = nil
	inst.sweepErr = nil
	inst.sweepNote = ""
	started := inst.sweep.StartReporting(inst.tasks, bgjob.Spec{Kind: sweepKind, Title: sweepTitle},
		func(ctx context.Context, report bgjob.Reporter) (res *sweepResult, err error) {
			return runSweep(ctx, inst.logger, report, p)
		})
	if !started {
		inst.logger.Debug().Msg("terrainscope: sweep already running")
	}
}

// runSweep is the worker body. The error it returns is the one the window
// shows (W8), so it says what failed in the reader's terms; the structured
// context goes to the log.
func runSweep(ctx context.Context, logger zerolog.Logger, report bgjob.Reporter, p sweepParams) (res *sweepResult, err error) {
	report(0, 0, "opening the elevation tiles")
	s, err := ensureSampler()
	if err != nil {
		err = eh.Errorf("elevation tiles not available: %w", err)
		return
	}
	if ctx.Err() != nil {
		err = ctx.Err()
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
	report(0, 0, fmt.Sprintf("nominal sweep and %d ensemble members", spec.Samples))
	t0 := time.Now()
	ens, err := s.LineOfSightSweepEnsemble(fromLV, p.observer, toLV, p.target, spec)
	computeDur := time.Since(t0)
	if err != nil {
		logger.Error().Err(err).
			Float64("fromLat", p.fromLat).Float64("fromLon", p.fromLon).
			Float64("toLat", p.toLat).Float64("toLon", p.toLon).
			Int64("samples", p.samples).
			Msg("terrainscope: line-of-sight sweep ensemble failed")
		err = eh.Errorf("line-of-sight sweep ensemble: %w", err)
		return
	}
	res = &sweepResult{ens: ens, fromLV: fromLV, toLV: toLV, computeDur: computeDur}
	return
}

// fmtDur renders a simulation wall-clock as milliseconds (microsecond
// resolution), or an em dash when unmeasured.
func fmtDur(d time.Duration) (s string) {
	if d <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f ms", float64(d.Microseconds())/1000.0)
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

// rayTone is the shared ramp behind the two ray colours. Bearings are ORDERED,
// so the encoding is sequential, and it comes from the IDS data-encoding
// palette rather than a hand-rolled interpolation (ADR-0031 §SD6). Batlow is
// the IDS default and perceptually uniform, so equal steps in bearing read as
// equal steps in colour — which the previous blue→red lerp did not give, since
// its midpoint collapsed towards grey.
//
// A single ray has no ordering to encode, so it takes the ramp's midpoint.
func rayTone(i int, n int) (rgba styletokens.RGBA8) {
	t := float32(0.5)
	if n > 1 {
		t = float32(i) / float32(n-1)
	}
	return styletokens.Sequential(styletokens.SequentialBatlow, t)
}

// paletteAlpha re-alphas an IDS palette sample.
//
// RGBA8.AsHex packs 0xRRGGBBAA, which is exactly what color.Hex consumes, so
// swapping the low byte takes the hue from the palette and the opacity from
// the caller. Worth the helper: the alternative is color.RGBA(t.R, t.G, t.B,
// a), which reads as hand-written colour to both a reader and the L2 rule even
// though every component came from the ramp.
func paletteAlpha(t styletokens.RGBA8, alpha uint8) (col color.Color) {
	return color.Hex(t.AsHex()&^uint32(0xff) | uint32(alpha))
}

// rayColorAt returns the profile-line colour for ray i of n.
func rayColorAt(i int, n int) (col color.Color) {
	return paletteAlpha(rayTone(i, n), rayLineAlpha)
}

// rayFillRGBA is rayColorAt as a packed 0xRRGGBBAA with an explicit alpha, for
// the translucent envelope polygons. It MUST stay on the same ramp: the
// envelope is the band around the line of the same bearing, and two ramps
// would colour one ray two ways.
func rayFillRGBA(i int, n int, alpha uint8) (rgba uint32) {
	return rayTone(i, n).AsHex()&^uint32(0xff) | uint32(alpha)
}

// visProbColor maps a visibility probability in [0,1] to the map fan's colour
// (0 = blocked from every sampled centre, 1 = clear).
//
// From the IDS sequential palette rather than the previous red→amber→green
// lerp (ADR-0031 §SD6), and the change is not only about where colours come
// from: red-to-green is the one ramp that disappears for the commonest form of
// colour-vision deficiency, and this encodes the panel's primary quantity.
// Batlow separates on lightness as well as hue, so the ordering survives.
func visProbColor(p float64) (col color.Color) {
	return paletteAlpha(styletokens.Sequential(styletokens.SequentialBatlow, float32(p)), visFanAlpha)
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
