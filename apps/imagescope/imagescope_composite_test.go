package imagescope

import (
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
)

// bandsApp is a window on the sample stack, rendered frame by frame so the
// viewer's own work — histograms, zscale samples — lands as in a window.
func bandsApp(t *testing.T) (inst *App, frame func()) {
	t.Helper()
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	inst = testApp(t)
	frame = func() {
		inst.takeResults()
		inst.mirrorPhase()
		inst.renderBody()
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
	}
	require.True(t, inst.openSample(samples.StackBands, image.Rectangle{}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	frame()
	return
}

// settleView runs frames until the composite asked for and any limits
// pending are shown.
func settleView(t *testing.T, inst *App, frame func()) {
	t.Helper()
	settle(t, inst, func() bool {
		frame()
		return !inst.selectionPending() && inst.compAsk == nil && !inst.viewer.LimitsPending()
	})
}

func recipeManual(lo, hi float64) *SetRecipeArgs {
	return &SetRecipeArgs{Limits: limitsManual, Lo: lo, Hi: hi, Transfer: "linear"}
}

// A composite of two bands of a stack takes the selection that holds them,
// shows them in their colours, and reports itself; unlinked, it states so.
func TestACompositeOfTwoBandsOfAStack(t *testing.T) {
	inst, frame := bandsApp(t)
	args := CompositeArgs{Linked: true, Layers: []LayerArgs{{Band: 0, Colour: "green"}, {Band: 3, Colour: "magenta"}}}
	require.NoError(t, command(t, inst, opSetComposite, args))
	assert.NotNil(t, inst.compAsk, "the selection holding B2 and B8 is taken first")
	_, settled, _ := settleComposite(inst, app.OperationCall{})
	assert.False(t, settled)
	settleView(t, inst, frame)
	out, settled, err := settleComposite(inst, app.OperationCall{})
	require.NoError(t, err)
	require.True(t, settled)
	require.NotNil(t, out.Composite)

	v := getView(t, inst)
	assert.Equal(t, modeComposite, v.Mode)
	assert.Equal(t, planeComposite, v.View.Plane)
	assert.Equal(t, []string{"B2", "B8", "B2"}, v.Image.Planes, "two bands, the first repeated: an image has no two-plane colour model but grey and alpha")
	assert.True(t, v.Stack.Shown.Composite)
	assert.Contains(t, v.Stack.Shown.Text, "bands B2, B8 for a composite")
	ci := v.Composite
	require.NotNil(t, ci)
	assert.True(t, ci.Linked)
	assert.Empty(t, ci.Statement)
	require.Len(t, ci.Layers, 2)
	assert.Equal(t, LayerInfo{Index: 1, Label: "B8", Band: 3, Plane: 1, Colour: "magenta", Pixel: "#ff00ff", Recipe: ci.Layers[1].Recipe}, ci.Layers[1])
	joint, ok := dataLimitsOver(inst.pic, []int{0, 1})
	require.True(t, ok)
	for _, l := range ci.Layers {
		assert.Equal(t, limitsData, l.Recipe.Limits)
		assert.Equal(t, joint.Lo, l.Recipe.Lo, "a linked composite over the range of its planes together")
		assert.Equal(t, joint.Hi, l.Recipe.Hi)
	}
	assert.Contains(t, inst.phaseLine(), "composite of B2 in green, B8 in magenta, linked")

	// Unlinked: the recipes each layer has are kept, and the statement is
	// made.
	require.NoError(t, command(t, inst, opSetComposite, CompositeArgs{Layers: []LayerArgs{
		{Band: 0, Colour: "green"}, {Band: 3, Colour: "magenta", Recipe: recipeManual(0, 2000)}}, InHand: 1}))
	settleView(t, inst, frame)
	v = getView(t, inst)
	ci = v.Composite
	assert.False(t, ci.Linked)
	assert.Equal(t, imageview.CompositeUnlinkedText, ci.Statement)
	assert.Equal(t, 1, ci.InHand)
	assert.Equal(t, limitsManual, ci.Layers[1].Recipe.Limits)
	own, _ := dataLimitsOver(inst.pic, []int{0})
	assert.Equal(t, own.Hi, ci.Layers[0].Recipe.Hi, "a layer without a recipe takes its own plane's data range")
	assert.Equal(t, 2000.0, v.View.Recipe.Hi, "the view's recipe is the layer in hand's")

	// set_recipe reaches the layer in hand alone while unlinked.
	require.NoError(t, command(t, inst, opSetRecipe, *recipeManual(10, 900)))
	ci = getView(t, inst).Composite
	assert.Equal(t, 900.0, ci.Layers[1].Recipe.Hi)
	assert.Equal(t, own.Hi, ci.Layers[0].Recipe.Hi)

	// set_layer changes one layer whole, and makes it the layer in hand.
	l0 := ci.Layers[0]
	require.NoError(t, command(t, inst, opSetLayer, SetLayerArgs{Index: 0, InHand: true,
		Layer: LayerArgs{Band: 1, Colour: "cyan", Recipe: &l0.Recipe}}))
	settleView(t, inst, frame)
	ci = getView(t, inst).Composite
	assert.Equal(t, "B3", ci.Layers[0].Label)
	assert.Equal(t, "cyan", ci.Layers[0].Colour)
	assert.Equal(t, 0, ci.InHand)
	assert.Equal(t, 900.0, ci.Layers[1].Recipe.Hi, "the other layer is kept")
}

// The composite's commands refuse what cannot be shown, naming the way on.
func TestTheCompositeCommandsRefuseNamingTheRemedy(t *testing.T) {
	inst, _ := bandsApp(t)
	layers := func(bands ...int) []LayerArgs {
		out := make([]LayerArgs, len(bands))
		for i, b := range bands {
			out[i] = LayerArgs{Band: b, Colour: display.AllLayerColors[i%7].String()}
		}
		return out
	}
	nine := layers(0, 1, 2, 0, 1, 2, 0, 1, 2)
	for _, tc := range []struct {
		args CompositeArgs
		want string
	}{
		{CompositeArgs{}, "at least one layer"},
		{CompositeArgs{Layers: nine}, "9 layers: a composite holds at most 8"},
		{CompositeArgs{Layers: []LayerArgs{{Band: 0, Colour: "orange"}}}, `no layer colour named "orange"; the colours are red, green, blue, cyan, magenta, yellow, white`},
		{CompositeArgs{Layers: []LayerArgs{{Band: 4, Colour: "red"}}}, `layer 0: band 4 is outside "band", whose positions are 0 to 3`},
		{CompositeArgs{Linked: true, Layers: []LayerArgs{{Band: 0, Colour: "red", Recipe: recipeManual(0, 1)}}}, "the composite is linked"},
		{CompositeArgs{Recipe: recipeManual(0, 1), Layers: layers(0)}, "an unlinked one takes a Recipe per layer"},
		{CompositeArgs{Layers: layers(0, 1), InHand: 2}, "InHand 2 is not a layer"},
		{CompositeArgs{Layers: layers(0, 1, 2, 3)}, "four different bands, and this stack's samples declare an invalid value"},
		{CompositeArgs{Layers: []LayerArgs{{Band: 0, Colour: "red", Recipe: &SetRecipeArgs{Limits: "median", Transfer: "linear"}}}}, "layer 0: limits are data, nominal, percentile, zscale or manual"},
	} {
		assert.Contains(t, refusal(t, command(t, inst, opSetComposite, tc.args)), tc.want)
	}
	assert.False(t, inst.selectionPending(), "a refused command asks for nothing")
	assert.Nil(t, inst.compAsk)

	assert.Contains(t, refusal(t, command(t, inst, opSetLayer, SetLayerArgs{Layer: LayerArgs{Colour: "red"}})), "no composite is shown")
	require.NoError(t, command(t, inst, opSetComposite, CompositeArgs{Linked: true, Layers: layers(0, 1, 2)}))
	settle(t, inst, func() bool { return !inst.selectionPending() && inst.compAsk == nil })
	assert.Contains(t, refusal(t, command(t, inst, opSetLayer, SetLayerArgs{Index: 3, Layer: LayerArgs{Colour: "red"}})), "layer 3 is not one of the composite's, which are 0 to 2")
	assert.Contains(t, refusal(t, command(t, inst, opSetLayer, SetLayerArgs{Layer: LayerArgs{Colour: "red", Recipe: recipeManual(0, 1)}})), "set_recipe sets every layer's recipe")
	assert.Contains(t, refusal(t, command(t, inst, opSetRecipe, SetRecipeArgs{Limits: limitsManual, Lo: 0, Hi: 1, Transfer: "linear", FlagClipped: true})), "clipped")

	plain := withSample(t, samples.NameColour)
	assert.Contains(t, refusal(t, command(t, plain, opSetComposite, CompositeArgs{Layers: []LayerArgs{{Plane: 4, Colour: "red"}}})), "plane 4 is outside this image, whose planes are 0 to 3")
}

// A position change keeps the layers, their colours and recipes; limits
// taken from the data are taken again, and the status line says so.
func TestAPositionChangeKeepsTheComposite(t *testing.T) {
	inst, frame := bandsApp(t)
	require.NoError(t, command(t, inst, opSetComposite, CompositeArgs{Layers: []LayerArgs{
		{Band: 2, Colour: "red", Recipe: &SetRecipeArgs{Limits: limitsData, Transfer: "sqrt"}},
		{Band: 3, Colour: "cyan", Recipe: recipeManual(5, 4000)}}}))
	settleView(t, inst, frame)
	before := getView(t, inst).Composite
	require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "time", Position: 2}))
	settleView(t, inst, frame)
	v := getView(t, inst)
	after := v.Composite
	require.NotNil(t, after)
	assert.Equal(t, 2, v.Stack.Shown.Positions[0].Position)
	assert.False(t, after.Linked)
	for i := range after.Layers {
		assert.Equal(t, before.Layers[i].Band, after.Layers[i].Band)
		assert.Equal(t, before.Layers[i].Colour, after.Layers[i].Colour)
		assert.Equal(t, before.Layers[i].Recipe.Transfer, after.Layers[i].Recipe.Transfer)
	}
	assert.Equal(t, before.Layers[1].Recipe, after.Layers[1].Recipe, "manual limits are kept")
	data, ok := dataLimitsOver(inst.pic, []int{0})
	require.True(t, ok)
	assert.Equal(t, data.Hi, after.Layers[0].Recipe.Hi, "data limits are those of the new selection")
	assert.Contains(t, v.SelectionNote, "limits taken again from the data")
}

