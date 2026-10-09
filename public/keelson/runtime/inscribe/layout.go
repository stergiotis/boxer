package inscribe

import (
	"math"
	"slices"
	"strconv"
)

// The overlay's measures, in logical points.
const (
	noteFont   float32 = 14
	tagFont    float32 = 10
	badgeFont  float32 = 11
	notePad    float32 = 6
	noteGap    float32 = 12
	outlineGap float32 = 3
	badgeR     float32 = 9
	tagRowGap  float32 = 2
	tabPad     float32 = 3
	// noteAccent is the hue bar down a note's left edge; tagDot the hue dot
	// and gap before the attribution.
	noteAccent float32 = 4
	tagDot     float32 = 10
)

// ShapeKindE is what a Shape draws.
type ShapeKindE uint8

const (
	ShapeDim ShapeKindE = iota
	ShapeOutline
	ShapeArrow
	ShapeLeader
	ShapeBadge
	ShapeNote
	// ShapeTab is a mark's attribution alone, a small tab on the corner of
	// its outline: a mark without text needs no note.
	ShapeTab
)

// Shape is one drawing instruction of the overlay, in viewport points.
type Shape struct {
	Kind ShapeKindE
	Hue  int
	// Rect is an outline's, a dim area's or a note's rect.
	Rect Rect
	// X0, Y0 → X1, Y1 is an arrow or a leader; X0, Y0 a badge's centre.
	X0, Y0, X1, Y1 float32
	// Dashed is a target behind another window.
	Dashed bool
	// Text is a note's text or a badge's number; Tag a note's attribution.
	Text, Tag string
	// Seed nudges the shape's sketched strokes: from the mark's identity,
	// so the shape keeps its look from frame to frame.
	Seed uint64
	// Style is how an outline marks its target.
	Style OutlineStyleE
}

// OutlineStyleE is how a target is marked, by what it is (ADR-0297 §SD6).
type OutlineStyleE uint8

const (
	// OutlineCircle is a pen circle round a small target: a button, a cell.
	OutlineCircle OutlineStyleE = iota
	// OutlineSwipe is a highlighter swipe over a row or a line of text.
	OutlineSwipe
	// OutlineBrackets marks a whole window, or another large target, by its
	// corners.
	OutlineBrackets
)

// styleFor is the outline a target gets: brackets for a whole window or a
// large area, a swipe for something wide and one line tall, a circle
// otherwise.
func styleFor(a Anchor, r Rect) OutlineStyleE {
	switch {
	case a.Window != 0 && a.Local == nil, r.W > 320 && r.H > 160:
		return OutlineBrackets
	case r.W >= 3*r.H && r.H <= 48:
		return OutlineSwipe
	}
	return OutlineCircle
}

// Resolved is an item with its targets resolved this frame.
type Resolved struct {
	Item
	Rects []Rect
	Vis   []VisibilityE
	// Windows are the outer rects of the targets' windows; zero for a
	// viewport anchor.
	Windows []Rect
}

// MeasureFunc gives a single line's size in the given font size.
type MeasureFunc func(text string, fontSize float32) (w, h float32)

// EstimateMeasure is a text size from its length: what a note uses before
// egui's measure arrives (ADR-0297 §SD6).
func EstimateMeasure(text string, fontSize float32) (w, h float32) {
	return float32(len([]rune(text))) * fontSize * 0.56, fontSize * 1.3
}

// Tag is the attribution a task's marks carry.
func Tag(task string) string {
	short := task
	if len(short) > 6 {
		short = short[len(short)-6:]
	}
	return "agent · " + short
}

