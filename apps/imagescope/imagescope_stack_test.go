package imagescope

import (
	"bytes"
	"context"
	"encoding/hex"
	"image"
	"math"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/decode"
	"github.com/stergiotis/boxer/public/science/imaging/decode/decodetest"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

// openBytes opens a file's bytes through the window's open path, as the
// person's Open… does once the picker has handed them over, and waits for
// the open to end.
func openBytes(t *testing.T, inst *App, name string, body []byte) {
	t.Helper()
	require.True(t, inst.startOpening(image.Rectangle{}, func(ctx context.Context, report bgjob.Reporter, arrive func(*tiles.Store)) (*opened, error) {
		return openBody(ctx, inst.registry(), report, arrive, name, body)
	}))
	settle(t, inst, func() bool { return !inst.open.Running() && inst.landedGen == inst.openGen })
}

// decodedStack is a file's default entry decoded directly, to hold the
// window against.
func decodedStack(t *testing.T, body []byte) imaging.Stack {
	t.Helper()
	return decodedEntry(t, body, "")
}

func getView(t *testing.T, inst *App) View {
	t.Helper()
	v, err := query[appops.None, View](t, inst, opGetView, appops.None{})
	require.NoError(t, err)
	return v
}

// settleSelection runs frames until the selection asked for is shown.
func settleShown(t *testing.T, inst *App) {
	t.Helper()
	settle(t, inst, func() bool { return !inst.selectionPending() })
}

// A file a registered reader reads opens through the window's open path
// into the default selection of its default entry's stack: the image shown
// is the one the stack gives for it, sample for sample. The files are the
// conformance kit's test format, standing in for a host's formats.
func TestAStackOpensFromAFile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   []byte
		axes   []string
		planes []string
	}{
		{"pages.decodetest", testFile(t, decodetest.Member{Stack: pagesStack(t)}), []string{"page", "band"}, []string{"gray"}},
		{"nodata.decodetest", testFile(t, decodetest.Member{Stack: nodataStack(t)}), []string{"band"}, []string{"near infrared"}},
		{"geo.decodetest", testFile(t, decodetest.Member{Stack: geoStack(t)}), []string{"band"}, []string{"gray"}},
		{"cube.decodetest", testFile(t, decodetest.Member{Stack: cubeStack(t)}), []string{"axis3"}, []string{"gray"}},
		{"sky.decodetest", testFile(t, decodetest.Member{Stack: skyStack(t)}), nil, []string{"gray"}},
		{"multi.decodetest", multiFile(t), []string{"time", "band"}, []string{"B2"}},
	} {
		body := tc.body
		st := decodedStack(t, body)
		inst := testApp(t)
		openBytes(t, inst, tc.name, body)
		require.Empty(t, inst.openErr, tc.name)
		v := getView(t, inst)
		assert.Equal(t, "viewing", v.Phase, tc.name)
		require.True(t, v.StackSelection, tc.name)
		require.NotNil(t, v.Stack, tc.name)
		assert.Equal(t, tc.planes, v.Image.Planes, tc.name)
		assert.Equal(t, st.PlaneCount(), v.Stack.Planes, tc.name)
		assert.Equal(t, st.Kind().String(), v.Stack.Kind, tc.name)
		if tc.axes != nil {
			names := make([]string, len(v.Stack.Axes))
			for i, a := range v.Stack.Axes {
				names[i] = a.Name
			}
			assert.Equal(t, tc.axes, names, tc.name)
		}
		want, err := imageOf(st, defaultSelection(st))
		require.NoError(t, err, tc.name)
		h := want.Hash()
		assert.Equal(t, hex.EncodeToString(h[:]), v.Image.SampleHash, "%s: the image shown is the default selection's", tc.name)
		// The reader's notes are shown where the source notes are.
		for _, n := range st.Provenance().Notes {
			assert.Contains(t, v.Image.Notes, n, tc.name)
		}
	}
}