// zscale and percentile limits of a composite's layer are found off the
// frame, LimitsPending meanwhile, and the clipped shares follow the layer
// in hand once its histogram is counted.
func TestLimitsFoundFromTheDataOfALayer(t *testing.T) {
	inst, frame := bandsApp(t)
	require.NoError(t, command(t, inst, opSetComposite, CompositeArgs{Layers: []LayerArgs{
		{Band: 0, Colour: "green"}, {Band: 1, Colour: "magenta", Recipe: &SetRecipeArgs{Limits: limitsZScale, Transfer: "linear"}}}, InHand: 1}))
	settleView(t, inst, frame)
	settle(t, inst, func() bool { frame(); return inst.clippedInfo() != nil })
	v := getView(t, inst)
	require.Empty(t, v.SelectionError)
	require.NotNil(t, v.Composite)
	l := v.Composite.Layers[1].Recipe
	assert.Equal(t, limitsZScale, l.Limits)
	assert.Equal(t, display.ZScaleContrast, l.ZScaleContrast, "what was used is reported")
	assert.EqualValues(t, display.ZScaleSamples, l.ZScaleSamples)
	img, err := imageOf(inst.stk.stack, inst.stk.shown)
	require.NoError(t, err)
	want, ok := display.ZScaleLimits(img.Plane(1), 0, 0)
	require.True(t, ok)
	assert.Equal(t, want.Lo, l.Lo, "zscale of B3's samples, read from the store's tiles")
	assert.Equal(t, want.Hi, l.Hi)
	require.NotNil(t, v.Clipped, "the layer in hand's histogram is counted by the strip")
	assert.Equal(t, "B3", v.Clipped.Plane)
	assert.True(t, v.Clipped.Exact, "a 16-bit plane is counted code by code")
	assert.Equal(t, imageview.ClippedText(display.ClippedShare{Below: v.Clipped.Below, Above: v.Clipped.Above, Exact: true}), v.Clipped.Text)

	// A direct count of the samples the limits clip.
	var below, above, n int
	p := img.Plane(1)
	for row := range p.Height() {
		for col := range p.Width() {
			x, valid := p.ValueAt(col, row)
			if !valid {
				continue
			}
			n++
			switch {
			case x < l.Lo:
				below++
			case x > l.Hi:
				above++
			}
		}
	}
	assert.InDelta(t, float64(below)/float64(n), v.Clipped.Below, 1e-12)
	assert.InDelta(t, float64(above)/float64(n), v.Clipped.Above, 1e-12)

	require.NoError(t, command(t, inst, opSetRecipe, SetRecipeArgs{Limits: limitsPercentile, PercentLo: 1, PercentHi: 99, Transfer: "linear"}))
	settleView(t, inst, frame)
	v = getView(t, inst)
	assert.False(t, v.LimitsPending)
	assert.Equal(t, limitsPercentile, v.Composite.Layers[1].Recipe.Limits)
	assert.Equal(t, limitsData, v.Composite.Layers[0].Recipe.Limits, "unlinked: the other layer keeps its own")
}

