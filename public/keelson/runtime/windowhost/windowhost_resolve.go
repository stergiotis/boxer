package windowhost

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/inscribe"
)

// GeomEntry is one window's geometry, as Resolve reads it.
type GeomEntry struct {
	Key  WindowKeyT
	Geom WindowGeom
}

// Resolve turns a mark's anchor into a viewport rect against the
// windows' geometry of the last completed frame (ADR-0297 §SD3). A pure
// function: the overlay calls it every frame, so a mark follows its window.
//
//   - A viewport anchor is its rect, always shown.
//   - A window that is not in geoms is gone; one that has not reported a
//     frame yet is pending.
//   - A collapsed window resolves to its title bar, whatever part was named.
//   - A rect is behind when a window ranked further front overlaps it. Ranks
//     of 0 are unknown and never cover.
func Resolve(a inscribe.Anchor, geoms []GeomEntry) (r inscribe.Rect, v inscribe.VisibilityE) {
	if a.Viewport != nil {
		return *a.Viewport, inscribe.VisibilityShown
	}
	var target *GeomEntry
	for i := range geoms {
		if uint64(geoms[i].Key) == a.Window {
			target = &geoms[i]
			break
		}
	}
	switch {
	case target == nil:
		return r, inscribe.VisibilityGone
	case !target.Geom.Shown:
		return r, inscribe.VisibilityPending
	}
	g := target.Geom.Rect
	r = inscribe.Rect{X: g.MinX, Y: g.MinY, W: g.W(), H: g.H()}
	if target.Geom.Collapsed {
		return r, inscribe.VisibilityCollapsed
	}
	if a.Local != nil {
		r = inscribe.Rect{X: g.MinX + a.Local.X, Y: g.MinY + a.Local.Y, W: a.Local.W, H: a.Local.H}
	}
	v = inscribe.VisibilityShown
	if target.Geom.Stack == 0 {
		return
	}
	for _, o := range geoms {
		if o.Key == target.Key || !o.Geom.Shown || o.Geom.Stack <= target.Geom.Stack {
			continue
		}
		og := o.Geom.Rect
		if r.Intersects(inscribe.Rect{X: og.MinX, Y: og.MinY, W: og.W(), H: og.H()}) {
			v = inscribe.VisibilityBehind
			return
		}
	}
	return
}

// geomResolver resolves against one frame's geometry.
type geomResolver []GeomEntry

func (inst geomResolver) Resolve(a inscribe.Anchor) (inscribe.Rect, inscribe.VisibilityE) {
	return Resolve(a, inst)
}
