package imagescope

// Regions on screen (ADR-0399 §M2): the tools that make them by gesture,
// the overlay that draws them through the viewer, the fields that edit the
// one selected, the table that lists them with their figures, and the
// profile of a line. Every change goes through the catalog's handlers as
// the person's gesture (ADR-0269 §SD8).

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/science/imaging/regions"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bgjobrow"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// toolE is the tool in hand: what a click or a drag on the image does.
type toolE uint8

const (
	toolSelect toolE = iota
	toolPoint
	toolLine
	toolRectangle
	toolEllipse
	toolPolygon
)

var allTools = []toolE{toolSelect, toolPoint, toolLine, toolRectangle, toolEllipse, toolPolygon}

func (inst toolE) String() string {
	switch inst {
	case toolSelect:
		return "select"
	case toolPoint:
		return "point"
	case toolLine:
		return "line"
	case toolRectangle:
		return "rectangle"
	case toolEllipse:
		return "ellipse"
	case toolPolygon:
		return "polygon"
	}
	return "?"
}

// takesDrag reports a tool that makes its shape by a drag.
func (inst toolE) takesDrag() bool {
	return inst == toolLine || inst == toolRectangle || inst == toolEllipse
}

// Hover help of the region controls (keelson-app A5).
const (
	tipToolSelect    = "Click a region to select it, drag the selected one to move it, Delete removes it. Elsewhere a drag pans, or marks the rectangle while Select region is on."
	tipToolPoint     = "Click to add a point. A point covers the pixel that contains it."
	tipToolLine      = "Drag from one end to the other to add a line. A line covers the pixels it passes through; its profile lists them in order."
	tipToolRectangle = "Drag across opposite corners to add a rectangle. Its rotation is a number in the fields of the selected region."
	tipToolEllipse   = "Drag across the box that holds it to add an ellipse. Its rotation is a number in the fields of the selected region."
	tipToolPolygon   = "Click each vertex; click the first again, or press Enter, to close it; Escape drops it."
	tipKeepRegion    = "Keep the marked rectangle as a region of its own, with a label, in pixel coordinates; the marked rectangle stays the transient selection that the statistics above read."
	tipMeasureArea   = "Add the figures by covered area: every pixel the shape touches weighted by the exact fraction of its area inside the shape. For a small shape they can differ from the figures by centre by several percent."
	tipRegionFigures = methodRegions
	tipProfile       = methodProfile
	tipRegionsDoc    = "The regions as the versioned regions document: shapes by their parameters, each in its stated frame, with its anchor, label, note and origin, and nothing about how they are drawn."
	tipRegionOrigin  = "Who made the region: person for your own gestures and fields, task:… or agent for an agent's call, document for a document that named none. A region an agent made is drawn dashed and its label starts with ◇."
)

var toolTips = map[toolE]string{toolSelect: tipToolSelect, toolPoint: tipToolPoint, toolLine: tipToolLine,
	toolRectangle: tipToolRectangle, toolEllipse: tipToolEllipse, toolPolygon: tipToolPolygon}

// regionKeys are the keys the viewer hands the window while it has focus.
var regionKeys = keycodes.MaskOf(keycodes.Delete, keycodes.Backspace, keycodes.Enter, keycodes.Escape)

// renderToolRow is the row of tools, and the way from the marked rectangle
// to a region.
func (inst *App) renderToolRow() {
	for range c.Horizontal().KeepIter() {
		c.Label("Tool").Selectable(false).Send()
		for range c.IdScope(inst.ids.PrepareStr("tool")) {
			for _, t := range allTools {
				if inst.button(t.String(), t.String(), toolTips[t], inst.tool == t) && inst.tool != t {
					inst.setTool(t)
				}
			}
		}
		if r, marked := inst.viewer.Region(); marked && inst.button("keep-region", "Keep as region", tipKeepRegion, false) {
			spec := RegionSpec{Shape: ShapeArgs{Kind: "rectangle",
				Centre: [2]float64{float64(r.Min.X+r.Max.X) / 2, float64(r.Min.Y+r.Max.Y) / 2},
				Size:   [2]float64{float64(r.Dx()), float64(r.Dy())}}}
			inst.addRegionGesture(spec)
		}
		if len(inst.polyDraft) > 0 {
			c.Label(fmt.Sprintf("polygon: %d vertices — click the first, or Enter, to close; Escape drops it", len(inst.polyDraft))).Send()
		}
	}
}

// setTool takes another tool; a polygon being drawn is dropped.
func (inst *App) setTool(t toolE) {
	inst.tool, inst.polyDraft, inst.dragPreview = t, nil, nil
}

// prepareViewer sets what the viewer needs from the regions before it is
// drawn: the overlay, the keys, and whether a drag is a gesture — always
// for the tools that drag, and with the select tool while the pointer is
// over the region selected, so a drag elsewhere pans.
func (inst *App) prepareViewer() {
	inst.viewer.Opts.Overlay = inst.paintRegions
	inst.viewer.Opts.Keys = regionKeys
	gestures := inst.tool.takesDrag()
	if inst.tool == toolSelect && inst.selRegion != "" && inst.hoverOk {
		gestures = inst.hitRegion(inst.hoverAt, inst.selRegion) == inst.selRegion
	}
	if !gestures && inst.tool == toolSelect && inst.moving {
		gestures = true
	}
	inst.viewer.Opts.DragGestures = gestures
}

