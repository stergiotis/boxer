package imagescope

import (
	"bytes"
	"context"
	"errors"
	"image"
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// testApp is an unmounted window, as the tour and the catalog tests have it:
// no bus, no tasks, its jobs on plain goroutines.
func testApp(t *testing.T) (inst *App) {
	t.Helper()
	inst = newApp()
	inst.logger = zerolog.Nop()
	t.Cleanup(func() { _ = inst.Unmount(nil) })
	return
}

// withSample is testApp showing a sample image.
func withSample(t *testing.T, name string) (inst *App) {
	t.Helper()
	inst = testApp(t)
	inst.setPic(sampleStore(t, name))
	return
}

// sampleStore is a sample image as a store.
func sampleStore(t *testing.T, name string) *tiles.Store {
	t.Helper()
	img, err := samples.ByName(name)
	require.NoError(t, err)
	s, err := tiles.FromImage(context.Background(), img, tiles.Options{})
	require.NoError(t, err)
	return s
}

// sampleImage is a sample image in memory, to compare a store against.
func sampleImage(t *testing.T, name string) imaging.Image {
	t.Helper()
	img, err := samples.ByName(name)
	require.NoError(t, err)
	return img
}

// settle runs the top of a frame until cond holds.
func settle(t *testing.T, inst *App, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		inst.takeResults()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the window did not settle")
}

func query[In any, Out any](t *testing.T, inst *App, name string, in In) (out Out, err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	raw, err := inst.Operations().Snapshot().Query(name, args)
	if err != nil {
		return
	}
	out, decErr := buscodec.Decode[Out](raw)
	require.NoError(t, decErr)
	return
}

func command[In any](t *testing.T, inst *App, name string, in In) (err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	_, err = inst.Operations().ApplyCommand(app.OperationCall{}, name, args)
	return
}

func refusal(t *testing.T, err error) string {
	t.Helper()
	var r *app.OperationRefusal
	require.True(t, errors.As(err, &r), "expected a refusal, got %v", err)
	return r.Reason
}

// ---- the machine (keelson-app T4) ----

func TestThePhaseFollowsTheImageAndTheRegion(t *testing.T) {
	inst := testApp(t)
	assert.Equal(t, phaseEmpty, inst.observePhase())

	require.True(t, inst.openSample(samples.NameSmall, image.Rectangle{}))
	assert.Equal(t, phaseOpening, inst.observePhase())
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	assert.Equal(t, phaseViewing, inst.observePhase())
	assert.Contains(t, inst.phaseLine(), "synthetic small")
	assert.Empty(t, inst.openNote, "a landed open needs no note beside the phase line")

	inst.viewer.SetRegion(image.Rect(2, 2, 10, 6))
	inst.takeResults()
	assert.Contains(t, []phaseE{phaseMeasuring, phaseMeasured}, inst.observePhase())
	settle(t, inst, func() bool { return inst.measured != nil })
	assert.Equal(t, phaseMeasured, inst.observePhase())

	inst.viewer.SetRegion(image.Rectangle{})
	inst.takeResults()
	assert.Equal(t, phaseViewing, inst.observePhase())
	assert.Nil(t, inst.measured)
}

func TestAnOpenThatFailsReturnsToThePhaseItInterrupted(t *testing.T) {
	boom := errors.New("not an image")
	inst := testApp(t)
	fail := func(err error) {
		require.True(t, inst.startOpen(image.Rectangle{}, func(context.Context, bgjob.Reporter, func(*tiles.Store)) (*tiles.Store, error) {
			return nil, err
		}))
		settle(t, inst, func() bool { return !inst.open.Running() && inst.landedGen == inst.openGen })
	}
	fail(boom)
	assert.Equal(t, phaseEmpty, inst.observePhase())
	assert.Contains(t, inst.openErr, "not an image")

	inst.setPic(sampleStore(t, samples.NameSmall))
	fail(errOpenCancelled)
	assert.Equal(t, phaseViewing, inst.observePhase(), "the image that was shown stays")
	assert.Empty(t, inst.openErr, "choosing no file is not a failure")
	assert.Equal(t, "open cancelled", inst.openNote)
	assert.NotNil(t, inst.pic)
}

func TestEveryPhaseIsReachable(t *testing.T) {
	m := newPhaseMachine()
	for _, ph := range allPhases[1:] {
		assert.True(t, m.CanReach(phaseEmpty, ph), ph.String())
	}
	inst := withSample(t, samples.NameSmall)
	inst.mirrorPhase()
	assert.Equal(t, phaseViewing, inst.phaseMachine.Current(), "an open nobody sampled is a skipped state, not a contradiction")
}

func TestTheImageMachineIsMounted(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))

	h := testApp(t).Operations()
	raw, err := h.Snapshot().Query("image_machine", nil)
	require.NoError(t, err)
	mach, err := buscodec.Decode[opfsm.Machine](raw)
	require.NoError(t, err)
	assert.Len(t, mach.States, len(allPhases))
	raw, err = h.Snapshot().Query("image_state", nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[opfsm.State](raw)
	require.NoError(t, err)
	assert.Equal(t, "empty", st.Current)
	assert.Contains(t, st.Next, opfsm.Edge{From: "empty", To: "opening", Label: "Open, or a sample"})
}

