// Package view renders a layeredgraph.Layout into an imzero2 PaintCanvas using
// the existing painter binding — there is no new IDL/FFI surface (ADR-0069).
// It is the imzero2-facing half of the layered-graph widget: layeredgraph (the
// engine seam) and any engine that satisfies layeredgraph.Engine stay UI-free,
// and this package is the only one here that imports the egui2 bindings.
//
// layeredgraph coordinates are points, top-left origin, y-down. Render fits
// them into the target canvas (uniform scale, centred — the v1 "fit to view";
// pan/zoom is deferred to v2 per ADR-0069), then paints nodes (box/circle),
// edges (cubic-Bézier splines plus a synthesised arrow head) and labels, with
// one PaintSenseRegion per node so hover/click is reported back. The reported
// interaction is from the previous frame (immediate-mode one-frame lag).
package view

import (
	"hash/fnv"
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// Style holds the colours and metrics used to paint a Layout. A zero Style is
// treated as DefaultStyle() (detected via a non-positive NodeFontSize), so
// RenderOpts{} renders with sensible defaults.
type Style struct {
	Background  color.Color // canvas background
	NodeFill    color.Color
	NodeStroke  color.Color
	NodeText    color.Color
	EdgeStroke  color.Color
	EdgeText    color.Color
	Highlight   color.Color // hovered-node border
	NodeStrokeW float32
	EdgeStrokeW float32
	Rounding    float32 // box-node corner rounding (points, pre-scale)
	NodeFontSize float32 // points, pre-scale
	EdgeFontSize float32 // points, pre-scale
}

// DefaultStyle returns the IDS-token-based default appearance.
func DefaultStyle() Style {
	hex := func(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }
	return Style{
		Background:   hex(styletokens.NeutralBgPanel),
		NodeFill:     hex(styletokens.NeutralBgSurface),
		NodeStroke:   hex(styletokens.NeutralBorderDefault),
		NodeText:     hex(styletokens.NeutralTextPrimary),
		EdgeStroke:   hex(styletokens.NeutralBorderDefault),
		EdgeText:     hex(styletokens.NeutralTextSecondary),
		Highlight:    hex(styletokens.AccentDefault),
		NodeStrokeW:  1.0,
		EdgeStrokeW:  1.25,
		Rounding:     4.0,
		NodeFontSize: 14.0,
		EdgeFontSize: 11.0,
	}
}

// RenderOpts configures one Render call. The zero value is valid: default
// style, natural size (1 point = 1 pixel), no per-element overrides.
type RenderOpts struct {
	Style Style
	// CanvasW/CanvasH: target canvas size in pixels. When both are > 0 the
	// layout is scaled uniformly to fit and centred. When 0, the canvas is the
	// layout's natural bounding box.
	CanvasW, CanvasH float32
	// NodeFill overrides a node's fill by id (e.g. to mark the current FSM
	// state). Returning ok=false keeps the style default. Optional.
	NodeFill func(id string) (col color.Color, ok bool)
	// EdgeStroke overrides an edge's stroke colour by endpoints (e.g. to mark
	// the active transition). Returning ok=false keeps the style default.
	EdgeStroke func(from, to string) (col color.Color, ok bool)
	// State, when non-nil, enables interactive pan/zoom: Render reads pointer
	// drag (pan) and the zoom gesture (Ctrl+scroll / pinch / +/-) over the
	// canvas and updates it in place. The caller holds one ViewState per graph
	// across frames and passes its address. Nil keeps the static fit-to-view.
	State *ViewState
}

// ViewState carries interactive pan/zoom across frames for one graph. The
// caller owns it (one per graph instance) and passes &it via RenderOpts.State;
// Render mutates Zoom/PanX/PanY from user input. Zoom 0 is treated as 1, so the
// zero value starts at the fitted view — reset to the zero value to recentre.
type ViewState struct {
	Zoom       float64 // multiplicative, composed on top of fit-to-view (0 → 1)
	PanX, PanY float64 // screen-pixel offset

	lastPtrX float32
	lastPtrY float32
	dragging bool
}

// RenderResult reports hit-testing from the previous frame: the node currently
// hovered (empty if none) and the node a primary click landed on this frame
// (empty if none).
type RenderResult struct {
	Hovered string
	Clicked string
}

// Render paints lay and returns hover/click hit-testing. idBase namespaces this
// widget's canvas + per-node sense-region ids; pass a stable per-instance
// high-entropy constant so two layered graphs on screen do not collide.
func Render(idBase uint64, lay *layeredgraph.Layout, opts RenderOpts) RenderResult {
	st := opts.Style
	if st.NodeFontSize <= 0 {
		st = DefaultStyle()
	}

	sm := c.CurrentApplicationState.StateManager
	canvasID := c.MakeAbsoluteIdHighEntropy(idBase + 1)
	scale, offX, offY, canvasW, canvasH := fit(lay, opts.CanvasW, opts.CanvasH)

	// User pan/zoom (opt-in via opts.State) composes on top of fit-to-view as a
	// single affine: screen = p*(scale*zoom) + offset, zoom about the canvas
	// centre, then panned. Input is read from the previous frame's canvas
	// response (drag) and the zoom-gesture register, both scoped to the canvas.
	zoom, panX, panY := 1.0, 0.0, 0.0
	if vs := opts.State; vs != nil {
		resp := sm.GetResponse(widgethandle.Make(canvasID.Derive()))
		if resp.HasContainsPointer() {
			if zd := sm.GetZoomDelta().Zoom; zd > 0 && zd != 1 {
				z := vs.Zoom
				if z <= 0 {
					z = 1
				}
				vs.Zoom = clampF64(z*float64(zd), 0.2, 5.0)
			}
		}
		cp := sm.GetCanvasPointer()
		ptrValid := cp.HoverX == cp.HoverX && cp.HoverY == cp.HoverY // == rejects NaN
		if resp.HasIsPointerButtonDown() && resp.HasContainsPointer() && ptrValid {
			if vs.dragging {
				vs.PanX += float64(cp.HoverX - vs.lastPtrX)
				vs.PanY += float64(cp.HoverY - vs.lastPtrY)
			}
			vs.dragging = true
			vs.lastPtrX, vs.lastPtrY = cp.HoverX, cp.HoverY
		} else {
			vs.dragging = false
		}
		if vs.Zoom <= 0 {
			vs.Zoom = 1
		}
		zoom, panX, panY = vs.Zoom, vs.PanX, vs.PanY
	}
	ccx, ccy := float64(canvasW)/2, float64(canvasH)/2
	escale := scale * zoom
	ox := (offX-ccx)*zoom + ccx + panX
	oy := (offY-ccy)*zoom + ccy + panY
	tf := func(p layeredgraph.Point) (x, y float32) {
		return float32(p.X*escale + ox), float32(p.Y*escale + oy)
	}
	nodeKey := func(id string) uint64 {
		h := fnv.New64a()
		_, _ = h.Write([]byte(id))
		// <<1 keeps bit 0 clear so the |1 that Derive applies cannot collapse
		// two distinct keys (their high bits still differ).
		return idBase + (h.Sum64() << 1)
	}

	// Read previous-frame node interaction; it drives this frame's highlight.
	hovered := make(map[string]bool, len(lay.Nodes))
	var res RenderResult
	for _, n := range lay.Nodes {
		resp := sm.GetResponse(widgethandle.Make(c.MakeAbsoluteIdHighEntropy(nodeKey(n.ID)).Derive()))
		if resp.HasHovered() {
			hovered[n.ID] = true
			res.Hovered = n.ID
		}
		if resp.HasPrimaryClicked() {
			res.Clicked = n.ID
		}
	}

	// Edges first, so nodes paint over the spline ends.
	for _, e := range lay.Edges {
		col := st.EdgeStroke
		if opts.EdgeStroke != nil {
			if c2, ok := opts.EdgeStroke(e.From, e.To); ok {
				col = c2
			}
		}
		drawEdge(e, tf, col, st.EdgeStrokeW)
		if e.LabelPos != nil && e.Label != "" {
			lx, ly := tf(*e.LabelPos)
			c.PaintText(lx, ly, 1, 1, e.Label, st.EdgeFontSize*float32(escale), st.EdgeText).Send()
		}
	}

	// Nodes.
	for _, n := range lay.Nodes {
		cx, cy := tf(n.Center)
		w := float32(n.W * escale)
		h := float32(n.H * escale)
		fill := st.NodeFill
		if opts.NodeFill != nil {
			if c2, ok := opts.NodeFill(n.ID); ok {
				fill = c2
			}
		}
		drawNode(n.Shape, cx, cy, w, h, st, fill, hovered[n.ID])
		c.PaintText(cx, cy, 1, 1, n.Label, st.NodeFontSize*float32(escale), st.NodeText).Send()
		c.PaintSenseRegion(c.MakeAbsoluteIdHighEntropy(nodeKey(n.ID)), cx-w/2, cy-h/2, w, h).Send()
	}

	// Drain into the canvas. Sense click/drag/hover only when pan/zoom is
	// enabled, so Render can read drag + zoom over the canvas.
	cv := c.PaintCanvas(canvasID, canvasW, canvasH).Background(st.Background)
	if opts.State != nil {
		cv = cv.Sense(true, true, true)
	}
	cv.Send()

	return res
}

func clampF64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// fitPad is the fraction of the canvas kept clear on each side when fitting,
// so node strokes, the self-loop and edge labels (whose egui font metrics
// differ slightly from Graphviz's layout estimate) don't clip at the edges.
const fitPad = 0.06

// fit computes the uniform scale + centring offset to map the layout's bounding
// box into the target canvas (less a fitPad margin), and the resulting canvas
// size. A non-positive or degenerate target falls back to the layout's natural
// size at 1:1.
func fit(lay *layeredgraph.Layout, targetW, targetH float32) (scale, offX, offY float64, canvasW, canvasH float32) {
	w, h := lay.Width, lay.Height
	if targetW <= 0 || targetH <= 0 || w <= 0 || h <= 0 {
		if w <= 0 {
			w = 1
		}
		if h <= 0 {
			h = 1
		}
		return 1, 0, 0, float32(w), float32(h)
	}
	sx := float64(targetW) / w
	sy := float64(targetH) / h
	scale = sx
	if sy < sx {
		scale = sy
	}
	scale *= 1 - 2*fitPad // leave a margin on every side
	// Centring uses the padded scale, so the margin is split evenly.
	offX = (float64(targetW) - w*scale) / 2
	offY = (float64(targetH) - h*scale) / 2
	return scale, offX, offY, targetW, targetH
}

func drawNode(sh layeredgraph.NodeShape, cx, cy, w, h float32, st Style, fill color.Color, hovered bool) {
	switch sh {
	case layeredgraph.NodeShapeCircle:
		r := w
		if h < w {
			r = h
		}
		r /= 2
		c.PaintCircleFilled(cx, cy, r, fill).Send()
		c.PaintCircleStroke(cx, cy, r, st.NodeStroke, st.NodeStrokeW).Send()
		if hovered {
			c.PaintCircleStroke(cx, cy, r+2, st.Highlight, st.NodeStrokeW+1.5).Send()
		}
	case layeredgraph.NodeShapeEllipse:
		rx, ry := w/2, h/2
		c.PaintEllipseFilled(cx, cy, rx, ry, fill).Send()
		c.PaintEllipseStroke(cx, cy, rx, ry, st.NodeStroke, st.NodeStrokeW).Send()
		if hovered {
			c.PaintEllipseStroke(cx, cy, rx+2, ry+2, st.Highlight, st.NodeStrokeW+1.5).Send()
		}
	default: // box
		minX, minY, maxX, maxY := cx-w/2, cy-h/2, cx+w/2, cy+h/2
		c.PaintRectFilled(minX, minY, maxX, maxY, st.Rounding, fill).Send()
		c.PaintRectStroke(minX, minY, maxX, maxY, st.Rounding, st.NodeStroke, st.NodeStrokeW).Send()
		if hovered {
			c.PaintRectStroke(minX-2, minY-2, maxX+2, maxY+2, st.Rounding, st.Highlight, st.NodeStrokeW+1.5).Send()
		}
	}
}

func drawEdge(e layeredgraph.EdgeLayout, tf func(layeredgraph.Point) (float32, float32), col color.Color, strokeW float32) {
	pts := e.Points
	switch {
	case len(pts) >= 4:
		// Graphviz B-spline: start point then groups of three cubic controls.
		for i := 0; i+3 < len(pts); i += 3 {
			x0, y0 := tf(pts[i])
			x1, y1 := tf(pts[i+1])
			x2, y2 := tf(pts[i+2])
			x3, y3 := tf(pts[i+3])
			c.PaintCubicBezier(x0, y0, x1, y1, x2, y2, x3, y3, col, strokeW).Send()
		}
	case len(pts) >= 2:
		xs := make([]float32, len(pts))
		ys := make([]float32, len(pts))
		for i, p := range pts {
			xs[i], ys[i] = tf(p)
		}
		c.PaintPolyline(xs, ys, col, strokeW).Send()
	}
	// Arrow head: a solid triangle from the spline end (base) to the head tip,
	// drawn with the filled-polygon primitive for a clean Graphviz-style head.
	if e.ArrowHead != nil && len(pts) >= 1 {
		bx, by := tf(pts[len(pts)-1]) // spline end = arrow base
		hx, hy := tf(*e.ArrowHead)    // arrow tip
		dx, dy := hx-bx, hy-by
		l := float32(math.Hypot(float64(dx), float64(dy)))
		if l > 0.5 {
			ux, uy := dx/l, dy/l    // shaft direction (unit)
			hw := l * 0.35          // half-width of the head base
			if hw < 2 {
				hw = 2
			}
			px, py := -uy*hw, ux*hw // perpendicular to the shaft
			xs := []float32{hx, bx + px, bx - px}
			ys := []float32{hy, by + py, by - py}
			c.PaintPolygonFilled(xs, ys, col).Send()
		}
	}
}