// ---- drawing ----

// drawnRegion is a region as the overlay draws it, kept until its key
// changes: the region's revision, the image shown, the stack position and
// the zoom bucket, so nothing is mapped or tessellated per frame.
type drawnRegion struct {
	key    string
	d      regionPlacement
	pts    []regions.Point
	closed bool
	lo, hi regions.Point
}

// zoomBucket is the power of two at or below a magnification.
func zoomBucket(mag float64) int {
	if !(mag > 0) {
		return 0
	}
	return int(math.Floor(math.Log2(mag)))
}

// drawn is a region's drawing for this image, zoom and stack position,
// made when its key changes. An ellipse's polyline stays within a quarter
// of a screen pixel of it at every magnification of the bucket.
func (inst *App) drawn(e regionEntry, bucket int) *drawnRegion {
	stk := inst.stackShown()
	pos := ""
	if stk != nil {
		pos = stk.shown.key()
	}
	key := fmt.Sprintf("%d|%p|%s|%d", e.rev, inst.pic, pos, bucket)
	if d, ok := inst.drawCache[e.r.Id]; ok && d.key == key {
		return d
	}
	out := &drawnRegion{key: key, d: placeRegion(e.r, inst.pic, stk)}
	if out.d.Drawn {
		tol := 0.25 / math.Pow(2, float64(bucket+1))
		out.pts, out.closed, _ = regions.OutlinePolyline(out.d.Pixels, tol)
		out.lo, out.hi = out.d.Pixels.Bounds()
	}
	if inst.drawCache == nil {
		inst.drawCache = make(map[string]*drawnRegion, len(inst.regs))
	}
	inst.drawCache[e.r.Id] = out
	return out
}

// pruneDrawCache forgets the drawings of regions that are gone.
func (inst *App) pruneDrawCache() {
	for id := range inst.drawCache {
		if regionIndex(inst.regs, id) < 0 {
			delete(inst.drawCache, id)
		}
	}
}

// regionColour is the token a region is drawn in: the accent for the
// person's, the info role for any other origin; the selected one in the
// strong variant of its role.
func regionColour(origin string, selected bool) styletokens.RGBA8 {
	switch {
	case isPersonOrigin(origin) && selected:
		return styletokens.AccentStrong
	case isPersonOrigin(origin):
		return styletokens.AccentDefault
	case selected:
		return styletokens.InfoStrong
	}
	return styletokens.InfoDefault
}

// regionText is a region's label as it is drawn: a region another than the
// person made starts with ◇, beside its dashed line, so the distinction does
// not rest on colour alone.
func regionText(r regions.Region) string {
	if isPersonOrigin(r.Origin) {
		return r.Label
	}
	return "◇ " + r.Label
}

// paintRegions is the viewer's overlay: every region drawn on the image
// shown, the one being made, and the polygon being clicked. Per frame it
// projects the vertices it keeps; nothing is mapped or tessellated unless
// a key changed, and a region outside the view is skipped.
func (inst *App) paintRegions(p imageview.Projector) {
	bucket := zoomBucket(p.Magnification())
	vis := p.Visible()
	for _, e := range inst.regs {
		d := inst.drawn(e, bucket)
		if !d.d.Drawn || len(d.pts) == 0 {
			continue
		}
		if d.hi.X < float64(vis.Min.X)-1 || d.lo.X > float64(vis.Max.X)+1 || d.hi.Y < float64(vis.Min.Y)-1 || d.lo.Y > float64(vis.Max.Y)+1 {
			continue
		}
		selected := e.r.Id == inst.selRegion
		col := color.Hex(regionColour(e.r.Origin, selected).AsHex())
		stroke := styletokens.StrokeRegular
		if selected {
			stroke = styletokens.StrokeStrong
		}
		xs, ys := inst.project(p, d.pts)
		dashed := !isPersonOrigin(e.r.Origin)
		if len(xs) == 1 {
			paintPointMark(xs[0], ys[0], col, stroke)
		} else {
			paintOutline(xs, ys, d.closed, dashed, col, stroke)
		}
		if selected {
			for i := range xs {
				if i >= 64 && d.d.Pixels.Kind() == regions.ShapeKindEllipse {
					break
				}
				if d.d.Pixels.Kind() != regions.ShapeKindEllipse || i%max(1, len(xs)/8) == 0 {
					c.PaintCircleFilled(xs[i], ys[i], 3, col).Send()
				}
			}
		}
		lx, ly := p.ToCanvas(d.lo.X, d.lo.Y)
		if ly >= 14 {
			c.PaintText(lx, ly-2, 0, 2, regionText(e.r), 11, col).Send()
		} else {
			// No room above the region inside the canvas: the label goes
			// inside its top edge.
			c.PaintText(lx+2, ly+2, 0, 0, regionText(e.r), 11, col).Send()
		}
		hx, hy := p.ToCanvas(d.hi.X, d.hi.Y)
		x0, y0, x1, y1 := min(lx, hx)-6, min(ly, hy)-6, max(lx, hx)+6, max(ly, hy)+6
		if c.PaintHitRect(inst.ids.PrepareStr("region-hit-"+e.r.Id), x0, y0, x1, y1).
			Label("region "+e.r.Label).Tooltip(regionTooltip(e.r)).SendResp().HasPrimaryClicked() && inst.tool == toolSelect {
			inst.pendingSelect = e.r.Id
		}
	}
	inst.paintDraft(p)
}

