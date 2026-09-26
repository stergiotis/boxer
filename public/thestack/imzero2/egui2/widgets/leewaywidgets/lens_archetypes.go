package leewaywidgets

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwlens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The archetype form draws a band as one line — its template, each slot at
// how typical it is or what it typically holds — and under it only the rows
// that break the pattern. A batch of n rows in k clusters reads in about k
// lines plus its exceptions.

const (
	// lensTemplateAt is the band support a slot needs to be in a template
	// under ScopeRow, where the plan gives a band no frame.
	lensTemplateAt = 0.5
	// lensOutlierAt is the value surprise over which a value is an
	// exception worth a line: the outer 5% of its slot at either end.
	lensOutlierAt = 0.9
	// lensExceptionLines bounds the exception lines drawn per band.
	lensExceptionLines = 6
)

// bandValues is a band's values in slot s.
func (inst *lensPainter) bandValues(b *lwlens.Band, s int32) (nums []float64, texts []string) {
	for _, r := range b.Rows {
		if cell, ok := inst.a.Model.Rows[r].Cell(s); ok {
			if cell.HasNum {
				nums = append(nums, cell.Num)
			}
			texts = append(texts, cell.Text)
		}
	}
	slices.Sort(nums)
	return
}

// mode is the most frequent text and its share.
func mode(texts []string) (m string, share float64, distinct int) {
	counts := map[string]int{}
	best := 0
	for _, t := range texts {
		counts[t]++
		if k := counts[t]; k > best || (k == best && t < m) {
			m, best = t, k
		}
	}
	if len(texts) > 0 {
		share = float64(best) / float64(len(texts))
	}
	return m, share, len(counts)
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(q*float64(len(sorted)-1)+0.5)]
}

// templateText is what a band typically holds in slot s, in at most n
// runes: the longest of its readings that fits, so a narrow column drops the
// spread or the share before it cuts the value.
func (inst *lensPainter) templateText(b *lwlens.Band, s int32, n int) string {
	for _, t := range inst.templateTexts(b, s) {
		if utf8.RuneCountInString(t) <= n {
			return t
		}
	}
	ts := inst.templateTexts(b, s)
	return lensFit(ts[len(ts)-1], n)
}

// templateTexts are a band's readings of slot s, longest first.
func (inst *lensPainter) templateTexts(b *lwlens.Band, s int32) (out []string) {
	nums, texts := inst.bandValues(b, s)
	sl := inst.a.Model.Slots[s]
	if sl.Kind == lwlens.ValueKindNumeric && len(nums) > 0 {
		med := inst.numText(s, quantile(nums, 0.5))
		if inst.p.Detail == lwlens.DetailValues && len(nums) > 2 {
			out = append(out, fmt.Sprintf("%s (%s–%s)", med, inst.numText(s, quantile(nums, 0.1)), inst.numText(s, quantile(nums, 0.9))))
		}
		return append(out, med, lensShortNum(quantile(nums, 0.5)))
	}
	m, share, distinct := mode(texts)
	if share < 0.5 && len(texts) > 1 {
		// Timestamps are read as the span they cover.
		if _, ok := lensShortTime(texts[0]); ok {
			sorted := slices.Clone(texts)
			slices.Sort(sorted)
			first, _ := lensShortTime(sorted[0])
			last, _ := lensShortTime(sorted[len(sorted)-1])
			return []string{first + "…" + last, first[:5] + "…" + last[:5]}
		}
		return []string{fmt.Sprintf("%d distinct", distinct), fmt.Sprintf("(%d)", distinct)}
	}
	if ts, ok := lensShortTime(m); ok {
		m = ts
	}
	if inst.p.Detail == lwlens.DetailValues && share < 1 {
		out = append(out, fmt.Sprintf("%s %.0f%%", m, 100*share))
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
		c.PaintRectStroke(x+0.5, cy-h/2+0.5, x+h-0.5, cy+h/2-0.5, 1, inst.sectionTone[m.SectionOf(s)], 1).Send()
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
	nums, texts := inst.bandValues(b, s)
	st := &inst.a.Stats[s]
	track := lensMix(styletokens.NeutralBgSurface, styletokens.NeutralTextSecondary, 0.2)
	if inst.a.Model.Slots[s].Kind == lwlens.ValueKindNumeric && len(nums) > 0 && len(st.Sorted) > 1 {
		lo, hi := st.Sorted[0], st.Sorted[len(st.Sorted)-1]
		at := func(v float64) float32 {
			if hi == lo {
				return (x0 + x1) / 2
			}
			return x0 + (x1-x0)*float32((v-lo)/(hi-lo))
		}
		c.PaintRectFilled(x0, cy-1, x1, cy+1, 0, track).Send()
		a, z := at(quantile(nums, 0.1)), at(quantile(nums, 0.9))
		c.PaintRectFilled(a, cy-2.5, max(z, a+2), cy+2.5, 1, lensTok(styletokens.NeutralTextSecondary)).Send()
		m := at(quantile(nums, 0.5))
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

// exception is one row's departures from its band, as drawn text.
type exception struct {
	rows  []int32
	parts []string
	bad   bool
}

// exceptions lists a band's rows that break its pattern: missing and unusual
// slots, and above the shape detail extreme values. Rows with the same
// departures share a line.
func (inst *lensPainter) exceptions(pb *lwlens.PlanBand, b *lwlens.Band) (out []exception) {
	idx := map[string]int{}
	m := inst.a.Model
	for _, pr := range pb.Rows {
		var parts []string
		bad := len(pr.Missing) > 0
		for _, s := range pr.Missing {
			parts = append(parts, "−"+m.Slots[s].Label())
		}
		for _, s := range pr.Unexpected {
			parts = append(parts, "+"+m.Slots[s].Label())
		}
		if inst.p.Detail >= lwlens.DetailGist {
			row := &m.Rows[pr.Row]
			for _, cell := range row.Cells {
				s := cell.Slot
				if m.Slots[s].Kind != lwlens.ValueKindNumeric || !cell.HasNum || b.Support[s] < lensTemplateAt {
					continue
				}
				if lwlens.ValueSurprise(inst.a, s, &cell) < lensOutlierAt {
					continue
				}
				dir := "↑"
				if inst.a.Stats[s].Percentile(cell.Num) < 0.5 {
					dir = "↓"
				}
				parts = append(parts, fmt.Sprintf("%s %s%s", inst.memberName(s), inst.numText(s, cell.Num), dir))
			}
		}
		if len(parts) == 0 {
			continue
		}
		rows := append([]int32{pr.Row}, pr.Also...)
		key := strings.Join(parts, "\x00")
		if i, ok := idx[key]; ok {
			out[i].rows = append(out[i].rows, rows...)
			continue
		}
		idx[key] = len(out)
		out = append(out, exception{rows: rows, parts: parts, bad: bad})
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
		if inst.y+3*rh > inst.h-lensPad {
			inst.text(lensPad, inst.y+rh/2, fmt.Sprintf("… %d more clusters", len(inst.p.Bands)-bi), lensSmallFont, sec)
			inst.y += rh
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
				if inst.y+rh > inst.h-lensPad {
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
				if b.Support[s] >= lensTemplateAt && !slices.Contains(pb.Constants, s) &&
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
		exc := inst.exceptions(pb, b)
		for i, e := range exc {
			if i == lensExceptionLines || inst.y+rh > inst.h-lensPad {
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
