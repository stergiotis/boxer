package fibscope

// fibscope_tour.go enrols two synthetic scenes into the imzero2 demo registry
// (ADR-0057) so the central TestDriver captures them in the widgets tour: the
// tagged-id anatomy (bit strip + split readout + SQL) and the tag-value
// trade-off (advisor + plot + stats table). The live App is a windowed AppI
// with two dock tabs; the tour renders each tab's body directly on a fixed
// starting id so the scenes are deterministic and network-free. Screenshot
// scaffolding only.

import (
	"github.com/stergiotis/boxer/public/identity/fibonacci"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "fibscope-explore",
		Category: "Data",
		Title:    icons.PhBinary + " fibscope — tagged-id anatomy",
		Stage:    [2]float32{980, 540},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "One 64-bit id, coloured by region: the fibonacci-coded tag (blue), its trailing " +
			"11 comma (amber), and the body (green). Below it, the decoded split — tag value, width, " +
			"body, the Zeckendorf sum behind the code, the contiguous same-tag range — and the sargable " +
			"ClickHouse filter the tag folds into. The live app makes the tag, body, and raw id editable.",
		Init:           fibTourInit,
		RenderStateful: fibTourRenderExplore,
		SourceFunc:     (*App).renderExplore,
	})
	registry.Register(registry.Demo{
		Name:     "fibscope-tradeoffs",
		Category: "Data",
		Title:    icons.PhTable + " fibscope — tag-value trade-off",
		Stage:    [2]float32{980, 640},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "The advisor: given the ids expected in the busiest category, the widest tag (most " +
			"categories) that still leaves room. The plot shows body headroom falling and tag capacity " +
			"rising as the tag widens; the stats table lists every code width, with the recommended row " +
			"marked. Screenshot scaffolding for the live fibscope app.",
		Init:           fibTourInit,
		RenderStateful: fibTourRenderTradeoff,
		SourceFunc:     (*App).renderTradeoff,
	})
	registry.Register(registry.Demo{
		Name:     "fibscope-exhaust",
		Category: "Data",
		Title:    icons.PhTable + " fibscope — id-space exhaustion table",
		Stage:    [2]float32{1000, 460},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindUX,
		Description: "The stats table's exhaustion columns: for every code width, how long one tag's body " +
			"space lasts at typical ingress rates (100Hz … 10MHz) — max ids ÷ rate, humanized with " +
			"go-humanize. Narrow tags are effectively inexhaustible (giga-years); a wide tag at MHz rates " +
			"fills in milliseconds. Screenshot scaffolding for the live fibscope app.",
		Init:           fibTourInit,
		RenderStateful: fibTourRenderExhaust,
		SourceFunc:     (*App).renderStatsTable,
	})
}

func fibTourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.ids = ids
	return inst
}

func fibTourRenderExplore(ids *c.WidgetIdStack, state any) {
	if inst, ok := state.(*App); ok && inst != nil {
		inst.ids = ids
		inst.renderExplore()
	}
}

func fibTourRenderTradeoff(ids *c.WidgetIdStack, state any) {
	if inst, ok := state.(*App); ok && inst != nil {
		inst.ids = ids
		inst.renderTradeoff()
	}
}

// fibTourRenderExhaust renders just the stats table (with the exhaustion
// columns) so the tour captures it unclipped, above the fold.
func fibTourRenderExhaust(ids *c.WidgetIdStack, state any) {
	if inst, ok := state.(*App); ok && inst != nil {
		inst.ids = ids
		recWidth := 0
		if lo, _, err := fibonacci.SelectFittingTagValueRange(clampMaxExp(inst.maxExp)); err == nil {
			recWidth = lo.GetTag().GetTagWidth()
		}
		inst.renderStatsTable(recWidth)
	}
}