// A file a reader refuses ends the open with the reader's own sentence, and
// what was shown before stays; get_view says which refusal it was.
func TestAReadersRefusalReachesThePersonAsItsSentence(t *testing.T) {
	whole := testFile(t, decodetest.Member{Stack: cubeStack(t)})
	for _, tc := range []struct {
		name, feature, kind string
		body                []byte
	}{
		{"jpeg.decodetest", "compression 7 (JPEG)", "unsupported", testFile(t, decodetest.Member{Unsupported: "compression 7 (JPEG)"})},
		{"u32.decodetest", "unsigned 32-bit samples", "unsupported",
			testFile(t, decodetest.Member{Title: "the only HDU", Unsupported: "unsigned 32-bit samples", Stack: cubeStack(t)})},
		{"cut.decodetest", "the file is malformed", "malformed", whole[:len(whole)-10]},
	} {
		body := tc.body
		_, rerr := testReaders.Decode(context.Background(), bytes.NewReader(body), int64(len(body)), decode.Request{SourceName: tc.name})
		require.Error(t, rerr, tc.name)
		inst := withSample(t, samples.NameSmall)
		before := inst.pic
		openBytes(t, inst, tc.name, body)
		assert.Contains(t, inst.openErr, rerr.Error(), "%s: the reader's sentence, whole", tc.name)
		assert.Contains(t, inst.openErr, tc.name, "%s: the file named", tc.name)
		assert.Contains(t, inst.openErr, tc.feature, tc.name)
		assert.Same(t, before, inst.pic, "%s: what was shown stays", tc.name)
		v := getView(t, inst)
		assert.Equal(t, inst.openErr, v.Error, tc.name)
		assert.Equal(t, tc.kind, v.ErrorKind, tc.name)
		assert.False(t, v.StackSelection, tc.name)
		assert.Nil(t, v.File, "%s: the file of what is shown, the sample, is none", tc.name)
	}
	assert.NoError(t, checkSampleName(samples.StackBands), "the seed takes the sample stack")
}

