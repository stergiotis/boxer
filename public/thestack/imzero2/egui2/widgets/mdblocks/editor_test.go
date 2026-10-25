package mdblocks

import (
	"reflect"
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
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

func TestEdgeKeyMaskFollowsTheCaretRow(t *testing.T) {
	up, down, bs := keycodes.MaskOf(keycodes.ArrowUp), keycodes.MaskOf(keycodes.ArrowDown), keycodes.MaskOf(keycodes.Backspace)
	// No geometry yet: the text's edges stand in for the rows.
	if m := edgeKeyMask(3, 10, true, c.TextRectsValue{}, false); m != 0 {
		t.Fatalf("mid-text without geometry: %b", m)
	}
	if m := edgeKeyMask(0, 10, true, c.TextRectsValue{}, false); m != up|bs {
		t.Fatalf("start without geometry: %b", m)
	}
	if m := edgeKeyMask(10, 10, true, c.TextRectsValue{}, false); m != down {
		t.Fatalf("end without geometry: %b", m)
	}
	// With geometry the row decides, wherever the caret is in the text.
	if m := edgeKeyMask(5, 10, true, c.TextRectsValue{Row: 0, Rows: 3}, true); m != up {
		t.Fatalf("first row: %b", m)
	}
	if m := edgeKeyMask(5, 10, true, c.TextRectsValue{Row: 1, Rows: 3}, true); m != 0 {
		t.Fatalf("middle row: %b", m)
	}
	if m := edgeKeyMask(5, 10, true, c.TextRectsValue{Row: 2, Rows: 3}, true); m != down {
		t.Fatalf("last row: %b", m)
	}
	if m := edgeKeyMask(0, 10, true, c.TextRectsValue{Row: 0, Rows: 1}, true); m != up|down|bs {
		t.Fatalf("one row at the start: %b", m)
	}
	// A selection captures nothing: Shift+arrows extend it.
	if m := edgeKeyMask(0, 10, false, c.TextRectsValue{Row: 0, Rows: 1}, true); m != 0 {
		t.Fatalf("selection: %b", m)
	}
}

func TestToggleTaskFlipsTheBoxInPlace(t *testing.T) {
	e := newTestEditor(t, "- [ ] a\n- [x] b\n")
	frame(e)
	e.toggleTask(markdown.TaskClick{Offset: 2})
	e.toggleTask(markdown.TaskClick{Offset: 10, Checked: true})
	if e.src != "- [x] a\n- [ ] b\n" {
		t.Fatalf("%q", e.src)
	}
	// Not a box: nothing happens.
	e.toggleTask(markdown.TaskClick{Offset: 0})
	e.toggleTask(markdown.TaskClick{Offset: 99})
	if e.src != "- [x] a\n- [ ] b\n" {
		t.Fatalf("%q", e.src)
	}
	e.Undo()
	if e.src != "- [x] a\n- [x] b\n" {
		t.Fatalf("undo: %q", e.src)
	}
}

func TestBlockChordsMoveDuplicateAndSetHeadings(t *testing.T) {
	e := newTestEditor(t, "one\n\ntwo\n\nthree")
	e.FocusAt(6) // "t|wo"
	frame(e)
	if !e.applyBlockChord(keycodes.ArrowUp) || e.src != "two\n\none\n\nthree" || e.caret != 1 {
		t.Fatalf("up: %q caret %d", e.src, e.caret)
	}
	frame(e)
	if !e.applyBlockChord(keycodes.ArrowDown) || e.src != "one\n\ntwo\n\nthree" || e.caret != 6 {
		t.Fatalf("down: %q caret %d", e.src, e.caret)
	}
	frame(e)
	if !e.applyBlockChord(keycodes.KeyD) || e.src != "one\n\ntwo\n\ntwo\n\nthree" || e.caret != 11 {
		t.Fatalf("duplicate: %q caret %d", e.src, e.caret)
	}
	frame(e)
	if !e.applyBlockChord(keycodes.Digit2) {
		t.Fatal("heading chord")
	}
	if e.src != "one\n\ntwo\n\n## two\n\nthree" || e.caret != 14 {
		t.Fatalf("heading: %q caret %d", e.src, e.caret)
	}
	frame(e)
	if !e.applyBlockChord(keycodes.Digit0) || e.src != "one\n\ntwo\n\ntwo\n\nthree" {
		t.Fatalf("paragraph: %q", e.src)
	}
	if e.applyBlockChord(keycodes.KeyB) {
		t.Fatal("a wrap is not a block chord")
	}
	// The first block cannot move up; the last cannot move down.
	e.FocusAt(1)
	frame(e)
	if e.applyBlockChord(keycodes.ArrowUp); e.src != "one\n\ntwo\n\ntwo\n\nthree" {
		t.Fatalf("first up: %q", e.src)
	}
}

func TestSelectionChordsMoveAndDuplicateTheRange(t *testing.T) {
	e := newTestEditor(t, "a\n\nb\n\nc\n\nd")
	e.FocusAt(3)
	frame(e)
	e.selectCurrent()
	e.selB = 2 // b..c
	if !e.moveBlocks(1, 2, 1) || e.src != "a\n\nd\n\nb\n\nc" || e.selA != 2 || e.selB != 3 {
		t.Fatalf("move down: %q sel %d..%d", e.src, e.selA, e.selB)
	}
	if !e.duplicateBlocks(2, 3) || e.src != "a\n\nd\n\nb\n\nc\n\nb\n\nc" || e.selA != 4 || e.selB != 5 {
		t.Fatalf("duplicate: %q sel %d..%d", e.src, e.selA, e.selB)
	}
}

func TestPastedURLOverASelectionBecomesALink(t *testing.T) {
	if _, ok := pastedLink("", "https://x.y"); ok {
		t.Fatal("no selection")
	}
	if _, ok := pastedLink("two\nlines", "https://x.y"); ok {
		t.Fatal("multi-line selection")
	}
	if _, ok := pastedLink("word", "plain text"); ok {
		t.Fatal("not an address")
	}
	if _, ok := pastedLink("https://a.b", "https://x.y"); ok {
		t.Fatal("an address over an address")
	}
	if l, ok := pastedLink("docs", "www.example.com/d"); !ok || l != "[docs](www.example.com/d)" {
		t.Fatalf("%q %v", l, ok)
	}
	e := newTestEditor(t, "see docs here")
	e.FocusAt(4)
	frame(e)
	// The widget held "docs" selected as of that emit, then wrote back the
	// selection replaced by a pasted address sharing its first letter —
	// which a diff of the two buffers would split in the wrong place.
	e.bufCursor = c.PackCursorRange(4, 8)
	frame(e)
	typeInto(e, "see https://docs.x.y/d here", 22)
	frame(e)
	if e.src != "see [docs](https://docs.x.y/d) here" || e.caret != 30 {
		t.Fatalf("%q caret %d", e.src, e.caret)
	}
	frame(e)
	if e.buf != "see [docs](https://docs.x.y/d) here" {
		t.Fatalf("rebound: %q", e.buf)
	}
	e.Undo()
	if e.src != "see docs here" {
		t.Fatalf("undo: %q", e.src)
	}
}

func TestMatchesPaintOnlyInsideTheOpenBlock(t *testing.T) {
	e := newTestEditor(t, "one two\n\nthree two")
	e.SetMatches([]Span{{4, 7}, {15, 18}}, 1)
	if secs, cuts, key := e.matchOverlays(); secs != nil || cuts != nil || key != "" {
		t.Fatalf("nothing open: %v %v %q", secs, cuts, key)
	}
	e.FocusAt(10)
	frame(e)
	secs, cuts, key := e.matchOverlays()
	if len(secs) != 1 || secs[0].Start != 6 || secs[0].Stop != 9 || secs[0].Flags&codeview.StyleUnderline == 0 {
		t.Fatalf("current match in the open block: %+v", secs)
	}
	if !reflect.DeepEqual(cuts, []int{6, 9}) || key != "6:9* " {
		t.Fatalf("cuts %v key %q", cuts, key)
	}
	e.SetMatches([]Span{{4, 7}, {15, 18}}, 0)
	secs, _, key = e.matchOverlays()
	if secs[0].Flags&codeview.StyleUnderline != 0 || key != "6:9 " {
		t.Fatalf("not current: %+v %q", secs, key)
	}
	// A match straddling the block's start is clipped to it.
	e.SetMatches([]Span{{7, 11}}, -1)
	secs, _, _ = e.matchOverlays()
	if len(secs) != 1 || secs[0].Start != 0 || secs[0].Stop != 2 {
		t.Fatalf("clipped: %+v", secs)
	}
}
