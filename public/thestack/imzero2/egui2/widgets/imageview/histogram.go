package imageview

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

const (
	histogramHeight = 64
	// handleGrab is how wide a limit's handle is to the pointer.
	handleGrab = 10
	// minLimitSpan keeps the two limits apart, as a share of the strip's
	// axis: a recipe needs its upper limit above its lower one.
	minLimitSpan = 1e-4
)

// histResult is a histogram a goroutine has finished, for the image and
// plane it names.
type histResult struct {
	gen   uint64
	plane int
	hist  *stats.Histogram
}

// limitDrag is a drag of one limit's handle in progress. The strip's axis is
// held as it was at the press, so the value under the pointer does not slide
// while the limits — which the axis is fitted around — move; and the limit
// is its value at the press plus what the pointer has travelled since, so a
// press that does not move changes nothing.
type limitDrag struct {
	active  bool
	upper   bool
	axisLo  float64
	axisHi  float64
	origin  float64
	pressPx float32
}

// shownPlaneIndex is the plane a single-plane view shows.
func (inst *Viewer) shownPlaneIndex() int { return max(inst.plane, 0) }

// Histogram is the histogram of the plane a single-plane view shows, over
// the whole plane. ok is false for a colour composite, and until the count
// is done: it runs on a goroutine, which the first call starts, and a later
// Render takes over its result.
func (inst *Viewer) Histogram() (h stats.Histogram, ok bool) {
	if !inst.singlePlane() {
		return h, false
	}
	if p := inst.hists[inst.shownPlaneIndex()]; p != nil {
		return *p, true
	}
	inst.wantHistogram()
	return h, false
}

// wantHistogram counts the shown plane on a goroutine unless it is counted
// or being counted already.
func (inst *Viewer) wantHistogram() {
	idx := inst.shownPlaneIndex()
	if inst.hists[idx] != nil || inst.histWanted[idx] {
		return
	}
	inst.histWanted[idx] = true
	plane, gen := inst.img.Plane(idx), inst.buildGen
	inst.building.Go(func() {
		h := stats.Of(plane)
		inst.histDone <- histResult{gen: gen, plane: idx, hist: &h}
	})
}

// adoptHistograms takes over finished histograms of the current image and
// applies a percentile stretch that was waiting for one.
func (inst *Viewer) adoptHistograms() {
	for {
		select {
		case r := <-inst.histDone:
			if r.gen != inst.buildGen {
				continue
			}
			inst.hists[r.plane] = r.hist
			delete(inst.histWanted, r.plane)
		default:
			inst.applyPendingPercentile()
			return
		}
	}
}

// SetPercentileLimits shows a single plane between two percentiles of its
// values, a stretch a few extreme samples cannot pull wide. The histogram it
// needs is counted off the frame; when it is not ready the stretch is
// applied by the Render that finds it so. A colour composite is refused: its
// limits are not a property of one plane.
func (inst *Viewer) SetPercentileLimits(percentLo, percentHi float64) error {
	if !inst.singlePlane() {
		return eb.Build().Errorf("percentile limits apply to a single plane; show one first")
	}
	if !(percentLo >= 0 && percentLo < percentHi && percentHi <= 100) {
		return eb.Build().Float64("lo", percentLo).Float64("hi", percentHi).
			Errorf("percentiles must be an ascending pair within 0 and 100")
	}
	inst.pendingPercentile, inst.percentLo, inst.percentHi = true, percentLo, percentHi
	inst.wantHistogram()
	inst.applyPendingPercentile()
	return nil
}

func (inst *Viewer) applyPendingPercentile() {
	if !inst.pendingPercentile || !inst.singlePlane() {
		return
	}
	p := inst.hists[inst.shownPlaneIndex()]
	if p == nil {
		return
	}
	h := *p
	inst.pendingPercentile = false
	limits, ok := display.PercentileLimits(h, inst.percentLo, inst.percentHi)
	if !ok {
		// The percentiles fall on one value: there is no range to show, and
		// the view keeps the one it has.
		return
	}
	recipe := inst.recipe
	recipe.Limits = limits
	_ = inst.SetRecipe(recipe)
}

// histogramAxis is the range of values the strip spans: the plane's data
// and the limits, whichever reach further, so both handles are on it.
func (inst *Viewer) histogramAxis(h stats.Histogram) (lo, hi float64) {
	lo, hi = inst.recipe.Limits.Lo, inst.recipe.Limits.Hi
	if a, b, ok := h.Range(); ok {
		lo, hi = min(lo, a), max(hi, b)
	}
	return lo, hi
}