// get_view reports the stack's axes, the selection and the placement.
func TestGetViewReportsTheStack(t *testing.T) {
	inst := testApp(t)
	require.NoError(t, command(t, inst, opOpenSample, OpenSampleArgs{Name: samples.StackBands}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	v := getView(t, inst)
	require.True(t, v.StackSelection)
	s := v.Stack
	require.NotNil(t, s)
	assert.Equal(t, 12, s.Planes)
	assert.Equal(t, "u16", s.Kind)
	assert.Empty(t, s.Model, "the bands are no colour model")
	assert.Equal(t, []AxisInfo{
		{Index: 0, Name: "time", Role: "selector", Size: 3, Unit: "d", Labels: []string{"day 0", "day 16", "day 32"}, Coords: []float64{0, 16, 32}},
		{Index: 1, Name: "band", Role: "band", Size: 4, Unit: "nm", Labels: []string{"B2", "B3", "B4", "B8"}, Coords: []float64{490, 560, 665, 842}},
	}, s.Axes)
	assert.Equal(t, SelectionInfo{Positions: []AxisPosition{{Axis: "time", Position: 0, Label: "day 0", HasCoord: true, Unit: "d"}},
		Bands: []BandRef{{Index: 0, Label: "B2"}}, Text: "time day 0 · band B2"}, s.Shown, "the first band, as the bands are no colour model")
	assert.Equal(t, s.Shown, s.Asked)
	assert.False(t, s.Pending)
	assert.Equal(t, []string{"B2"}, v.Image.Planes)
	require.NotNil(t, v.Image.Placement)
	p := v.Image.Placement
	assert.Equal(t, "reference-system", p.Frame)
	assert.Equal(t, "EPSG:32632", p.Code)
	assert.Equal(t, [2]string{"E", "N"}, p.AxisNames)
	assert.Equal(t, [2]string{"m", "m"}, p.Units)
	assert.Equal(t, [2]float64{10, 10}, p.PixelSize)
	assert.Equal(t, "synthetic", p.Source)
	assert.Contains(t, p.Description, "EPSG:32632")
	assert.Contains(t, v.Image.Notes[0], "0 marks no measurement")
	require.NotNil(t, v.View.Selection)
	assert.Equal(t, s.Asked, *v.View.Selection)
	assert.Contains(t, inst.phaseLine(), "time day 0 · band B2")

	// An image that is no stack's has none of it.
	inst = withSample(t, samples.NameSmall)
	v = getView(t, inst)
	assert.False(t, v.StackSelection)
	assert.Nil(t, v.Stack)
	assert.Nil(t, v.Image.Placement)
}

// set_position and set_bands set the selection, settle when it is shown,
// and refuse what they cannot do naming the way on.
func TestTheSelectionCommands(t *testing.T) {
	inst := testApp(t)
	require.True(t, inst.openSample(samples.StackBands, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })

	require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "time", Position: 1}))
	assert.Equal(t, phaseSelecting, inst.observePhase(), "asked for and not shown yet")
	_, settled, _ := settleSelection(inst, app.OperationCall{})
	assert.False(t, settled)
	v := getView(t, inst)
	assert.True(t, v.Stack.Pending)
	assert.Equal(t, "day 16", v.Stack.Asked.Positions[0].Label)
	assert.Equal(t, "day 0", v.Stack.Shown.Positions[0].Label)
	settleShown(t, inst)
	out, settled, err := settleSelection(inst, app.OperationCall{})
	require.NoError(t, err)
	require.True(t, settled)
	assert.Equal(t, "day 16", out.Selection.Positions[0].Label)
	assert.Equal(t, "viewing", out.Phase)
	v = getView(t, inst)
	assert.False(t, v.Stack.Pending)
	assert.Equal(t, 1, v.Stack.Shown.Positions[0].Position)
	assert.Equal(t, 16.0, v.Stack.Shown.Positions[0].Coord)

	require.NoError(t, command(t, inst, opSetBands, SetBandsArgs{Bands: []int{3, 2, 1}}))
	settleShown(t, inst)
	v = getView(t, inst)
	assert.Equal(t, "rgb", v.Image.Model)
	assert.Equal(t, []string{"B8", "B4", "B3"}, v.Image.Planes)
	assert.Equal(t, []BandRef{{3, "B8"}, {2, "B4"}, {1, "B3"}}, v.Stack.Shown.Bands)
	assert.Contains(t, v.SelectionNote, "a colour image where it was a single plane")

	// A band chosen twice: set_plane names the second of the two by index.
	require.NoError(t, command(t, inst, opSetBands, SetBandsArgs{Bands: []int{3, 2, 2}}))
	settleShown(t, inst)
	assert.Equal(t, []string{"B8", "B4", "B4"}, getView(t, inst).Image.Planes)
	require.NoError(t, command(t, inst, opSetPlane, SetPlaneArgs{ByIndex: true, Index: 2}))
	i, single := inst.viewer.Plane()
	assert.True(t, single)
	assert.Equal(t, 2, i)
	v = getView(t, inst)
	assert.Equal(t, "B4", v.View.Plane)
	assert.Equal(t, 2, v.View.PlaneIndex)
	assert.Contains(t, refusal(t, command(t, inst, opSetPlane, SetPlaneArgs{ByIndex: true, Index: 3})), "planes are 0 to 2: B8, B4, B4")

	for _, tc := range []struct {
		op   string
		args any
		want string
	}{
		{opSetPosition, SetPositionArgs{Axis: "band", Position: 1}, `"band" is the band axis; set_bands chooses`},
		{opSetPosition, SetPositionArgs{Axis: "depth"}, `no axis named "depth"; the selector axes are "time"`},
		{opSetPosition, SetPositionArgs{Axis: "time", Position: 3}, `position 3 is outside "time", whose positions are 0 to 2`},
		{opSetPosition, SetPositionArgs{Axis: "time", Position: -1}, "outside"},
		{opSetBands, SetBandsArgs{Bands: []int{4}}, `band 4 is outside "band", whose positions are 0 to 3`},
		{opSetBands, SetBandsArgs{Bands: []int{0, 1}}, "2 bands: name one band, three for red, green and blue"},
		{opSetBands, SetBandsArgs{}, "no colour model, so there is no image as stored; name one band"},
	} {
		var err error
		switch a := tc.args.(type) {
		case SetPositionArgs:
			err = command(t, inst, tc.op, a)
		case SetBandsArgs:
			err = command(t, inst, tc.op, a)
		}
		assert.Contains(t, refusal(t, err), tc.want)
	}
	assert.False(t, inst.selectionPending(), "a refused command asks for nothing")

	// A repeat sets the same selection and settles at once.
	require.NoError(t, command(t, inst, opSetBands, SetBandsArgs{Bands: []int{3, 2, 2}}))
	assert.False(t, inst.selectionPending())

	// With no stack the commands refuse, naming where one comes from.
	plain := withSample(t, samples.NameSmall)
	assert.Contains(t, refusal(t, command(t, plain, opSetPosition, SetPositionArgs{Axis: "time"})), "open_sample with bands")
	assert.Contains(t, refusal(t, command(t, plain, opSetBands, SetBandsArgs{Bands: []int{0}})), "list_formats lists what this host reads")
	_, err = query[GetAxisArgs, AxisInfo](t, plain, opGetAxis, GetAxisArgs{Axis: "time"})
	assert.Contains(t, refusal(t, err), "not a stack")
	empty := testApp(t)
	assert.Contains(t, refusal(t, command(t, empty, opSetPosition, SetPositionArgs{Axis: "time"})), "there is no image")
}

