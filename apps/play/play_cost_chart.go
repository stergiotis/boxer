package play

import (
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/math/numerical/finddivisions"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// play_cost_chart.go paints the Diagnostics pane's cost waterfall: a staggered
// horizontal bar per phase, offset to when it ran and sized by what it cost —
// the shape a browser's network Timing tab uses, for the same reason. The
// phases here are strictly sequential, so where a bar STARTS is information,
// and a reader finds the expensive one by looking rather than by reading.
//
// Local to play rather than a widget under egui2/widgets: it draws one
// panel's data shape, and a new widget contract is a decision this does not
// need (ADR-0192).

// costToneE classifies a bar by what its time bought. Colour carries the
// verdict so the chart needs no per-row prose: length says how long, hue says
// whether the wait produced anything.
type costToneE uint8

const (
	costToneInvalid costToneE = iota
	// costToneRewrote — the pass changed the statement. Time well spent.
	costToneRewrote
	// costToneUnchanged — it ran, cost a full re-parse, and handed back what
	// it was given. This is the tone a reader is hunting for.
	costToneUnchanged
	// costToneFailed — it errored and its rewrite was dropped (ADR-0108 §SD3).
	costToneFailed
	// costToneClient / costToneServer / costToneTransfer are the run-level
	// tier: what this process spent compiling, what the server spent, and the
	// remainder that is neither.
	costToneClient
	costToneServer
	costToneTransfer
)

func (inst costToneE) color() color.Color {
	hex := func(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }
	switch inst {
	case costToneRewrote:
		return hex(styletokens.AccentDefault)
	case costToneUnchanged:
		return hex(styletokens.NeutralTextDisabled)
	case costToneFailed:
		return hex(styletokens.ErrorDefault)
	case costToneClient:
		return hex(styletokens.WarningDefault)
	case costToneServer:
		return hex(styletokens.InfoDefault)
	case costToneTransfer:
		return hex(styletokens.NeutralBorderDefault)
	}
	return hex(styletokens.NeutralTextDisabled)
}

// costBar is one row of a waterfall: a span on a shared timeline that starts
// at Start and lasts Dur, drawn at Depth's indent.
type costBar struct {
	Label string
	Start time.Duration
	Dur   time.Duration
	Depth int
	Tone  costToneE
	// Note rides beside the label — the fixed-point iteration count, where a
	// pass looped. Empty otherwise.
	Note string
}

// Chart metrics, in logical points. Rows are tight on purpose: the whole point
// is that a dozen phases fit in one glance without scrolling.
const (
	costRowH       float32 = 15
	costBarH       float32 = 9
	costAxisH      float32 = 16
	costLabelMinW  float32 = 110
	costLabelMaxW  float32 = 260
	costDurW       float32 = 58 // right gutter holding the ms figure
	costFontSize   float32 = 11
	costIndentStep float32 = 10
)

// renderCostWaterfall paints bars against a [0, span] timeline and returns the
// index of the bar the pointer is over, or -1. The hit test is the previous
// frame's (immediate-mode lag), like every other sense region.
//
// span is passed rather than derived so two charts stacked in one pane can
// share a scale where that is meaningful, and so a chart whose bars do not
// reach the end still shows the end.
func renderCostWaterfall(seq uint64, bars []costBar, span time.Duration, w float32) (hovered int) {
	hovered = -1
	if len(bars) == 0 || span <= 0 || w <= 0 {
		return
	}
	sm := c.CurrentApplicationState.StateManager
	h := float32(len(bars))*costRowH + costAxisH

	labelW := min(max(w*0.42, costLabelMinW), costLabelMaxW)
	trackX0 := labelW
	trackX1 := max(w-costDurW, trackX0+20)
	trackW := trackX1 - trackX0
	atTime := func(d time.Duration) float32 {
		return trackX0 + trackW*float32(float64(d)/float64(span))
	}

	hex := func(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }
	inkPrimary := hex(styletokens.NeutralTextPrimary)
	inkWeak := hex(styletokens.NeutralTextSecondary)
	gridCol := hex(styletokens.NeutralBorderDefault)

	wis := c.NewWidgetIdStack()
	for range c.IdScope(wis.PrepareHighEntropy(seq)) {
		for i := range bars {
			resp := sm.GetResponse(widgethandle.Make(wis.PrepareStr("bar" + strconv.Itoa(i)).Derive()))
			if resp.HasHovered() {
				hovered = i
			}
		}

		// Gridlines first, so the bars sit over them. Ticks come from the
		// shared nice-number helper, but the SCALE stays exactly [0, span]:
		// Heckbert extends its view to round bounds, and a bar drawn against an
		// extended axis would misreport its own duration.
		axisY := float32(len(bars)) * costRowH
		if ax, err := finddivisions.Heckbert(0, float64(span.Milliseconds()), 5); err == nil {
			for _, tv := range ax.TickValues {
				d := time.Duration(tv) * time.Millisecond
				if d < 0 || d > span {
					continue
				}
				x := atTime(d)
				c.PaintLine(x, 0, x, axisY, gridCol, 0.5).Send()
				c.PaintText(x, axisY+2, 1, 0, strconv.FormatInt(int64(tv), 10), costFontSize*0.9, inkWeak).Send()
			}
		}
		c.PaintLine(trackX0, axisY, trackX1, axisY, gridCol, 1).Send()
		c.PaintText(trackX1+4, axisY+2, 0, 0, "ms", costFontSize*0.9, inkWeak).Send()

		for i, b := range bars {
			rowY := float32(i) * costRowH
			midY := rowY + costRowH/2

			label := b.Label
			if b.Note != "" {
				label += " " + b.Note
			}
			c.PaintText(float32(b.Depth)*costIndentStep, midY, 0, 1, label, costFontSize, barInk(b, inkPrimary, inkWeak)).Send()

			// A toneless row is a SUMMARY, not a span — the collapsed remainder
			// of an expanded unit, whose members are interleaved in time with
			// the rows above it. Drawing any single bar for it would place work
			// at a time it did not happen, so it gets a figure and no bar.
			if b.Tone != costToneInvalid {
				x0 := atTime(b.Start)
				x1 := atTime(b.Start + b.Dur)
				// A sub-millisecond span still gets a visible sliver: a bar
				// that vanished would read as a phase that did not run.
				if x1-x0 < 1.5 {
					x1 = x0 + 1.5
				}
				c.PaintRectFilled(x0, midY-costBarH/2, min(x1, trackX1), midY+costBarH/2, 1.5, b.Tone.color()).Send()
			}

			c.PaintText(w, midY, 2, 1, formatCostDur(b.Dur), costFontSize, inkWeak).Send()
			c.PaintSenseRegion(wis.PrepareStr("bar"+strconv.Itoa(i)), 0, rowY, w, costRowH).Send()
		}

		c.PaintCanvas(wis.PrepareStr("canvas"), w, h).Send()
	}
	return
}

// barInk dims a sub-pass row so the units it belongs to stay the primary
// reading, and greys out a phase that changed nothing.
func barInk(b costBar, primary color.Color, weak color.Color) color.Color {
	if b.Depth > 0 || b.Tone == costToneUnchanged {
		return weak
	}
	return primary
}
