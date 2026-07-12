// Package kanban is an imzero2 widget that renders a board of columns and the
// cards in them, and lets the user move a card between columns (and reorder it
// within a column) with per-card controls. The host owns the data — a flat
// [Model] of [Column]s and [Card]s — and the widget mutates only which column a
// card sits in (and its order), reporting each change as a [Move] the host
// drains to persist.
//
// # Shape
//
// The public surface is a single pure entry point, [Render], taking an [Input]
// that carries the host id stack, the [Model], and the standard FillHost flag
// (see the field doc). This mirrors the schemaview idiom: immediate-mode, no
// retained widget struct. The one deviation immediate mode forces — a board has
// state that must survive frames (which card is selected, the pending-move
// queue) — lives on the caller-owned Model, the sanctioned pattern in this
// codebase (layeredgraph.ViewState, treemap's breadcrumb). Render applies a move
// to the Model on the same frame the button is clicked, so the card visibly
// relocates immediately, and appends a Move for the host.
//
// # Layout
//
// Columns lay out left-to-right inside one board-level ScrollArea (horizontal
// for many columns, vertical for tall ones); each column is a fixed-width lane
// (a panel Frame around a width-pinned Vertical) with a header — title + a count
// badge — over a stack of card Frames. A card Frame senses clicks for selection
// (accent-stroked when selected); a compact control row sits beneath it as a
// footer: ◀ ▶ move to the adjacent column, ▲ ▼ reorder within the column. Edge
// controls are omitted rather than disabled (no ◀ in the first column, no ▶ in
// the last). The controls are a footer, not in-card, because a click-sensed
// Frame wins the pointer over buttons drawn inside it — placed inside, the card
// would select but the buttons would never fire.
//
// # Sub-items
//
// A [Card] may carry a ParentID naming another top-level card (one level only):
// its sub-item. Sub-items are scheduled — placed in a column — independently of
// their parent; moving a parent never moves its children. v1 renders every card
// the same way and only surfaces the link (a "sub-item of …" trailer on a child,
// a "◱ N sub" chip on a parent).
//
// deferred: how sub-items should ultimately *present* on the board is an open
// decision — three candidates are (a) independent badged cards in their own
// columns (what v1 approximates), (b) an in-card sub-status list where only
// parents are board cards, (c) nest-in-parent-until-moved. The flat Model +
// neutral rendering above forecloses none of them; revisit once this lands.
//
// # Dragging
//
// Cards also move by drag-and-drop: grabbing a card body starts a drag (the card
// is accent-stroked and a ghost tracks the pointer), an insertion line marks
// where it will land, and releasing drops it into that column at that position.
// It is pure Go — a drag-sensed card Frame reports the drag flags, GetPointer
// gives the cursor, CaptureUiRect / GetUiRect snapshot the lane and card rects
// (one-frame lag, exact here because the layout is frozen for a drag's
// duration), and the ghost + line paint through PaintAbsoluteOverlay's
// foreground layer. No Rust/IDL codegen was needed. A quick click still selects;
// only a press-and-move starts a drag.
//
// # Scope
//
// Independent per-column vertical scrolling is deferred (the board scrolls
// together) to avoid the width-pinned-column / nested-ScrollArea collapse
// documented in the imzero2 skill; the single board ScrollArea sidesteps it.
package kanban