// A float colour image opens as the linked composite of its colour planes
// over the range of their data, named as such in the status row, and three
// float bands asked for are shown so and said to be (ADR-0401 §SD3).
func TestThreeFloatBandsOpenAsALinkedComposite(t *testing.T) {
	body := testFile(t, decodetest.Member{Stack: rgbF32Stack(t)})
	inst := testApp(t)
	openBytes(t, inst, "f.decodetest", body)
	require.Empty(t, inst.openErr)
	v := getView(t, inst)
	assert.Equal(t, "rgb", v.Stack.Model)
	assert.True(t, v.Stack.Shown.AsStored, "a stack whose bands are a colour model opens as stored")
	assert.Equal(t, modeComposite, v.Mode, "a float colour image opens as its composite")
	require.NotNil(t, v.Composite)
	assert.True(t, v.Composite.Linked)
	assert.Empty(t, v.Composite.Statement)
	data, ok := dataLimitsOver(inst.pic, []int{0, 1, 2})
	require.True(t, ok)
	for i, l := range v.Composite.Layers {
		assert.Equal(t, []string{"red", "green", "blue"}[i], l.Colour)
		assert.Equal(t, i, l.Plane)
		assert.Equal(t, limitsData, l.Recipe.Limits)
		assert.Equal(t, data.Lo, l.Recipe.Lo, "over the range of the three planes' data together")
		assert.Equal(t, data.Hi, l.Recipe.Hi)
	}
	assert.Contains(t, inst.phaseLine(), "composite of red in red, green in green, blue in blue, linked", "named in the status row")

	require.NoError(t, command(t, inst, opSetBands, SetBandsArgs{Bands: []int{2}}))
	settleShown(t, inst)
	assert.Equal(t, "gray", getView(t, inst).Image.Model)
	assert.Equal(t, modeSingle, getView(t, inst).Mode)

	require.NoError(t, command(t, inst, opSetBands, SetBandsArgs{Bands: []int{2, 1, 0}}))
	settle(t, inst, func() bool { return !inst.selectionPending() && inst.compAsk == nil })
	out, settled, err := settleAsync[SetBandsArgs, SelectionOutcome](t, inst, opSetBands, SetBandsArgs{Bands: []int{2, 1, 0}})
	require.NoError(t, err)
	require.True(t, settled)
	assert.Contains(t, out.Note, "three bands of f32 samples are shown as a linked composite in red, green and blue over the range of their data")
	v = getView(t, inst)
	require.NotNil(t, v.Composite)
	assert.Equal(t, []int{2, 1, 0}, []int{v.Composite.Layers[0].Band, v.Composite.Layers[1].Band, v.Composite.Layers[2].Band})
	assert.True(t, v.Stack.Shown.Composite)

	require.NoError(t, command(t, inst, opSetBands, SetBandsArgs{}), "as stored again")
	settleShown(t, inst)
	v = getView(t, inst)
	assert.True(t, v.Stack.Shown.AsStored)
	assert.Equal(t, modeComposite, v.Mode)
}

// meanOver is the mean of the valid values of a plane over a rectangle,
// computed here without the window's statistics.
func meanOver(p imaging.Plane, r image.Rectangle) (mean float64, n int) {
	sum := 0.0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if v, ok := p.ValueAt(x, y); ok {
				sum += v
				n++
			}
		}
	}
	return sum / float64(n), n
}