// ---- the catalog ----

func TestOperationsCatalogRegisters(t *testing.T) {
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	for _, name := range []string{opGetView, opReadPixels, opMeasure, opOpenSample, opSetRecipe, opSetPlane, opSetRegion, opSetZoom, opSendToPlay} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
		assert.NotEmpty(t, spec.Summary, name)
	}
	spec, _ := m.Operations.Lookup(opSendToPlay)
	assert.Equal(t, app.OperationEffectConsequential, spec.Effect, "publishing writes outside the app")
	spec, _ = m.Operations.Lookup(opOpenSample)
	assert.True(t, spec.Async, "open_sample settles when the image is shown")
	for _, name := range []string{opSetRecipe, opSetPlane, opSetRegion, opSetZoom} {
		spec, _ = m.Operations.Lookup(name)
		assert.Equal(t, app.OperationEffectView, spec.Effect, name)
	}
}

func TestWithoutAnImageTheViewOperationsRefuseNamingTheWayOn(t *testing.T) {
	inst := testApp(t)
	v, err := query[appops.None, View](t, inst, opGetView, appops.None{})
	require.NoError(t, err)
	assert.Equal(t, "empty", v.Phase)
	assert.False(t, v.Image.Open)

	_, err = query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{})
	assert.Contains(t, refusal(t, err), "open_sample")
	_, err = query[RegionArgs, Measurement](t, inst, opMeasure, RegionArgs{})
	assert.Contains(t, refusal(t, err), "open_sample")
	for _, e := range []error{inst.setRecipe(SetRecipeArgs{}), inst.setPlane(planeAsStored), inst.setRegion(RegionArgs{}), inst.setZoom(zoomFit)} {
		assert.Contains(t, refusal(t, e), "there is no image")
	}
	assert.Contains(t, refusal(t, command(t, inst, opOpenSample, OpenSampleArgs{Name: "nope"})), "the samples are field, colour, small, float, scaled")
}

