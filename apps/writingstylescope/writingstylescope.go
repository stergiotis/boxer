// Package writingstylescope is a keelson app for finding shared writing
// between two documents. Paste two Markdown documents; the app splits each
// into sections (one per heading), measures the compression distance of every
// A-section against every B-section, and shows the result three ways: a
// document-level headline, the cross-matrix as a heatmap, and the empirical
// distribution of all the pairwise distances.
//
// The measurement is normalized compression distance (NCD): compress the two
// texts together and see how much smaller that is than compressing them apart.
// A pair that shares wording compresses well together. The engine is
// public/analytics/similarity/compression and its stylometry sub-package.
//
// The app returns no verdict, and it asserts no threshold — NCD has no
// absolute scale that survives a change of subject, language, or section
// length. What it shows instead is where each pair sits in the background
// distribution formed by every other pair of the same two documents. A section
// that was copied is the point that does not belong to that distribution.
// Deciding what that means is the reader's job. The decision and its
// alternatives are ADR-0175.
//
// Three dock tabs: "Documents" (paste, sweep, headline), "Matrix" (the
// section-by-section heatmap and the closest pairs) and "Distribution" (the
// ECDF that calibrates them). The pairs table hands the whole cross-matrix to
// the SQL playground as an ephemeral dataset — see writingstylescope_handover.go.
//
// Lifecycle: Mount captures the id stack, logger and bus; Frame renders inside
// the host-owned window and runs the sweep when the user asks for one; Unmount
// cancels any confidence-band warm-up still in flight and retracts the
// published dataset.
package writingstylescope

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

const (
	stageW = float32(920) // width of the plot stages
	margin = float32(10)  // vertical breathing room around plots

	paneW    = float32(440) // one document paste pane
	paneRows = uint32(16)
	// paneH pins each paste pane's height. A multiline TextEdit grows with
	// its content, so without a bound a pasted document pushes the controls
	// and the readout off the bottom of the tab; the pane scrolls internally
	// instead. Min and max are both set so the two panes stay level whatever
	// their contents.
	paneH = float32(300)

	// heatPrefH / ecdfPrefH are PREFERRED plot-box heights. Both boxes follow
	// the pane when the pane is shorter — implot draws its x tick labels
	// inside the box along the bottom, so a box taller than its pane loses
	// them and the clipped y range reads as missing data rather than as a
	// cropped view.
	heatPrefH = float32(430)
	ecdfPrefH = float32(360)

	// paneSlack keeps a box off the pane's right edge.
	paneSlack = float32(8)
	// colorBarH is the gradient strip's height under the heatmap.
	colorBarH = float32(34)
	// plotMinW floors the width so a very narrow window degrades to a
	// scrollable box rather than an unreadable sliver.
	plotMinW = float32(420)

	// heatChromeBelow / ecdfChromeBelow are the room each tab needs *under*
	// its plot — the colour bar, hover readout, separator and pair table on
	// the Matrix tab; the status line and the three notes on the Distribution
	// tab. Subtracted from the pane so the plot does not push its own
	// explanation out of view. Overflow past this still scrolls.
	heatChromeBelow = float32(420)
	ecdfChromeBelow = float32(150)

	// ecdfGridN is the resolution of the grid the ECDF and its confidence
	// band are evaluated at. The band's calibration depends on the sample
	// size, not on this (see the ecdf widget's RenderGrid contract), so a
	// coarse grid costs nothing statistically and keeps the emitted geometry
	// small even for a full 128×128 matrix.
	ecdfGridN = 256

	// maxTickLabels is the largest number of sections that still gets named
	// axis ticks. Past it the labels overlap into illegibility and the axes
	// fall back to section indices; the hover readout names the pair either
	// way.
	maxTickLabels = 22
	tickLabelMax  = 20 // characters, before elision

	// Dock tab ids — stable across frames (they name entries in egui_dock's
	// persistent layout state).
	dockTabDocuments = uint64(1)
	dockTabMatrix    = uint64(2)
	dockTabDist      = uint64(3)

	sep = "  ·  " // separates inline metric chips in a row
)

