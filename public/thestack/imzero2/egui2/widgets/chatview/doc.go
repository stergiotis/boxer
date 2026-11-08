// Package chatview is an imzero2 widget that renders a conversation — a
// sequence of timestamped, attributed messages — as a transcript of bubbles:
// a two-party dialogue laid out like a phone's SMS view (the viewer's
// messages on the right, the other party's on the left), or a group chat
// (every message on the left under its sender's name and colour, the
// viewer's on the right), or a thread (every message a full-width block
// from the left edge, the viewer's tinted — for long bodies, and with
// nothing right-aligned to measure). ADR-0239 is the decision record.
//
// # Shape
//
// The input is columnar and the state is the host's, the shape widgets/tree
// takes: [Model] is one slice per attribute over message ordinals, with the
// reactions as a ragged co-array (values plus offsets), and [State] carries
// what must survive frames — the selection, whether the view follows the
// tail, the tail window, a pending jump, the measured row heights. [Render]
// draws where it is called and reports the frame's clicks in a [Result].
// Ordinals are the widget's only identity; a host whose model is rebuilt
// between frames projects its own keys onto State before each Render.
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
//
// # Virtualization
//
// Of the window, only the messages near the viewport are laid out (ADR-0363
// M5, after the technique of the zapfast messaging client). The unit is a
// message or system row together with the day separator that opens it,
// keyed by its message (see below). A unit within three viewports above
// the viewport or one below it (never less than a fixed floor) is laid out,
// with a probe either side of it; while following, so are the units that
// fill a viewport and the margin above the tail; a jump's target is laid
// out wherever it is. Every other unit
// stands in as space: its measured height from the cache in [State], or,
// until it has been laid out once, an estimate from its body's length and
// the chrome its rows carry. The estimate is kept until a measurement
// replaces it, so a unit stands in with the same height every frame.
//
// Every readback is one frame late — the host runs one egui pass per frame
// — so a unit's height is known the frame after it is first laid out, and a
// unit is first laid out a few viewports before it shows. When units that
// lay entirely above the viewport turn out taller or shorter than they were
// assumed to be, what the reader is looking at moves for that one frame.
// The next frame takes the difference out of the space above the first
// laid-out unit and asks for a scroll by as much (UiScrollWithDelta, which
// is relative, so the wheel's own movement in that frame is kept), which
// lands the frame after; from then on the content sits where it was. No
// correction is made while following or while a jump is under way: both
// scroll to a cursor in their own frame's layout. A jump stays under way,
// asking for its target every frame, until the target sits where the jump
// put it, the reader scrolls away, or a bound of frames runs out, since the
// units it passes may be measured on the way.
//
// Units are keyed by [Model.Key], or, without one, by a message's place
// among the rows that are not markers, so a height stays with its message
// when a marker is inserted before it or its time changes. Without Key,
// messages inserted before a message rename it.
//
// # Accessibility
//
// The units are one list to assistive technology (ADR-0363 M2): an
// AccessibleList named "Messages" whose length is the model's message count,
// and a row per laid-out unit at its ordinal, named by its key, so its node
// keeps its id when rows before it come and go. The length is the model's,
// not the window's, so a row's position does not change when the window
// widens. Rows standing in as space have no node. The current row — the
// list's active descendant — is the newest message while following, else
// the laid-out unit at the viewport's top.
package chatview