// regionTooltip is what the hit region of a region says.
func regionTooltip(r regions.Region) string {
	s := r.Shape.Kind().String() + " in the " + r.Frame.String() + " frame, made by " + r.Origin
	if r.Note != "" {
		s += " — " + r.Note
	}
	return s
}

// project is a polyline in canvas coordinates, in the window's scratch.
func (inst *App) project(p imageview.Projector, pts []regions.Point) (xs, ys []float32) {
	xs, ys = inst.scratchX[:0], inst.scratchY[:0]
	for _, q := range pts {
		x, y := p.ToCanvas(q.X, q.Y)
		xs, ys = append(xs, x), append(ys, y)
	}
	inst.scratchX, inst.scratchY = xs, ys
	return
}

// paintOutline draws a polyline, closed or not, solid or dashed. A long
// edge is dashed by the painter; an outline of many short edges, an
// ellipse's, every other edge.
func paintOutline(xs, ys []float32, closed, dashed bool, col color.Color, stroke float32) {
	if !dashed {
		if closed {
			xs, ys = append(xs, xs[0]), append(ys, ys[0])
		}
		c.PaintPolyline(xs, ys, col, stroke).Send()
		return
	}
	n := len(xs)
	edges := n - 1
	if closed {
		edges = n
	}
	for i := range edges {
		j := (i + 1) % n
		if math.Hypot(float64(xs[j]-xs[i]), float64(ys[j]-ys[i])) >= 12 {
			c.PaintDashedLine(xs[i], ys[i], xs[j], ys[j], 6, 4, col, stroke).Send()
		} else if i%2 == 0 {
			c.PaintLine(xs[i], ys[i], xs[j], ys[j], col, stroke).Send()
		}
	}
}

// paintPointMark draws a point: a cross in a ring.
func paintPointMark(x, y float32, col color.Color, stroke float32) {
	c.PaintCircleStroke(x, y, 5, col, stroke).Send()
	c.PaintLine(x-8, y, x+8, y, col, stroke).Send()
	c.PaintLine(x, y-8, x, y+8, col, stroke).Send()
}

// paintDraft draws what is being made: the shape a drag would add, and the
// polygon being clicked with a line to the pointer.
func (inst *App) paintDraft(p imageview.Projector) {
	col := color.Hex(styletokens.AccentDefault.AsHex())
	if inst.dragPreview != nil {
		if pts, closed, err := regions.OutlinePolyline(*inst.dragPreview, 0.25/p.Magnification()); err == nil && len(pts) > 1 {
			xs, ys := inst.project(p, pts)
			paintOutline(xs, ys, closed, true, col, styletokens.StrokeRegular)
		}
	}
	if len(inst.polyDraft) > 0 {
		pts := append([]regions.Point(nil), inst.polyDraft...)
		if inst.hoverOk {
			pts = append(pts, regions.Point{X: inst.hoverAt.X, Y: inst.hoverAt.Y})
		}
		xs, ys := inst.project(p, pts)
		if len(xs) > 1 {
			c.PaintPolyline(xs, ys, col, styletokens.StrokeRegular).Send()
		}
		for i := range len(inst.polyDraft) {
			c.PaintCircleFilled(xs[i], ys[i], 3, col).Send()
		}
	}
}

// ---- gestures ----

// hitRegion is the region drawn on the image shown that the point lies
// on: inside an area shape, or within six screen pixels of a line or a
// point; the last one drawn wins, and prefer, when given, wins ties. Empty
// for none.
func (inst *App) hitRegion(at imageview.PixelPoint, prefer string) string {
	tol := 6 / max(inst.viewer.Magnification(), 1e-9)
	q := regions.Point{X: at.X, Y: at.Y}
	hit := ""
	bucket := zoomBucket(inst.viewer.Magnification())
	for _, e := range inst.regs {
		d := inst.drawn(e, bucket)
		if !d.d.Drawn || !onOutline(d, q, tol) {
			continue
		}
		hit = e.r.Id
		if hit == prefer {
			return hit
		}
	}
	return hit
}