// Layout turns the resolved items into shapes, back to front: dimming,
// outlines and arrows, leaders and badges, then notes and tabs. Notes are
// placed in one pass over every task's items (ADR-0297 §SD6, §SD7), each in
// the first place inside bounds and clear of the notes already placed and
// of every mark's target: first beside its target's window, on desktop no
// window covers, so the note hides nothing; then beside the target itself.
// A mark without text gets a tab on its outline's corner instead of a note.
// windows are the outer rects of every shown window.
func Layout(items []Resolved, bounds Rect, windows []Rect, measure MeasureFunc) (shapes []Shape) {
	var holes []Rect
	var marks, leaders, notes []Shape
	var placed []Rect
	// Every target is in the way of a note, so a note never covers what
	// another mark points at.
	var targets []Rect
	for _, it := range items {
		for _, r := range it.Rects {
			targets = append(targets, r.Inflate(outlineGap))
		}
	}
	placeNote := func(it Resolved, target Rect, win Rect, text string) {
		tag := Tag(it.Task)
		gw, gh := measure(tag, tagFont)
		if text == "" {
			// A tab hugging the outline's top-left corner, above it when
			// there is room.
			w, h := tagDot+gw+2*tabPad, gh+2*tabPad
			box := placeTab(target, w, h, bounds, append(slices.Clone(placed), obstacles(targets, target)...))
			placed = append(placed, box)
			notes = append(notes, Shape{Kind: ShapeTab, Hue: it.Hue, Rect: box, Tag: tag})
			return
		}
		tw, th := measure(text, noteFont)
		w := max(tw, tagDot+gw) + 2*notePad + noteAccent
		h := th + tagRowGap + gh + 2*notePad
		in := append(slices.Clone(placed), obstacles(targets, target)...)
		box, ok := placeOutside(target, win, w, h, bounds, in, windows)
		if !ok {
			box = placeBox(target, w, h, bounds, in)
		}
		placed = append(placed, box)
		notes = append(notes, Shape{Kind: ShapeNote, Hue: it.Hue, Rect: box, Text: text, Tag: tag})
		lx0, ly0 := nearestOnRect(box, target)
		lx1, ly1 := nearestOnRect(target, box)
		if !box.Intersects(target) {
			leaders = append(leaders, Shape{Kind: ShapeLeader, Hue: it.Hue, X0: lx0, Y0: ly0, X1: lx1, Y1: ly1})
		}
	}
	for _, it := range items {
		// Every shape of the mark is seeded by the mark and its place in it.
		mark0 := len(marks)
		leader0, note0 := len(leaders), len(notes)
		dashed := func(k int) bool { return it.Vis[k] == VisibilityBehind }
		outline := func(k int) {
			var a Anchor
			if k < len(it.Targets) {
				a = it.Targets[k]
			}
			marks = append(marks, Shape{Kind: ShapeOutline, Hue: it.Hue, Rect: it.Rects[k].Inflate(outlineGap), Dashed: dashed(k),
				Style: styleFor(a, it.Rects[k])})
		}
		win := func(k int) Rect {
			if k < len(it.Windows) {
				return it.Windows[k]
			}
			return Rect{}
		}
		switch it.Op {
		case OpHighlight:
			for k := range it.Rects {
				outline(k)
			}
			placeNote(it, it.Rects[0].Inflate(outlineGap), win(0), it.Text)
		case OpCallout:
			outline(0)
			placeNote(it, it.Rects[0].Inflate(outlineGap), win(0), it.Text)
		case OpStep:
			outline(0)
			r := it.Rects[0].Inflate(outlineGap)
			marks = append(marks, Shape{Kind: ShapeBadge, Hue: it.Hue, X0: r.X, Y0: r.Y, Text: strconv.Itoa(it.Step)})
			placeNote(it, r, win(0), strconv.Itoa(it.Step)+". "+it.Text)
		case OpArrow:
			a, b := it.Rects[0], it.Rects[1]
			x0, y0 := edgeToward(a, b)
			x1, y1 := edgeToward(b, a)
			marks = append(marks, Shape{Kind: ShapeArrow, Hue: it.Hue, X0: x0, Y0: y0, X1: x1, Y1: y1,
				Dashed: dashed(0) || dashed(1)})
			if it.Text == "" {
				placeNote(it, Rect{X: x0, Y: y0, W: 1, H: 1}, Rect{}, "")
			} else {
				placeNote(it, Rect{X: (x0+x1)/2 - 1, Y: (y0+y1)/2 - 1, W: 2, H: 2}, win(1), it.Text)
			}
		case OpSpotlight:
			for k := range it.Rects {
				outline(k)
				holes = append(holes, it.Rects[k].Inflate(outlineGap+4))
			}
			placeNote(it, it.Rects[0].Inflate(outlineGap), win(0), it.Text)
		}
		n := 0
		for _, set := range [][]Shape{marks[mark0:], leaders[leader0:], notes[note0:]} {
			for i := range set {
				set[i].Seed = seedOf(it.Task, it.Id, n)
				n++
			}
		}
	}
	if len(holes) > 0 {
		// Spotlights from every task combine: one dimming outside them all.
		for _, r := range subtract([]Rect{bounds}, holes) {
			shapes = append(shapes, Shape{Kind: ShapeDim, Rect: r})
		}
	}
	shapes = append(shapes, marks...)
	shapes = append(shapes, leaders...)
	shapes = append(shapes, notes...)
	return
}

// obstacles are the targets a note for target must keep clear of: all but
// those that hold target — a whole window around a button inside it —
// which every place beside target would touch.
func obstacles(targets []Rect, target Rect) (out []Rect) {
	for _, t := range targets {
		if !target.Inside(t.Inflate(1)) {
			out = append(out, t)
		}
	}
	return
}

// placeTab puts a w × h tab on a corner of target — above its left end,
// below it, above its right end, below that — the first inside bounds and
// clear of avoid; otherwise the first, moved inside bounds.
func placeTab(target Rect, w, h float32, bounds Rect, avoid []Rect) Rect {
	cands := []Rect{
		{X: target.X, Y: target.Y - h, W: w, H: h},
		{X: target.X, Y: target.MaxY(), W: w, H: h},
		{X: target.MaxX() - w, Y: target.Y - h, W: w, H: h},
		{X: target.MaxX() - w, Y: target.MaxY(), W: w, H: h},
	}
	for _, c := range cands {
		if c.Inside(bounds) && !hits(c, avoid, 1) {
			return c
		}
	}
	c := cands[0]
	c.X = min(max(c.X, bounds.X), bounds.MaxX()-c.W)
	c.Y = min(max(c.Y, bounds.Y), bounds.MaxY()-c.H)
	return c
}

