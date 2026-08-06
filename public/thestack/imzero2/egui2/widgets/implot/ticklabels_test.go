package implot

import (
	"math"
	"testing"
)

// bandOf lays out n labels of the given widths, evenly spaced across
// [lo, hi] — a category axis in miniature.
func bandOf(t *testing.T, widths []float32, lo, hi float32, maxLanes int) *labelBand {
	t.Helper()
	b := &labelBand{}
	cand := b.begin(len(widths))
	step := (hi - lo) / float32(len(widths))
	for i := range widths {
		cand[i] = labelCand{tick: i, pos: lo + step*(float32(i)+0.5), width: widths[i]}
	}
	b.layout(lo, hi, tickLabelGap, maxLanes)
	return b
}

func uniformWidths(n int, w float32) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = w
	}
	return out
}

// checkBand asserts the invariants every arrangement must hold: labels stay
// inside the band, no two in a lane overlap, and a lane's labels keep the
// order of the ticks they name along the band. Band order, not tick order —
// the y band's pixels descend as its values ascend.
func checkBand(t *testing.T, b *labelBand, lo, hi float32) {
	t.Helper()
	type span struct {
		tick     int
		at       float32
		from, to float32
	}
	lanes := map[int][]span{}
	for _, pl := range b.place {
		w := float32(0)
		for i := range b.cand {
			if b.cand[i].tick == pl.tick {
				w = b.cand[i].width
			}
		}
		s := span{tick: pl.tick, at: pl.at, from: pl.center - w/2, to: pl.center + w/2}
		if s.from < lo-0.01 || s.to > hi+0.01 {
			t.Errorf("label of tick %d spans [%.1f, %.1f], outside band [%.1f, %.1f]",
				pl.tick, s.from, s.to, lo, hi)
		}
		lanes[pl.lane] = append(lanes[pl.lane], s)
	}
	for lane, spans := range lanes {
		for i := 1; i < len(spans); i++ {
			if spans[i].at <= spans[i-1].at {
				t.Errorf("lane %d: band order broken at %d (tick %d at %.1f after tick %d at %.1f)",
					lane, i, spans[i].tick, spans[i].at, spans[i-1].tick, spans[i-1].at)
			}
			if spans[i].from < spans[i-1].to {
				t.Errorf("lane %d: labels of ticks %d and %d overlap ([%.1f, %.1f] vs [%.1f, %.1f])",
					lane, spans[i-1].tick, spans[i].tick,
					spans[i-1].from, spans[i-1].to, spans[i].from, spans[i].to)
			}
		}
	}
	if b.lanes < 1 {
		t.Errorf("lanes = %d", b.lanes)
	}
}

func TestLabelBandSortsIntoBandOrder(t *testing.T) {
	// The y axis hands its labels over with pixels descending, and a caller
	// may hand SetupAxisTicks values in any order at all. Read as one long
	// overlap, that folds the whole band into a single block at its mean —
	// the bug this sort exists to stop. Here: the y band of a 600×300 plot,
	// ticks at 0/50/100 landing bottom to top, each claiming a line box in a
	// band that runs half a box past the plot area at either end.
	slack := float32(tickLabelLaneH) / 2
	lo, hi := 6-slack, 276+slack
	b := &labelBand{}
	cand := b.begin(3)
	cand[0] = labelCand{tick: 0, pos: 276, width: tickLabelLaneH}
	cand[1] = labelCand{tick: 1, pos: 141, width: tickLabelLaneH}
	cand[2] = labelCand{tick: 2, pos: 6, width: tickLabelLaneH}
	b.layout(lo, hi, tickLabelGapY, 1)
	if b.moved {
		t.Error("labels 135px apart reported as displaced")
	}
	for _, pl := range b.place {
		if math.Abs(float64(pl.center-pl.at)) > 0.01 {
			t.Errorf("tick %d moved from %.1f to %.1f", pl.tick, pl.at, pl.center)
		}
	}
	checkBand(t, b, lo, hi)
}

func TestLabelBandLeavesFittingLabelsAlone(t *testing.T) {
	// The property the whole pass rests on: a band that already fits must
	// emit exactly what it emitted before there was a band.
	b := bandOf(t, uniformWidths(6, 40), 0, 600, labelBandMaxLanes)
	if b.lanes != 1 || b.thin != 1 || b.moved {
		t.Fatalf("untouched band reported lanes=%d thin=%d moved=%v", b.lanes, b.thin, b.moved)
	}
	if len(b.place) != 6 {
		t.Fatalf("kept %d of 6 labels", len(b.place))
	}
	for _, pl := range b.place {
		if math.Abs(float64(pl.center-pl.at)) > 1e-3 {
			t.Errorf("tick %d moved from %.2f to %.2f with room to spare", pl.tick, pl.at, pl.center)
		}
	}
	checkBand(t, b, 0, 600)
}

