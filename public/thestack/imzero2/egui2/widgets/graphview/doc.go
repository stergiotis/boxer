// Package graphview is the live node-and-edge graph widget of ADR-0224
// (proposed): random, force-directed (Fruchterman–Reingold, optionally with
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
// Layout parameters keep the meaning they have in egui_graphs 0.31 (MIT), the
// crate whose parameterisation this package re-derives; the two documented
// departures — the canvas-area ideal edge length and deterministic
// placement — are ADR-0224 §SD2.
package graphview
