package mdedit

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdline"
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
//   - Ctrl+0..6 as chords: the caret's line becomes a heading of that
//     level, or a paragraph for 0 (mdline.SetHeading).
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
var editorChordMask = keycodes.MaskOf(keycodes.KeyB, keycodes.KeyI, keycodes.KeyK, keycodes.KeyE,
	keycodes.Digit0, keycodes.Digit1, keycodes.Digit2, keycodes.Digit3, keycodes.Digit4, keycodes.Digit5, keycodes.Digit6)

// headingChords maps Ctrl+0..6 to a heading level, 0 a paragraph: the
// caret's line is rewritten through mdline.SetHeading, the same rewrite the
// block editor's chord makes.
var headingChords = map[keycodes.Code]int{
	keycodes.Digit0: 0, keycodes.Digit1: 1, keycodes.Digit2: 2, keycodes.Digit3: 3,
	keycodes.Digit4: 4, keycodes.Digit5: 5, keycodes.Digit6: 6,
}

var editorChords = map[keycodes.Code]string{
	keycodes.KeyB: "bold",
	keycodes.KeyI: "italic",
	keycodes.KeyK: "link",
	keycodes.KeyE: "code",
}

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
	caretByte := mdline.CharToByte(inst.src, caret)
	for _, k := range keys {
		switch {
		case k.Code == keycodes.Enter:
			inst.applyEnter(caretByte, k.Shift())
		case k.Code == keycodes.Tab:
			inst.applyTab(caretByte, k.Shift())
		case k.Command():
			if key, ok := editorChords[k.Code]; ok {
				inst.applyChord(key)
			} else if level, ok := headingChords[k.Code]; ok {
				inst.applyLineAction(mdline.SetHeading(inst.src, caretByte, level))
			}
		}
		// One gesture per frame: a second key in the same batch would act on
		// a caret the first one has already moved. Key repeat delivers the
		// rest on the frames that follow.
		return
	}
}

// applyEnter is the Enter gesture at a caret, in bytes (mdline.Enter).
func (inst *App) applyEnter(caretByte int, shift bool) {
	inst.applyLineAction(mdline.Enter(inst.src, caretByte, shift))
}

// applyTab is the Tab gesture at a caret, in bytes (mdline.Tab).
func (inst *App) applyTab(caretByte int, shift bool) {
	inst.applyLineAction(mdline.Tab(inst.src, caretByte, shift))
}

// applyLineAction lands a line gesture: a snippet goes to the widget as
// insertAtCursor, a rewrite is a rebind with the caret moved along.
func (inst *App) applyLineAction(act mdline.Action) {
	switch {
	case act.Rebind:
		if inst.rebindBuffer(act.Next) {
			inst.requestCaret(act.Caret, act.Caret, true)
		}
	case act.Insert != "":
		inst.pendingInsert = act.Insert
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
