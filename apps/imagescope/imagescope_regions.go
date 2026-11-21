package imagescope

// Regions in the window (ADR-0399 §M2): shapes in a stated frame, with an
// anchor and an identity, that the person draws on the image and an agent
// adds through the catalog. They belong to the window, not to one image:
// a region in pixel coordinates applies to every image of the list, one in
// a placement's frame to every image placed in that frame, one anchored
// to positions of a stack to those positions of the stack opened. The
// rectangle marked with Select region is not one of them: it stays the
// transient selection that measure and send_to_play read, and Keep as a
// region turns it into one.

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/regions"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

const (
	// originPerson is the origin of a region the person made: by a
	// gesture, a field or a document they loaded.
	originPerson = opwire.WriterPerson
	// originAgent is the origin of a region an agent made whose call names
	// no writer; an agent's call through the host carries its task's.
	originAgent = "agent"
	// originDocument is the origin of a region read from a document that
	// named none.
	originDocument = "document"

	// maxRegions bounds the regions a window holds: a frame draws each, and
	// a handover measures each.
	maxRegions = 256
	// regionsPageMax bounds a page of list_regions.
	regionsPageMax = 64
	// maxRegionsDocumentBytes bounds the regions document an operation
	// carries either way.
	maxRegionsDocumentBytes = 1 << 20
	// maxPolygonVertices bounds a polygon a person or an agent makes here;
	// a document read may hold larger ones, up to the document's bound.
	maxPolygonVertices = 4096
)

// regionEntry is a region of the window with its revision, which moves on
// every change to it. It is never modified once made.
type regionEntry struct {
	r   regions.Region
	rev uint64
}

// originOf is the origin a region made by a call gets: the person for the
// person's gesture, the caller's identity otherwise — "task:<id>" for an
// agent's task, as the host names it. The caller does not choose it.
func originOf(call app.OperationCall) string {
	switch call.Writer {
	case opwire.WriterPerson:
		return originPerson
	case "":
		return originAgent
	}
	return call.Writer
}

// isPersonOrigin reports a region the person made.
func isPersonOrigin(origin string) bool { return origin == originPerson }

// ShapeArgs is a shape by its parameters, as the region document holds it
// (ADR-0399 §SD1). Only the members of its kind are read.
type ShapeArgs struct {
	Kind     string       `desc:"point, line, rectangle, ellipse or polygon"`
	At       [2]float64   `json:",omitzero" desc:"a point: its position"`
	From     [2]float64   `json:",omitzero" desc:"a line: its start"`
	To       [2]float64   `json:",omitzero" desc:"a line: its end"`
	Centre   [2]float64   `json:",omitzero" desc:"a rectangle's or an ellipse's centre"`
	Size     [2]float64   `json:",omitzero" desc:"a rectangle's width and height, along its own axes"`
	Radii    [2]float64   `json:",omitzero" desc:"an ellipse's radii along its own x and y axes; a circle has two equal ones"`
	Angle    float64      `json:",omitzero" desc:"a rectangle's or an ellipse's rotation in degrees; positive turns +x toward +y, which in pixel coordinates is clockwise on screen"`
	Vertices [][2]float64 `json:",omitzero" desc:"a polygon's ring, at least three vertices, closed from the last back to the first; its inside is by the even-odd rule"`
}

// shapeArgsOf is a shape as ShapeArgs.
func shapeArgsOf(s regions.Shape) (out ShapeArgs) {
	out.Kind = s.Kind().String()
	pair := func(p regions.Point) [2]float64 { return [2]float64{p.X, p.Y} }
	switch s.Kind() {
	case regions.ShapeKindPoint:
		out.At = pair(s.At())
	case regions.ShapeKindLine:
		a, b := s.Ends()
		out.From, out.To = pair(a), pair(b)
	case regions.ShapeKindRectangle:
		w, h := s.Size()
		out.Centre, out.Size, out.Angle = pair(s.Centre()), [2]float64{w, h}, s.Angle()
	case regions.ShapeKindEllipse:
		rx, ry := s.Radii()
		out.Centre, out.Radii, out.Angle = pair(s.Centre()), [2]float64{rx, ry}, s.Angle()
	case regions.ShapeKindPolygon:
		for _, v := range s.Vertices() {
			out.Vertices = append(out.Vertices, pair(v))
		}
	}
	return
}

