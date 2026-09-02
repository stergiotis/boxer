package mdedit

// Heading numbering: numeric section prefixes ("## 2.1 Title") inserted,
// refreshed or stripped across the whole document in one gesture, from the
// outline pane.
//
// The heading set is the one the preview and the outline already render —
// [markdown.Doc.Headings] — rather than a second scan, so the numbers match
// the tree the reader is looking at by construction: the same nesting rule
// for skipped levels, the same setext headings (which the source lexer
// deliberately reads as prose), and nothing inside fences or frontmatter,
// which goldmark never reports as headings. HeadingInfo.ByteOffset points at
// the heading TEXT, which is exactly where a prefix goes, so no marker
// hunting is needed and setext headings work for free.
//
// A renumber is a whole-buffer rebind (see rebindBuffer): written from Go
// rather than typed, so it is not an edit egui's undo describes — M3's
// standing caveat. The way back is Clear numbers, not Ctrl+Z, and the
// tooltips say so. Slugs move with the text, so outline collapse state and
// in-document anchors move too.
//
// The rewrite itself is deferred by one frame: the outline renders AFTER the
// source pane, so a rebind issued from its buttons would miss the frame's
// databinding override and the frontend's cached buffer would win at the next
// Sync. The click stashes an action instead, and renderBody applies it at the
// top of the next frame, before anything derived is read.

import (
	"strings"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// numberActionE is the outline's stashed numbering gesture.
type numberActionE uint8

const (
	numberActNone numberActionE = iota
	numberActRenumber
	numberActStrip
)

// numberPrefixLen reports the byte length of an existing numeric section
// prefix at the start of s — "1. ", "2.3 ", "2.3. " — including the spaces
// that end it. A prefix must contain at least one '.' and be followed by at
// least one space: "2024 review" is a title that starts with a year, not a
// numbered heading, and stripping it would eat a word the writer chose. The
// dot rule cannot save a decimal-looking title ("3.14 constants"), which is
// the accepted edge every textual convention has.
func numberPrefixLen(s string) (n int) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	if i == 0 {
		return 0
	}
	dots := 0
	for i < len(s) && s[i] == '.' {
		dots++
		i++
		j := i
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		if j == i {
			// A dot with no digits after it is the optional trailing dot;
			// the prefix ends here.
			break
		}
		i = j
	}
	if dots == 0 {
		return 0
	}
	j := i
	for j < len(s) && s[j] == ' ' {
		j++
	}
	if j == i {
		return 0
	}
	return j
}

func isDigit(b byte) (yes bool) {
	yes = b >= '0' && b <= '9'
	return
}

// headingNumbers computes one hierarchical number per heading — "1.", "2.",
// "2.1", "2.1.1" — using the outline's own nesting rule: a heading's parent
// is the nearest heading above it with a smaller level. So skipped levels
// nest ("#" straight to "###" numbers x.1), and a document with no H1 numbers
// its H2s as roots. Degenerate headings (ByteOffset < 0) get "" — there is no
// text to prefix — and are left out of the hierarchy entirely.
//
// The display shape is the Word/DocBook convention: a single-component number
// carries a trailing dot ("1. Title"), a multi-component one does not
// ("2.1 Title"). Both shapes contain a dot, which is what lets
// numberPrefixLen round-trip everything this emits while refusing bare
// integers.
func headingNumbers(headings []markdown.HeadingInfo) (nums []string) {
	nums = make([]string, len(headings))
	type frame struct {
		level uint8
		path  string // dotted components, no trailing dot; "" for the root
		kids  int
	}
	stack := make([]frame, 1, 8) // frame 0 is the virtual root, level 0
	for i, h := range headings {
		if h.ByteOffset < 0 {
			continue
		}
		for len(stack) > 1 && stack[len(stack)-1].level >= h.Level {
			stack = stack[:len(stack)-1]
		}
		p := &stack[len(stack)-1]
		p.kids++
		path := itoa(p.kids)
		if p.path != "" {
			path = p.path + "." + itoa(p.kids)
		}
		nums[i] = formatHeadingNumber(path)
		stack = append(stack, frame{level: h.Level, path: path})
	}
	return
}

// formatHeadingNumber renders a dotted path in the display shape
// headingNumbers documents.
func formatHeadingNumber(path string) (s string) {
	if strings.IndexByte(path, '.') < 0 {
		return path + "."
	}
	return path
}

// renumberHeadings strips any existing numeric prefix and inserts the
// computed one at each heading's text offset, splicing back to front so the
// earlier offsets stay valid against the buffer being rewritten. Returns the
// rewritten buffer and how many headings actually changed — re-running it on
// its own output changes nothing, which is what makes the button safe to
// press twice.
func renumberHeadings(src string, headings []markdown.HeadingInfo) (out string, changed int) {
	nums := headingNumbers(headings)
	out = src
	for i := len(headings) - 1; i >= 0; i-- {
		h := headings[i]
		if h.ByteOffset < 0 || nums[i] == "" || h.ByteOffset > len(out) {
			continue
		}
		strip := numberPrefixLen(out[h.ByteOffset:])
		want := nums[i] + " "
		if strip == len(want) && strings.HasPrefix(out[h.ByteOffset:], want) {
			continue
		}
		out = out[:h.ByteOffset] + want + out[h.ByteOffset+strip:]
		changed++
	}
	return
}

// stripHeadingNumbers is the inverse: prefixes removed, nothing inserted.
func stripHeadingNumbers(src string, headings []markdown.HeadingInfo) (out string, changed int) {
	out = src
	for i := len(headings) - 1; i >= 0; i-- {
		h := headings[i]
		if h.ByteOffset < 0 || h.ByteOffset > len(out) {
			continue
		}
		strip := numberPrefixLen(out[h.ByteOffset:])
		if strip == 0 {
			continue
		}
		out = out[:h.ByteOffset] + out[h.ByteOffset+strip:]
		changed++
	}
	return
}

// applyPendingNumbering consumes the outline's stashed gesture, at the top of
// the frame AFTER the click — before the source pane renders, which is what
// lets the rebind's override land (see the file header). The parse gate keeps
// it honest: if anything else rebound the buffer in the same drain (a file
// open landing), the headings no longer describe it and the gesture is
// dropped rather than applied to the wrong document.
func (inst *App) applyPendingNumbering() {
	act := inst.numberAction
	inst.numberAction = numberActNone
	if act == numberActNone {
		return
	}
	if inst.doc == nil || !inst.docOk || inst.docSrc != inst.src {
		return
	}
	headings := inst.doc.Headings()
	var next string
	var n int
	if act == numberActStrip {
		next, n = stripHeadingNumbers(inst.src, headings)
	} else {
		next, n = renumberHeadings(inst.src, headings)
	}
	if n == 0 || !inst.rebindBuffer(next) {
		inst.status = "numbering unchanged"
		return
	}
	if act == numberActStrip {
		inst.status = "cleared numbers on " + itoa(n)
		return
	}
	inst.status = "renumbered " + itoa(n)
}
