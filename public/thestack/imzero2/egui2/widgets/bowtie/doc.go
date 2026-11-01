// Package bowtie draws a bow-tie diagram, a semi-retained widget (ADR-0267):
// threats on the left, the prevention barriers each must pass in series, one
// top event in the centre, the mitigation barriers in series after it, and
// the consequences on the right. Hover and click are reported from the
// previous frame's input, the one-frame lag every canvas widget has.
//
// It is a reading of layeredgraph (ADR-0069), not a drawing of its own. The
// bow-tie is a layered graph whose layers are fixed by meaning rather than
// found by the engine, so the widget pins every node with Node.Rank —
// threats, then barrier columns, then the top event, then the mirrored
// right half — and lets Graphviz order the nodes within a layer and route the
// edges. A path with fewer barriers than its side's longest keeps them next
// to the top event, so a column of barriers is the same distance from the top
// event on every path. layeredgraph/view paints the result; a barrier is a
// row node whose title is its name and whose one row is its status.
//
// A barrier's status (StatusE) is drawn as a colour, an icon and a label
// together, never colour alone: the status row is filled with the status's
// semantic tone (styletokens.Tone), carries its Phosphor icon and its label,
// and the legend above the drawing repeats the four as badges. Nodes are
// otherwise neutral; the top event is inverted.
//
// Every node is a hit region (ADR-0149's hit regions, through layeredgraph
// view's RenderOpts.NodeRegion): named for its barrier, path or top event,
// so an accessibility client and a scene find it by name; a Tab stop; and a
// tooltip at the pointer — or beside the node while it is focused — with a
// barrier's status and evidence, a path's barrier count. Enter or Space on a
// focused node is reported as a click on it.
//
// The model is the host's: Model directly, or BuildModel over Row values —
// one per element of a path, the shape a table of bow-tie data has, which is
// what play's Bow-tie pane folds a result into (ADR-0122's 2026-11-01
// update). BuildModel keeps each Row's index on the path or barrier it built,
// so a host can map an Events element back to its own row.
//
// Usage:
//
//	d := bowtie.New(ids, "risk-bowtie", bowtie.Options{})
//	// every frame:
//	ev := d.RenderFill(&model, 900, 420)
//	if ev.Clicked.Kind == bowtie.ElementBarrier { … }
//
// The layout runs Graphviz in process (goccyengine), once per change of the
// model's names, structure or statuses, not per frame. Under wasm the engine
// is unavailable and the widget draws the reason instead.
package bowtie