func TestLabelBandSlidesInsteadOfDropping(t *testing.T) {
	// 15 module-name-ish labels in the width the chart panel gives them:
	// too wide to sit centred, wide enough to fit once slid and stacked.
	b := bandOf(t, uniformWidths(15, 85), 0, 850, labelBandMaxLanes)
	if b.thin != 1 {
		t.Errorf("dropped labels (stride %d) that stacking could have kept", b.thin)
	}
	if len(b.place) != 15 {
		t.Errorf("kept %d of 15 labels", len(b.place))
	}
	if b.lanes < 2 {
		t.Errorf("lanes = %d, expected the band to stack", b.lanes)
	}
	if !b.moved {
		t.Error("no label reported displaced, so none would draw a leader line")
	}
	checkBand(t, b, 0, 850)
}

func TestLabelBandStacksBeforeItThins(t *testing.T) {
	// Two lanes' worth of ink: one lane cannot hold it, two can, and the
	// ladder must reach for the lane before it reaches for the stride.
	b := bandOf(t, uniformWidths(10, 90), 0, 500, labelBandMaxLanes)
	if b.lanes != 2 {
		t.Errorf("lanes = %d, want 2", b.lanes)
	}
	if b.thin != 1 {
		t.Errorf("stride = %d, want every label kept", b.thin)
	}
	checkBand(t, b, 0, 500)
}

func TestLabelBandThinsWhenNoStackingFits(t *testing.T) {
	// 40 categories in 600px: three lanes still cannot hold 40 names, so
	// labels go — but the survivors are a plain stride, and they fit.
	b := bandOf(t, uniformWidths(40, 60), 0, 600, labelBandMaxLanes)
	if b.thin < 2 {
		t.Fatalf("stride = %d, expected thinning", b.thin)
	}
	if len(b.place) == 0 {
		t.Fatal("thinned the band empty")
	}
	for _, pl := range b.place {
		if pl.tick%b.thin != 0 {
			t.Errorf("kept tick %d, not a multiple of stride %d", pl.tick, b.thin)
		}
	}
	checkBand(t, b, 0, 600)
}

func TestLabelBandSharesTheDisplacement(t *testing.T) {
	// Two labels overlapping by 20px must each give 10px, not one give 20.
	// That is the difference between the block-merge solution and a greedy
	// push, and on a category axis it is the difference between labels that
	// straddle their ticks and a run that walks off to the right.
	b := &labelBand{}
	cand := b.begin(2)
	cand[0] = labelCand{tick: 0, pos: 100, width: 60}
	cand[1] = labelCand{tick: 1, pos: 154, width: 60}
	b.layout(0, 400, tickLabelGap, 1)
	if len(b.place) != 2 {
		t.Fatalf("kept %d of 2", len(b.place))
	}
	d0 := b.place[0].center - 100
	d1 := b.place[1].center - 154
	if d0 > -5 || d1 < 5 {
		t.Errorf("displacements %.1f / %.1f: expected both labels to move apart", d0, d1)
	}
	if math.Abs(float64(d0+d1)) > 0.01 {
		t.Errorf("displacements %.1f / %.1f are not symmetric", d0, d1)
	}
	checkBand(t, b, 0, 400)
}

func TestLabelBandClampsIntoTheBand(t *testing.T) {
	// A label centred past the edge is pulled in rather than half drawn.
	b := &labelBand{}
	cand := b.begin(2)
	cand[0] = labelCand{tick: 0, pos: 4, width: 60}
	cand[1] = labelCand{tick: 1, pos: 396, width: 60}
	b.layout(0, 400, tickLabelGap, 1)
	checkBand(t, b, 0, 400)
	if !b.moved {
		t.Error("clamped labels not reported as displaced")
	}
}

func TestLabelBandKeepsOneLabelWiderThanItself(t *testing.T) {
	// Degenerate: a label wider than the whole band. Dropping every label
	// would hide that the axis has names at all, so one survives, head on.
	b := &labelBand{}
	cand := b.begin(3)
	for i := range cand {
		cand[i] = labelCand{tick: i, pos: float32(50 + 50*i), width: 400}
	}
	b.layout(0, 200, tickLabelGap, labelBandMaxLanes)
	if len(b.place) != 1 {
		t.Fatalf("kept %d labels, want 1", len(b.place))
	}
	// Its ink starts at the band's left edge: the half gap a label reserves
	// beside it is outside the ink bounds the caller gave.
	if got := b.place[0].center - 400/2; math.Abs(float64(got)) > 0.01 {
		t.Errorf("label ink starts at %.1f, want the band's left edge (0)", got)
	}
}