// shape is the shape ShapeArgs describe; a refusal names what is wrong.
func (inst ShapeArgs) shape() (s regions.Shape, err error) {
	pt := func(p [2]float64) regions.Point { return regions.Point{X: p[0], Y: p[1]} }
	switch inst.Kind {
	case "point":
		s, err = regions.NewPoint(pt(inst.At))
	case "line":
		s, err = regions.NewLine(pt(inst.From), pt(inst.To))
	case "rectangle":
		s, err = regions.NewRectangle(pt(inst.Centre), inst.Size[0], inst.Size[1], inst.Angle)
	case "ellipse":
		s, err = regions.NewEllipse(pt(inst.Centre), inst.Radii[0], inst.Radii[1], inst.Angle)
	case "polygon":
		if len(inst.Vertices) > maxPolygonVertices {
			return s, app.RefuseOperation(fmt.Sprintf("a polygon of %d vertices; at most %d are taken here", len(inst.Vertices), maxPolygonVertices))
		}
		vs := make([]regions.Point, len(inst.Vertices))
		for i, v := range inst.Vertices {
			vs[i] = pt(v)
		}
		s, err = regions.NewPolygon(vs)
	default:
		return s, app.RefuseOperation("no shape kind " + strconv.Quote(inst.Kind) + "; the kinds are point, line, rectangle, ellipse and polygon")
	}
	if err != nil {
		return s, app.RefuseOperation("the " + inst.Kind + " is not one: " + refusalText(err) + " (sizes and radii are positive; a polygon has three vertices or more and an area)")
	}
	return s, nil
}

// FrameArgs names a region's frame (ADR-0399 §SD2).
type FrameArgs struct {
	Kind  string    `json:",omitzero" desc:"pixel (the default: continuous pixel coordinates, pixel (col, row) covers [col, col+1) × [row, row+1)), image (the frame of the image shown's placement), or physical, reference-system or celestial named by Code or Units, as get_region reports a frame"`
	Code  string    `json:",omitzero" desc:"a reference system's or a celestial system's code, as the placement names it: EPSG:2056, ICRS"`
	Units [2]string `json:",omitzero" desc:"a physical frame's two units"`
}

const frameImage = "image"

// frameArgsOf is a frame as FrameArgs.
func frameArgsOf(f regions.Frame) FrameArgs {
	return FrameArgs{Kind: f.Kind.String(), Code: f.Code, Units: f.Units}
}

// frame is the frame the arguments name; image is the frame of the image
// shown's placement, refused for an image without one.
func (inst FrameArgs) frame(pic *tiles.Store) (f regions.Frame, err error) {
	switch inst.Kind {
	case "", "pixel":
		return regions.PixelFrame(), nil
	case frameImage:
		if pic == nil {
			return f, errNoImage
		}
		f, ok := regions.FrameOf(pic.Placement())
		if !ok {
			return f, app.RefuseOperation("the image shown has no placement, so it has no frame of its own; use the pixel frame")
		}
		return f, nil
	}
	for _, k := range regions.AllFrameKinds {
		if k.String() == inst.Kind {
			f = regions.Frame{Kind: k, Code: inst.Code, Units: inst.Units}
			if err = f.Validate(); err != nil {
				return f, app.RefuseOperation("the frame is not one: " + refusalText(err))
			}
			return f, nil
		}
	}
	return f, app.RefuseOperation("no frame " + strconv.Quote(inst.Kind) + "; the frames are pixel, image, physical, reference-system and celestial")
}

// AnchorPosition is one position of a region's anchor.
type AnchorPosition struct {
	Axis     string `desc:"a selector axis of the stack, by name"`
	Position int    `json:",omitzero" desc:"a position on it, 0 to its size - 1"`
}

// RegionSpec is a region as add_region and update_region take it.
type RegionSpec struct {
	Label string    `json:",omitzero" desc:"the region's name for a person; empty takes a numbered one"`
	Note  string    `json:",omitzero" desc:"free text about the region"`
	Shape ShapeArgs `desc:"the shape, by its parameters, in the frame's coordinates"`
	Frame FrameArgs `json:",omitzero" desc:"the frame the shape's numbers are in; empty is pixel"`
	// AnchorShown and Anchor say which positions of a stack the region
	// belongs to.
	AnchorShown bool             `json:",omitzero" desc:"anchor the region to the stack position shown; Anchor is then not read"`
	Anchor      []AnchorPosition `json:",omitzero" desc:"the positions the region belongs to, by selector axis; several on one axis are alternatives; empty with AnchorShown false is every position"`
}

