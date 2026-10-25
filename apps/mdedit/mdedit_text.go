package mdedit

import (
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdline"
	"strings"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mdoutline"
)

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

// scrollTarget resolves the caret to a section slug and reports whether that
// section differs from the one the preview was last scrolled to.
//
// The changed flag is the whole point: re-passing the same slug to
// [markdown.Doc.Render] every frame re-issues the scroll every frame and pins
// the preview, so the reader can never scroll it by hand. The option's own doc
// comment names this guard, and helphost implements the same one.
func scrollTarget(src string, headings []markdown.HeadingInfo, caretChar int, last string) (slug string, changed bool) {
	slug = mdoutline.SectionAt(src, headings, mdline.CharToByte(src, caretChar))
	changed = slug != last
	return
}