func TestLabelBandEmptyAndDegenerate(t *testing.T) {
	b := &labelBand{}
	b.begin(0)
	b.layout(0, 400, tickLabelGap, labelBandMaxLanes)
	if len(b.place) != 0 || b.lanes != 1 {
		t.Errorf("empty band produced %d placements, lanes=%d", len(b.place), b.lanes)
	}
	cand := b.begin(1)
	cand[0] = labelCand{tick: 0, pos: 10, width: 20}
	b.layout(0, 0, tickLabelGap, labelBandMaxLanes) // zero-width band
	if len(b.place) != 0 {
		t.Errorf("zero-width band produced %d placements", len(b.place))
	}
}

func TestLabelBandManyTicksStaysCheap(t *testing.T) {
	// A caller may hand SetupAxisTicks thousands of ticks; the stride
	// search must start from the width it needs, not walk up from one.
	b := bandOf(t, uniformWidths(4000, 50), 0, 600, labelBandMaxLanes)
	if len(b.place) == 0 {
		t.Fatal("thinned the band empty")
	}
	if b.thin < 100 {
		t.Errorf("stride %d is implausibly small for 4000 labels in 600px", b.thin)
	}
	checkBand(t, b, 0, 600)
}

// layoutTestPlot is a detached plot sized for layoutFrame, which paints
// nothing and so needs no live StateManager.
func layoutTestPlot(w, h float32) *Plot {
	p := NewDetached()
	p.w, p.h = w, h
	p.st.x.rng, p.st.y.rng = Range{0, 10}, Range{0, 100}
	return p
}

func TestLayoutFrameLeavesAnOrdinaryPlotAlone(t *testing.T) {
	// The geometry every existing plot has: one tick row, gutters unchanged.
	p := layoutTestPlot(600, 300)
	areaX, areaY, areaW, areaH := p.layoutFrame(1, labelBandMaxLanes)
	if p.st.xBand.lanes != 1 || p.st.xBand.moved || p.st.yBand.moved {
		t.Errorf("plain plot stacked or displaced: lanes=%d xmoved=%v ymoved=%v",
			p.st.xBand.lanes, p.st.xBand.moved, p.st.yBand.moved)
	}
	if areaY != 6 {
		t.Errorf("top gutter %v, want 6", areaY)
	}
	if want := 300 - 6 - (6 + tickLen + 14); areaH != float32(want) {
		t.Errorf("area height %v, want %v", areaH, want)
	}
	if areaX <= 0 || areaW <= 0 || areaX+areaW > 600 {
		t.Errorf("area x=%v w=%v does not sit inside the canvas", areaX, areaW)
	}
}

func TestLayoutFrameReservesTheStackedBand(t *testing.T) {
	// Fifteen module names, the vol-top book's bar chart. The band stacks,
	// and End's second pass must hand back the gutter it asked for.
	labels := []string{
		"clickhouse-go", "arrow-go", "zerolog", "protobuf", "grpc",
		"sqlite", "prometheus", "opentelemetry", "yaml.v3", "testify",
		"crypto", "net-http2", "goldmark", "uuid", "compress",
	}
	vals := make([]float64, len(labels))
	for i := range vals {
		vals[i] = float64(i)
	}
	// 620 wide: the width the gallery demo and the play Chart panel give a
	// plot in a pane, and the width at which these names cannot share a row.
	p := layoutTestPlot(620, 300)
	p.st.x.rng = Range{-0.5, float64(len(labels)) - 0.5}
	p.SetupAxisTicks(AxisX1, vals, labels)

	_, _, _, oneLane := p.layoutFrame(1, labelBandMaxLanes)
	lanes := p.st.xBand.lanes
	if lanes < 2 {
		t.Fatalf("band used %d lane(s); these labels do not fit in one", lanes)
	}
	if len(p.st.xBand.place) != len(labels) {
		t.Errorf("kept %d of %d labels", len(p.st.xBand.place), len(labels))
	}
	_, _, _, stacked := p.layoutFrame(lanes, lanes)
	if want := oneLane - float32(lanes-1)*tickLabelLaneH; stacked != want {
		t.Errorf("stacked area height %v, want %v (the reserved lanes)", stacked, want)
	}
	if p.st.xBand.lanes > lanes {
		t.Errorf("second pass asked for %d lanes after being given %d", p.st.xBand.lanes, lanes)
	}
}

