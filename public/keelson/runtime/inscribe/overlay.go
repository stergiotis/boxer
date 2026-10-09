package inscribe

import (
	"hash/fnv"
	"math"
	"strconv"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Overlay draws a scene once per frame, on the render goroutine, after every
// window and the shell chrome (ADR-0297 §SD1). It holds no annotations of
// its own: those are the scene's.
type Overlay struct {
	scene *Scene
	// widths hold egui's measure of each text a note shows, written by the
	// frame's Sync (one frame late); a text not measured yet is estimated.
	widths map[measureKey]*float64
}

type measureKey struct {
	text string
	size float32
}

// NewOverlay returns an overlay drawing scene.
func NewOverlay(scene *Scene) *Overlay {
	return &Overlay{scene: scene, widths: map[measureKey]*float64{}}
}

// Scene is the scene the overlay draws.
func (inst *Overlay) Scene() *Scene { return inst.scene }

// Frame resolves every annotation against this frame's geometry, retires
// those whose window is gone, and draws the rest inside bounds — unless
// hidden, while a host modal is open (ADR-0297 §SD8). Render goroutine only.
func (inst *Overlay) Frame(r ResolverI, bounds Rect, hidden bool) {
	if inst.scene.Len() == 0 {
		if len(inst.widths) > 0 {
			clear(inst.widths)
		}
		return
	}
	items := inst.scene.Snapshot()
	resolved := make([]Resolved, 0, len(items))
	for _, it := range items {
		rv := Resolved{Item: it, Rects: make([]Rect, len(it.Targets)), Vis: make([]VisibilityE, len(it.Targets))}
		drawable := true
		for k, a := range it.Targets {
			rv.Rects[k], rv.Vis[k] = r.Resolve(a)
			switch rv.Vis[k] {
			case VisibilityGone:
				// The window closed: the authority the mark was drawn
				// under is gone with it.
				inst.scene.ClearWindow(it.Task, a.Window)
				drawable = false
			case VisibilityPending:
				drawable = false
			}
		}
		if drawable {
			resolved = append(resolved, rv)
		}
	}
	if hidden || len(resolved) == 0 {
		return
	}
	shapes := Layout(resolved, bounds, inst.measure)
	inst.draw(shapes)
}

// measure asks egui for a text's width, which arrives at the next Sync, and
// answers what it has: the measured width, or an estimate before it.
func (inst *Overlay) measure(text string, size float32) (w, h float32) {
	ew, eh := EstimateMeasure(text, size)
	k := measureKey{text: text, size: size}
	out, ok := inst.widths[k]
	if !ok {
		if len(inst.widths) > 4*MaxScene {
			clear(inst.widths)
		}
		out = new(float64)
		inst.widths[k] = out
	}
	c.MeasureTextBind(measureId(k), text, size, false, out)
	if *out > 0 {
		ew = float32(*out)
	}
	return ew, eh
}

// measureId is a stable id for a measured text, in a space of its own.
func measureId(k measureKey) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("inscribe\x00" + strconv.FormatFloat(float64(k.size), 'f', -1, 32) + "\x00" + k.text))
	return h.Sum64()
}

// The overlay's stroke weights: part of its own visual language, outside
// the design system's tokens like its colours (ADR-0297 §SD6).
const (
	noteBorder  float32 = 1.5
	leaderWidth float32 = 1.5
)

func (inst *Overlay) draw(shapes []Shape) {
	const (
		stroke    float32 = 2.5
		haloExtra float32 = 3
		dash      float32 = 7
		gap       float32 = 5
	)
	line := func(x0, y0, x1, y1 float32, dashed bool, hue int, w float32) {
		col := hueColor(hue)
		if dashed {
			c.PaintDashedLine(x0, y0, x1, y1, dash, gap, halo, w+haloExtra).Send()
			c.PaintDashedLine(x0, y0, x1, y1, dash, gap, col, w).Send()
			return
		}
		c.PaintLine(x0, y0, x1, y1, halo, w+haloExtra).Send()
		c.PaintLine(x0, y0, x1, y1, col, w).Send()
	}
	for _, s := range shapes {
		switch s.Kind {
		case ShapeDim:
			c.PaintRectFilled(s.Rect.X, s.Rect.Y, s.Rect.MaxX(), s.Rect.MaxY(), 0, dim).Send()
		case ShapeOutline:
			r := s.Rect
			if s.Dashed {
				line(r.X, r.Y, r.MaxX(), r.Y, true, s.Hue, stroke)
				line(r.MaxX(), r.Y, r.MaxX(), r.MaxY(), true, s.Hue, stroke)
				line(r.MaxX(), r.MaxY(), r.X, r.MaxY(), true, s.Hue, stroke)
				line(r.X, r.MaxY(), r.X, r.Y, true, s.Hue, stroke)
				continue
			}
			c.PaintRectStroke(r.X, r.Y, r.MaxX(), r.MaxY(), 4, halo, stroke+haloExtra).Send()
			c.PaintRectStroke(r.X, r.Y, r.MaxX(), r.MaxY(), 4, hueColor(s.Hue), stroke).Send()
		case ShapeArrow:
			inst.arrow(s, line, stroke, haloExtra)
		case ShapeLeader:
			line(s.X0, s.Y0, s.X1, s.Y1, false, s.Hue, leaderWidth)
		case ShapeBadge:
			c.PaintCircleFilled(s.X0, s.Y0, badgeR+1.5, halo).Send()
			c.PaintCircleFilled(s.X0, s.Y0, badgeR, hueColor(s.Hue)).Send()
			c.PaintText(s.X0, s.Y0, 1, 1, s.Text, badgeFont, plate).Send()
		case ShapeNote:
			r := s.Rect
			col := hueColor(s.Hue)
			c.PaintRectFilled(r.X, r.Y, r.MaxX(), r.MaxY(), 4, plate).Send()
			c.PaintRectStroke(r.X, r.Y, r.MaxX(), r.MaxY(), 4, col, noteBorder).Send()
			c.PaintText(r.X+notePad, r.Y+notePad, 0, 0, s.Tag, tagFont, col).Send()
			if s.Text != "" {
				_, th := EstimateMeasure(s.Tag, tagFont)
				c.PaintText(r.X+notePad, r.Y+notePad+th+tagRowGap, 0, 0, s.Text, noteFont, plateText).Send()
			}
		}
	}
	c.PaintAbsoluteOverlay()
}

// arrow draws a shaft and a head; a dashed arrow's shaft is dashed and its
// head solid, so the direction still reads.
func (inst *Overlay) arrow(s Shape, line func(x0, y0, x1, y1 float32, dashed bool, hue int, w float32), stroke, haloExtra float32) {
	dx, dy := s.X1-s.X0, s.Y1-s.Y0
	n := float32(math.Hypot(float64(dx), float64(dy)))
	if n < 1 {
		return
	}
	ux, uy := dx/n, dy/n
	const head float32 = 14
	bx, by := s.X1-ux*head, s.Y1-uy*head
	line(s.X0, s.Y0, bx, by, s.Dashed, s.Hue, stroke+0.5)
	px, py := -uy*head*0.5, ux*head*0.5
	xs := []float32{s.X1, bx + px, bx - px}
	ys := []float32{s.Y1, by + py, by - py}
	c.PaintPolyline(append(xs, xs[0]), append(ys, ys[0]), halo, haloExtra).Send()
	c.PaintPolygonFilled(xs, ys, hueColor(s.Hue)).Send()
}