// regionOf is the region a spec describes, with the id and origin given; a
// refusal names what is wrong.
func (inst *App) regionOf(spec RegionSpec, id, origin string) (r regions.Region, err error) {
	s, err := spec.Shape.shape()
	if err != nil {
		return r, err
	}
	f, err := spec.Frame.frame(inst.pic)
	if err != nil {
		return r, err
	}
	var anchor regions.Anchor
	switch {
	case spec.AnchorShown:
		if anchor, err = inst.anchorShown(); err != nil {
			return r, err
		}
	case len(spec.Anchor) > 0:
		ps := make([]regions.AxisPosition, len(spec.Anchor))
		for i, a := range spec.Anchor {
			ps[i] = regions.AxisPosition{Axis: a.Axis, Position: a.Position}
		}
		if anchor, err = regions.NewAnchor(ps...); err != nil {
			return r, app.RefuseOperation("the anchor is not one: " + refusalText(err))
		}
	}
	label := strings.TrimSpace(spec.Label)
	if label == "" {
		label = inst.nextRegionLabel(s.Kind())
	}
	r = regions.Region{Id: id, Label: label, Note: spec.Note, Origin: origin, Frame: f, Anchor: anchor, Shape: s}
	if err = r.Validate(); err != nil {
		return r, app.RefuseOperation("the region is not one: " + refusalText(err))
	}
	return r, nil
}

// anchorShown is the anchor of the stack position shown: one position per
// selector axis.
func (inst *App) anchorShown() (a regions.Anchor, err error) {
	stk := inst.stackShown()
	if stk == nil {
		return a, app.RefuseOperation("the image shown is no stack's selection, so there is no position to anchor to; leave the anchor empty for every position")
	}
	ps := make([]regions.AxisPosition, 0, len(stk.axes))
	for i, ax := range stk.axes {
		if ax.Role == imaging.AxisRoleSelector {
			ps = append(ps, regions.AxisPosition{Axis: ax.Name, Position: stk.shown.at[i]})
		}
	}
	return regions.NewAnchor(ps...)
}

// nextRegionLabel is a numbered label for a region that is given none.
func (inst *App) nextRegionLabel(kind regions.ShapeKindE) string {
	inst.regLabels++
	return kind.String() + " " + strconv.Itoa(inst.regLabels)
}

// regionIndex is the index of the region of that id, -1 when there is none.
func regionIndex(regs []regionEntry, id string) int {
	return slices.IndexFunc(regs, func(e regionEntry) bool { return e.r.Id == id })
}

// errNoRegion is the refusal of an id that names no region.
func errNoRegion(id string) error {
	return app.RefuseOperation("no region " + strconv.Quote(id) + "; list_regions lists them")
}

// touchRegions marks the set changed: the table, the resource and the
// drawing follow it.
func (inst *App) touchRegions() { inst.regRev++ }

// addRegion adds a region made from a spec by origin, and selects it.
func (inst *App) addRegion(spec RegionSpec, origin string) (id string, err error) {
	if len(inst.regs) >= maxRegions {
		return "", app.RefuseOperation(fmt.Sprintf("the window holds %d regions, the most it holds; remove one with remove_region", maxRegions))
	}
	if id, err = regions.NewId(); err != nil {
		return "", err
	}
	r, err := inst.regionOf(spec, id, origin)
	if err != nil {
		return "", err
	}
	inst.touchRegions()
	inst.regs = append(slices.Clone(inst.regs), regionEntry{r: r, rev: inst.regRev})
	inst.selRegion = id
	return id, nil
}

// updateRegion replaces a region's label, note, shape, frame and anchor —
// the whole of it, as set_recipe sets a recipe — keeping its id. The
// origin becomes that of the caller who changed it.
func (inst *App) updateRegion(id string, spec RegionSpec, origin string) error {
	i := regionIndex(inst.regs, id)
	if i < 0 {
		return errNoRegion(id)
	}
	if strings.TrimSpace(spec.Label) == "" {
		spec.Label = inst.regs[i].r.Label
	}
	r, err := inst.regionOf(spec, id, origin)
	if err != nil {
		return err
	}
	return inst.replaceRegion(i, r)
}

// replaceRegion puts r in place of region i; the same region again is no
// change.
func (inst *App) replaceRegion(i int, r regions.Region) error {
	if sameRegion(inst.regs[i].r, r) {
		return nil
	}
	inst.touchRegions()
	regs := slices.Clone(inst.regs)
	regs[i] = regionEntry{r: r, rev: inst.regRev}
	inst.regs = regs
	return nil
}

