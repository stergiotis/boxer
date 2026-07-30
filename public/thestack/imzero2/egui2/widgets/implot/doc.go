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
// M1 scope: linear x/y axes, default tick locator/formatter, grid, line
// series, pan/zoom/fit/box-zoom, hover readout. Legends, more item types,
// log/time scales, subplots and linked axes follow in later milestones per
// the ADR.
//
// Known deviations from upstream, M1:
//   - Box-zoom is Shift+drag (upstream: right-drag). The response-flag
//     register does not yet distinguish which button a drag uses.
//   - Setup* calls after the first item are ignored with a debug log
//     (upstream asserts).
//
// Interaction state is read one frame behind, like every imzero2 register
// (ADR-0140 wheel, R24 canvas pointer, R7 response flags) — imperceptible
// at interactive rates.
//
// Derivative-work notice: the tick locator, fit and interaction logic are
// ported from implot.cpp; see LICENSE-implot.txt in this directory for the
// upstream license text carried per ADR-0149 SD8.
package implot