// Hover-help. Each quantity is explained at the widget that shows it rather
// than in a wall of prose (the terrainscope/fibscope idiom).
const (
	tipIntro = "Paste two documents. Every section of the first is compared against every section of the second by compressing the pair together: a pair that shares wording compresses better than a pair that does not."
	tipPaneA = "Document A. Its sections become the rows of the matrix. Markdown headings mark the section boundaries; a document with no headings is compared as a whole."
	tipPaneB = "Document B. Its sections become the columns of the matrix."
	tipFloor = "Sections shorter than this are left out of the sweep. Compression distance between very short texts measures framing overhead more than content, and an empty nesting heading is mostly framing."
	tipRun   = "Run the sweep. Cost grows with the product of the two section counts, so it runs when you ask rather than as you type."
	tipStale = "The text has changed since this result was computed. Press Analyze to refresh it."

	tipSections = "Sections that cleared the length floor, and how many did not. A section is one heading's own text — the span up to the next heading, whatever its level — so a nesting heading owns only its lead-in and nothing is counted twice."
	tipElapsed  = "Wall-clock time of the sweep, and how many pairs it measured."

	tipProfileNcd = "Profile mode: document A against document B's sections concatenated and truncated to A's length, as one number. Length-matched, so it answers 'do these two documents resemble each other overall', not 'is any part of one inside the other'."
	tipProfileCcc = "Conditional complexity of compression: how many extra compressed bytes B costs once A is already known. Unnormalised, so it tracks length as well as content — readable here only because profile mode matched the lengths first."
	tipInstance   = "Instance mode: document A as a fixed reference, each B section streamed past it, with running statistics. It stops early once the spread of the statistic stabilises — the count and the converged flag say whether it did."

	tipHeat     = "Every A section (rows) against every B section (columns). Bright is a low distance — the pair compressed well together. The colour range spans this matrix's own minimum and maximum, so brightness is relative to these two documents and not to any fixed scale."
	tipCell     = "The pair under the cursor, its distance, and where that distance falls among all the pairs in this matrix."
	tipTop      = "The closest pairs, and the fraction of all pairs each one beats. A pair at 0.0% is closer than every other pair measured — worth opening both sections side by side."
	tipHandover = "Publish the whole cross-matrix — every pair, not just the ranked ones — as an ephemeral dataset and open it in the SQL playground, seeded with the query behind this table. The dataset lives as long as this window does."

	tipEcdf = "The distribution of all the pairwise distances. The bulk of the curve is this document pair's own background: what 'unrelated' looks like for this subject matter, in this language, at these section lengths. A shared section is a point detached from the bulk on the left."
	tipBand = "The shaded band is a 95% simultaneous confidence band. Its coverage guarantee assumes independent observations, and these are not independent — every section appears in many pairs. Read it as a scale reference, not as a calibrated probability statement."
	tipMark = "Each vertical line marks one of the closest pairs from the Matrix tab, placed where it falls in the background."
	tipWarm = "The exact confidence band is an O(n²) inversion. Until it is ready the plot shows the wider closed-form band instead, so the curve is never held up."
)

// bandJobSeq gives every App instance a distinct confidence-band job key, so
// two open windows warming bands for different matrices do not cancel each
// other's solve.
var bandJobSeq atomic.Uint64

// paneProbeSalt namespaces this app's pane-probe register slots. The seq is
// XORed with the instance id stack so two open windows probe different slots
// (the r21 map is process-wide).
const paneProbeSalt uint64 = 0x77535330b3d50001

// plotMinH floors every pane-following box at the height below which implot
// clips its own bottom gutter — the x tick labels. Both axes are labelled on
// both plots and neither carries a title, so one value covers them.
var plotMinH = implot.MinBoxHeight(false, true, true, 1)

