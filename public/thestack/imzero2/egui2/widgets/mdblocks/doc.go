// Package mdblocks is a semi-retained widget (ADR-0267) that edits a markdown
// document as a vertical list of blocks over the one source string — the
// shape ADR-0334 decided. Every block the parse reports renders as the
// preview would draw it; the one the caret is in is a TextEdit over its own
// source slice, and every edit is a byte-range splice into the source
// followed by a reparse, so bytes the person did not touch never change
// (§SD1). What the editor reads back from the TextEdit — its text and its
// caret — is one frame stale, the ADR-0130 rule every TextEdit consumer
// lives with; the widget applies it before the next emit.
//
// The caret is a byte offset into the source, and focus is derived from it
// each frame: a caret inside a block edits that block, a caret between
// blocks edits an empty block at that gap, and the first character typed
// into a gap is padded with the blank lines that make it a block of its own.
// Undo is the document's: a history of inverse splices owned here, with
// Ctrl+Z and Ctrl+Y captured in the focused TextEdit before egui's own undo
// can see them (§SD5, ADR-0177 chords).
//
// Usage:
//
//	ed := mdblocks.New(ids, "doc", mdblocks.Opts{})
//	ed.SetSource(src)
//	// per frame
//	ev := ed.Render()
//	if ev.Changed { src = ed.Source() }
package mdblocks
