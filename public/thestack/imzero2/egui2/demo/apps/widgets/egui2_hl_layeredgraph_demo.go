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

	// multiLine switches to boxes whose labels run one to five lines, the
	// view in which a box too short for its text shows.
	multiLine   bool
	mlLayout    *layeredgraph.Layout
	mlErr       error
	mlViewState view.ViewState
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

// multiLineModel is a chain of boxes whose labels run one to five lines.
func multiLineModel() layeredgraph.GraphModel {
	labels := []string{
		"one line",
		"two\nlines",
		"three\nlines\nhere",
		"four\nlines\nof\ntext",
		"five\nlines\nof\nlabel\ntext",
	}
	var m layeredgraph.GraphModel
	for i, l := range labels {
		id := fmt.Sprintf("l%d", i+1)
		m.Nodes = append(m.Nodes, layeredgraph.Node{ID: id, Label: l})
		if i > 0 {
			m.Edges = append(m.Edges, layeredgraph.Edge{From: fmt.Sprintf("l%d", i), To: id})
		}
	}
	return m
}

func demoLayeredGraphMultiLine(st *layeredGraphDemoState, canvasW float32) {
	if st.mlLayout == nil {
		if eng, err := goccyengine.Shared(); err != nil {
			st.mlErr = err
		} else {
			st.mlLayout, st.mlErr = eng.Layout(context.Background(), multiLineModel(),
				layeredgraph.LayoutOpts{RankDir: layeredgraph.RankDirLeftRight, FontSize: 14})
		}
	}
	if st.mlErr != nil {
		c.Label("layered-graph error: " + st.mlErr.Error()).Send()
		return
	}
	if st.mlLayout == nil {
		return
	}
	view.Render(layeredGraphDemoIDBase+1, st.mlLayout, view.RenderOpts{
		CanvasW: canvasW,
		CanvasH: 440,
		State:   &st.mlViewState,
	})
}

func demoLayeredGraph(ids *c.WidgetIdStack, st *layeredGraphDemoState) {
	c.Checkbox(ids.PrepareStr("multi-line"), st.multiLine, "multi-line labels").SendRespVal(&st.multiLine)
	if st.multiLine {
		availW, _, availOk := c.CapturePaneSize(demoProbeSeq(ids, "layeredgraph-demo", "pane"))
		canvasW := float32(760)
		if availOk && availW > 16 {
			canvasW = availW - 8
		}
		demoLayeredGraphMultiLine(st, canvasW)
		return
	}

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

	// Responsive width: fill the gallery panel's available width, from this
	// demo's own seq-keyed probe (one frame behind; absent until the first
	// lands). Not CaptureAvailableSize — one process-wide slot, last capture of
	// the frame wins, so a gallery showing two probing demos sizes each by the
	// other.
	availW, _, availOk := c.CapturePaneSize(demoProbeSeq(ids, "layeredgraph-demo", "pane"))
	canvasW := float32(760)
	if availOk && availW > 16 {
		canvasW = availW - 8
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
