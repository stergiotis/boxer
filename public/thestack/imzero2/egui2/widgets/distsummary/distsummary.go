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
//     [instanceStates]; the window keeps a fixed size across tab swaps
//     — the ECDF curve fills the content width while the narrow
//     letter-value plot stays centred — so swapping does not reflow the
//     window. A bezier connector (via
//     [inspector.AnchorTether]) visually tethers the toggle to the
//     open window. When the digest is too sparse for an ECDF
//     (Count==0 or Min==Max) the ECDF tab silently falls back to the
//     boxenplot body for the current frame; the user's tab choice is
//     preserved so the ECDF returns automatically once the digest
//     recovers.
//
// The ECDF tab clips a long tail adaptively per-side (a quantile cutoff,
// engaged only when a tail is long relative to the IQR — see
// [tailClipBounds] / [Renderer.TailClip]) so a heavy-tailed distribution's
// body fills the plot instead of being crushed into the left edge, and
// annotates the hidden tail below the curve. A fixed-height verbose
// readout below the plot describes the cursor's F(x) reading and its
// confidence interval, and an always-visible band-state line names the
// band (exact family + calibration n, or the conservative DKW preview),
// flagging it conservative when the calibration n lags the true count —
// which also lets a live or recomputed inspector's exact band settle
// rather than restart its solve every frame (see [bucketExactN]).
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
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
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

// defaultEcdfPlotWidth is the level-2 window's first-open content width in
// points. The default tab is the ECDF, whose body fills the window's
// content width; opening at this width rather than the boxenplot-sized
// popupWidth gives egui_plot the horizontal room it needs to draw x-axis
// tick labels. egui_plot culls any X label whose inter-mark pixel spacing
// falls under its 60 px minimum (axis.rs add_tick_labels), so at popupWidth
// (320) a wide-range distribution renders grid lines with no numbers under
// them. The user can still resize; the ECDF tracks the new width and the
// narrow boxenplot tab stays centred in whatever room there is.
const defaultEcdfPlotWidth float32 = 560

// ecdfPlotChromeW is the horizontal slack the ECDF body leaves between the
// captured content width and the plot's requested Width. The plot is rendered
// inside a Horizontal row flanked by two popupPad AddSpace insets, and egui
// inserts an item_spacing (≈8 pt) between each of the row's items; a plot sized
// to exactly avail.W-2*pad therefore makes the row measure those ~2 spacings
// WIDER than the avail.W it was derived from every frame. The resizable
// inspector Window grows to fit that overflow, which re-inflates next frame's
// captured avail.W, which re-enlarges plotW — the monotonic host-window
// auto-grow loop sccmap's treemapChromeW guards the same way. Sized to cover
// the two item_spacings with a little margin so the row fits strictly inside
// avail.W and the window stops growing.
const ecdfPlotChromeW float32 = 18

// ecdfPlotGrowGuardPx is the anti-ratchet deadband on the ECDF plot width.
// Frame-over-frame upward deltas smaller than this are almost always
// [ecdfPlotChromeW] mis-estimates rather than user intent and would resurrect
// the growth loop, so they are clamped to the previous applied width; a real
// resize moves in larger steps and passes through, as do downward deltas
// (window shrink, tab/close reset). Belt-and-suspenders behind the chrome
// budget — matches sccmap's containerGrowGuardPx.
const ecdfPlotGrowGuardPx float32 = 24

// ecdfYTickVals / ecdfYTickLabels pin the F(x) axis to quarter-point ticks.
// egui_plot's default logarithmic grid spacer only lands marks on powers of
// ten, so over the CDF's [0,1] range at the inspector's height it labels
// just 0 and 1; the explicit quartile marks keep the probability axis
// readable independent of window height. Forwarded via [c.PlotFluid.YGridMarks].
var (
	ecdfYTickVals   = []float64{0, 0.25, 0.5, 0.75, 1}
	ecdfYTickLabels = []string{"0", "0.25", "0.5", "0.75", "1"}
)

