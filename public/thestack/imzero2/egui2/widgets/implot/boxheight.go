package implot

// Box height — what a plot needs before it starts clipping itself.
//
// A plot's gutters are taken OUT of the box height it was given: the title and
// the horizontal y label sit above the plot area, the x tick labels and the x
// axis label below it. layoutFrame subtracts both and floors what remains at
// minPlotAreaH, so a box shorter than gutters+floor does not produce a shorter
// plot — it produces a layout taller than the canvas, and the canvas clips.
// What lands at the clipped edge is the bottom gutter, which is exactly where
// the x tick labels are.
//
// A caller sizing a plot to its pane therefore has two distinct ways to lose
// those labels, and only one of them is about the pane:
//
//   - a box TALLER than its pane — the pane clips the box (ADR-0172);
//   - a box SHORTER than MinBoxHeight — the box clips itself.
//
// MinBoxHeight is the second bound, exported because the sizing decision is
// the caller's. Its arithmetic and layoutFrame's are the same helpers, so the
// two cannot drift.

const (
	// minPlotAreaH is the plot area layoutFrame refuses to shrink past. Below
	// it the gutters have already taken the whole box.
	minPlotAreaH = 16
	// The gutter depths, in points. Each is the room one piece of chrome needs
	// on the side of the plot area it sits on.
	gutterBare      = 6  // nothing to reserve — a sparkline
	gutterTitle     = 24 // the plot title, above everything
	gutterYLabel    = 20 // the horizontal y label, when there is no title
	gutterTickLabel = 14 // one lane of x tick label text
	gutterAxisLabel = 16 // the x axis label, under the tick labels
)

// topGutterFor is the room above the plot area. A title displaces the y label
// rather than stacking with it — the y label is drawn horizontally, and both
// want the same strip.
func topGutterFor(hasTitle bool, hasYLabel bool) (h float32) {
	switch {
	case hasTitle:
		return gutterTitle
	case hasYLabel:
		return gutterYLabel
	}
	return gutterBare
}

// bottomGutterFor is the room below the plot area: the tick marks and their
// labels, deepened by every extra lane a crowded axis stacks into, plus the
// axis label under them.
func bottomGutterFor(hasXLabel bool, hasTickLabels bool, lanes int) (h float32) {
	h = gutterBare
	if hasTickLabels {
		h = gutterBare + tickLen + gutterTickLabel + float32(lanes-1)*tickLabelLaneH
	}
	if hasXLabel {
		h += gutterAxisLabel
	}
	return h
}

// MinBoxHeight reports the shortest box a plot with these label choices can
// draw inside without clipping its own x tick labels. lanes is the x label
// depth (1 unless the axis stacks; see maxBandLanes, which bounds stacking by
// a quarter of the canvas so a stacked axis cannot outgrow its box).
//
// Size a pane-following plot with this as the floor. A floor chosen for
// READABILITY instead is a different and softer thing — it may sit well above
// this, but it must never sit below it, or the box clips the labels from the
// inside while the pane looks roomy enough.
func MinBoxHeight(hasTitle bool, hasXLabel bool, hasYLabel bool, lanes int) (h float32) {
	return topGutterFor(hasTitle, hasYLabel) + minPlotAreaH +
		bottomGutterFor(hasXLabel, true, max(lanes, 1))
}