func TestOpenSampleShowsTheImageAsStored(t *testing.T) {
	inst := testApp(t)
	require.NoError(t, command(t, inst, opOpenSample, OpenSampleArgs{Name: samples.NameColour}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	v, err := query[appops.None, View](t, inst, opGetView, appops.None{})
	require.NoError(t, err)
	assert.Equal(t, "viewing", v.Phase)
	assert.Equal(t, ImageInfo{Open: true, Name: "synthetic colour", Width: 640, Height: 480, Model: "rgba", Kind: "u8",
		Planes: []string{"red", "green", "blue", "alpha"}, SampleHash: v.Image.SampleHash}, v.Image)
	assert.Len(t, v.Image.SampleHash, 64)
	assert.Equal(t, planeAsStored, v.View.Plane)
	assert.Equal(t, limitsNominal, v.View.Recipe.Limits)
	assert.Equal(t, 255.0, v.View.Recipe.Hi)
	assert.Equal(t, RegionArgs{}, v.View.Region)
}

func TestSetRecipeSetsTheWholeRecipeAndNeverToggles(t *testing.T) {
	inst := withSample(t, samples.NameField)
	args := recipeArgs(inst.viewer.Recipe())
	assert.Equal(t, limitsData, args.Limits, "a single plane opens on its data range")
	args.Limits, args.Lo, args.Hi = limitsManual, 1000, 5000
	args.Transfer, args.Invert, args.Palette = "asinh", true, "viridis"
	for range 2 {
		require.NoError(t, command(t, inst, opSetRecipe, args))
		r := inst.viewer.Recipe()
		assert.Equal(t, display.Limits{Lo: 1000, Hi: 5000, Source: display.LimitsSourceManual}, r.Limits)
		assert.Equal(t, display.TransferAsinh, r.Transfer)
		assert.True(t, r.Invert, "a repeat sets it again; it does not toggle")
		assert.Equal(t, "viridis", r.Palette)
		assert.True(t, inst.invert, "the check box follows a recipe set from elsewhere")
	}

	before := inst.viewer.Recipe()
	bad := args
	bad.Transfer = "cubic"
	assert.Contains(t, refusal(t, command(t, inst, opSetRecipe, bad)), "the names are linear, log, sqrt, asinh, gamma")
	bad = args
	bad.Palette = "rainbow"
	assert.Contains(t, refusal(t, command(t, inst, opSetRecipe, bad)), "no colour map named rainbow")
	bad = args
	bad.Hi = 1000
	assert.Equal(t, "manual limits need Hi above Lo", refusal(t, command(t, inst, opSetRecipe, bad)))
	bad = args
	bad.Limits = "auto"
	assert.Equal(t, "limits are data, nominal, percentile or manual", refusal(t, command(t, inst, opSetRecipe, bad)))
	assert.Equal(t, before, inst.viewer.Recipe(), "a refused command changes nothing")

	args.Limits, args.PercentLo, args.PercentHi = limitsPercentile, 1, 99
	require.NoError(t, command(t, inst, opSetRecipe, args))
	t.Cleanup(scenetest.Install())
	settle(t, inst, func() bool {
		inst.viewer.Render(400, 300) // a frame adopts the counted histogram
		c.CurrentApplicationState.StateManager.ScriptReset()
		return inst.viewer.Recipe().Limits.Source == display.LimitsSourcePercentile
	})
	l := inst.viewer.Recipe().Limits
	assert.Less(t, l.Hi, 20000.0, "the saturated column and the blob's peak are outside the 99th percentile")
}

func TestSetPlaneAndTheViewResource(t *testing.T) {
	inst := withSample(t, samples.NameColour)
	args := recipeArgs(inst.viewer.Recipe())
	args.Limits, args.PercentLo, args.PercentHi = limitsPercentile, 1, 99
	assert.Contains(t, refusal(t, command(t, inst, opSetRecipe, args)), "set_plane shows one")

	require.NoError(t, command(t, inst, opSetPlane, SetPlaneArgs{Plane: "green"}))
	i, single := inst.viewer.Plane()
	assert.True(t, single)
	assert.Equal(t, 1, i)
	assert.Contains(t, refusal(t, command(t, inst, opSetPlane, SetPlaneArgs{Plane: "luma"})), "this image has red, green, blue, alpha, and as stored")

	require.NoError(t, command(t, inst, opSetRegion, RegionArgs{Col: 10, Row: 20, Width: 30, Height: 40}))
	vs, ok := inst.Operations().ResourceValue(resView).(ViewState)
	require.True(t, ok)
	assert.Equal(t, "green", vs.Plane)
	assert.Equal(t, RegionArgs{Col: 10, Row: 20, Width: 30, Height: 40}, vs.Region)

	require.NoError(t, command(t, inst, opSetPlane, SetPlaneArgs{Plane: planeAsStored}))
	require.NoError(t, command(t, inst, opSetRegion, RegionArgs{}))
	h := inst.Operations()
	assert.True(t, h.Restore(resView, vs))
	i, single = inst.viewer.Plane()
	assert.True(t, single)
	assert.Equal(t, 1, i)
	r, marked := inst.viewer.Region()
	assert.True(t, marked)
	assert.Equal(t, image.Rect(10, 20, 40, 60), r)
	assert.False(t, h.Restore(resView, "not a view"))

	assert.Contains(t, refusal(t, command(t, inst, opSetRegion, RegionArgs{Col: 600, Row: 0, Width: 100, Height: 10})), "640 columns by 480 rows")
	assert.Equal(t, "zoom is fit or actual", refusal(t, command(t, inst, opSetZoom, SetZoomArgs{Mode: "wide"})))
	require.NoError(t, command(t, inst, opSetZoom, SetZoomArgs{Mode: zoomActual}))
}

func TestReadPixelsIsTheSamplesAndIsBounded(t *testing.T) {
	inst := withSample(t, samples.NameColour)
	colour := sampleImage(t, samples.NameColour)
	// The recipe has no say in what a pixel is.
	args := recipeArgs(inst.viewer.Recipe())
	args.Invert = true
	require.NoError(t, command(t, inst, opSetRecipe, args))

	px, err := query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{Col: 639, Row: 479})
	require.NoError(t, err)
	assert.Equal(t, RegionArgs{Col: 639, Row: 479, Width: 1, Height: 1}, px.Region)
	assert.Equal(t, []string{"red", "green", "blue", "alpha"}, px.Planes)
	for i, name := range px.Planes {
		want, _ := colour.Plane(i).StoredAt(639, 479)
		assert.Equal(t, []float64{want}, px.Values[i], name)
	}
	assert.False(t, px.Truncated)

	px, err = query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{Col: 0, Row: 0, Width: 100, Height: 100})
	require.NoError(t, err)
	assert.True(t, px.Truncated)
	assert.Equal(t, RegionArgs{Width: 100, Height: 10}, px.Region, "whole rows that fit in 1024 pixels")
	assert.Len(t, px.Values[0], 1000)
	want, _ := colour.Plane(0).StoredAt(37, 9)
	assert.Equal(t, want, px.Values[0][9*100+37], "row-major")

	px, err = query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{Col: 630, Row: 470, Width: 50, Height: 50})
	require.NoError(t, err)
	assert.True(t, px.Truncated, "clipped at the image's edge")
	assert.Equal(t, RegionArgs{Col: 630, Row: 470, Width: 10, Height: 10}, px.Region)
	_, err = query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{Col: 700, Row: 0, Width: 2, Height: 2})
	assert.Contains(t, refusal(t, err), "outside the image")
}