// The numbers follow the selection shown: one region measured at two times
// gives the two answers the planes give, and each result names the
// selection it is of.
func TestTheNumbersFollowTheSelection(t *testing.T) {
	st, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	inst := testApp(t)
	require.True(t, inst.openSample(samples.StackBands, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	require.NoError(t, command(t, inst, opSetBands, SetBandsArgs{Bands: []int{3}}))
	settleShown(t, inst)
	// Columns 0–19 hold the strip declared invalid at every time; it widens
	// with the time.
	region := RegionArgs{Col: 0, Row: 10, Width: 90, Height: 30}
	require.NoError(t, command(t, inst, opSetRegion, region))

	answers := map[int]float64{}
	for _, at := range []int{0, 2} {
		require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "time", Position: at}))
		settleShown(t, inst)
		settle(t, inst, func() bool { return inst.measured != nil })
		m, err := query[RegionArgs, Measurement](t, inst, opMeasure, RegionArgs{})
		require.NoError(t, err)
		plane, err := st.Plane([]int{at, 3})
		require.NoError(t, err)
		want, n := meanOver(plane, region.rect())
		assert.InDelta(t, want, m.Planes[0].Mean, 1e-9, "time %d", at)
		assert.EqualValues(t, n, m.Planes[0].Samples, "time %d", at)
		assert.EqualValues(t, 90*30-n, m.Planes[0].Invalid, "time %d", at)
		require.NotNil(t, m.Selection)
		assert.Equal(t, at, m.Selection.Positions[0].Position)
		assert.Equal(t, []BandRef{{3, "B8"}}, m.Selection.Bands)
		answers[at] = m.Planes[0].Mean

		px, err := query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{Col: 50, Row: 20})
		require.NoError(t, err)
		wantPx, _ := plane.ValueAt(50, 20)
		assert.Equal(t, []float64{wantPx}, px.Values[0])
		require.NotNil(t, px.Selection)
		assert.Equal(t, st.Label(0, at), px.Selection.Positions[0].Label)
		r, marked := inst.viewer.Region()
		assert.True(t, marked, "the marked region is kept")
		assert.Equal(t, region.rect(), r)
	}
	assert.NotEqual(t, answers[0], answers[2], "the two times differ")

	// What the window measured once, a later selection does not answer for.
	m0 := inst.measured
	require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "time", Position: 1}))
	settleShown(t, inst)
	assert.NotSame(t, m0, inst.measured)
	m, err := query[RegionArgs, Measurement](t, inst, opMeasure, region)
	require.NoError(t, err)
	plane, _ := st.Plane([]int{1, 3})
	want, _ := meanOver(plane, region.rect())
	assert.InDelta(t, want, m.Planes[0].Mean, 1e-9)
}

// A selection change keeps the view, and the recipe while the view stays
// of one kind; limits taken from the data are taken again, and the note
// says so.
func TestASelectionChangeKeepsTheViewAndTheRecipe(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	inst := testApp(t)
	frame := func() {
		inst.takeResults()
		inst.viewer.Render(400, 300)
		sm.ScriptReset()
	}
	require.True(t, inst.openSample(samples.StackBands, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	frame()
	inst.viewer.SetView(70.5, 40.25, 4)
	frame()
	x0, y0, mag0, ok := inst.viewer.View()
	require.True(t, ok)

	args := recipeArgs(inst.viewer.Recipe())
	args.Limits, args.Lo, args.Hi, args.Transfer, args.Palette = limitsManual, 300, 3000, "sqrt", "viridis"
	require.NoError(t, command(t, inst, opSetRecipe, args))
	require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "time", Position: 2}))
	settle(t, inst, func() bool { frame(); return !inst.selectionPending() })
	frame()
	x, y, mag, ok := inst.viewer.View()
	require.True(t, ok)
	assert.InDelta(t, x0, x, 1e-6, "the centre is kept")
	assert.InDelta(t, y0, y, 1e-6)
	assert.InDelta(t, mag0, mag, 1e-9, "the magnification is kept")
	r := inst.viewer.Recipe()
	assert.Equal(t, display.Limits{Lo: 300, Hi: 3000, Source: display.LimitsSourceManual}, r.Limits, "manual limits are kept as set")
	assert.Equal(t, display.TransferSqrt, r.Transfer)
	assert.Equal(t, "viridis", r.Palette)
	assert.Empty(t, inst.selectNote, "nothing had to change")

	// Data limits are taken again, from the new plane.
	args.Limits = limitsData
	require.NoError(t, command(t, inst, opSetRecipe, args))
	require.NoError(t, command(t, inst, opSetBands, SetBandsArgs{Bands: []int{3}}))
	settle(t, inst, func() bool { frame(); return !inst.selectionPending() })
	lo, hi, _ := inst.pic.StoredRange(0)
	assert.Equal(t, display.Limits{Lo: lo, Hi: hi, Source: display.LimitsSourceData}, inst.viewer.Recipe().Limits)
	assert.Equal(t, display.TransferSqrt, inst.viewer.Recipe().Transfer)
	assert.Contains(t, inst.selectNote, "limits taken again from the data: the range of B8, "+formatFigure(lo)+" to "+formatFigure(hi))
	assert.Contains(t, getView(t, inst).SelectionNote, "limits taken again")
	_, _, mag, _ = inst.viewer.View()
	assert.InDelta(t, mag0, mag, 1e-9)

	// From one band to three the view changes kind: the composite's default.
	require.NoError(t, command(t, inst, opSetBands, SetBandsArgs{Bands: []int{3, 2, 1}}))
	settle(t, inst, func() bool { frame(); return !inst.selectionPending() })
	assert.Contains(t, inst.selectNote, "a colour image where it was a single plane")
	assert.Equal(t, display.TransferLinear, inst.viewer.Recipe().Transfer)
}