// onOutline reports a point inside an area shape's outline, by the
// even-odd rule over its drawn polygon, or within tol of a line or a point.
func onOutline(d *drawnRegion, q regions.Point, tol float64) bool {
	pts := d.pts
	switch {
	case len(pts) == 1:
		return math.Hypot(q.X-pts[0].X, q.Y-pts[0].Y) <= tol
	case !d.closed:
		return segDistance(q, pts[0], pts[1]) <= tol
	}
	in := false
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		a, b := pts[i], pts[j]
		if (a.Y > q.Y) != (b.Y > q.Y) && q.X < (b.X-a.X)*(q.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

// segDistance is the distance from q to the segment a–b.
func segDistance(q, a, b regions.Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = min(max(((q.X-a.X)*dx+(q.Y-a.Y)*dy)/l2, 0), 1)
	}
	return math.Hypot(q.X-(a.X+t*dx), q.Y-(a.Y+t*dy))
}

// dragShape is the shape a drag from one point to another makes with the
// tool in hand: a line between them, or the rectangle or the ellipse whose
// box they span. ok is false for a drag that makes none: one of no extent.
func dragShape(t toolE, from, to imageview.PixelPoint) (spec ShapeArgs, ok bool) {
	cx, cy := (from.X+to.X)/2, (from.Y+to.Y)/2
	w, h := math.Abs(to.X-from.X), math.Abs(to.Y-from.Y)
	switch t {
	case toolLine:
		return ShapeArgs{Kind: "line", From: [2]float64{from.X, from.Y}, To: [2]float64{to.X, to.Y}}, w > 0 || h > 0
	case toolRectangle:
		return ShapeArgs{Kind: "rectangle", Centre: [2]float64{cx, cy}, Size: [2]float64{w, h}}, w > 0 && h > 0
	case toolEllipse:
		return ShapeArgs{Kind: "ellipse", Centre: [2]float64{cx, cy}, Radii: [2]float64{w / 2, h / 2}}, w > 0 && h > 0
	}
	return spec, false
}

// closesPolygon reports a click on the polygon's first vertex: within six
// screen pixels of it.
func (inst *App) closesPolygon(at imageview.PixelPoint) bool {
	if len(inst.polyDraft) < 3 {
		return false
	}
	f := inst.polyDraft[0]
	return math.Hypot(at.X-f.X, at.Y-f.Y)*inst.viewer.Magnification() <= 6
}

// handleViewerEvents applies what the person did on the image this frame
// with the tool in hand. Each change is the person's gesture, through the
// catalog's handler.
func (inst *App) handleViewerEvents(ev imageview.Events) {
	inst.hoverAt, inst.hoverOk = ev.HoverAt, ev.HoverOk
	inst.dragPreview = nil
	if ev.DragOk && inst.tool.takesDrag() {
		if spec, ok := dragShape(inst.tool, ev.DragFrom, ev.DragTo); ok {
			if s, err := spec.shape(); err == nil {
				inst.dragPreview = &s
			}
		}
	}
	inst.moving = ev.DragOk && inst.tool == toolSelect && inst.viewer.Opts.DragGestures
	for _, k := range ev.Keys {
		inst.regionKey(k.Code)
	}
	switch {
	case ev.DragEnded && inst.tool.takesDrag():
		if spec, ok := dragShape(inst.tool, ev.DragEndFrom, ev.DragEndTo); ok {
			inst.addRegionGesture(RegionSpec{Shape: spec})
		} else {
			inst.regionNote = "a " + inst.tool.String() + " needs an extent: drag across the image"
		}
	case ev.DragEnded && inst.tool == toolSelect && inst.selRegion != "":
		inst.moveSelected(ev.DragEndTo.X-ev.DragEndFrom.X, ev.DragEndTo.Y-ev.DragEndFrom.Y)
	case ev.ClickedOk && inst.tool == toolPoint:
		inst.addRegionGesture(RegionSpec{Shape: ShapeArgs{Kind: "point", At: [2]float64{ev.ClickedAt.X, ev.ClickedAt.Y}}})
	case ev.ClickedOk && inst.tool == toolPolygon:
		if inst.closesPolygon(ev.ClickedAt) {
			inst.closePolygon()
		} else if len(inst.polyDraft) < maxPolygonVertices {
			inst.polyDraft = append(inst.polyDraft, regions.Point{X: ev.ClickedAt.X, Y: ev.ClickedAt.Y})
		}
	case ev.ClickedOk && inst.tool == toolSelect:
		inst.selectGesture(inst.hitRegion(ev.ClickedAt, inst.selRegion))
	}
	if inst.pendingSelect != "" {
		inst.selectGesture(inst.pendingSelect)
		inst.pendingSelect = ""
	}
}

// gestureUnderWay reports a gesture the overlay follows from frame to
// frame: a move, a drag that previews its shape, a polygon being clicked.
func (inst *App) gestureUnderWay() bool {
	return inst.moving || inst.dragPreview != nil || len(inst.polyDraft) > 0
}

// regionKey applies a key the viewer handed over.
func (inst *App) regionKey(code keycodes.Code) {
	switch code {
	case keycodes.Enter:
		if inst.tool == toolPolygon {
			inst.closePolygon()
		}
	case keycodes.Escape:
		inst.polyDraft, inst.dragPreview = nil, nil
	case keycodes.Delete, keycodes.Backspace:
		if inst.selRegion != "" && len(inst.polyDraft) == 0 {
			id := inst.selRegion
			gestureOr(inst, opRemoveRegion, RegionIdArgs{Id: id}, func() { inst.regionNote = refusalText(inst.removeRegion(id)) })
		}
	}
}

// closePolygon adds the polygon being clicked, of three vertices or more.
func (inst *App) closePolygon() {
	if len(inst.polyDraft) < 3 {
		inst.regionNote = "a polygon needs three vertices or more"
		return
	}
	vs := make([][2]float64, len(inst.polyDraft))
	for i, p := range inst.polyDraft {
		vs[i] = [2]float64{p.X, p.Y}
	}
	inst.polyDraft = nil
	inst.addRegionGesture(RegionSpec{Shape: ShapeArgs{Kind: "polygon", Vertices: vs}})
}

// addRegionGesture is the person's add_region.
func (inst *App) addRegionGesture(spec RegionSpec) {
	inst.regionNote = ""
	gestureOr(inst, opAddRegion, spec, func() {
		if _, err := inst.addRegion(spec, originPerson); err != nil {
			inst.regionNote = refusalText(err)
		}
	})
}

// selectGesture is the person's select_region.
func (inst *App) selectGesture(id string) {
	if id == inst.selRegion {
		return
	}
	gestureOr(inst, opSelectRegion, RegionIdArgs{Id: id}, func() { inst.regionNote = refusalText(inst.selectRegion(id)) })
}

// updateGesture is the person's update_region.
func (inst *App) updateGesture(id string, spec RegionSpec) {
	inst.regionNote = ""
	args := UpdateRegionArgs{Id: id, Region: spec}
	gestureOr(inst, opUpdateRegion, args, func() { inst.regionNote = refusalText(inst.updateRegion(id, spec, originPerson)) })
}

// moveSelected moves the region selected by a drag of (dx, dy) pixels.
func (inst *App) moveSelected(dx, dy float64) {
	i := regionIndex(inst.regs, inst.selRegion)
	if i < 0 || inst.pic == nil || (dx == 0 && dy == 0) {
		return
	}
	r, err := movedBy(inst.regs[i].r, dx, dy, inst.pic.Placement())
	if err != nil {
		inst.regionNote = refusalText(err)
		return
	}
	inst.updateGesture(r.Id, specOf(r))
}

// ---- the pane ----

// renderRegionsPane is the regions section of the side panel: the
// selected region's fields, the measurement, the table, the profile, the
// handovers and the document.
func (inst *App) renderRegionsPane() {
	if inst.regionNote != "" {
		badge.New(inst.ids.PrepareStr("region-note"), inst.regionNote).Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
	}
	if inst.regionJob.Running() {
		bgjobrow.Render(bgjobrow.Input{Ids: inst.ids, ScopeKey: "region-measure-row", Job: &inst.regionJob, Note: "measuring regions…", Cancel: true, Inline: true})
	}
	if inst.regionJobErr != "" {
		c.Label(inst.regionJobErr).Send()
	}
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("%d regions", len(inst.regs))).Send()
		for range c.HoverText(tipMeasureArea).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("measure-area"), inst.measureArea, "Area figures").SendRespVal(&inst.measureArea)
		}
		if len(inst.regs) > 0 && inst.button("measure-all", "Measure all", tipRegionFigures, false) && !inst.regionJob.Running() {
			inst.measureGesture("")
		}
	}
	if i := regionIndex(inst.regs, inst.selRegion); i >= 0 {
		inst.renderRegionEditor(inst.regs[i])
	} else if len(inst.regs) == 0 {
		c.Label("No regions. Pick a tool above the image and draw one, or keep the marked rectangle as one.").Send()
	} else {
		c.Label("No region is selected. Click one with the select tool, or a row below.").Send()
	}
	inst.renderRegionsTable()
	inst.renderProfile()
	inst.renderRegionsHandover()
}

