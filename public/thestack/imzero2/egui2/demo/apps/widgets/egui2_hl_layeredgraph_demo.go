package widgets

import (
	"context"
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
)

// Demonstrates the layered-graph widget (ADR-0069): a static directed FSM laid
// out by Graphviz `dot` in-process (WASM, cgo-free) on the Go host, rendered
// through the existing painter binding — no new FFI surface. Deterministic, so
// the demo is registered without DemoFlagNonDeterministic.

const layeredGraphDemoIDBase uint64 = 0xab12cd34ef000000

type layeredGraphDemoState struct {
	engine      *goccyengine.Engine
	layout      *layeredgraph.Layout
	err         error
	initialised bool
	lastClicked string
	gvState     view.ViewState // interactive pan/zoom
}

func init() {
	registry.Register(registry.Demo{
		Name:        "layered-graph",
		Category:    "Charts & plots",
		Title:       "layered graph (dot)",
		Stage:       [2]float32{820, 700},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "Static layered (hierarchical / Sugiyama) graph laid out by Graphviz dot in-process via WASM, drawn through the painter. Hover or click a node.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			return &layeredGraphDemoState{}
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoLayeredGraph(ids, state.(*layeredGraphDemoState))
		},
		SourceFunc: demoLayeredGraph,
	})
}

// trafficLightModel is a small directed FSM with a cycle and a self-loop — the
// canonical directed-flow graph the layered engine targets.
func trafficLightModel() layeredgraph.GraphModel {
	return layeredgraph.GraphModel{
		Nodes: []layeredgraph.Node{
			{ID: "red", Label: "Red", Shape: layeredgraph.NodeShapeEllipse},
			{ID: "green", Label: "Green", Shape: layeredgraph.NodeShapeEllipse},
			{ID: "yellow", Label: "Yellow", Shape: layeredgraph.NodeShapeEllipse},
		},
		Edges: []layeredgraph.Edge{
			{From: "red", To: "green", Label: "go"},
			{From: "green", To: "yellow", Label: "caution"},
			{From: "yellow", To: "red", Label: "stop"},
			{From: "red", To: "red", Label: "flash"},
		},
	}
}

func demoLayeredGraph(ids *c.WidgetIdStack, st *layeredGraphDemoState) {
	// Lazily build the engine + layout the first time the demo renders, so no
	// WASM runtime spins up for a demo that is never opened. The layout is
	// static, so it is computed once and reused.
	if !st.initialised {
		st.initialised = true
		eng, err := goccyengine.New(context.Background())
		if err != nil {
			st.err = err
		} else {
			st.engine = eng
			st.layout, st.err = eng.Layout(context.Background(), trafficLightModel(),
				layeredgraph.LayoutOpts{RankDir: layeredgraph.RankDirTopBottom, FontSize: 14})
		}
	}

	if st.err != nil {
		c.Label("layered-graph error: " + st.err.Error()).Send()
		return
	}
	if st.layout == nil {
		return
	}

	// Responsive width: fill the gallery panel's available width (captured
	// from the previous frame; NaN until the first capture lands).
	sm := c.CurrentApplicationState.StateManager
	avail := sm.GetAvailableSize()
	c.CaptureAvailableSize()
	canvasW := float32(760)
	if avail.W == avail.W && avail.W > 16 { // avail.W == avail.W rejects NaN
		canvasW = avail.W - 8
	}

	res := view.Render(layeredGraphDemoIDBase, st.layout, view.RenderOpts{
		CanvasW: canvasW,
		CanvasH: 440,
		State:   &st.gvState,
	})
	if res.Clicked != "" {
		st.lastClicked = res.Clicked
	}

	hovered := res.Hovered
	if hovered == "" {
		hovered = "—"
	}
	clicked := st.lastClicked
	if clicked == "" {
		clicked = "—"
	}
	c.Label(fmt.Sprintf("hover: %s    last click: %s", hovered, clicked)).Send()
}
