package imagescope

import (
	"bytes"
	"image"
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/science/imaging/regions"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
)

// commandAs applies a command as a caller with a writer, as the host does
// for the person's gestures ("person") and an agent's task ("task:<id>").
func commandAs[In any, Out any](t *testing.T, inst *App, writer, name string, in In) (out Out, err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	raw, err := inst.Operations().ApplyCommand(app.OperationCall{Writer: writer}, name, args)
	if err != nil || len(raw) == 0 {
		return
	}
	out, decErr := buscodec.Decode[Out](raw)
	require.NoError(t, decErr)
	return
}

// settleAsync asks an asynchronous command whether it settled, decoding
// what it settled with.
func settleAsync[In any, Out any](t *testing.T, inst *App, name string, in In) (out Out, settled bool, err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	settled, raw, err := inst.Operations().(app.OperationsAsyncI).SettleAsync(app.OperationCall{}, name, args)
	if err != nil || !settled || len(raw) == 0 {
		return
	}
	out, decErr := buscodec.Decode[Out](raw)
	require.NoError(t, decErr)
	return
}

func rectSpec(cx, cy, w, h float64) RegionSpec {
	return RegionSpec{Shape: ShapeArgs{Kind: "rectangle", Centre: [2]float64{cx, cy}, Size: [2]float64{w, h}}}
}

func listRegions(t *testing.T, inst *App) RegionList {
	t.Helper()
	out, err := query[ListRegionsArgs, RegionList](t, inst, opListRegions, ListRegionsArgs{})
	require.NoError(t, err)
	return out
}

// A region made by an operation takes the caller's origin, never one it
// names; the person's gesture, through the host or directly, is the
// person's.
func TestARegionTakesItsOriginFromTheCaller(t *testing.T) {
	inst := withSample(t, samples.NameSmall)
	spec := rectSpec(10, 8, 6, 4)
	byTask, err := commandAs[RegionSpec, AddRegionOutcome](t, inst, "task:abc", opAddRegion, spec)
	require.NoError(t, err)
	byNobody, err := commandAs[RegionSpec, AddRegionOutcome](t, inst, "", opAddRegion, spec)
	require.NoError(t, err)
	byPerson, err := commandAs[RegionSpec, AddRegionOutcome](t, inst, opwire.WriterPerson, opAddRegion, spec)
	require.NoError(t, err)
	inst.addRegionGesture(rectSpec(20, 8, 6, 4)) // no host: applied directly, as the person's

	l := listRegions(t, inst)
	require.Len(t, l.Regions, 4)
	assert.Equal(t, []string{"task:abc", originAgent, originPerson, originPerson},
		[]string{l.Regions[0].Origin, l.Regions[1].Origin, l.Regions[2].Origin, l.Regions[3].Origin})
	assert.Equal(t, byTask.Id, l.Regions[0].Id)
	assert.Equal(t, byNobody.Id, l.Regions[1].Id)
	assert.Equal(t, byPerson.Id, l.Regions[2].Id)
	assert.Equal(t, l.Regions[3].Id, l.Selected, "the last region made is selected")
	assert.Equal(t, "rectangle 1", l.Regions[0].Label, "a region given no label is numbered")
	assert.True(t, l.Regions[0].Drawn)

	// An agent's update makes the region its own, whatever the region was.
	spec.Label = "mine now"
	_, err = commandAs[UpdateRegionArgs, appops.None](t, inst, "task:xyz", opUpdateRegion, UpdateRegionArgs{Id: byPerson.Id, Region: spec})
	require.NoError(t, err)
	r, err := query[RegionIdArgs, RegionInfo](t, inst, opGetRegion, RegionIdArgs(byPerson))
	require.NoError(t, err)
	assert.Equal(t, "task:xyz", r.Origin)
	assert.Equal(t, "mine now", r.Label)
	assert.Equal(t, "rectangle", r.Spec.Shape.Kind)

	v := getView(t, inst)
	assert.Equal(t, 4, v.Regions)
	assert.Equal(t, l.Regions[3].Id, v.SelectedRegion)
	assert.Equal(t, "select", v.Tool)
}

