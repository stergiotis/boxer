// Package view renders a pipelineview.Layout into an imzero2 PaintCanvas
// using the existing painter binding — no new IDL/FFI surface, following the
// layeredgraph precedent (ADR-0069, ADR-0119). pipelineview stays UI-free;
// this package is the only half that imports the egui2 bindings.
//
// Layout coordinates are points, top-left origin, y-down. Render fits them
// into the target canvas (uniform scale, centred; a spine schematic is
// expected to fit its host, so there is no pan/zoom), then paints edges
// (orthogonal polylines with rounded corners, dashed for feedback, a filled
// arrow head at the target), stage boxes, endpoint glyphs (document,
// cylinder, parallelogram, discard), class-tinted port pins and labels, with
// one PaintSenseRegion per node so hover/click is reported back. The
// reported interaction is from the previous frame (immediate-mode one-frame
// lag).
package view

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pipelineview"
)

// Style holds the colours and metrics used to paint a Layout. A zero Style
// is treated as DefaultStyle() (detected via a non-positive StageFontSize).
type Style struct {
	Background     color.Color
	StageFill      color.Color
	StageStroke    color.Color
	StageText      color.Color
	EndpointFill   color.Color
	EndpointStroke color.Color
	EndpointText   color.Color
	EndpointSub    color.Color // endpoint sublabel ink (the detail line)
	SpineStroke    color.Color // primary flow (spine + skip edges)
	DiagStroke     color.Color // diagnostic pins + edges
	ConfigStroke   color.Color // config pins + edges
	ArtifactStroke color.Color // artifact pins + edges
	FeedbackStroke color.Color // feedback edges (drawn dashed)
	EdgeText       color.Color
	Highlight      color.Color // hovered-node border
	StageStrokeW   float32
	EdgeStrokeW    float32
	Rounding       float32 // stage-box corner rounding (points, pre-scale)
	CornerR        float32 // edge corner rounding (points, pre-scale)
	StageFontSize  float32 // fallback when Layout.FontSize == 0
	EdgeFontSize   float32
	PinRadius      float32 // port-pin dot radius (points, pre-scale)
}

// DefaultStyle returns the IDS-token-based default appearance. Side-channel
// classes reuse the semantic tones: diagnostics warning, config info,
// artifacts accent.
func DefaultStyle() Style {
	hex := func(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }
	return Style{
		Background:     hex(styletokens.NeutralBgPanel),
		StageFill:      hex(styletokens.NeutralBgSurface),
		StageStroke:    hex(styletokens.NeutralBorderDefault),
		StageText:      hex(styletokens.NeutralTextPrimary),
		EndpointFill:   hex(styletokens.NeutralBgPanel),
		EndpointStroke: hex(styletokens.NeutralBorderDefault),
		EndpointText:   hex(styletokens.NeutralTextSecondary),
		EndpointSub:    hex(styletokens.NeutralTextDisabled),
		SpineStroke:    hex(styletokens.NeutralTextSecondary),
		DiagStroke:     hex(styletokens.WarningDefault),
		ConfigStroke:   hex(styletokens.InfoDefault),
		ArtifactStroke: hex(styletokens.AccentDefault),
		FeedbackStroke: hex(styletokens.NeutralTextSecondary),
		EdgeText:       hex(styletokens.NeutralTextSecondary),
		Highlight:      hex(styletokens.AccentDefault),
		StageStrokeW:   1.0,
		EdgeStrokeW:    1.25,
		Rounding:       4.0,
		CornerR:        8.0,
		StageFontSize:  14.0,
		EdgeFontSize:   11.0,
		PinRadius:      3.5,
	}
}

// RenderOpts configures one Render call. The zero value is valid: default
// style, natural size, no per-element overrides.
type RenderOpts struct {
	Style Style
	// CanvasW/CanvasH: target canvas size in pixels. When both are > 0 the
	// layout is scaled uniformly to fit and centred; when 0 the canvas is the
	// layout's natural bounding box.
	CanvasW, CanvasH float32
	// NodeFill overrides a node's fill by id (stage or endpoint) — the hook a
	// host uses to mark running/failed/selected stages. Returning ok=false
	// keeps the style default.
	NodeFill func(id string) (col color.Color, ok bool)
	// NodeText overrides a node's label colour by id, paired with NodeFill so
	// a light fill can carry dark ink.
	NodeText func(id string) (col color.Color, ok bool)
	// EdgeStroke overrides an edge's stroke colour by endpoint node ids.
	EdgeStroke func(from, to string) (col color.Color, ok bool)
}

