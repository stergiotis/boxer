package mdblocks

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
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

func TestUnparsedSourceIsARawBlockAndNeverMergedInto(t *testing.T) {
	src := "a\n\n[r]: http://x\n\n<div>\nhtml\n</div>\n\nb [r]"
	e := newTestEditor(t, src)
	texts := make([]string, 0, len(e.blocks))
	for _, b := range e.blocks {
		texts = append(texts, src[b.start:b.stop])
	}
	want := []string{"a", "[r]: http://x", "<div>\nhtml\n</div>", "b [r]"}
	if len(texts) != len(want) {
		t.Fatalf("blocks %q", texts)
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Fatalf("block %d: %q want %q", i, texts[i], want[i])
		}
	}
	if e.blocks[1].kind != kindRaw || e.blocks[2].kind != kindRaw {
		t.Fatalf("kinds: %+v", e.blocks)
	}
	e.FocusAt(e.blocks[3].start)
	frame(e)
	e.applyBackspaceAtStart()
	if e.src != src || !e.selMode || e.selA != 2 {
		t.Fatalf("merge into raw: src %q sel %v %d", e.src, e.selMode, e.selA)
	}
}

func TestSetextHeadingLeavesAndDemotesWhole(t *testing.T) {
	e := newTestEditor(t, "Title\n=====\n\npara")
	e.FocusAt(11)
	frame(e)
	if e.buf != "Title\n=====" {
		t.Fatalf("buf %q", e.buf)
	}
	e.applyEnter(len(e.buf), false)
	frame(e)
	typeInto(e, "x", 1)
	frame(e)
	if e.src != "Title\n=====\n\nx\n\npara" {
		t.Fatalf("leave: %q", e.src)
	}
	e = newTestEditor(t, "Title\n=====\n\npara")
	e.FocusAt(0)
	frame(e)
	e.applyBackspaceAtStart()
	if e.src != "Title\n\npara" || e.caret != 0 {
		t.Fatalf("demote: %q caret %d", e.src, e.caret)
	}
}

func TestShiftEnterAtTheEndIsASoftBreak(t *testing.T) {
	e := newTestEditor(t, "para\n\nnext")
	e.FocusAt(4)
	frame(e)
	e.applyEnter(4, true)
	if e.pendingInsert != "\n" || !e.tailOk || e.tailAt != 5 {
		t.Fatalf("insert %q tail %v at %d", e.pendingInsert, e.tailOk, e.tailAt)
	}
	frame(e)
	typeInto(e, "para\n", 5)
	frame(e)
	if e.src != "para\n\n\nnext" || e.buf != "para\n" || e.bufStart != 0 || e.bufStop != 5 {
		t.Fatalf("after the break: src %q buf %q [%d,%d)", e.src, e.buf, e.bufStart, e.bufStop)
	}
	typeInto(e, "para\nx", 6)
	frame(e)
	if e.src != "para\nx\n\nnext" || e.caret != 6 || e.tailOk {
		t.Fatalf("soft break: %q caret %d tail %v", e.src, e.caret, e.tailOk)
	}
}

func TestBackspaceIntoAnATXHeadingJoinsItsLine(t *testing.T) {
	e := newTestEditor(t, "# Title\n\npara")
	e.FocusAt(9)
	frame(e)
	e.applyBackspaceAtStart()
	if e.src != "# Titlepara" || e.caret != 7 || len(e.hist.undo) != 1 {
		t.Fatalf("join: %q caret %d undo %d", e.src, e.caret, len(e.hist.undo))
	}
	e = newTestEditor(t, "![a](b.png)\n\npara")
	e.FocusAt(13)
	frame(e)
	e.applyBackspaceAtStart()
	if !e.selMode || e.selA != 0 || e.src != "![a](b.png)\n\npara" {
		t.Fatalf("image above: sel %v %d src %q", e.selMode, e.selA, e.src)
	}
}

func TestSpliceThatChangesNothingRecordsNothing(t *testing.T) {
	e := newTestEditor(t, "para")
	e.splice(0, "pa", "pa", 2, true)
	if len(e.hist.undo) != 0 || e.changed || e.caret != 2 {
		t.Fatalf("undo %d changed %v caret %d", len(e.hist.undo), e.changed, e.caret)
	}
}

func TestTypingKeepsTheBindingWithoutARebind(t *testing.T) {
	e := newTestEditor(t, "para\n\nnext")
	e.FocusAt(4)
	frame(e)
	n := e.rebinds
	typeInto(e, "parab", 5)
	frame(e)
	typeInto(e, "parabc", 6)
	frame(e)
	if e.rebinds != n || e.bufStop != 6 || e.src != "parabc\n\nnext" {
		t.Fatalf("rebinds %d stop %d src %q", e.rebinds-n, e.bufStop, e.src)
	}
	// The first character in a gap moves the binding onto the new block.
	e = newTestEditor(t, "para\n\nnext")
	e.FocusAt(5)
	frame(e)
	n = e.rebinds
	typeInto(e, "N", 1)
	frame(e)
	if e.rebinds != n || e.src != "para\n\nN\n\nnext" || e.bufStart != 6 || e.bufStop != 7 {
		t.Fatalf("gap: rebinds %d src %q [%d,%d)", e.rebinds-n, e.src, e.bufStart, e.bufStop)
	}
}

func TestGapTypingIsOneUndoEntry(t *testing.T) {
	e := newTestEditor(t, "para\n\nnext")
	e.FocusAt(5)
	frame(e)
	typeInto(e, "N", 1)
	frame(e)
	typeInto(e, "Ne", 2)
	frame(e)
	if e.src != "para\n\nNe\n\nnext" || len(e.hist.undo) != 1 {
		t.Fatalf("src %q undo %d", e.src, len(e.hist.undo))
	}
	e.Undo()
	if e.src != "para\n\nnext" {
		t.Fatalf("undo: %q", e.src)
	}
}

func TestAWrapIsItsOwnUndoEntry(t *testing.T) {
	e := newTestEditor(t, "para")
	e.FocusAt(4)
	frame(e)
	typeInto(e, "parab", 5)
	frame(e)
	e.applyChord(keycodes.KeyB)
	if e.pendingInsert != "**bold**" || !e.sealNext {
		t.Fatalf("chord: %q seal %v", e.pendingInsert, e.sealNext)
	}
	frame(e)
	typeInto(e, "parab**bold**", 13)
	frame(e)
	typeInto(e, "parab**bold**c", 14)
	frame(e)
	if n := len(e.hist.undo); n != 3 {
		t.Fatalf("undo entries %d: %+v", n, e.hist.undo)
	}
}

func TestAClickOpensAfterTheFrameIsDrawn(t *testing.T) {
	e := newTestEditor(t, "first\n\nlast")
	e.FocusAt(11)
	frame(e)
	e.pendingOpen, e.pendingOpenOk = 0, true
	frame(e)
	if e.caret != 5 || !e.pendingCaretOk {
		t.Fatalf("caret %d pending %v", e.caret, e.pendingCaretOk)
	}
	frame(e)
	if e.buf != "first" || e.caret != 5 {
		t.Fatalf("buf %q caret %d", e.buf, e.caret)
	}
}