// App is the per-window writingstylescope instance.
type App struct {
	// ids is the per-instance WidgetIdStack. The host pre-pushes a
	// window-unique salt onto it before every Frame() call (ADR-0026 §SD9,
	// windowhost.renderWindowBody), so every widget id the app derives is
	// unique across all concurrently open instances. Captured from ctx.Ids()
	// in Mount; the app must NOT Reset() it.
	ids *c.WidgetIdStack

	logger zerolog.Logger

	// docA and docB are the pasted sources. They are the FFI write-back
	// targets of the two TextEdits, so their addresses must outlive the frame
	// the edit is emitted in — which they do, being App fields.
	docA string
	docB string

	// minSectionBytes backs the length-floor slider (§SD1). Held as a
	// float64 because that is the only slider width with a value binding;
	// it is read back through an integer clamp.
	minSectionBytes float64

	// res is the cached sweep; err is what the last attempt failed with.
	// Exactly one of them is meaningful at a time.
	res *Analysis
	err error

	// ranA / ranB / ranFloor are the inputs res was computed from, so an
	// edit since the sweep can be labelled stale rather than silently shown
	// against the new text.
	ranA     string
	ranB     string
	ranFloor float64

	// pending defers the first sweep to the first frame, keeping construction
	// (and therefore opening a window) free of compression work.
	pending bool

	// cmap and scale are retained across frames: ColorScale caches a tick
	// layout keyed on the range, and rebuilding it every frame would throw
	// that away. The colormap's range is updated in place when a new sweep
	// lands.
	cmap  *colormap.Config
	scale *colorscale.ColorScale

	// bandKey identifies this instance's confidence-band warm-up job.
	bandKey string

	// heatPane / ecdfPane hold the last good pane measurement for each plot.
	// CapturePaneSize answers one frame behind and reports ok=false on the
	// frame a dock tab comes back, so sizing off a miss would flash the box to
	// its floor on every tab switch.
	heatPane [2]float32
	ecdfPane [2]float32

	// bus is the app runtime's message bus, captured in Mount. nil on hosts
	// that wire none (the tour), which disables the play handover.
	bus app.BusI

	// handover is the "Open in play" state: the published dataset handle plus
	// the in-flight/last-outcome fields the button reads. Guarded because the
	// publish and the launch request are bus round-trips and must run off the
	// render thread.
	handoverMu   sync.Mutex
	handoverBusy bool
	handoverErr  string
	handoverNote string
	handle       string
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids:             c.NewWidgetIdStack(),
		logger:          log.Logger,
		docA:            sampleDocA,
		docB:            sampleDocB,
		minSectionBytes: defaultMinSectionBytes,
		pending:         true,
		bandKey:         fmt.Sprintf("writingstylescope/%d", bandJobSeq.Add(1)),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.bus = ctx.Bus()
	return
}

// Unmount cancels a confidence-band solve still running for this window and
// retracts the published pairs dataset. A band that already finished stays in
// the shared ecdfbands cache, so a reopen still renders instantly.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	ecdf.CancelBandJob(inst.bandKey)
	inst.retractHandover()
	return
}

// Frame renders the app body. The host has already pre-pushed a window-unique
// salt onto inst.ids (ADR-0026 §SD9), so the app must not Reset() the stack or
// wrap the body in its own instance salt.
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.renderBody()
	return
}

func (inst *App) renderBody() {
	inst.runPending()
	for range c.PanelCentralInside().KeepIter() {
		for dock := range c.DockArea(inst.ids.PrepareStr("wss-dock")) {
			dock.InitRoot(dockTabDocuments, dockTabMatrix, dockTabDist)
			for range dock.Tab(dockTabDocuments, "Documents") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderDocuments()
				}
			}
			for range dock.Tab(dockTabMatrix, "Matrix") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderMatrix()
				}
			}
			for range dock.Tab(dockTabDist, "Distribution") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderDistribution()
				}
			}
		}
	}
}

// runPending performs the deferred first sweep. Construction seeds the example
// pair but does no compression, so opening a window is free; the first frame
// pays for it.
func (inst *App) runPending() {
	if !inst.pending {
		return
	}
	inst.pending = false
	inst.run()
}

// run sweeps the current panes and records what it was computed from.
func (inst *App) run() {
	inst.res, inst.err = analyze(inst.docA, inst.docB, clampFloor(inst.minSectionBytes))
	inst.ranA, inst.ranB, inst.ranFloor = inst.docA, inst.docB, inst.minSectionBytes
	if inst.err != nil {
		inst.logger.Warn().Err(inst.err).Msg("writingstylescope: sweep failed")
		return
	}
	if inst.cmap != nil {
		inst.cmap.DataMin, inst.cmap.DataMax = inst.colorRange()
		inst.scale = nil // tick layout is cached against the old range
	}
}

// stale reports whether the panes have moved on since the cached sweep.
func (inst *App) stale() (yes bool) {
	return inst.docA != inst.ranA || inst.docB != inst.ranB || inst.minSectionBytes != inst.ranFloor
}

// colorRange is the matrix extent the heatmap and the colour bar span. A
// degenerate matrix (every cell equal) is widened so colormap.NewConfig's
// min < max precondition holds.
func (inst *App) colorRange() (lo float64, hi float64) {
	if inst.res == nil || len(inst.res.Sorted) == 0 {
		return 0, 1
	}
	lo, hi = inst.res.Min(), inst.res.Max()
	if !(lo < hi) {
		return lo - 0.5, lo + 0.5
	}
	return
}

