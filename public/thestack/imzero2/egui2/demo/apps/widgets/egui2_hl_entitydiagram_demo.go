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

// Demonstrates the layered-graph widget drawing an entity diagram in its own
// notation: row nodes for the things with records, a diamond for an n-ary
// relation whose ends are undirected and carry their roles, parallel edges, a
// self-loop, cardinalities as end labels, and a dashed open-headed edge to a
// contract. Deterministic, like the layered-graph demo.

const entityDiagramDemoIDBase uint64 = 0xab12cd34ef100000

type entityDiagramDemoState struct {
	layout      *layeredgraph.Layout
	err         error
	lastClicked string
	gvState     view.ViewState
}

func init() {
	registry.Register(registry.Demo{
		Name:        "entity-diagram",
		Category:    "Charts & plots",
		Title:       "entity diagram (dot)",
		Stage:       [2]float32{1100, 760},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "An entity diagram on the layered-graph widget: row nodes with a title band and banded key rows, a diamond relation hub with undirected role-labelled ends, parallel edges, a self-loop, cardinalities at edge ends, and a dashed open-headed edge to a contract. Laid out by Graphviz dot in-process via WASM. Hover or click a node.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			return &entityDiagramDemoState{}
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoEntityDiagram(ids, state.(*entityDiagramDemoState))
		},
		SourceFunc: demoEntityDiagram,
	})
}

// entityDiagramModel is a small maritime domain: vessels, their owners and
// home ports, an encounter between two vessels at a port, and a contract the
// vessel fulfils.
func entityDiagramModel() layeredgraph.GraphModel {
	return layeredgraph.GraphModel{
		Nodes: []layeredgraph.Node{
			{ID: "owner", Label: "Owner", Subtitle: "entity", Rows: []layeredgraph.Row{
				{ID: "name", Text: "name: Text"},
				{ID: "country", Text: "country: Country"},
			}},
			{ID: "vessel", Label: "Vessel", Subtitle: "entity", Rows: []layeredgraph.Row{
				{ID: "imo", Text: "imo: Number"},
				{ID: "name", Text: "name: Text"},
				{ID: "flag", Text: "flag: Country"},
				{ID: "built", Text: "built: Date"},
			}},
			{ID: "port", Label: "Port", Subtitle: "entity", Rows: []layeredgraph.Row{
				{ID: "unlocode", Text: "unlocode: Text"},
				{ID: "name", Text: "name: Text"},
			}},
			{ID: "encounter", Label: "encounter", Shape: layeredgraph.NodeShapeDiamond},
			{ID: "tracked", Label: "Tracked", Subtitle: "contract"},
		},
		Edges: []layeredgraph.Edge{
			{ID: "owns", From: "owner", To: "vessel", Label: "owns", TailLabel: "1", HeadLabel: "0..*"},
			{ID: "escort", From: "encounter", To: "vessel", HeadLabel: "escort", Head: layeredgraph.EdgeHeadNone},
			{ID: "escorted", From: "encounter", To: "vessel", HeadLabel: "escorted", Head: layeredgraph.EdgeHeadNone},
			{ID: "at", From: "encounter", To: "port", HeadLabel: "at", Head: layeredgraph.EdgeHeadNone},
			{ID: "home", From: "vessel", To: "port", Label: "home port"},
			{ID: "sister", From: "vessel", To: "vessel", Label: "sister ship"},
			{ID: "fulfils", From: "vessel", To: "tracked", Label: "fulfils", Head: layeredgraph.EdgeHeadOpen, Dashed: true},
		},
	}
}

// entityKeyRow bands the rows that identify their entity.
func entityKeyRow(nodeID, rowID string) (col color.Color, ok bool) {
	if (nodeID == "vessel" && rowID == "imo") || (nodeID == "port" && rowID == "unlocode") {
		return color.Hex(styletokens.InfoSubtle.AsHex()), true
	}
	return color.Color{}, false
}

func demoEntityDiagram(ids *c.WidgetIdStack, st *entityDiagramDemoState) {
	if st.layout == nil {
		if eng, err := goccyengine.Shared(); err != nil {
			st.err = err
		} else {
			st.layout, st.err = eng.Layout(context.Background(), entityDiagramModel(),
				layeredgraph.LayoutOpts{RankDir: layeredgraph.RankDirLeftRight, FontSize: 14})
		}
	}
	if st.err != nil {
		c.Label("entity-diagram error: " + st.err.Error()).Send()
		return
	}
	if st.layout == nil {
		return
	}

	availW, _, availOk := c.CapturePaneSize(demoProbeSeq(ids, "entitydiagram-demo", "pane"))
	canvasW := float32(1040)
	if availOk && availW > 16 {
		canvasW = availW - 8
	}

	res := view.Render(entityDiagramDemoIDBase, st.layout, view.RenderOpts{
		CanvasW: canvasW,
		CanvasH: 600,
		RowFill: entityKeyRow,
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