// to_world and to_pixel are the placement's World and Pixel, on the sample
// stack, and on files whose reader delivered a reference system's placement
// and a gnomonic projection of the sky.
func TestFrameCoordinatesBothWays(t *testing.T) {
	check := func(name string, inst *App, p imaging.Placement, col, row float64) {
		t.Helper()
		w, err := query[PixelPoint, FrameReading](t, inst, opToWorld, PixelPoint{Col: col, Row: row})
		require.NoError(t, err, name)
		a, b, ok := p.World(col, row)
		require.True(t, ok, name)
		assert.Equal(t, a, w.A, name)
		assert.Equal(t, b, w.B, name)
		assert.Equal(t, int(math.Floor(col)), w.PixelCol, name)
		assert.True(t, w.Inside, name)
		assert.NotEmpty(t, w.Text, name)
		back, err := query[WorldPoint, FrameReading](t, inst, opToPixel, WorldPoint{A: w.A, B: w.B})
		require.NoError(t, err, name)
		assert.InDelta(t, col, back.Col, 1e-6, name)
		assert.InDelta(t, row, back.Row, 1e-6, name)
		assert.Equal(t, w.Text, back.Text, name)
	}

	inst := testApp(t)
	require.True(t, inst.openSample(samples.StackBands, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	check("bands", inst, samples.BandsPlacement, 80.5, 60.25)
	w, _ := query[PixelPoint, FrameReading](t, inst, opToWorld, PixelPoint{Col: 80.5, Row: 60.25})
	assert.Equal(t, "E 500805.00 m  N 5199397.50 m  (EPSG:32632)", w.Text)
	assert.Equal(t, [2]string{"E", "N"}, w.AxisNames)
	assert.Equal(t, "EPSG:32632", w.Code)
	far, err := query[PixelPoint, FrameReading](t, inst, opToWorld, PixelPoint{Col: -5, Row: 500})
	require.NoError(t, err)
	assert.False(t, far.Inside, "a point off the image has frame coordinates too, and says it is off")

	geo := testFile(t, decodetest.Member{Stack: geoStack(t)})
	inst = testApp(t)
	openBytes(t, inst, "geo.decodetest", geo)
	p := decodedStack(t, geo).Placement()
	require.Equal(t, geoPlacement, p)
	check("reference system", inst, p, 3.25, 7.5)
	assert.Equal(t, "EPSG:2056", getView(t, inst).Image.Placement.Code)

	tan := testFile(t, decodetest.Member{Stack: skyStack(t)})
	inst = testApp(t)
	openBytes(t, inst, "sky.decodetest", tan)
	p = decodedStack(t, tan).Placement()
	require.Equal(t, imaging.FrameKindCelestialGnomonic, p.Frame.Kind)
	check("sky", inst, p, 4.5, 3.5)
	w, err = query[PixelPoint, FrameReading](t, inst, opToWorld, PixelPoint{Col: 4.5, Row: 3.5})
	require.NoError(t, err)
	assert.Equal(t, [2]string{"deg", "deg"}, w.Units)
	assert.Contains(t, w.Text, "°")
	_, err = query[WorldPoint, FrameReading](t, inst, opToPixel, WorldPoint{A: math.Mod(p.Frame.RefLon+180, 360), B: -p.Frame.RefLat})
	assert.Contains(t, refusal(t, err), "on the hemisphere opposite the reference point")
	assert.Equal(t, "celestial-gnomonic", getView(t, inst).Image.Placement.Frame)

	inst = withSample(t, samples.NameSmall)
	_, err = query[PixelPoint, FrameReading](t, inst, opToWorld, PixelPoint{Col: 1, Row: 1})
	assert.Contains(t, refusal(t, err), "the image has no placement")
	_, err = query[WorldPoint, FrameReading](t, inst, opToPixel, WorldPoint{A: 1, B: 1})
	assert.Contains(t, refusal(t, err), "the image has no placement")
}

// get_view lists at most 32 positions of an axis, and get_axis pages the
// rest.
func TestAnAxisIsListedInPages(t *testing.T) {
	const n = 300
	planes := make([]imaging.Plane, n)
	labels := make([]string, n)
	coords := make([]float64, n)
	for i := range planes {
		p, err := imaging.NewPlaneU16(2, 2, []uint16{uint16(i), 1, 2, 3})
		require.NoError(t, err)
		planes[i], labels[i], coords[i] = p, "z"+strconv.Itoa(i), float64(i)*0.5
	}
	st, err := imaging.NewStack([]imaging.Axis{{Name: "depth", Role: imaging.AxisRoleSelector, Size: n, Unit: "um", Coords: coords, Labels: labels}},
		planes, 0, imaging.Placement{}, imaging.Provenance{SourceName: "deep"})
	require.NoError(t, err)
	inst := testApp(t)
	o, err := openedStack(context.Background(), st, "", func(uint64, uint64, string) {})
	require.NoError(t, err)
	inst.setShown(o.pic, o.stk)

	v := getView(t, inst)
	a := v.Stack.Axes[0]
	assert.Len(t, a.Labels, axisInlineMax)
	assert.Len(t, a.Coords, axisInlineMax)
	assert.True(t, a.Truncated)
	page, err := query[GetAxisArgs, AxisInfo](t, inst, opGetAxis, GetAxisArgs{Axis: "depth", From: 90, Count: 50})
	require.NoError(t, err)
	assert.Equal(t, 90, page.From)
	assert.Equal(t, labels[90:140], page.Labels)
	assert.Equal(t, coords[90:140], page.Coords)
	assert.True(t, page.Truncated)
	page, err = query[GetAxisArgs, AxisInfo](t, inst, opGetAxis, GetAxisArgs{Axis: "depth", Count: 1000})
	require.NoError(t, err)
	assert.Len(t, page.Labels, axisPageMax, "held to the page bound")
	page, err = query[GetAxisArgs, AxisInfo](t, inst, opGetAxis, GetAxisArgs{Axis: "depth", From: 280})
	require.NoError(t, err)
	assert.Equal(t, labels[280:], page.Labels)
	assert.False(t, page.Truncated, "the last page")
	_, err = query[GetAxisArgs, AxisInfo](t, inst, opGetAxis, GetAxisArgs{Axis: "depth", From: n})
	assert.Contains(t, refusal(t, err), "position 300 is outside")
	_, err = query[GetAxisArgs, AxisInfo](t, inst, opGetAxis, GetAxisArgs{Axis: "time"})
	assert.Contains(t, refusal(t, err), `no axis named "time"; the axes are "depth"`)

	require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "depth", Position: 42}))
	settleShown(t, inst)
	px, err := query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{})
	require.NoError(t, err)
	assert.Equal(t, []float64{42}, px.Values[0])
	assert.Equal(t, "z42", px.Selection.Positions[0].Label)
	assert.Contains(t, refusal(t, command(t, inst, opSetBands, SetBandsArgs{Bands: []int{0}})), "this stack has no band axis")
}

