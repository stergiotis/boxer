package implot

import "slices"

// Tick-label placement — the pass that keeps labels off each other.
//
// A label band is one-dimensional: the x band is the strip under the plot
// area, the y band the column beside it. Labels want to sit centred on their
// tick; when they cannot all have that, three answers apply in order of what
// they cost the reader.
//
//   - Fewer ticks. A located tick is a choice, not data, so an axis whose own
//     labels do not fit should locate fewer of them — locateTicksFitted
//     re-locates against a narrower effective axis until the widest label fits
//     its slot. A coarser step also carries fewer digits (stepPrecision), so
//     the loop closes from both ends. This runs before the band ever sees the
//     labels, which is why an ordinary numeric plot never reaches the rest.
//
//   - Move the label, and say where it came from. Caller-supplied ticks
//     (SetupAxisTicks) are data — a category axis names its bars — so they are
//     slid along the band to the nearest arrangement that does not overlap and
//     keeps their order, stacking into extra lanes when one is not enough. A
//     label that moved gets a leader line back to its tick: Wolfram's Callout
//     idiom, which is what makes a displaced label still readable as a label
//     *of that tick*.
//
//   - Drop labels. Only when no stacking fits: every k-th label survives.
//     Tick marks and grid lines are never dropped, so the positions stay
//     readable even where the names do not.
//
// The pass is a no-op when the labels already fit — same PaintText calls, no
// leader lines, no gutter growth. Widths come from the estimate (text.go), so
// treat the gaps as a budget; ADR-0149 §SD6 records the measured alternative.
const (
	// tickLabelGap is the ink-free space kept between two labels along the x
	// band. It is the whole difference between "adjacent" and "touching": at
	// the tick font two labels one pixel apart read as one word.
	tickLabelGap = 6.0
	// tickLabelGapY is the same for the y band, where the separation the eye
	// needs is smaller: stacked lines of text are read as lines, and the
	// glyphs themselves already leave the line box short of its box.
	tickLabelGapY = 2.0
	// tickLabelLaneH is the pitch of stacked lanes in the x band, and the
	// height one label claims in the y band. The tick font's line box.
	tickLabelLaneH = 12.0
	// calloutMinPx is the displacement below which a leader line is noise
	// rather than information: a label that moved less than one glyph still
	// reads as sitting on its tick. The case this exists for is the label
	// on the last tick, which the band pulls in by a pixel or two when it
	// would otherwise be clipped by the canvas edge — worth doing, not
	// worth annotating. Labels in a stacked lane draw a leader whatever
	// their displacement, since the lane alone makes them ambiguous.
	calloutMinPx = charW
	// calloutRunPx is the horizontal run reserved beside the y tick marks
	// when that band displaces, so its leader lines have a direction to be
	// read in rather than doubling back along the tick.
	calloutRunPx = 7.0
	// labelBandMaxLanes bounds the x band's stacking. Past three lanes the
	// band stops being an axis and starts being a table, and the leader
	// lines cross too much of the plot to follow.
	labelBandMaxLanes = 3
)

// labelCand is one label offered to a band: the tick it names, where that
// tick sits along the band, and the room the label needs there — width along
// the x band, line height along the y one.
type labelCand struct {
	tick  int
	pos   float32
	width float32
}

// labelPlacement is one label the band kept: its tick, that tick's own
// position (the leader line's anchor), where the label ended up, and which
// lane it landed in (0 = nearest the plot area).
type labelPlacement struct {
	tick   int
	at     float32
	center float32
	lane   int
}

// labelBand resolves one axis's labels for a frame. It owns its scratch so a
// steady-state frame allocates nothing; begin/layout is the whole protocol.
type labelBand struct {
	cand  []labelCand
	place []labelPlacement

	lanes  int  // lanes the arrangement used
	moved  bool // any label displaced far enough to earn a leader line
	thin   int  // stride applied; 1 = every label kept
	gap    float32
	member []int
	box    []float32
	start  []float32
	blocks []labelBlock
}

