package bowtie

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
)

// canvasHandle derives the handle of the canvas a Diagram built with key on
// ids draws into: the view's own stack, seeded from the diagram's scope.
func canvasHandle(ids *c.WidgetIdStack, key string) (h widgethandle.WidgetHandle) {
	var base uint64
	for range c.IdScope(ids.PrepareStr(key)) {
		base = ids.PrepareStr("canvas").Derive()
	}
	wis := c.NewWidgetIdStack()
	for range c.IdScope(wis.PrepareHighEntropy(base)) {
		h = widgethandle.Make(wis.PrepareStr("canvas").Derive())
	}
	return
}

// regionHandle derives the handle of the hit region the view declares for
// node id: under the canvas's stack, in its "regions" scope, keyed on the id.
func regionHandle(ids *c.WidgetIdStack, key, id string) (h widgethandle.WidgetHandle) {
	var base uint64
	for range c.IdScope(ids.PrepareStr(key)) {
		base = ids.PrepareStr("canvas").Derive()
	}
	wis := c.NewWidgetIdStack()
	for range c.IdScope(wis.PrepareHighEntropy(base)) {
		for range c.IdScope(wis.PrepareStr("regions")) {
			h = widgethandle.Make(wis.PrepareStr(id).Derive())
		}
	}
	return
}

// centreOf is where the view paints a node's centre in a w×h canvas at the
// fitted view (no pan, no zoom): the layout scaled uniformly with a 6 %
// margin each side and centred, as view.fit does.
func centreOf(lay *layeredgraph.Layout, id string, w, h float32) (x, y float32) {
	scale := math.Min(float64(w)/lay.Width, float64(h)/lay.Height) * (1 - 2*0.06)
	offX := (float64(w) - lay.Width*scale) / 2
	offY := (float64(h) - lay.Height*scale) / 2
	for _, n := range lay.Nodes {
		if n.ID == id {
			return float32(n.Center.X*scale + offX), float32(n.Center.Y*scale + offY)
		}
	}
	return
}

func TestRenderFromAFreshDiagram(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	d := New(ids, "", Options{})
	ev := d.Render(prototypeModel(t), 900, 420)
	require.False(t, typed.HasErrors(), typed.GetError())
	require.NoError(t, ev.Err)
	assert.Equal(t, ElementNone, ev.Hovered.Kind)
	assert.Equal(t, -1, ev.Clicked.Path)
	require.NotNil(t, d.layout)

	// Nil and empty models draw a message and no layout.
	d2 := New(ids, "other", Options{})
	ev = d2.Render(nil, 900, 420)
	require.NoError(t, ev.Err)
	ev = d2.Render(&Model{}, 900, 420)
	require.NoError(t, ev.Err)
	assert.Nil(t, d2.layout)
}

type failingEngine struct{}

func (failingEngine) Layout(context.Context, layeredgraph.GraphModel, layeredgraph.LayoutOpts) (*layeredgraph.Layout, error) {
	return nil, errors.New("no engine here")
}

func TestLayoutFailureIsReportedNotPanicked(t *testing.T) {
	t.Cleanup(scenetest.Install())
	d := New(c.NewWidgetIdStack(), "fail", Options{Engine: failingEngine{}})
	ev := d.Render(prototypeModel(t), 900, 420)
	require.Error(t, ev.Err)
	assert.Contains(t, ev.Err.Error(), "no engine here")
}

// TestHoverAndClickABarrier scripts the pointer onto a barrier: the frame
// reports it hovered and clicked, and the hit region it declares carries its
// name and evidence.
func TestHoverAndClickABarrier(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	const key, w, h = "hover", 900, 420
	d := New(ids, key, Options{NoPanZoom: true, HideLegend: true})
	m := prototypeModel(t)

	d.Render(m, w, h)
	require.NotNil(t, d.layout)
	sm.ScriptReset()

	x, y := centreOf(d.layout, "t2b1", w, h) // Session recording and review
	canvas := canvasHandle(ids, key)
	nan := float32(math.NaN())
	sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{OriginX: 0, OriginY: 0, PosX: nan, PosY: nan})
	sm.ScriptPointer(c.PointerValue{X: x, Y: y, Valid: true})
	sm.ScriptResponse(canvas, c.ContainsPointerResponseFlags|c.PrimaryClickedResponseFlags)
	ev := d.Render(m, w, h)
	require.False(t, typed.HasErrors(), typed.GetError())
	want := Element{Kind: ElementBarrier, Side: SidePrevention, Path: 2, Barrier: 1}
	assert.Equal(t, want, ev.Hovered)
	assert.Equal(t, want, ev.Clicked)
	r, ok := regionFor(m, ev.Hovered)
	require.True(t, ok)
	assert.Equal(t, "Session recording and review", r.Label)
	assert.Contains(t, r.Tooltip, "Tested, failed")
	assert.Contains(t, r.Tooltip, "3 of 10 sessions unrecorded")

	// The pointer leaves: nothing hovered.
	sm.ScriptReset()
	ev = d.Render(m, w, h)
	assert.Equal(t, ElementNone, ev.Hovered.Kind)
	_, ok = regionFor(m, ev.Hovered)
	assert.False(t, ok)
}

// TestKeyboardActivationIsAClick: Enter or Space on a focused node comes
// back as its hit region's click, with no pointer on the canvas, and the
// Diagram reports it like a pointer click.
func TestKeyboardActivationIsAClick(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	const key = "keys"
	d := New(ids, key, Options{NoPanZoom: true, HideLegend: true})
	m := prototypeModel(t)
	d.Render(m, 900, 420)
	sm.ScriptReset()

	sm.ScriptResponse(regionHandle(ids, key, "t2b1"), c.HasFocusResponseFlags|c.PrimaryClickedResponseFlags)
	ev := d.Render(m, 900, 420)
	require.False(t, typed.HasErrors(), typed.GetError())
	assert.Equal(t, ElementNone, ev.Hovered.Kind)
	assert.Equal(t, Element{Kind: ElementBarrier, Side: SidePrevention, Path: 2, Barrier: 1}, ev.Clicked)
}

func TestRegionText(t *testing.T) {
	m := prototypeModel(t)
	r, ok := regionFor(m, Element{Kind: ElementTopEvent, Path: -1, Barrier: -1})
	assert.True(t, ok)
	assert.Equal(t, view.NodeRegion{Label: "Ransomware encrypts core ERP", Tooltip: "top event", Focusable: true}, r)
	r, _ = regionFor(m, Element{Kind: ElementPath, Side: SideMitigation, Path: 1, Barrier: -1})
	assert.Equal(t, "Notification deadline missed", r.Label)
	assert.Equal(t, "consequence · 1 barrier in series", r.Tooltip)
	_, ok = regionFor(m, Element{Kind: ElementPath, Side: SideMitigation, Path: 9, Barrier: -1})
	assert.False(t, ok)
	m.Threats[0].Barriers[0].Evidence = ""
	r, _ = regionFor(m, Element{Kind: ElementBarrier, Path: 0, Barrier: 0})
	assert.Contains(t, r.Tooltip, "no evidence recorded")
}