// The view resource carries the selection, and restoring it asks for it
// again.
func TestTheViewResourceRestoresTheSelection(t *testing.T) {
	inst := testApp(t)
	require.True(t, inst.openSample(samples.StackBands, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "time", Position: 2}))
	settleShown(t, inst)
	vs, ok := inst.Operations().ResourceValue(resView).(ViewState)
	require.True(t, ok)
	require.NotNil(t, vs.Selection)
	require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "time", Position: 0}))
	settleShown(t, inst)
	assert.True(t, inst.Operations().Restore(resView, vs))
	settleShown(t, inst)
	assert.Equal(t, []int{2, 0}, inst.stk.shown.at)
}

// A cancelled or failed selection is dropped, and the one shown stays.
func TestASelectionThatFailsKeepsTheOneShown(t *testing.T) {
	inst := testApp(t)
	require.True(t, inst.openSample(samples.StackBands, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	shown := inst.pic
	// A selection the stack cannot give: the band axis's entry is ignored,
	// so a position past the selector axis is what fails here.
	inst.setWant(selection{at: []int{7, 0}, bands: []int{0}})
	settle(t, inst, func() bool { return !inst.selectionPending() })
	assert.Same(t, shown, inst.pic)
	assert.Contains(t, inst.selectErr, "the selection could not be taken")
	assert.Equal(t, "viewing", inst.observePhase().String())
	assert.True(t, slices.Equal([]int{0, 0}, inst.want.at))
}

// The window renders a stack in every band mode, and the person's controls
// reach the same handlers as the commands.
func TestTheWindowRendersAStack(t *testing.T) {
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
	require.True(t, inst.openSample(samples.StackBands, image.Rect(60, 20, 100, 60)))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	frame()
	inst.positionGesture("time", 1)
	frame()
	assert.Equal(t, phaseSelecting, inst.phaseMachine.Current(), "mirrored while the selection is taken")
	settle(t, inst, func() bool { frame(); return !inst.selectionPending() })
	assert.Equal(t, []int{1, 0}, inst.stk.shown.at)
	inst.bandsGesture(inst.defaultTriple())
	settle(t, inst, func() bool { frame(); return !inst.selectionPending() })
	assert.Equal(t, []int{0, 1, 2}, inst.stk.shown.bands)
	assert.Equal(t, []int{0, 1, 2}, inst.lastTriple)
	frame()
	inst.bandsGesture([]int{2})
	settle(t, inst, func() bool { frame(); return !inst.selectionPending() })
	assert.Equal(t, []int{0, 1, 2}, inst.defaultTriple(), "the last three chosen come back")

	// The slider moved by the person is a set_position.
	inst.posSlider[0] = 2
	frame()
	settle(t, inst, func() bool { frame(); return !inst.selectionPending() })
	assert.Equal(t, 2, inst.stk.shown.at[0])
	assert.Equal(t, 2.0, inst.posSlider[0])

	// Three float bands are the person's composite of them.
	f32 := testFile(t, decodetest.Member{Stack: rgbF32Stack(t)})
	openBytes(t, inst, "f.decodetest", f32)
	frame()
	inst.bandsGesture([]int{1, 0, 2})
	settle(t, inst, func() bool { frame(); return !inst.selectionPending() && inst.compAsk == nil })
	assert.Empty(t, inst.problem)
	assert.Contains(t, inst.selectNote, "linked composite in red, green and blue")
	frame()

	// The composite's controls draw, and a layer's gesture reaches set_layer.
	inst.compositeGesture(inst.defaultComposite())
	frame()
	inst.layerShownBox[1] = false
	frame()
	layers, ok := inst.viewer.Composite()
	require.True(t, ok)
	assert.True(t, layers[1].Hidden, "the shown box is a set_layer")
	inst.linkedBox = false
	frame()
	assert.False(t, inst.viewer.CompositeLinked(), "the Linked box is a set_composite")
	frame()
}

// The stack's tour entry draws in its seeded state.
func TestTheStackTourDraws(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	state := tourStackInit(ids)
	inst, ok := state.(*App)
	require.True(t, ok)
	t.Cleanup(func() { _ = inst.Unmount(nil) })
	require.NotNil(t, inst.stk)
	for range 3 {
		tourRender(ids, state)
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
	}
	assert.Equal(t, "time day 16 · bands B8, B4, B3 as red, green, blue", inst.stk.describe(inst.stk.shown))
}
