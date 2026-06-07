package widgets

import (
	"context"
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
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
	layout      *layeredgraph.Layout
	err         error
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
		Description: "Static layered (hierarchical / Sugiyama) graph laid out by Graphviz dot in-process via WASM, drawn through the painter. Nodes are filled with IDS semantic tones (error/warning/success). Hover or click a node.",
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

// idsNodeFill colours each traffic-light state with its IDS semantic tone,
// demonstrating the view's per-node fill override (RenderOpts.NodeFill). The
// vivid *Default tones are used (the recognisable error/warning/success
// colours); idsNodeStyle flips the label ink dark to stay legible on them.
// Red→Error, Green→Success, Yellow→Warning; unknown ids keep the style default.
func idsNodeFill(id string) (col color.Color, ok bool) {
	switch id {
	case "red":
		return color.Hex(styletokens.ErrorDefault.AsHex()), true
	case "green":
		return color.Hex(styletokens.SuccessDefault.AsHex()), true
	case "yellow":
		return color.Hex(styletokens.WarningDefault.AsHex()), true
	}
	return color.Color{}, false
}

// idsNodeStyle is DefaultStyle with the node label ink darkened to the near-black
// NeutralBgExtreme: the *Default semantic fills are light foreground tones, so
// the default light NodeText would wash out on them. IDS has no on-colour text
// token, so the darkest neutral is the standard ink for a light semantic surface.
func idsNodeStyle() view.Style {
	st := view.DefaultStyle()
	st.NodeText = color.Hex(styletokens.NeutralBgExtreme.AsHex())
	return st
}

func demoLayeredGraph(ids *c.WidgetIdStack, st *layeredGraphDemoState) {
	// Lazily lay out the (static) graph the first time the demo renders — no
	// WASM runtime spins up for a demo that's never opened — using the
	// process-shared engine. Retried while no layout exists, so a transient
	// engine failure recovers instead of sticking.
	if st.layout == nil {
		if eng, err := goccyengine.Shared(); err != nil {
			st.err = err
		} else {
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
		Style:    idsNodeStyle(),
		CanvasW:  canvasW,
		CanvasH:  440,
		NodeFill: idsNodeFill,
		State:    &st.gvState,
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