// regionEdit is what the fields of the selected region hold: seeded from
// the region when it or its revision changes, applied by Apply.
type regionEdit struct {
	id    string
	rev   uint64
	label string
	note  string
	nums  []string
	poly  string
}

// numberNames are the fields of a kind's numbers, in the order ShapeArgs
// takes them.
func numberNames(kind regions.ShapeKindE) []string {
	switch kind {
	case regions.ShapeKindPoint:
		return []string{"x", "y"}
	case regions.ShapeKindLine:
		return []string{"x₁", "y₁", "x₂", "y₂"}
	case regions.ShapeKindRectangle:
		return []string{"x", "y", "w", "h", "°"}
	case regions.ShapeKindEllipse:
		return []string{"x", "y", "rx", "ry", "°"}
	}
	return nil
}

func numbersOf(a ShapeArgs) []float64 {
	switch a.Kind {
	case "point":
		return a.At[:]
	case "line":
		return []float64{a.From[0], a.From[1], a.To[0], a.To[1]}
	case "rectangle":
		return []float64{a.Centre[0], a.Centre[1], a.Size[0], a.Size[1], a.Angle}
	case "ellipse":
		return []float64{a.Centre[0], a.Centre[1], a.Radii[0], a.Radii[1], a.Angle}
	}
	return nil
}

// shapeFromNumbers is a shape of a kind from its numbers, as the fields
// hold them.
func shapeFromNumbers(kind string, v []float64, poly string) (a ShapeArgs, err error) {
	a.Kind = kind
	switch kind {
	case "point":
		a.At = [2]float64{v[0], v[1]}
	case "line":
		a.From, a.To = [2]float64{v[0], v[1]}, [2]float64{v[2], v[3]}
	case "rectangle":
		a.Centre, a.Size, a.Angle = [2]float64{v[0], v[1]}, [2]float64{v[2], v[3]}, v[4]
	case "ellipse":
		a.Centre, a.Radii, a.Angle = [2]float64{v[0], v[1]}, [2]float64{v[2], v[3]}, v[4]
	case "polygon":
		for _, pair := range strings.Split(poly, ";") {
			f := strings.Fields(strings.ReplaceAll(pair, ",", " "))
			if len(f) == 0 {
				continue
			}
			x, ex := strconv.ParseFloat(f[0], 64)
			y, ey := strconv.ParseFloat(f[len(f)-1], 64)
			if len(f) != 2 || ex != nil || ey != nil {
				return a, app.RefuseOperation("a vertex is two numbers: " + strconv.Quote(strings.TrimSpace(pair)))
			}
			a.Vertices = append(a.Vertices, [2]float64{x, y})
		}
	}
	return a, nil
}

