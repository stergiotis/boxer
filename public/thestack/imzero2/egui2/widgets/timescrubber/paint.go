package timescrubber

import (
	"fmt"
	"math"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/axisruler"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

type visuals struct {
	background, band, rangeFill, rangeEdge, shade color.Color
	bar, peak, playhead, playheadText, hover      color.Color
	hoverText, context, now, past, day, mark      color.Color
	held, loading, missing, idle, hint            color.Color
}

func newVisuals() visuals {
	hex := func(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }
	return visuals{
		background:   hex(styletokens.NeutralBgPanel),
		band:         hex(styletokens.NeutralBorderFaint),
		rangeFill:    withAlpha(hex(styletokens.SuccessDefault), 0x70),
		rangeEdge:    hex(styletokens.SuccessDefault),
		shade:        withAlpha(hex(styletokens.NeutralBgPanel), 0xb8),
		bar:          hex(styletokens.AccentDefault),
		peak:         hex(styletokens.NeutralTextSecondary),
		playhead:     hex(styletokens.WarningDefault),
		playheadText: hex(styletokens.NeutralTextPrimary),
		hover:        withAlpha(hex(styletokens.NeutralTextSecondary), 0x90),
		hoverText:    hex(styletokens.NeutralTextSecondary),
		context:      hex(styletokens.NeutralTextSecondary),
		now:          hex(styletokens.InfoDefault),
		past:         withAlpha(hex(styletokens.NeutralTextSecondary), 0x22),
		day:          withAlpha(hex(styletokens.NeutralTextSecondary), 0x16),
		mark:         hex(styletokens.InfoDefault),
		held:         hex(styletokens.AccentStrong),
		loading:      hex(styletokens.WarningStrong),
		missing:      hex(styletokens.ErrorDefault),
		idle:         hex(styletokens.NeutralBorderFaint),
		hint:         hex(styletokens.NeutralTextSecondary),
	}
}

func withAlpha(col color.Color, alpha uint8) color.Color {
	return color.Hex(col.Literal()&^0xff | uint32(alpha))
}

// paint draws the strip back to front.
func (inst *Scrubber) paint(axis timeAxis, steps []Step, g geometry, vis visuals) {
	c.PaintClipPush(0, 0, g.w, g.h).Send()
	cols := inst.columns[:0]
	if crowded(axis) {
		cols = columnsOf(axis, inst.shownSteps(steps), cols)
	}
	inst.columns = cols
	if !g.compact {
		inst.paintCalendar(axis, steps, g, vis)
		inst.paintBand(axis, steps, g, vis)
		inst.paintBars(axis, steps, cols, g, vis)
		inst.paintShade(axis, steps, g, vis)
		inst.paintAxis(axis, steps, g, vis)
		inst.paintMarks(axis, steps, g, vis)
	} else {
		inst.paintCompactRange(axis, steps, g, vis)
	}
	inst.paintStates(axis, steps, cols, g, vis)
	switch {
	case inst.drag == dragPlayhead && !inst.Opts.NoSnap:
		// Where a release would land, seen while the playhead is held.
		inst.paintHover(axis, steps, int(math.Round(inst.Transport.Pos)), g, vis)
	case inst.hoverValid && inst.hoverTop:
		inst.paintBandHint(g, vis)
	case inst.hoverValid:
		inst.paintHover(axis, steps, inst.hoverStep, g, vis)
	}
	inst.paintPlayhead(axis, steps, g, vis)
	c.PaintClipPop().Send()
}

// shownSteps is the steps with each state as the strip draws it.
func (inst *Scrubber) shownSteps(steps []Step) []Step {
	if len(inst.loadingSince) == 0 {
		return steps
	}
	out := append([]Step(nil), steps...)
	for i := range inst.loadingSince {
		if i < len(out) {
			out[i].State = inst.shownState(steps, i)
		}
	}
	return out
}

// paintCalendar tints what lies before now and shades alternate days: on a
// linear strip it is the days that give a forecast's diurnal signal its
// cyclic reading (ADR-0251 §SD8).
func (inst *Scrubber) paintCalendar(axis timeAxis, steps []Step, g geometry, vis visuals) {
	n := len(axis.ms)
	if !axis.ordered() {
		return
	}
	top, bottom := g.band, g.baseY
	xOf := func(ms int64) float32 { return axis.posToX(axis.msToPos(float64(ms))) }
	days := midnights(axis.ms[0], axis.ms[n-1], inst.location(), 120)
	if len(days) > 0 && len(days) < 120 {
		edges := append(append([]int64{axis.ms[0]}, days...), axis.ms[n-1])
		for i := 1; i+1 < len(edges); i += 2 {
			c.PaintRectFilled(xOf(edges[i]), top, xOf(edges[i+1]), bottom, 0, vis.day).Send()
		}
	}
	if nowMS := inst.wall().UnixMilli(); nowMS > axis.ms[0] {
		c.PaintRectFilled(axis.x0, top, xOf(min(nowMS, axis.ms[n-1])), bottom, 0, vis.past).Send()
	}
}

// flagSpan is where the playhead's time flag is drawn. Everything else that
// shares the label row — the calendar context, a mark's word, the key —
// stands out of its way, because the flag is the one that moves.
func flagSpan(x, w float32) (x0, x1 float32) {
	if x > w-flagReach-8 {
		return x - 6 - flagReach, x - 6
	}
	return x + 6, x + 6 + flagReach
}

// apart says two horizontal spans do not overlap.
func apart(a0, a1, b0, b1 float32) bool { return a1 < b0 || a0 > b1 }

// textWidth is a generous estimate of a short label's width at fontSize, for
// deciding whether it has room — never for placing it.
func textWidth(text string, fontSize float32) float32 {
	return float32(len([]rune(text))) * fontSize * 0.56
}

// paintBand draws the top band, the range on it with its grips, and the
// range's edges down the strip.
func (inst *Scrubber) paintBand(axis timeAxis, steps []Step, g geometry, vis visuals) {
	mid := g.band / 2
	c.PaintLine(axis.x0, mid, axis.x1, mid, vis.band, styletokens.StrokeHair).Send()
	t := &inst.Transport
	if !t.RangeOn {
		return
	}
	lo, hi := t.Bounds(len(steps))
	xa, xb := axis.posToX(float64(lo)), axis.posToX(float64(hi))
	c.PaintRectFilled(xa, 2, xb, g.band-2, 2, vis.rangeFill).Send()
	for _, x := range []float32{xa, xb} {
		c.PaintRectFilled(x-2, 0, x+2, g.band, 1.5, vis.rangeEdge).Send()
		c.PaintLine(x, g.band, x, g.baseY, vis.rangeEdge, styletokens.StrokeHair).Send()
	}
}

func (inst *Scrubber) paintCompactRange(axis timeAxis, steps []Step, g geometry, vis visuals) {
	t := &inst.Transport
	if !t.RangeOn {
		return
	}
	lo, hi := t.Bounds(len(steps))
	xa, xb := axis.posToX(float64(lo)), axis.posToX(float64(hi))
	c.PaintRectFilled(xa, 2, xb, g.baseY-notchH-3, 2, vis.rangeFill).Send()
}

// paintBandHint says what the band is for while the pointer is on it: the
// gesture is otherwise found by accident (ADR-0251 §SD5).
func (inst *Scrubber) paintBandHint(g geometry, vis visuals) {
	text := "drag to set the range playback loops over"
	if inst.Transport.RangeOn {
		text = "drag an edge or the body · double click clears"
	}
	c.PaintText(g.w-padX, g.band+2, 2, 0, text, 10, vis.hint).Send()
}

// paintShade dims what playback leaves out. It goes over the bars, which is
// what makes it show: under them it is the background's colour on the
// background.
func (inst *Scrubber) paintShade(axis timeAxis, steps []Step, g geometry, vis visuals) {
	t := &inst.Transport
	if !t.RangeOn {
		return
	}
	lo, hi := t.Bounds(len(steps))
	half := barWidth(axis)/2 + 1
	xa, xb := axis.posToX(float64(lo))-half, axis.posToX(float64(hi))+half
	c.PaintRectFilled(0, g.band, xa, g.baseY-notchH-4, 0, vis.shade).Send()
	c.PaintRectFilled(xb, g.band, g.w, g.baseY-notchH-4, 0, vis.shade).Send()
}

// barWidth is as wide as the narrowest gap between two steps allows.
func barWidth(axis timeAxis) (w float32) {
	w = maxBarW
	for i := 1; i < len(axis.ms); i++ {
		gap := axis.posToX(float64(i)) - axis.posToX(float64(i-1))
		w = min(w, gap*0.72)
	}
	return max(w, minBarW)
}

// valueScale is what a full-height bar stands for: the largest value, or the
// largest peak of a series that has no values. A peak above it is drawn
// clipped (ADR-0251 §SD2).
func valueScale(steps []Step) (vmax float32) {
	for i := range steps {
		vmax = maxNum(vmax, steps[i].Value)
	}
	if !(vmax > 0) {
		for i := range steps {
			vmax = maxNum(vmax, steps[i].Peak)
		}
	}
	return vmax * 1.04
}

// maxNum is the larger of two values, of which b may be NaN: the builtin max
// answers NaN if either is, and one step without a value would then leave the
// whole strip without bars.
func maxNum(a, b float32) float32 {
	if b > a {
		return b
	}
	return a
}

// paintBars draws, per step, a bar up to its value and a stem from there up
// to its peak with a cap on it — a mean and a maximum read as one mark — all
// as one batch. A crowded strip draws a column's largest value and largest
// peak in the same marks.
func (inst *Scrubber) paintBars(axis timeAxis, steps []Step, cols []column, g geometry, vis visuals) {
	top := g.band + g.labels
	full := g.baseY - top
	if full < 4 {
		return
	}
	vmax := valueScale(steps)
	if !(vmax > 0) {
		return
	}
	inst.xs0, inst.ys0, inst.xs1, inst.ys1 = inst.xs0[:0], inst.ys0[:0], inst.xs1[:0], inst.ys1[:0]
	inst.cols = inst.cols[:0]
	add := func(x0, y0, x1, y1 float32, col uint32) {
		inst.xs0 = append(inst.xs0, x0)
		inst.ys0 = append(inst.ys0, y0)
		inst.xs1 = append(inst.xs1, x1)
		inst.ys1 = append(inst.ys1, y1)
		inst.cols = append(inst.cols, col)
	}
	colorOf := func(v float32) uint32 {
		if inst.ValueColor != nil {
			return inst.ValueColor(v)
		}
		return vis.bar.Literal()
	}
	mark := func(x, half, v, p float32) {
		yv := g.baseY
		if v > 0 {
			yv = g.baseY - full*min(v/vmax, 1)
			add(x-half, yv, x+half, g.baseY, colorOf(v))
		}
		if !(p > 0 && p >= v) {
			return
		}
		if p > vmax {
			// Past the top: the stem runs out of the strip and a chevron
			// says so; the number is the hover's to give.
			add(x-stemW/2, top, x+stemW/2, yv, colorOf(p))
			add(x-half*0.6-1, top, x+half*0.6+1, top+1.5, colorOf(p))
			add(x-half*0.3-0.5, top-2.5, x+half*0.3+0.5, top-1, colorOf(p))
			return
		}
		yp := g.baseY - full*p/vmax
		add(x-stemW/2, yp, x+stemW/2, yv, colorOf(p))
		add(x-half*0.6, yp-0.75, x+half*0.6, yp+0.75, colorOf(p))
	}
	if len(cols) > 0 {
		for i := range cols {
			mark(cols[i].x, columnW/2, cols[i].value, cols[i].peak)
		}
	} else {
		half := barWidth(axis) / 2
		for i := range steps {
			mark(axis.posToX(float64(i)), half, steps[i].Value, steps[i].Peak)
		}
	}
	if len(inst.cols) > 0 {
		c.PaintRectsFilled(inst.xs0, inst.ys0, inst.xs1, inst.ys1, inst.cols).Send()
	}
}

// paintAxis draws calendar ticks under the baseline, and the coarser unit —
// the day, usually — once along the top of the bars where it changes.
func (inst *Scrubber) paintAxis(axis timeAxis, steps []Step, g geometry, vis visuals) {
	st := axisruler.DefaultStyle()
	inst.ticks = inst.ticks[:0]
	n := len(steps)
	if axis.byIndex {
		every := max(int(math.Ceil(float64(n)*tickSpacingPx/float64(max(g.w-2*padX, 1)))), 1)
		for i := 0; i < n; i += every {
			label := fmt.Sprintf("%d", i+1)
			if axis.ordered() {
				label = steps[i].At.In(inst.location()).Format("Jan 2 15:04")
			}
			inst.ticks = append(inst.ticks, axisruler.Tick{Pos: axis.posToX(float64(i)), Label: label})
		}
		axisruler.Paint(axisruler.SideBottom, g.baseY, axis.x0, axis.x1, inst.ticks, st)
		return
	}
	layout := timeticks.TimeTicks(steps[0].At, steps[n-1].At, timeticks.TimeTickOptions{
		PanelWidthPx:    int32(g.w - 2*padX),
		TargetSpacingPx: tickSpacingPx,
		Location:        inst.location(),
		PrevStep:        inst.prevTick,
		HysteresisFrac:  0.25,
	})
	inst.prevTick = layout.Step
	xOf := func(at time.Time) float32 { return axis.posToX(axis.msToPos(float64(at.UnixMilli()))) }
	for i, at := range layout.TickValues {
		if at.Before(steps[0].At) || at.After(steps[n-1].At) || i >= len(layout.TickLabels) {
			continue
		}
		inst.ticks = append(inst.ticks, axisruler.Tick{Pos: xOf(at), Label: layout.TickLabels[i]})
	}
	axisruler.Paint(axisruler.SideBottom, g.baseY, axis.x0, axis.x1, inst.ticks, st)
	if len(layout.RolloverRows) == 0 {
		return
	}
	// The rows run coarsest first, and the finest is the context a reader of
	// a three-day strip wants: the day, not the year.
	for _, label := range layout.RolloverRows[len(layout.RolloverRows)-1].Labels {
		if int(label.StartIdx) >= len(layout.TickValues) {
			continue
		}
		at := layout.TickValues[label.StartIdx]
		x := axis.x0
		if !at.Before(steps[0].At) {
			x = xOf(at)
			c.PaintLine(x, g.band, x, g.baseY, vis.band, styletokens.StrokeHair).Send()
		}
		f0, f1 := flagSpan(axis.posToX(inst.Transport.Pos), g.w)
		if apart(x+3, x+3+textWidth(label.Label, st.FontSize), f0, f1) {
			c.PaintText(x+3, g.band+2, 0, 0, label.Label, st.FontSize, vis.context).Send()
		}
		break // the first is the context the strip opens in; the rest are marked by their lines
	}
}

// paintMarks draws what the caller named: a tick in the label row with its
// word, and a line down the bars.
func (inst *Scrubber) paintMarks(axis timeAxis, steps []Step, g geometry, vis visuals) {
	n := len(axis.ms)
	if n < 2 || !axis.ordered() {
		return
	}
	f0, f1 := flagSpan(axis.posToX(inst.Transport.Pos), g.w)
	for i := range inst.Marks {
		ms := inst.Marks[i].At.UnixMilli()
		if ms < axis.ms[0] || ms > axis.ms[n-1] {
			continue
		}
		x := axis.posToX(axis.msToPos(float64(ms)))
		c.PaintDashedLine(x, g.band+g.labels, x, g.baseY, 3, 3, vis.mark, styletokens.StrokeHair).Send()
		c.PaintPolygonFilled([]float32{x - 3.5, x, x + 3.5, x}, []float32{g.band + 8, g.band + 4.5, g.band + 8, g.band + 11.5}, vis.mark).Send()
		if inst.Marks[i].Label == "" {
			continue
		}
		mw := textWidth(inst.Marks[i].Label, 10)
		anchorH, tx, m0 := uint8(0), x+6, x+6
		if x > g.w-padX-mw {
			anchorH, tx, m0 = 2, x-6, x-6-mw
		}
		if apart(m0, m0+mw, f0, f1) {
			c.PaintText(tx, g.band+2, anchorH, 0, inst.Marks[i].Label, 10, vis.mark).Send()
		}
	}
}

// paintStates marks every step on the baseline by the state of its data, and
// tells the states apart by shape: the bars' hue is the data's (ADR-0251
// §SD6). Held is a filled notch, idle a short tick, loading an outlined
// notch, missing a cross through the baseline.
func (inst *Scrubber) paintStates(axis timeAxis, steps []Step, cols []column, g geometry, vis visuals) {
	inst.xs0, inst.ys0, inst.xs1, inst.ys1 = inst.xs0[:0], inst.ys0[:0], inst.xs1[:0], inst.ys1[:0]
	inst.cols = inst.cols[:0]
	one := func(x float32, state StepStateE) {
		switch state {
		case StepStateHeld:
			inst.xs0 = append(inst.xs0, x-2)
			inst.ys0 = append(inst.ys0, g.baseY-notchH-3)
			inst.xs1 = append(inst.xs1, x+2)
			inst.ys1 = append(inst.ys1, g.baseY+1)
			inst.cols = append(inst.cols, vis.held.Literal())
		case StepStateLoading:
			c.PaintRectStroke(x-2.5, g.baseY-notchH-3, x+2.5, g.baseY+1, 0, vis.loading, styletokens.StrokeRegular).Send()
		case StepStateMissing:
			c.PaintLine(x-3, g.baseY-4, x+3, g.baseY+2, vis.missing, styletokens.StrokeRegular).Send()
			c.PaintLine(x-3, g.baseY+2, x+3, g.baseY-4, vis.missing, styletokens.StrokeRegular).Send()
		default:
			inst.xs0 = append(inst.xs0, x-0.5)
			inst.ys0 = append(inst.ys0, g.baseY-notchH+1)
			inst.xs1 = append(inst.xs1, x+0.5)
			inst.ys1 = append(inst.ys1, g.baseY+1)
			inst.cols = append(inst.cols, vis.idle.Literal())
		}
	}
	if len(cols) > 0 {
		for i := range cols {
			one(cols[i].x, cols[i].state)
		}
	} else {
		for i := range steps {
			one(axis.posToX(float64(i)), inst.shownState(steps, i))
		}
	}
	if len(inst.cols) > 0 {
		c.PaintRectsFilled(inst.xs0, inst.ys0, inst.xs1, inst.ys1, inst.cols).Send()
	}
}

func (inst *Scrubber) paintHover(axis timeAxis, steps []Step, step int, g geometry, vis visuals) {
	i := min(max(step, 0), len(steps)-1)
	x := axis.posToX(float64(i))
	c.PaintLine(x, g.band, x, g.baseY, vis.hover, styletokens.StrokeHair).Send()
	if g.compact {
		return
	}
	text := fmt.Sprintf("step %d", i+1)
	if axis.ordered() {
		text = newLabels(axis.ms, inst.location()).short(steps[i].At)
	}
	text += inst.valueWords(steps, i)
	switch steps[i].State {
	case StepStateLoading:
		text += " · loading"
	case StepStateMissing:
		text += " · missing"
	case StepStateIdle:
		text += " · not loaded"
	}
	anchorH, tx := uint8(0), x+5
	if x > g.w/2 {
		anchorH, tx = 2, x-5
	}
	c.PaintText(tx, g.baseY-notchH-5, anchorH, 2, text, 11, vis.hoverText).Send()
}

func (inst *Scrubber) paintPlayhead(axis timeAxis, steps []Step, g geometry, vis visuals) {
	pos := inst.Transport.Pos
	x := axis.posToX(pos)
	if n := len(axis.ms); axis.ordered() && !g.compact {
		// Where the wall clock stands in a series that spans it — the line
		// between analysis and forecast.
		if nowMS := inst.wall().UnixMilli(); nowMS > axis.ms[0] && nowMS < axis.ms[n-1] {
			xn := axis.posToX(axis.msToPos(float64(nowMS)))
			c.PaintLine(xn, g.band, xn, g.baseY, vis.now, styletokens.StrokeHair).Send()
			f0, f1 := flagSpan(x, g.w)
			if w := textWidth("now", 10); apart(xn+3, xn+3+w, f0, f1) {
				c.PaintText(xn+3, g.band+2, 0, 0, "now", 10, vis.now).Send()
			}
		}
	}
	lo, _ := inst.Transport.Bounds(len(axis.ms))
	if xlo := axis.posToX(float64(lo)); x > xlo {
		c.PaintLine(xlo, g.baseY, x, g.baseY, vis.playhead, styletokens.StrokeRegular).Send()
	}
	top := max(g.band-1, 1)
	// The bars behind it are of any colour, so its contrast comes from an
	// outline in the panel's and not from its own hue.
	c.PaintLine(x, top, x, g.baseY+2, vis.background, styletokens.StrokeRegular+2).Send()
	c.PaintLine(x, top, x, g.baseY+2, vis.playhead, styletokens.StrokeRegular).Send()
	if g.compact {
		return
	}
	c.PaintPolygonFilled([]float32{x - 5, x + 5, x}, []float32{g.band - 6, g.band - 6, g.band + 1}, vis.playhead).Send()
	if !axis.ordered() {
		return
	}
	lab := newLabels(axis.ms, inst.location())
	text := lab.short(axis.timeAt(pos, inst.location()))
	anchorH, tx := uint8(0), x+6
	if x > g.w-flagReach-8 {
		anchorH, tx = 2, x-6
	}
	c.PaintText(tx, g.band+2, anchorH, 0, text, 11, vis.playheadText).Send()
	inst.paintKey(steps, x, g, vis)
}

// paintKey says what the bar and the cap are, at the end of the label row,
// where the playhead's flag is not (ADR-0251 §SD6).
func (inst *Scrubber) paintKey(steps []Step, playheadX float32, g geometry, vis visuals) {
	if inst.Opts.ValueName == "" || !(valueScale(steps) > 0) || inst.hoverTop && inst.hoverValid {
		return
	}
	text := "bar " + inst.Opts.ValueName + " · cap " + inst.peakName()
	if inst.Opts.ValueUnit != "" {
		text += " · " + inst.Opts.ValueUnit
	}
	kw := textWidth(text, 10)
	f0, f1 := flagSpan(playheadX, g.w)
	if g.w < kw+2*padX+contextReach || !apart(g.w-padX-kw, g.w-padX, f0, f1) {
		return
	}
	c.PaintText(g.w-padX, g.band+2, 2, 0, text, 10, vis.hint).Send()
}