func TestMeasureIsOverTheSamplesOfTheRegionAndStatesItsMethod(t *testing.T) {
	inst := withSample(t, samples.NameSmall)
	_, err := query[RegionArgs, Measurement](t, inst, opMeasure, RegionArgs{})
	assert.Contains(t, refusal(t, err), "no region is marked")

	// Samples of the small image are (5·col + 3·row) mod 256; rows 0–1 and
	// columns 0–3 hold 0, 5, 10, 15 and 3, 8, 13, 18.
	m, err := query[RegionArgs, Measurement](t, inst, opMeasure, RegionArgs{Width: 4, Height: 2})
	require.NoError(t, err)
	assert.EqualValues(t, 8, m.Pixels)
	require.Len(t, m.Planes, 1)
	p := m.Planes[0]
	assert.Equal(t, "gray", p.Plane)
	assert.EqualValues(t, 8, p.Samples)
	assert.Equal(t, 0.0, p.Min)
	assert.Equal(t, 18.0, p.Max)
	assert.Equal(t, 9.0, p.Mean)
	assert.InDelta(t, 5.7879, p.StdDev, 1e-4, "the population standard deviation of the eight samples")
	assert.Equal(t, 8.0, p.Median, "the lower of the two middle samples, 8 and 10")
	assert.Equal(t, 0.0, p.P01)
	assert.Equal(t, 18.0, p.P99)
	assert.Contains(t, m.Method, "full resolution")
	assert.Contains(t, m.Method, "divisor n")

	// The marked region is measured by the window and read back unchanged.
	require.NoError(t, command(t, inst, opSetRegion, RegionArgs{Width: 4, Height: 2}))
	settle(t, inst, func() bool { return inst.measured != nil })
	marked, err := query[RegionArgs, Measurement](t, inst, opMeasure, RegionArgs{})
	require.NoError(t, err)
	assert.Equal(t, m, marked)
	v, _ := query[appops.None, View](t, inst, opGetView, appops.None{})
	assert.True(t, v.Measured)
	assert.Equal(t, "measured", v.Phase)

	rec := statsRecord(inst.measured)
	defer rec.Release()
	assert.EqualValues(t, 1, rec.NumRows())
	assert.EqualValues(t, len(inst.statsColumns()), rec.NumCols())
	assert.Equal(t, "17694.9", formatFigure(17694.9343359375), "six significant digits, not fifteen")
	assert.Equal(t, "3347", formatFigure(3347))
	assert.Equal(t, 9.0, rec.Column(5).(*array.Float64).Value(0), "the mean")
	assert.Equal(t, "columns 0–3, rows 0–1 · 4 × 2 = 8 pixels", regionLine(inst.measured.region))
}