// placeOutside puts a w × h box beside the window holding target, level
// with the target — right, left, below, above — where it covers no window,
// stays inside bounds and keeps clear of avoid. ok is false when no side
// has room, or target has no window.
func placeOutside(target Rect, win Rect, w, h float32, bounds Rect, avoid []Rect, windows []Rect) (box Rect, ok bool) {
	if win.W <= 0 || win.H <= 0 {
		return
	}
	cx, cy := target.Center()
	cands := []Rect{
		{X: win.MaxX() + noteGap, Y: cy - h/2, W: w, H: h},
		{X: win.X - noteGap - w, Y: cy - h/2, W: w, H: h},
		{X: cx - w/2, Y: win.MaxY() + noteGap, W: w, H: h},
		{X: cx - w/2, Y: win.Y - noteGap - h, W: w, H: h},
	}
	for _, c := range cands {
		if !c.Inside(bounds) || hits(c, avoid, 2) || hits(c, windows, 0) {
			continue
		}
		return c, true
	}
	return
}

func hits(c Rect, rs []Rect, pad float32) bool {
	for _, r := range rs {
		if c.Intersects(r.Inflate(pad)) {
			return true
		}
	}
	return false
}

// placeBox puts a w × h box beside target: right, left, below, above, in
// that order, the first inside bounds and clear of placed; otherwise the
// first, moved inside bounds.
func placeBox(target Rect, w, h float32, bounds Rect, placed []Rect) Rect {
	cx, cy := target.Center()
	cands := []Rect{
		{X: target.MaxX() + noteGap, Y: cy - h/2, W: w, H: h},
		{X: target.X - noteGap - w, Y: cy - h/2, W: w, H: h},
		{X: cx - w/2, Y: target.MaxY() + noteGap, W: w, H: h},
		{X: cx - w/2, Y: target.Y - noteGap - h, W: w, H: h},
	}
	for _, c := range cands {
		if !c.Inside(bounds) {
			continue
		}
		clear := true
		for _, p := range placed {
			if c.Intersects(p.Inflate(2)) {
				clear = false
				break
			}
		}
		if clear {
			return c
		}
	}
	c := cands[0]
	c.X = min(max(c.X, bounds.X), bounds.MaxX()-c.W)
	c.Y = min(max(c.Y, bounds.Y), bounds.MaxY()-c.H)
	return c
}

// nearestOnRect is the point of r nearest to o's centre.
func nearestOnRect(r Rect, o Rect) (x, y float32) {
	ox, oy := o.Center()
	return min(max(ox, r.X), r.MaxX()), min(max(oy, r.Y), r.MaxY())
}

// edgeToward is where the line from a's centre to b's centre leaves a.
func edgeToward(a Rect, b Rect) (x, y float32) {
	ax, ay := a.Center()
	bx, by := b.Center()
	dx, dy := bx-ax, by-ay
	if dx == 0 && dy == 0 {
		return ax, ay
	}
	tx, ty := float32(math.Inf(1)), float32(math.Inf(1))
	if dx != 0 {
		tx = (a.W / 2) / abs(dx)
	}
	if dy != 0 {
		ty = (a.H / 2) / abs(dy)
	}
	t := min(tx, ty, 1)
	return ax + dx*t, ay + dy*t
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// subtract is what of rs lies outside every hole, as rects.
func subtract(rs []Rect, holes []Rect) []Rect {
	for _, h := range holes {
		var next []Rect
		for _, r := range rs {
			next = append(next, cut(r, h)...)
		}
		rs = next
	}
	return rs
}

// cut is r less h: up to four rects — above, below, left and right of h.
func cut(r Rect, h Rect) (out []Rect) {
	if !r.Intersects(h) {
		return []Rect{r}
	}
	top := max(r.Y, h.Y)
	bottom := min(r.MaxY(), h.MaxY())
	if h.Y > r.Y {
		out = append(out, Rect{X: r.X, Y: r.Y, W: r.W, H: h.Y - r.Y})
	}
	if h.MaxY() < r.MaxY() {
		out = append(out, Rect{X: r.X, Y: h.MaxY(), W: r.W, H: r.MaxY() - h.MaxY()})
	}
	if h.X > r.X {
		out = append(out, Rect{X: r.X, Y: top, W: h.X - r.X, H: bottom - top})
	}
	if h.MaxX() < r.MaxX() {
		out = append(out, Rect{X: h.MaxX(), Y: top, W: r.MaxX() - h.MaxX(), H: bottom - top})
	}
	return
}