// ---------------------------------------------------------------------------
// Documents tab
// ---------------------------------------------------------------------------

func (inst *App) renderDocuments() {
	metric("Two documents, compared section by section. A section that appears in both compresses far better against its twin than against anything else.", tipIntro)
	c.Separator().Send()
	inst.renderPanes()
	c.Separator().Send()
	inst.renderControls()
	c.Separator().Send()
	inst.renderStatus()
}

func (inst *App) renderPanes() {
	for range c.HorizontalTop().KeepIter() {
		for range c.Vertical().KeepIter() {
			metric("Document A — rows", tipPaneA)
			inst.renderPane("doc-a", &inst.docA, "paste the first document…")
		}
		for range c.Vertical().KeepIter() {
			metric("Document B — columns", tipPaneB)
			inst.renderPane("doc-b", &inst.docB, "paste the second document…")
		}
	}
}

// renderPane draws one paste pane at a pinned height. The TextEdit itself has
// no height ceiling — a multiline egui TextEdit sizes to max(desired_rows,
// content) — so the bound comes from the enclosing ScrollArea's ui, pinned top
// and bottom so a long document scrolls inside the pane instead of stretching
// the tab, and a short one still leaves the pane at full height beside its
// neighbour.
//
// draft must be an App field: it is the FFI write-back target and the frontend
// writes into it one frame after the edit.
func (inst *App) renderPane(idStr string, draft *string, hint string) {
	for range c.ScrollArea().Vscroll(true).KeepIter() {
		c.UiSetMinHeight(paneH)
		c.UiSetMaxHeight(paneH)
		_ = c.TextEdit(inst.ids.PrepareStr(idStr), *draft, true).
			CodeEditor().DesiredWidth(paneW).DesiredRows(paneRows).
			HintText(hint).
			SendRespVal(draft)
	}
}

func (inst *App) renderControls() {
	for range c.Horizontal().KeepIter() {
		for range c.HoverText(tipRun).KeepIter() {
			if c.Button(inst.ids.PrepareStr("run"), c.Atoms().Text("Analyze").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.run()
			}
		}
		if c.Button(inst.ids.PrepareStr("example"), c.Atoms().Text("Load example").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.docA, inst.docB = sampleDocA, sampleDocB
			inst.run()
		}
		if c.Button(inst.ids.PrepareStr("clear"), c.Atoms().Text("Clear").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.docA, inst.docB = "", ""
			inst.run()
		}
	}
	for range c.HoverText(tipFloor).KeepIter() {
		_ = c.SliderF64(inst.ids.PrepareStr("floor"), inst.minSectionBytes,
			minMinSectionBytes, maxMinSectionBytes).
			Text("section-length floor (bytes)").FixedDecimals(0).
			SendRespVal(&inst.minSectionBytes)
	}
	if inst.stale() {
		metric("⚠ stale — the text or the floor changed since this result was computed.", tipStale)
	}
}

func (inst *App) renderStatus() {
	if inst.err != nil {
		metric("cannot sweep: "+inst.err.Error(), tipRun)
		return
	}
	r := inst.res
	if r == nil {
		metric("No result yet — press Analyze.", tipRun)
		return
	}
	for range c.Horizontal().KeepIter() {
		metric(fmt.Sprintf("A: %d sections (%d below the floor)", r.Rows(), r.DroppedA), tipSections)
		c.Label(sep).Send()
		metric(fmt.Sprintf("B: %d sections (%d below the floor)", r.Cols(), r.DroppedB), tipSections)
		c.Label(sep).Send()
		metric(fmt.Sprintf("%d pairs in %s", r.Rows()*r.Cols(), humanMs(r.Elapsed.Seconds()*1000)), tipElapsed)
	}
	c.Separator().Send()
	metric("Document-level readings — these compare the documents as wholes, and are deliberately blind to a single copied section. The Matrix tab is where that shows up.", tipProfileNcd)
	for range c.Horizontal().KeepIter() {
		metric(fmt.Sprintf("profile NCD %.4f", r.ProfileNcd), tipProfileNcd)
		c.Label(sep).Send()
		metric(fmt.Sprintf("profile CCC %.0f bytes", r.ProfileCcc), tipProfileCcc)
	}
	metric(fmt.Sprintf("instance sweep over %d B sections%s: min %.4f, mean %.4f ± %.4f, max %.4f",
		r.InstCount, convergedNote(r.InstConverged), r.InstMin, r.InstMean, r.InstStdDev, r.InstMax), tipInstance)
}

