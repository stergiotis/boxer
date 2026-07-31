// Package sqleditor is the imzero2 SQL editing surface: a no-wrap monospace
// [c.TextEdit] with a line-number gutter and marks lane beside it, lexical and
// semantic syntax colour, a sparse overlay channel for the embedder's own
// decorations, and multi-statement awareness (which statement the caret is in,
// and what a run-under-cursor would ship).
//
// It was play's editor until ADR-0147 §SD1; the extraction is what lets a
// second SQL surface inherit the affordances rather than re-implement them.
//
// # The seam runs in both directions
//
// *Inwards*, the embedder contributes what only it can know: the bound buffer,
// and a [Decoration] carrying overlay sections and the subquery mark. Nothing
// derivable from buffer-and-caret alone is asked for.
//
// *Outwards*, [Editor.Bind] publishes a [Result] — the buffer, the caret, the
// active statement and its 1-based number, the statement count, the composed
// run buffer, and the entity the caret sits on or inside. The embedder is told,
// not asked. That is what makes a consumer such as play's Docs pane a reader of
// the editor rather than a second implementation of caret analysis.
//
// # Frame order
//
// [Editor.Bind] must run before [Editor.Render], and before the embedder
// composes its [Decoration]:
//
//	res := ed.Bind(sqleditor.Frame{IDSlot: "sql", Value: &buf, Rows: rows})
//	deco := myOverlays(res)          // optional; reads res.Caret, res.Statement
//	ed.Render(ids, deco)
//
// The order is load-bearing rather than stylistic. The caret arrives one frame
// late through the FFI, so Bind is where last frame's packed caret is resolved
// against this frame's buffer; every consumer within the frame — the gutter,
// the overlays, the run gate — must see that one value or they disagree about
// which statement is active. An embedder with nothing to decorate passes the
// zero [Decoration].
//
// # Coordinates
//
// [Frame.Value] may be a suffix view of a larger canonical buffer (play binds
// the residual-only mirror behind its hide-prelude toggle). [Frame.Offset] says
// where the bound buffer starts inside that canonical buffer, and everything
// crossing the seam — [Result.Caret], [Result.Statement], [Decoration]'s spans —
// is in CANONICAL coordinates. The rebasing onto the bound view happens once,
// here, rather than at each consumer: two transforms of one list is one place
// too many to get the elided prefix wrong.
//
// # Alignment contract
//
// No-wrap. The layouter runs at f32::INFINITY wrap width, so a galley row is
// exactly a logical line and the gutter's Nth row is the editor's Nth line by
// construction. Monospace on both sides makes the row height match without
// measuring anything. The gutter is ONE widget rather than a column of labels,
// which would accumulate item_spacing and drift out of step within a screenful.
//
// # Ids
//
// Multi-child, so every embedded widget id derives from [Frame.IDSlot] under
// the caller's own stack (ADR-0013). Two editors in one app need two id slots.
package sqleditor