// Each operation of the regions refuses what it cannot do, naming the
// remedy, and changes nothing.
func TestTheRegionOperationsRefuseNamingTheRemedy(t *testing.T) {
	inst := withSample(t, samples.NameSmall)
	for _, tc := range []struct {
		spec RegionSpec
		want string
	}{
		{RegionSpec{Shape: ShapeArgs{Kind: "circle"}}, "the kinds are point, line, rectangle, ellipse and polygon"},
		{RegionSpec{Shape: ShapeArgs{Kind: "rectangle", Size: [2]float64{0, 3}}}, "sizes and radii are positive"},
		{RegionSpec{Shape: ShapeArgs{Kind: "polygon", Vertices: [][2]float64{{0, 0}, {1, 1}}}}, "three vertices or more"},
		{RegionSpec{Shape: ShapeArgs{Kind: "point"}, Frame: FrameArgs{Kind: "image"}}, "has no placement"},
		{RegionSpec{Shape: ShapeArgs{Kind: "point"}, Frame: FrameArgs{Kind: "lunar"}}, "the frames are pixel, image"},
		{RegionSpec{Shape: ShapeArgs{Kind: "point"}, AnchorShown: true}, "no stack's selection"},
		{RegionSpec{Shape: ShapeArgs{Kind: "point"}, Anchor: []AnchorPosition{{Axis: "time", Position: -1}}}, "anchor is not one"},
	} {
		assert.Contains(t, refusal(t, command(t, inst, opAddRegion, tc.spec)), tc.want)
	}
	assert.Empty(t, inst.regs)
	assert.Contains(t, refusal(t, command(t, inst, opRemoveRegion, RegionIdArgs{Id: "nope"})), "list_regions lists them")
	assert.Contains(t, refusal(t, command(t, inst, opSelectRegion, RegionIdArgs{Id: "nope"})), "list_regions lists them")
	_, err := query[RegionIdArgs, RegionInfo](t, inst, opGetRegion, RegionIdArgs{Id: "nope"})
	assert.Contains(t, refusal(t, err), "list_regions")

	inst.resultsCap = 0
	for range maxRegions {
		_, err := inst.addRegion(rectSpec(4, 4, 2, 2), originPerson)
		require.NoError(t, err)
	}
	assert.Contains(t, refusal(t, command(t, inst, opAddRegion, rectSpec(4, 4, 2, 2))), "remove one with remove_region")
	l, err := query[ListRegionsArgs, RegionList](t, inst, opListRegions, ListRegionsArgs{From: 250, Count: 100})
	require.NoError(t, err)
	assert.Len(t, l.Regions, maxRegions-250)
	assert.False(t, l.Truncated)
	l = listRegions(t, inst)
	assert.Len(t, l.Regions, regionsPageMax)
	assert.True(t, l.Truncated, "a page is bounded and says more follow")
}

