package cardgrid

import "math"

// The layout's constants. Line heights are the body font's row and the small
// font's row with their leading; every slot is clipped to its span, so a
// host whose fonts run taller loses descenders rather than its grid.
const (
	lineBody  float32 = 18
	lineSmall float32 = 15
	// lineFact is a fact row: a small line plus the air that keeps a column
	// of them readable.
	lineFact float32 = 17
	// lineTags is a row of small badges.
	lineTags float32 = 22
	// slotGap separates two slots, headGap two lines of the head block;
	// cardPad is the text column's inset.
	slotGap float32 = 6
	headGap float32 = 2
	cardPad float32 = 10
	// cardGap separates two cards, on both axes.
	cardGap float32 = 12
	// titleLines is the title's line clamp — the common card convention.
	titleLines = 2
	// runePx estimates a proportional rune's advance at the body size, on
	// the wide side so a cut usually lands before the clip; runeSmallPx the
	// same at the small size.
	runePx      float32 = 7.0
	runeSmallPx float32 = 6.2
	// factLabelShare is the label column's share of a fact row.
	factLabelShare float32 = 0.38
)

// densitySpec is what a density step decides.
type densitySpec struct {
	minW      float32
	bodyLines int
	factRows  int
}

func (inst DensityE) spec() densitySpec {
	switch inst {
	case DensitySmall:
		return densitySpec{minW: 180, bodyLines: 2, factRows: 2}
	case DensityLarge:
		return densitySpec{minW: 360, bodyLines: 5, factRows: 6}
	}
	return densitySpec{minW: 260, bodyLines: 3, factRows: 4}
}

// ratio is height over width.
func (inst AspectE) ratio() float32 {
	switch inst {
	case Aspect4x3:
		return 3.0 / 4.0
	case Aspect1x1:
		return 1
	}
	return 9.0 / 16.0
}

// Span is a slot's vertical extent inside a card; H is zero for a slot the
// model does not declare.
type Span struct {
	Y, H float32
}

// Layout is a page's geometry: the column count, the one card size, and
// where each slot sits inside a card. It depends on the pane's width, the
// density, the aspect and the declared slots — never on a card's content.
type Layout struct {
	Cols         int
	CardW, CardH float32

	// Head is the overline, title and subtitle as one block: they flow
	// top-down inside it, so a one-line title is followed directly by its
	// subtitle and the slack falls below the block rather than inside it.
	Hero, Head, Body, Facts, Tags, Footer Span

	// FactRows is how many fact rows a card has room for.
	FactRows int
}

// Plan computes the layout for a pane paneW wide. Cards stretch to fill the
// row: n = ⌊(W + gap) / (minW + gap)⌋ columns, at least one.
func Plan(paneW float32, d DensityE, a AspectE, slots SlotsE) (l Layout) {
	spec := d.spec()
	l.Cols = max(1, int(math.Floor(float64((paneW+cardGap)/(spec.minW+cardGap)))))
	l.CardW = (paneW - float32(l.Cols-1)*cardGap) / float32(l.Cols)
	if l.CardW < spec.minW {
		// A pane narrower than one card: the card keeps its minimum and the
		// host's scroll area takes the overflow.
		l.CardW = spec.minW
	}
	l.CardW = float32(math.Floor(float64(l.CardW)))

	y := float32(0)
	if slots.Has(SlotsHero) {
		l.Hero = Span{Y: 0, H: float32(math.Round(float64(l.CardW * a.ratio())))}
		y = l.Hero.H
	}
	y += cardPad
	place := func(declared bool, h float32) (s Span) {
		if !declared {
			return
		}
		s = Span{Y: y, H: h}
		y += h + slotGap
		return
	}
	head := float32(0)
	for _, part := range []struct {
		slot SlotsE
		h    float32
	}{{SlotsOverline, lineSmall}, {SlotsTitle, titleLines * lineBody}, {SlotsSubtitle, lineBody}} {
		if slots.Has(part.slot) {
			if head > 0 {
				head += headGap
			}
			head += part.h
		}
	}
	l.Head = place(head > 0, head)
	l.Body = place(slots.Has(SlotsBody), float32(spec.bodyLines)*lineBody)
	if slots.Has(SlotsFacts) {
		l.FactRows = spec.factRows
	}
	l.Facts = place(slots.Has(SlotsFacts), float32(spec.factRows)*lineFact)
	l.Tags = place(slots.Has(SlotsTags), lineTags)
	l.Footer = place(slots.Has(SlotsFooter), lineSmall)
	if slots&^SlotsHero != 0 {
		// The last slot's trailing gap becomes the bottom padding.
		y += cardPad - slotGap
	} else {
		// A hero-only card has no text column to pad.
		y = l.Hero.H
	}
	l.CardH = y
	return
}

