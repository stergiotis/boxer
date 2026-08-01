package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pipelineview"
	pview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pipelineview/view"
)

// Demonstrates the pipeline-schematic widget (ADR-0119): a shell-style
// pipeline laid out by grid recursion over its series/parallel stage tree —
// spine left-to-right, stderr below, config above, written artifacts as
// glyph leaves — rendered through the existing painter. Deterministic, so
// the demo is registered without DemoFlagNonDeterministic.

const pipelineViewDemoIDBase uint64 = 0x91f3a57bd2000000

type pipelineViewDemoState struct {
	layout      *pipelineview.Layout
	err         error
	lastClicked string
	volumes     bool
}

func init() {
	registry.Register(registry.Demo{
		Name:        "pipelineview",
		Category:    "Charts & plots",
		Title:       "pipeline schematic",
		Stage:       [2]float32{980, 560},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "Schematic pipeline (`a | b | c` with side ports): stages on a straight spine, stderr dropping south, config entering north, written files as document glyphs, plus a skip edge (top lane) and a dashed feedback edge (bottom lane). Layout is deterministic grid recursion over the stage tree — no graph engine. The volume overlay (ADR-0119 §SD5) thickens each edge by the bytes it carries, on a square-root curve: this is a schematic, not a Sankey, so the width orders and emphasises rather than measuring — and an edge with no volume keeps its default width, since 0 means unknown. Hover or click a node.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			return &pipelineViewDemoState{volumes: true}
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoPipelineView(ids, state.(*pipelineViewDemoState))
		},
		SourceFunc: demoPipelineView,
	})
}

// shellPipelineModel is the canned ADR-0119 M2 pipeline: a four-column spine
// with one parallel group, a north config file, south stderr/artifact
// leaves, an east store sink, a labelled skip edge and a dashed feedback
// edge. The same model is pinned by the layout golden test.
func shellPipelineModel() pipelineview.Pipeline {
	return pipelineview.Pipeline{
		Root: pipelineview.Group{Children: []pipelineview.Element{
			pipelineview.Stage{ID: "fetch", Label: "fetch (curl)", Ports: []pipelineview.Port{
				{Name: "auth", Class: pipelineview.PortConfig},
			}},
			pipelineview.Stage{ID: "decompress", Label: "gunzip"},
			pipelineview.Group{Par: true, Children: []pipelineview.Element{
				pipelineview.Stage{ID: "transform", Label: "transform (jq)", Ports: []pipelineview.Port{
					{Name: "stderr", Class: pipelineview.PortDiagnostic},
					{Name: "rejects", Class: pipelineview.PortArtifact},
				}},
				pipelineview.Stage{ID: "stats", Label: "stats (awk)"},
			}},
			pipelineview.Stage{ID: "load", Label: "load (ch-client)", Ports: []pipelineview.Port{
				{Name: "stderr", Class: pipelineview.PortDiagnostic},
			}},
		}},
		Endpoints: []pipelineview.Endpoint{
			{ID: "netrc", Label: "~/.netrc", Kind: pipelineview.EndpointFile},
			{ID: "errlog", Label: "errors.log", Kind: pipelineview.EndpointFile},
			{ID: "rejfile", Label: "rejects.jsonl", Kind: pipelineview.EndpointFile},
			{ID: "journald", Label: "journald", Kind: pipelineview.EndpointStream},
			{ID: "warehouse", Label: "warehouse", Sublabel: "localhost:9000", Kind: pipelineview.EndpointStore},
		},
		Edges: []pipelineview.Edge{
			{From: pipelineview.Ref{Endpoint: "netrc"}, To: pipelineview.Ref{Stage: "fetch", Port: "auth"}},
			{From: pipelineview.Ref{Stage: "transform", Port: "stderr"}, To: pipelineview.Ref{Endpoint: "errlog"}, Volume: 12 << 10},
			{From: pipelineview.Ref{Stage: "transform", Port: "rejects"}, To: pipelineview.Ref{Endpoint: "rejfile"}, Label: "rejected rows", Volume: 44 << 20},
			{From: pipelineview.Ref{Stage: "load", Port: "stderr"}, To: pipelineview.Ref{Endpoint: "journald"}, Volume: 3 << 10},
			{From: pipelineview.Ref{Stage: "load"}, To: pipelineview.Ref{Endpoint: "warehouse"}, Volume: 1 << 30},
			// Declaring an adjacency explicitly routes exactly as the implied
			// spine edge does, but carries a volume — the only way to weight
			// the spine, since implied edges have no caller to take one from.
			{From: pipelineview.Ref{Stage: "fetch"}, To: pipelineview.Ref{Stage: "decompress"}, Volume: 512 << 20},
			{From: pipelineview.Ref{Stage: "decompress"}, To: pipelineview.Ref{Stage: "transform"}, Volume: 3 << 30},
			{From: pipelineview.Ref{Stage: "decompress"}, To: pipelineview.Ref{Stage: "stats"}, Volume: 3 << 30},
			{From: pipelineview.Ref{Stage: "transform"}, To: pipelineview.Ref{Stage: "load"}, Volume: 980 << 20},
			{From: pipelineview.Ref{Stage: "stats"}, To: pipelineview.Ref{Stage: "load"}, Volume: 2 << 20},
			{From: pipelineview.Ref{Stage: "fetch"}, To: pipelineview.Ref{Stage: "load"}, Label: "manifest"},
			{From: pipelineview.Ref{Stage: "load"}, To: pipelineview.Ref{Stage: "fetch"}, Label: "retry"},
		},
	}
}