// The tools make regions by gesture: a click for a point, a drag for a
// line, a rectangle or an ellipse, clicks for a polygon closed on its first
// vertex or by Enter; the select tool selects by a click, moves the region
// selected by a drag, and Delete removes it.
func TestRegionsAreMadeByGesture(t *testing.T) {
	inst := withSample(t, samples.NameSmall)
	at := func(x, y float64) imageview.PixelPoint { return imageview.PixelPoint{X: x, Y: y} }
	shape := func(i int) ShapeArgs { return shapeArgsOf(inst.regs[i].r.Shape) }

	inst.setTool(toolPoint)
	inst.handleViewerEvents(imageview.Events{ClickedOk: true, ClickedAt: at(3.25, 4.5)})
	require.Len(t, inst.regs, 1)
	assert.Equal(t, [2]float64{3.25, 4.5}, shape(0).At)
	assert.Equal(t, originPerson, inst.regs[0].r.Origin)

	inst.setTool(toolLine)
	inst.handleViewerEvents(imageview.Events{DragOk: true, DragFrom: at(1, 1), DragTo: at(20, 9)})
	assert.NotNil(t, inst.dragPreview, "a drag under way previews its shape")
	inst.handleViewerEvents(imageview.Events{DragEnded: true, DragEndFrom: at(1, 1), DragEndTo: at(20, 9)})
	require.Len(t, inst.regs, 2)
	assert.Equal(t, ShapeArgs{Kind: "line", From: [2]float64{1, 1}, To: [2]float64{20, 9}}, shape(1))

	inst.setTool(toolRectangle)
	inst.handleViewerEvents(imageview.Events{DragEnded: true, DragEndFrom: at(30, 20), DragEndTo: at(10, 4)})
	require.Len(t, inst.regs, 3)
	assert.Equal(t, [2]float64{20, 12}, shape(2).Centre)
	assert.Equal(t, [2]float64{20, 16}, shape(2).Size, "by opposite corners, in either direction")
	inst.handleViewerEvents(imageview.Events{DragEnded: true, DragEndFrom: at(30, 20), DragEndTo: at(30, 4)})
	assert.Len(t, inst.regs, 3, "a rectangle of no width is not made")
	assert.Contains(t, inst.regionNote, "needs an extent")

	inst.setTool(toolEllipse)
	inst.handleViewerEvents(imageview.Events{DragEnded: true, DragEndFrom: at(2, 2), DragEndTo: at(12, 8)})
	require.Len(t, inst.regs, 4)
	assert.Equal(t, [2]float64{5, 3}, shape(3).Radii)

	inst.setTool(toolPolygon)
	for _, p := range []imageview.PixelPoint{at(1, 1), at(10, 1), at(10, 10)} {
		inst.handleViewerEvents(imageview.Events{ClickedOk: true, ClickedAt: p})
	}
	assert.Len(t, inst.polyDraft, 3)
	inst.handleViewerEvents(imageview.Events{ClickedOk: true, ClickedAt: at(1.5, 1.5)})
	require.Len(t, inst.regs, 5, "a click on the first vertex closes it")
	assert.Len(t, shape(4).Vertices, 3)
	for _, p := range []imageview.PixelPoint{at(1, 1), at(10, 1), at(10, 10), at(1, 10)} {
		inst.handleViewerEvents(imageview.Events{ClickedOk: true, ClickedAt: p})
	}
	inst.handleViewerEvents(imageview.Events{Keys: []c.CapturedKey{{Code: keycodes.Enter, Edges: 1}}})
	require.Len(t, inst.regs, 6, "Enter closes it")
	assert.Len(t, shape(5).Vertices, 4)
	inst.handleViewerEvents(imageview.Events{ClickedOk: true, ClickedAt: at(2, 2)})
	inst.handleViewerEvents(imageview.Events{Keys: []c.CapturedKey{{Code: keycodes.Escape, Edges: 1}}})
	assert.Empty(t, inst.polyDraft, "Escape drops it")
	assert.Len(t, inst.regs, 6)

	// The select tool: a click on the rectangle selects it; a drag moves it.
	inst.setTool(toolSelect)
	rect := inst.regs[2].r.Id
	inst.handleViewerEvents(imageview.Events{ClickedOk: true, ClickedAt: at(25, 18)})
	assert.Equal(t, rect, inst.selRegion)
	inst.handleViewerEvents(imageview.Events{ClickedOk: true, ClickedAt: at(45, 30)})
	assert.Empty(t, inst.selRegion, "a click on nothing selects nothing")
	inst.selRegion = rect
	inst.handleViewerEvents(imageview.Events{DragEnded: true, DragEndFrom: at(20, 12), DragEndTo: at(23, 10)})
	assert.Equal(t, [2]float64{23, 10}, shape(2).Centre, "moved by the drag")
	inst.handleViewerEvents(imageview.Events{Keys: []c.CapturedKey{{Code: keycodes.Delete, Edges: 1}}})
	assert.Len(t, inst.regs, 5, "Delete removes the region selected")
	assert.Equal(t, -1, regionIndex(inst.regs, rect))
}

// The marked rectangle stays what it is, and Keep as region makes a region
// of it.
func TestTheMarkedRectangleIsKeptAsARegion(t *testing.T) {
	inst := withSample(t, samples.NameSmall)
	require.NoError(t, command(t, inst, opSetRegion, RegionArgs{Col: 2, Row: 3, Width: 10, Height: 4}))
	r, _ := inst.viewer.Region()
	spec := RegionSpec{Shape: ShapeArgs{Kind: "rectangle", Centre: [2]float64{float64(r.Min.X+r.Max.X) / 2, float64(r.Min.Y+r.Max.Y) / 2},
		Size: [2]float64{float64(r.Dx()), float64(r.Dy())}}}
	inst.addRegionGesture(spec)
	require.Len(t, inst.regs, 1)
	cov, err := regions.CoverageOf(inst.regs[0].r.Shape, 48, 32)
	require.NoError(t, err)
	assert.Equal(t, image.Rect(2, 3, 12, 7), cov.Bounds(), "the region covers the pixels the rectangle marked")
	_, marked := inst.viewer.Region()
	assert.True(t, marked, "the marked rectangle stays")
}

