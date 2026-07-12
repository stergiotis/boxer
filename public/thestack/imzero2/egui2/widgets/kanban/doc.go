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
// # Sub-items and grouping
//
// A [Card] may carry a ParentID naming another top-level card (one level only):
// its sub-item. Sub-items are scheduled — placed in a column — independently of
// their parent; moving a parent never moves its children. A parent shows a
// rollup pill (◱ k/n) counting how many children sit in a done column
// ([Column.IsDone], or the last column when none is flagged).
//
// How sub-items *present* is an [Input.Group] view mode over this one flat
// model, not a fixed layout — the resolution of what was an open A/B/C question.
// GroupNone lays every card out flat (children as ordinary cards with a
// "sub-item of …" trailer). GroupByParent stacks a swimlane per parent — the
// parent as the lane header (its rollup + own-status chip), its children in the
// columns — plus a Standalone lane for childless top-level cards. GroupByField
// generalizes the swimlane to any caller-supplied key ([Input.GroupField] — an
// owner, priority, label): one lane per distinct value, plus an Unassigned lane.
// The grouping attribute stays caller-side (a closure over the caller's own
// data, keyed by card id), so Card itself stays lean. This mirrors how issue
// trackers model hierarchy and swimlanes: first-class items + a grouping axis +
// a rollup, rather than competing card layouts.
//
// # Dots and legend
//
// A [Card] may also carry up to 3 Dots: [DotTally] entries, each naming a
// [DotKind.ID] in the board's [Model.DotLegend] plus a repeat Count. Rendered
// along the card's bottom edge as a packed tally — Count small "•" dots per
// entry, back to back with no gap even where the colour changes — a compact
// stand-in for labels or flags that would otherwise need a tooltip on every
// card. [RenderLegend] draws that vocabulary once instead — a label and an
// optional hover tooltip per DotKind — as a separate call the host places
// wherever it fits (above the board, a toolbar, a footer); it is not drawn
// automatically by Render. Dots past the third, non-positive counts, and any
// id absent from DotLegend, are silently skipped.
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
// GroupByParent is a read/select view for now: pointer-drag moves cards in flat
// mode, and grouped-mode drag (horizontal to re-status, vertical to reparent) is
// the next slice. Ordering is by slice position within a column; a fractional-
// rank field is the recommended evolution once order is persisted or shared
// (O(1) inserts, no renumbering). Independent per-column vertical scrolling is
// deferred (the board scrolls together) to avoid the width-pinned-column /
// nested-ScrollArea collapse documented in the imzero2 skill; the single board
// ScrollArea sidesteps it.
package kanban
