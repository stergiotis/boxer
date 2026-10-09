package inscribe

// Rect is a rect in logical points: its top-left corner and its size.
type Rect struct {
	X, Y, W, H float32
}

func (inst Rect) MaxX() float32 { return inst.X + inst.W }
func (inst Rect) MaxY() float32 { return inst.Y + inst.H }

// Center is the rect's centre point.
func (inst Rect) Center() (x, y float32) { return inst.X + inst.W/2, inst.Y + inst.H/2 }

// Intersects reports whether the two rects share an area.
func (inst Rect) Intersects(o Rect) bool {
	return inst.X < o.MaxX() && o.X < inst.MaxX() && inst.Y < o.MaxY() && o.Y < inst.MaxY()
}

// Inflate grows the rect by d on every side.
func (inst Rect) Inflate(d float32) Rect {
	return Rect{X: inst.X - d, Y: inst.Y - d, W: inst.W + 2*d, H: inst.H + 2*d}
}

// Inside reports whether the rect lies wholly within o.
func (inst Rect) Inside(o Rect) bool {
	return inst.X >= o.X && inst.Y >= o.Y && inst.MaxX() <= o.MaxX() && inst.MaxY() <= o.MaxY()
}

// Anchor names what an annotation points at (ADR-0297 §SD2). Exactly one
// form is set: Window alone is the window's outer rect; Window with Local a
// rect relative to the window's top-left corner; Viewport a rect of the
// viewport, with Window zero.
type Anchor struct {
	Window   uint64
	Local    *Rect
	Viewport *Rect
}

// Valid reports whether the anchor has exactly one form.
func (inst Anchor) Valid() bool {
	if inst.Viewport != nil {
		return inst.Window == 0 && inst.Local == nil
	}
	return inst.Window != 0
}

// VisibilityE is how an anchor's target stands this frame.
type VisibilityE uint8

const (
	// VisibilityShown is a target in view.
	VisibilityShown VisibilityE = iota
	// VisibilityBehind is a target a window ranked further front overlaps:
	// drawn dashed.
	VisibilityBehind
	// VisibilityCollapsed is a target in a collapsed window: placed on its
	// title bar.
	VisibilityCollapsed
	// VisibilityPending is a window not shown yet — opened this frame: not
	// drawn, not retired.
	VisibilityPending
	// VisibilityGone is a window that closed: the annotation is retired.
	VisibilityGone
)

// ResolverI turns an anchor into a viewport rect, against the geometry of
// the last completed frame. The window host implements it.
type ResolverI interface {
	Resolve(a Anchor) (r Rect, v VisibilityE)
}
