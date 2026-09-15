// Package chatview is an imzero2 widget that renders a conversation — a
// sequence of timestamped, attributed messages — as a transcript of bubbles:
// a two-party dialogue laid out like a phone's SMS view (the viewer's
// messages on the right, the other party's on the left), or a group chat
// (every message on the left under its sender's name and colour, the
// viewer's on the right). ADR-0239 is the decision record.
//
// # Shape
//
// The input is columnar and the state is the host's, the shape widgets/tree
// takes: [Model] is one slice per attribute over message ordinals, with the
// reactions as a ragged co-array (values plus offsets), and [State] carries
// what must survive frames — the selection, whether the view follows the
// tail, the tail window, a pending jump. [Render] draws where it is called
// and reports the frame's clicks in a [Result]. Ordinals are the widget's
// only identity; a host whose model is rebuilt between frames projects its
// own keys onto State before each Render.
//
// The widget knows nothing about how a body is rendered beyond plain wrapped
// text: [Input.Block] lets the host draw a message's body itself — a markdown
// document, an image, a code view — through a [Block], the contract the
// leeway card's block faces already have. That keeps the widget free of any
// content catalog; in play the callback is filled from the gloss resolution.
//
// # Layout
//
// Rows are derived from the model each frame: a day separator wherever the
// local date changes, a centred line for a system message, and message
// clusters — consecutive messages of one sender within a few minutes carry
// the name once, sit tighter, and round the sender-side corner only on the
// cluster's last bubble. The transcript is a vertical ScrollArea over the
// newest [State.Window] messages; while [State.Follow] is set the view is
// pinned to the tail, a wheel movement against the flow releases it, and an
// "older" affordance above the first drawn row widens the window. The
// bubble width is a fraction of the pane's width, read from the pane-size
// probe and held across the frame the probe is absent.
package chatview