// bandWarmRepaintIntervalSecs keeps frames flowing while the confidence
// band is unsettled — warming on a background goroutine, or just-cancelled
// and offering Compute. The warm-up goroutine advances the progress
// snapshot with no input event, so absent an explicit repaint request the
// bar stalls; and because button responses carry a one-frame lag, the
// inline Cancel/Compute click is read only on a following frame, which
// under reactive render cadence (IMZERO2_RENDER_CADENCE=reactive) arrives
// only on the next user input — so a single click appears to do nothing.
// Requesting a near-term repaint each unsettled frame makes the progress
// animate and both clicks land promptly regardless of host cadence, and
// stops once the band is cached (bandReady) so a settled inspector falls
// back to the idle heartbeat. 0.05s (20fps) mirrors the background-progress
// pattern in egui2_hl_progressbar_demo.go — smooth for a progress bar, well
// below vsync so it costs little.
const bandWarmRepaintIntervalSecs = 0.05

// exactBandAutoMaxN is the largest effective (post-cap) sample size at
// which renderEcdfBody auto-requests the exact confidence band when the
// inspector opens, instead of waiting for a "Compute exact band" click.
// The exact O(n²) inversion runs ~2 s at n≈500 on commodity hardware and
// grows quadratically (≈8 s at 1000, minutes past a few thousand), so 500
// keeps the auto path snappy while everything larger stays an explicit
// opt-in — the instant DKW preview band covers the gap meanwhile.
const exactBandAutoMaxN = 500

// defaultTailLowerP / defaultTailUpperP are the per-side quantile cutoffs
// the ECDF x-view clips to when a tail is long enough to trigger clipping
// (see [tailClipBounds]). p0.1 / p99.9 hide only the extreme 0.1% on a
// clipped side — enough to recover a heavy-tailed body without dropping
// visible structure. Tunable via [Renderer.TailClip].
const (
	defaultTailLowerP = 0.001
	defaultTailUpperP = 0.999
)

// defaultTailTriggerIQR is the tail-length-over-IQR ratio past which a
// side is clipped — Tukey's "far out" 3×IQR multiple, used here only as
// the trigger (the cutoff itself is a quantile). A side whose extreme
// lies within 3·IQR of its quartile is left unclipped, so a well-behaved
// distribution renders full-range. Tunable via [Renderer.TailTrigger].
const defaultTailTriggerIQR = 3.0

// defaultExactBandBucketRatio is the geometric step [bucketExactN] rounds
// the exact-band n down to so a drifting sample size reuses a cached
// solve instead of restarting it every frame. 1.25 caps the resulting
// band conservatism at √1.25 ≈ 1.12 (~12% wider half-width worst case)
// while coarsening enough to let live / recomputed inspectors settle.
// Tunable (or disabled, ratio ≤ 1) via [Renderer.ExactBandBucket].
const defaultExactBandBucketRatio = 1.25

// ecdfStatusBudget is the vertical room (points) the ECDF tab's status
// area needs below the plot in the first-open window envelope: the
// Reset-zoom button, the optional hidden-tail note, the band-state /
// controls row, and the fixed-height verbose cursor readout
// ([ecdf.ReadoutLineCount] rows). Sized generously so the readout is
// visible without an immediate resize; the window stays resizable.
const ecdfStatusBudget float32 = 140

