package mdblocks

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

func newTestEditor(t *testing.T, src string) *Editor {
	t.Helper()
	t.Cleanup(scenetest.Install())
	e := New(c.NewWidgetIdStack(), "t", Opts{})
	e.SetSource(src)
	return e
}

func frame(e *Editor) Events {
	ev := e.Render()
	c.CurrentApplicationState.StateManager.ScriptReset()
	return ev
}

// typeInto simulates the TextEdit's write-back: the buffer it holds after
// the person typed, and the caret it reports, both landing a frame late.
func typeInto(e *Editor, buf string, caretChars int) {
	e.buf = buf
	e.bufCursor = c.PackCursorRange(caretChars, caretChars)
}

func TestRenderHeadlessAndFocusDerivesTheBlock(t *testing.T) {
	e := newTestEditor(t, "# Title\n\nfirst para\n\n- a\n- b\n")
	ev := frame(e)
	if ev.Editing || ev.Caret != -1 || ev.Changed {
		t.Fatalf("fresh: %+v", ev)
	}
	e.FocusAt(10) // inside "first para"
	ev = frame(e)
	if !ev.Editing || ev.Caret != 10 || e.buf != "first para" || e.bufStart != 9 {
		t.Fatalf("focus: %+v buf %q start %d", ev, e.buf, e.bufStart)
	}
}

func TestTypingSplicesOnlyTheEditedBytes(t *testing.T) {
	src := "# Title\n\nfirst para\n\n- a\n- b\n"
	e := newTestEditor(t, src)
	e.FocusAt(14) // "first| para"
	frame(e)
	typeInto(e, "firstX para", 6)
	ev := frame(e)
	if !ev.Changed || e.src != "# Title\n\nfirstX para\n\n- a\n- b\n" {
		t.Fatalf("splice: changed %v src %q", ev.Changed, e.src)
	}
	if ev.Caret != 15 {
		t.Fatalf("caret %d", ev.Caret)
	}
	if !e.CanUndo() {
		t.Fatal("history")
	}
	e.Undo()
	if e.src != src || e.caret != 14 {
		t.Fatalf("undo: %q caret %d", e.src, e.caret)
	}
	e.Redo()
	if e.src != "# Title\n\nfirstX para\n\n- a\n- b\n" {
		t.Fatalf("redo: %q", e.src)
	}
}

func TestEnterAtEndOfParagraphOpensAGapAndTypingPadsIt(t *testing.T) {
	e := newTestEditor(t, "one\n\ntwo")
	e.FocusAt(3) // end of "one"
	frame(e)
	e.applyEnter(3, false)
	frame(e)
	if !e.editing || e.bufStart != e.bufStop || e.bufStart != 4 || e.buf != "" {
		t.Fatalf("gap: start %d stop %d buf %q src %q", e.bufStart, e.bufStop, e.buf, e.src)
	}
	frame(e)
	typeInto(e, "new", 3)
	frame(e)
	if e.src != "one\n\nnew\n\ntwo" {
		t.Fatalf("padded: %q", e.src)
	}
	if e.buf != "new" || e.bufStart != 5 {
		t.Fatalf("rebound to the new block: %q at %d", e.buf, e.bufStart)
	}
}

func TestEnterMidParagraphSplitsIntoTwoBlocks(t *testing.T) {
	e := newTestEditor(t, "one two")
	e.FocusAt(3)
	frame(e)
	e.applyEnter(3, false)
	frame(e)
	if e.src != "one\n\n two" || e.buf != " two" || e.caret != 5 {
		t.Fatalf("split: %q buf %q caret %d", e.src, e.buf, e.caret)
	}
}

func TestEnterInAListContinuesAndExits(t *testing.T) {
	e := newTestEditor(t, "- a")
	e.FocusAt(3)
	frame(e)
	e.applyEnter(3, false)
	if e.pendingInsert != "\n- " {
		t.Fatalf("continue: %q", e.pendingInsert)
	}
	frame(e) // emits the insert; the widget would write back next frame
	typeInto(e, "- a\n- ", 6)
	frame(e)
	if e.src != "- a\n- " {
		t.Fatalf("after insert: %q", e.src)
	}
	e.applyEnter(6, false)
	frame(e)
	if e.src != "- a\n" || e.caret != 4 {
		t.Fatalf("exit: %q caret %d", e.src, e.caret)
	}
}

func TestBackspaceAtStartDemotesThenMerges(t *testing.T) {
	e := newTestEditor(t, "para\n\n## Head")
	e.FocusAt(6)
	frame(e)
	e.applyBackspaceAtStart()
	if e.src != "para\n\nHead" || e.caret != 6 {
		t.Fatalf("demote: %q caret %d", e.src, e.caret)
	}
	frame(e)
	e.applyBackspaceAtStart()
	if e.src != "para\nHead" || e.caret != 5 {
		t.Fatalf("merge: %q caret %d", e.src, e.caret)
	}
}

func TestBackspaceInAGapClosesOntoTheBlockAbove(t *testing.T) {
	e := newTestEditor(t, "para\n\n")
	e.FocusAt(5)
	frame(e)
	e.applyBackspaceAtStart()
	if e.src != "para\n" || e.caret != 4 {
		t.Fatalf("gap close: %q caret %d", e.src, e.caret)
	}
}

func TestArrowsMoveBetweenBlocksAndEscapeSelects(t *testing.T) {
	e := newTestEditor(t, "one\n\ntwo\n\nthree")
	e.FocusAt(5)
	frame(e)
	e.moveToNeighbour(-1)
	if e.caret != 3 {
		t.Fatalf("up: %d", e.caret)
	}
	e.moveToNeighbour(+1)
	if e.caret != 5 {
		t.Fatalf("down: %d", e.caret)
	}
	frame(e)
	e.selectCurrent()
	if !e.selMode || e.selA != 1 || e.editing {
		t.Fatalf("select: %+v", e)
	}
	e.selB = 2
	e.deleteSelection()
	if e.src != "one\n\n" || !e.editing || e.caret != 5 {
		t.Fatalf("delete: %q caret %d", e.src, e.caret)
	}
	e.Undo()
	if e.src != "one\n\ntwo\n\nthree" {
		t.Fatalf("undo delete: %q", e.src)
	}
}

func TestSetSourceResetsEditingAndHistory(t *testing.T) {
	e := newTestEditor(t, "a")
	e.FocusAt(1)
	frame(e)
	e.splice(1, "", "b", 2, false)
	e.SetSource("fresh")
	if e.editing || e.CanUndo() || e.src != "fresh" {
		t.Fatal("reset")
	}
}