// renderHistogram draws the strip under the image and applies last frame's
// drag of a limit handle. It reports whether the limits changed.
func (inst *Viewer) renderHistogram(w float32) (changed bool) {
	const h = float32(histogramHeight)
	sm := c.CurrentApplicationState.StateManager
	hist, ready := inst.Histogram()
	axisLo, axisHi := inst.histogramAxis(hist)
	if inst.drag.active {
		axisLo, axisHi = inst.drag.axisLo, inst.drag.axisHi
	}
	span := axisHi - axisLo
	toPx := func(value float64) float32 { return float32((value - axisLo) / span * float64(w)) }

	// ---- input: the handle regions' flags and the pointer, one frame old.
	canvasH := widgethandle.Make(inst.ids.PrepareStr("hist").Derive())
	loH := widgethandle.Make(inst.ids.PrepareStr("hist-lo").Derive())
	hiH := widgethandle.Make(inst.ids.PrepareStr("hist-hi").Derive())
	cur, live := sm.GetCanvasCursor(canvasH)
	ptr := sm.GetPointer()
	loFlags, hiFlags := sm.GetResponse(loH), sm.GetResponse(hiH)
	pointerPx, pointerOk := float32(0), false
	if live && ptr.Valid && !math.IsNaN(float64(cur.OriginX)) {
		pointerPx, pointerOk = ptr.X-cur.OriginX, true
	}
	if !inst.drag.active && pointerOk && (loFlags.HasDragStarted() || hiFlags.HasDragStarted()) {
		upper := hiFlags.HasDragStarted()
		at := inst.recipe.Limits.Lo
		if upper {
			at = inst.recipe.Limits.Hi
		}
		inst.drag = limitDrag{active: true, upper: upper, axisLo: axisLo, axisHi: axisHi, origin: at, pressPx: pointerPx}
	}
	if inst.drag.active {
		flags := loFlags
		if inst.drag.upper {
			flags = hiFlags
		}
		if pointerOk && (flags.HasDragged() || flags.HasDragStarted() || flags.HasIsPointerButtonDown()) {
			value := inst.drag.origin + float64(pointerPx-inst.drag.pressPx)/float64(w)*span
			recipe := inst.recipe
			gap := span * minLimitSpan
			if inst.drag.upper {
				recipe.Limits.Hi = max(value, recipe.Limits.Lo+gap)
			} else {
				recipe.Limits.Lo = min(value, recipe.Limits.Hi-gap)
			}
			// Only a limit that moved becomes one a person set.
			if recipe.Limits != inst.recipe.Limits {
				recipe.Limits.Source, recipe.Limits.PercentLo, recipe.Limits.PercentHi = display.LimitsSourceManual, 0, 0
				changed = inst.SetRecipe(recipe) == nil
			}
		}
		if flags.HasDragStopped() || !(flags.HasDragged() || flags.HasDragStarted() || flags.HasIsPointerButtonDown()) {
			inst.drag = limitDrag{}
		}
		c.RequestRepaint()
	}

	// ---- paint: the counts on a logarithmic height, dimmed outside the
	// limits, and the two handles.
	xLo, xHi := toPx(inst.recipe.Limits.Lo), toPx(inst.recipe.Limits.Hi)
	if ready && span > 0 {
		inst.paintHistogramBars(hist, axisLo, span, w, h, xLo, xHi)
	}
	handle := color.Hex(styletokens.AccentDefault.AsHex())
	for _, x := range [2]float32{xLo, xHi} {
		c.PaintLine(x, 0, x, h, handle, styletokens.StrokeStrong).Send()
	}
	c.PaintRectStroke(0, 0, w, h, 0, color.Hex(styletokens.NeutralBorderFaint.AsHex()), styletokens.StrokeHair).Send()
	text := color.Hex(styletokens.NeutralTextSecondary.AsHex())
	c.PaintText(3, 2, 0, 0, formatValue(axisLo), barFontSize, text).Send()
	c.PaintText(w-3, 2, 2, 0, formatValue(axisHi), barFontSize, text).Send()

	// The handles' regions go last so they win the hit test; the upper one
	// after the lower, so where they overlap the upper limit is the one
	// taken — it can always be dragged off the lower.
	c.PaintSenseRegion(inst.ids.PrepareStr("hist-lo"), xLo-handleGrab/2, 0, handleGrab, h).Send()
	c.PaintSenseRegion(inst.ids.PrepareStr("hist-hi"), xHi-handleGrab/2, 0, handleGrab, h).Send()
	c.PaintCanvas(inst.ids.PrepareStr("hist"), w, h).Sense(false, false, true).Send()
	return changed
}

// paintHistogramBars draws one bar per two pixels of the strip: the count of
// the samples whose values fall under it, on a logarithmic height so that a
// handful of samples is still visible beside a million.
func (inst *Viewer) paintHistogramBars(hist stats.Histogram, axisLo, span float64, w, h, xLo, xHi float32) {
	n := max(int(w/2), 1)
	if cap(inst.histBars) < n {
		inst.histBars = make([]uint64, n)
	}
	bars := inst.histBars[:n]
	clear(bars)
	for i, count := range hist.Counts {
		if count == 0 {
			continue
		}
		value := hist.Value(hist.BinStored(i))
		bar := int((value - axisLo) / span * float64(n))
		bars[min(max(bar, 0), n-1)] += count
	}
	var top uint64
	for _, v := range bars {
		top = max(top, v)
	}
	if top == 0 {
		return
	}
	inst.histX0, inst.histY0, inst.histX1, inst.histY1 = inst.histX0[:0], inst.histY0[:0], inst.histX1[:0], inst.histY1[:0]
	inst.histCols = inst.histCols[:0]
	inside, outside := styletokens.NeutralDefault.AsHex(), styletokens.NeutralBorderFaint.AsHex()
	barW := w / float32(n)
	scale := float64(h-14) / math.Log1p(float64(top))
	for i, v := range bars {
		if v == 0 {
			continue
		}
		x := float32(i) * barW
		colour := inside
		if mid := x + barW/2; mid < xLo || mid > xHi {
			colour = outside
		}
		inst.histX0 = append(inst.histX0, x)
		inst.histX1 = append(inst.histX1, x+barW)
		inst.histY0 = append(inst.histY0, h-float32(math.Log1p(float64(v))*scale)-1)
		inst.histY1 = append(inst.histY1, h)
		inst.histCols = append(inst.histCols, colour)
	}
	c.PaintRectsFilled(inst.histX0, inst.histY0, inst.histX1, inst.histY1, color.ColorsFromU32(inst.histCols)).Send()
}