// RenderResult reports hit-testing from the previous frame: the node
// currently hovered and the node a primary click landed on (empty if none).
type RenderResult struct {
	Hovered string
	Clicked string
}

// Render paints lay and returns hover/click hit-testing. idBase namespaces
// this widget's canvas + per-node sense-region ids; pass a stable
// per-instance high-entropy constant so two pipelines on screen do not
// collide.
func Render(idBase uint64, lay *pipelineview.Layout, opts RenderOpts) RenderResult {
	st := opts.Style
	if st.StageFontSize <= 0 {
		st = DefaultStyle()
	}

	sm := c.CurrentApplicationState.StateManager
	scale, offX, offY, canvasW, canvasH := fit(lay, opts.CanvasW, opts.CanvasH)
	tf := func(p pipelineview.Point) (x, y float32) {
		return float32(p.X*scale + offX), float32(p.Y*scale + offY)
	}
	fscale := float32(scale)

	wis := c.NewWidgetIdStack()
	var res RenderResult
	for range c.IdScope(wis.PrepareHighEntropy(idBase)) {
		// Previous-frame node interaction drives this frame's highlight.
		hovered := make(map[string]bool, len(lay.Nodes))
		for _, n := range lay.Nodes {
			resp := sm.GetResponse(widgethandle.Make(wis.PrepareStr(n.ID).Derive()))
			if resp.HasHovered() {
				hovered[n.ID] = true
				res.Hovered = n.ID
			}
			if resp.HasPrimaryClicked() {
				res.Clicked = n.ID
			}
		}

		// Edges first, so nodes paint over the line ends.
		for _, e := range lay.Edges {
			col := edgeColor(st, e)
			if opts.EdgeStroke != nil {
				if c2, ok := opts.EdgeStroke(e.From.Key(), e.To.Key()); ok {
					col = c2
				}
			}
			drawOrtho(e.Points, tf, col, st.EdgeStrokeW, st.CornerR*fscale, e.Dashed)
			if e.LabelPos != nil && e.Label != "" {
				lx, ly := tf(*e.LabelPos)
				if e.Kind == pipelineview.EdgeSide {
					// Side edges are short vertical drops in a tight band —
					// a centred-above label collides with the boxes. Set it
					// beside the wire instead, like a schematic net label.
					c.PaintText(lx+4*fscale, ly, 0, 1, e.Label, st.EdgeFontSize*fscale, st.EdgeText).Send()
				} else {
					c.PaintText(lx, ly-3*fscale, 1, 2, e.Label, st.EdgeFontSize*fscale, st.EdgeText).Send()
				}
			}
		}

		stageFontPt := st.StageFontSize
		if lay.FontSize > 0 {
			stageFontPt = float32(lay.FontSize)
		}
		endFontPt := stageFontPt * 0.85
		for _, n := range lay.Nodes {
			cx, cy := tf(n.Center)
			w := float32(n.W * scale)
			h := float32(n.H * scale)
			fill := st.StageFill
			txt := st.StageText
			fontPt := stageFontPt
			if n.Kind == pipelineview.NodeEndpoint {
				fill = st.EndpointFill
				txt = st.EndpointText
				fontPt = endFontPt
			}
			if opts.NodeFill != nil {
				if c2, ok := opts.NodeFill(n.ID); ok {
					fill = c2
				}
			}
			if opts.NodeText != nil {
				if c2, ok := opts.NodeText(n.ID); ok {
					txt = c2
				}
			}
			if n.Kind == pipelineview.NodeStage {
				minX, minY, maxX, maxY := cx-w/2, cy-h/2, cx+w/2, cy+h/2
				c.PaintRectFilled(minX, minY, maxX, maxY, st.Rounding, fill).Send()
				c.PaintRectStroke(minX, minY, maxX, maxY, st.Rounding, st.StageStroke, st.StageStrokeW).Send()
			} else {
				drawEndpointGlyph(n.EKind, cx, cy, w, h, fill, st.EndpointStroke, st.StageStrokeW)
			}
			if hovered[n.ID] {
				c.PaintRectStroke(cx-w/2-2, cy-h/2-2, cx+w/2+2, cy+h/2+2, st.Rounding, st.Highlight, st.StageStrokeW+1.5).Send()
			}
			if n.Sublabel != "" {
				// Stack the two lines around the centre: label bottom-anchored
				// just above it, sublabel top-anchored just below.
				c.PaintText(cx, cy+1*fscale, 1, 2, n.Label, fontPt*fscale, txt).Send()
				c.PaintText(cx, cy+1*fscale, 1, 0, n.Sublabel, fontPt*0.85*fscale, st.EndpointSub).Send()
			} else {
				c.PaintText(cx, cy, 1, 1, n.Label, fontPt*fscale, txt).Send()
			}
			c.PaintSenseRegion(wis.PrepareStr(n.ID), cx-w/2, cy-h/2, w, h).Send()
		}

		// Port pins on top of the stage borders, class-tinted.
		for _, pin := range lay.Pins {
			px, py := tf(pin.Pos)
			c.PaintCircleFilled(px, py, st.PinRadius*fscale, classColor(st, pin.Class)).Send()
		}

		c.PaintCanvas(wis.PrepareStr("canvas"), canvasW, canvasH).Background(st.Background).Send()
	}
	return res
}

