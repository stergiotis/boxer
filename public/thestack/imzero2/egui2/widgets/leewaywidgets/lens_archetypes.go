package leewaywidgets

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwlens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The archetype form draws a band as one line — its template, each slot at
// how typical it is or what it typically holds — and under it only the rows
// that break the pattern. What it says is lwlens.Archetypes; this file is how
// it is drawn.

// lensExceptionLines bounds the exception lines drawn per band.
const lensExceptionLines = 6

// templateText is what a band typically holds in slot s, in at most n
// runes: the longest of its readings that fits, so a narrow column drops the
// spread or the share before it cuts the value.
func (inst *lensPainter) templateText(b *lwlens.Band, s int32, n int) string {
	ts := inst.templateTexts(lwlens.BandTypical(inst.a, b, s))
	for _, t := range ts {
		if utf8.RuneCountInString(t) <= n {
			return t
		}
	}
	return lensFit(ts[len(ts)-1], n)
}

// templateTexts are the readings of t, longest first.
func (inst *lensPainter) templateTexts(t lwlens.Typical) (out []string) {
	s := t.Slot
	if t.Numeric {
		med := inst.numText(s, t.Median)
		if inst.p.Detail == lwlens.DetailValues && t.Count > 2 {
			out = append(out, fmt.Sprintf("%s (%s–%s)", med, inst.numText(s, t.Low), inst.numText(s, t.High)))
		}
		return append(out, med, lensShortNum(t.Median))
	}
	if t.Share < 0.5 && t.Count > 1 {
		// Timestamps are read as the span they cover.
		if first, ok := lensShortTime(t.First); ok {
			if last, ok := lensShortTime(t.Last); ok {
				return []string{first + "…" + last, first[:5] + "…" + last[:5]}
			}
		}
		return []string{fmt.Sprintf("%d distinct", t.Distinct), fmt.Sprintf("(%d)", t.Distinct)}
	}
	m := t.Mode
	if ts, ok := lensShortTime(m); ok {
		m = ts
	}
	if inst.p.Detail == lwlens.DetailValues && t.Share < 1 {
		out = append(out, fmt.Sprintf("%s %.0f%%", m, 100*t.Share))
	}
	return append(out, m, inst.a.Stats[s].Elide(m))
}

// paintTemplateCell draws one slot of a band's template in [x, x+w).
func (inst *lensPainter) paintTemplateCell(x, cy float32, s int32, b *lwlens.Band) {
	support := b.Support[s]
	if support == 0 {
		return
	}
	w := inst.cellW(s)
	m := inst.a.Model
	switch inst.p.Detail {
	case lwlens.DetailShape:
		// How typical: the square fills from the bottom by the band's
		// support, over a rim the full square's size.
		h := lensShapeCell
		c.PaintRectStroke(x+0.5, cy-h/2+0.5, x+h-0.5, cy+h/2-0.5, 1, inst.sectionTone[m.SectionOf(s)], styletokens.StrokeHair).Send()
		c.PaintRectFilled(x, cy+h/2-h*support, x+h, cy+h/2, 1, inst.sectionTone[m.SectionOf(s)]).Send()
	case lwlens.DetailFingerprint:
		inst.paintDistribution(x, x+lensFingerCell, cy, s, b)
	default:
		n := inst.cellChars(s)
		txt := inst.templateText(b, s, n)
		pri := lensTok(styletokens.NeutralTextPrimary)
		if support < 0.95 {
			pri = lensTok(styletokens.NeutralTextSecondary)
		}
		if m.Slots[s].Kind == lwlens.ValueKindNumeric && inst.p.Detail == lwlens.DetailGist {
			inst.paintDistribution(x, x+w-6, cy+7, s, b)
		}
		if m.Slots[s].Kind == lwlens.ValueKindNumeric {
			inst.textRight(x+w-8, cy-1, txt, lensFont, pri)
		} else {
			inst.text(x, cy-1, txt, lensFont, pri)
		}
	}
}