func TestBandDepthIsBoundedByTheCanvas(t *testing.T) {
	// A short plot must not spend its area on names: at play's 80pt floor for
	// a pane-sized box, three lanes plus gutters would leave a few pixels of
	// plot. Tall plots keep the full ladder.
	cases := []struct {
		h    float32
		want int
	}{{80, 1}, {120, 2}, {300, labelBandMaxLanes}, {16, 1}}
	for _, tc := range cases {
		if got := layoutTestPlot(600, tc.h).maxBandLanes(); got != tc.want {
			t.Errorf("a %vpt plot allows %d lanes, want %d", tc.h, got, tc.want)
		}
	}
}

func TestLayoutFrameYBandThinsAndSizesTheGutter(t *testing.T) {
	// Forty row names down a 300px plot: they cannot all be drawn, and the
	// gutter must be sized by the ones that survive.
	labels := make([]string, 40)
	vals := make([]float64, 40)
	for i := range labels {
		labels[i] = "row-name-" + string(rune('a'+i%26))
		vals[i] = float64(i)
	}
	p := layoutTestPlot(600, 300)
	p.st.y.rng = Range{-0.5, 39.5}
	p.SetupAxisTicks(AxisY1, vals, labels)
	areaX, _, _, _ := p.layoutFrame(1, labelBandMaxLanes)
	if p.st.yBand.thin < 2 {
		t.Errorf("40 labels down 300px kept a stride of %d", p.st.yBand.thin)
	}
	widest := float32(0)
	for _, pl := range p.st.yBand.place {
		if w := EstimateTextWidth(p.st.ticksY[pl.tick].label, tickFontSize); w > widest {
			widest = w
		}
	}
	if areaX < widest {
		t.Errorf("left gutter %v cannot hold its widest kept label (%v)", areaX, widest)
	}
}

func TestLocateTicksFittedThinsForWideLabels(t *testing.T) {
	// Fifteen-character labels (a sign, and precision the step demands) on
	// axes wide enough for eleven of them by the pixel-density rule but not
	// by their own width. formatTick's exponent form caps most labels well
	// under this, which is why the case is a narrow one — and why the band
	// behind it still has to exist.
	rng := Range{-0.000123456789, -0.000123456779}
	slotOK := func(ticks []tick, sizePx float32) bool {
		var widest float32
		majors := 0
		for i := range ticks {
			if !ticks[i].major {
				continue
			}
			majors++
			if w := EstimateTextWidth(ticks[i].label, tickFontSize); w > widest {
				widest = w
			}
		}
		return majors < 3 || widest+tickLabelGap <= sizePx/float32(majors-1)
	}
	collided := 0
	for _, sizePx := range []float32{700, 900, 1000} {
		if slotOK(locateTicksScaled(rng, sizePx, ScaleLinear, nil), sizePx) {
			continue
		}
		collided++
		fitted := locateTicksFitted(rng, sizePx, ScaleLinear, tickLabelGap, nil)
		if !slotOK(fitted, sizePx) {
			t.Errorf("at %vpx the fitted ticks still collide: %d majors", sizePx, countMajor(fitted))
		}
		if countMajor(fitted) < 2 {
			t.Errorf("at %vpx fitted down to %d majors — an axis needs two", sizePx, countMajor(fitted))
		}
	}
	if collided == 0 {
		t.Fatal("no case collided: the test no longer exercises the fit loop")
	}
}

func TestLocateTicksFittedLeavesRoomyAxesAlone(t *testing.T) {
	// The common case must be untouched, or every existing plot moves.
	rng := Range{0, 10}
	plain := locateTicksScaled(rng, 500, ScaleLinear, nil)
	fitted := locateTicksFitted(rng, 500, ScaleLinear, tickLabelGap, nil)
	if len(plain) != len(fitted) {
		t.Fatalf("fitted %d ticks, plain %d", len(fitted), len(plain))
	}
	for i := range plain {
		if plain[i].value != fitted[i].value || plain[i].label != fitted[i].label {
			t.Errorf("tick %d differs: %v %q vs %v %q", i,
				plain[i].value, plain[i].label, fitted[i].value, fitted[i].label)
		}
	}
}

func TestLocateTicksFittedTerminatesOnDecadeLocator(t *testing.T) {
	// Log10 ignores the size hint, so the fit loop cannot converge there;
	// it must stop at the iteration cap and leave usable ticks behind.
	fitted := locateTicksFitted(Range{1e-9, 1e9}, 80, ScaleLog10, tickLabelGap, nil)
	if countMajor(fitted) == 0 {
		t.Error("no major ticks survived the fit loop")
	}
}
