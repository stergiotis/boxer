package riskviews

import (
	"math"
	"strconv"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

var dotQs = quantileDots(50)

func (inst *App) renderDots(w float32, p palette) {
	ids := inst.ids
	legendRow(ids, "dots-legend", w, p, []legItem{
		{kind: swDot, col: p.s[0], label: "Year within the €1.5m reserve"},
		{kind: swDot, col: p.s[1], label: "Year beyond it"},
	})
	c.AddSpace(4)
	for range c.IdScope(ids.PrepareStr("dots")) {
		drawDots(ids, w, p)
	}
	c.AddSpace(8)
	for range c.IdScope(ids.PrepareStr("icons")) {
		drawIcons(ids, w, p)
	}
}

func drawDots(ids *c.WidgetIdStack, W float32, p palette) {
	const q = 50
	qs := dotQs
	zero, beyond := 0, 0
	for _, v := range qs {
		if v < 1e3 {
			zero++
		}
		if v > reserve {
			beyond++
		}
	}
	ml, mr, mt, mb := float32(14), float32(14), float32(40), float32(44)
	pw, zw := W-ml-mr, float32(56)
	lo, hi := 10e3, 50e6
	xS := logScale(lo, hi, ml+zw+16, ml+pw)
	nb := max(14, int(math.Floor(float64((pw-zw-16)/13))))
	edges := logSpace(lo, hi, nb)
	r := float32(5.2)
	step := 2*r + 1.5
	bin := func(v float64) int {
		k := int(math.Floor((math.Log(v) - math.Log(lo)) / (math.Log(hi) - math.Log(lo)) * float64(nb)))
		return min(nb-1, max(0, k))
	}
	counts := make([]int, nb)
	maxStack := zero
	for _, v := range qs {
		if v < 1e3 {
			continue
		}
		k := bin(v)
		counts[k]++
		maxStack = max(maxStack, counts[k])
	}
	H := mt + float32(maxStack)*step + mb + 6
	cv := newCanvas(ids, W, H, p)
	base := H - mb
	line(ml, base, ml+pw, base, p.axis, 1)
	line(xS(reserve), mt-14, xS(reserve), base, p.ink2, 1.5)
	text(xS(reserve)+6, mt-6, anchorStart, anchorBottom, "Reserve "+eur(reserve), fsLbl, p.ink2)
	for z := 0; z < zero; z++ {
		cx, cy := ml+zw/2, base-r-1-float32(z)*step
		c.PaintCircleFilled(cx, cy, r, p.s[0]).Send()
		cv.tipCircle(cx, cy, r, "No loss that year", strconv.Itoa(zero)+" of 50 years")
	}
	mono(ml+zw/2, base+8, anchorMiddle, anchorTop, "€0", fsAx, p.ink2)
	filled := make([]int, nb)
	for _, v := range qs {
		if v < 1e3 {
			continue
		}
		k := bin(v)
		cx := (xS(edges[k]) + xS(edges[k+1])) / 2
		cy := base - r - 1 - float32(filled[k])*step
		filled[k]++
		col := p.s[0]
		if v > reserve {
			col = p.s[1]
		}
		c.PaintCircleFilled(cx, cy, r, col).Send()
		cv.tipCircle(cx, cy, r, "One year in 50", "Total loss about "+eur(v))
	}
	for _, v := range []float64{100e3, 1e6, 10e6} {
		mono(xS(v), base+8, anchorMiddle, anchorTop, eur(v), fsAx, p.ink2)
	}
	text(ml+pw/2+30, H-6, anchorMiddle, anchorBottom, "Total annual loss, residual (log scale)", fsLbl, p.ink2)
	text(ml, 6, anchorStart, anchorTop, strconv.Itoa(beyond)+" of 50 years exceed the reserve", fsBody, p.ink)
	cv.finish()
}

func drawIcons(ids *c.WidgetIdStack, W float32, p palette) {
	r1 := risks[0]
	pres, pinh := gmean(r1.P), gmean(r1.IP)
	sets := []struct {
		name string
		n    int
	}{{"Without controls", int(jsRound(pinh * 100))}, {"With controls", int(jsRound(pres * 100))}}
	cell := float32(min(15, math.Floor(float64((W-40-54)/20))))
	gap := float32(3)
	gw := 10*cell + 9*gap
	H := 34 + gw + 30
	cv := newCanvas(ids, W, H, p)
	text(0, 2, anchorStart, anchorTop, "R1 Ransomware: years with an attack, out of 100", fsBody, p.ink)
	for si, s := range sets {
		ox, oy := float32(si)*(gw+40), float32(30)
		// one batched opcode per array: PaintRectsFilled takes per-rect colours
		minX, minY, maxX, maxY := make([]float32, 100), make([]float32, 100), make([]float32, 100), make([]float32, 100)
		cols := color.NewColors(100)
		on, off := p.s[1].Literal(), p.grid.Literal()
		for k := range 100 {
			row, col := float32(k/10), float32(k%10)
			minX[k], minY[k] = ox+col*(cell+gap), oy+row*(cell+gap)
			maxX[k], maxY[k] = minX[k]+cell, minY[k]+cell
			if k < s.n {
				cols.SetHex(k, on)
			} else {
				cols.SetHex(k, off)
			}
		}
		c.PaintRectsFilled(minX, minY, maxX, maxY, cols).Send()
		text(ox, oy+gw+8, anchorStart, anchorTop, s.name+": about "+strconv.Itoa(s.n)+" in 100", fsLbl, p.ink2)
		cv.tipRect(ox, oy, ox+gw, oy+gw, "R1 Ransomware · "+s.name, strconv.Itoa(s.n)+" of 100 years with an attack")
	}
	cv.finish()
}
