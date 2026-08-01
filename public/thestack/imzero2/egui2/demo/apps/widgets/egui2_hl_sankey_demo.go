package widgets

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey"
	sankeyview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey/view"
)

// The flow-widget demo (ADR-0159): a Sankey and an alluvial diagram drawn as
// implot custom items. implot owns the frame — the axes are pinned to the
// unit box the layout emits into, so pan, wheel zoom and box-zoom work on the
// diagram without this demo implementing any of them, and hit tests stay in
// plot space and therefore stay correct at any zoom.
//
// The fill selector switches between the two ribbon routes: one concave
// filled polygon per ribbon, or the whole diagram as a single batched rect
// call. The second also carries a gradient, and is the one that renders in
// the headless scene lane (ADR-0159 SD5).

type sankeyDemoDiagram struct {
	name    string
	diagram sankey.Diagram
	layout  *sankey.Layout
}

type sankeyDemoState struct {
	diagrams   []sankeyDemoDiagram
	diagramIdx int
	fillIdx    int
	gradient   bool
	layers     bool
	hideLabels bool
	selected   sankeyview.Hit
	status     string
	// One Renderer, kept across frames so its scratch buffers survive; one
	// per pane is the contract.
	renderer sankeyview.Renderer
}

var sankeyFillModes = []struct {
	label string
	mode  sankeyview.FillMode
}{
	{"concave polygon", sankeyview.FillPolygon},
	{"batched columns", sankeyview.FillColumns},
}

// sankeyDemoEnergy is an energy balance: three primary sources through a
// thermal plant and a grid into useful work and losses. Conservation holds at
// every interior node, which is what makes the bar heights read as
// subdivisions.
func sankeyDemoEnergy() sankey.Diagram {
	return sankey.Diagram{
		Unit: "PJ",
		Nodes: []sankey.Node{
			{ID: "coal", Label: "coal"},
			{ID: "gas", Label: "gas"},
			{ID: "solar", Label: "solar"},
			{ID: "thermal", Label: "thermal plant"},
			{ID: "grid", Label: "grid"},
			{ID: "industry", Label: "industry"},
			{ID: "homes", Label: "homes"},
			{ID: "transport", Label: "transport"},
			{ID: "losses", Label: "losses"},
		},
		Links: []sankey.Link{
			{Source: "coal", Target: "thermal", Value: 40},
			{Source: "gas", Target: "thermal", Value: 25},
			{Source: "solar", Target: "grid", Value: 15},
			{Source: "thermal", Target: "grid", Value: 39},
			{Source: "thermal", Target: "losses", Value: 26},
			{Source: "grid", Target: "industry", Value: 24},
			{Source: "grid", Target: "homes", Value: 18},
			{Source: "grid", Target: "transport", Value: 8},
			{Source: "grid", Target: "losses", Value: 4},
		},
	}
}

// sankeyDemoCohort is the alluvial case: one population re-labelled at three
// checkpoints, with the caller fixing both the stage and the order so a
// category keeps its slot across the diagram.
func sankeyDemoCohort() sankey.Diagram {
	return sankey.Diagram{
		Mode: sankey.ModeAlluvial,
		Unit: "accounts",
		Nodes: []sankey.Node{
			{ID: "t0.free", Label: "free", Stage: 0, Order: 0},
			{ID: "t0.paid", Label: "paid", Stage: 0, Order: 1},
			{ID: "t1.free", Label: "free", Stage: 1, Order: 0},
			{ID: "t1.paid", Label: "paid", Stage: 1, Order: 1},
			{ID: "t1.gone", Label: "churned", Stage: 1, Order: 2},
			{ID: "t2.free", Label: "free", Stage: 2, Order: 0},
			{ID: "t2.paid", Label: "paid", Stage: 2, Order: 1},
			{ID: "t2.gone", Label: "churned", Stage: 2, Order: 2},
		},
		Links: []sankey.Link{
			{Source: "t0.free", Target: "t1.free", Value: 620},
			{Source: "t0.free", Target: "t1.paid", Value: 90},
			{Source: "t0.free", Target: "t1.gone", Value: 190},
			{Source: "t0.paid", Target: "t1.paid", Value: 260},
			{Source: "t0.paid", Target: "t1.gone", Value: 40},
			{Source: "t1.free", Target: "t2.free", Value: 500},
			{Source: "t1.free", Target: "t2.paid", Value: 60},
			{Source: "t1.free", Target: "t2.gone", Value: 60},
			{Source: "t1.paid", Target: "t2.paid", Value: 320},
			{Source: "t1.paid", Target: "t2.gone", Value: 30},
			{Source: "t1.gone", Target: "t2.gone", Value: 230},
		},
	}
}