func classColor(st Style, cl pipelineview.PortClass) color.Color {
	switch cl {
	case pipelineview.PortDiagnostic:
		return st.DiagStroke
	case pipelineview.PortConfig:
		return st.ConfigStroke
	case pipelineview.PortArtifact:
		return st.ArtifactStroke
	}
	return st.SpineStroke
}

func edgeColor(st Style, e pipelineview.EdgeLayout) color.Color {
	switch e.Kind {
	case pipelineview.EdgeFeedback:
		return st.FeedbackStroke
	case pipelineview.EdgeSide:
		return classColor(st, e.Class)
	}
	return st.SpineStroke
}

// fitPad is the fraction of the canvas kept clear on each side when fitting.
const fitPad = 0.04

func fit(lay *pipelineview.Layout, targetW, targetH float32) (scale, offX, offY float64, canvasW, canvasH float32) {
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
	scale = math.Min(float64(targetW)/w, float64(targetH)/h)
	scale *= 1 - 2*fitPad
	offX = (float64(targetW) - w*scale) / 2
	offY = (float64(targetH) - h*scale) / 2
	return scale, offX, offY, targetW, targetH
}

// drawOrtho paints an orthogonal polyline with rounded interior corners (a
// quarter cubic Bézier per corner) and a filled arrow head on the final
// segment. Dashed edges keep sharp corners — a dash pattern cannot follow a
// Bézier — and use the dashed-line primitive per segment.
func drawOrtho(pts []pipelineview.Point, tf func(pipelineview.Point) (float32, float32), col color.Color, strokeW float32, cornerR float32, dashed bool) {
	if len(pts) < 2 {
		return
	}
	xs := make([]float32, len(pts))
	ys := make([]float32, len(pts))
	for i, p := range pts {
		xs[i], ys[i] = tf(p)
	}
	if dashed {
		for i := 0; i+1 < len(xs); i++ {
			c.PaintDashedLine(xs[i], ys[i], xs[i+1], ys[i+1], 6, 4, col, strokeW).Send()
		}
	} else {
		curX, curY := xs[0], ys[0]
		for i := 1; i+1 < len(xs); i++ {
			inDX, inDY := norm(xs[i]-xs[i-1], ys[i]-ys[i-1])
			outDX, outDY := norm(xs[i+1]-xs[i], ys[i+1]-ys[i])
			inLen := hyp(xs[i]-curX, ys[i]-curY)
			outLen := hyp(xs[i+1]-xs[i], ys[i+1]-ys[i])
			r := cornerR
			if inLen/2 < r {
				r = inLen / 2
			}
			if outLen/2 < r {
				r = outLen / 2
			}
			ax, ay := xs[i]-inDX*r, ys[i]-inDY*r
			bx, by := xs[i]+outDX*r, ys[i]+outDY*r
			if hyp(ax-curX, ay-curY) > 0.5 {
				c.PaintLine(curX, curY, ax, ay, col, strokeW).Send()
			}
			if r > 0.5 {
				c.PaintCubicBezier(ax, ay, xs[i], ys[i], xs[i], ys[i], bx, by, col, strokeW).Send()
			}
			curX, curY = bx, by
		}
		last := len(xs) - 1
		if hyp(xs[last]-curX, ys[last]-curY) > 0.5 {
			c.PaintLine(curX, curY, xs[last], ys[last], col, strokeW).Send()
		}
	}
	// Arrow head: a solid triangle whose tip is the final point, oriented
	// along the final segment.
	last := len(xs) - 1
	dx, dy := norm(xs[last]-xs[last-1], ys[last]-ys[last-1])
	if dx == 0 && dy == 0 {
		return
	}
	const headLen, headHalfW = 8.0, 3.6
	bxp, byp := xs[last]-dx*headLen, ys[last]-dy*headLen
	px, py := -dy*headHalfW, dx*headHalfW
	c.PaintPolygonFilled(
		[]float32{xs[last], bxp + px, bxp - px},
		[]float32{ys[last], byp + py, byp - py}, col).Send()
}