// ---- the handover ----

func TestTheHandoverCarriesTheRegionsSamplesInTheirOwnKind(t *testing.T) {
	img, err := samples.ByName(samples.NameField)
	require.NoError(t, err)
	rect := image.Rect(198, 0, 202, 3)
	region, err := sampleStore(t, samples.NameField).Read(context.Background(), rect)
	require.NoError(t, err)
	stream, rows, err := pixelsArrow(region, rect.Min)
	require.NoError(t, err)
	assert.EqualValues(t, 12, rows)
	rd, err := ipc.NewReader(bytes.NewReader(stream))
	require.NoError(t, err)
	defer rd.Release()
	require.True(t, rd.Next())
	rec := rd.RecordBatch()
	assert.EqualValues(t, 12, rec.NumRows())
	assert.Equal(t, []string{"col", "row", "gray"}, []string{rec.ColumnName(0), rec.ColumnName(1), rec.ColumnName(2)})
	cols, rws, gray := rec.Column(0).(*array.Uint32), rec.Column(1).(*array.Uint32), rec.Column(2).(*array.Uint16)
	for i := range 12 {
		x, y := 198+i%4, i/4
		assert.EqualValues(t, x, cols.Value(i))
		assert.EqualValues(t, y, rws.Value(i))
		want, _ := img.Plane(0).StoredAt(x, y)
		assert.EqualValues(t, want, gray.Value(i))
	}
	assert.EqualValues(t, 65535, gray.Value(2), "the saturated column at row 0 arrives as 16 bits")

	sql := handoverSql(datasetAlias, sampleStore(t, samples.NameColour))
	assert.Contains(t, sql, "FROM keelson('pixels')")
	assert.Contains(t, sql, "avg(red)")
	assert.Contains(t, sql, "UNION ALL")
	assert.Contains(t, sql, "quantileExactLow(0.5)(alpha)")
	assert.NotContains(t, sql, "isNaN", "integer samples have no invalid ones")

	sql = handoverSql(datasetAlias, sampleStore(t, samples.NameScaled))
	assert.Contains(t, sql, "avg((gray * 0.01 + -40))", "the statistics are restated in values")
	assert.Contains(t, sql, "count() AS samples")
	assert.Contains(t, handoverSql(datasetAlias, sampleStore(t, samples.NameFloat)), "WHERE NOT isNaN(gray)", "invalid samples are left out, as in the window")
}

func TestTheHandoverRefusesWhatIsTooLargeNamingTheRemedy(t *testing.T) {
	_, err := handoverRegion(nil, image.Rectangle{})
	assert.Contains(t, refusal(t, err), "there is no image")

	img := sampleStore(t, samples.NameField)
	r, err := handoverRegion(img, image.Rectangle{})
	require.NoError(t, err)
	assert.Equal(t, image.Rect(0, 0, 1400, 1000), r, "no region: the whole image")
	r, err = handoverRegion(img, image.Rect(1, 2, 3, 4))
	require.NoError(t, err)
	assert.Equal(t, image.Rect(1, 2, 3, 4), r)

	big, err := imaging.NewPlaneU8(4096, 2048, make([]uint8, 4096*2048))
	require.NoError(t, err)
	bigImg, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{big}, imaging.Provenance{})
	require.NoError(t, err)
	bigStore, err := tiles.FromImage(context.Background(), bigImg, tiles.Options{})
	require.NoError(t, err)
	_, err = handoverRegion(bigStore, image.Rectangle{})
	assert.Equal(t, "the whole image has 8388608 pixels and at most 4194304 are sent; mark a smaller region", refusal(t, err))

	inst := withSample(t, samples.NameSmall)
	assert.Contains(t, refusal(t, command(t, inst, opSendToPlay, appops.None{})), "no bus on this host")
}

// ---- seeds and notes ----

