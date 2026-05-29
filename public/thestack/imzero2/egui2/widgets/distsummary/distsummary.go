//go:build llm_generated_opus47

// Package distsummary implements a two-level summarisation widget for a
// single statistical distribution.
//
//   - Level 1 (anchor): a compact inline label — chart-line icon + the
//     5-number summary (n, min, Q1, median, Q3, max) in monospace —
//     paired with the standard [inspector.AnchorToggle] glyph. Every
//     distsummary instance carries the toggle by default; there is no
//     opt-in.
//   - Level 2 (inspector window): a draggable [c.Window] containing a
//     two-tab body — ECDF + simultaneous confidence band (default, via
//     widgets/ecdf bridged through widgets/ecdfdigest) and the
//     scientifically correct letter-value plot (widgets/boxenplot) —
//     plus the standard [inspector.ProvenanceChip], opened by clicking
//     the toggle and closed by clicking it again or the window's
//     title-bar X. The active tab is held per-instance in
//     [instanceStates]; tabs share the same plot extent so swapping
//     does not reflow the window. A bezier connector (via
//     [inspector.AnchorTether]) visually tethers the toggle to the
//     open window. When the digest is too sparse for an ECDF
//     (Count==0 or Min==Max) the ECDF tab silently falls back to the
//     boxenplot body for the current frame; the user's tab choice is
//     preserved so the ECDF returns automatically once the digest
//     recovers.
//
// Each idPrefix names one logical distsummary instance: the pinned
// open/closed state is held in a package-level state map keyed by
// idPrefix, so the value-receiver / fluent-builder pattern stays
// intact and callers don't have to thread a *bool through every Render
// call. Multiple instances on screen must use distinct idPrefixes so
// their pinned-state slots, AnchorToggle ids, window ids, and bezier
// rect-capture seqs don't collide.
//
// Caller passes a *tdigest.TDigest as the data source. The widget
// derives the level-1 summary directly from the digest (Count + Min +
// Max + Quantile(0.25/0.5/0.75)). For level 2 it forwards the digest
// (as a letterval.QuantileOracle) plus the optional extremes slice
// to the embedded boxenplot.Renderer, and bridges the same digest to
// the embedded ecdf.Renderer through [ecdfdigest.BuildDigestGrid] so
// both tabs share the single tdigest sketch the caller owns — no
// duplicate accumulation, no API change at the call site.
package distsummary

import (
	"strconv"
	"sync"

	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/boxenplot"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdfdigest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
)

// tabE selects which body the inspector window renders.
type tabE uint8

const (
	// tabECDF is the zero value so a fresh instanceState lands on the
	// ECDF tab without an explicit initialiser.
	tabECDF tabE = iota
	tabBoxenplot
)

// defaultEcdfGridN is the per-call grid resolution forwarded to
// [ecdfdigest.RenderDigest]. 128 samples keep the band's
// Moscovich-Nadler inversion well under a frame budget while still
// producing a visually smooth ECDF — coarser grids (e.g. 32) start
// showing the piecewise-linear segments the grid path emits in place
// of the canonical step function.
const defaultEcdfGridN = 128

// FormatFunc converts one of the summary's float values into a display
// string for the level-1 label. The default uses strconv.FormatFloat
// with verb 'g' and 4 significant digits — terse and human-readable
// across unbounded value ranges.
type FormatFunc func(float64) string

// Renderer is the configured distsummary widget. Values are immutable
// after construction; fluent setters return modified copies. Per-instance
// pinned-window state is held in the [instanceStates] package map keyed
// by idPrefix, not on this struct — so the value-receiver / fluent
// chain pattern can stay intact while still giving every Renderer a
// persistent inspector toggle.
type Renderer struct {
	idPrefix    string
	popupWidth  float32
	popupHeight float32
	popupPad    float32
	showN       bool
	showIcon    bool
	formatFunc  FormatFunc
	plot        boxenplot.Renderer
	ecdfPlot    ecdf.Renderer
	// gridN is the uniform sample count forwarded to
	// [ecdfdigest.RenderDigest] when rendering the ECDF tab. Clamped
	// to ≥ 2 by the bridge but tuned higher (default
	// [defaultEcdfGridN]) so the band reads as continuous.
	gridN int

	// provenance, when non-zero, renders the standard
	// [inspector.ProvenanceChip] inside the inspector window's body so
	// operators can see which subject / source-app produced the
	// distribution this widget is summarising. Zero value (default)
	// suppresses the chip — pure method-arg digest plots (the common
	// case in tests + ad-hoc dashboards) leave the inspector window
	// header free of provenance chrome.
	provenance inspector.Provenance

	// tasks, when non-nil, is the keelson task API the embedded ECDF
	// widget uses to warm its confidence band on a background job
	// (ADR-0038), so a large-n O(n²) inversion runs off the render
	// thread, shows in the supervisor / taskmonitor, and cancels on
	// window close. nil (default) still computes the band off-thread via
	// the in-process job registry; only task-framework visibility is
	// lost. Set via Tasks.
	tasks task.TaskApiI
}