// begin resets the band for n candidates and hands back the slice to fill.
func (b *labelBand) begin(n int) []labelCand {
	if cap(b.cand) >= n {
		b.cand = b.cand[:n]
	} else {
		b.cand = make([]labelCand, n)
	}
	b.place = b.place[:0]
	b.lanes, b.moved, b.thin = 1, false, 1
	return b.cand
}

func byBandPos(a, b labelCand) int {
	switch {
	case a.pos < b.pos:
		return -1
	case a.pos > b.pos:
		return 1
	}
	return 0
}

func resize32(s []float32, n int) []float32 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]float32, n)
}

// layout resolves the band into [lo, hi], stacking up to maxLanes and keeping
// gap between neighbours. Read the result off place/lanes/moved/thin.
//
// lo and hi bound the labels' ink. The gap is what a label keeps from its
// neighbour, so each label is packed as a box half a gap wider on each side —
// and the band widens by the same half gap at each end, or an edge label
// would be pulled inward to make room for a neighbour it does not have.
func (b *labelBand) layout(lo, hi, gap float32, maxLanes int) {
	b.place = b.place[:0]
	b.lanes, b.moved, b.thin, b.gap = 1, false, 1, gap
	n := len(b.cand)
	if n == 0 || hi-lo <= 0 || maxLanes < 1 {
		return
	}
	lo, hi = lo-gap/2, hi+gap/2
	// The pack walks the band from one end to the other, so the candidates
	// have to arrive in band order. They usually do — tick values ascend and
	// the x axis ascends with them — but the y axis inverts (pixels grow
	// downward), and SetupAxisTicks may hand over values in any order at
	// all. Either would otherwise read as one long overlap and fold the
	// whole band into a single block at its mean.
	if !slices.IsSortedFunc(b.cand, byBandPos) {
		slices.SortFunc(b.cand, byBandPos)
	}
	span := hi - lo
	var total float32
	for i := range b.cand {
		total += b.cand[i].width + gap
	}
	// Start the stride search where the total width says it must end up
	// rather than walking up from 1: a caller may hand SetupAxisTicks
	// thousands of ticks, and every candidate stride costs a pass over them.
	stride := max(int(total/(span*float32(maxLanes)))+1, 1)
	lanes := 0
	for {
		for l := 1; l <= maxLanes; l++ {
			if b.fits(stride, l, span) {
				lanes = l
				break
			}
		}
		if lanes > 0 || stride >= n {
			break
		}
		stride++
	}
	if lanes == 0 {
		// A single label wider than the whole band. Keep one — a band with
		// nothing in it hides that the axis has names at all — and let it
		// pack against the edges below.
		lanes, stride = 1, n
	}
	b.lanes, b.thin = lanes, stride
	for lane := range lanes {
		b.member = b.member[:0]
		for i, k := 0, 0; i < n; i, k = i+stride, k+1 {
			if k%lanes == lane {
				b.member = append(b.member, i)
			}
		}
		b.pack(lo, hi)
		for m, i := range b.member {
			cd := b.cand[i]
			center := b.start[m] + (cd.width+gap)/2
			if center-cd.pos > calloutMinPx || cd.pos-center > calloutMinPx {
				b.moved = true
			}
			b.place = append(b.place, labelPlacement{
				tick: cd.tick, at: cd.pos, center: center, lane: lane,
			})
		}
	}
}

// fits reports whether a stride/lane assignment leaves every lane's labels
// room to sit side by side within span.
func (b *labelBand) fits(stride, lanes int, span float32) bool {
	if stride < 1 {
		return false
	}
	for lane := range lanes {
		var sum float32
		for i, k := 0, 0; i < len(b.cand); i, k = i+stride, k+1 {
			if k%lanes == lane {
				sum += b.cand[i].width + b.gap
			}
		}
		if sum-b.gap > span {
			return false
		}
	}
	return true
}

// labelBlock is a run of labels packed tight against each other, carried
// through the merge as one rigid body. sum is the run's members' desired
// starts, each pulled back by the widths preceding it inside the run, so
// sum/n is the position that minimises the run's total squared displacement.
type labelBlock struct {
	n     int
	sum   float32
	width float32
}

