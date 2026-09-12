// Package nav is the navigation layer above graphview (ADR-0225): a
// universe of nodes and edges the caller feeds, a small navigation state —
// roots, expansions, hidden nodes, a focus list — and the declaration
// graphview renders, derived from the two. The visible set is a function
// of the universe and the state, never an edit log, so collapse is the
// inverse of expand and hide composes with either (§SD2).
//
//	nv := nav.New(nav.Options{Mode: nav.ModeFocus, FocusRadius: 2})
//	nv.AddNodes(nodes)   // nav.Node{Spec: graphview.NodeSpec{...}, Stub: false}
//	nv.AddEdges(edges)   // graphview.EdgeSpec
//	nv.Focus(rootId, 1)
//	// every frame:
//	ns, es := nv.Declare()
//	gv.Render(ns, es, w, h)
//	for _, ev := range gv.Events() { nv.Apply(ev) }
//	for _, id := range nv.Pending() { load(id) } // then AddNodes / AddEdges
//
// Loading is a request, not a callback (§SD5): a stub the walk would
// expand further lands on Pending, the caller answers with data, and the
// next Declare walks again. Styling is the caller's through Options.Style,
// run per visible node with its relevance and depth (§SD3).
package nav
