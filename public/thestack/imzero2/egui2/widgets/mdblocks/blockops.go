package mdblocks

import (
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdline"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

// Block-level gestures over the open block or the block selection
// (ADR-0334 §SD4): move the blocks up or down past a neighbour, duplicate
// them below themselves, and the heading level by chord. Each is a splice
// of the source with the caret carried along, an undo entry of its own.

// headingChords maps the digit chords to a heading level, 0 a paragraph.
var headingChords = map[keycodes.Code]int{
	keycodes.Digit0: 0, keycodes.Digit1: 1, keycodes.Digit2: 2, keycodes.Digit3: 3,
	keycodes.Digit4: 4, keycodes.Digit5: 5, keycodes.Digit6: 6,
}

// moveBlocks swaps blocks a..b with the block before them (d < 0) or after
// them (d > 0), the gap between the two kept as it was. The caret moves
// with its block, and so does a block selection. Reports whether anything
// moved.
func (e *Editor) moveBlocks(a, b, d int) bool {
	if a < 0 || b >= len(e.blocks) || a > b {
		return false
	}
	var first, second block
	if d < 0 {
		if a == 0 {
			return false
		}
		first = e.blocks[a-1]
		second = block{start: e.blocks[a].start, stop: e.blocks[b].stop}
	} else {
		if b+1 >= len(e.blocks) {
			return false
		}
		first = block{start: e.blocks[a].start, stop: e.blocks[b].stop}
		second = e.blocks[b+1]
	}
	x := e.src[first.start:first.stop]
	gap := e.src[first.stop:second.start]
	y := e.src[second.start:second.stop]
	caret := e.caret
	if d < 0 {
		caret = first.start + (e.caret - second.start)
	} else {
		caret = first.start + len(y) + len(gap) + (e.caret - first.start)
	}
	e.splice(first.start, x+gap+y, y+gap+x, caret, true)
	if d < 0 {
		e.selA, e.selB = a-1, b-1
	} else {
		e.selA, e.selB = a+1, b+1
	}
	return true
}

// duplicateBlocks puts a copy of blocks a..b after them, a blank line
// between, with the caret at the same place in the copy and a block
// selection moved onto it.
func (e *Editor) duplicateBlocks(a, b int) bool {
	if a < 0 || b >= len(e.blocks) || a > b {
		return false
	}
	start, stop := e.blocks[a].start, e.blocks[b].stop
	ins := "\n\n" + e.src[start:stop]
	caret := stop + 2 + (e.caret - start)
	if e.caret < start || e.caret > stop {
		caret = stop + 2
	}
	e.splice(stop, "", ins, caret, true)
	n := b - a
	e.selA, e.selB = b+1, b+1+n
	return true
}

// applyBlockChord is a chord over the open block: the heading level, a
// move, a duplicate. Reports whether the chord was one of these.
func (e *Editor) applyBlockChord(code keycodes.Code) bool {
	f := blockAt(e.blocks, e.caret)
	switch {
	case code == keycodes.ArrowUp || code == keycodes.ArrowDown:
		d := -1
		if code == keycodes.ArrowDown {
			d = 1
		}
		if f >= 0 && e.moveBlocks(f, f, d) {
			e.pendingCaretOk = true
			e.pendingFocus = true
		}
	case code == keycodes.KeyD:
		if f >= 0 && e.duplicateBlocks(f, f) {
			e.pendingCaretOk = true
			e.pendingFocus = true
		}
	default:
		level, ok := headingChords[code]
		if !ok {
			return false
		}
		cb := clampInt(e.caret-e.bufStart, 0, len(e.buf))
		e.applyLine(mdline.SetHeading(e.buf, cb, level))
	}
	return true
}

// urlRe is what a pasted web address looks like: a scheme or a www host,
// then no whitespace to the end.
var urlRe = regexp.MustCompile(`^(?:https?://|www\.)\S+$`)

// pastedOverSelection reads the write-back as a paste over the selection
// the widget held before it: the text outside the selection unchanged, the
// selection replaced by what was pasted. The diff of the two buffers
// cannot say this — a selection and a pasted address that share their
// first letters would split in the wrong place — so it is read against
// the cursor the widget reported with the previous emit. Returns the
// selection's byte offset in the buffer, the selection, and the link it
// becomes.
func (e *Editor) pastedOverSelection() (at int, sel, link string, ok bool) {
	lo, hi := c.UnpackCursorRange(e.lastCursor)
	if lo == hi {
		return
	}
	prev := e.lastEmitted
	a, b := mdline.CharToByte(prev, lo), mdline.CharToByte(prev, hi)
	tail := len(prev) - b
	if a > b || b > len(prev) || len(e.buf) < a+tail || e.buf[:a] != prev[:a] || e.buf[len(e.buf)-tail:] != prev[b:] {
		return
	}
	link, ok = pastedLink(prev[a:b], e.buf[a:len(e.buf)-tail])
	return a, prev[a:b], link, ok
}

// pastedLink is the text a selection becomes when a web address is pasted
// over it: the selection as the label, the address as the target. Only a
// one-line selection that is not itself an address, and only an address
// — a typed character never looks like one.
func pastedLink(selection, pasted string) (link string, ok bool) {
	if selection == "" || strings.ContainsAny(selection, "\n\r") || !urlRe.MatchString(pasted) || urlRe.MatchString(strings.TrimSpace(selection)) {
		return "", false
	}
	return "[" + selection + "](" + pasted + ")", true
}