func TestTheRegionSeedIsFourIntegers(t *testing.T) {
	r, err := parseRegionSeed("10, 20,30;40")
	require.NoError(t, err)
	assert.Equal(t, image.Rect(10, 20, 40, 60), r)
	_, err = parseRegionSeed("1,2,3")
	assert.Equal(t, "3 numbers", refusal(t, err))
	_, err = parseRegionSeed("1,2,x,4")
	assert.Equal(t, `"x" is not an integer`, refusal(t, err))
	_, err = parseRegionSeed("1,2,0,4")
	assert.Equal(t, "width and height are positive", refusal(t, err))
}

func TestALossySourceSaysSoBeforeItsNumbers(t *testing.T) {
	p, _ := imaging.NewPlaneU8(2, 2, make([]uint8, 4))
	img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{
		SourceName: "x.jpg", Format: "jpeg", Lossy: true,
		Derivations: imaging.DerivationYCbCrConverted | imaging.DerivationDefaultFrameOnly, ColorDeclaration: "gAMA 0.45455"})
	require.NoError(t, err)
	notes := sourceNotes(img.Provenance())
	require.Len(t, notes, 4)
	assert.Equal(t, noteLossy, notes[0])
	assert.Contains(t, notes[1], "luma and chroma")
	assert.Contains(t, notes[2], "animation")
	assert.Contains(t, notes[3], "gAMA 0.45455")
	assert.Contains(t, notes[3], "not applied")
	pic, err := tiles.FromImage(context.Background(), img, tiles.Options{})
	require.NoError(t, err)
	info := imageInfo(pic)
	assert.True(t, info.Lossy)
	assert.Equal(t, notes, info.Notes)

	assert.Empty(t, sourceNotes(sampleImage(t, samples.NameSmall).Provenance()))
	assert.Empty(t, sourceNotes(imaging.Provenance{}))
}

func TestAFloatRegionCountsItsInvalidSamplesAndAScaledOneIsInValues(t *testing.T) {
	inst := withSample(t, samples.NameFloat)
	// Columns 0–199, rows 280–399 overlap the invalid block, columns 40–119
	// and rows 300–379, in 80 × 80 samples.
	m, err := query[RegionArgs, Measurement](t, inst, opMeasure, RegionArgs{Col: 0, Row: 280, Width: 200, Height: 120})
	require.NoError(t, err)
	assert.EqualValues(t, 24000, m.Pixels)
	assert.EqualValues(t, 6400, m.Planes[0].Invalid)
	assert.EqualValues(t, 24000-6400, m.Planes[0].Samples)
	assert.False(t, math.IsNaN(m.Planes[0].Mean), "the mean is over the valid samples")
	assert.Contains(t, m.Method, "4096 equal bins", "a float plane's percentiles are said to be right to a bin")
	px, err := query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{Col: 50, Row: 310})
	require.NoError(t, err)
	assert.Equal(t, 1, px.Invalid)

	inst = withSample(t, samples.NameScaled)
	m, err = query[RegionArgs, Measurement](t, inst, opMeasure, RegionArgs{Width: 1, Height: 1})
	require.NoError(t, err)
	assert.InDelta(t, 0.0, m.Planes[0].Mean, 1e-9, "stored 4000 is the value 0")
	px, err = query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{})
	require.NoError(t, err)
	assert.InDelta(t, 0.0, px.Values[0][0], 1e-9, "read_pixels gives values")
	info := imageInfo(inst.pic)
	assert.Equal(t, "u16", info.Kind)
}

// The parameter slider follows the recipe, and a value moved on it since the
// last frame becomes the recipe's; a transfer function without a parameter
// draws no slider and sets nothing.
func TestTheParameterSliderAndTheRecipeStayInStep(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	inst := withSample(t, samples.NameFloat)
	args := recipeArgs(inst.viewer.Recipe())
	args.Transfer = "asinh"
	require.NoError(t, command(t, inst, opSetRecipe, args))
	assert.Equal(t, 0.1, inst.param, "asinh's default, as the slider shows it")

	inst.param = 1e-5 // the slider was moved
	inst.renderParamSlider(inst.viewer.Recipe())
	sm.ScriptReset()
	assert.Equal(t, 1e-5, inst.viewer.Recipe().Param)
	assert.Equal(t, 1e-5, inst.param)

	args = recipeArgs(inst.viewer.Recipe())
	args.Transfer, args.Param = "log", 0
	require.NoError(t, command(t, inst, opSetRecipe, args))
	assert.Equal(t, 1000.0, inst.param, "a new transfer function starts from its default")

	args.Transfer = "sqrt"
	require.NoError(t, command(t, inst, opSetRecipe, args))
	before := inst.viewer.Recipe()
	inst.renderParamSlider(before)
	sm.ScriptReset()
	assert.Equal(t, before, inst.viewer.Recipe(), "sqrt takes no parameter")
}