// sameRegion compares two regions by their documents.
func sameRegion(a, b regions.Region) bool {
	da, ea := regions.MarshalRegion(a)
	db, eb := regions.MarshalRegion(b)
	return ea == nil && eb == nil && string(da) == string(db)
}

// removeRegion removes a region and what was computed of it.
func (inst *App) removeRegion(id string) error {
	i := regionIndex(inst.regs, id)
	if i < 0 {
		return errNoRegion(id)
	}
	inst.touchRegions()
	inst.regs = slices.Delete(slices.Clone(inst.regs), i, i+1)
	delete(inst.regMeasures, id)
	if inst.profile != nil && inst.profile.regionId == id {
		inst.profile = nil
	}
	if inst.selRegion == id {
		inst.selRegion = ""
	}
	return nil
}

// selectRegion selects a region, or none for the empty id.
func (inst *App) selectRegion(id string) error {
	if id != "" && regionIndex(inst.regs, id) < 0 {
		return errNoRegion(id)
	}
	inst.selRegion = id
	return nil
}

// regionsDocument is the window's regions as the versioned document of
// ADR-0399 §SD6.
func regionsDocument(regs []regionEntry) ([]byte, error) {
	rs := make([]regions.Region, len(regs))
	for i, e := range regs {
		rs[i] = e.r
	}
	return regions.MarshalRegions(rs)
}

// setRegionsDocument replaces the window's regions with a document's. The
// person's document keeps the origins it records — a region with none is
// the document's; a document an agent sets takes the agent's origin for
// every region, so an agent cannot name its regions the person's.
func (inst *App) setRegionsDocument(doc []byte, origin string) error {
	if len(doc) > maxRegionsDocumentBytes {
		return app.RefuseOperation(fmt.Sprintf("the document is %d bytes; at most %d are read", len(doc), maxRegionsDocumentBytes))
	}
	rs, err := regions.UnmarshalRegions(doc)
	if err != nil {
		return app.RefuseOperation("the document is not a regions document of version " + strconv.Itoa(regions.DocumentVersion) + ": " + refusalText(err))
	}
	if len(rs) > maxRegions {
		return app.RefuseOperation(fmt.Sprintf("the document holds %d regions; a window holds at most %d", len(rs), maxRegions))
	}
	inst.touchRegions()
	regs := make([]regionEntry, len(rs))
	for i, r := range rs {
		switch {
		case !isPersonOrigin(origin):
			r.Origin = origin
		case r.Origin == "":
			r.Origin = originDocument
		}
		regs[i] = regionEntry{r: r, rev: inst.regRev}
	}
	inst.regs = regs
	inst.selRegion = ""
	clear(inst.regMeasures)
	inst.profile = nil
	return nil
}

// regionPlacement is what the window makes of a region over the image
// shown (ADR-0399 §SD2, §SD5): whether it applies at the stack position
// shown, whether it reached the image's pixels, how, and why not.
type regionPlacement struct {
	// Applies is true when the region belongs to the image shown: every
	// position, or the anchor names the position shown.
	Applies bool
	// Drawn is true when it applies and reached the image's pixels: it is
	// drawn and can be measured.
	Drawn bool
	// Reason says why it does not apply or was not drawn.
	Reason  string
	Pixels  regions.Shape
	Mapping regions.Mapping
}

// placeRegion decides a region over an image: its anchor against the
// stack position shown — stk is nil for an image that is no stack's
// selection — and its frame against the image's placement.
func placeRegion(r regions.Region, pic *tiles.Store, stk *stackView) (d regionPlacement) {
	if pic == nil {
		d.Reason = "no image is shown"
		return
	}
	switch {
	case r.Anchor.IsEmpty():
		d.Applies = true
	case stk == nil:
		d.Reason = "anchored to positions of a stack; the image shown is no stack's selection"
		return
	default:
		applies, err := r.Anchor.AppliesAt(stk.axes, stk.shown.at)
		if err != nil {
			d.Reason = "anchored to positions this stack does not have: " + refusalText(err)
			return
		}
		if !applies {
			d.Reason = "anchored to other positions of the stack: " + anchorText(r.Anchor)
			return
		}
		d.Applies = true
	}
	p := pic.Placement()
	if !r.Frame.Matches(p) {
		if f, ok := regions.FrameOf(p); ok {
			d.Reason = "in the " + r.Frame.String() + " frame; the image shown is in the " + f.String() + " frame"
		} else {
			d.Reason = "in the " + r.Frame.String() + " frame; the image shown has no placement"
		}
		return
	}
	px, m, err := r.ToPixels(p, 0)
	if err != nil {
		d.Reason = "does not reach the image's pixels: " + refusalText(err)
		return
	}
	d.Drawn, d.Pixels, d.Mapping = true, px, m
	return
}