// paintDistribution draws a band's values in slot s over [x0, x1]: for a
// number, the band's 10th–90th percentile as a bar and its median as a tick
// over the slot's whole range; for a label, the band's shares of the slot's
// most frequent labels, in their chip colours.
func (inst *lensPainter) paintDistribution(x0, x1, cy float32, s int32, b *lwlens.Band) {
	t := lwlens.BandTypical(inst.a, b, s)
	_, texts := lwlens.BandValues(inst.a, b, s)
	st := &inst.a.Stats[s]
	track := lensMix(styletokens.NeutralBgSurface, styletokens.NeutralTextSecondary, 0.2)
	if t.Numeric && len(st.Sorted) > 1 {
		lo, hi := st.Sorted[0], st.Sorted[len(st.Sorted)-1]
		at := func(v float64) float32 {
			if hi == lo {
				return (x0 + x1) / 2
			}
			return x0 + (x1-x0)*float32((v-lo)/(hi-lo))
		}
		c.PaintRectFilled(x0, cy-1, x1, cy+1, 0, track).Send()
		a, z := at(t.Low), at(t.High)
		c.PaintRectFilled(a, cy-2.5, max(z, a+2), cy+2.5, 1, lensTok(styletokens.NeutralTextSecondary)).Send()
		m := at(t.Median)
		c.PaintRectFilled(m-1, cy-4, m+1, cy+4, 0, lensTok(styletokens.NeutralTextExtreme)).Send()
		return
	}
	if len(texts) == 0 {
		return
	}
	x := x0
	counts := map[string]int{}
	for _, t := range texts {
		counts[t]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(p, q string) int { return st.CategoryRank(p) - st.CategoryRank(q) })
	for _, k := range keys {
		w := (x1 - x0) * float32(counts[k]) / float32(len(texts))
		cell := lwlens.Cell{Slot: s, Text: k}
		col, ok := inst.chipTone(s, &cell)
		if !ok {
			col = lensTok(styletokens.NeutralBorderFaint)
		}
		c.PaintRectFilled(x, cy-4, x+max(w-1, 1), cy+4, 0, col).Send()
		x += w
	}
}

// exception is an lwlens.Exception as drawn: its rows, and its departures
// as text.
type exception struct {
	rows     []int32
	parts    []string
	class    lwlens.ExceptionClassE
	surprise float64
}

// exceptions formats a band's exceptions, most telling first.
func (inst *lensPainter) exceptions(pb *lwlens.PlanBand, b *lwlens.Band) (out []exception) {
	m := inst.a.Model
	for _, e := range lwlens.BandExceptions(inst.a, inst.p.Detail, pb, b) {
		parts := make([]string, 0, len(e.Departures))
		for _, d := range e.Departures {
			switch d.Kind {
			case lwlens.DepartureKindMissing:
				parts = append(parts, "−"+m.Slots[d.Slot].Label())
			case lwlens.DepartureKindUnexpected:
				parts = append(parts, "+"+m.Slots[d.Slot].Label())
			case lwlens.DepartureKindRareLabel:
				parts = append(parts, fmt.Sprintf("%s %s (%d of %d rows)", inst.memberName(d.Slot),
					lensFit(d.Text, 24), d.Count, len(b.Rows)))
			case lwlens.DepartureKindOutlier:
				dir := "↓"
				if d.High {
					dir = "↑"
				}
				parts = append(parts, fmt.Sprintf("%s %s%s", inst.memberName(d.Slot), inst.numText(d.Slot, d.Value), dir))
			}
		}
		out = append(out, exception{rows: e.Rows, parts: parts, class: e.Class, surprise: e.Surprise})
	}
	return
}