func init() {
	registry.Register(registry.Demo{
		Name:        "sankey",
		Category:    "Graphics & canvas",
		Title:       icons.IconGitMerge + " sankey / alluvial flow",
		Stage:       [2]float32{860, 660},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "Flow-quantity diagrams on the implot custom-item lane (ADR-0159): ribbon thickness is the value, through one diagram-wide scale. Sankey mode derives the stage from the graph and relaxes the order within it to reduce crossings; alluvial mode takes both from the caller, so a category keeps its slot across checkpoints. The fill selector switches a ribbon between one concave filled polygon each and the whole diagram as a single batched rect call — the batched route also carries a source-to-target gradient, and is the one that renders in the headless scene lane. Axes are pinned to the layout's unit box, so implot supplies pan, wheel zoom and box-zoom; the layer legend (off by default, since it overlays the diagram) toggles flows, bars and labels. Hover and click resolve in plot space against the same sampled geometry that was drawn: click pins, clicking empty area clears.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &sankeyDemoState{
				selected: sankeyview.NoHit,
				diagrams: []sankeyDemoDiagram{
					{name: "energy balance (sankey)", diagram: sankeyDemoEnergy()},
					{name: "cohort (alluvial)", diagram: sankeyDemoCohort()},
				},
			}
			// The layouts are static, so they are computed once rather than
			// per frame; a live host would memoize on a diagram fingerprint
			// the same way.
			for i := range st.diagrams {
				lay, err := sankey.Compute(st.diagrams[i].diagram, sankey.Options{})
				if err != nil {
					st.status = "layout: " + err.Error()
					continue
				}
				st.diagrams[i].layout = lay
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoSankey(ids, state.(*sankeyDemoState))
		},
		SourceFunc: demoSankey,
	})
}

func demoSankey(ids *c.WidgetIdStack, st *sankeyDemoState) {
	for range c.HorizontalTop().KeepIter() {
		c.Label("Diagram:").Send()
		c.AddSpace(padInner())
		for range c.ComboBox(ids.PrepareStr("sk-diagram-cb"),
			c.WidgetText().Text("diagram").Keep(),
			c.WidgetText().Text(st.diagrams[st.diagramIdx].name).Keep()).KeepIter() {
			for i := range st.diagrams {
				selected := i == st.diagramIdx
				if c.Button(ids.PrepareSeq(uint64(0x5A0000+i)),
					c.Atoms().Text(st.diagrams[i].name).Keep()).
					Selected(selected).
					FrameWhenInactive(!selected).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					st.diagramIdx = i
					st.selected = sankeyview.NoHit // ids do not carry across diagrams
				}
			}
		}
		c.AddSpace(gapSections())

		c.Label("Fill:").Send()
		c.AddSpace(padInner())
		for range c.ComboBox(ids.PrepareStr("sk-fill-cb"),
			c.WidgetText().Text("fill").Keep(),
			c.WidgetText().Text(sankeyFillModes[st.fillIdx].label).Keep()).KeepIter() {
			for i := range sankeyFillModes {
				selected := i == st.fillIdx
				if c.Button(ids.PrepareSeq(uint64(0x5A1000+i)),
					c.Atoms().Text(sankeyFillModes[i].label).Keep()).
					Selected(selected).
					FrameWhenInactive(!selected).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					st.fillIdx = i
				}
			}
		}
		c.AddSpace(gapSections())

		// A single polygon carries one colour, so the gradient is only
		// offered on the batched route.
		if sankeyFillModes[st.fillIdx].mode == sankeyview.FillColumns {
			c.Checkbox(ids.PrepareStr("sk-gradient"), st.gradient, "Gradient").
				SendRespVal(&st.gradient)
			c.AddSpace(gapSections())
		}
		c.Checkbox(ids.PrepareStr("sk-layers"), st.layers, "Layer legend").
			SendRespVal(&st.layers)
		c.AddSpace(gapSections())
		c.Checkbox(ids.PrepareStr("sk-hidelabels"), st.hideLabels, "Hide labels").
			SendRespVal(&st.hideLabels)
	}
	c.AddSpace(padInner())

	cur := &st.diagrams[st.diagramIdx]
	if cur.layout == nil {
		c.LabelAtoms(c.Atoms().Text(st.status).Keep()).Send()
		return
	}

	hover, click, clicked := st.renderer.Show(ids, "flow##sankeydemo", 820, 360, cur.layout, sankeyview.Opts{
		Fill:       sankeyFillModes[st.fillIdx].mode,
		Gradient:   st.gradient,
		Layers:     st.layers,
		HideLabels: st.hideLabels,
		Selected:   st.selected,
	})
	// Any click updates the pin, including one that landed on empty area —
	// that is what clears it.
	if clicked {
		st.selected = click
	}

	c.LabelAtoms(c.Atoms().Text(sankeyDemoStatus(cur, hover, st.selected)).Keep()).Send()
}

// sankeyDemoStatus describes what the pointer is over, falling back to the
// layout's own report — the total, and anything it could not decide for the
// caller.
func sankeyDemoStatus(cur *sankeyDemoDiagram, hover sankeyview.Hit, selected sankeyview.Hit) string {
	lay := cur.layout
	rep := lay.Report
	describe := func(h sankeyview.Hit) string {
		switch {
		case h.Node >= 0:
			n := &lay.Nodes[h.Node]
			return fmt.Sprintf("%s — %.0f %s in, %.0f out", n.Label, n.In, rep.Unit, n.Out)
		case h.Link >= 0:
			l := &lay.Links[h.Link]
			return fmt.Sprintf("%s → %s — %.0f %s (%.1f%% of total)",
				lay.Nodes[l.Source].Label, lay.Nodes[l.Target].Label,
				l.Value, rep.Unit, 100*l.Value/rep.Total)
		}
		return ""
	}
	if s := describe(hover); s != "" {
		return s
	}
	if s := describe(selected); s != "" {
		return "pinned: " + s
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d stages · %.0f %s total · hover a bar or a ribbon, click to pin",
		lay.Stages, rep.Total, rep.Unit)
	if rep.ThinLinks > 0 {
		fmt.Fprintf(&b, " · %d flow(s) too thin to read", rep.ThinLinks)
	}
	if len(rep.NonConserving) > 0 {
		fmt.Fprintf(&b, " · unbalanced: %s", strings.Join(rep.NonConserving, ", "))
	}
	return b.String()
}
