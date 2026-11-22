// Package decision is an immediate-mode widget (ADR-0267) that draws one
// question the person settles with a button: what is asked, the thing it is
// asked about, what follows from answering, notes that the question has gone
// stale or cannot be granted, and the options. It is the drawing ADR-0404
// SD3 decides on, with the emphasis of SD4.
//
// # Shape
//
// [Input] is the whole declaration, given every frame; the widget keeps no
// state. [Render] draws, in this order: the title, the subject, one sentence
// per entry of Follows, the notes, the caller's Form, the options in one row,
// the reason beside each disabled option, and the caller's Detail. Follows,
// notes and reasons are body text and always wrap to the width the host
// gives. Detail comes after the options, so a long diff or preview never
// pushes them out of reach.
//
// # Emphasis
//
// The caller states who started the act ([OriginE]) and each option's role
// ([RoleE]); the widget picks the button kind. The affirmative is primary,
// on a confirmation, where the person started the act, and on a request,
// where another party asks the person for something. A request that carries
// a note in a warning or an error tone has no primary: the surface does not
// favour yes over its own caution. An option that destroys is Danger in
// either case. A decline is
// never weaker than a plain button; an option that is neither affirmative nor
// decline is a ghost button.
//
// An option's label is its button's whole text, prefixed by its icon when it
// has one, so a driver finds it by that text.
//
// A structurally broken Input (no title, no options, an empty or repeated
// key, two affirmatives, a disabled option with no reason) is drawn in place
// as a message and returned in [Result.Err].
//
// Like every response, a click is read one frame after it happened.
package decision