// paintArchetypes draws every band as its template and its exceptions.
func (inst *lensPainter) paintArchetypes() {
	inst.paintSummary()
	inst.paintLegend()
	if inst.p.Scope == lwlens.ScopeGlobal {
		inst.paintConstants(inst.p.Constants, "every row:")
		inst.paintFrameHeader(inst.p.Frame)
	}
	rh := lensRowHeight(inst.p.Detail)
	if inst.p.Detail == lwlens.DetailGist {
		rh += 4 // the distribution under the text
	}
	sec := lensTok(styletokens.NeutralTextSecondary)
	for bi := range inst.p.Bands {
		pb := &inst.p.Bands[bi]
		b := &inst.a.Bands[pb.Band]
		inst.reserveFor(len(inst.p.Bands) - bi - 1)
		if inst.y+3*rh > inst.h-lensPad-inst.reserve {
			inst.reserve = 0
			inst.paintBandStubs(inst.p.Bands[bi:])
			return
		}
		if bi > 0 {
			inst.y += lensBandGap
		}
		inst.paintBandHeader(b, pb.Band)
		if b.Cluster < 0 {
			// Rows no cluster took have no template to break: each is drawn
			// on its own terms.
			for i := range pb.Rows {
				if inst.y+rh > inst.h-lensPad-inst.reserve {
					inst.text(lensPad, inst.y+rh/2, fmt.Sprintf("… %d more rows", len(pb.Rows)-i), lensSmallFont, sec)
					inst.y += rh
					break
				}
				// Its own slots, all of them: under a shared frame the plan
				// lists only those outside it.
				pr := pb.Rows[i]
				pr.Own = nil
				for _, cell := range inst.a.Model.Rows[pr.Row].Cells {
					sl := inst.a.Model.Slots[cell.Slot]
					if inst.p.Detail != lwlens.DetailShape || (!sl.Plain && inst.a.Support[cell.Slot] < 1) {
						pr.Own = append(pr.Own, cell.Slot)
					}
				}
				slices.SortStableFunc(pr.Own, func(x, y int32) int {
					return slices.Index(inst.a.Order, x) - slices.Index(inst.a.Order, y)
				})
				inst.paintRow(&pr, nil, rh)
				inst.y += rh
			}
			continue
		}
		inst.paintConstants(pb.Constants, "all:")
		frame := inst.p.Frame
		switch inst.p.Scope {
		case lwlens.ScopeBand:
			frame = pb.Frame
			inst.paintFrameHeader(frame)
		case lwlens.ScopeRow:
			frame = nil
			for _, s := range inst.a.Order {
				if b.Support[s] >= lwlens.TemplateAt && !slices.Contains(pb.Constants, s) &&
					(inst.p.Detail != lwlens.DetailShape || (!inst.a.Model.Slots[s].Plain && inst.a.Support[s] < 1)) {
					frame = append(frame, s)
				}
			}
			inst.paintFrameHeader(frame)
		}
		cy := inst.y + rh/2
		inst.text(lensPad, cy, "typical", lensFont, sec)
		n := inst.frameFit(frame)
		xs := inst.frameXs(frame[:n])
		for i, s := range frame[:n] {
			inst.paintTemplateCell(xs[i], cy, s, b)
		}
		inst.y += rh + 2
		if inst.p.Detail >= lwlens.DetailGist && inst.y+rh <= inst.h-lensPad-inst.reserve {
			inst.paintExtremes(inst.extremes(frame, b), rh)
		}
		exc := inst.exceptions(pb, b)
		for i, e := range exc {
			if i == lensExceptionLines || inst.y+rh > inst.h-lensPad-inst.reserve {
				rest := 0
				for _, e := range exc[i:] {
					rest += len(e.rows)
				}
				inst.text(lensPad, inst.y+rh/2, fmt.Sprintf("… %d more rows with exceptions", rest), lensSmallFont, sec)
				inst.y += rh
				break
			}
			inst.paintException(e, rh)
		}

	}
}

// extremes formats the band's extremes over the numeric slots of frame.
func (inst *lensPainter) extremes(frame []int32, b *lwlens.Band) (parts []string) {
	for _, e := range lwlens.BandExtremes(inst.a, b, frame) {
		parts = append(parts, fmt.Sprintf("%s ↓%s %s ↑%s %s", inst.memberName(e.Slot),
			inst.rowLabel(e.LowRow), inst.numText(e.Slot, e.LowValue), inst.rowLabel(e.HighRow), inst.numText(e.Slot, e.HighValue)))
	}
	return parts
}

// paintExtremes draws the band's extremes as one line under its template.
func (inst *lensPainter) paintExtremes(parts []string, rh float32) {
	if len(parts) == 0 {
		return
	}
	cy := inst.y + rh/2
	sec := lensTok(styletokens.NeutralTextSecondary)
	inst.text(lensPad, cy, "extremes", lensFont, sec)
	x := lensPad + inst.labelW
	for _, part := range parts {
		if x+lensTextW(part, lensSmallFont) > inst.w-lensPad {
			inst.text(x, cy, "…", lensSmallFont, sec)
			break
		}
		inst.text(x, cy, part, lensSmallFont, lensTok(styletokens.NeutralTextPrimary))
		x += lensTextW(part, lensSmallFont) + 14
	}
	inst.y += rh
}

// paintException draws one exception line: its rows, then its departures.
func (inst *lensPainter) paintException(e exception, rh float32) {
	cy := inst.y + rh/2
	label := inst.rowLabel(e.rows[0])
	if len(e.rows) > 1 {
		label = fmt.Sprintf("%s +%d", label, len(e.rows)-1)
	}
	room := int((inst.labelW - 8) / lensAdvance)
	inst.text(lensPad, cy, lensFit(label, room), lensFont, lensTok(styletokens.NeutralTextPrimary))
	x := lensPad + inst.labelW
	for _, part := range e.parts {
		col := lensTok(styletokens.AccentDefault)
		switch part[0] {
		case '+':
			col = lensTok(styletokens.WarningDefault)
		case 0xe2: // "−", the minus sign
			col = lensTok(styletokens.ErrorDefault)
		}
		if x+lensTextW(part, lensSmallFont) > inst.w-lensPad {
			inst.text(x, cy, "…", lensSmallFont, col)
			break
		}
		inst.text(x, cy, part, lensSmallFont, col)
		x += lensTextW(part, lensSmallFont) + 10
	}
	inst.y += rh
}
