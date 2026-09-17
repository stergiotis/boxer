// Package cardgrid is an imzero2 widget that renders a page of items as a
// responsive grid of uniform cards — hero, overline, title, subtitle, body,
// facts, tags, footer. ADR-0245 is the decision record.
//
// # Shape
//
// The input is columnar and the state is the host's, the shape widgets/tree
// and widgets/chatview take: [Model] is one slice per slot over card
// ordinals, with facts and tags as ragged co-arrays (values plus offsets),
// and [State] carries what must survive frames — the selection, the density,
// the hero aspect, a pending reveal. [Render] draws where it is called and
// reports the frame's click and keys in a [Result]. Ordinals are the widget's
// only identity. The model holds one page; paging is the host's.
//
// The widget knows nothing about how a hero or a rich body is rendered:
// [Input.Hero] and [Input.Body] let the host draw them through a [Block] —
// the contract chatview and the leeway card already have — and everything
// else arrives as text plus a colour. That keeps the widget free of any
// content catalog; in play the callbacks are filled from the gloss
// resolution.
//
// # Layout
//
// [Plan] is the whole layout and is a pure function: the column count from
// the pane's width and the density, and one card height for every card,
// derived from [Model.Slots] and the density alone. A slot the model
// declares reserves its budget whether or not a card fills it; a slot the
// model does not declare reserves nothing. No card's height depends on its
// content, which is what lets the grid place every card at a computed rect
// before anything is laid out.
//
// Each card and each slot inside it is an allocated rect with a hard paint
// clip (the treemap's fixed-cell recipe), so content that outgrows its
// budget — a long title, a tall body, a block that misjudged its height —
// is cut at the slot's edge and cannot reach a neighbour. Text is
// additionally cut in Go to an estimated rune budget so an ellipsis usually
// lands before the clip does; [OneLine] and [Lines] are the helpers a host
// uses to bound what it puts in the model in the first place.
//
// The card's click sense is emitted first and the slots after it, so the
// sense sits behind the card's content: labels are not selectable and let
// the click through, while a host block's own controls — a play button in a
// hero — sit in front and keep their clicks.
package cardgrid