// instanceState carries the per-distsummary pinned-window open flag
// and the operator's last-selected inspector tab. Lives in the
// package-level [instanceStates] map keyed by idPrefix so the
// value-receiver Renderer can pretend to be stateless while still
// driving a real per-instance toggle. The zero value lands on
// [tabECDF] so a freshly opened window honours the documented
// "ECDF is default" contract without an explicit initialiser.
type instanceState struct {
	pinned bool
	tab    tabE
}

// instanceStates is the package-level pinned-state map, keyed by
// idPrefix. One entry per unique idPrefix ever rendered, never
// reclaimed — acceptable for typical app shapes (dozens of unique
// distsummary surfaces); apps that dynamically mount/unmount
// short-lived distsummary instances with one-shot idPrefixes leak
// O(mounts) memory. Document but don't engineer for that yet.
//
// Per memory entry composite_widget_state: composite widgets keep
// state on a stable identity (receiver pointer or IdScope). This map
// is the IdScope variant for a value-receiver widget.
var instanceStates sync.Map // map[string]*instanceState

func getInstanceState(idPrefix string) *instanceState {
	actual, _ := instanceStates.LoadOrStore(idPrefix, &instanceState{})
	return actual.(*instanceState)
}

// New constructs a Renderer with IDS-aligned defaults:
//
//   - popup size: 320×200 (compact letter-value plot)
//   - popup padding: 4 px (small visual breathing room around the
//     plot; the previous 12 px halo existed to absorb egui_plot's
//     auto-sized box-hover overlay — the boxenplot widget now
//     suppresses that overlay and renders a fixed status line
//     beneath the plot instead, so the halo no longer needs to
//     compensate for tooltip clipping)
//   - n is shown
//   - chart-line icon (Phosphor) is shown
//   - format uses strconv.FormatFloat('g', 4)
//   - embedded boxenplot uses its own defaults; override via Boxenplot.
//   - embedded ECDF uses [ecdf.New] defaults (Berk-Jones, α=0.05);
//     override via Ecdf. Grid resolution defaults to
//     [defaultEcdfGridN]; override via GridN.
//
// idPrefix scopes any widget-id-bearing primitive emitted by Render —
// pass a stable short string (e.g. "lat-cluster", "p99-mem").
func New(idPrefix string) (inst Renderer) {
	inst = Renderer{
		idPrefix:    idPrefix,
		popupWidth:  320,
		popupHeight: 200,
		popupPad:    4,
		showN:       true,
		showIcon:    true,
		formatFunc:  defaultFormat,
		plot:        boxenplot.New(idPrefix + "-bp"),
		ecdfPlot:    ecdf.New(),
		gridN:       defaultEcdfGridN,
	}
	return
}

// Boxenplot replaces the embedded level-2 Renderer with a caller-supplied
// configuration. Use this to swap palette, outlier mode, or box width
// without forking the distsummary builder.
func (inst Renderer) Boxenplot(bp boxenplot.Renderer) (out Renderer) {
	inst.plot = bp
	out = inst
	return
}

// Ecdf replaces the embedded ECDF Renderer used by the default tab in
// the inspector window. Use this to swap the band family
// (Berk-Jones / DKW / equal-precision / higher-criticism), the
// confidence level (alpha), stroke / fill colours, or the series
// label without forking the distsummary builder.
func (inst Renderer) Ecdf(r ecdf.Renderer) (out Renderer) {
	inst.ecdfPlot = r
	out = inst
	return
}

