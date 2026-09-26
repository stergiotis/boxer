package leewaywidgets

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwlens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The focus form draws one row the way a detail pane would, in the context
// the analysis gives it: one line per slot, each with the row's value, where
// that value stands among its cluster's, and the same slot of its nearest
// peers beside it. Which slots get a line, and in what order, follows the
// stable intent; what a line shows, the values intent — which also decides
// what "nearest" means.

const (
	// lensPeers is how many peers are drawn beside the focus row.
	lensPeers = 4
	// lensFocusLine is a focus line's pitch.
	lensFocusLine float32 = 17
	// lensStripW is the width of a line's context strip.
	lensStripW float32 = 120
	// lensFocusValueMax is the widest value, in runes, a focus line shows.
	lensFocusValueMax = 34
)

// peers are the rows nearest r, nearest first.
func peers(a *lwlens.Analysis, r int32, values float64, k int) (out []int32) {
	type cand struct {
		row  int32
		dist float64
	}
	var cs []cand
	for q := range a.Model.Rows {
		if int32(q) != r {
			cs = append(cs, cand{int32(q), lwlens.RowDistance(a, r, int32(q), values)})
		}
	}
	slices.SortStableFunc(cs, func(x, y cand) int { return cmp.Compare(x.dist, y.dist) })
	for _, c := range cs[:min(k, len(cs))] {
		out = append(out, c.row)
	}
	return
}

// focusSlots are the slots the focus row gets a line for, in order.
func (inst *lensPainter) focusSlots(r int32) (slots []int32, constants []int32) {
	p := inst.p
	bi := inst.a.BandOf[r]
	var pb *lwlens.PlanBand
	var pr *lwlens.PlanRow
	for i := range p.Bands {
		if p.Bands[i].Band == int(bi) {
			pb = &p.Bands[i]
		}
	}
	if pb == nil {
		return
	}
	for i := range pb.Rows {
		if pb.Rows[i].Row == r || slices.Contains(pb.Rows[i].Also, r) {
			pr = &pb.Rows[i]
		}
	}
	constants = append(slices.Clone(pb.Constants), p.Constants...)
	switch p.Scope {
	case lwlens.ScopeGlobal:
		slots = slices.Clone(p.Frame)
	case lwlens.ScopeBand:
		slots = slices.Clone(pb.Frame)
	}
	if pr != nil {
		slots = append(slots, pr.Own...)
		// A row's own terms include what it lacks that its cluster has.
		for _, s := range pr.Missing {
			if !slices.Contains(slots, s) {
				slots = append(slots, s)
			}
		}
	}
	return
}

