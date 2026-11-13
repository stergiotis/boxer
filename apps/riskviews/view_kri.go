package riskviews

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
)

func (inst *App) renderKRI(w float32, p palette) {
	ids := inst.ids
	legendRow(ids, "kri-legend", w, p, []legItem{
		{kind: swBox, col: p.band[0], label: "Within appetite"},
		{kind: swBox, col: p.band[1], label: "Within tolerance"},
		{kind: swBox, col: p.band[2], label: "Beyond tolerance"},
		{kind: swLine, col: p.s[0], label: "Monthly value"},
		{kind: swLine, col: tok(styletokens.NeutralBorderDefault), label: "Appetite limit in force"},
		{kind: swDot, col: statusFill(StatusWarn), label: "Month within tolerance only"},
		{kind: swDiamond, col: statusFill(StatusCrit), label: "Month beyond tolerance"},
	})
	c.AddSpace(10)
	const gap = 18
	// egui adds item spacing between the row's four children on top of the
	// explicit gaps; leave room for it or the row widens the card.
	avail := w - 2*gap - 4*styletokens.GapItems(inst.density)
	tot := 1.25 + 1.4 + 1.0
	w0 := avail * float32(1.25/tot)
	w1 := avail * float32(1.4/tot)
	w2 := avail * float32(1.0/tot)
	for ki, k := range kris {
		for range c.IdScope(ids.PrepareSeq(uint64(ki))) {
			st := kriStatus(k, 11)
			for range c.HorizontalTop().KeepIter() {
				for range c.Vertical().KeepIter() {
					c.UiSetMinWidth(w0)
					c.UiSetMaxWidth(w0)
					c.LabelAtoms(c.Atoms().BeginRichTextColored(p.ink, transparent, k.Name).Strong().End().Keep()).Wrap().Send()
					c.LabelAtoms(c.Atoms().BeginRichTextColored(p.muted, transparent, k.Note).Monospace().Size(styletokens.CaptionPt).End().Keep()).Send()
					badge.New(ids.PrepareStr("chip"), kriLabel(st)+" · now "+fmtK(k, k.Hist[11])).
						Tone(statusTone(st)).Variant(badge.VariantSoft).Size(badge.SizeSm).
						Icon(statusGlyph(st)).Send()
				}
				c.AddSpace(gap)
				for range c.IdScope(ids.PrepareStr("bullet")) {
					drawBullet(ids, k, w1, p)
				}
				c.AddSpace(gap)
				for range c.IdScope(ids.PrepareStr("spark")) {
					drawSpark(ids, k, w2, p)
				}
			}
			c.AddSpace(14)
		}
	}
}

func drawBullet(ids *c.WidgetIdStack, k Kri, W float32, p palette) {
	const H = 46
	cv := newCanvas(ids, W, H, p)
	lo, hi := k.Min, k.Max
	xS := linScale(lo, hi, 0, W-4)
	a, t := k.Appetite[11], k.Tolerance[11]
	type band struct {
		from, to float64
		idx      int
	}
	var bands []band
	if k.LowerBetter {
		bands = []band{{lo, a, 0}, {a, t, 1}, {t, hi, 2}}
	} else {
		bands = []band{{lo, t, 2}, {t, a, 1}, {a, hi, 0}}
	}
	for _, b := range bands {
		if b.to > b.from {
			c.PaintRectFilled(xS(b.from), 6, xS(b.to), 28, 0, p.band[b.idx]).Send()
		}
	}
	v := k.Hist[11]
	c.PaintRectFilled(xS(lo), 14, xS(lo)+max(2, xS(v)-xS(lo)), 20, 2, p.ink).Send()
	cmp := "≥ "
	if k.LowerBetter {
		cmp = "≤ "
	}
	c.PaintHitRect(cv.next(), xS(lo), 6, xS(hi), 28).Label(k.Name).TooltipUi(func() {
		for range c.Grid(ids.PrepareStr("tip")).NumColumns(2).KeepIter() {
			for _, row := range [][2]string{{"Now", fmtK(k, v)}, {"Appetite", cmp + fmtK(k, a)}, {"Tolerance", cmp + fmtK(k, t)}} {
				c.Label(row[0]).Send()
				c.LabelAtoms(c.Atoms().BeginRichText(row[1]).Monospace().End().Keep()).Send()
				c.EndRow()
			}
		}
	}).Send()
	line(xS(a), 3, xS(a), 31, p.ink2, 2)
	seen := map[float64]bool{}
	for _, x := range []float64{lo, a, t, hi} {
		if seen[x] {
			continue
		}
		seen[x] = true
		ah := anchorMiddle
		switch x {
		case lo:
			ah = anchorStart
		case hi:
			ah = anchorEnd
		}
		mono(min(W-4, max(0, xS(x))), H-1, ah, anchorBottom, fmtK(k, x), fsAx, p.ink2)
	}
	cv.finish()
}

