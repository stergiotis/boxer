package mdblocks

import (
	"hash/fnv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// The model half of the widget: the source string, the blocks a parse finds
// in it, the caret as a byte offset, and the history of splices. Nothing
// here touches the FFI, so all of it is testable without a frame.

// blockKindE is a block's kind for the editor's own decisions. The markdown
// widget's kinds plus the frontmatter, which the parser consumes without a
// node and the editor still has to let the person edit.
type blockKindE uint8

const (
	kindParagraph blockKindE = iota
	kindHeading
	kindCode
	kindList
	kindQuote
	kindRule
	kindCallout
	kindTable
	kindFrontmatter
)

// block is one editable block: its byte range in the source (line-snapped,
// trailing newline excluded), its kind, the index into the parsed document's
// segments it renders through (-1 for the frontmatter), and a key stable
// across reparses that do not touch its text, for the id scope its embedded
// widgets live under.
type block struct {
	start, stop int
	kind        blockKindE
	docIndex    int
	key         uint64
}

// blocksOf lists the editable blocks of a parsed source, frontmatter first,
// skipping what the parse reports synthetic. Keys hash the kind and the
// text with an ordinal among identical blocks, the way the markdown widget
// keys its code blocks: a block keeps its id when text moves above it and
// takes a new one when its own text changes.
func blocksOf(src string, doc *markdown.Doc) (out []block) {
	seen := map[uint64]int{}
	add := func(b block) {
		h := fnv.New64a()
		_, _ = h.Write([]byte{byte(b.kind)})
		_, _ = h.Write([]byte(src[b.start:b.stop]))
		base := h.Sum64()
		ord := seen[base]
		seen[base] = ord + 1
		b.key = base ^ (uint64(ord) * 0x9e3779b97f4a7c15)
		out = append(out, b)
	}
	if s, e, ok := doc.FrontmatterSpan(); ok {
		add(block{start: s, stop: e, kind: kindFrontmatter, docIndex: -1})
	}
	for i, bi := range doc.Blocks() {
		if bi.Synthetic {
			continue
		}
		add(block{start: bi.Start, stop: bi.Stop, kind: kindOf(bi.Kind), docIndex: i})
	}
	return
}

func kindOf(k markdown.BlockKindE) blockKindE {
	switch k {
	case markdown.BlockHeading:
		return kindHeading
	case markdown.BlockCodeBlock:
		return kindCode
	case markdown.BlockList:
		return kindList
	case markdown.BlockBlockquote:
		return kindQuote
	case markdown.BlockHorizontalRule:
		return kindRule
	case markdown.BlockCallout:
		return kindCallout
	case markdown.BlockTable:
		return kindTable
	default:
		return kindParagraph
	}
}

// blockAt is the index of the block whose range holds the caret, with the
// range closed at both ends so a caret just past a block's last byte still
// edits it, or -1 for a caret in a gap.
func blockAt(blocks []block, caret int) int {
	for i, b := range blocks {
		if caret >= b.start && caret <= b.stop {
			return i
		}
	}
	return -1
}

// blockBefore is the index of the last block ending at or before off, or -1.
func blockBefore(blocks []block, off int) int {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].stop <= off {
			return i
		}
	}
	return -1
}

// blockAfter is the index of the first block starting at or after off, or -1.
func blockAfter(blocks []block, off int) int {
	for i, b := range blocks {
		if b.start >= off {
			return i
		}
	}
	return -1
}

// editRegion is the single replacement that turns old into new, as a common
// prefix and suffix diff: at is the offset into old, oldMid and newMid the
// bytes replaced and replacing. Snapped to UTF-8 boundaries so an entry's
// halves are valid text on their own; both are concatenations of valid
// strings either way.
func editRegion(old, new string) (at int, oldMid, newMid string) {
	p := 0
	for p < len(old) && p < len(new) && old[p] == new[p] {
		p++
	}
	for p > 0 && p < len(old) && !utf8.RuneStart(old[p]) {
		p--
	}
	s := 0
	for s < len(old)-p && s < len(new)-p && old[len(old)-1-s] == new[len(new)-1-s] {
		s++
	}
	for s > 0 && !utf8.RuneStart(old[len(old)-s]) {
		s--
	}
	return p, old[p : len(old)-s], new[p : len(new)-s]
}