// Whether a region is drawn on the image shown is data: its anchor against
// the stack position shown, its frame against the image's placement.
func TestWhereARegionIsDrawnIsDecidedByItsAnchorAndFrame(t *testing.T) {
	inst := testApp(t)
	require.True(t, inst.openSample(samples.StackBands, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })

	pixel, err := inst.addRegion(rectSpec(40, 30, 10, 10), originPerson)
	require.NoError(t, err)
	here, err := inst.addRegion(RegionSpec{Shape: ShapeArgs{Kind: "point", At: [2]float64{5, 5}}, AnchorShown: true}, originPerson)
	require.NoError(t, err)
	world, err := inst.addRegion(RegionSpec{Shape: ShapeArgs{Kind: "point", At: [2]float64{500_400.5, 5_199_800.5}}, Frame: FrameArgs{Kind: "image"}}, originPerson)
	require.NoError(t, err)
	other, err := inst.addRegion(RegionSpec{Shape: ShapeArgs{Kind: "point", At: [2]float64{1, 1}},
		Frame: FrameArgs{Kind: "reference-system", Code: "EPSG:2056"}}, originPerson)
	require.NoError(t, err)
	alien, err := inst.addRegion(RegionSpec{Shape: ShapeArgs{Kind: "point", At: [2]float64{1, 1}}, Anchor: []AnchorPosition{{Axis: "depth"}}}, originPerson)
	require.NoError(t, err)

	info := func(id string) RegionInfo {
		r, err := query[RegionIdArgs, RegionInfo](t, inst, opGetRegion, RegionIdArgs{Id: id})
		require.NoError(t, err)
		return r
	}
	assert.True(t, info(pixel).Drawn)
	assert.Equal(t, "identity", info(pixel).Mapping.How)
	assert.True(t, info(here).Drawn)
	assert.Equal(t, []AnchorPosition{{Axis: "time"}}, info(here).Anchor)
	w := info(world)
	require.True(t, w.Drawn, w.Reason)
	assert.Equal(t, "exact", w.Mapping.How)
	assert.InDelta(t, 40.05, w.Mapping.Shape.At[0], 1e-9, "500 400.5 m east of the origin at 10 m a pixel")
	assert.InDelta(t, 19.95, w.Mapping.Shape.At[1], 1e-9, "north runs up, rows down")
	o := info(other)
	assert.False(t, o.Drawn)
	assert.True(t, o.Applies)
	assert.Contains(t, o.Reason, "EPSG:2056 frame; the image shown is in the reference-system EPSG:32632 frame")
	a := info(alien)
	assert.False(t, a.Applies)
	assert.Contains(t, a.Reason, "positions this stack does not have")

	// At another time the anchored region does not apply, and says why.
	require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "time", Position: 2}))
	settleShown(t, inst)
	h := info(here)
	assert.False(t, h.Applies)
	assert.False(t, h.Drawn)
	assert.Contains(t, h.Reason, "anchored to other positions of the stack: time 0")
	assert.True(t, info(pixel).Drawn, "a region of every position applies at every one")

	// The drawing follows the same decision, cached by revision, image,
	// position and zoom.
	d := inst.drawn(regionEntryOf(t, inst, here), 0)
	assert.False(t, d.d.Drawn)
	d = inst.drawn(regionEntryOf(t, inst, pixel), 0)
	require.True(t, d.d.Drawn)
	assert.Len(t, d.pts, 4)
	assert.Same(t, d, inst.drawn(regionEntryOf(t, inst, pixel), 0), "kept while its key stands")
	assert.NotSame(t, d, inst.drawn(regionEntryOf(t, inst, pixel), 3), "drawn again at another zoom")
}

func regionEntryOf(t *testing.T, inst *App, id string) regionEntry {
	t.Helper()
	i := regionIndex(inst.regs, id)
	require.GreaterOrEqual(t, i, 0)
	return inst.regs[i]
}

