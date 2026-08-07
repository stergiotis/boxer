package writingstylescope

// writingstylescope_tour.go enrols three scenes into the imzero2 demo registry
// (ADR-0057) so the central TestDriver captures them in the widgets tour: the
// paste panes with the document-level headline, the section-by-section
// heatmap, and the distribution that calibrates it. The live App is a windowed
// AppI with three dock tabs; the tour renders each tab's body directly against
// the built-in example pair, so the scenes are deterministic and free of I/O.
// Screenshot scaffolding only.

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "writingstylescope-documents",
		Category: "Data",
		Title:    icons.PhGitDiff + " writingstylescope — two documents in",
		Stage:    [2]float32{980, 620},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "Two pasted Markdown documents and the sweep's headline. Each is split into " +
			"sections — one per heading, holding that heading's own text — and every A section is " +
			"compared against every B section by compressing the pair together. The readout below " +
			"carries the document-level numbers from stylometry's profile and instance modes, which " +
			"compare the documents as wholes and are deliberately blind to a single copied section.",
		Init:           tourInit,
		RenderStateful: tourRenderDocuments,
		SourceFunc:     (*App).renderDocuments,
	})
	registry.Register(registry.Demo{
		Name:     "writingstylescope-matrix",
		Category: "Data",
		Title:    icons.PhGitDiff + " writingstylescope — section cross-matrix",
		Stage:    [2]float32{980, 860},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "Normalized compression distance for every section pair, as an implot heatmap: " +
			"rows are the first document's sections, columns the second's, and bright is a low " +
			"distance. The colour range spans this matrix's own extent, so brightness is relative to " +
			"these two documents. In the built-in example one section was copied between them and " +
			"reads as the single bright cell; the table below ranks the closest pairs, and hands the " +
			"whole matrix to the SQL playground as an ad-hoc dataset — disabled here, since the tour " +
			"host wires no bus.",
		Init:           tourInit,
		RenderStateful: tourRenderMatrix,
		SourceFunc:     (*App).renderMatrix,
	})
	registry.Register(registry.Demo{
		Name:     "writingstylescope-distribution",
		Category: "Data",
		Title:    icons.PhGitDiff + " writingstylescope — the background distribution",
		Stage:    [2]float32{980, 660},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "The ECDF of every pairwise distance, with a confidence band and hover readout. " +
			"The bulk of the curve is what 'unrelated' looks like for this subject matter at these " +
			"section lengths; the copied pair is the point detached from it on the left. No threshold " +
			"is asserted — NCD has no absolute scale that survives a change of corpus, so the pair's " +
			"own background is the only honest reference.",
		Init:           tourInit,
		RenderStateful: tourRenderDistribution,
		SourceFunc:     (*App).renderDistribution,
	})
}

// tourInit builds an app on the example pair and runs its sweep eagerly, so
// the first captured frame already has a matrix to draw rather than the
// "no result yet" placeholder.
func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.ids = ids
	inst.runPending()
	return inst
}

func tourRenderDocuments(ids *c.WidgetIdStack, state any) {
	if inst, ok := state.(*App); ok && inst != nil {
		inst.ids = ids
		inst.renderDocuments()
	}
}

func tourRenderMatrix(ids *c.WidgetIdStack, state any) {
	if inst, ok := state.(*App); ok && inst != nil {
		inst.ids = ids
		inst.renderMatrix()
	}
}

func tourRenderDistribution(ids *c.WidgetIdStack, state any) {
	if inst, ok := state.(*App); ok && inst != nil {
		inst.ids = ids
		inst.renderDistribution()
	}
}
