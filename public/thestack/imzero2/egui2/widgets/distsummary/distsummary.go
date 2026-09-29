// Package distsummary is an immediate-mode widget (ADR-0267) summarising a
// single statistical distribution on two levels.
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
//     title-bar X. The active tab is held in the host's [State]; the
//     window keeps a fixed size across tab swaps
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
// [tailClipBounds] / [Input.TailLowerP]) so a heavy-tailed distribution's
// body fills the plot instead of being crushed into the left edge, and
// annotates the hidden tail below the curve. A fixed-height verbose
// readout below the plot describes the cursor's F(x) reading and its
// confidence interval, and an always-visible band-state line names the
// band (exact family + calibration n, or the conservative DKW preview),
// flagging it conservative when the calibration n lags the true count —
// which also lets a live or recomputed inspector's exact band settle
// rather than restart its solve every frame (see [bucketExactN]).
//
// The host passes its id stack and a scope key on [Input] and owns the
// [State] — pinned open, the selected tab, the exact-band request — so two
// summaries in one host differ by ScopeKey alone and a host can persist or
// drive the inspector like any other value.
//
// Caller passes a *tdigest.TDigest as the data source. The widget
// derives the level-1 summary directly from the digest (Count + Min +
// Max + Quantile(0.25/0.5/0.75)). For level 2 it forwards the digest
// (as a letterval.QuantileOracle) plus the optional extremes slice
// to the boxenplot painter, and bridges the same digest to the ecdf
// painter through [ecdfdigest.BuildDigestGridRange] so
// both tabs share the single tdigest sketch the caller owns — no
// duplicate accumulation, no API change at the call site.
package distsummary