// A region's frame switches between pixels and the image's frame exactly,
// or is refused naming why.
func TestTheFrameOfARegionSwitchesExactlyOrIsRefused(t *testing.T) {
	inst := testApp(t)
	require.True(t, inst.openSample(samples.StackBands, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	p := inst.pic.Placement()
	id, err := inst.addRegion(RegionSpec{Shape: ShapeArgs{Kind: "ellipse", Centre: [2]float64{40, 30}, Radii: [2]float64{8, 5}, Angle: 30}}, originPerson)
	require.NoError(t, err)
	r := regionEntryOf(t, inst, id).r
	f, err := FrameArgs{Kind: "image"}.frame(inst.pic)
	require.NoError(t, err)
	world, err := convertFrame(r, f, p)
	require.NoError(t, err)
	assert.Equal(t, regions.FrameKindReferenceSystem, world.Frame.Kind)
	assert.InDelta(t, 500_400, world.Shape.Centre().X, 1e-6)
	back, err := convertFrame(world, regions.PixelFrame(), p)
	require.NoError(t, err)
	assert.InDelta(t, 40, back.Shape.Centre().X, 1e-9)
	assert.InDelta(t, 30, back.Shape.Centre().Y, 1e-9)
	rx, ry := back.Shape.Radii()
	assert.InDelta(t, 8, rx, 1e-9)
	assert.InDelta(t, 5, ry, 1e-9)

	// Through the person's button, as update_region.
	inst.frameGesture(r, FrameArgs{Kind: "image"})
	assert.Equal(t, regions.FrameKindReferenceSystem, regionEntryOf(t, inst, id).r.Frame.Kind)
	assert.Empty(t, inst.regionNote)

	// A placement that shears: a rectangle would be a polygon, so the
	// switch is refused and the region stays.
	shear := p
	shear.Affine = [6]float64{0, 1, 0.3, 0, 0.1, 1}
	rect, err := regions.NewRectangle(regions.Point{X: 10, Y: 10}, 4, 2, 0)
	require.NoError(t, err)
	_, err = convertFrame(regions.Region{Id: "x", Frame: regions.PixelFrame(), Shape: rect}, f, shear)
	assert.Contains(t, refusal(t, err), "polygon, not the same shape")
}

// The figures of a region are computed off the frame over the selection
// shown, say how they were made, follow the selection, and are marked stale
// when the region, the image or the selection changes.
func TestRegionFiguresFollowTheSelectionAndGoStale(t *testing.T) {
	inst := testApp(t)
	require.True(t, inst.openSample(samples.StackBands, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	id, err := inst.addRegion(RegionSpec{Shape: ShapeArgs{Kind: "ellipse", Centre: [2]float64{100, 60}, Radii: [2]float64{6, 4}}}, originPerson)
	require.NoError(t, err)

	measure := func(area bool) RegionMeasurement {
		require.NoError(t, command(t, inst, opMeasureRegion, MeasureRegionArgs{Id: id, Area: area}))
		_, settled, err := settleAsync[MeasureRegionArgs, RegionMeasurement](t, inst, opMeasureRegion, MeasureRegionArgs{Id: id})
		require.NoError(t, err)
		require.False(t, settled, "not before its frame takes it")
		settle(t, inst, func() bool { return !inst.regionJob.Running() && inst.regionLanded == inst.regionCallGen })
		m, settled, err := settleAsync[MeasureRegionArgs, RegionMeasurement](t, inst, opMeasureRegion, MeasureRegionArgs{Id: id})
		require.NoError(t, err)
		require.True(t, settled)
		return m
	}
	m := measure(false)
	assert.Equal(t, regions.MethodId, m.Method)
	assert.Equal(t, "centre", m.Rule)
	assert.Equal(t, "identity", m.Mapping.How)
	assert.Equal(t, openedId, m.Image)
	require.NotNil(t, m.Selection)
	assert.Equal(t, "day 0", m.Selection.Positions[0].Label)
	assert.Equal(t, 0, m.Level)
	require.Len(t, m.Planes, 1)
	assert.Equal(t, "B2", m.Planes[0].Plane)
	assert.Nil(t, m.Planes[0].Area)
	assert.False(t, m.Stale)
	assert.Contains(t, m.Statement, "whose centre lies inside it")

	// Held against the engine over the same samples.
	want, err := regions.Measure(t.Context(), regions.StoreSource(inst.pic), regionEntryOf(t, inst, id).r, regions.MeasureOptions{})
	require.NoError(t, err)
	assert.Equal(t, want.Pixels, m.Pixels)
	assert.Equal(t, want.Planes[0].Mean, m.Planes[0].Mean)

	require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "time", Position: 1}))
	settleShown(t, inst)
	r, err := query[RegionIdArgs, RegionInfo](t, inst, opGetRegion, RegionIdArgs{Id: id})
	require.NoError(t, err)
	assert.Equal(t, "stale", r.Figures, "another selection is shown")

	m = measure(true)
	assert.Equal(t, "day 16", m.Selection.Positions[0].Label)
	require.NotNil(t, m.Planes[0].Area)
	assert.Equal(t, "area", m.Planes[0].Area.Rule)
	assert.InDelta(t, math.Pi*6*4, m.Planes[0].Area.Area+m.Planes[0].Area.InvalidArea, 1e-9, "the exact area of the ellipse")
	assert.True(t, m.Area)

	require.NoError(t, command(t, inst, opUpdateRegion, UpdateRegionArgs{Id: id, Region: RegionSpec{Shape: ShapeArgs{Kind: "ellipse",
		Centre: [2]float64{100, 60}, Radii: [2]float64{6, 5}}}}))
	r, err = query[RegionIdArgs, RegionInfo](t, inst, opGetRegion, RegionIdArgs{Id: id})
	require.NoError(t, err)
	assert.Equal(t, "stale", r.Figures, "the region changed")

	// A region not drawn on the image shown is refused, naming why.
	other, err := inst.addRegion(RegionSpec{Shape: ShapeArgs{Kind: "point", At: [2]float64{1, 1}}, AnchorShown: true}, originPerson)
	require.NoError(t, err)
	require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "time", Position: 0}))
	settleShown(t, inst)
	assert.Contains(t, refusal(t, command(t, inst, opMeasureRegion, MeasureRegionArgs{Id: other})), "anchored to other positions")
	assert.Contains(t, refusal(t, command(t, inst, opMeasureRegion, MeasureRegionArgs{Id: "nope"})), "list_regions")

	// The table carries the figures and their state.
	rows, ids := inst.regionRows()
	require.Len(t, rows, 2)
	assert.Equal(t, []string{id, other}, ids)
	assert.Equal(t, "stale, area", rows[0][len(regionColumns)-1])
}

