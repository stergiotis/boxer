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
	leaderHead    float32 = 8
	dotRadius     float32 = 0.8
)

func (inst *Overlay) draw(shapes []Shape) {
	for _, s := range shapes {
		r := newSketchRng(s.Seed)
		switch s.Kind {
		case ShapeDim:
			c.PaintRectFilled(s.Rect.X, s.Rect.Y, s.Rect.MaxX(), s.Rect.MaxY(), 0, dim).Send()
		case ShapeOutline:
			drawOutline(s, r)
		case ShapeArrow:
			drawArrow(s.X0, s.Y0, s.X1, s.Y1, s.Hue, strokeWidth, headSize, s.Dashed, r)
		case ShapeLeader:
			// A short curved arrow from the note to what it is about.
			drawArrow(s.X0, s.Y0, s.X1, s.Y1, s.Hue, leaderWidth, leaderHead, false, r)
		case ShapeBadge:
			c.PaintCircleFilled(s.X0, s.Y0, badgeR+underlayExtra, underlay).Send()
			c.PaintCircleFilled(s.X0, s.Y0, badgeR, hueColor(s.Hue)).Send()
			c.PaintText(s.X0, s.Y0, 1, 1, s.Text, badgeFont, plate).Send()
		case ShapeTab:
			rc := s.Rect
			c.PaintRectFilled(rc.X+2, rc.Y+2, rc.MaxX()+2, rc.MaxY()+2, 4, shadow).Send()
			c.PaintRectFilled(rc.X, rc.Y, rc.MaxX(), rc.MaxY(), 4, plate).Send()
			drawTag(rc.X+tabPad, rc.Y+tabPad, s.Tag, s.Hue)
		case ShapeNote:
			rc := s.Rect
			// A soft shadow, the plate, and the task's hue down its left
			// edge.
			c.PaintRectFilled(rc.X+3, rc.Y+4, rc.MaxX()+3, rc.MaxY()+4, 6, shadow).Send()
			c.PaintRectFilled(rc.X, rc.Y, rc.MaxX(), rc.MaxY(), 6, plate).Send()
			if xs, ys := dotGrid(Rect{X: rc.X + noteAccent, Y: rc.Y, W: rc.W - noteAccent, H: rc.H}); len(xs) > 0 {
				c.PaintMarkers(xs, ys, 0, dotRadius, noteDots, 0).Send()
			}
			c.PaintRectFilled(rc.X, rc.Y+3, rc.X+noteAccent, rc.MaxY()-3, 2, hueColor(s.Hue)).Send()
			x := rc.X + noteAccent + notePad
			c.PaintText(x, rc.Y+notePad, 0, 0, s.Text, noteFont, plateText).Send()
			_, th := EstimateMeasure(s.Text, noteFont)
			drawTag(x, rc.Y+notePad+th+tagRowGap, s.Tag, s.Hue)
		}
	}
	c.PaintAbsoluteOverlay()
}

// drawTag is a mark's attribution: a dot in the task's hue and the tag,
// dimmed, so it says who drew the mark without competing with it.
func drawTag(x, y float32, tag string, hue int) {
	_, h := EstimateMeasure(tag, tagFont)
	c.PaintCircleFilled(x+3, y+h/2, 3, hueColor(hue)).Send()
	c.PaintText(x+tagDot, y, 0, 0, tag, tagFont, tagText).Send()
}

// drawOutline marks a target in its style: a pen circle, a highlighter
// swipe, or corner brackets. A target behind another window is a dashed
// sketch, so it reads as covered.
func drawOutline(s Shape, r *sketchRng) {
	switch s.Style {
	case OutlineSwipe:
		if s.Dashed {
			strokes(sketchRect(s.Rect, r), s.Hue, leaderWidth, true)
			return
		}
		xs, ys := swipe(s.Rect, r)
		c.PaintPolygonFilled(xs, ys, swipeColor(s.Hue)).Send()
	case OutlineBrackets:
		strokes(sketchBrackets(s.Rect, r), s.Hue, strokeWidth, s.Dashed)
	default:
		strokes(sketchEllipse(s.Rect, r), s.Hue, strokeWidth, s.Dashed)
	}
}

// drawArrow is a bowed, sketched shaft with an open head; the head is solid
// even when the shaft is dashed, so the direction still reads.
func drawArrow(x0, y0, x1, y1 float32, hue int, w float32, head float32, dashed bool, r *sketchRng) {
	shaft := sketchBow(x0, y0, x1, y1, r)
	if len(shaft) == 0 {
		return
	}
	strokes(shaft, hue, w, dashed)
	p := shaft[0]
	a, b := p[len(p)-2], p[len(p)-1]
	dx, dy := b.X-a.X, b.Y-a.Y
	n := float32(math.Hypot(float64(dx), float64(dy)))
	if n > 0 {
		strokes(sketchHead(x1, y1, dx/n, dy/n, head, r), hue, w, false)
	}
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