// FormatFunc converts one of the summary's float values into a display
// string for the level-1 label. The default ([humanizeValue]) prints
// plain ~3-significant-figure decimals in the comfortable [0.001, 1000)
// band and switches to SI metric prefixes outside it (1.23k, 4.5M, 12µ)
// so the inline summary stays compact and never falls back to
// scientific notation. Override via [Renderer.Format] for unit suffixes
// or domain-specific precision.
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
	// inline suppresses the level-1 anchor's own [c.Horizontal] wrapper so
	// the label + toggle are emitted directly into the caller's row. A
	// centered horizontal nested inside another centered horizontal sits a
	// few px below its plain-widget siblings (the "Ragged Control Row" issue
	// in imzero2 SKILL.md), so a distsummary dropped into a status bar reads
	// as misaligned. Callers already inside a [c.Horizontal] set this via
	// [Renderer.Inline]; the default (false) keeps the wrapper so a
	// standalone Render in a vertical parent still lays the toggle beside
	// the label rather than beneath it.
	inline     bool
	formatFunc FormatFunc
	// unit, when non-empty, is written once after the last quantile value
	// (e.g. "fps", "ms", "MB") so the level-1 summary reads as a dimensioned
	// line. Empty (default) appends nothing. Set via Unit.
	unit     string
	plot     boxenplot.Renderer
	ecdfPlot ecdf.Renderer
	// gridN is the uniform sample count forwarded to
	// [ecdfdigest.RenderDigest] when rendering the ECDF tab. Clamped
	// to ≥ 2 by the bridge but tuned higher (default
	// [defaultEcdfGridN]) so the band reads as continuous.
	gridN int

	// exactBandMaxN caps the effective sample size at which the EXACT
	// confidence band's O(n²) critical value is computed (the DKW preview
	// is always drawn at the true n). Zero (default) means uncapped —
	// statistically exact but minutes-long past a few thousand points. A
	// positive cap keeps the opt-in solve tractable and cancellable at large
	// n by computing a slightly-conservative band calibrated at min(n, cap)
	// — the band converges as n grows, so the capped band is a tight
	// over-cover. Set via ExactBandMaxN; the demos cap it so the exact path
	// is demonstrable.
	exactBandMaxN int

	// tailClipEnabled / tailLowerP / tailUpperP / tailTriggerIQR configure
	// the ECDF x-view's adaptive, per-side tail cutoff (see [tailClipBounds]
	// and [Renderer.TailClip] / [Renderer.TailTrigger] / [Renderer.NoTailClip]).
	// Defaults: enabled, p0.1 / p99.9 cutoffs, 3×IQR trigger. The cutoff only
	// bounds the view — the band's calibration always uses the true count.
	tailClipEnabled bool
	tailLowerP      float64
	tailUpperP      float64
	tailTriggerIQR  float64

	// exactBandBucketRatio is the geometric ladder [bucketExactN] rounds the
	// exact-band n down to so a drifting sample size reuses a cached solve
	// instead of restarting it every frame — what lets a live / recomputed
	// inspector's exact band settle. Default [defaultExactBandBucketRatio];
	// ≤ 1 disables bucketing. Set via [Renderer.ExactBandBucket].
	exactBandBucketRatio float64

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
	// (ADR-0038), so a large-n O(n²) inversion runs off the render thread
	// and shows in the supervisor / taskmonitor. nil (default) still
	// computes the band off-thread via the in-process job registry — only
	// task-framework visibility and the host mount-cancel path are lost.
	// Either way the warm-up is cancelled when this inspector closes or
	// retracts (see Render). Set via Tasks.
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
	// exactRequested drives the ECDF tab's confidence-band detail under the
	// progressive-quality scheme (see renderEcdfBody): false draws only the
	// instant closed-form DKW preview band and offers a "Compute exact band"
	// affordance; true warms — and then shows — the tighter exact band (the
	// renderer's Method) with progress + a Cancel button. Set by Compute and
	// by the small-n auto-seed; cleared by Cancel and on inspector close.
	exactRequested bool
	// exactInit records whether exactRequested has been seeded for the
	// current open yet. The seed (renderEcdfBody) auto-requests the exact
	// band when its solve is cheap (small effective n) and leaves it opt-in
	// otherwise; it runs once per open and is reset on close so a reopen
	// re-evaluates against the current sample size.
	exactInit bool
	// lastEcdfPlotW caches the ECDF tab's last applied plot width so the
	// grow guard in renderEcdfBody (see [ecdfPlotGrowGuardPx]) can damp the
	// host Window's auto-grow loop. 0 until the first ECDF frame; reset on
	// close so a reopen re-fills from the popupWidth floor.
	lastEcdfPlotW float32
	// ecdfResetReq is a one-frame latch set by the ECDF tab's "Reset zoom"
	// button and consumed by the plot block (forwarded to ResetBounds), which
	// re-fits the curve to the data. Cleared on close.
	ecdfResetReq bool
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
//   - format humanizes values with SI metric prefixes outside the
//     [0.001, 1000) band (1.23k, 4.5M, 12µ); override via Format
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
		formatFunc:  humanizeValue,
		plot:        boxenplot.New(idPrefix + "-bp"),
		ecdfPlot:    ecdf.New(),
		gridN:       defaultEcdfGridN,

		tailClipEnabled:      true,
		tailLowerP:           defaultTailLowerP,
		tailUpperP:           defaultTailUpperP,
		tailTriggerIQR:       defaultTailTriggerIQR,
		exactBandBucketRatio: defaultExactBandBucketRatio,
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