func drawSpark(ids *c.WidgetIdStack, k Kri, W float32, p palette) {
	const H = 52
	ml, mr, mt, mb := float32(2), float32(40), float32(6), float32(16)
	cv := newCanvas(ids, W, H, p)
	lo, hi := k.Min, k.Max
	pw, ph := W-ml-mr, float32(H)-mt-mb
	xS := linScale(0, 11, ml, ml+pw)
	yS := linScale(lo, hi, mt+ph, mt)
	// stepped, versioned appetite threshold
	var sx, sy []float32
	for i, a := range k.Appetite {
		x0 := xS(0)
		if i > 0 {
			x0 = (xS(float64(i-1)) + xS(float64(i))) / 2
		}
		x1 := xS(11)
		if i < 11 {
			x1 = (xS(float64(i)) + xS(float64(i+1))) / 2
		}
		sx = append(sx, x0, x1)
		sy = append(sy, yS(a), yS(a))
	}
	// IDS has no tertiary ("muted") ink; NeutralTextDisabled is too dark for
	// a line on the plot surface, so the step takes the default border token.
	c.PaintPolyline(sx, sy, tok(styletokens.NeutralBorderDefault), 1.5).Send()
	// area under the history: one convex quad per segment (the painter's
	// default fill is convex-only; a whole-run polygon would need .Concave())
	areaCol := tokA(slotColor(slotInherent), 0x1a)
	var lx, ly []float32
	for i, v := range k.Hist {
		lx = append(lx, xS(float64(i)))
		ly = append(ly, yS(v))
	}
	base := yS(lo)
	for i := 1; i < len(lx); i++ {
		c.PaintPolygonFilled([]float32{lx[i-1], lx[i], lx[i], lx[i-1]}, []float32{ly[i-1], ly[i], base, base}, areaCol).Send()
	}
	c.PaintPolyline(lx, ly, p.s[0], 2).Send()
	for i, v := range k.Hist {
		st := kriStatus(k, i)
		last := i == 11
		x, y := xS(float64(i)), yS(v)
		if st == StatusGood && !last {
			cv.tipCircle(x, y, 7, months[i], fmtK(k, v)+" · appetite "+fmtK(k, k.Appetite[i])+" in force")
			continue
		}
		if st == StatusCrit {
			const d = 6.4 // half-diagonal of the prototype's 9 px square rotated 45°
			c.PaintPolygonFilled([]float32{x, x + d, x, x - d}, []float32{y - d, y, y + d, y}, statusFill(st)).
				Stroke(p.bg, 2).Send()
		} else {
			r := float32(4)
			if last {
				r = 4.5
			}
			col := p.s[0]
			if st == StatusWarn {
				col = statusFill(st)
			}
			c.PaintCircleFilled(x, y, r, col).Send()
			c.PaintCircleStroke(x, y, r, p.bg, 2).Send()
		}
		cv.tipCircle(x, y, 5, months[i]+" · "+kriLabel(st), fmtK(k, v)+" · appetite "+fmtK(k, k.Appetite[i])+", tolerance "+fmtK(k, k.Tolerance[i])+" in force")
	}
	mono(xS(11)+7, yS(k.Hist[11]), anchorStart, anchorCenter, fmtK(k, k.Hist[11]), fsAx+0.5, p.ink)
	mono(xS(0), H-1, anchorStart, anchorBottom, "Nov", fsAx, p.ink2)
	mono(xS(11), H-1, anchorEnd, anchorBottom, "Oct", fsAx, p.ink2)
	cv.finish()
}
