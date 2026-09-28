package play

import (
	"fmt"
	"math"
	"sort"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/flowoverlay"
)

// The legend row says what the picture encodes, because nothing else on the
// pane did: a reader saw trails of some colour, length and number and could
// not tell which of the three was the data. The answer (ADR-0249 §SD4) is
// that colour is the speed, through the same palette and range as the
// particles and the time strip's bars; a trail's pace is the speed too, but
// clamped, so below one speed every trail creeps alike and above another
// every trail moves alike; and the density is a display setting. The ramp
// carries the unit and the two clamp speeds as ticks, so the reader can see
// where on the ramp the pace stops being informative.

const (
	// vectorFieldLegendRampW is the ramp's width in points; the row's
	// words take the rest of the pane.
	vectorFieldLegendRampW float32 = 240
	// vectorFieldLegendH is the row's canvas height: the ramp and one line
	// of tick labels under it. Pinned, so the legend's arrival with the
	// field does not move the map (the status row's reason).
	vectorFieldLegendH     float32 = 30
	vectorFieldLegendRampH float32 = 9
	vectorFieldLegendFont  float32 = 10
	// vectorFieldLegendMinTickGap keeps two tick labels from overprinting:
	// a tick closer than this to the one before it is drawn without its
	// label.
	vectorFieldLegendMinTickGap float32 = 28
)

// legendTick is one labelled speed on the ramp. A pinned tick is one the
// legend exists for — zero, the creep floor, the full pace — and keeps its
// label; a round tick between them gives its label up to a pinned one it
// would overprint.
type legendTick struct {
	value  float32
	label  string
	pinned bool
}

// legendTicks is the ramp's ticks for a field whose particles top the
// palette at fullSpeed and creep at floorSpeed: zero, the floor, the full
// pace, and the round speeds between them that leave room for their labels.
// The unit rides the last label only. A floor at or beyond the full pace,
// or at zero, is left out — there is then no creep to mark.
func legendTicks(floorSpeed, fullSpeed float32, unit string, rampW float32) (ticks []legendTick) {
	if !(fullSpeed > 0) {
		return
	}
	withUnit := func(v float32, last bool) (s string) {
		s = fmt.Sprintf("%.3g", v)
		if last && unit != "" {
			s += " " + unit
		}
		return
	}
	ticks = append(ticks, legendTick{value: 0, label: "0", pinned: true})
	step := niceStep(float64(fullSpeed), float64(rampW/vectorFieldLegendMinTickGap))
	for v := step; v < float64(fullSpeed)*0.999; v += step {
		ticks = append(ticks, legendTick{value: float32(v), label: withUnit(float32(v), false)})
	}
	ticks = append(ticks, legendTick{value: fullSpeed, label: withUnit(fullSpeed, true), pinned: true})
	if floorSpeed > 0 && floorSpeed < fullSpeed {
		ticks = append(ticks, legendTick{value: floorSpeed, label: withUnit(floorSpeed, false), pinned: true})
	}
	sort.Slice(ticks, func(i, j int) bool { return ticks[i].value < ticks[j].value })
	return
}

// labelledTicks is which of the sorted ticks draw their label on a ramp
// rampW wide: every pinned one, and a round one only where it is at least
// minGap from the labelled tick before it and from the pinned tick after
// it. The tick marks themselves are all drawn.
func labelledTicks(ticks []legendTick, fullSpeed, rampW, minGap float32) (labelled []bool) {
	labelled = make([]bool, len(ticks))
	x := func(i int) float32 { return rampW * ticks[i].value / fullSpeed }
	last := float32(math.Inf(-1))
	for i := range ticks {
		if ticks[i].pinned {
			labelled[i] = true
			last = x(i)
			continue
		}
		if x(i)-last < minGap {
			continue
		}
		clear := true
		for j := i + 1; j < len(ticks); j++ {
			if ticks[j].pinned {
				clear = x(j)-x(i) >= minGap
				break
			}
		}
		if clear {
			labelled[i] = true
			last = x(i)
		}
	}
	return
}