// Rows is how many grid rows n cards take.
func (inst Layout) Rows(n int) int {
	if n <= 0 || inst.Cols <= 0 {
		return 0
	}
	return (n + inst.Cols - 1) / inst.Cols
}

// GridHeight is the height n cards take, gaps included.
func (inst Layout) GridHeight(n int) float32 {
	rows := inst.Rows(n)
	if rows == 0 {
		return 0
	}
	return float32(rows)*inst.CardH + float32(rows-1)*cardGap
}

// GridWidth is the width a full row takes.
func (inst Layout) GridWidth() float32 {
	return float32(inst.Cols)*inst.CardW + float32(inst.Cols-1)*cardGap
}

// CardOrigin is card i's top-left corner in the grid.
func (inst Layout) CardOrigin(i int) (x, y float32) {
	col, row := i%inst.Cols, i/inst.Cols
	return float32(col) * (inst.CardW + cardGap), float32(row) * (inst.CardH + cardGap)
}

// textW is the text column's width.
func (inst Layout) textW() float32 { return inst.CardW - 2*cardPad }

// lineRunes estimates how many body-size runes fit one line of the text
// column; smallRunes the same at the small size.
func (inst Layout) lineRunes() int  { return max(8, int(inst.textW()/runePx)) }
func (inst Layout) smallRunes() int { return max(8, int(inst.textW()/runeSmallPx)) }

// VisibleRange is the ordinals [lo, hi) of the cards whose row meets the
// viewport — offset down the grid and viewH tall — widened by slack rows on
// each side.
func (inst Layout) VisibleRange(n int, offset, viewH float32, slack int) (lo, hi int) {
	if n <= 0 || inst.Cols <= 0 {
		return 0, 0
	}
	pitch := inst.CardH + cardGap
	// An unbounded host reports no usable viewport: draw everything.
	if pitch <= 0 || !(viewH > 0) || math.IsInf(float64(viewH), 0) || math.IsNaN(float64(offset)) {
		return 0, n
	}
	first := int(math.Floor(float64(offset/pitch))) - slack
	last := int(math.Floor(float64((offset+viewH)/pitch))) + slack
	lo = min(max(first, 0)*inst.Cols, n)
	hi = min(max(last+1, 0)*inst.Cols, n)
	return
}

// Neighbour is the ordinal an arrow key moves the selection to from ordinal
// i among n cards. dx walks the reading order, so → at a row's end goes to
// the next row's first card; dy moves by rows, staying put at the grid's
// top and bottom, and a move down onto a short last row lands on its last
// card. With nothing selected the first key selects the first card.
func (inst Layout) Neighbour(i, n, dx, dy int) int {
	if n <= 0 || inst.Cols <= 0 {
		return -1
	}
	if i < 0 {
		return 0
	}
	i = min(max(i+dx, 0), n-1)
	if dy == 0 {
		return i
	}
	row := i/inst.Cols + dy
	if row < 0 || row >= inst.Rows(n) {
		return i
	}
	return min(row*inst.Cols+i%inst.Cols, n-1)
}

// Fit is the size a source of (srcW, srcH) takes contained in box: scaled
// down to fit with its aspect kept, never scaled up past its own size, and
// never under one point on a side — so a panorama becomes a band, a strip a
// sliver, and an icon stays an icon. A degenerate source takes the box.
func Fit(srcW, srcH float32, box Box) (w, h float32) {
	if srcW <= 0 || srcH <= 0 || box.W <= 0 || box.H <= 0 {
		return box.W, box.H
	}
	scale := min(box.W/srcW, box.H/srcH, 1)
	return max(1, float32(math.Round(float64(srcW*scale)))), max(1, float32(math.Round(float64(srcH*scale))))
}
