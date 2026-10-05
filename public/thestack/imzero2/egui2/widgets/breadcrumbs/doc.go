// Package breadcrumbs is an immediate-mode widget (ADR-0267) that draws a
// trail of items in one row, separated by a caret: the segments of a path,
// or the steps of a wizard. It has no decision record of its own; its shape
// is ADR-0267's IM contract.
//
// # Shape
//
// [Model] is the host's data, one slice per attribute over item ordinals:
// the labels, and optionally an icon, a done mark and a lock reason per item.
// [State] carries the current item — drawn strong and not a link — and its
// zero value makes the last item current, which is what a path wants. A
// click on any other unlocked item moves the current item there and is
// reported in [Result.Clicked]; what the click means is the host's to
// decide. A host whose own state says where it is (a wizard's step, a
// browser's directory) projects it onto State before each Render with
// [State.SetCurrent], so the trail always shows the host's position.
//
// A done item carries a tick, unless it is current. A locked item is dimmed,
// cannot be clicked, and shows its reason on hover. Labels are the buttons'
// whole text, so a driver finds an item by its label whatever its marks.
//
// The separator and the tick are Phosphor glyphs, which the client loads
// itself, so neither falls back to another font (imzero2 skill §12).
//
// Like every response, a click is read one frame after it happened.
package breadcrumbs
