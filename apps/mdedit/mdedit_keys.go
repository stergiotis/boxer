package mdedit

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

// The source pane's keyboard (ADR-0334 §SD4, M2). Three captures ride the
// editor's TextEdit:
//
//   - Enter, as a plain capture: every newline the reader types reaches Go
//     first, so a list or quote line continues its marker on the next line,
//     an empty item exits the list, and everything else gets the newline it
//     asked for. Shift+Enter is always a bare newline.
//   - Tab, through captureTab: a list line indents, Shift+Tab outdents, and
//     any other line gets the tab character the code editor used to insert.
//   - Ctrl+B / I / K / E as chords (ADR-0177, update of 2026-10-24): the
//     formatting bar's wraps from the keyboard. The letters without the
//     modifier are never captured and type as before.
//
// Each capture is read back one frame after the press and applied before the
// editor re-renders, the same cadence the find field's Enter has. What it
// applies is either a snippet spliced at the caret (insertAtCursor, inside
// the widget's own edit path) or, for the two gestures that rewrite a line
// start, a buffer rebind with the caret moved along — the standing trade
// ADR-0178 M3 records: a rebind is outside egui's undo.

// editorKeyMask is what the source editor captures plainly.
var editorKeyMask = keycodes.MaskOf(keycodes.Enter)

// editorChordMask is what the source editor captures only under the command
// modifier, and editorChords maps each key to the bar action it triggers.
var editorChordMask = keycodes.MaskOf(keycodes.KeyB, keycodes.KeyI, keycodes.KeyK, keycodes.KeyE)

var editorChords = map[keycodes.Code]string{
	keycodes.KeyB: "bold",
	keycodes.KeyI: "italic",
	keycodes.KeyK: "link",
	keycodes.KeyE: "code",
}

// indentUnit is what Tab adds in front of a list item. A tab, as Obsidian's
// default has it; the parser reads it as four columns, which is more than the
// two a `- ` marker needs and the three a `1. ` one does.
const indentUnit = "\t"

// applyEditorKeys consumes last frame's captures on the source editor. Called
// at the top of renderSource, before the editor is emitted, so a rebind it
// issues lands in the same frame's databinding override.
func (inst *App) applyEditorKeys() {
	if inst.editorFieldId == 0 {
		return
	}
	keys := c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(inst.editorFieldId))
	if len(keys) == 0 {
		return
	}
	caret, _ := c.UnpackCursorRange(inst.cursor)
	caretByte := charToByte(inst.src, caret)
	for _, k := range keys {
		switch {
		case k.Code == keycodes.Enter:
			inst.applyEnter(caretByte, k.Shift())
		case k.Code == keycodes.Tab:
			inst.applyTab(caretByte, k.Shift())
		case k.Command():
			if key, ok := editorChords[k.Code]; ok {
				inst.applyChord(key)
			}
		}
		// One gesture per frame: a second key in the same batch would act on
		// a caret the first one has already moved. Key repeat delivers the
		// rest on the frames that follow.
		return
	}
}

// applyEnter is the Enter gesture at a caret, in bytes.
func (inst *App) applyEnter(caretByte int, shift bool) {
	act := enterAction(inst.src, caretByte, shift)
	switch {
	case act.exit:
		if inst.rebindBuffer(act.next) {
			inst.requestCaret(act.caret, act.caret, true)
		}
	default:
		inst.pendingInsert = act.insert
	}
}

// applyTab is the Tab gesture at a caret, in bytes.
func (inst *App) applyTab(caretByte int, shift bool) {
	act := tabAction(inst.src, caretByte, shift)
	switch {
	case act.rebind:
		if inst.rebindBuffer(act.next) {
			inst.requestCaret(act.caret, act.caret, true)
		}
	case act.insert != "":
		inst.pendingInsert = act.insert
	}
}

// applyChord runs one formatting action from the keyboard, exactly as its bar
// button would.
func (inst *App) applyChord(key string) {
	for _, act := range formatActions {
		if act.key == key {
			selStart, selEnd := c.UnpackCursorRange(inst.cursor)
			inst.pendingInsert = formatSnippet(inst.src, act, selStart, selEnd)
			return
		}
	}
}

// keyAction is what a captured key resolved to: a snippet for the widget to
// splice, or a rewritten buffer with the caret it should land on.
type keyAction struct {
	insert string
	exit   bool
	rebind bool
	next   string
	caret  int
}

// linePrefix is the marker run at the start of a line: indentation, any
// number of `> ` quote markers, and at most one list marker, with the list
// marker's parts broken out so a continuation can renumber it or reset its
// task box.
type linePrefix struct {
	// text is the whole prefix as written, indentation included.
	text string
	// list is the list marker alone ("- ", "1. ", "- [x] "), "" for none.
	list string
	// ordered is the list marker's number when it has one.
	ordered bool
	number  int
	// task is the task-box part of a list marker ("[x] "), "" for none.
	task string
	// quote says the prefix carries at least one `> `.
	quote bool
}

