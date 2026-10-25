// Package mdline decides what the editing keys do on one line of markdown
// source: Enter continues a list or quote line's marker and leaves a list
// from an empty item, Tab indents a list line and Shift+Tab outdents it. Pure
// functions over the source text and a caret, shared by the mdedit source
// pane and the block editor (ADR-0334 §SD4), so the two cannot disagree about
// what a line start means.
//
// The rules are the conventions every surveyed editor settled on: Enter
// continues the marker, Enter on an item that is nothing but its marker
// removes it, Shift+Enter is always a bare newline, and Tab moves a list item
// by one unit — a tab, the unit Obsidian's default writes.
package mdline

import (
	"strconv"
	"strings"
)

// IndentUnit is what Tab adds in front of a list item. The parser reads a
// tab as four columns, more than the two a `- ` marker needs and the three a
// `1. ` one does.
const IndentUnit = "\t"

// Action is what a key resolved to: text for the editor to insert at the
// caret, or a rewritten source with the caret it should land on (Rebind).
// Exit says the rewrite removed a marker the caret was on.
type Action struct {
	Insert string
	Rebind bool
	Exit   bool
	Next   string
	Caret  int
}

// Prefix is the marker run at the start of a line: indentation, any number of
// `> ` quote markers, and at most one list marker, with the list marker's
// parts broken out so a continuation can renumber it or reset its task box.
type Prefix struct {
	// Text is the whole prefix as written, indentation included.
	Text string
	// List is the list marker alone ("- ", "1. "), "" for none.
	List string
	// Ordered is set with Number when the list marker carries one.
	Ordered bool
	Number  int
	// Task is the task-box part after a list marker ("[x] "), "" for none.
	Task string
	// Quote says the prefix carries at least one `> `.
	Quote bool
}

// ParsePrefix reads the marker run at the start of line.
func ParsePrefix(line string) (p Prefix) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	for i < len(line) && line[i] == '>' {
		p.Quote = true
		i++
		for i < len(line) && line[i] == ' ' {
			i++
		}
	}
	markStart := i
	switch {
	case i+1 < len(line) && (line[i] == '-' || line[i] == '*' || line[i] == '+') && line[i+1] == ' ':
		i += 2
	default:
		j := i
		for j < len(line) && line[j] >= '0' && line[j] <= '9' && j-i < 9 {
			j++
		}
		if j > i && j+1 < len(line) && (line[j] == '.' || line[j] == ')') && line[j+1] == ' ' {
			p.Ordered = true
			n := 0
			for _, d := range line[i:j] {
				n = n*10 + int(d-'0')
			}
			p.Number = n
			i = j + 2
		}
	}
	if i > markStart {
		p.List = line[markStart:i]
		if i+3 < len(line) && line[i] == '[' && line[i+2] == ']' && line[i+3] == ' ' {
			p.Task = line[i : i+4]
			i += 4
		}
	}
	p.Text = line[:i]
	return
}

// Continuation is the prefix the next line starts with: the same indentation
// and quote markers, the list marker renumbered, a task box emptied.
func (p Prefix) Continuation() string {
	if p.List == "" {
		return p.Text
	}
	head := p.Text[:len(p.Text)-len(p.List)-len(p.Task)]
	list := p.List
	if p.Ordered {
		list = strconv.Itoa(p.Number+1) + p.List[len(p.List)-2:]
	}
	task := ""
	if p.Task != "" {
		task = "[ ] "
	}
	return head + list + task
}

// Enter decides what Enter does at caret, a byte offset into src. Shift gives
// a bare newline; so does a line with no marker, or the caret inside the
// marker itself. A marked line continues its marker, and a line that is
// nothing but its marker exits: the marker is removed and the caret parks at
// the line start.
func Enter(src string, caret int, shift bool) (act Action) {
	caret = clamp(caret, len(src))
	act.Insert = "\n"
	if shift {
		return
	}
	ls, le := lineBounds(src, caret)
	line := src[ls:le]
	p := ParsePrefix(line)
	if p.List == "" && !p.Quote {
		return
	}
	if caret < ls+len(p.Text) {
		return
	}
	if strings.TrimSpace(line[len(p.Text):]) == "" {
		act = Action{Rebind: true, Exit: true, Next: src[:ls] + src[ls+len(p.Text):], Caret: ls}
		return
	}
	act.Insert = "\n" + p.Continuation()
	return
}