// polygonText writes a polygon's vertices as the field holds them.
func polygonText(vs [][2]float64) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = formatNumber(v[0]) + " " + formatNumber(v[1])
	}
	return strings.Join(parts, "; ")
}

// formatNumber writes a number of a region's parameters exactly.
func formatNumber(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// renderRegionEditor is the fields of the region selected.
func (inst *App) renderRegionEditor(e regionEntry) {
	r := e.r
	a := shapeArgsOf(r.Shape)
	if inst.edit.id != r.Id || inst.edit.rev != e.rev {
		inst.edit = regionEdit{id: r.Id, rev: e.rev, label: r.Label, note: r.Note}
		for _, v := range numbersOf(a) {
			inst.edit.nums = append(inst.edit.nums, formatNumber(v))
		}
		inst.edit.poly = polygonText(a.Vertices)
	}
	for range c.IdScope(inst.ids.PrepareStr("region-edit")) {
		metric(fmt.Sprintf("Selected: %s · %s · %s frame · %s · made by %s", r.Label, r.Shape.Kind(), r.Frame, anchorText(r.Anchor), r.Origin), tipRegionOrigin)
		if d := placeRegion(r, inst.pic, inst.stackShown()); !d.Drawn {
			badge.New(inst.ids.PrepareStr("not-drawn"), "not drawn: "+d.Reason).Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
		}
		for range c.Horizontal().KeepIter() {
			c.TextEdit(inst.ids.PrepareStr("label"), inst.edit.label, false).AccessibleName("Region label").HintText("label").DesiredWidth(140).SendRespVal(&inst.edit.label)
			c.TextEdit(inst.ids.PrepareStr("note"), inst.edit.note, false).AccessibleName("Region note").HintText("note").DesiredWidth(160).SendRespVal(&inst.edit.note)
		}
		names := numberNames(r.Shape.Kind())
		if len(inst.edit.nums) == len(names) {
			for range c.Horizontal().KeepIter() {
				for k, name := range names {
					c.Label(name).Selectable(false).Send()
					c.TextEdit(inst.ids.PrepareSeq(uint64(k)), inst.edit.nums[k], false).AccessibleName("Region " + name).HintText(name).DesiredWidth(48).SendRespVal(&inst.edit.nums[k])
				}
			}
		}
		if r.Shape.Kind() == regions.ShapeKindPolygon {
			c.TextEdit(inst.ids.PrepareStr("poly"), inst.edit.poly, true).AccessibleName("Region vertices").HintText("x y; x y; x y").DesiredWidth(300).SendRespVal(&inst.edit.poly)
		}
		for range c.Horizontal().KeepIter() {
			if c.Button(inst.ids.PrepareStr("apply"), c.Atoms().Text("Apply").Keep()).SendResp().HasPrimaryClicked() {
				inst.applyEdit(e)
			}
			if c.Button(inst.ids.PrepareStr("remove"), c.Atoms().Text("Remove").Keep()).Kind(c.ButtonKindDanger).SendResp().HasPrimaryClicked() {
				gestureOr(inst, opRemoveRegion, RegionIdArgs{Id: r.Id}, func() { inst.regionNote = refusalText(inst.removeRegion(r.Id)) })
			}
			if inst.button("measure", "Measure", tipRegionFigures, false) && !inst.regionJob.Running() {
				inst.measureGesture(r.Id)
			}
			if r.Shape.Kind() == regions.ShapeKindLine && inst.button("profile", "Profile", tipProfile, false) && !inst.profileJob.Running() {
				inst.profileGesture(r.Id)
			}
		}
		inst.renderFrameAnchorRow(r)
		inst.renderSelectedFigures(e)
	}
}

// applyEdit sends the fields as update_region.
func (inst *App) applyEdit(e regionEntry) {
	vals := make([]float64, len(inst.edit.nums))
	for k, s := range inst.edit.nums {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			inst.regionNote = "field " + numberNames(e.r.Shape.Kind())[k] + " is not a number: " + strconv.Quote(s)
			return
		}
		vals[k] = v
	}
	a, err := shapeFromNumbers(e.r.Shape.Kind().String(), vals, inst.edit.poly)
	if err != nil {
		inst.regionNote = refusalText(err)
		return
	}
	spec := specOf(e.r)
	spec.Label, spec.Note, spec.Shape = inst.edit.label, inst.edit.note, a
	inst.updateGesture(e.r.Id, spec)
}

// Hover help of the frame and anchor buttons.
const (
	tipFramePixel = "State the region in pixel coordinates: it then applies to every image of the window, whatever its placement. The numbers are converted exactly, or the switch is refused naming why."
	tipFrameImage = "State the region in the frame of the image shown's placement: it then applies to every image placed in that frame. The numbers are converted exactly, or the switch is refused naming why."
	tipAnchorAll  = "The region belongs to every position of a stack."
	tipAnchorHere = "The region belongs to the stack position shown only: it is drawn, measured and listed as applying there and nowhere else."
)