import (
	"strconv"

	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/boxenplot"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdfdigest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
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
// [ecdfdigest.BuildDigestGridRange]. 128 samples keep the band's
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
// distribution renders full-range. Tunable via [Input.TailTriggerIQR].
const defaultTailTriggerIQR = 3.0

// defaultExactBandBucketRatio is the geometric step [bucketExactN] rounds
// the exact-band n down to so a drifting sample size reuses a cached
// solve instead of restarting it every frame. 1.25 caps the resulting
// band conservatism at √1.25 ≈ 1.12 (~12% wider half-width worst case)
// while coarsening enough to let live / recomputed inspectors settle.
// Tunable (or disabled, ratio 1) via [Input.ExactBandBucketRatio].
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
// scientific notation. Override via [Input.Format] for unit suffixes
// or domain-specific precision.
type FormatFunc func(float64) string

// Input is one distribution to summarise and how. Zero fields take the
// defaults [Input.resolved] fills in.
type Input struct {
	// Ids is the host's widget id stack. Render opens one IdScope under it,
	// so two summaries in one host need only differ in ScopeKey.
	Ids *c.WidgetIdStack
	// ScopeKey names this summary within the host's id space; empty uses
	// "distsummary". Two summaries sharing a host and a ScopeKey share
	// their toggle, window and probe slots.
	ScopeKey string
	// Digest is the streaming oracle behind both levels. nil or empty
	// (Count() == 0) draws a "(no data)" placeholder in place of the
	// summary.
	Digest *tdigest.TDigest
	// Extremes are forwarded to the boxenplot for OutlierModePoints; nil for
	// the other outlier modes.
	Extremes []float64
	// State is the host-owned pinned flag, tab and exact-band request.
	// Required: a summary with nowhere to record that its inspector is open
	// cannot be drawn. It must live where a pointer to it stays valid across
	// frames — the window's close box is a databinding onto it.
	State *State

	// Title names the inspector window; "" reads "distribution".
	Title string
	// PopupWidth and PopupHeight are the level-2 plot extent in points.
	// Height applies to both tabs; width is the boxenplot tab's fixed width
	// and the ECDF tab's lower bound — the curve fills the window's content
	// width above it, and the window first opens at [defaultEcdfPlotWidth]
	// (or the width, whichever is larger). Zero → 320 × 200.
	PopupWidth  float32
	PopupHeight float32
	// PopupPad is the breathing room in points around the plot inside the
	// window body; zero → 4.
	PopupPad float32
	// HideN drops the `n=<count>` term from the level-1 label; HideIcon the
	// chart-line affordance icon.
	HideN    bool
	HideIcon bool
	// Inline emits the level-1 anchor (summary label + inspector toggle)
	// straight into the caller's layout instead of the widget's own
	// [c.Horizontal]. Set it when the anchor is placed in a row the caller
	// already opened — a status bar — so it aligns on the row's shared
	// baseline; egui seats a centered horizontal nested inside another a few
	// px below its plain-widget siblings (the "Ragged Control Row" note in
	// the imzero2 skill). The caller must then provide the horizontal.
	Inline bool
	// Format renders one summary value; nil takes [humanizeValue].
	Format FormatFunc
	// Unit, when non-empty, is written once after the last quantile value
	// ("fps", "ms", "MB") so the level-1 summary reads as dimensioned. Not
	// applied per value — for unit-suffixed numbers use Format.
	Unit string

	// Boxen styles the letter-value tab; Ecdf the ECDF tab's band and curve.
	// Zero fields take each painter's defaults.
	Boxen boxenplot.Style
	Ecdf  ecdf.Style
	// GridN is the uniform sample count the ECDF tab's grid is built over.
	// Below 2 → [defaultEcdfGridN].
	GridN int
	// ExactBandMaxN caps the effective sample size at which the EXACT band's
	// O(n²) critical value is computed; the DKW preview is always drawn at
	// the true n. Zero means uncapped — statistically exact but minutes-long
	// past a few thousand points. A positive cap keeps the opt-in solve
	// tractable and cancellable at large n by calibrating a slightly
	// conservative band at min(n, cap).
	ExactBandMaxN int
	// NoTailClip disables the ECDF x-view's adaptive per-side tail cutoff
	// (see [tailClipBounds]); by default a side is clipped to TailLowerP /
	// TailUpperP when its tail is long relative to the IQR. TailLowerP and
	// TailUpperP both zero → p0.1 / p99.9; TailTriggerIQR zero → 3×IQR,
	// negative clips whenever the cutoff quantile lies inside the support.
	// The cutoff only bounds the view; the band's calibration always uses
	// the true count.
	NoTailClip     bool
	TailLowerP     float64
	TailUpperP     float64
	TailTriggerIQR float64
	// ExactBandBucketRatio is the geometric ladder [bucketExactN] rounds the
	// exact-band n down to so a drifting sample size reuses a cached solve
	// instead of restarting it every frame. Zero → [defaultExactBandBucketRatio];
	// 1 disables bucketing.
	ExactBandBucketRatio float64
	// Provenance, when non-zero, renders the standard [inspector.ProvenanceChip]
	// in the inspector window so operators see which subject / source app
	// produced the distribution.
	Provenance inspector.Provenance
	// Tasks, when non-nil, is the keelson task API the ECDF band warm-up runs
	// under (ADR-0038), so a large-n O(n²) inversion shows in the supervisor
	// and taskmonitor. nil still computes the band off-thread through the
	// in-process job registry; only task-framework visibility is lost.
	Tasks task.TaskApiI
}

// State is the host-owned part that persists across frames: the pinned
// (inspector open) flag, the selected tab and the exact-band request. The
// zero value is usable and lands on the ECDF tab.
type State struct {
	pinned bool
	tab    tabE
	// exactRequested drives the ECDF tab's confidence-band detail under the
	// progressive-quality scheme (see renderEcdfBody): false draws only the
	// instant closed-form DKW preview band and offers a "Compute exact band"
	// affordance; true warms — and then shows — the tighter exact band with
	// progress + a Cancel button. Set by Compute and by the small-n
	// auto-seed; cleared by Cancel and on inspector close.
	exactRequested bool
	// exactInit records whether exactRequested has been seeded for the
	// current open yet; reset on close so a reopen re-evaluates against the
	// current sample size.
	exactInit bool
	// lastEcdfPlotW caches the ECDF tab's last applied plot width so the grow
	// guard in renderEcdfBody (see [ecdfPlotGrowGuardPx]) can damp the host
	// Window's auto-grow loop. 0 until the first ECDF frame; reset on close.
	lastEcdfPlotW float32
	// ecdfResetReq is a one-frame latch set by the ECDF tab's "Reset zoom"
	// button and consumed by the plot block (FitNext). Cleared on close.
	ecdfResetReq bool
}

// Pinned reports whether the inspector window is open.
func (st *State) Pinned() bool { return st.pinned }

// SetPinned opens or closes the inspector window from code.
func (st *State) SetPinned(open bool) { st.pinned = open }

// Result is what one Render reports.
type Result struct {
	// Pinned is whether the inspector window is open after this frame.
	Pinned bool
	// Err is a host programming error — a nil Ids or State — drawn in place
	// of the summary.
	Err error
}

// Render emits the level-1 inline label paired with the standard
// [inspector.AnchorToggle] and, while pinned, the inspector window with the
// ECDF / Boxenplot tab body and the optional provenance chip. Clicking the
// toggle opens the window; clicking it again or the window's title-bar X
// closes it. A bezier connector ties the toggle to the open window via
// [inspector.AnchorTether]. Every id is relative under the widget's id
// scope; the window's is the one absolute id, derived from that scope.
func Render(in Input) (res Result) {
	if in.Ids == nil || in.State == nil {
		res.Err = eh.Errorf("distsummary: Input.Ids and Input.State are required")
		c.LabelAtoms(c.Atoms().BeginRichText("distsummary: missing Ids or State").Small().Weak().End().Keep()).Send()
		return
	}
	in = in.resolved()
	for range c.IdScope(in.Ids.PrepareStr(in.ScopeKey)) {
		res = in.render()
	}
	return
}

// resolved returns in with every zero field replaced by its default and the
// clamps applied.
func (in Input) resolved() Input {
	if in.ScopeKey == "" {
		in.ScopeKey = "distsummary"
	}
	if in.Title == "" {
		in.Title = "distribution"
	}
	if in.PopupWidth == 0 {
		in.PopupWidth = 320
	}
	if in.PopupHeight == 0 {
		in.PopupHeight = 200
	}
	if in.PopupPad == 0 {
		in.PopupPad = 4
	} else if in.PopupPad < 0 {
		in.PopupPad = 0
	}
	if in.Format == nil {
		in.Format = humanizeValue
	}
	if in.GridN < 2 {
		in.GridN = defaultEcdfGridN
	}
	if in.ExactBandMaxN < 0 {
		in.ExactBandMaxN = 0
	}
	if in.TailLowerP == 0 && in.TailUpperP == 0 {
		in.TailLowerP, in.TailUpperP = defaultTailLowerP, defaultTailUpperP
	}
	if in.TailUpperP < in.TailLowerP {
		in.TailLowerP, in.TailUpperP = in.TailUpperP, in.TailLowerP
	}
	in.TailLowerP = min(1, max(0, in.TailLowerP))
	in.TailUpperP = min(1, max(0, in.TailUpperP))
	if in.TailTriggerIQR == 0 {
		in.TailTriggerIQR = defaultTailTriggerIQR
	}
	if in.ExactBandBucketRatio == 0 {
		in.ExactBandBucketRatio = defaultExactBandBucketRatio
	}
	return in
}

// bandJobKey is this summary's confidence-band warm-up key, derived from
// its id scope so two summaries never share or cancel each other's solve.
func (in Input) bandJobKey() ecdf.BandJobKey {
	return ecdf.BandJobKey(in.Ids.ProbeSeq("band-job"))
}

// tetherScope names this summary's tether capture slots; the tether takes
// a string, so the id-scope-derived slot is spelled in hex.
func (in Input) tetherScope() string {
	return "distsummary#" + strconv.FormatUint(in.Ids.ProbeSeq("tether"), 16)
}

func (in Input) render() (res Result) {
	state := in.State
	summary := computeFiveNumberSummary(in.Digest)
	label := formatSummary(summary, !in.HideN, !in.HideIcon, in.Format, in.Unit)

	if summary.n == 0 {
		c.LabelAtoms(
			c.Atoms().BeginRichText(label).Monospace().Weak().End().Keep(),
		).Send()
		return
	}

	labelAtoms := c.Atoms().BeginRichText(label).Monospace().End().Keep()
	tether := inspector.NewAnchorTether(in.tetherScope())
	// Level-1 anchor: summary label + inspector toggle. In inline mode the
	// caller already owns a horizontal row, so emit straight into it — a
	// nested horizontal would seat the anchor a few px below its siblings
	// (see [Input.Inline]). Otherwise wrap in our own horizontal so a
	// standalone Render keeps the toggle beside, not beneath, the label.
	// CaptureToggle still pins the tether's "from" endpoint at the toggle's
	// right edge in both modes: the toggle is the last item emitted, so the
	// captured row's max-x is the toggle's right edge either way.
	emitAnchor := func() {
		c.LabelAtoms(labelAtoms).Send()
		inspector.AnchorToggle(in.Ids.PrepareStr("toggle"), &state.pinned)
		tether.CaptureToggle()
	}
	if in.Inline {
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
		// job is in flight, and a band that already finished stays in the
		// shared ecdfbands cache for an instant reopen.
		ecdf.CancelBandJob(in.bandJobKey())
		// Reset the progressive-band choice so a reopen re-seeds it against
		// the current sample size (auto-request for a cheap solve, opt-in
		// otherwise) rather than reviving the previous open's state.
		state.exactRequested = false
		state.exactInit = false
		// Drop the cached ECDF width so a reopen re-fills from the PopupWidth
		// floor up to the (possibly resized) window rather than the grow guard
		// pinning it to a stale value.
		state.lastEcdfPlotW = 0
		// Clear any pending zoom-reset latch so a reopen starts un-latched.
		state.ecdfResetReq = false
		return
	}
	in.renderPinnedWindow(tether)
	tether.Paint()
	res.Pinned = state.pinned
	return
}

// renderLevel2Body emits the inspector body inside the pinned window.
// Layout from top to bottom: provenance chip (when bound) → tab bar
// (ECDF / Boxenplot) → tab-specific body.
//
// The ECDF tab falls back to the boxenplot body for the current frame when
// the digest cannot support an ECDF (Count == 0 or support collapsed to a
// single value); State.tab is left untouched so the ECDF returns the moment
// the digest broadens again.
func (in Input) renderLevel2Body() {
	if !in.Provenance.IsZero() {
		inspector.ProvenanceChip(in.Provenance)
		c.Separator().Horizontal().Send()
	}
	in.renderTabBar()
	c.Separator().Horizontal().Send()
	switch in.State.tab {
	case tabBoxenplot:
		in.renderBoxenplotBody()
	default:
		if !in.renderEcdfBody() {
			in.renderBoxenplotBody()
		}
	}
}

// renderTabBar emits the two-tab selector controlling which body the
// inspector window shows.
func (in Input) renderTabBar() {
	selector.Segmented(in.Ids, "tab", &in.State.tab).
		Style(selector.StyleSelectable).
		Gap(styletokens.GapInline(styletokens.ActiveDensity())).
		Option(tabECDF, "ECDF").
		Option(tabBoxenplot, "Boxenplot").
		SendResp()
}

// renderBoxenplotBody emits the letter-value plot body. The plot renders
// through the implot port (ADR-0149 SD7) under the window's id scope; At()
// reads the plot's own hover state right after Begin, so the crosshair
// still layers above the boxes declared after it.
func (in Input) renderBoxenplotBody() {
	levels := letterval.RecommendedLevels(in.Digest)
	pad := in.PopupPad
	// Centre the fixed-width boxenplot horizontally: the window opens wide for
	// the ECDF tab (see [defaultEcdfPlotWidth]), so without a centring lead the
	// narrow letter-value plot would hug the left edge with dead space to its
	// right. availW is last frame's content width from this instance's own r21
	// probe (absent until the first lands) — fall back to the plain pad then.
	// The slot is derived from the widget's id scope (ADR-0267 W7), so two
	// summaries — or two windows of one app — never read each other's pane.
	availW, _, availOk := c.CapturePaneSize(in.Ids.ProbeSeq("boxen-pane"))
	lead := pad
	if availOk && availW == availW { // reject NaN
		if centred := (availW - in.PopupWidth) / 2; centred > lead {
			lead = centred
		}
	}
	if pad > 0 {
		c.AddSpace(pad)
	}
	bin := boxenplot.Input{Style: in.Boxen, Argument: 0.0, Levels: levels, Extremes: in.Extremes, Name: in.Title}
	var ch boxenplot.Crosshair
	for range c.Horizontal().KeepIter() {
		if lead > 0 {
			c.AddSpace(lead)
		}
		pl := implot.Begin(in.Ids, "##bp-plot", in.PopupWidth, in.PopupHeight)
		pl.SetupAxes("", "value",
			implot.AxisFlagsNoGrid|implot.AxisFlagsNoTickLabels,
			implot.AxisFlagsNone)
		pl.NoInputs()
		pl.NoLegend()
		pl.IncludeX(-0.6)
		pl.IncludeX(0.6)
		// The boxenplot's argument axis is hidden, so any single x
		// position works — 0.0 keeps the median annotation centred.
		ch = boxenplot.At(pl, bin)
		boxenplot.Paint(pl, bin)
		boxenplot.PaintCrosshair(pl, in.Boxen, ch)
		pl.End()
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
// graceful fallback. The plot's title differs from the boxenplot's so the
// hover register stays per-tab and a cached hover from the previous tab
// does not surface here as a stale crosshair.
//
// The x-view is clipped adaptively per-side ([tailClipBounds]) so a long
// tail does not crush the body, and the grid is built over the clipped
// window so resolution lands where it is visible. The cutoff bounds only
// the view; the band's calibration uses the true count.
//
// The exact (1-α) band needs an O(n²) critical-value inversion that runs
// into minutes past a few thousand points, so it is never on the critical
// render path. The exact-band n is capped ([Input.ExactBandMaxN]) and
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
func (in Input) renderEcdfBody() (rendered bool) {
	state, digest := in.State, in.Digest
	if digest == nil || digest.Count() == 0 {
		return
	}
	xmin := digest.Min()
	xmax := digest.Max()
	if !(xmax > xmin) {
		return
	}
	n := int(digest.Count())
	// Adaptive, per-side tail cutoff: clip a long tail to a quantile so the
	// body fills the plot, and build the grid over the clipped window so the
	// resolution lands where it is visible — a full-range uniform grid wastes
	// most of its points in a flat tail. The cutoff bounds only the view; the
	// band's calibration still uses the true count n, not this window.
	clipLo, clipHi, clippedLo, clippedHi := tailClipBounds(
		digest, in.TailLowerP, in.TailUpperP, in.TailTriggerIQR, !in.NoTailClip)
	xs, fn := ecdfdigest.BuildDigestGridRange(digest, in.GridN, clipLo, clipHi)

	// The exact band's critical value is computed at a capped n so the
	// opt-in solve stays tractable + cancellable on large samples; the DKW
	// preview is always drawn at the true n. See [Input.ExactBandMaxN].
	nExact := n
	if in.ExactBandMaxN > 0 && nExact > in.ExactBandMaxN {
		nExact = in.ExactBandMaxN
	}
	// Bucket the (capped) exact-band n down to a stable value so a digest
	// whose size drifts reuses the cached solve instead of cancelling and
	// restarting it every frame — what lets a live / recomputed inspector's
	// exact band settle. The bucketed value is the calibration n shown in the
	// readout (flagged conservative when it lags the true count). See
	// [bucketExactN].
	nExact = bucketExactN(nExact, in.ExactBandBucketRatio)

	// Seed the exact-band choice once per open: auto-request when its solve
	// is cheap (small effective n), opt-in above. Reset on close (see Render)
	// so a reopen re-evaluates against the current sample size.
	if !state.exactInit {
		state.exactRequested = nExact <= exactBandAutoMaxN
		state.exactInit = true
	}

	exactReady := ecdf.BandReady(in.Ecdf, nExact)
	warming := !exactReady && state.exactRequested

	var job ecdf.BandJobSnapshot
	if warming {
		job = ecdf.EnsureBandJob(in.bandJobKey(), in.Tasks, nExact, in.Ecdf)
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
	// boxenplot tab keeps. availW is last frame's content width from this
	// instance's own r21 probe (one-frame lag; absent until the first lands),
	// and popupWidth is the floor so a just-opened or shrunk-narrow window never
	// collapses the curve. Widening is also what restores the x-axis ticks:
	// egui_plot culls any X label whose inter-mark spacing drops under 60 px,
	// which starves a fixed popupWidth plot of a wide-range distribution (the
	// window opens at [defaultEcdfPlotWidth] so the fresh view already clears
	// that bar). The slot is derived from the widget's id scope, as the boxen
	// tab's is.
	availW, _, availOk := c.CapturePaneSize(in.Ids.ProbeSeq("ecdf-pane"))
	pad := in.PopupPad
	plotW := in.PopupWidth
	// Subtract a chrome budget beyond the two pad insets so the rendered row
	// (pad + plot + pad + egui's inter-item spacings) fits strictly inside the
	// width it was measured from, instead of overflowing it by ~2 item_spacings
	// and ratcheting the host Window wider every frame. See [ecdfPlotChromeW].
	if availOk && availW == availW { // availW == availW rejects NaN
		if fill := availW - 2*pad - ecdfPlotChromeW; fill > plotW {
			plotW = fill
		}
	}
	// Grow guard: clamp sub-deadband frame-over-frame upticks (chrome-budget
	// mis-estimates) to last frame's width so any residual overflow can't
	// resurrect the auto-grow loop; a real resize moves in larger steps. See
	// [ecdfPlotGrowGuardPx]. Downward deltas pass through (window shrink).
	//
	// The seq-keyed probe does NOT retire this: the loop it damps runs through
	// the host Window's auto-grow (content sized from a width the content
	// itself sets), not through the register the probe replaced.
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
	var ch ecdf.Crosshair
	for range c.Horizontal().KeepIter() {
		if pad > 0 {
			c.AddSpace(pad)
		}
		// The plot renders through the implot port (ADR-0149 SD7) under
		// the window's id scope. implot's own nice-number locator serves the
		// x axis (the bridge needed XAxisAutoTicks to dodge egui_plot's
		// label culling); the fixed 0/¼/½/¾/1 marks stay on y, and the
		// viewport constraints replace ClampX/ClampY.
		pl := implot.Begin(in.Ids, "##ecdf-plot", plotW, in.PopupHeight)
		pl.SetupAxes("value", "F(x)", implot.AxisFlagsNone, implot.AxisFlagsNone)
		pl.SetupAxisTicks(implot.AxisY1, ecdfYTickVals, ecdfYTickLabels)
		pl.SetupAxisLimitsConstraints(implot.AxisX1, clipLo, clipHi)
		pl.SetupAxisLimitsConstraints(implot.AxisY1, 0, 1)
		pl.IncludeY(0)
		pl.IncludeY(1)
		pl.NoLegend()
		if resetZoom {
			pl.FitNext()
		}
		ein := ecdf.Input{Style: in.Ecdf, Xs: xs, FnAt: fn}
		switch {
		case exactReady:
			ein.N, ein.Band = nExact, ecdf.BandExact
			ch = ecdf.At(pl, ein)
			ch.SampleN = n // At only knew the bucketed/capped solve size
			// A grid the exact band rejects (e.g. a sub-ULP non-monotone F_n)
			// must not blank the plot — Paint still draws the curve without
			// the band, so the ECDF does not vanish along with it.
			_ = ecdf.Paint(pl, ein)
		default: // warming or preview — both draw the instant DKW preview band
			ein.N, ein.Band = n, ecdf.BandPreview
			ch = ecdf.At(pl, ein)
			ch.SampleN = n // already the true n on the preview path; explicit for parity
			_ = ecdf.Paint(pl, ein)
		}
		ecdf.PaintCrosshair(pl, in.Ecdf, ch)
		pl.End()
		if pad > 0 {
			c.AddSpace(pad)
		}
	}
	// Reset-zoom affordance below the curve — re-fits after the reader zooms
	// into tail detail. egui_plot's double-click reset also works but is
	// undiscoverable; this latches ecdfResetReq, consumed on the next frame.
	for range c.Horizontal().KeepIter() {
		if c.Button(in.Ids.PrepareStr("ecdf-reset"),
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
	if note := formatTailClipNote(digest, clipLo, clipHi, clippedLo, clippedHi, in.Format); note != "" {
		c.LabelAtoms(c.Atoms().BeginRichText(note).Small().Weak().End().Keep()).Send()
	}
	// Band-state row / controls: always visible per state, independent of
	// hover, so staleness (calibration n vs sample n) is readable without
	// pointing at the curve.
	switch {
	case exactReady:
		c.LabelAtoms(c.Atoms().BeginRichText(
			formatBandStateLine(in.Ecdf.Resolved().Method, nExact, n)).Small().Weak().End().Keep()).Send()
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
				Ids:      in.Ids,
				ScopeKey: "band",
				Title:    "computing exact band (" + in.Ecdf.Resolved().Method.String() + ", n=" + strconv.Itoa(nExact) + ")",
				Fraction: job.Fraction,
				EtaMs:    job.EtaMs,
				Cancel:   true,
			}).CancelClicked {
				ecdf.CancelBandJob(in.bandJobKey())
				state.exactRequested = false
			}
		}
	default: // DKW preview shown — offer to upgrade to the exact band
		for range c.Horizontal().KeepIter() {
			c.LabelAtoms(
				c.Atoms().BeginRichText("conservative DKW preview band").Small().Weak().End().Keep(),
			).Send()
			c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
			if c.Button(in.Ids.PrepareStr("band-compute"), c.Atoms().Text("Compute exact band").Keep()).
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

// renderPinnedWindow emits the c.Window holding the inspector body when
// the host's pinned flag is true. Native title-bar X is wired to that same
// flag via OpenBound + R10 databinding so closing the window through egui's
// chrome flips the toggle the same way clicking the anchor would; the flag
// lives in the host's State, which is why State must stay at one address.
// The tether's [inspector.AnchorTether.CaptureWindow] runs at the top of
// the body so the bezier "to" endpoint anchors on the window's content rect
// (title bar excluded). The window id is the widget's one absolute id,
// derived from its id scope (ADR-0267 W6), so two summaries open two
// independent windows.
//
// Window envelope adds title-bar + frame headroom plus a tab-bar budget
// around the (PopupWidth + 2*pad) × (PopupHeight + 2*pad) body so the
// first-open size fits the plot and the tab selector without immediate user
// resize. tabBarBudget covers one row of SelectableLabel + an inline
// separator at the IDS standard densities.
func (in Input) renderPinnedWindow(tether inspector.AnchorTether) {
	const tabBarBudget float32 = 32
	winId := c.MakeAbsoluteIdHighEntropy(in.Ids.PrepareStr("window").Derive())
	// First-open width targets the default tab (the ECDF curve), which fills
	// the window's content width and needs room for x-axis ticks — see
	// [defaultEcdfPlotWidth]. max() so an explicit oversized PopupWidth still
	// wins; the narrow boxenplot tab keeps PopupWidth and sits centred.
	bodyW := in.PopupWidth
	if defaultEcdfPlotWidth > bodyW {
		bodyW = defaultEcdfPlotWidth
	}
	envW := bodyW + 2*in.PopupPad + 24
	// ecdfStatusBudget reserves room for the ECDF tab's status area (band
	// state, hidden-tail note, and the fixed-height verbose readout) so the
	// window first-opens tall enough to show it without an immediate resize;
	// the narrow boxenplot tab simply leaves that space below its status line.
	envH := in.PopupHeight + 2*in.PopupPad + 40 + tabBarBudget + ecdfStatusBudget
	win := c.Window(winId, c.WidgetText().Text(in.Title).Keep()).
		DefaultOpen(true).
		Resizable(true).
		Collapsible(false).
		AlwaysOnTop(true).
		DefaultSize(envW, envH)
	bindId := win.Id()
	win = win.OpenBound(bindId)
	c.CurrentApplicationState.StateManager.AddR10Databinding(bindId, &in.State.pinned)
	for range win.KeepIter() {
		tether.CaptureWindow()
		in.renderLevel2Body()
	}
}