// ---------------------------------------------------------------------------
// Matrix tab
// ---------------------------------------------------------------------------

func (inst *App) renderMatrix() {
	r := inst.res
	if r == nil {
		metric("No result yet — paste two documents on the Documents tab and press Analyze.", tipRun)
		return
	}
	metric("Every A section against every B section. Bright is close.", tipHeat)
	if inst.stale() {
		metric("⚠ showing the previous sweep — the text has changed since.", tipStale)
	}

	inst.ensureColormap()
	// Probe HERE — after the chrome above the plot and before the plot itself.
	// The rect is the room left for the NEXT widget, so a probe placed after
	// the plot would size the plot against its own output.
	inst.probePane("matrix", &inst.heatPane)
	c.AddSpace(margin)

	rows, cols := r.Rows(), r.Cols()
	w, h := boxSize(inst.heatPane, heatPrefH, heatChromeBelow)
	var hovI, hovJ int = -1, -1
	p := implot.Begin(inst.ids, "##wss-matrix", w, h)
	p.SetupAxes("document B — sections", "document A — sections",
		implot.AxisFlagsNone, implot.AxisFlagsNone)
	applyTicks(p, implot.AxisX1, colTickValues(cols), sectionLabels(r.SecB, cols))
	applyTicks(p, implot.AxisY1, rowTickValues(rows), sectionLabels(r.SecA, rows))
	p.NoLegend()
	p.Heatmap("NCD", r.Ncd, rows, cols, inst.cmap, 0, 0, float64(cols), float64(rows))
	if x, y, ok := p.HoverPlotPos(); ok {
		hovJ, hovI = cellAt(x, y, rows, cols)
	}
	p.End()

	c.AddSpace(margin)
	inst.renderScale()
	inst.renderHoverReadout(hovI, hovJ)
	c.Separator().Send()
	inst.renderTopPairs()
}

// ensureColormap builds (or rebuilds) the retained colormap + colour bar for
// the current matrix range. The palette is reversed so a *low* distance — the
// interesting end — is the bright one.
func (inst *App) ensureColormap() {
	lo, hi := inst.colorRange()
	if inst.cmap == nil {
		inst.cmap = colormap.NewConfig(reversedPalette(colormap.Viridis8), lo, hi)
	}
	if inst.cmap.DataMin != lo || inst.cmap.DataMax != hi {
		inst.cmap.DataMin, inst.cmap.DataMax = lo, hi
		inst.scale = nil
	}
	if inst.scale == nil {
		inst.scale = colorscale.New(inst.ids, "wss-scale", inst.cmap,
			colorscale.WithOrientation(colorscale.OrientationHorizontal))
	}
	w, _ := boxSize(inst.heatPane, heatPrefH, heatChromeBelow)
	inst.scale.SetSize(w, colorBarH)
}

func (inst *App) renderScale() {
	for range c.HoverText(tipHeat).KeepIter() {
		c.Label("NCD — bright is a low distance; the range spans this matrix only").Send()
	}
	inst.scale.Render()
}

func (inst *App) renderHoverReadout(i int, j int) {
	r := inst.res
	if i < 0 || j < 0 {
		metric("Hover a cell to name the pair it measures.", tipCell)
		return
	}
	v := r.At(i, j)
	metric(fmt.Sprintf("A §%s  ×  B §%s   —   NCD %.4f, closer than %s of all pairs",
		r.SecA[i].Label(), r.SecB[j].Label(), v, percentText(1-r.Quantile(v))), tipCell)
}