// niceStep is a 1-2-5 step over span that yields at most n intervals.
func niceStep(span float64, n float64) (step float64) {
	if !(span > 0) || !(n >= 1) {
		return span
	}
	raw := span / n
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 5, 10} {
		step = m * mag
		if step >= raw {
			return
		}
	}
	return
}

// renderVectorFieldLegend draws the row: the ramp with its ticks on a
// canvas, then the words. floorSpeed and fullSpeed are the layer's own
// (flowoverlay.Layer.Pace), so the ticks name the clamps the trails actually
// have; with no field yet the ramp is drawn without numbers.
func (inst *VectorFieldDriver) renderVectorFieldLegend(has bool, unit string) {
	g := inst.guest
	var floorSpeed, fullSpeed float32
	palette := flowoverlay.DefaultPalette
	if has && g.layer != nil {
		floorSpeed, fullSpeed = g.layer.Pace()
		if g.Opts.Palette != nil {
			palette = g.Opts.Palette
		}
	}
	span := fullSpeed
	if !(span > 0) {
		span = 1
	}
	if inst.legendColors == nil || inst.legendColorMax != span || len(inst.legendPalette) != len(palette) || (len(palette) > 0 && &inst.legendPalette[0] != &palette[0]) {
		inst.legendColors = colormap.NewConfig(palette, 0, float64(span))
		inst.legendColorMax, inst.legendPalette = span, palette
	}
	ink := color.Hex(styletokens.NeutralTextSecondary.AsHex())
	weak := color.Hex(styletokens.NeutralTextDisabled.AsHex())
	for range c.Horizontal().KeepIter() {
		c.UiSetMinHeight(vectorFieldLegendH)
		// The ramp: one rect per column, coloured as a trail of that speed.
		const cols = 48
		w := vectorFieldLegendRampW
		xs0 := make([]float32, cols)
		ys0 := make([]float32, cols)
		xs1 := make([]float32, cols)
		ys1 := make([]float32, cols)
		cs := make([]uint32, cols)
		for i := range cols {
			xs0[i], xs1[i] = w*float32(i)/cols, w*float32(i+1)/cols+0.5
			ys0[i], ys1[i] = 0, vectorFieldLegendRampH
			cs[i] = inst.legendColors.At(float64(span)*(float64(i)+0.5)/cols) | 0xff
		}
		c.PaintRectsFilled(xs0, ys0, xs1, ys1, color.ColorsFromU32(cs)).Send()
		if has && fullSpeed > 0 {
			ticks := legendTicks(floorSpeed, fullSpeed, unit, w)
			labelled := labelledTicks(ticks, fullSpeed, w, vectorFieldLegendMinTickGap)
			for i, tk := range ticks {
				x := w * tk.value / fullSpeed
				c.PaintLine(x, 0, x, vectorFieldLegendRampH+3, ink, 1).Send()
				if !labelled[i] {
					continue
				}
				anchor := uint8(1)
				if tk.value == 0 {
					anchor = 0
				} else if tk.value >= fullSpeed {
					anchor = 2
				}
				c.PaintText(x, vectorFieldLegendRampH+4, anchor, 0, tk.label, vectorFieldLegendFont, ink).Send()
			}
		} else {
			c.PaintText(0, vectorFieldLegendRampH+4, 0, 0, "speed, once the field is described", vectorFieldLegendFont, weak).Send()
		}
		c.PaintCanvas(inst.ids.PrepareStr("vf-legend"), w, vectorFieldLegendH).Send()
		c.AddSpace(vectorFieldStatusGap)
		diagWeak(legendWords(has, floorSpeed, fullSpeed, unit))
	}
}

// legendWords is the sentence beside the ramp. It names the three channels
// and what each one is: the reader's question is which of colour, pace and
// count carries the data, and the answer is one, one clamped, and none.
func legendWords(has bool, floorSpeed, fullSpeed float32, unit string) (s string) {
	u := unit
	if u != "" {
		u = " " + u
	}
	if !has || !(fullSpeed > 0) {
		return "colour and pace follow the speed; the count of trails is a display setting"
	}
	return fmt.Sprintf("colour: speed · pace: speed, clamped — below %.3g%s every trail creeps alike, above %.3g%s every trail moves alike · count: a display setting, not data",
		floorSpeed, u, fullSpeed, u)
}