// A line's profile is its pixels in order with every plane's value,
// bounded in pages; a region that is no line is refused.
func TestALineProfileIsItsPixelsInOrderAndPaged(t *testing.T) {
	inst := withSample(t, samples.NameSmall)
	id, err := inst.addRegion(RegionSpec{Shape: ShapeArgs{Kind: "line", From: [2]float64{0.5, 0.5}, To: [2]float64{40.5, 0.5}}}, originPerson)
	require.NoError(t, err)
	p, err := query[ProfileArgs, ProfileRows](t, inst, opProfileRegion, ProfileArgs{Id: id, Count: 16})
	require.NoError(t, err)
	assert.Equal(t, 41, p.Total)
	assert.True(t, p.Truncated)
	require.Len(t, p.Rows, 16)
	assert.Equal(t, regions.MethodId, p.Method)
	assert.Equal(t, []string{"gray"}, p.Planes)
	small := sampleImage(t, samples.NameSmall)
	for i, row := range p.Rows {
		assert.Equal(t, i, row.Col)
		assert.Equal(t, 0, row.Row)
		want, _ := small.Plane(0).ValueAt(row.Col, 0)
		assert.Equal(t, want, row.Values[0], "the stored sample, not an interpolation")
	}
	assert.InDelta(t, 0.5, p.Rows[0].Length, 1e-12, "the line starts in the first pixel's middle")
	assert.InDelta(t, 0.25, p.Rows[0].Distance, 1e-12, "to the middle of its part")
	p, err = query[ProfileArgs, ProfileRows](t, inst, opProfileRegion, ProfileArgs{Id: id, From: 32})
	require.NoError(t, err)
	assert.Len(t, p.Rows, 9)
	assert.False(t, p.Truncated)
	assert.Equal(t, 32, p.From)
	assert.InDelta(t, 40, p.Length, 1e-12)

	rect, err := inst.addRegion(rectSpec(5, 5, 4, 4), originPerson)
	require.NoError(t, err)
	_, err = query[ProfileArgs, ProfileRows](t, inst, opProfileRegion, ProfileArgs{Id: rect})
	assert.Contains(t, refusal(t, err), "a profile is of a line")

	// The window's own profile, computed as a task, draws one point per
	// valid sample.
	require.NoError(t, inst.startProfile(id))
	settle(t, inst, func() bool { return inst.profile != nil })
	assert.Len(t, inst.profile.xs[0], 41)
	assert.False(t, inst.profileStale(inst.profile))
	rec := profileRecord(inst.profile)
	defer rec.Release()
	assert.EqualValues(t, 41, rec.NumRows())
	assert.Equal(t, []string{"step", "col", "row", "length", "distance", "frame_distance", "gray"}, profileHeaders(inst.profile))
	assert.Contains(t, profileSql(inst.profile), "FROM keelson('profile')")
}