func (inst *App) renderTopPairs() {
	r := inst.res
	metric("Closest pairs", tipTop)

	c.TableColumn().Exact(40).Send()    // rank
	c.TableColumn().Initial(230).Send() // A section
	c.TableColumn().Initial(230).Send() // B section
	c.TableColumn().Initial(80).Send()  // NCD
	c.TableColumn().Initial(90).Send()  // percentile
	c.TableColumn().Remainder().Send()  // sizes

	c.TableHeaderText("#").Send()
	c.TableHeaderText("document A section").Send()
	c.TableHeaderText("document B section").Send()
	c.TableHeaderText("NCD").Send()
	c.TableHeaderText("beats").Send()
	c.TableHeaderText("bytes").Send()

	for k, pr := range r.Pairs {
		c.TableCellText(fmt.Sprintf("%d", k+1)).Send()
		c.TableCellText(r.SecA[pr.I].Label()).Send()
		c.TableCellText(r.SecB[pr.J].Label()).Send()
		c.TableCellText(fmt.Sprintf("%.4f", pr.Ncd)).Send()
		c.TableCellText(percentText(1 - r.Quantile(pr.Ncd))).Send()
		c.TableCellText(fmt.Sprintf("%d / %d", r.SecA[pr.I].Bytes(), r.SecB[pr.J].Bytes())).Send()
	}
	c.Table(inst.ids.PrepareStr("toppairs"), 20, uint64(len(r.Pairs))).
		Striped(true).Vscroll(true).MaxScrollHeight(300).Send()
	inst.renderHandover()
}

// renderHandover draws the "Open in play" control under the ranked table. The
// button starts a bus round-trip on its own goroutine — bus.Request is
// synchronous and would stall the frame — and the outcome is read back from
// the handover fields on a later frame.
func (inst *App) renderHandover() {
	busy, note, errText := inst.handoverState()
	for range c.Horizontal().KeepIter() {
		label := "Open in play"
		if busy {
			label = "Opening…"
		}
		for range c.HoverText(tipHandover).KeepIter() {
			if c.Button(inst.ids.PrepareStr("handover"), c.Atoms().Text(label).Keep()).
				SendResp().HasPrimaryClicked() && !busy && inst.bus != nil {
				go inst.requestHandover()
			}
		}
		if inst.bus == nil {
			metric("(needs the app runtime — no bus on this host)", tipHandover)
		}
	}
	if errText != "" {
		metric("handover failed: "+errText, tipHandover)
		return
	}
	if note != "" {
		metric(note, tipHandover)
	}
}

// ---------------------------------------------------------------------------
// Distribution tab
// ---------------------------------------------------------------------------

func (inst *App) renderDistribution() {
	r := inst.res
	if r == nil {
		metric("No result yet — paste two documents on the Documents tab and press Analyze.", tipRun)
		return
	}
	metric("Where each pair sits among all the others. There is no threshold here on purpose: NCD has no absolute scale that survives a change of subject, language, or section length, so the only honest reference is the pair's own background.", tipEcdf)

	n := len(r.Sorted)
	if n < 2 || !(r.Min() < r.Max()) {
		metric(fmt.Sprintf("Not enough spread to draw a distribution — %d pairs, all at the same distance.", n), tipEcdf)
		return
	}

	xs, fnAt := ecdfGrid(r.Sorted, ecdfGridN)
	rr := ecdf.New().SeriesName("NCD, all section pairs")
	exact := rr.BandReady(n)
	var snap ecdf.BandJobSnapshot
	if !exact {
		snap = rr.EnsureBandJob(inst.bandKey, nil, n)
	}

	// Probe after the chrome above the plot, before the plot — see renderMatrix.
	inst.probePane("ecdf", &inst.ecdfPane)
	c.AddSpace(margin)
	w, h := boxSize(inst.ecdfPane, ecdfPrefH, ecdfChromeBelow)
	var ch ecdf.Crosshair
	p := implot.Begin(inst.ids, "##wss-ecdf", w, h)
	p.SetupAxes("NCD", "fraction of pairs at or below", implot.AxisFlagsNone, implot.AxisFlagsNone)
	p.IncludeY(0)
	p.IncludeY(1)
	if exact {
		_ = rr.RenderGrid(p, xs, fnAt, n)
		ch = rr.AtGrid(p, xs, fnAt, n)
	} else {
		_ = rr.RenderGridPreview(p, xs, fnAt, n)
		ch = rr.AtGridPreview(p, xs, fnAt, n)
	}
	if marks := markValues(r.Pairs); len(marks) > 0 {
		p.SetNextWeight(1.2)
		p.InfLinesV("closest pairs", marks)
	}
	rr.PaintCrosshair(p, ch)
	p.End()
	c.AddSpace(margin)

	ecdf.WriteStatusLine(ch)
	c.Separator().Send()
	if !exact {
		metric(fmt.Sprintf("exact confidence band warming — %s%s", percentText(float64(snap.Fraction)), bandNote(snap)), tipWarm)
	}
	metric(bandLine(exact), tipBand)
	metric("Vertical lines mark the closest pairs from the Matrix tab.", tipMark)
	inst.renderTailNote()
}