// GridN sets the uniform sample count used when bridging the digest
// to the ECDF widget via [ecdfdigest.RenderDigest]. Values below 2
// are clamped to [defaultEcdfGridN] so the inspector never renders a
// degenerate two-point grid by accident; pass a larger value for
// smoother bands on heavy-tailed distributions, smaller for cheaper
// per-frame inversion when many distsummary windows are open.
func (inst Renderer) GridN(n int) (out Renderer) {
	if n < 2 {
		n = defaultEcdfGridN
	}
	inst.gridN = n
	out = inst
	return
}

// Tasks wires a keelson task API so the embedded ECDF widget warms its
// confidence band on a background job (ADR-0038) rather than blocking
// the render thread, surfacing the work in the supervisor and any
// taskmonitor panel and cancelling it when the host window closes.
// Optional: when unset the band still computes off-thread via the
// in-process job registry — only task-framework visibility is lost.
func (inst Renderer) Tasks(api task.TaskApiI) (out Renderer) {
	inst.tasks = api
	out = inst
	return
}

// PopupSize sets the level-2 plot's width and height in points. The
// tooltip envelope grows to roughly (w+2*pad, h+2*pad) plus the bottom
// status line so the plot keeps the requested extent. Default 320×200.
func (inst Renderer) PopupSize(w, h float32) (out Renderer) {
	inst.popupWidth = w
	inst.popupHeight = h
	out = inst
	return
}

// PopupPadding sets the visual breathing room in points added around
// the plot inside the inspector window body. Negative values are
// clamped to 0. Default 4. Larger values used to be needed to absorb
// egui_plot's auto-sized box-hover overlay before it clipped at the
// tooltip edge — that overlay is now suppressed by the boxenplot
// widget and the readout lives in the bottom status line, so the
// halo is purely cosmetic.
func (inst Renderer) PopupPadding(p float32) (out Renderer) {
	if p < 0 {
		p = 0
	}
	inst.popupPad = p
	out = inst
	return
}

// ShowN toggles the `n=<count>` term in the level-1 label. Default true.
func (inst Renderer) ShowN(b bool) (out Renderer) {
	inst.showN = b
	out = inst
	return
}

// ShowIcon toggles the chart-line affordance icon. Default true.
func (inst Renderer) ShowIcon(b bool) (out Renderer) {
	inst.showIcon = b
	out = inst
	return
}

// Format replaces the per-value formatter. Useful for unit-suffixed
// values, fixed precision, or domain-specific rounding. Passing nil
// is a no-op (the existing formatter is retained).
func (inst Renderer) Format(f FormatFunc) (out Renderer) {
	if f != nil {
		inst.formatFunc = f
	}
	out = inst
	return
}

// Provenance binds the distribution to its source value's
// [inspector.Provenance] identity card. When set (non-zero), the
// inspector window renders the standard [inspector.ProvenanceChip]
// above the tab bar so operators can see which subject / source-app
// produced the digest this widget is summarising. Zero value
// (default) suppresses the chip — receiver-owned digests without an
// external binding leave the inspector window header unchanged.
func (inst Renderer) Provenance(p inspector.Provenance) (out Renderer) {
	inst.provenance = p
	out = inst
	return
}