// anchorText writes an anchor for a person: "every position", or the
// positions by axis.
func anchorText(a regions.Anchor) string {
	if a.IsEmpty() {
		return "every position"
	}
	var parts []string
	for p := range a.All() {
		parts = append(parts, p.Axis+" "+strconv.Itoa(p.Position))
	}
	return strings.Join(parts, ", ")
}

// anchorPositions is an anchor as the catalog reports it.
func anchorPositions(a regions.Anchor) (out []AnchorPosition) {
	for p := range a.All() {
		out = append(out, AnchorPosition{Axis: p.Axis, Position: p.Position})
	}
	return
}

// specOf is a region as the spec that would make it again: what
// update_region takes back.
func specOf(r regions.Region) RegionSpec {
	return RegionSpec{Label: r.Label, Note: r.Note, Shape: shapeArgsOf(r.Shape), Frame: frameArgsOf(r.Frame), Anchor: anchorPositions(r.Anchor)}
}

// convertFrame brings a region to another frame without changing what it
// covers: from the image's frame to pixels through ToPixels, from pixels
// to the image's frame through FromPixels. It is refused where the shape
// would not stay what it is — a rectangle that becomes a polygon, an
// ellipse on the sky — naming the reason, and the region stays as it was.
func convertFrame(r regions.Region, to regions.Frame, p imaging.Placement) (out regions.Region, err error) {
	if r.Frame == to {
		return r, nil
	}
	px, m, err := r.ToPixels(p, 0)
	if err != nil {
		return r, app.RefuseOperation("the region does not reach the image's pixels: " + refusalText(err))
	}
	if m.How != regions.MappingIdentity && m.How != regions.MappingExact {
		return r, app.RefuseOperation("in pixels the " + r.Shape.Kind().String() + " would be " + m.How.String() + ", not the same shape; it stays in its frame")
	}
	s, m, err := regions.FromPixels(px, to, p)
	if err != nil {
		return r, app.RefuseOperation("the region cannot be stated in that frame: " + refusalText(err))
	}
	if m.How != regions.MappingIdentity && m.How != regions.MappingExact {
		return r, app.RefuseOperation("in that frame the " + r.Shape.Kind().String() + " would be a " + m.How.String() + ", not the same shape; it stays in its frame")
	}
	out = r
	out.Frame, out.Shape = to, s
	return out, nil
}

// translated is a shape moved by (dx, dy) in its own coordinates.
func translated(s regions.Shape, dx, dy float64) (regions.Shape, error) {
	a := shapeArgsOf(s)
	mv := func(p [2]float64) [2]float64 { return [2]float64{p[0] + dx, p[1] + dy} }
	a.At, a.From, a.To, a.Centre = mv(a.At), mv(a.From), mv(a.To), mv(a.Centre)
	for i := range a.Vertices {
		a.Vertices[i] = mv(a.Vertices[i])
	}
	return a.shape()
}

// movedBy is a region moved by (dx, dy) image pixels: in pixel coordinates
// directly, in an affine placement's frame by the affine's linear part. A
// region on the sky is not moved by a drag; its numbers are edited.
func movedBy(r regions.Region, dx, dy float64, p imaging.Placement) (out regions.Region, err error) {
	switch r.Frame.Kind {
	case regions.FrameKindPixel:
	case regions.FrameKindPhysical, regions.FrameKindReferenceSystem:
		a := p.Affine
		dx, dy = a[1]*dx+a[2]*dy, a[4]*dx+a[5]*dy
	default:
		return r, app.RefuseOperation("a region on the sky is not moved by a drag; edit its numbers")
	}
	if math.IsNaN(dx) || math.IsNaN(dy) {
		return r, app.RefuseOperation("the move has no size in the region's frame")
	}
	s, err := translated(r.Shape, dx, dy)
	if err != nil {
		return r, err
	}
	out = r
	out.Shape = s
	return out, nil
}