// gapPadding is what a first insertion into a gap needs around it to become a
// block of its own: blank lines between it and the blocks on either side.
// before is prepended, after appended, and the text at pos is otherwise what
// the person typed.
func gapPadding(src string, pos int) (before, after string) {
	left := src[:pos]
	switch {
	case left == "" || strings.HasSuffix(left, "\n\n"):
	case strings.HasSuffix(left, "\n"):
		before = "\n"
	default:
		before = "\n\n"
	}
	right := src[pos:]
	switch {
	case right == "" || strings.HasPrefix(right, "\n\n"):
	case strings.HasPrefix(right, "\n"):
		after = "\n"
	default:
		after = "\n\n"
	}
	return
}

// entry is one splice in the history: at [at, at+len(new)) the source now
// holds new where it held old, with the caret before and after.
type entry struct {
	at          int
	old, new    string
	caretBefore int
	caretAfter  int
	at_         time.Time
	sealed      bool
}

// history is the document's undo: inverse splices, coalesced the way the
// surveyed frameworks document — contiguous typing within coalesceWindow
// joins the open entry; a structural gesture seals it (ADR-0334 §SD5).
type history struct {
	undo []entry
	redo []entry
	now  func() time.Time
}

// coalesceWindow is how long after the last keystroke a new one still joins
// its undo entry; the value ProseMirror and CodeMirror document.
const coalesceWindow = 500 * time.Millisecond

func (h *history) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

// record adds a splice. Typing that extends the open entry — an insertion
// right after its text, or a deletion right before it — within the window
// joins it; anything else opens a new entry. A record clears the redo stack.
func (h *history) record(e entry) {
	h.redo = h.redo[:0]
	e.at_ = h.clock()
	if n := len(h.undo); n > 0 {
		last := &h.undo[n-1]
		if !last.sealed && e.at_.Sub(last.at_) <= coalesceWindow {
			switch {
			case e.old == "" && last.old == "" && e.at == last.at+len(last.new):
				last.new += e.new
				last.caretAfter = e.caretAfter
				last.at_ = e.at_
				return
			case e.new == "" && last.new == "" && e.at+len(e.old) == last.at:
				last.at = e.at
				last.old = e.old + last.old
				last.caretAfter = e.caretAfter
				last.at_ = e.at_
				return
			}
		}
	}
	h.undo = append(h.undo, e)
}

// seal closes the open entry so the next keystroke starts a new one.
func (h *history) seal() {
	if n := len(h.undo); n > 0 {
		h.undo[n-1].sealed = true
	}
}

// pop takes the most recent entry for undo, moving it to the redo stack.
func (h *history) pop() (e entry, ok bool) {
	n := len(h.undo)
	if n == 0 {
		return
	}
	e = h.undo[n-1]
	h.undo = h.undo[:n-1]
	e.sealed = true
	h.redo = append(h.redo, e)
	return e, true
}

// unpop takes the most recent undone entry for redo, moving it back.
func (h *history) unpop() (e entry, ok bool) {
	n := len(h.redo)
	if n == 0 {
		return
	}
	e = h.redo[n-1]
	h.redo = h.redo[:n-1]
	h.undo = append(h.undo, e)
	return e, true
}

func (h *history) clear() {
	h.undo, h.redo = h.undo[:0], h.redo[:0]
}

// apply performs a splice on src and returns the result; the entry is what
// record takes afterwards.
func apply(src string, at int, old, new string) string {
	return src[:at] + new + src[at+len(old):]
}

// invert is the splice that undoes e.
func invert(e entry) entry {
	return entry{at: e.at, old: e.new, new: e.old, caretBefore: e.caretAfter, caretAfter: e.caretBefore, sealed: true}
}

// charToByte converts a char offset into s, the unit the caret channel
// speaks, to a byte offset, clamped.
func charToByte(s string, charOff int) int {
	if charOff <= 0 {
		return 0
	}
	n := 0
	for i := range s {
		if n == charOff {
			return i
		}
		n++
	}
	return len(s)
}

// byteToChar converts a byte offset into s to a char offset, clamped.
func byteToChar(s string, byteOff int) int {
	if byteOff <= 0 {
		return 0
	}
	n := 0
	for i := range s {
		if i >= byteOff {
			return n
		}
		n++
	}
	return n
}

// countLines is the number of lines s occupies, one for an empty string.
func countLines(s string) int {
	return strings.Count(s, "\n") + 1
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
