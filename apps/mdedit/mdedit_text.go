package mdedit

import (
	"strings"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// charToByte converts a CHAR offset — the unit the editor's caret report
// carries — into a byte offset into src, clamped to [0, len(src)].
//
// The conversion runs against Go's own copy of the buffer, never the live one.
// Text and caret arrive on two databindings keyed by the same widget id and
// carry the same one-frame lag, so they agree with each other; neither agrees
// with what the reader has typed since (ADR-0130 §Decision 1).
func charToByte(src string, charOff int) (byteOff int) {
	if charOff <= 0 {
		return 0
	}
	n := 0
	for i := range src {
		if n == charOff {
			return i
		}
		n++
	}
	return len(src)
}

// lineStart returns the byte offset of the first byte of the line containing
// off.
func lineStart(src string, off int) (start int) {
	if off <= 0 {
		return 0
	}
	if off > len(src) {
		off = len(src)
	}
	if i := strings.LastIndexByte(src[:off], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

// headingSlugAt resolves a byte offset to the slug of the section containing
// it: the last heading starting at or before off. An offset above the first
// heading returns "" — the document-level section, which is also the value
// [markdown.WithScrollToSection] treats as a no-op, so a document with no
// headings simply never scrolls.
//
// Heading offsets are normalised to their line start before comparing.
// [markdown.HeadingInfo.ByteOffset] points at the heading TEXT, not at the `#`
// marker, so without the normalisation a caret resting on the marker resolves
// to the PREVIOUS section — the preview would jump backwards at exactly the
// moment the reader clicked into a heading.
func headingSlugAt(src string, headings []markdown.HeadingInfo, off int) (slug string) {
	for _, h := range headings {
		if h.ByteOffset < 0 || h.Slug == "" {
			// A heading with no text lines (`##` alone) carries offset -1 and
			// an empty slug. It names no section to scroll to.
			continue
		}
		if lineStart(src, h.ByteOffset) > off {
			break // headings arrive in document order
		}
		slug = h.Slug
	}
	return
}

// scrollTarget resolves the caret to a section slug and reports whether that
// section differs from the one the preview was last scrolled to.
//
// The changed flag is the whole point: re-passing the same slug to
// [markdown.Doc.Render] every frame re-issues the scroll every frame and pins
// the preview, so the reader can never scroll it by hand. The option's own doc
// comment names this guard, and helphost implements the same one.
func scrollTarget(src string, headings []markdown.HeadingInfo, caretChar int, last string) (slug string, changed bool) {
	slug = headingSlugAt(src, headings, charToByte(src, caretChar))
	changed = slug != last
	return
}