// renderFrameAnchorRow is the frame and the anchor of the region selected,
// each set by a button.
func (inst *App) renderFrameAnchorRow(r regions.Region) {
	for range c.Horizontal().KeepIter() {
		c.Label("Frame").Selectable(false).Send()
		if inst.button("frame-pixel", "pixel", tipFramePixel, r.Frame.Kind == regions.FrameKindPixel) && r.Frame.Kind != regions.FrameKindPixel {
			inst.frameGesture(r, FrameArgs{Kind: "pixel"})
		}
		if inst.pic != nil {
			if f, ok := regions.FrameOf(inst.pic.Placement()); ok {
				if inst.button("frame-image", "image: "+f.String(), tipFrameImage, r.Frame == f) && r.Frame != f {
					inst.frameGesture(r, FrameArgs{Kind: frameImage})
				}
			}
		}
		if inst.stackShown() != nil || !r.Anchor.IsEmpty() {
			c.Label("Anchor").Selectable(false).Send()
			if inst.button("anchor-all", "every position", tipAnchorAll, r.Anchor.IsEmpty()) && !r.Anchor.IsEmpty() {
				spec := specOf(r)
				spec.Anchor = nil
				inst.updateGesture(r.Id, spec)
			}
			if inst.stackShown() != nil && inst.button("anchor-here", "position shown", tipAnchorHere, false) {
				spec := specOf(r)
				spec.Anchor, spec.AnchorShown = nil, true
				inst.updateGesture(r.Id, spec)
			}
		}
	}
}

// frameGesture converts the region selected to another frame and sends it
// as update_region; a conversion that cannot be exact is refused here and
// changes nothing.
func (inst *App) frameGesture(r regions.Region, to FrameArgs) {
	f, err := to.frame(inst.pic)
	if err != nil {
		inst.regionNote = refusalText(err)
		return
	}
	out, err := convertFrame(r, f, inst.pic.Placement())
	if err != nil {
		inst.regionNote = refusalText(err)
		return
	}
	inst.updateGesture(r.Id, specOf(out))
}

// measureGesture is the person's measure_region, or measure_regions for
// the empty id.
func (inst *App) measureGesture(id string) {
	inst.regionNote = ""
	args := MeasureRegionArgs{Id: id, Area: inst.measureArea}
	if id == "" {
		all := MeasureRegionsArgs{Area: inst.measureArea}
		gestureOr(inst, opMeasureRegions, all, func() { inst.regionNote = refusalText(inst.startRegionMeasure(nil, all.Area)) })
		return
	}
	gestureOr(inst, opMeasureRegion, args, func() { inst.regionNote = refusalText(inst.startRegionMeasure([]string{id}, args.Area)) })
}

// profileGesture is the person's profile of a line: computed as a task and
// drawn as a chart; profile_region reads it.
func (inst *App) profileGesture(id string) {
	inst.regionNote = refusalText(inst.startProfile(id))
}

// renderSelectedFigures states the figures of the region selected, with
// what they were computed on, or that they are stale.
func (inst *App) renderSelectedFigures(e regionEntry) {
	m, stale := inst.measureState(e)
	if m == nil {
		return
	}
	if m.err != nil {
		badge.New(inst.ids.PrepareStr("figures-err"), "not measured: "+refusalText(m.err)).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
		return
	}
	line := fmt.Sprintf("%s · %s · %d pixels by %s · on %s", m.m.Method, m.m.Mapping.How, m.m.Pixels, m.m.Rule, m.imageId)
	if m.selection != nil {
		line += " · " + m.selection.Text
	}
	if stale {
		line = "stale — " + line
	}
	metric(line, tipRegionFigures)
	for _, p := range m.m.Planes {
		s := fmt.Sprintf("%s: valid %d, invalid %d, mean %s, sd %s", p.Label, p.Valid, p.Invalid, formatFigure(p.Mean), formatFigure(p.StdDev))
		if p.Area != nil {
			s += fmt.Sprintf(" · by area: %s px², mean %s", formatFigure(p.Area.Area), formatFigure(p.Area.Mean))
		}
		metric(s, tipRegionFigures)
	}
}

// ---- the table ----

// regionColumns are the table's headers; each row is a region on a plane
// of the image shown, or a region not measured.
var regionColumns = []string{"region", "shape", "origin", "frame", "anchor", "shown", "plane", "pixels", "valid", "mean", "sd", "median", "figures"}

const (
	tipColShown   = "Whether the region is drawn on the image shown: it applies at the stack position shown and reaches the image's pixels. Otherwise the reason."
	tipColFigures = "current: of the region and the image as they stand; stale: computed before the region, the image or its selection changed; area: the area figures were asked for too."
)

// regionRows are the table's rows and, for each, the region it is of.
func (inst *App) regionRows() (rows [][]string, ids []string) {
	stk := inst.stackShown()
	for _, e := range inst.regs {
		r := e.r
		d := placeRegion(r, inst.pic, stk)
		shown := "yes"
		if !d.Drawn {
			shown = "no: " + d.Reason
		}
		base := []string{regionText(r), r.Shape.Kind().String(), r.Origin, r.Frame.String(), anchorText(r.Anchor), shown}
		m, stale := inst.measureState(e)
		if m == nil || m.err != nil || len(m.m.Planes) == 0 {
			state := ""
			if m != nil && m.err != nil {
				state = "failed: " + refusalText(m.err)
			}
			rows = append(rows, append(base, "", "", "", "", "", "", state))
			ids = append(ids, r.Id)
			continue
		}
		state := "current"
		if stale {
			state = "stale"
		}
		if m.area {
			state += ", area"
		}
		for _, p := range m.m.Planes {
			median := math.NaN()
			for _, pc := range p.Percentiles {
				if pc.P == 50 {
					median = pc.Value
				}
			}
			rows = append(rows, append(slicesClone(base), p.Label, strconv.FormatInt(m.m.Pixels, 10), strconv.FormatUint(p.Valid, 10),
				formatFigure(p.Mean), formatFigure(p.StdDev), formatFigure(median), state))
			ids = append(ids, r.Id)
		}
	}
	return
}