// renderTailNote states what the left tail is showing, in the terms the app is
// willing to defend: separation from the background, not a verdict.
func (inst *App) renderTailNote() {
	r := inst.res
	if len(r.Pairs) == 0 {
		return
	}
	best := r.Pairs[0]
	median := quantileOf(r.Sorted, 0.5)
	gap := median - best.Ncd
	metric(fmt.Sprintf("Closest pair %.4f, median pair %.4f — a gap of %.4f. A pair detached from the bulk is worth reading side by side; a pair sitting inside it is what two independently written sections on one subject look like.",
		best.Ncd, median, gap), tipEcdf)
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// probePane arms this frame's seq-keyed pane probe for role and folds the
// answer — which describes the *previous* frame — into last. A miss leaves
// last alone: CapturePaneSize reports ok=false on the frame a dock tab comes
// back, and sizing off the miss would flash the box to its floor on every tab
// switch. The seq is XORed with the instance id stack so two open windows do
// not share a slot.
func (inst *App) probePane(role string, last *[2]float32) {
	seq := c.ProbeSeq("writingstylescope", role) ^ inst.ids.PrepareHighEntropy(paneProbeSalt).Derive()
	if w, h, ok := c.CapturePaneSize(seq); ok {
		*last = [2]float32{w, h}
	}
}

// boxSize turns a measured pane into a plot-box size: the preferred height
// unless the pane is shorter once the chrome below the plot is reserved, and
// never below implot's own clipping floor. An unmeasured pane (the first
// frame, or a host that never answers) falls back to the fixed preference.
//
// The floor is the widget's minimum, not a readability number: implot lays its
// gutters out at the minimum whatever height it is handed, so a box under the
// floor is not a smaller plot, only one that has clipped its own tick labels.
// Anything the pane cannot hold spills into the tab's ScrollArea.
func boxSize(pane [2]float32, prefH float32, chromeBelow float32) (w float32, h float32) {
	w, h = stageW, prefH
	if pane[0] > plotMinW+paneSlack {
		w = pane[0] - paneSlack
	} else if pane[0] > 0 {
		w = plotMinW
	}
	if pane[1] > 0 {
		h = min(prefH, pane[1]-chromeBelow)
	}
	return w, max(h, plotMinH)
}

// clampFloor turns the slider's float into the integer byte floor the
// analysis takes, clamped to the declared domain (NaN included, via the
// negated comparison).
func clampFloor(f float64) (n int) {
	if !(f > minMinSectionBytes) {
		return minMinSectionBytes
	}
	if f > maxMinSectionBytes {
		return maxMinSectionBytes
	}
	return int(f)
}

// cellAt maps a plot position to matrix coordinates. Heatmap row 0 is at the
// TOP edge (implot's convention), so the row index counts down from the top.
// Returns (-1, -1) when the position is off the grid.
func cellAt(x float64, y float64, rows int, cols int) (j int, i int) {
	j, i = -1, -1
	if x < 0 || y < 0 || x >= float64(cols) || y >= float64(rows) {
		return
	}
	j = int(math.Floor(x))
	i = rows - 1 - int(math.Floor(y))
	if i < 0 || i >= rows || j < 0 || j >= cols {
		return -1, -1
	}
	return
}

// colTickValues places one tick at the centre of each column.
func colTickValues(cols int) (vs []float64) {
	vs = make([]float64, cols)
	for j := range vs {
		vs[j] = float64(j) + 0.5
	}
	return
}

// rowTickValues places one tick at the centre of each row, counting down from
// the top so tick k names section k.
func rowTickValues(rows int) (vs []float64) {
	vs = make([]float64, rows)
	for i := range vs {
		vs[i] = float64(rows-1-i) + 0.5
	}
	return
}

// sectionLabels returns elided section titles, or nil when there are too many
// to place without overlapping.
func sectionLabels(secs []Section, n int) (labels []string) {
	if n > maxTickLabels || n != len(secs) {
		return nil
	}
	labels = make([]string, n)
	for i, s := range secs {
		labels[i] = elide(s.Label(), tickLabelMax)
	}
	return
}

// applyTicks installs named ticks, or leaves the axis on its default numeric
// ticks when there is no label set.
func applyTicks(p *implot.Plot, axis implot.AxisE, values []float64, labels []string) {
	if len(labels) == 0 || len(labels) != len(values) {
		return
	}
	p.SetupAxisTicks(axis, values, labels)
}

// elide shortens s to at most n characters, ending in an ellipsis when cut.
// Counts runes, so a multi-byte title is not cut mid-character.
func elide(s string, n int) (out string) {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(rs[:n-1]) + "…"
}

// ecdfGrid evaluates the empirical CDF of a sorted sample at n equally spaced
// points spanning the sample. The result is monotone non-decreasing in [0, 1],
// which is what the ecdf widget's grid path requires.
func ecdfGrid(sorted []float64, n int) (xs []float64, fnAt []float64) {
	if len(sorted) == 0 || n < 2 {
		return
	}
	lo, hi := sorted[0], sorted[len(sorted)-1]
	if !(lo < hi) {
		return
	}
	xs = make([]float64, n)
	fnAt = make([]float64, n)
	for k := range xs {
		x := lo + (hi-lo)*float64(k)/float64(n-1)
		xs[k] = x
		fnAt[k] = float64(sort.SearchFloat64s(sorted, math.Nextafter(x, math.Inf(1)))) / float64(len(sorted))
	}
	return
}

// quantileOf returns the q-quantile of a sorted sample by nearest rank.
func quantileOf(sorted []float64, q float64) (v float64) {
	n := len(sorted)
	if n == 0 {
		return math.NaN()
	}
	idx := int(q * float64(n))
	if idx >= n {
		idx = n - 1
	}
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

// markValues collects the ranked pairs' distances for the ECDF's marker lines,
// de-duplicated so identical scores do not stack into one heavy line.
func markValues(pairs []Pair) (vs []float64) {
	const maxMarks = 8
	seen := make(map[float64]bool, maxMarks)
	for _, p := range pairs {
		if len(vs) >= maxMarks {
			break
		}
		if seen[p.Ncd] {
			continue
		}
		seen[p.Ncd] = true
		vs = append(vs, p.Ncd)
	}
	return
}

// reversedPalette flips a colormap palette end for end, so the low end of the
// data range gets what was the high end's colour.
func reversedPalette(palette []uint32) (out []uint32) {
	out = make([]uint32, len(palette))
	for i, v := range palette {
		out[len(palette)-1-i] = v
	}
	return
}

// percentText renders a fraction in [0, 1] as a percentage, keeping enough
// digits to distinguish the extreme tail (where the interesting pairs live)
// from merely-low values.
func percentText(f float64) (s string) {
	if math.IsNaN(f) {
		return "—"
	}
	switch {
	case f <= 0:
		return "0%"
	case f < 0.001:
		return fmt.Sprintf("%.3f%%", f*100)
	case f < 0.01:
		return fmt.Sprintf("%.2f%%", f*100)
	default:
		return fmt.Sprintf("%.1f%%", f*100)
	}
}

// humanMs renders a sweep duration compactly.
func humanMs(ms float64) (s string) {
	if ms < 1000 {
		return fmt.Sprintf("%.0f ms", ms)
	}
	return fmt.Sprintf("%.2f s", ms/1000)
}

// convergedNote says whether the instance sweep stopped early.
func convergedNote(converged bool) (s string) {
	if converged {
		return " (stopped early — the statistic stabilised)"
	}
	return ""
}

// bandLine names the band that is actually on screen.
func bandLine(exact bool) (s string) {
	if exact {
		return "Shaded: 95% simultaneous Berk-Jones band. It assumes independent observations; these pairs share sections and are not independent, so read it as a scale reference."
	}
	return "Shaded: the wider closed-form (DKW) band, drawn while the exact one warms. Same caveat — the pairs are not independent."
}

// bandNote appends the warm-up's own message when it has one.
func bandNote(snap ecdf.BandJobSnapshot) (s string) {
	if snap.Err != nil {
		return " — " + snap.Err.Error()
	}
	if snap.Note != "" {
		return " — " + snap.Note
	}
	if snap.EtaMs > 0 {
		return fmt.Sprintf(" — about %s left", humanMs(float64(snap.EtaMs)))
	}
	return ""
}

// metric renders a label carrying a hover tooltip — used for every displayed
// quantity so the reader can learn what each part means. (terrainscope idiom.)
func metric(text string, tip string) {
	for range c.HoverText(tip).KeepIter() {
		c.Label(text).Send()
	}
}
