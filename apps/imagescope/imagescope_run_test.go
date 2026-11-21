package imagescope

import (
	"context"
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	imgops "github.com/stergiotis/boxer/public/science/imaging/ops"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
)

// opened is a window on a sample, settled.
func openedOn(t *testing.T, name string) *App {
	t.Helper()
	inst := testApp(t)
	require.True(t, inst.openSample(name, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	return inst
}

// runToEnd runs an operation through run_operation and settles it.
func runToEnd(t *testing.T, inst *App, args RunArgs) RunOutcome {
	t.Helper()
	require.NoError(t, command(t, inst, opRunOperation, args))
	settle(t, inst, func() bool { return !inst.runJob.Running() && inst.runLanded == inst.runCallGen })
	out, settled, err := settleAsync[RunArgs, RunOutcome](t, inst, opRunOperation, args)
	require.NoError(t, err)
	require.True(t, settled)
	return out
}

// Each operator runs through run_operation on a sample: its result joins
// the window's images, is shown, carries its record, and its record
// replays to the same digest.
func TestEveryOperatorRunsThroughTheCatalog(t *testing.T) {
	for _, tc := range []struct {
		sample string
		args   RunArgs
		planes []string
	}{
		{samples.NameElevation, RunArgs{Operator: "expr", Params: []ParamArg{{Name: "expression", Value: "elevation * 2 - 1"}},
			Inputs: []RunInputArgs{{Name: "elevation"}}}, []string{"elevation * 2 - 1"}},
		{samples.NameElevation, RunArgs{Operator: "gaussian", Params: []ParamArg{{Name: "sigma", Value: "1.5"}, {Name: "border", Value: "nearest"}},
			Inputs: []RunInputArgs{{}}}, []string{"gray"}},
		{samples.NameField, RunArgs{Operator: "median-filter", Params: []ParamArg{{Name: "border", Value: "invalid"}},
			Inputs: []RunInputArgs{{}}, Name: "field median"}, []string{"gray"}},
		{samples.NameField, RunArgs{Operator: "standardize", Inputs: []RunInputArgs{{}}}, []string{"gray"}},
		{samples.StackBands, RunArgs{Operator: "axis-stat", Params: []ParamArg{{Name: "stat", Value: "mean"}},
			Inputs: []RunInputArgs{{Name: "b4", Band: "B4", Over: "time"}}}, []string{"mean"}},
	} {
		inst := openedOn(t, tc.sample)
		out := runToEnd(t, inst, tc.args)
		require.Empty(t, out.Error, tc.args.Operator)
		assert.Equal(t, "r1", out.Image, tc.args.Operator)
		require.NotNil(t, out.Record)
		assert.Contains(t, out.Record.Operator, tc.args.Operator+"@")
		l, err := query[appops.None, ImageList](t, inst, opListImages, appops.None{})
		require.NoError(t, err)
		require.Len(t, l.Images, 2, tc.args.Operator)
		assert.Equal(t, "r1", l.Shown, "a result is shown when it lands")
		assert.Equal(t, tc.planes, l.Images[1].Planes, tc.args.Operator)
		assert.True(t, l.Images[1].Result)
		if tc.args.Name != "" {
			assert.Equal(t, tc.args.Name, l.Images[1].Name)
		}
		v := getView(t, inst)
		assert.Equal(t, "r1", v.ShownImage)
		assert.False(t, v.Running)
		assert.False(t, v.StackSelection, "a result is no stack's selection")

		page, err := query[GetRecordArgs, RecordPage](t, inst, opGetRecord, GetRecordArgs{Id: "r1"})
		require.NoError(t, err)
		assert.False(t, page.Truncated)
		rec, err := imgops.DecodeRecord([]byte(page.Record))
		require.NoError(t, err)
		assert.Equal(t, tc.args.Operator, rec.Operator)

		// Replayed on the same inputs, the record gives the same digest.
		src := inst.runSourceNow()
		src.shownId = openedId // the inputs as they were taken: the image opened was shown
		req, _, err := src.request(context.Background(), tc.args)
		require.NoError(t, err)
		_, match, err := imgops.Replay(context.Background(), rec, req.Inputs, imgops.Options{})
		require.NoError(t, err, tc.args.Operator)
		assert.True(t, match, "%s: the record replays to its digest", tc.args.Operator)
	}
}

// The composite over time of the bands stack takes one input per time, at
// the band named, each recorded with its positions; an expression over two
// bands names them as the stack labels them.
func TestOperationsTakeTheStacksPlanesAtTheirPositions(t *testing.T) {
	inst := openedOn(t, samples.StackBands)
	out := runToEnd(t, inst, RunArgs{Operator: "axis-stat", Params: []ParamArg{{Name: "stat", Value: "max"}},
		Inputs: []RunInputArgs{{Name: "nir", Band: "B8", Over: "time"}}})
	require.Empty(t, out.Error)
	require.Len(t, out.Record.Inputs, 3)
	for i, in := range out.Record.Inputs {
		assert.Equal(t, "nir_"+string(rune('0'+i)), in.Name)
		assert.Contains(t, in.Positions, "band B8 (3)")
	}
	assert.Contains(t, out.Record.Inputs[1].Positions, "time day 16 (1)")

	plan, err := query[RunArgs, PlanInfo](t, inst, opPlanOperation, RunArgs{Operator: "expr",
		Params: []ParamArg{{Name: "expression", Value: "(B8 - B4) / (B8 + B4)"}},
		Inputs: []RunInputArgs{{Image: openedId, Band: "B8"}, {Image: openedId, Band: "B4", Positions: []AnchorPosition{{Axis: "time", Position: 2}}}}})
	require.NoError(t, err)
	assert.Equal(t, []string{"B8", "B4"}, plan.Inputs, "named by their bands' labels")
	assert.Equal(t, 160, plan.Width)
	assert.Equal(t, "f32", plan.Kind, "16-bit inputs are exact in 32-bit floats")

	// The panel opens on the bands shown.
	inst.chooseOperator("expr")
	assert.Len(t, inst.panel.inputs, 1)
	inst.chooseOperator("axis-stat")
	inst.fillOverAxis(0)
	assert.Len(t, inst.panel.inputs, 3)
	assert.Equal(t, "B2", inst.panel.inputs[0].Band, "the band shown")
}

// A plan the operator refuses, a parameter it does not take, and inputs it
// cannot read are refused naming why; Run is withheld with the reason.
func TestAPlanThatCannotRunIsRefusedNamingWhy(t *testing.T) {
	inst := openedOn(t, samples.StackBands)
	for _, tc := range []struct {
		args RunArgs
		want string
	}{
		{RunArgs{Operator: "sharpen"}, "the operators are axis-stat, expr, gaussian, median-filter, standardize"},
		{RunArgs{Operator: "gaussian", Inputs: []RunInputArgs{{}}}, "required and has no default (parameter border; choices invalid, nearest)"},
		{RunArgs{Operator: "gaussian", Params: []ParamArg{{Name: "width", Value: "3"}}, Inputs: []RunInputArgs{{}}}, `its parameters are "sigma", "border"`},
		{RunArgs{Operator: "gaussian", Params: []ParamArg{{Name: "sigma", Value: "wide"}}, Inputs: []RunInputArgs{{}}}, "takes a number"},
		{RunArgs{Operator: "expr", Params: []ParamArg{{Name: "expression", Value: "a +"}}, Inputs: []RunInputArgs{{Name: "a"}}}, "does not parse"},
		{RunArgs{Operator: "expr", Params: []ParamArg{{Name: "expression", Value: "a"}}, Inputs: []RunInputArgs{{Name: "a", Band: "B9"}}}, `no band "B9"; the bands are "B2", "B3", "B4", "B8"`},
		{RunArgs{Operator: "expr", Params: []ParamArg{{Name: "expression", Value: "a"}}, Inputs: []RunInputArgs{{Name: "a", Image: "r7"}}}, "names no image"},
		{RunArgs{Operator: "expr", Params: []ParamArg{{Name: "expression", Value: "a"}}, Inputs: []RunInputArgs{{Name: "a", Over: "depth"}}}, `Over names no selector axis "depth"`},
	} {
		_, err := query[RunArgs, PlanInfo](t, inst, opPlanOperation, tc.args)
		assert.Contains(t, refusal(t, err), tc.want, tc.args.Operator)
		assert.Contains(t, refusal(t, command(t, inst, opRunOperation, tc.args)), tc.want, "run refuses what plan refuses")
	}
	assert.False(t, inst.runJob.Running())

	inst.chooseOperator("gaussian")
	assert.Equal(t, "border is required: choose one", inst.refreshPlan(), "Run waits for the border")
	inst.panel.setParam("border", "nearest")
	assert.Empty(t, inst.refreshPlan())
	assert.Contains(t, inst.panel.plan, "160 × 120 gray f32")

	// Images of two sizes are not on one grid.
	small := openedOn(t, samples.NameSmall)
	small.images = append(small.images, imageEntry{id: "r1", name: "field", store: sampleStore(t, samples.NameField)})
	_, err := query[RunArgs, PlanInfo](t, small, opPlanOperation, RunArgs{Operator: "expr", Params: []ParamArg{{Name: "expression", Value: "a + b"}},
		Inputs: []RunInputArgs{{Name: "a", Image: openedId}, {Name: "b", Image: "r1"}}})
	assert.Contains(t, refusal(t, err), "not on one grid")
}

// A run that would take the results past the cap is refused naming the
// remedy; dropping a result frees room.
func TestTheResultsCapIsRefusedNamingTheRemedy(t *testing.T) {
	inst := openedOn(t, samples.NameElevation)
	args := RunArgs{Operator: "standardize", Inputs: []RunInputArgs{{}}}
	out := runToEnd(t, inst, args)
	require.Empty(t, out.Error)
	held := heldResultBytes(inst.images)
	require.Positive(t, held)
	inst.resultsCap = held + 1000
	assert.Contains(t, refusal(t, command(t, inst, opRunOperation, args)), "drop a result with drop_image")
	assert.Contains(t, refusal(t, command(t, inst, opDropImage, ImageIdArgs{Id: openedId})), "the image opened is not dropped")
	require.NoError(t, command(t, inst, opDropImage, ImageIdArgs{Id: "r1"}))
	assert.Equal(t, openedId, inst.shownId, "a result dropped while shown gives way to the image opened")
	assert.Zero(t, heldResultBytes(inst.images))
	inst.resultsCap = 0
	out = runToEnd(t, inst, args)
	assert.Equal(t, "r2", out.Image, "ids are not reused")
	assert.Contains(t, refusal(t, command(t, inst, opShowImage, ImageIdArgs{Id: "r1"})), "list_images lists them")
	require.NoError(t, command(t, inst, opShowImage, ImageIdArgs{Id: openedId}))
	assert.Equal(t, openedId, getView(t, inst).ShownImage)
	_, err := query[GetRecordArgs, RecordPage](t, inst, opGetRecord, GetRecordArgs{Id: openedId})
	assert.Contains(t, refusal(t, err), "has no record")
}

// A cancelled run leaves nothing, and what was shown before is shown
// again; a second run while one is under way is refused.
func TestACancelledRunLeavesNothing(t *testing.T) {
	inst := openedOn(t, samples.NameField)
	slow := RunArgs{Operator: "gaussian", Params: []ParamArg{{Name: "sigma", Value: "64"}, {Name: "border", Value: "nearest"}}, Inputs: []RunInputArgs{{}}}
	require.NoError(t, command(t, inst, opRunOperation, slow))
	assert.Equal(t, phaseRunning, inst.observePhase())
	assert.Contains(t, refusal(t, command(t, inst, opRunOperation, slow)), "one at a time")
	_, settled, err := settleAsync[RunArgs, RunOutcome](t, inst, opRunOperation, slow)
	require.NoError(t, err)
	assert.False(t, settled)
	require.NoError(t, command(t, inst, opCancelOperation, appops.None{}))
	settle(t, inst, func() bool { return !inst.runJob.Running() && inst.runLanded == inst.runCallGen })
	out, settled, err := settleAsync[RunArgs, RunOutcome](t, inst, opRunOperation, slow)
	require.NoError(t, err)
	require.True(t, settled)
	assert.Empty(t, out.Image)
	assert.Contains(t, out.Error, "cancelled")
	assert.Len(t, inst.images, 1, "nothing was kept")
	assert.Equal(t, openedId, inst.shownId)
	assert.Equal(t, phaseViewing, inst.observePhase())
	assert.Contains(t, refusal(t, command(t, inst, opCancelOperation, appops.None{})), "no run is under way")
}

// A result made from a result carries its record, so the chain back to the
// file is in it; regions measured on a result say so.
func TestAChainOfResultsCarriesItsRecords(t *testing.T) {
	inst := openedOn(t, samples.NameElevation)
	first := runToEnd(t, inst, RunArgs{Operator: "gaussian", Params: []ParamArg{{Name: "sigma", Value: "2"}, {Name: "border", Value: "invalid"}}, Inputs: []RunInputArgs{{}}})
	require.Empty(t, first.Error)
	second := runToEnd(t, inst, RunArgs{Operator: "standardize", Inputs: []RunInputArgs{{Name: "smooth", Image: first.Image}}})
	require.Empty(t, second.Error)
	assert.Equal(t, "gaussian@1", second.Record.Inputs[0].Result)
	page, err := query[GetRecordArgs, RecordPage](t, inst, opGetRecord, GetRecordArgs{Id: second.Image})
	require.NoError(t, err)
	rec, err := imgops.DecodeRecord([]byte(page.Record))
	require.NoError(t, err)
	require.NotNil(t, rec.Inputs[0].Record)
	assert.Equal(t, "gaussian", rec.Inputs[0].Record.Operator)

	id, err := inst.addRegion(rectSpec(100, 100, 20, 20), originPerson)
	require.NoError(t, err)
	require.NoError(t, command(t, inst, opMeasureRegion, MeasureRegionArgs{Id: id}))
	settle(t, inst, func() bool { return !inst.regionJob.Running() && inst.regionLanded == inst.regionCallGen })
	m, settled, err := settleAsync[MeasureRegionArgs, RegionMeasurement](t, inst, opMeasureRegion, MeasureRegionArgs{Id: id})
	require.NoError(t, err)
	require.True(t, settled)
	assert.Equal(t, second.Image, m.Image, "the figures name the result they were measured on")
	assert.Nil(t, m.Selection)
}

// list_operators gives every declared operator with its parameters, enough
// to call run_operation without the help book.
func TestListOperatorsGivesTheDeclarations(t *testing.T) {
	inst := testApp(t)
	l, err := query[appops.None, OperatorList](t, inst, opListOperators, appops.None{})
	require.NoError(t, err)
	ids := make([]string, len(l.Operators))
	for i, o := range l.Operators {
		ids[i] = o.Id
	}
	assert.Equal(t, []string{"axis-stat", "expr", "gaussian", "median-filter", "standardize"}, ids)
	g := l.Operators[2]
	require.Len(t, g.Params, 2)
	assert.Equal(t, OperatorParam{Name: "sigma", Type: "number", Unit: "px", HasRange: true, Min: 0.1, Max: 64, Default: "1",
		Doc: g.Params[0].Doc}, g.Params[0])
	assert.True(t, g.Params[1].Required)
	assert.Contains(t, g.Params[1].Choices[0], "invalid: ")
	assert.Contains(t, l.Functions, "where")
	assert.True(t, l.Operators[0].LeavesInvalidOut)
}

// The window renders the operations section and a result with its record.
func TestTheWindowRendersARunAndItsRecord(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	inst := openedOn(t, samples.StackBands)
	frame := func() {
		inst.takeResults()
		inst.mirrorPhase()
		inst.renderBody()
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
	}
	frame()
	inst.panel.setParam("expression", "(B3 - B2) / (B3 + B2)")
	inst.panel.inputs = []RunInputArgs{{Image: openedId, Band: "B2"}, {Image: openedId, Band: "B3"}}
	frame()
	assert.Empty(t, inst.panel.planErr)
	assert.Equal(t, []string{"B2", "B3"}, inst.panel.names)
	require.NoError(t, inst.startRun(inst.panelArgs()))
	for inst.runJob.Running() || inst.runLanded != inst.runCallGen {
		frame()
	}
	frame()
	assert.Equal(t, "r1", inst.shownId)
	assert.Equal(t, []string{"(B3 - B2) / (B3 + B2)"}, inst.pic.PlaneLabels())
	inst.panel.setParam("expression", "(B3 - ")
	frame()
	assert.Contains(t, inst.panel.exprErr, "column 7")
}

// The regions tour draws the window with its regions measured and an
// operation's result, as the screenshot driver sees it.
func TestTheRegionsTourDraws(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	state := tourRegionsInit(ids)
	inst, ok := state.(*App)
	require.True(t, ok)
	t.Cleanup(func() { _ = inst.Unmount(nil) })
	require.Len(t, inst.regs, 3)
	settle(t, inst, func() bool { return !inst.runJob.Running() && !inst.regionJob.Running() && inst.lastResult != "" })
	for range 3 {
		tourRegionsRender(ids, state)
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
	}
	assert.Equal(t, "r1", inst.shownId)
	assert.Len(t, inst.regMeasures, 3)
}

// Add input takes the stack's next band not taken yet, so a ratio of two
// bands needs no picking.
func TestAddInputTakesTheNextBand(t *testing.T) {
	inst := openedOn(t, samples.StackBands)
	inst.chooseOperator("expr")
	require.Len(t, inst.panel.inputs, 1)
	assert.Equal(t, RunInputArgs{Image: openedId, Band: "B2"}, inst.panel.inputs[0], "the band shown, named, so a result shown later changes nothing")
	inst.panel.inputs = append(inst.panel.inputs, inst.nextInput())
	assert.Equal(t, RunInputArgs{Image: openedId, Band: "B3"}, inst.panel.inputs[1], "B2 is taken by the first row")
	inst.panel.setParam("expression", "(B3 - B2) / (B3 + B2)")
	assert.Empty(t, inst.refreshPlan())
	assert.Equal(t, []string{"B2", "B3"}, inst.panel.names)
}

// A result's provenance notes — what it was run on, what its plan decided —
// are drawn in the neutral tone; a caveat about the samples stays a warning.
func TestAResultsNotesAreNeutralAndACaveatAWarning(t *testing.T) {
	inst := openedOn(t, samples.NameElevation)
	out := runToEnd(t, inst, RunArgs{Operator: "gaussian", Params: []ParamArg{{Name: "sigma", Value: "2"}, {Name: "border", Value: "invalid"}}, Inputs: []RunInputArgs{{}}})
	require.Empty(t, out.Error)
	notes := storeNotes(inst.pic)
	require.NotEmpty(t, notes)
	assert.Contains(t, notes[0], "Operation gaussian@1")
	for _, n := range notes {
		assert.Equal(t, badge.ToneNeutral, noteTone(inst.pic, n), "the operation's own note: %s", n)
	}
	lossy := sampleStore(t, samples.NameSmall)
	assert.Equal(t, badge.ToneWarning, noteTone(lossy, noteLossy))
}

// A multi-plane input that is no stack's offers its planes by name: the
// catalog's Band, which the panel had no picker for.
func TestAPlainMultiPlaneInputOffersItsPlanes(t *testing.T) {
	inst := withSample(t, samples.NameColour)
	labels, ok := inst.planeChoices(&RunInputArgs{})
	require.True(t, ok)
	assert.Equal(t, []string{"red", "green", "blue", "alpha"}, labels)
	_, ok = inst.planeChoices(&RunInputArgs{Image: "r9"})
	assert.False(t, ok, "no such image")
	gray := withSample(t, samples.NameSmall)
	_, ok = gray.planeChoices(&RunInputArgs{})
	assert.False(t, ok, "one plane: nothing to pick")

	// The panel draws the picker, and a plane named runs on that plane.
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	inst.panel.inputs = []RunInputArgs{{Band: "green"}}
	inst.renderSidePanel()
	require.False(t, typed.HasErrors(), typed.GetError())
	sm.ScriptReset()
	res := runToEnd(t, inst, RunArgs{Operator: "gaussian", Params: []ParamArg{{Name: "sigma", Value: "1"}, {Name: "border", Value: "nearest"}}, Inputs: []RunInputArgs{{Band: "green"}}})
	require.Empty(t, res.Error)
	assert.Equal(t, []string{"green"}, getView(t, inst).Image.Planes)
}