func hyp(dx, dy float32) float32 {
	return float32(math.Hypot(float64(dx), float64(dy)))
}

func norm(dx, dy float32) (nx, ny float32) {
	l := hyp(dx, dy)
	if l == 0 {
		return 0, 0
	}
	return dx / l, dy / l
}

// drawEndpointGlyph paints the endpoint shape for kind at centre (cx, cy)
// with full size (w, h): document (dog-eared rectangle), cylinder,
// parallelogram, or discard (circle + slash).
func drawEndpointGlyph(kind pipelineview.EndpointKind, cx, cy, w, h float32, fill, stroke color.Color, strokeW float32) {
	l, t, r, b := cx-w/2, cy-h/2, cx+w/2, cy+h/2
	switch kind {
	case pipelineview.EndpointStore:
		// Cap the ellipse caps so a taller cylinder grows its body (where the
		// text lives), not its lids.
		ry := min(h*0.16, 7)
		c.PaintEllipseFilled(cx, b-ry, w/2, ry, fill).Send()
		c.PaintEllipseStroke(cx, b-ry, w/2, ry, stroke, strokeW).Send()
		c.PaintRectFilled(l, t+ry, r, b-ry, 0, fill).Send()
		c.PaintLine(l, t+ry, l, b-ry, stroke, strokeW).Send()
		c.PaintLine(r, t+ry, r, b-ry, stroke, strokeW).Send()
		c.PaintEllipseFilled(cx, t+ry, w/2, ry, fill).Send()
		c.PaintEllipseStroke(cx, t+ry, w/2, ry, stroke, strokeW).Send()
	case pipelineview.EndpointStream:
		s := w * 0.10
		xsP := []float32{l + s, r, r - s, l}
		ysP := []float32{t, t, b, b}
		c.PaintPolygonFilled(xsP, ysP, fill).Send()
		c.PaintPolyline([]float32{l + s, r, r - s, l, l + s}, []float32{t, t, b, b, t}, stroke, strokeW).Send()
	case pipelineview.EndpointNull:
		rad := h / 2
		c.PaintCircleFilled(cx, cy, rad, fill).Send()
		c.PaintCircleStroke(cx, cy, rad, stroke, strokeW).Send()
		k := rad * 0.7071
		c.PaintLine(cx-k, cy+k, cx+k, cy-k, stroke, strokeW).Send()
	default: // EndpointFile
		ear := h * 0.30
		xsP := []float32{l, r - ear, r, r, l}
		ysP := []float32{t, t, t + ear, b, b}
		c.PaintPolygonFilled(xsP, ysP, fill).Send()
		c.PaintPolyline([]float32{l, r - ear, r, r, l, l}, []float32{t, t, t + ear, b, b, t}, stroke, strokeW).Send()
		c.PaintPolyline([]float32{r - ear, r - ear, r}, []float32{t, t + ear, t + ear}, stroke, strokeW).Send()
	}
}
