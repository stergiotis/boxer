package implot

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// imageFrame carries one Image declaration to the render pass.
type imageFrame struct {
	pix            []uint32
	rows, cols     int
	x0, y0, x1, y1 float64
	version        uint64
}

// Image declares a rows×cols RGBA (0xRRGGBBAA, row 0 at the TOP edge y1 —
// Heatmap's orientation contract) texture drawn across the plot-space rect
// (x0,y0)-(x1,y1). The caller owns the pixel buffer and bumps version when
// its content changes; an unchanged version ships no pixels and reuses the
// GPU-resident texture (the mapRaster ship-once protocol — the substrate
// has no upstream-style GPU texture handles, see doc.go).
func (p *Plot) Image(label string, pix []uint32, rows int, cols int, x0 float64, y0 float64, x1 float64, y1 float64, version uint64) *Plot {
	p.setupLocked = true
	p.takeNextStyle() // pixel content owns its colors; an override must not leak
	if !p.st.hidden[label] {
		p.fitX(x0)
		p.fitX(x1)
		p.fitY(y0)
		p.fitY(y1)
	}
	p.series = append(p.series, seriesFrame{kind: kindImage, label: label, slot: p.assignSlot(label),
		img: &imageFrame{pix: pix, rows: rows, cols: cols, x0: x0, y0: y0, x1: x1, y1: y1, version: version}})
	return p
}

// emitImage renders one image declaration through the ship-once protocol:
// pixels go over the wire only on a version change or when the host
// reports the texture starved (hidden-tab discard, LRU eviction).
func (p *Plot) emitImage(s *seriesFrame, tr transform) {
	im := s.img
	n := im.rows * im.cols
	if n == 0 || len(im.pix) < n {
		return
	}
	st := p.st
	if st.imgSent == nil {
		st.imgSent = make(map[string]uint64, 2)
	}
	texId := p.ids.PrepareStr("img-" + s.label).Derive()
	sm := c.CurrentApplicationState.StateManager
	send := im.pix[:n]
	if sent, ok := st.imgSent[s.label]; ok && sent == im.version && !sm.TextureStarved(texId) {
		send = emptyPixels
	}
	c.PaintImage(texId, tr.pxX(im.x0), tr.pxY(im.y1), tr.pxX(im.x1), tr.pxY(im.y0),
		uint32(im.cols), uint32(im.rows), im.version, send).
		Send()
	st.imgSent[s.label] = im.version
}