// ExactBandMaxN caps the effective sample size at which the inspector's
// EXACT confidence band is computed (see [Renderer.exactBandMaxN]). The
// instant DKW preview band is always drawn at the true n; this only bounds
// the opt-in exact solve. Pass a positive cap (e.g. ~2000) to keep that
// solve tractable and cancellable on large samples at the cost of a
// slightly conservative band; pass 0 (default) for the statistically exact
// band at the true n, accepting an O(n²) solve that runs into minutes past
// a few thousand points. Values below 0 are treated as 0.
func (inst Renderer) ExactBandMaxN(n int) (out Renderer) {
	if n < 0 {
		n = 0
	}
	inst.exactBandMaxN = n
	out = inst
	return
}

// TailClip sets the per-side quantile cutoffs the ECDF x-view clips to
// when a tail triggers clipping, and (re-)enables clipping. lowerP /
// upperP are clamped to [0, 1] and swapped if mis-ordered; pass e.g.
// (0.001, 0.999) to hide the extreme 0.1% on a clipped side. Clipping
// stays adaptive — a side is trimmed only when its tail is long relative
// to the IQR (see [Renderer.TailTrigger]) — so this never trims a
// well-behaved distribution. Use [Renderer.NoTailClip] to turn it off.
func (inst Renderer) TailClip(lowerP, upperP float64) (out Renderer) {
	if upperP < lowerP {
		lowerP, upperP = upperP, lowerP
	}
	inst.tailLowerP = min(1, max(0, lowerP))
	inst.tailUpperP = min(1, max(0, upperP))
	inst.tailClipEnabled = true
	out = inst
	return
}

// TailTrigger sets the tail-length-over-IQR ratio past which the ECDF
// x-view clips a side (default [defaultTailTriggerIQR]). Larger clips less
// eagerly (only longer tails); ≤ 0 clips whenever the cutoff quantile lies
// strictly inside the support. Does not re-enable clipping if
// [Renderer.NoTailClip] turned it off.
func (inst Renderer) TailTrigger(iqrRatio float64) (out Renderer) {
	inst.tailTriggerIQR = iqrRatio
	out = inst
	return
}

// NoTailClip disables the ECDF x-view tail cutoff, restoring the full
// [Min, Max] range (the pre-cutoff behaviour): the grid spans the whole
// support again. The level-1 summary and band are unaffected either way.
func (inst Renderer) NoTailClip() (out Renderer) {
	inst.tailClipEnabled = false
	out = inst
	return
}

// ExactBandBucket sets the geometric step the exact-band n is rounded
// down to so a drifting sample size reuses a cached solve instead of
// restarting it every frame (see [bucketExactN]) — the mechanism that
// lets a live or repeatedly-recomputed inspector's exact band settle.
// Default [defaultExactBandBucketRatio]; a ratio ≤ 1 disables bucketing
// (the band is calibrated at the exact, capped n, which can thrash and
// never settle on a fast-growing digest — it then stays on the DKW
// preview, which the readout explains).
func (inst Renderer) ExactBandBucket(ratio float64) (out Renderer) {
	inst.exactBandBucketRatio = ratio
	out = inst
	return
}

// Tasks wires a keelson task API so the embedded ECDF widget warms its
// confidence band on a background job (ADR-0038) rather than blocking
// the render thread, surfacing the work in the supervisor and any
// taskmonitor panel. Optional: when unset the band still computes
// off-thread via the in-process job registry — only task-framework
// visibility (and the host mount-cancel path) is lost. The warm-up is
// cancelled when the inspector window closes or retracts regardless of
// this setting.
func (inst Renderer) Tasks(api task.TaskApiI) (out Renderer) {
	inst.tasks = api
	out = inst
	return
}

// PopupSize sets the level-2 plot extent in points. Height applies to both
// tabs. Width is the boxenplot tab's fixed width and the ECDF tab's lower
// bound — the ECDF curve fills the window's content width above that floor,
// and the window first opens at [defaultEcdfPlotWidth] (or w, whichever is
// larger) so the curve has room for axis ticks. Default 320×200.
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

