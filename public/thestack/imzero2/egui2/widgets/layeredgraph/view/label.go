package view

import (
	"strings"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// lineSpacing is the pitch, in ems, at which Graphviz stacks the lines of a
// multi-line label when it sizes the box around it.
const lineSpacing = 1.2

// lineCentres returns the vertical centre of each of n lines stacked at
// lineSpacing×fontPx around cy.
func lineCentres(cy float32, fontPx float32, n int) (ys []float32) {
	if n <= 0 {
		return
	}
	pitch := lineSpacing * fontPx
	top := cy - pitch*float32(n-1)/2
	ys = make([]float32, n)
	for i := range ys {
		ys[i] = top + pitch*float32(i)
	}
	return
}

// paintLabel paints text centred vertically on (x, y), anchored horizontally
// by anchorH (0 left, 1 centre, 2 right). A label of several lines is painted
// line by line at the pitch the layout sized its box for, so the text stays
// inside the box at any scale; the renderer's own line height is wider, and
// whole-pixel rounding widens it further as the text gets small.
func paintLabel(x, y float32, anchorH uint8, text string, fontPx float32, col color.Color) {
	if !strings.Contains(text, "\n") {
		c.PaintText(x, y, anchorH, 1, text, fontPx, col).Send()
		return
	}
	lines := strings.Split(text, "\n")
	for i, ly := range lineCentres(y, fontPx, len(lines)) {
		c.PaintText(x, ly, anchorH, 1, strings.TrimSuffix(lines[i], "\r"), fontPx, col).Send()
	}
}
