// Package graphview is the live node-and-edge graph widget of ADR-0224:
// random, force-directed (Fruchterman–Reingold, optionally with
// centre gravity) and hierarchical layouts, drawn on the imzero2 painter lane
// with no IDL, Rust or fetcher of its own. It is the Go sibling of the
// egui_graphs-backed `Graph` binding and carries that binding's feature set
// and parameter semantics, so a consumer moves over by renaming types.
//
// The caller declares the full node and edge set every frame (ADR-0224 §SD1);
// the widget retains positions, selection and the camera across frames and
// reconciles the declaration against them. Input is read from the previous
// frame's canvas registers, so events and hover lag one frame like every
// canvas widget. Read [View.Events], [View.Metrics] and the selection
// iterators after [View.Render].
//
//	gv := graphview.New(ids, "my-graph", graphview.Options{Layout: graphview.LayoutForceDirectedCG})
//	// every frame:
//	gv.Render(nodes, edges, w, h)
//	for _, ev := range gv.Events() { … }
//
// Nodes that name the same aura id are drawn over one translucent blob when
// [Options.Auras] is enabled (ADR-0224 §SD11): a per-aura scalar field on a
// screen-space grid, contoured and filled beneath the graph, with an
// optional legend from the shared widgets/legend package.
//
// The force-directed layouts run one of two force models on the same tree
// and integrator (ADR-0230 §SD2): the Fruchterman–Reingold step the package
// shipped with, and a neighbour-embedding step with the t-SNE kernel whose
// one knob, [ForceParams.Exaggeration], moves a declared graph along the
// attraction–repulsion spectrum from t-SNE through UMAP to ForceAtlas2.
// Given the neighbour graph of a feature matrix
// ([github.com/stergiotis/boxer/public/analytics/graph/knn]) it is a
// dimensionality reduction drawn as an interactive graph.
//
// The declaration has two forms (ADR-0232 §SD2). The row form above — a
// [NodeSpec] and an [EdgeSpec] per item, zero meaning unset — suits a
// hand-written graph and a caller that styles one item at a time. The
// columnar form, [NodeColumns] and [EdgeColumns] through [View.RenderColumns],
// suits a caller whose data is already columns: one optional slice per
// field, NaN as the unset value so a declared zero is a zero, and the
// ragged columns in Arrow's list layout. The two reconcile to the same
// retained state and paint the same picture. Slots follow the declaration's
// row order (§SD3), so a caller that declares in ascending id order shares
// the slot order of the analytics engine's CSR and reads its columns with
// no join; [View.PositionColumns] reads the layout back in the same order.
//
// A node or edge may be faded with Opacity and taken out of the pointer's
// reach with NoPick (ADR-0224 §SD14). The two are what a caller spends a
// relevance, a search result or a dimmed background on; the widget's own
// pin, selection and hover paint keeps full strength so a dimmed item still
// shows what it is doing.
//
// Layout parameters keep the meaning they have in egui_graphs 0.31 (MIT), the
// crate whose parameterisation this package re-derives; the two documented
// departures — the canvas-area ideal edge length and deterministic
// placement — are ADR-0224 §SD2.
package graphview