// The view resource carries the composite, its link with it, and
// restoring it shows it again (undo).
func TestTheViewResourceRestoresTheComposite(t *testing.T) {
	inst, frame := bandsApp(t)
	require.NoError(t, command(t, inst, opSetComposite, CompositeArgs{Layers: []LayerArgs{
		{Band: 3, Colour: "red", Recipe: recipeManual(0, 3000)}, {Band: 1, Colour: "blue", Hidden: true}}}))
	settleView(t, inst, frame)
	vs, ok := inst.Operations().ResourceValue(resView).(ViewState)
	require.True(t, ok)
	require.NotNil(t, vs.Composite)
	assert.False(t, vs.Composite.Linked, "the link is in what is exported")
	before := getView(t, inst).Composite

	require.NoError(t, command(t, inst, opSetBands, SetBandsArgs{Bands: []int{2}}))
	settleView(t, inst, frame)
	assert.Nil(t, getView(t, inst).Composite)
	assert.True(t, inst.Operations().Restore(resView, vs))
	settleView(t, inst, frame)
	after := getView(t, inst).Composite
	require.NotNil(t, after)
	assert.Equal(t, before, after)
}

// Three 16-bit bands of a stack without a colour model open over the range
// of their data together, and the status line says so.
func TestThreeBandsOfAStackWithoutAColourModelOpenOverTheirData(t *testing.T) {
	inst, frame := bandsApp(t)
	require.NoError(t, command(t, inst, opSetBands, SetBandsArgs{Bands: []int{2, 1, 0}}))
	settleView(t, inst, frame)
	v := getView(t, inst)
	assert.Equal(t, modeAsStored, v.Mode, "the colour path, as before")
	joint, ok := dataLimitsOver(inst.pic, []int{0, 1, 2})
	require.True(t, ok)
	assert.Equal(t, limitsData, v.View.Recipe.Limits)
	assert.Equal(t, joint.Lo, v.View.Recipe.Lo)
	assert.Equal(t, joint.Hi, v.View.Recipe.Hi)
	assert.Contains(t, v.SelectionNote, "three bands of a stack without a colour model open over the range of their data together")
}

// An image that is no stack's composites its own planes, and the window's
// composite control and its rows draw.
func TestACompositeOfAnImagesPlanes(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	inst := withSample(t, samples.NameColour)
	frame := func() {
		inst.takeResults()
		inst.renderBody()
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
	}
	frame()
	inst.compositeGesture(inst.defaultComposite())
	frame()
	v := getView(t, inst)
	require.NotNil(t, v.Composite)
	require.Len(t, v.Composite.Layers, 3, "red, green and blue; alpha is no layer")
	assert.Equal(t, []string{"red", "green", "blue"}, []string{v.Composite.Layers[0].Label, v.Composite.Layers[1].Label, v.Composite.Layers[2].Label})
	inst.compositeGesture(inst.withLayerAdded(*v.View.Composite))
	frame()
	assert.Len(t, getView(t, inst).Composite.Layers, 4)
	require.NoError(t, command(t, inst, opSetPlane, SetPlaneArgs{Plane: planeAsStored}))
	assert.Equal(t, modeAsStored, getView(t, inst).Mode)
	frame()
}