// paintFocus draws row r in focus.
func (inst *lensPainter) paintFocus(r int32) {
	a := inst.a
	m := a.Model
	if r < 0 || int(r) >= len(m.Rows) {
		r = 0
	}
	row := &m.Rows[r]
	b := &a.Bands[a.BandOf[r]]
	context := "among its cluster"
	if b.Cluster < 0 || !a.Clustered {
		// A row no cluster took is read against the whole batch.
		all := lwlens.Band{Cluster: -1, Support: a.Support}
		for q := range m.Rows {
			all.Rows = append(all.Rows, int32(q))
		}
		b = &all
		context = "among all rows"
	}
	pri := lensTok(styletokens.NeutralTextPrimary)
	sec := lensTok(styletokens.NeutralTextSecondary)

	// Heading: the row, then where the analysis puts it and why.
	inst.text(lensPad, inst.y+9, lensFit(row.Label, 60), lensFont+2, pri)
	inst.y += 22
	where := fmt.Sprintf("one of %d rows", len(m.Rows))
	if a.Clustered {
		if a.Bands[a.BandOf[r]].Cluster < 0 {
			where = fmt.Sprintf("unclustered: shares too little with any of %d clusters", len(a.Bands)-1)
		} else {
			where = fmt.Sprintf("cluster %s, %d rows", lensBandName(b, int(a.BandOf[r])), len(b.Rows))
			if rule := inst.ruleText(b); rule != "" {
				where += ": " + rule
			}
		}
	}
	inst.text(lensPad, inst.y+7, lensFit(where, int((inst.w-2*lensPad)/(0.62*lensSmallFont))), lensSmallFont, sec)
	inst.y += 18

	slots, constants := inst.focusSlots(r)
	var missing, unexpected []int32
	for _, pb := range inst.p.Bands {
		for _, pr := range pb.Rows {
			if pr.Row == r || slices.Contains(pr.Also, r) {
				missing, unexpected = pr.Missing, pr.Unexpected
			}
		}
	}
	if len(constants) > 0 && inst.p.Detail != lwlens.DetailShape && b.Cluster >= 0 {
		inst.paintConstants(constants, "as its cluster:")
	}
	near := peers(a, r, inst.p.Intent.Values, lensPeers)
	// A peer that shares no slot with the row is no peer: a row of a kind
	// of its own is shown alone.
	near = slices.DeleteFunc(near, func(q int32) bool { return lwlens.RowDistance(a, r, q, 0) >= 0.999 })

	// Columns: name, value, context strip, then one per peer.
	nameW := float32(0)
	for _, s := range slots {
		nameW = max(nameW, lensTextW(m.Slots[s].Section+" "+inst.memberName(s), lensSmallFont))
	}
	nameW = min(nameW, 30*0.62*lensSmallFont) + 14
	valueChars := lensFocusValueMax
	switch inst.p.Detail {
	case lwlens.DetailShape, lwlens.DetailFingerprint:
		valueChars = 0
	case lwlens.DetailGist:
		valueChars = 12
	}
	x0 := lensPad
	xVal := x0 + nameW
	xStrip := xVal + float32(valueChars)*lensAdvance + 12
	if valueChars == 0 {
		xStrip = xVal + 16
	}
	peerChars := 10
	peerW := float32(peerChars)*lensAdvance + 14
	xPeers := xStrip + lensStripW + 40
	nPeers := min(len(near), max(0, int((inst.w-lensPad-xPeers)/peerW)))

	// Column heads.
	y := inst.y + 6
	inst.text(xStrip, y, context, lensSmallFont, sec)
	for i, q := range near[:nPeers] {
		inst.text(xPeers+float32(i)*peerW, y, lensFit(inst.rowLabel(q), peerChars+1), lensSmallFont, sec)
	}
	inst.y += 16

	prevSec := ""
	for i, s := range slots {
		if inst.y+lensFocusLine > inst.h-lensPad {
			inst.text(x0, inst.y+lensFocusLine/2, fmt.Sprintf("… %d more slots", len(slots)-i), lensSmallFont, sec)
			inst.y += lensFocusLine
			break
		}
		sl := m.Slots[s]
		cy := inst.y + lensFocusLine/2
		tone := inst.sectionTone[m.SectionOf(s)]
		c.PaintRectFilled(x0, cy-6, x0+3, cy+6, 0, tone).Send()
		name := inst.memberName(s)
		if sl.Section != prevSec {
			name = sl.Section + " " + name
			prevSec = sl.Section
		}
		nameCol := pri
		cell, has := row.Cell(s)
		isMissing, isUnexpected := slices.Contains(missing, s), slices.Contains(unexpected, s)
		switch {
		case isMissing:
			nameCol = lensTok(styletokens.ErrorDefault)
		case isUnexpected:
			nameCol = lensTok(styletokens.WarningDefault)
		case !has:
			nameCol = sec
		}
		inst.text(x0+8, cy, lensFit(name, int((nameW-14)/(0.62*lensSmallFont))), lensSmallFont, nameCol)

		// Value.
		switch {
		case !has:
			if isMissing && valueChars == 0 {
				c.PaintRectStroke(xVal+0.5, cy-3.5, xVal+7.5, cy+3.5, 1, lensTok(styletokens.ErrorDefault), 1.5).Send()
			} else if isMissing {
				inst.text(xVal, cy, "missing", lensSmallFont, lensTok(styletokens.ErrorDefault))
			}
		case valueChars == 0:
			c.PaintRectFilled(xVal, cy-4, xVal+8, cy+4, 1, tone).Send()
		case len(cell.Series) >= 2:
			inst.paintSeriesCell(xVal, xVal+float32(valueChars)*lensAdvance, cy, cell)
		default:
			inst.text(xVal, cy, inst.valueText(s, cell, valueChars), lensFont, inst.valueTone(s, cell))
		}

		// Context: at the shape detail how typical the slot is in the
		// cluster; above it, where the value stands among the cluster's.
		if inst.p.Detail == lwlens.DetailShape || !has {
			sup := b.Support[s]
			c.PaintRectFilled(xStrip, cy-2, xStrip+lensStripW, cy+2, 1, lensMix(styletokens.NeutralBgSurface, styletokens.NeutralTextSecondary, 0.2)).Send()
			c.PaintRectFilled(xStrip, cy-2, xStrip+lensStripW*sup, cy+2, 1, sec).Send()
			inst.text(xStrip+lensStripW+4, cy, fmt.Sprintf("%.0f%%", 100*sup), lensSmallFont-1, sec)
		} else {
			inst.paintStanding(xStrip, xStrip+lensStripW, cy, s, cell, b)
		}

		// Peers.
		for k, q := range near[:nPeers] {
			px := xPeers + float32(k)*peerW
			pc, ok := m.Rows[q].Cell(s)
			switch {
			case !ok:
				c.PaintRectFilled(px, cy-0.5, px+6, cy+0.5, 0, lensTok(styletokens.NeutralBorderFaint)).Send()
			case inst.p.Detail == lwlens.DetailShape:
				c.PaintRectFilled(px, cy-4, px+8, cy+4, 1, inst.sectionTone[m.SectionOf(s)]).Send()
			case inst.p.Detail == lwlens.DetailFingerprint:
				inst.paintGlyph(px, px+peerW-24, cy, 4, s, pc)
			case len(pc.Series) >= 2:
				inst.paintSpark(px, px+peerW-14, cy, 5, pc.Series, sec)
			default:
				same := has && pc.Text == cell.Text
				col := pri
				if same {
					col = sec
				}
				inst.text(px, cy, inst.valueText(s, pc, peerChars), lensSmallFont, col)
			}
		}
		inst.y += lensFocusLine
	}
}