func slicesClone(s []string) []string { return append([]string(nil), s...) }

// renderRegionsTable draws the regions as an Arrow record (D1), rebuilt
// when the regions, the image shown or a measurement change, never per
// frame. A click on a row selects its region.
func (inst *App) renderRegionsTable() {
	if len(inst.regs) == 0 {
		return
	}
	key := fmt.Sprintf("%d|%p|%s|%d|%s", inst.regRev, inst.pic, inst.shownId, inst.regionLanded, inst.stackKey())
	if inst.regTable == nil || inst.regTableKey != key {
		if inst.regTable != nil {
			inst.regTable.Release()
		}
		rows, ids := inst.regionRows()
		inst.regTable, inst.regTableIds, inst.regTableKey = recordtable.StringRecord(regionColumns, rows), ids, key
	}
	if inst.regCols == nil {
		inst.regCols = make([]recordtable.Column, len(regionColumns))
		for i, h := range regionColumns {
			inst.regCols[i] = recordtable.Column{Arrow: i, Header: h, NoSort: true}
		}
		inst.regCols[2].Hover = tipRegionOrigin
		inst.regCols[5].Hover = tipColShown
		inst.regCols[12].Hover = tipColFigures
		for _, k := range []int{7, 8, 9, 10, 11} {
			inst.regCols[k].Hover = tipRegionFigures
		}
	}
	sel := int64(-1)
	for i, id := range inst.regTableIds {
		if id == inst.selRegion {
			sel = int64(i)
			break
		}
	}
	res := recordtable.RenderGrid(recordtable.Input{
		Ids: inst.ids, ScopeKey: "regions", Record: inst.regTable, Columns: inst.regCols,
		Selected: sel, HasSelection: sel >= 0, State: &inst.regGrid, NoRowNumbers: true, MaxHeight: 220,
	})
	if res.Clicked >= 0 && int(res.Clicked) < len(inst.regTableIds) {
		inst.selectGesture(inst.regTableIds[res.Clicked])
	}
}

// stackKey names the stack position shown, empty for none.
func (inst *App) stackKey() string {
	if stk := inst.stackShown(); stk != nil {
		return stk.shown.key()
	}
	return ""
}

// ---- the profile ----

const profileChartHeight = 180

// renderProfile draws the last profile: a chart of value against distance
// along the line, one series per plane, a point per pixel — nothing joins
// them, since nothing was measured between them — and its rows as a table
// under a fold.
func (inst *App) renderProfile() {
	if inst.profileJob.Running() {
		bgjobrow.Render(bgjobrow.Input{Ids: inst.ids, ScopeKey: "profile-row", Job: &inst.profileJob, Note: "profiling…", Cancel: true, Inline: true})
	}
	if inst.profileErr != "" {
		c.Label(inst.profileErr).Send()
	}
	rp := inst.profile
	if rp == nil {
		return
	}
	label := rp.regionId
	if i := regionIndex(inst.regs, rp.regionId); i >= 0 {
		label = inst.regs[i].r.Label
	}
	line := fmt.Sprintf("Profile of %s · %d pixels over %s px · on %s", label, rp.p.Len(), formatFigure(rp.p.Length), rp.imageId)
	if rp.selection != nil {
		line += " · " + rp.selection.Text
	}
	if inst.profileStale(rp) {
		line = "stale — " + line
	}
	metric(line, tipProfile)
	w := float32(340)
	for p := range implot.Scoped(inst.ids, "##region-profile", w, profileChartHeight) {
		p.SetupAxes("distance along the line, px", "value", implot.AxisFlagsNone, implot.AxisFlagsNone)
		for k, name := range rp.p.Labels {
			p.Scatter(name, rp.xs[k], rp.ys[k], implot.MarkerCircle, 2)
		}
	}
	for range c.CollapsingHeader(inst.ids.PrepareStr("profile-table"), c.WidgetText().Text("Profile as a table").Keep()).KeepIter() {
		if inst.profileTable == nil || inst.profileTableFor != rp {
			if inst.profileTable != nil {
				inst.profileTable.Release()
			}
			inst.profileTable, inst.profileTableFor = profileRecord(rp), rp
		}
		if inst.profileCols == nil {
			hdr := profileHeaders(rp)
			inst.profileCols = make([]recordtable.Column, len(hdr))
			for i, h := range hdr {
				inst.profileCols[i] = recordtable.Column{Arrow: i, Header: h, NoSort: true}
			}
		}
		if len(inst.profileCols) == int(inst.profileTable.NumCols()) {
			recordtable.RenderGrid(recordtable.Input{
				Ids: inst.ids, ScopeKey: "profile-rows", Record: inst.profileTable, Columns: inst.profileCols,
				Selected: -1, State: &inst.profileGrid, NoRowNumbers: true, MaxHeight: 200,
			})
		} else {
			inst.profileCols = nil
		}
	}
}