// Tab decides what Tab does at caret: a list line moves by one IndentUnit
// (Shift takes one back — a tab, or up to four leading spaces), any other
// line gets a tab character, and Shift+Tab outside a list does nothing.
func Tab(src string, caret int, shift bool) (act Action) {
	caret = clamp(caret, len(src))
	ls, le := lineBounds(src, caret)
	line := src[ls:le]
	p := ParsePrefix(line)
	if p.List == "" {
		if !shift {
			act.Insert = "\t"
		}
		return
	}
	if !shift {
		act = Action{Rebind: true, Next: src[:ls] + IndentUnit + src[ls:], Caret: caret + len(IndentUnit)}
		return
	}
	remove := 0
	if strings.HasPrefix(line, "\t") {
		remove = 1
	} else {
		for remove < 4 && remove < len(line) && line[remove] == ' ' {
			remove++
		}
	}
	if remove == 0 {
		return
	}
	act = Action{Rebind: true, Next: src[:ls] + src[ls+remove:], Caret: max(caret-remove, ls)}
	return
}

// LineStart is the byte offset of the first byte of the line holding off.
func LineStart(src string, off int) int {
	off = clamp(off, len(src))
	if i := strings.LastIndexByte(src[:off], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

// LineEnd is the byte offset of the newline ending the line holding off, or
// len(src) on the last line.
func LineEnd(src string, off int) int {
	off = clamp(off, len(src))
	if i := strings.IndexByte(src[off:], '\n'); i >= 0 {
		return off + i
	}
	return len(src)
}

func lineBounds(src string, off int) (ls, le int) {
	return LineStart(src, off), LineEnd(src, off)
}

func clamp(v, n int) int {
	if v < 0 {
		return 0
	}
	if v > n {
		return n
	}
	return v
}

// CharToByte converts a char offset into s — the unit the caret channel
// speaks — to a byte offset, clamped to [0, len(s)].
func CharToByte(s string, charOff int) int {
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

// ByteToChar converts a byte offset into s to a char offset, clamped. An
// offset inside a rune belongs to that rune: a caret cannot sit mid-
// character, and rounding up would put it one past where the byte pointed.
func ByteToChar(s string, byteOff int) int {
	if byteOff <= 0 {
		return 0
	}
	n := 0
	for i := range s {
		if i == byteOff {
			return n
		}
		if i > byteOff {
			return n - 1
		}
		n++
	}
	return n
}

// Wrap is one inline formatting: what goes before and after the selection,
// and what stands in for it when nothing is selected, so the gesture always
// leaves a word to type over.
type Wrap struct {
	Open, Close, Placeholder string
}

// Wraps are the inline formattings by name — the formatting bar's buttons
// and the editors' Ctrl chords, from one table so the two cannot disagree.
var Wraps = map[string]Wrap{
	"bold":   {"**", "**", "bold"},
	"italic": {"*", "*", "italic"},
	"code":   {"`", "`", "code"},
	"strike": {"~~", "~~", "struck"},
	"mark":   {"==", "==", "marked"},
	"link":   {"[", "](url)", "label"},
}

// WrapSnippet is the text that replaces the selection [selStart, selEnd) of
// src, given as char offsets in either order: the selection wrapped, or the
// placeholder wrapped when the selection is empty.
func WrapSnippet(src string, w Wrap, selStart, selEnd int) string {
	inner := w.Placeholder
	if selStart != selEnd {
		lo, hi := selStart, selEnd
		if lo > hi {
			lo, hi = hi, lo
		}
		if sel := src[CharToByte(src, lo):CharToByte(src, hi)]; sel != "" {
			inner = sel
		}
	}
	return w.Open + inner + w.Close
}
