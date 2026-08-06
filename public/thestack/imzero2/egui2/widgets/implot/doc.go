// Package implot is a Go port of the core of ImPlot
// (https://github.com/epezent/implot, MIT, © 2020 Evan Pezent), re-targeted
// from Dear ImGui's draw list onto the imzero2 painter lane (ADR-0149).
//
// The port preserves ImPlot's frame protocol and interaction semantics —
// Begin / Setup* / plot items / End ordering, drag pan, wheel zoom anchored
// at the pointer, double-click auto-fit, box-zoom — and re-idiomizes the
// API surface: Go slices instead of getter templates, methods on a *Plot
// instead of an implicit current-plot context, float64 plot space projected
// to float32 only at paint-command emission (SD4).
//
// For straight-line plot bodies prefer the range-based scope over the
// loose pair — `for p := range implot.Scoped(...)` (and sp.Scoped inside
// Subplots cells) guarantees End on early exit, the same contract as the
// house c.IdScope; Begin/End remains for bodies where the handle must
// outlive a lexical block.
//
// Coverage (M1–M7 per the ADR, plus the SD7 migration batch):
// linear/time/log10/symlog axes with the ported locators, grid,
// line/scatter/bars/shaded (constant or two-curve)/stairs/stems/infinite
// lines, error bars, heatmaps (rect and texture routes), histograms
// (1D/2D), pie, digital channels, images, inlay text, a letter-value box
// series, interactive legend, drag tools/annotations/tags, a native
// context menu, subplots and linked axes, caller-supplied axis ticks,
// viewport constraints, NoInputs/NoLegend, and plot-space click/hover
// readback. Same-label items share one legend entry and palette slot
// (the upstream label→item registry semantics), so error bars merge
// with the series they decorate.
//
// Caller-drawn custom items (Custom / CustomUnclipped, custom.go)
// re-express upstream's custom-rendering idiom — GetPlotDrawList +
// PushPlotClipRect + PlotToPixels — as draw closures invoked during End,
// because under this port's deferred emission the frame transform does
// not exist earlier (see custom.go). Declaration order is z-order, as
// upstream's draw-list order is; labeled customs join the legend like
// any item.
//
// Per-axis gesture locks (AxisFlagsNoPan / AxisFlagsNoZoom, and
// AxisFlagsLock for both) carry upstream's ImPlotAxisFlags_Lock, split so
// that an axis can be scrolled but not scaled — what a categorical or
// depth axis wants when its span is derived from the plot-area height to
// hold a constant pixels-per-unit. The locks apply per gesture kind, not
// to the resulting range: an anchored wheel zoom shifts an axis's centre
// as well as its span, so restoring only the span would let the wheel pan
// a NoZoom axis.
//
// House extensions beyond the upstream surface, named as such in their
// doc comments: Boxes, IncludeX/IncludeY, TimeTicksLocal,
// AxisFlagsFollow, Clicked/HoverPlotPos, NewDetached, the pixel-space
// readbacks HoverPixelPos/ClickedPixelPos/PlotAreaPrev (hit-testing
// pixel-pinned custom geometry), and the two process-wide palette
// selectors — SetChrome for the frame around the data (IDS-token default,
// the port's original palette as ChromeClassic; chrome.go) and
// SetSeriesPalette for the data itself (IDS qualitative default,
// upstream's Deep as PaletteDeep; palette.go). The two are independent.
//
// Tick labels are placed rather than simply drawn (ticklabels.go), which
// upstream does not do: a label band that does not fit first locates fewer
// ticks (for located axes, where a tick is a choice), then slides labels to
// the nearest free spot and stacks the band into extra rows, drawing a
// leader line from each moved label back to its tick, and only then drops
// every k-th label. Tick marks and grid lines are never dropped. A band
// whose labels already fit is untouched — same text, same positions, no
// leader lines — so this shows up only where the alternative was an
// unreadable smear (a category axis from SetupAxisTicks, typically).
//
// Known deviations from upstream:
//   - Box-zoom is Shift+drag (upstream: right-drag). The response-flag
//     register does not yet distinguish which button a drag uses.
//   - Setup* calls after the first item are ignored with a debug log
//     (upstream asserts).
//   - The y-axis label renders horizontally (no rotated-text command).
//   - SymLog uses the asinh transform with the default locator on raw
//     values; upstream's dedicated symlog locator is not yet ported.
//   - Time axes label in UTC only and place major ticks only — upstream's
//     minor time ticks and second-line context labels are deferred.
//   - Digital channels use fixed bit-height/gap constants (there is no
//     style system to override upstream's defaults).
//   - Image takes caller-owned RGBA pixels plus a content version (the
//     painter lane's ship-once texture protocol); upstream's GPU
//     texture-id parameter has no equivalent on this substrate.
//   - Error bars draw in a fixed foreground color and a fixed whisker
//     width (upstream styles both).
//   - Series cycle the IDS qualitative palette by default, not upstream's
//     Deep colormap; SetSeriesPalette(PaletteDeep) restores it.
//
// Interaction state is read one frame behind, like every imzero2 register
// (ADR-0140 wheel, R24 canvas pointer, R7 response flags) — imperceptible
// at interactive rates.
//
// Derivative-work notice: the tick locator, fit and interaction logic are
// ported from implot.cpp; see LICENSE-implot.txt in this directory for the
// upstream license text carried per ADR-0149 SD8.
package implot