// Render emits the level-1 inline label paired with the standard
// [inspector.AnchorToggle]. Clicking the toggle opens the inspector
// window containing the ECDF / Boxenplot tab body and the optional
// provenance chip; clicking the toggle again or the window's
// title-bar X closes it. A bezier connector ties the toggle to the
// open window via [inspector.AnchorTether]. The pinned open/closed
// state and the operator's last-selected tab are held in the
// package-level [instanceStates] map keyed by idPrefix so they
// survive across Render calls without forcing a pointer-receiver API.
//
//   - idGen is consumed exactly once via [c.WidgetIdCreatorI.Derive]
//     so the caller's WidgetIdStack state-machine contract
//     (Initial → Prepared → Initial) holds in one hop. The inset Plot
//     uses an absolute id derived from idPrefix instead, because the
//     boxenplot crosshair lookup needs a stable AbsoluteWidgetId that
//     matches the r15 hover register's stored value across frames
//     (a stack-prepared id XORs the surrounding stack top and would
//     silently miss the match). Toggle and window ids are derived the
//     same way and likewise do not consume idGen.
//   - digest is the streaming oracle backing both levels. If nil or
//     empty (Count() == 0) the widget renders a "(no data)" placeholder
//     in place of the summary and still consumes idGen (single
//     Derive) so the caller's state-machine invariant is preserved
//     across all branches.
//   - extremes is forwarded to the boxenplot for OutlierModePoints;
//     pass nil for other outlier modes.
func (inst Renderer) Render(idGen c.WidgetIdCreatorI, digest *tdigest.TDigest, extremes []float64) {
	// Consume idGen once at the top so the WidgetIdStack state-machine
	// contract (Initial → Prepared → Initial) is honoured uniformly
	// across all branches below. The derived value is also the per-
	// call-site scope disambiguator (see [callScope]).
	callId := idGen.Derive()
	summary := computeFiveNumberSummary(digest)
	label := formatSummary(summary, inst.showN, inst.showIcon, inst.formatFunc)

	if summary.n == 0 {
		c.LabelAtoms(
			c.Atoms().BeginRichText(label).Monospace().Weak().End().Keep(),
		).Send()
		return
	}

	// Per-call scope: combines the developer-supplied idPrefix with
	// the caller's per-call-site idGen. Two .Render(...) invocations
	// from the same Renderer (sccmap's size + color, imztop's cores +
	// history) pass distinct idGens and therefore see distinct scopes
	// — so their toggle ids, window ids, bezier rect-capture seqs,
	// and pinned-state map slots are all independent. Without this
	// disambiguation those calls collided on the same toggle id and
	// egui's interaction system silently dropped both clicks.
	scope := callScope(inst.idPrefix, callId)
	state := getInstanceState(scope)
	labelAtoms := c.Atoms().BeginRichText(label).Monospace().End().Keep()
	tether := inspector.NewAnchorTether(scope)
	toggleId := c.MakeAbsoluteIdStr(scope + "-anchor-toggle")
	for range c.Horizontal().KeepIter() {
		c.LabelAtoms(labelAtoms).Send()
		inspector.AnchorToggle(toggleId, &state.pinned)
		tether.CaptureToggle()
	}

	if !state.pinned {
		return
	}
	inst.renderPinnedWindow(scope, tether, state, digest, extremes)
	tether.Paint()
}

// callScope combines the developer-supplied idPrefix with the per-call
// disambiguator derived from idGen. Format: "idPrefix#<hex>". Stable
// across frames for the same call site (idGen.Derive is deterministic
// on the same prepared id under the same surrounding IdScope), so the
// derived toggle / window / state ids stay put while still being
// unique across multiple .Render(...) calls on the same Renderer.
func callScope(idPrefix string, callId uint64) string {
	return idPrefix + "#" + strconv.FormatUint(callId, 16)
}

// renderLevel2Body emits the inspector body inside the pinned window.
// Layout from top to bottom: provenance chip (when bound) → tab bar
// (ECDF / Boxenplot) → tab-specific body. All widget ids are scoped
// by the per-call `scope` so two .Render(...) calls on the same
// Renderer produce independent plot / crosshair / tab-selector
// lookup keys.
//
// The ECDF tab falls back to the boxenplot body for the current
// frame when the digest cannot support an ECDF (Count == 0 or
// support collapsed to a single value); state.tab is left untouched
// so the ECDF returns the moment the digest broadens again.
func (inst Renderer) renderLevel2Body(scope string, state *instanceState, digest *tdigest.TDigest, extremes []float64) {
	if !inst.provenance.IsZero() {
		inspector.ProvenanceChip(inst.provenance)
		c.Separator().Horizontal().Send()
	}
	inst.renderTabBar(scope, state)
	c.Separator().Horizontal().Send()
	switch state.tab {
	case tabBoxenplot:
		inst.renderBoxenplotBody(scope, digest, extremes)
	default:
		if !inst.renderEcdfBody(scope, digest) {
			inst.renderBoxenplotBody(scope, digest, extremes)
		}
	}
}