// The two handovers refuse without a bus, and the figures' dataset is one
// row per region and plane, a figure that is no number null.
func TestTheRegionHandoversAreBounded(t *testing.T) {
	inst := withSample(t, samples.NameFloat)
	assert.Contains(t, refusal(t, command(t, inst, opSendRegionsToPlay, appops.None{})), "no bus on this host")
	id, err := inst.addRegion(RegionSpec{Shape: ShapeArgs{Kind: "rectangle", Centre: [2]float64{80, 340}, Size: [2]float64{20, 20}}}, originPerson)
	require.NoError(t, err)
	assert.Contains(t, refusal(t, command(t, inst, opSendProfileToPlay, RegionIdArgs{Id: id})), "no bus on this host")

	m, err := regions.Measure(t.Context(), regions.StoreSource(inst.pic), regionEntryOf(t, inst, id).r, regions.MeasureOptions{})
	require.NoError(t, err)
	rm := regionMeasurementOf(&regionMeasure{regionId: id, m: m, imageId: openedId}, "blob", false)
	stream, rows, err := regionsArrow([]RegionMeasurement{rm}, inst.regs)
	require.NoError(t, err)
	assert.EqualValues(t, 1, rows)
	rd, err := ipc.NewReader(bytes.NewReader(stream))
	require.NoError(t, err)
	defer rd.Release()
	require.True(t, rd.Next())
	rec := rd.RecordBatch()
	assert.Equal(t, "region", rec.ColumnName(0))
	assert.Equal(t, id, rec.Column(0).(*array.String).Value(0))
	assert.True(t, rec.Column(16).IsNull(0), "the block is all invalid: no mean")
	assert.EqualValues(t, 400, rec.Column(13).(*array.Uint64).Value(0), "every covered sample is invalid")
	assert.True(t, rec.Column(21).IsNull(0), "no area figures were asked for")

	line, err := regions.NewLine(regions.Point{X: 0, Y: 0}, regions.Point{X: 1e7, Y: 1e7})
	require.NoError(t, err)
	assert.EqualValues(t, 640+480+2, profileBound(line, inst.pic), "bounded by the image's columns and rows")
}

// The regions document goes out and comes back as it was; an agent's
// document takes the agent's origin, the person's keeps its own; a
// document that does not read changes nothing.
func TestTheRegionsDocumentRoundTripsThroughTheWindow(t *testing.T) {
	inst := withSample(t, samples.NameSmall)
	_, err := inst.addRegion(rectSpec(10, 8, 6, 4), originPerson)
	require.NoError(t, err)
	_, err = commandAs[RegionSpec, AddRegionOutcome](t, inst, "task:abc", opAddRegion, RegionSpec{Label: "line", Note: "a transect",
		Shape: ShapeArgs{Kind: "line", From: [2]float64{0, 0}, To: [2]float64{10, 5}}})
	require.NoError(t, err)
	doc, err := query[appops.None, RegionsDocument](t, inst, opGetRegionsDocument, appops.None{})
	require.NoError(t, err)
	assert.Contains(t, doc.Document, `"version": 1`)

	other := withSample(t, samples.NameSmall)
	require.NoError(t, other.setRegionsDocument([]byte(doc.Document), originPerson))
	back, err := query[appops.None, RegionsDocument](t, other, opGetRegionsDocument, appops.None{})
	require.NoError(t, err)
	assert.Equal(t, doc.Document, back.Document, "the person's load keeps everything, origins included")

	_, err = commandAs[RegionsDocument, appops.None](t, other, "task:zzz", opSetRegionsDocument, doc)
	require.NoError(t, err)
	l := listRegions(t, other)
	require.Len(t, l.Regions, 2)
	for _, r := range l.Regions {
		assert.Equal(t, "task:zzz", r.Origin, "an agent cannot set a region down as the person's")
	}
	assert.Equal(t, "a transect", l.Regions[1].Note)

	before := len(other.regs)
	assert.Contains(t, refusal(t, command(t, other, opSetRegionsDocument, RegionsDocument{Document: `{"version": 2, "regions": []}`})), "not a regions document of version 1")
	assert.Contains(t, refusal(t, command(t, other, opSetRegionsDocument, RegionsDocument{Document: "not json"})), "not a regions document")
	assert.Len(t, other.regs, before, "a refused document changes nothing")

	// The resource is the document, and restoring it is undo.
	h := other.Operations()
	v := h.ResourceValue(resRegions)
	require.NoError(t, command(t, other, opRemoveRegion, RegionIdArgs{Id: l.Regions[0].Id}))
	assert.Len(t, other.regs, 1)
	assert.True(t, h.Restore(resRegions, v))
	assert.Len(t, other.regs, 2)
}