// paintStanding draws where a value stands among its cluster's: the
// cluster's values as ticks over the slot's range, the row's as a dot; for a
// label, the cluster's shares with the row's label outlined.
func (inst *lensPainter) paintStanding(x0, x1, cy float32, s int32, cell *lwlens.Cell, b *lwlens.Band) {
	st := &inst.a.Stats[s]
	if inst.a.Model.Slots[s].Kind == lwlens.ValueKindNumeric && cell.HasNum && len(st.Sorted) > 1 {
		lo, hi := st.Sorted[0], st.Sorted[len(st.Sorted)-1]
		at := func(v float64) float32 {
			if hi == lo {
				return (x0 + x1) / 2
			}
			return x0 + (x1-x0)*float32((v-lo)/(hi-lo))
		}
		c.PaintRectFilled(x0, cy-0.5, x1, cy+0.5, 0, lensMix(styletokens.NeutralBgSurface, styletokens.NeutralTextSecondary, 0.3)).Send()
		nums, _ := inst.bandValues(b, s)
		for _, v := range nums {
			x := at(v)
			c.PaintLine(x, cy-4, x, cy+4, lensMix(styletokens.NeutralBgSurface, styletokens.NeutralTextSecondary, 0.6), 1).Send()
		}
		col := lensTok(styletokens.NeutralTextExtreme)
		if lwlens.ValueSurprise(inst.a, s, cell) >= lensExtremeAt {
			col = lensTok(styletokens.AccentDefault)
		}
		c.PaintCircleFilled(at(cell.Num), cy, 3.5, col).Send()
		return
	}
	if inst.a.Model.Slots[s].Kind == lwlens.ValueKindText {
		return // free text has no standing to show
	}
	inst.paintDistribution(x0, x1, cy, s, b)
	_, texts := inst.bandValues(b, s)
	if len(texts) == 0 {
		return
	}
	// Outline the row's own label within the shares.
	counts := map[string]int{}
	for _, t := range texts {
		counts[t]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(p, q string) int { return st.CategoryRank(p) - st.CategoryRank(q) })
	x := x0
	for _, k := range keys {
		w := (x1 - x0) * float32(counts[k]) / float32(len(texts))
		if k == cell.Text {
			c.PaintRectStroke(x-1, cy-6, x+max(w-1, 1)+1, cy+6, 1, lensTok(styletokens.NeutralTextExtreme), 1.5).Send()
		}
		x += w
	}
}