func (bl labelBlock) start() float32 { return bl.sum / float32(bl.n) }

// pack resolves b.member's boxes into non-overlapping starts inside [lo, hi],
// preserving order and moving each label as little as it can. It is the
// block-merge solution to the ordered-separation problem: walk left to right,
// and whenever a box would overlap the run before it, merge the two into one
// rigid run and re-centre it on the mean of what its members wanted. Merging
// can expose an overlap with the run before *that*, so the merge repeats
// until the stack is ordered again — each label joins and leaves a run at
// most once, so the walk stays linear.
//
// The greedy alternative — push each colliding label right against its
// neighbour — is not the same answer: it spends the whole correction on the
// later label while the earlier one keeps its place, which on a category axis
// walks a whole run of labels off to the right of the ticks they name.
func (b *labelBand) pack(lo, hi float32) {
	n := len(b.member)
	b.box = resize32(b.box, n)
	b.start = resize32(b.start, n)
	b.blocks = b.blocks[:0]
	for m, i := range b.member {
		b.box[m] = b.cand[i].width + b.gap
		blk := labelBlock{n: 1, sum: b.cand[i].pos - b.box[m]/2, width: b.box[m]}
		for len(b.blocks) > 0 {
			prev := b.blocks[len(b.blocks)-1]
			if prev.start()+prev.width <= blk.start() {
				break
			}
			blk = labelBlock{
				n:     prev.n + blk.n,
				sum:   prev.sum + blk.sum - float32(blk.n)*prev.width,
				width: prev.width + blk.width,
			}
			b.blocks = b.blocks[:len(b.blocks)-1]
		}
		b.blocks = append(b.blocks, blk)
	}
	// Clamp the runs into the band. Left to right first, so no run starts
	// before the band or inside its predecessor; then right to left, so no
	// run ends past the band. The second pass cannot undo the first: the
	// arrangement was only accepted because the boxes fit in the span.
	edge := lo
	for k := range b.blocks {
		s := b.blocks[k].start()
		if s < edge {
			s = edge
		}
		b.blocks[k].sum = s * float32(b.blocks[k].n)
		edge = s + b.blocks[k].width
	}
	edge = hi
	for k := len(b.blocks) - 1; k >= 0; k-- {
		s := b.blocks[k].start()
		if s+b.blocks[k].width > edge {
			s = edge - b.blocks[k].width
		}
		if s < lo {
			// Only reachable when a single label is wider than the whole
			// band. Keep its head on the band: a name cut off at the end is
			// still recognisable, one cut off at the front is not.
			s = lo
		}
		b.blocks[k].sum = s * float32(b.blocks[k].n)
		edge = s
	}
	m := 0
	for _, blk := range b.blocks {
		x := blk.start()
		for range blk.n {
			b.start[m] = x
			x += b.box[m]
			m++
		}
	}
}

// locateTicksFitted locates ticks whose labels fit the axis they are on: it
// runs the scale's locator, and while the widest label would not clear its
// slot, re-runs it against a narrower effective axis. The narrowing step is
// the shortfall itself, so it converges in a round or two rather than
// halving blindly; the iteration cap catches the locators that ignore the
// size hint (log10 walks decades whatever it is told), and the band's
// stacking picks those up.
//
// sizePx is the real axis length in pixels; gap is the ink-free space a
// label wants beside it.
func locateTicksFitted(rng Range, sizePx float32, scale ScaleE, gap float32, dst []tick) []tick {
	eff := sizePx
	for range 5 {
		dst = locateTicksScaled(rng, eff, scale, dst)
		var widest float32
		majors := 0
		for i := range dst {
			if !dst[i].major {
				continue
			}
			majors++
			if w := EstimateTextWidth(dst[i].label, tickFontSize); w > widest {
				widest = w
			}
		}
		if majors < 3 {
			break
		}
		slot := sizePx / float32(majors-1)
		if widest+gap <= slot {
			break
		}
		next := eff * slot / (widest + gap)
		if next > eff*0.95 {
			next = eff * 0.7 // no progress from the shortfall step; force one
		}
		if next < 1 {
			break
		}
		eff = next
	}
	return dst
}