// The window renders with regions and the side panel in every phase of the
// regions' work, and the overlay draws each region once.
func TestTheWindowRendersWithRegions(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	inst := testApp(t)
	frame := func() {
		inst.takeResults()
		inst.mirrorPhase()
		inst.renderBody()
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
	}
	require.True(t, inst.openSample(samples.StackBands, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	frame()
	for _, spec := range []RegionSpec{
		rectSpec(40, 30, 10, 10),
		{Shape: ShapeArgs{Kind: "ellipse", Centre: [2]float64{100, 60}, Radii: [2]float64{6, 4}, Angle: 20}},
		{Shape: ShapeArgs{Kind: "line", From: [2]float64{0, 0}, To: [2]float64{150, 100}}},
		{Shape: ShapeArgs{Kind: "point", At: [2]float64{5, 5}}},
		{Shape: ShapeArgs{Kind: "polygon", Vertices: [][2]float64{{10, 10}, {30, 12}, {20, 40}}}},
	} {
		_, err := commandAs[RegionSpec, AddRegionOutcome](t, inst, "task:t", opAddRegion, spec)
		require.NoError(t, err)
		frame()
	}
	line := inst.regs[2].r.Id
	require.NoError(t, inst.selectRegion(line))
	frame()
	require.NoError(t, inst.startRegionMeasure(nil, true))
	frame()
	settle(t, inst, func() bool { return !inst.regionJob.Running() && inst.regionLanded == inst.regionCallGen })
	require.NoError(t, inst.startProfile(line))
	settle(t, inst, func() bool { return inst.profile != nil })
	frame()
	frame()
	assert.Len(t, inst.regMeasures, 5)
	assert.NotNil(t, inst.regTable)
	assert.Len(t, inst.drawCache, 5)
}

// Every edge of the machine is labelled with the gesture or the event that
// takes it, and the work phases are observed from their jobs.
func TestEveryEdgeIsLabelledAndTheWorkPhasesAreObserved(t *testing.T) {
	m := newPhaseMachine()
	n := 0
	for e, label := range m.Edges() {
		assert.NotEmpty(t, label, "%v → %v", e.From, e.To)
		n++
	}
	assert.Greater(t, n, 30)
	for _, w := range workPhases {
		for _, ph := range imagePhases {
			assert.True(t, m.CanReach(ph, w), "%v → %v", ph, w)
			assert.True(t, m.CanReach(w, ph), "%v → %v", w, ph)
		}
	}

	inst := withSample(t, samples.NameField)
	id, err := inst.addRegion(RegionSpec{Shape: ShapeArgs{Kind: "line", From: [2]float64{0, 0}, To: [2]float64{1399, 999}}}, originPerson)
	require.NoError(t, err)
	require.NoError(t, inst.startProfile(id))
	assert.Equal(t, phaseProfiling, inst.observePhase())
	settle(t, inst, func() bool { return inst.profile != nil })
	require.NoError(t, inst.startRegionMeasure(nil, false))
	assert.Equal(t, phaseRegionMeasuring, inst.observePhase())
	settle(t, inst, func() bool { return !inst.regionJob.Running() && inst.regionLanded == inst.regionCallGen })
	assert.Equal(t, phaseViewing, inst.observePhase())
	inst.mirrorPhase()
	assert.Equal(t, phaseViewing, inst.phaseMachine.Current())
}

// measure_regions measures every region drawn on the image shown in one
// task and settles with a line per region; their figures are then
// measure_region's.
func TestMeasureRegionsMeasuresEveryRegionDrawn(t *testing.T) {
	inst := withSample(t, samples.NameSmall)
	assert.Contains(t, refusal(t, command(t, inst, opMeasureRegions, MeasureRegionsArgs{})), "no region is drawn on the image shown")
	ids := make([]string, 0, 3)
	for k := range 3 {
		out, err := commandAs[RegionSpec, AddRegionOutcome](t, inst, "task:t", opAddRegion, rectSpec(8+float64(k)*10, 10, 6, 4))
		require.NoError(t, err)
		ids = append(ids, out.Id)
	}
	require.NoError(t, command(t, inst, opMeasureRegions, MeasureRegionsArgs{}))
	settle(t, inst, func() bool { return !inst.regionJob.Running() && inst.regionLanded == inst.regionCallGen })
	sum, settled, err := settleAsync[MeasureRegionsArgs, RegionsMeasured](t, inst, opMeasureRegions, MeasureRegionsArgs{})
	require.NoError(t, err)
	require.True(t, settled)
	assert.Equal(t, 3, sum.Measured)
	assert.Zero(t, sum.NotDrawn)
	require.Len(t, sum.Regions, 3)
	for k, line := range sum.Regions {
		assert.Equal(t, ids[k], line.Region)
		require.NoError(t, command(t, inst, opMeasureRegion, MeasureRegionArgs{Id: line.Region}))
		settle(t, inst, func() bool { return !inst.regionJob.Running() && inst.regionLanded == inst.regionCallGen })
		m, settled, err := settleAsync[MeasureRegionArgs, RegionMeasurement](t, inst, opMeasureRegion, MeasureRegionArgs{Id: line.Region})
		require.NoError(t, err)
		require.True(t, settled)
		assert.Equal(t, m.Pixels, line.Pixels)
		assert.Equal(t, m.Planes[0].Mean, line.Planes[0].Mean, "the line is measure_region's figures in short")
	}
}