func demoPipelineView(ids *c.WidgetIdStack, st *pipelineViewDemoState) {
	// The model is static; lay it out once. Retried while nil so a transient
	// error (there are none for the canned model, but the seam is uniform
	// with the layeredgraph demo) recovers instead of sticking.
	if st.layout == nil && st.err == nil {
		st.layout, st.err = pipelineview.Compute(shellPipelineModel(), pipelineview.LayoutOpts{})
	}
	if st.err != nil {
		c.Label("pipelineview error: " + st.err.Error()).Send()
		return
	}
	if st.layout == nil {
		return
	}

	c.Checkbox(ids.PrepareStr("pv-volumes"), st.volumes,
		"Volume overlay (edge width by bytes carried)").SendRespVal(&st.volumes)
	c.AddSpace(padInner())

	// Responsive width: fill the gallery panel's available width (captured
	// from the previous frame; NaN until the first capture lands).
	sm := c.CurrentApplicationState.StateManager
	avail := sm.GetAvailableSize()
	c.CaptureAvailableSize()
	canvasW := float32(940)
	if avail.W == avail.W && avail.W > 16 { // avail.W == avail.W rejects NaN
		canvasW = avail.W - 8
	}

	// Clicking a stage selects it: the NodeFill/NodeText hooks tint the
	// selection, the same seam a host uses for running/failed status.
	selected := st.lastClicked
	var edgeWidth func(from string, to string, volume float64) (float32, bool)
	if st.volumes {
		edgeWidth = pview.VolumeWidth(st.layout, 0, 0)
	}
	res := pview.Render(pipelineViewDemoIDBase, st.layout, pview.RenderOpts{
		CanvasW:   canvasW,
		CanvasH:   330,
		EdgeWidth: edgeWidth,
		NodeFill: func(id string) (col color.Color, ok bool) {
			if id == selected {
				return color.Hex(styletokens.AccentDefault.AsHex()), true
			}
			return color.Color{}, false
		},
		NodeText: func(id string) (col color.Color, ok bool) {
			if id == selected {
				return color.Hex(styletokens.NeutralBgExtreme.AsHex()), true
			}
			return color.Color{}, false
		},
	})
	if res.Clicked != "" {
		if res.Clicked == st.lastClicked {
			st.lastClicked = "" // click again to deselect
		} else {
			st.lastClicked = res.Clicked
		}
	}

	hovered := res.Hovered
	if hovered == "" {
		hovered = "—"
	}
	sel := st.lastClicked
	if sel == "" {
		sel = "—"
	}
	c.Label(fmt.Sprintf("hover: %s    selected: %s", hovered, sel)).Send()
}