// renderTabBar emits the two-tab selector controlling which body the
// inspector window shows. SelectableLabels carry an AbsoluteWidgetId
// derived from `scope` so multiple .Render(...) calls on the same
// Renderer (sccmap's size + color, imztop's cores + history) drive
// independent tab state without an idGen thread.
func (inst Renderer) renderTabBar(scope string, state *instanceState) {
	density := styletokens.DensityFromEnv()
	gap := styletokens.GapInline(density)
	ecdfID := c.MakeAbsoluteIdStr(scope + "-tab-ecdf")
	bpID := c.MakeAbsoluteIdStr(scope + "-tab-boxenplot")
	for range c.Horizontal().KeepIter() {
		if c.SelectableLabel(ecdfID, state.tab == tabECDF, "ECDF").
			SendResp().HasPrimaryClicked() {
			state.tab = tabECDF
		}
		c.AddSpace(gap)
		if c.SelectableLabel(bpID, state.tab == tabBoxenplot, "Boxenplot").
			SendResp().HasPrimaryClicked() {
			state.tab = tabBoxenplot
		}
	}
}

// renderBoxenplotBody emits the letter-value plot body — the legacy
// inspector content extracted into its own helper so the tab switch
// in [renderLevel2Body] reads as a flat case dispatch.
//
// Order matters: the boxenplot.Render call stages the letter-value
// series in-memory; c.Plot then opens the egui_plot block that
// actually draws them. The crosshair At() must run before Render so
// PaintCrosshair can layer the vline above the boxes inside the same
// plot block. The plot id is an AbsoluteWidgetId so its r15-hover
// lookup matches across frames (a stack-prepared id would XOR the
// surrounding stack top in and silently miss).
func (inst Renderer) renderBoxenplotBody(scope string, digest *tdigest.TDigest, extremes []float64) {
	plotID := c.MakeAbsoluteIdStr(scope + "-bp-plot")
	levels := letterval.RecommendedLevels(digest)
	// The boxenplot's argument axis is hidden, so any single x
	// position works — 0.0 keeps the median annotation centred.
	ch := inst.plot.At(plotID, 0.0, inst.idPrefix, levels)
	inst.plot.Render(0.0, levels, extremes, -1)
	inst.plot.PaintCrosshair(ch)
	pad := inst.popupPad
	if pad > 0 {
		c.AddSpace(pad)
	}
	for range c.Horizontal().KeepIter() {
		if pad > 0 {
			c.AddSpace(pad)
		}
		c.Plot(plotID).
			Width(inst.popupWidth).
			Height(inst.popupHeight).
			YAxisLabel("value").
			ShowAxes(false, true).
			ShowGrid(false, true).
			ShowBackground(false).
			AllowZoom(false).
			AllowDrag(false).
			AllowScroll(false).
			IncludeX(-0.6).
			IncludeX(0.6).
			Send()
		if pad > 0 {
			c.AddSpace(pad)
		}
	}
	if pad > 0 {
		c.AddSpace(pad)
	}
	boxenplot.WriteStatusLine(ch)
}