// A store handed over before it is filled is shown at once and fills in
// place; when it lands, the default limits follow its data range, unless
// the person changed the recipe meanwhile; a failure puts back what was
// shown before.
func TestAnImageIsShownWhileItIsRead(t *testing.T) {
	img := sampleImage(t, samples.NameField)
	inst := withSample(t, samples.NameSmall)
	before := inst.pic

	for _, outcome := range []string{"lands", "fails"} {
		inst.setPic(before)
		release := make(chan struct{})
		var b *tiles.Builder
		require.True(t, inst.startOpen(image.Rectangle{}, func(ctx context.Context, _ bgjob.Reporter, arrive func(*tiles.Store)) (*tiles.Store, error) {
			var err error
			b, err = tiles.NewBuilder(img.Width(), img.Height(), img.Model(), img.Kind(), nil, img.Provenance(), tiles.Options{})
			if err != nil {
				return nil, err
			}
			arrive(b.Store())
			<-release
			if outcome == "fails" {
				err = errors.New("the file ended early")
				b.Fail(err)
				return nil, err
			}
			if err = b.Add(img); err != nil {
				return nil, err
			}
			return b.Store(), b.Finish()
		}))
		settle(t, inst, func() bool { return inst.pic != before })
		assert.Equal(t, phaseOpening, inst.observePhase(), "%s: shown while it is read", outcome)
		assert.Equal(t, display.LimitsSourceNominal, inst.viewer.Recipe().Limits.Source, "%s: the data range is not known yet", outcome)
		close(release)
		settle(t, inst, func() bool { return !inst.open.Running() && inst.landedGen == inst.openGen })
		if outcome == "fails" {
			assert.Same(t, before, inst.pic, "what was shown before is put back")
			assert.Contains(t, inst.openErr, "the file ended early")
			continue
		}
		assert.NotSame(t, before, inst.pic)
		assert.Equal(t, display.LimitsSourceData, inst.viewer.Recipe().Limits.Source, "the default follows the data range once it is known")
		assert.Equal(t, img.Hash(), inst.pic.Provenance().SampleHash)
	}

	// Changed by the person while it was read: the recipe stays theirs.
	release := make(chan struct{})
	require.True(t, inst.startOpen(image.Rectangle{}, func(ctx context.Context, _ bgjob.Reporter, arrive func(*tiles.Store)) (*tiles.Store, error) {
		b, err := tiles.NewBuilder(img.Width(), img.Height(), img.Model(), img.Kind(), nil, img.Provenance(), tiles.Options{})
		if err != nil {
			return nil, err
		}
		arrive(b.Store())
		<-release
		if err = b.Add(img); err != nil {
			return nil, err
		}
		return b.Store(), b.Finish()
	}))
	settle(t, inst, func() bool { return inst.observePhase() == phaseOpening && inst.pic != nil && !inst.pic.Finished() })
	args := recipeArgs(inst.viewer.Recipe())
	args.Limits, args.Lo, args.Hi = limitsManual, 100, 200
	require.NoError(t, command(t, inst, opSetRecipe, args))
	close(release)
	settle(t, inst, func() bool { return !inst.open.Running() && inst.landedGen == inst.openGen })
	assert.Equal(t, display.Limits{Lo: 100, Hi: 200, Source: display.LimitsSourceManual}, inst.viewer.Recipe().Limits)
}

// The window renders in every phase: empty, opening, viewing, measured.
// The frame is the render goroutine's, so a nil it reads is a crash of the
// host.
func TestTheWindowRendersInEveryPhase(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	inst := testApp(t)
	frame := func() {
		inst.takeResults()
		inst.mirrorPhase()
		inst.renderBody()
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
	}
	frame()
	require.True(t, inst.openSample(samples.NameField, image.Rect(10, 10, 60, 40)))
	frame()
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	frame()
	settle(t, inst, func() bool { return inst.measured != nil })
	frame()
	assert.Equal(t, phaseMeasured, inst.observePhase())
	inst.setPic(nil)
	frame()
	assert.Equal(t, phaseEmpty, inst.observePhase())
}
