package inscribe

import (
	"hash/fnv"
	"math"
	"strconv"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Overlay draws a scene once per frame, on the render goroutine, after every
// window and the shell chrome (ADR-0297 §SD1). It holds no marks of
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

// Frame resolves every mark against this frame's geometry, retires
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
		rv := Resolved{Item: it, Rects: make([]Rect, len(it.Targets)), Vis: make([]VisibilityE, len(it.Targets)),
			Windows: make([]Rect, len(it.Targets))}
		drawable := true
		for k, a := range it.Targets {
			rv.Rects[k], rv.Vis[k] = r.Resolve(a)
			if a.Window != 0 {
				rv.Windows[k], _ = r.Window(a.Window)
			}
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
	shapes := Layout(resolved, bounds, r.Windows(), inst.measure)
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
// the design system's tokens like its colours (ADR-0297 §SD6). A sketched
// stroke is drawn twice, so each pass is thin.
const (
	strokeWidth   float32 = 1.8
	leaderWidth   float32 = 1.2
	noteBorder    float32 = 1.1
	underlayExtra float32 = 1.5
	dashLen       float32 = 7
	gapLen        float32 = 5
	headSize      float32 = 13
)

func (inst *Overlay) draw(shapes []Shape) {
	for _, s := range shapes {
		r := newSketchRng(s.Seed)
		switch s.Kind {
		case ShapeDim:
			c.PaintRectFilled(s.Rect.X, s.Rect.Y, s.Rect.MaxX(), s.Rect.MaxY(), 0, dim).Send()
		case ShapeOutline:
			strokes(sketchRect(s.Rect, r), s.Hue, strokeWidth, s.Dashed)
		case ShapeArrow:
			shaft := sketchBow(s.X0, s.Y0, s.X1, s.Y1, r)
			if len(shaft) == 0 {
				continue
			}
			strokes(shaft, s.Hue, strokeWidth, s.Dashed)
			// The head follows the shaft's last stretch, solid even when the
			// shaft is dashed, so the direction still reads.
			p := shaft[0]
			a, b := p[len(p)-2], p[len(p)-1]
			dx, dy := b.X-a.X, b.Y-a.Y
			n := float32(math.Hypot(float64(dx), float64(dy)))
			if n > 0 {
				strokes(sketchHead(s.X1, s.Y1, dx/n, dy/n, headSize, r), s.Hue, strokeWidth, false)
			}
		case ShapeLeader:
			strokes(sketchLine(s.X0, s.Y0, s.X1, s.Y1, r), s.Hue, leaderWidth, false)
		case ShapeBadge:
			c.PaintCircleFilled(s.X0, s.Y0, badgeR+underlayExtra, underlay).Send()
			c.PaintCircleFilled(s.X0, s.Y0, badgeR, hueColor(s.Hue)).Send()
			c.PaintText(s.X0, s.Y0, 1, 1, s.Text, badgeFont, plate).Send()
		case ShapeTab:
			rc := s.Rect
			c.PaintRectFilled(rc.X, rc.Y, rc.MaxX(), rc.MaxY(), 2, plate).Send()
			c.PaintText(rc.X+tabPad, rc.Y+tabPad, 0, 0, s.Tag, tagFont, hueColor(s.Hue)).Send()
		case ShapeNote:
			rc := s.Rect
			col := hueColor(s.Hue)
			c.PaintRectFilled(rc.X, rc.Y, rc.MaxX(), rc.MaxY(), 3, plate).Send()
			strokes(sketchRect(rc, r), s.Hue, noteBorder, false)
			c.PaintText(rc.X+notePad, rc.Y+notePad, 0, 0, s.Tag, tagFont, col).Send()
			if s.Text != "" {
				_, th := EstimateMeasure(s.Tag, tagFont)
				c.PaintText(rc.X+notePad, rc.Y+notePad+th+tagRowGap, 0, 0, s.Text, noteFont, plateText).Send()
			}
		}
	}
	c.PaintAbsoluteOverlay()
}

// strokes draws sketched polylines in a hue over a faint underlay, solid or
// dashed.
func strokes(ps []polyline, hue int, w float32, dashed bool) {
	col := hueColor(hue)
	for _, p := range ps {
		if len(p) < 2 {
			continue
		}
		if dashed {
			x0s, y0s, x1s, y1s := dashes(p, dashLen, gapLen)
			if len(x0s) == 0 {
				continue
			}
			c.PaintSegments(x0s, y0s, x1s, y1s, fill(len(x0s), underlay), w+underlayExtra).Send()
			c.PaintSegments(x0s, y0s, x1s, y1s, fill(len(x0s), col), w).Send()
			continue
		}
		xs := make([]float32, len(p))
		ys := make([]float32, len(p))
		for i, q := range p {
			xs[i], ys[i] = q.X, q.Y
		}
		c.PaintPolyline(xs, ys, underlay, w+underlayExtra).Send()
		c.PaintPolyline(xs, ys, col, w).Send()
	}
}

// fill is n copies of one colour, for a batch of segments.
func fill(n int, col color.Color) color.Colors {
	cs := make([]color.Color, n)
	for i := range cs {
		cs[i] = col
	}
	return color.ColorsFromSlice(cs)
}