// Inline emits the level-1 anchor (summary label + inspector toggle)
// directly into the caller's layout instead of inside distsummary's own
// [c.Horizontal] wrapper. Set it when the anchor is placed in a row the
// caller already opened — e.g. a status bar — so it aligns on the row's
// shared baseline; egui seats a centered horizontal nested inside another
// centered horizontal a few px below its plain-widget siblings (the
// "Ragged Control Row" note in imzero2 SKILL.md). The caller MUST provide
// the horizontal context: without it (a vertical parent) the toggle lands
// beneath the label instead of beside it. Default false keeps the wrapper,
// which is correct for a standalone Render. See [Renderer.inline].
func (inst Renderer) Inline() (out Renderer) {
	inst.inline = true
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

// Unit sets a unit string written once after the last quantile value in the
// level-1 summary (e.g. "fps", "ms", "MB"), so the labelled five-number line
// reads as dimensioned: "… · p100 67 fps". Empty (the default) appends
// nothing. The unit is not applied per-value — for unit-suffixed individual
// numbers use [Renderer.Format] instead.
func (inst Renderer) Unit(u string) (out Renderer) {
	inst.unit = u
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
	label := formatSummary(summary, inst.showN, inst.showIcon, inst.formatFunc, inst.unit)

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
	// Level-1 anchor: summary label + inspector toggle. In inline mode the
	// caller already owns a horizontal row, so emit straight into it — a
	// nested horizontal would seat the anchor a few px below its siblings
	// (see [Renderer.inline]). Otherwise wrap in our own horizontal so a
	// standalone Render keeps the toggle beside, not beneath, the label.
	// CaptureToggle still pins the tether's "from" endpoint at the toggle's
	// right edge in both modes: the toggle is the last item emitted, so the
	// captured row's max-x is the toggle's right edge either way.
	emitAnchor := func() {
		c.LabelAtoms(labelAtoms).Send()
		inspector.AnchorToggle(toggleId, &state.pinned)
		tether.CaptureToggle()
	}
	if inst.inline {
		emitAnchor()
	} else {
		for range c.Horizontal().KeepIter() {
			emitAnchor()
		}
	}

	if !state.pinned {
		// Inspector closed (title-bar X) or retracted (anchor handle): both
		// land here with pinned == false. Abort any confidence-band warm-up
		// this instance started so a long O(n²) inversion does not outlive
		// the window that requested it. Idempotent — a no-op when no band
		// job is in flight for this scope, and a band that already finished
		// stays in the shared ecdfbands cache for an instant reopen.
		ecdf.CancelBandJob(scope)
		// Reset the progressive-band choice so a reopen re-seeds it against
		// the current sample size (auto-request for a cheap solve, opt-in
		// otherwise) rather than reviving the previous open's state.
		state.exactRequested = false
		state.exactInit = false
		// Drop the cached ECDF width so a reopen re-fills from the popupWidth
		// floor up to the (possibly resized) window rather than the grow guard
		// pinning it to a stale value.
		state.lastEcdfPlotW = 0
		// Clear any pending zoom-reset latch so a reopen starts un-latched.
		state.ecdfResetReq = false
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
		if !inst.renderEcdfBody(scope, state, digest) {
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
	selector.SegmentedAbs(scope+"-tab", &state.tab).
		Style(selector.StyleSelectable).
		Gap(styletokens.GapInline(styletokens.DensityFromEnv())).
		Option(tabECDF, "ECDF").
		Option(tabBoxenplot, "Boxenplot").
		SendResp()
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
	// Centre the fixed-width boxenplot horizontally: the window opens wide for
	// the ECDF tab (see [defaultEcdfPlotWidth]), so without a centring lead the
	// narrow letter-value plot would hug the left edge with dead space to its
	// right. avail.W is last frame's captured content width (NaN until the
	// first capture) — fall back to the plain pad when it is unavailable.
	sm := c.CurrentApplicationState.StateManager
	avail := sm.GetAvailableSize()
	c.CaptureAvailableSize()
	lead := pad
	if avail.W == avail.W { // reject NaN
		if centred := (avail.W - inst.popupWidth) / 2; centred > lead {
			lead = centred
		}
	}
	if pad > 0 {
		c.AddSpace(pad)
	}
	for range c.Horizontal().KeepIter() {
		if lead > 0 {
			c.AddSpace(lead)
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
	// Explaining readout (parity with the ECDF tab's verbose readout) so
	// the inspector describes the hovered letter-value box in plain language
	// rather than the terse single line.
	boxenplot.WriteStatusLineVerbose(ch)
}

// renderEcdfBody emits the ECDF + simultaneous confidence band body under
// the progressive-quality scheme. Returns false when the digest is too
// sparse for an ECDF (nil, Count == 0, or support collapsed to a single
// value); the caller renders the boxenplot body in the same frame as a
// graceful fallback. The plot id is a distinct AbsoluteWidgetId from the
// boxenplot's so the r15 hover register stays per-tab and a cached hover
// from the previous tab does not surface here as a stale crosshair.
//
// The x-view is clipped adaptively per-side ([tailClipBounds]) so a long
// tail does not crush the body, and the grid is built over the clipped
// window so resolution lands where it is visible. The cutoff bounds only
// the view; the band's calibration uses the true count.
//
// The exact (1-α) band needs an O(n²) critical-value inversion that runs
// into minutes past a few thousand points, so it is never on the critical
// render path. The exact-band n is capped ([Renderer.exactBandMaxN]) and
// then bucketed ([bucketExactN]) so a drifting sample size reuses a cached
// solve rather than restarting it every frame. The body always draws the
// instant closed-form DKW preview band and layers the tighter exact band
// on top in one of three states, named in the always-visible band-state
// row below the plot:
//   - exact: the exact band for this (bucketed n, α, method) is cached —
//     draw it and the hover crosshair directly; the row names the family
//     and calibration n (flagged conservative when it lags the true n).
//   - warming: the exact band was requested (auto for small n, else via
//     the Compute button) and is warming on a keelson background job
//     (ADR-0038); keep drawing the DKW preview meanwhile with progress +
//     ETA + a Cancel button, and swap to the exact band once a later frame
//     finds the cache warm.
//   - preview: the exact band is opt-in and not yet requested — draw the
//     DKW preview alone with a "Compute exact band" affordance.
//
// Below those, a fixed-height verbose cursor readout ([ecdf.WriteStatusLine])
// describes F(x) and the confidence interval at the hover (a hint when the
// cursor is off the curve), and a hidden-tail note appears when a side was
// clipped.
//
// Order matters the same way as [renderBoxenplotBody]: At*() snapshots the
// hover before Render* stages the band rectangles + ECDF polyline, and
// PaintCrosshair piggybacks on the same plot block so the vline lands on
// top of band and curve.
func (inst Renderer) renderEcdfBody(scope string, state *instanceState, digest *tdigest.TDigest) (rendered bool) {
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
	// Adaptive, per-side tail cutoff: clip a long tail to a quantile so the
	// body fills the plot, and build the grid over the clipped window so the
	// resolution lands where it is visible — a full-range uniform grid wastes
	// most of its points in a flat tail. The cutoff bounds only the view; the
	// band's calibration still uses the true count n, not this window.
	clipLo, clipHi, clippedLo, clippedHi := tailClipBounds(
		digest, inst.tailLowerP, inst.tailUpperP, inst.tailTriggerIQR, inst.tailClipEnabled)
	xs, fn := ecdfdigest.BuildDigestGridRange(digest, inst.gridN, clipLo, clipHi)

	// The exact band's critical value is computed at a capped n so the
	// opt-in solve stays tractable + cancellable on large samples; the DKW
	// preview is always drawn at the true n. See [Renderer.exactBandMaxN].
	nExact := n
	if inst.exactBandMaxN > 0 && nExact > inst.exactBandMaxN {
		nExact = inst.exactBandMaxN
	}
	// Bucket the (capped) exact-band n down to a stable value so a digest
	// whose size drifts reuses the cached solve instead of cancelling and
	// restarting it every frame — what lets a live / recomputed inspector's
	// exact band settle. The bucketed value is the calibration n shown in the
	// readout (flagged conservative when it lags the true count). See
	// [bucketExactN].
	nExact = bucketExactN(nExact, inst.exactBandBucketRatio)

	// Seed the exact-band choice once per open: auto-request when its solve
	// is cheap (small effective n), opt-in above. Reset on close (see Render)
	// so a reopen re-evaluates against the current sample size.
	if !state.exactInit {
		state.exactRequested = nExact <= exactBandAutoMaxN
		state.exactInit = true
	}

	exactReady := inst.ecdfPlot.BandReady(nExact)
	warming := !exactReady && state.exactRequested
	var ch ecdf.Crosshair
	switch {
	case exactReady:
		ch = inst.ecdfPlot.AtGrid(plotID, xs, fn, nExact)
		ch.SampleN = n // AtGrid only knew the bucketed/capped solve size
		if err := inst.ecdfPlot.RenderGrid(xs, fn, nExact); err != nil {
			// A grid the exact band rejects (e.g. a sub-ULP non-monotone F_n)
			// must not blank the plot — fall back to the curve so the ECDF is
			// still drawn instead of vanishing along with the band.
			inst.ecdfPlot.RenderGridCurveOnly(xs, fn)
		}
		inst.ecdfPlot.PaintCrosshair(ch)
	default: // warming or preview — both draw the instant DKW preview band
		ch = inst.ecdfPlot.AtGridPreview(plotID, xs, fn, n)
		ch.SampleN = n // already the true n on the preview path; explicit for parity
		_ = inst.ecdfPlot.RenderGridPreview(xs, fn, n)
		inst.ecdfPlot.PaintCrosshair(ch)
	}

	var job ecdf.BandJobSnapshot
	if warming {
		job = inst.ecdfPlot.EnsureBandJob(scope, inst.tasks, nExact)
	}
	if !exactReady {
		// Heartbeat while the exact band is unsettled: animates the warming
		// progress bar (fed by a background goroutine, no input event) and
		// makes the one-frame-lagged Cancel (warming) / Compute (preview)
		// clicks land promptly even under reactive render cadence. Stops once
		// the exact band is cached. See [bandWarmRepaintIntervalSecs].
		c.RequestRepaintAfter(bandWarmRepaintIntervalSecs)
	}
	// Responsive width: the ECDF reads as a wide 2-D curve, so it fills the
	// window's content width instead of the fixed popupWidth the narrow
	// boxenplot tab keeps. avail is last frame's captured content size (one-
	// frame lag; W is NaN until the first capture lands), and popupWidth is
	// the floor so a just-opened or shrunk-narrow window never collapses the
	// curve. Widening is also what restores the x-axis ticks: egui_plot culls
	// any X label whose inter-mark spacing drops under 60 px, which starves a
	// fixed popupWidth plot of a wide-range distribution (the window opens at
	// [defaultEcdfPlotWidth] so the fresh view already clears that bar).
	sm := c.CurrentApplicationState.StateManager
	avail := sm.GetAvailableSize()
	c.CaptureAvailableSize()
	pad := inst.popupPad
	plotW := inst.popupWidth
	// Subtract a chrome budget beyond the two pad insets so the rendered row
	// (pad + plot + pad + egui's inter-item spacings) fits strictly inside the
	// width it was measured from, instead of overflowing it by ~2 item_spacings
	// and ratcheting the host Window wider every frame. See [ecdfPlotChromeW].
	if avail.W == avail.W { // avail.W == avail.W rejects NaN
		if fill := avail.W - 2*pad - ecdfPlotChromeW; fill > plotW {
			plotW = fill
		}
	}
	// Grow guard: clamp sub-deadband frame-over-frame upticks (chrome-budget
	// mis-estimates) to last frame's width so any residual overflow can't
	// resurrect the auto-grow loop; a real resize moves in larger steps. See
	// [ecdfPlotGrowGuardPx]. Downward deltas pass through (window shrink).
	if state.lastEcdfPlotW > 0 && plotW > state.lastEcdfPlotW && plotW-state.lastEcdfPlotW < ecdfPlotGrowGuardPx {
		plotW = state.lastEcdfPlotW
	}
	state.lastEcdfPlotW = plotW
	if pad > 0 {
		c.AddSpace(pad)
	}
	// Consume the one-frame reset latch set by the Reset zoom button below.
	resetZoom := state.ecdfResetReq
	state.ecdfResetReq = false
	for range c.Horizontal().KeepIter() {
		if pad > 0 {
			c.AddSpace(pad)
		}
		// XAxisAutoTicks replaces egui_plot's default log spacer with a nice-
		// number spacer that keeps a healthy, round-numbered tick count through
		// zoom — the default culls labels on this bounded, sometimes-narrow
		// surface and was the cause of the 0–1 X ticks worked around by the
		// width logic above.
		plot := c.Plot(plotID).
			Width(plotW).
			Height(inst.popupHeight).
			XAxisLabel("value").
			YAxisLabel("F(x)").
			ShowGrid(true, true).
			XAxisAutoTicks().
			YGridMarks(ecdfYTickVals, ecdfYTickLabels).
			AllowZoom(true).
			AllowDrag(false).
			AllowScroll(false).
			IncludeY(0).
			IncludeY(1).
			ClampX(clipLo, clipHi).
			ClampY(0, 1)
		if resetZoom {
			plot = plot.ResetBounds()
		}
		plot.Send()
		if pad > 0 {
			c.AddSpace(pad)
		}
	}
	// Reset-zoom affordance below the curve — re-fits after the reader zooms
	// into tail detail. egui_plot's double-click reset also works but is
	// undiscoverable; this latches ecdfResetReq, consumed on the next frame.
	for range c.Horizontal().KeepIter() {
		if c.Button(c.MakeAbsoluteIdStr(scope+"-ecdf-reset"),
			c.Atoms().Text("Reset zoom").Keep()).
			Small().
			SendResp().HasPrimaryClicked() {
			state.ecdfResetReq = true
		}
	}
	if pad > 0 {
		c.AddSpace(pad)
	}
	// Hidden-tail annotation: always visible when a side was clipped, so the
	// trim is honest about which tail (and how much mass) it dropped.
	if note := formatTailClipNote(digest, clipLo, clipHi, clippedLo, clippedHi, inst.formatFunc); note != "" {
		c.LabelAtoms(c.Atoms().BeginRichText(note).Small().Weak().End().Keep()).Send()
	}
	// Band-state row / controls: always visible per state, independent of
	// hover, so staleness (calibration n vs sample n) is readable without
	// pointing at the curve.
	switch {
	case exactReady:
		c.LabelAtoms(c.Atoms().BeginRichText(
			formatBandStateLine(inst.ecdfPlot.BandMethod(), nExact, n)).Small().Weak().End().Keep()).Send()
	case warming:
		switch job.State {
		case ecdf.BandJobError:
			c.Label("exact band unavailable: " + job.Note).Send()
		case ecdf.BandJobRunning:
			// The inline progress widget renders a Cancel button; a click
			// aborts the exact solve and drops back to the DKW preview band +
			// Compute affordance (exactRequested = false) rather than
			// respawning the solve on the next frame. The title names the
			// target family + bucketed n so the work in flight is identifiable.
			if jobprogress.Render(jobprogress.Input{
				Title:    "computing exact band (" + inst.ecdfPlot.BandMethod().String() + ", n=" + strconv.Itoa(nExact) + ")",
				Fraction: job.Fraction,
				EtaMs:    job.EtaMs,
				CancelId: c.MakeAbsoluteIdStr(scope + "-band-cancel"),
			}) {
				ecdf.CancelBandJob(scope)
				state.exactRequested = false
			}
		}
	default: // DKW preview shown — offer to upgrade to the exact band
		for range c.Horizontal().KeepIter() {
			c.LabelAtoms(
				c.Atoms().BeginRichText("conservative DKW preview band").Small().Weak().End().Keep(),
			).Send()
			c.AddSpace(styletokens.GapItems(styletokens.DensityFromEnv()))
			if c.Button(c.MakeAbsoluteIdStr(scope+"-band-compute"), c.Atoms().Text("Compute exact band").Keep()).
				Small().
				SendResp().
				HasPrimaryClicked() {
				state.exactRequested = true
			}
		}
	}
	// Verbose cursor readout: fixed-height stack below the controls,
	// describing F(x) and the confidence interval at the hover (a hover hint
	// when the cursor is off the curve). See [ecdf.WriteStatusLine].
	ecdf.WriteStatusLine(ch)
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
	// First-open width targets the default tab (the ECDF curve), which fills
	// the window's content width and needs room for x-axis ticks — see
	// [defaultEcdfPlotWidth]. max() so an explicit oversized PopupSize still
	// wins; the narrow boxenplot tab keeps popupWidth and sits centred.
	bodyW := inst.popupWidth
	if defaultEcdfPlotWidth > bodyW {
		bodyW = defaultEcdfPlotWidth
	}
	envW := bodyW + 2*inst.popupPad + 24
	// ecdfStatusBudget reserves room for the ECDF tab's status area (band
	// state, hidden-tail note, and the fixed-height verbose readout) so the
	// window first-opens tall enough to show it without an immediate resize;
	// the narrow boxenplot tab simply leaves that space below its status line.
	envH := inst.popupHeight + 2*inst.popupPad + 40 + tabBarBudget + ecdfStatusBudget
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
