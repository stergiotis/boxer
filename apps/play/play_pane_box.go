package play

// play_pane_box.go holds the box rule the result panels that draw into a FIXED
// box share — the Sankey, the Icicle, the Treemap and the Network. Each of them
// probes its pane (c.CapturePaneSize, a per-seq r21 slot that contends with
// nobody) and then wants the same thing: fill what the probe reported, less a
// margin, floored so a leaf too small to hold a drawing does not collapse it and
// capped in width so an ultrawide leaf does not stretch it across the screen.
//
// One rule in one place because the four had drifted into four near-copies of
// it, each with its own fixed aspect standing in for the pane height it could
// not read. The height became readable when captureUiAvailableRect landed; what
// is left that differs between them is bounds, which is what paneFill carries.
//
// This is NOT the rule the implot panels that shrink to fit use (Chart,
// Distribution, Series). Those have a PREFERRED height and take the pane only
// when the pane is shorter, because their box spends its bottom on x tick
// labels and a box taller than its pane loses them off the fold — which the
// leaf's ScrollArea does not rescue, since implot takes the wheel while the
// pointer is over the plot (ADR-0140). A panel here draws its whole picture
// inside the box and scales it, so there is nothing to push out of sight and
// filling is free.

// paneFill is one panel's bounds on that box, in points.
type paneFill struct {
	// slack keeps the drawing off the pane's edges, on both axes.
	slack float32
	// minW / minH floor the box. A leaf smaller than the floor gets the floor
	// and scrolls: below it the drawing stops being a reading, and for a plot
	// with tick labels the floor must also clear implot.MinBoxHeight or the box
	// clips its own labels from the inside while the pane still looks roomy.
	minW, minH float32
	// maxW caps the width. There is deliberately no maxH: the pane's own height
	// is the cap, and filling the leaf is the point.
	maxW float32
	// fallbackW / fallbackH are the box assumed on the frames the probe has not
	// reported one — the first, and the one a hidden tab comes back on, since a
	// seq that did not capture is absent from the drain rather than zero. A
	// plausible leaf rather than a small one, so the single frame before the
	// real box lands is not a jump.
	fallbackW, fallbackH float32
	// chrome is the height the WIDGET takes for itself around the box it is
	// handed — the Treemap's breadcrumb bar is the one case here. Filling a
	// pane means covering that too, so it comes off the pane before the floor
	// applies; a panel whose widget draws only what it was sized for leaves
	// this zero.
	chrome float32
}

// box is the drawing's box for the pane the probe last reported. Pass 0 for a
// dimension the probe has not answered for yet.
func (inst paneFill) box(paneW float32, paneH float32) (w, h float32) {
	if paneW <= 0 {
		paneW = inst.fallbackW
	}
	if paneH <= 0 {
		paneH = inst.fallbackH
	}
	w = min(max(paneW-inst.slack, inst.minW), inst.maxW)
	h = max(paneH-inst.slack-inst.chrome, inst.minH)
	return
}