// parseLinePrefix reads the marker run of the line starting at lineStart.
func parseLinePrefix(line string) (p linePrefix) {
	i := 0
	// indentation
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	// quote markers, each with its optional following space
	for i < len(line) && line[i] == '>' {
		p.quote = true
		i++
		if i < len(line) && line[i] == ' ' {
			i++
		}
		for i < len(line) && line[i] == ' ' {
			i++
		}
	}
	// one list marker
	markStart := i
	switch {
	case i < len(line) && (line[i] == '-' || line[i] == '*' || line[i] == '+') && i+1 < len(line) && line[i+1] == ' ':
		i += 2
	default:
		j := i
		for j < len(line) && line[j] >= '0' && line[j] <= '9' && j-i < 9 {
			j++
		}
		if j > i && j+1 < len(line) && (line[j] == '.' || line[j] == ')') && line[j+1] == ' ' {
			p.ordered = true
			n := 0
			for _, d := range line[i:j] {
				n = n*10 + int(d-'0')
			}
			p.number = n
			i = j + 2
		}
	}
	if i > markStart {
		p.list = line[markStart:i]
		// a task box right after the marker
		if i+3 < len(line) && line[i] == '[' && line[i+2] == ']' && line[i+3] == ' ' {
			p.task = line[i : i+4]
			i += 4
		}
	}
	p.text = line[:i]
	return
}

// continuation is the prefix the next line starts with: the same indentation
// and quote markers, the list marker renumbered, a task box emptied.
func (p linePrefix) continuation() string {
	if p.list == "" {
		return p.text
	}
	head := p.text[:len(p.text)-len(p.list)-len(p.task)]
	list := p.list
	if p.ordered {
		list = itoa(p.number+1) + p.list[len(p.list)-2:]
	}
	task := ""
	if p.task != "" {
		task = "[ ] "
	}
	return head + list + task
}

// enterAction decides what Enter does at caretByte. Shift gives a bare
// newline; so does a line with no marker. A marked line continues its marker,
// and a line that is nothing but its marker exits — the marker is removed and
// the caret parks where it was.
func enterAction(src string, caretByte int, shift bool) (act keyAction) {
	if caretByte < 0 {
		caretByte = 0
	}
	if caretByte > len(src) {
		caretByte = len(src)
	}
	act.insert = "\n"
	if shift {
		return
	}
	ls := lineStart(src, caretByte)
	le := len(src)
	if nl := strings.IndexByte(src[ls:], '\n'); nl >= 0 {
		le = ls + nl
	}
	line := src[ls:le]
	p := parseLinePrefix(line)
	if p.list == "" && !p.quote {
		return
	}
	// The caret inside the marker itself gets a plain newline: splitting a
	// marker in two is what the reader asked for by putting the caret there.
	if caretByte < ls+len(p.text) {
		return
	}
	if strings.TrimSpace(line[len(p.text):]) == "" {
		// Nothing but the marker: leave the list. A quote-only line exits
		// the quote the same way.
		act = keyAction{exit: true, next: src[:ls] + src[ls+len(p.text):], caret: ls}
		return
	}
	act.insert = "\n" + p.continuation()
	return
}

// tabAction decides what Tab does at caretByte: a list line indents by one
// unit (Shift removes one), anything else inserts a tab character.
func tabAction(src string, caretByte int, shift bool) (act keyAction) {
	if caretByte < 0 {
		caretByte = 0
	}
	if caretByte > len(src) {
		caretByte = len(src)
	}
	ls := lineStart(src, caretByte)
	le := len(src)
	if nl := strings.IndexByte(src[ls:], '\n'); nl >= 0 {
		le = ls + nl
	}
	line := src[ls:le]
	p := parseLinePrefix(line)
	if p.list == "" {
		if shift {
			return
		}
		act.insert = "\t"
		return
	}
	if !shift {
		act = keyAction{rebind: true, next: src[:ls] + indentUnit + src[ls:], caret: caretByte + len(indentUnit)}
		return
	}
	// outdent: one tab, or up to four leading spaces
	remove := 0
	switch {
	case strings.HasPrefix(line, "\t"):
		remove = 1
	default:
		for remove < 4 && remove < len(line) && line[remove] == ' ' {
			remove++
		}
	}
	if remove == 0 {
		return
	}
	caret := caretByte - remove
	if caret < ls {
		caret = ls
	}
	act = keyAction{rebind: true, next: src[:ls] + src[ls+remove:], caret: caret}
	return
}