// renderEcdfBody emits the ECDF + simultaneous confidence band body.
// Returns false when the digest is too sparse for an ECDF (nil,
// Count == 0, or support collapsed to a single value); the caller
// then renders the boxenplot body in the same frame as a graceful
// fallback. The plot id is a distinct AbsoluteWidgetId from the
// boxenplot's so the r15 hover register stays per-tab and a cached
// hover from the previous tab does not surface here as a stale
// crosshair.
//
// Order matters the same way as [renderBoxenplotBody]: At()
// snapshots the hover before Render stages the band rectangles +
// ECDF polyline, and PaintCrosshair piggybacks on the same plot
// block so the vline lands on top of band and curve.
func (inst Renderer) renderEcdfBody(scope string, digest *tdigest.TDigest) (rendered bool) {
	if digest == nil || digest.Count() == 0 {
		return
	}
	xmin := digest.Min()
	xmax := digest.Max()
	if !(xmax > xmin) {
		return
	}
	plotID := c.MakeAbsoluteIdStr(scope + "-ecdf-plot")
	n := int(digest.Count())
	xs, fn := ecdfdigest.BuildDigestGrid(digest, inst.gridN)

	// The simultaneous band needs an O(n²) critical-value inversion far
	// too slow for the render thread at large n (≈minutes at n=1e4).
	// When the band for this (n, α, method) is already cached we draw it
	// directly; otherwise draw the ECDF curve immediately, warm the band
	// on a keelson background job (ADR-0038), and show its progress + ETA
	// below the plot. A later frame finds the cache warm and renders the
	// full band + hover crosshair.
	bandReady := inst.ecdfPlot.BandReady(n)
	var ch ecdf.Crosshair
	var job ecdf.BandJobSnapshot
	if bandReady {
		ch = inst.ecdfPlot.AtGrid(plotID, xs, fn, n)
		_ = inst.ecdfPlot.RenderGrid(xs, fn, n)
		inst.ecdfPlot.PaintCrosshair(ch)
	} else {
		job = inst.ecdfPlot.EnsureBandJob(inst.tasks, n)
		inst.ecdfPlot.RenderGridCurveOnly(xs, fn)
	}
	pad := inst.popupPad
	if pad > 0 {
		c.AddSpace(pad)
	}
	for range c.Horizontal().KeepIter() {
		if pad > 0 {
			c.AddSpace(pad)
		}
		c.Plot(plotID).
			Width(inst.popupWidth).
			Height(inst.popupHeight).
			XAxisLabel("value").
			YAxisLabel("F(x)").
			ShowGrid(true, true).
			AllowZoom(true).
			AllowDrag(false).
			AllowScroll(false).
			IncludeY(0).
			IncludeY(1).
			ClampX(xmin, xmax).
			ClampY(0, 1).
			Send()
		if pad > 0 {
			c.AddSpace(pad)
		}
	}
	if pad > 0 {
		c.AddSpace(pad)
	}
	if bandReady {
		ecdf.WriteStatusLine(ch)
	} else {
		switch job.State {
		case ecdf.BandJobError:
			c.Label("confidence band unavailable: " + job.Note).Send()
		case ecdf.BandJobRunning:
			jobprogress.Render(jobprogress.Input{
				Title:    "computing confidence band",
				Fraction: job.Fraction,
				EtaMs:    job.EtaMs,
			})
		}
	}
	rendered = true
	return
}

// renderPinnedWindow emits the c.Window holding the inspector body
// when the instance's pinned flag is true. Native title-bar X is wired
// to that same flag via OpenBound + R10 databinding (fsmview pattern
// at widget.go:287-298) so closing the window through egui's chrome
// flips the toggle the same way clicking the anchor would. The
// tether's [inspector.AnchorTether.CaptureWindow] runs at the top of
// the body so the bezier "to" endpoint anchors on the window's content
// rect (title bar excluded). The window id is scoped by the per-call
// `scope`, not by idPrefix alone, so two .Render(...) calls on the
// same Renderer open two independent windows.
//
// Window envelope adds title-bar + frame headroom plus a tab-bar
// budget around the (popupWidth + 2*pad) × (popupHeight + 2*pad)
// body so the first-open size fits the plot and the tab selector
// without immediate user resize. tabBarBudget covers one row of
// SelectableLabel + an inline separator at the IDS standard
// densities; if a future redesign grows the tab bar, bump the
// constant rather than threading density into the envelope calc.
func (inst Renderer) renderPinnedWindow(scope string, tether inspector.AnchorTether, state *instanceState, digest *tdigest.TDigest, extremes []float64) {
	const tabBarBudget float32 = 32
	winId := c.MakeAbsoluteIdStr(scope + "-anchor-window")
	title := "distribution: " + inst.idPrefix
	envW := inst.popupWidth + 2*inst.popupPad + 24
	envH := inst.popupHeight + 2*inst.popupPad + 40 + tabBarBudget
	win := c.Window(winId, c.WidgetText().Text(title).Keep()).
		DefaultOpen(true).
		Resizable(true).
		Collapsible(false).
		AlwaysOnTop(true).
		DefaultSize(envW, envH)
	bindId := win.Id()
	win = win.OpenBound(bindId)
	c.CurrentApplicationState.StateManager.AddR10Databinding(bindId, &state.pinned)
	for range win.KeepIter() {
		tether.CaptureWindow()
		inst.renderLevel2Body(scope, state, digest, extremes)
	}
}
