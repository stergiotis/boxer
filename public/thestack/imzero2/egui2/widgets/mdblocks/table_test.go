package mdblocks

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

const tableSrc = "| a | b |\n| - | - |\n| x | 2 |\n| y | 10 |"

func loadTable(text string) *tableState {
	var t tableState
	t.load(text)
	return &t
}

func TestTableRowOperationsKeepTheHeader(t *testing.T) {
	tb := loadTable(tableSrc)
	tb.insertRow(0) // clamped below the header
	if len(tb.rows) != 4 || tb.rows[0][0] != "a" || tb.rows[1][0] != "" {
		t.Fatalf("insert above header: %v", tb.rows)
	}
	if tb.compose() != "| a | b |\n| - | - |\n|  |  |\n| x | 2 |\n| y | 10 |" {
		t.Fatalf("%q", tb.compose())
	}
	if tb.moveRow(1, -1) || tb.moveRow(0, +1) {
		t.Fatal("a row moved across the header")
	}
	if !tb.moveRow(2, +1) || tb.rows[2][0] != "y" || tb.rows[3][0] != "x" {
		t.Fatalf("move down: %v", tb.rows)
	}
	if !tb.deleteRow(1) || len(tb.rows) != 3 || tb.rows[1][0] != "y" {
		t.Fatalf("delete: %v", tb.rows)
	}
	// moved rows keep their original lines verbatim
	if tb.compose() != "| a | b |\n| - | - |\n| y | 10 |\n| x | 2 |" {
		t.Fatalf("%q", tb.compose())
	}
	if !tb.deleteRow(0) || tb.rows[0][0] != "y" {
		t.Fatalf("deleting the header promotes the row below: %v", tb.rows)
	}
	tb.deleteRow(1)
	if tb.deleteRow(0) || len(tb.rows) != 1 {
		t.Fatal("the last row is kept")
	}
	if tb.compose() != "| y | 10 |\n| - | - |" {
		t.Fatalf("%q", tb.compose())
	}
}

func TestTableColumnOperations(t *testing.T) {
	tb := loadTable("| a | b |\n|:--|--:|\n| x | 2 |")
	tb.insertColumn(1)
	if tb.cols() != 3 || tb.align[1] != "---" || tb.rows[1][1] != "" || tb.rows[1][2] != "2" {
		t.Fatalf("%v %v", tb.align, tb.rows)
	}
	if !tb.moveColumn(1, +1) || tb.align[2] != "---" || tb.align[1] != "--:" || tb.rows[1][1] != "2" {
		t.Fatalf("move: %v %v", tb.align, tb.rows)
	}
	if !tb.deleteColumn(2) || tb.cols() != 2 || tb.rows[0][1] != "b" {
		t.Fatalf("delete: %v", tb.rows)
	}
	tb.setAlign(0, alignCenter)
	if alignOf(tb.align[0]) != alignCenter || alignOf(tb.align[1]) != alignRight || alignOf("---") != alignNone || alignOf(":--") != alignLeft {
		t.Fatalf("%v", tb.align)
	}
	if tb.compose() != "| a | b |\n| :-: | --: |\n| x | 2 |" {
		t.Fatalf("%q", tb.compose())
	}
	tb.deleteColumn(0)
	if tb.deleteColumn(0) || tb.cols() != 1 {
		t.Fatal("the last column is kept")
	}
}

func TestTableSortIsNumericAwareAndStable(t *testing.T) {
	tb := loadTable("| n | v |\n| - | - |\n| b | 10 |\n| e |  |\n| a | 9 |\n| c | x |\n| B | 9 |")
	names := func() (got []string) {
		for _, r := range tb.rows[1:] {
			got = append(got, r[0])
		}
		return
	}
	tb.sortBody(1, false)
	if got := names(); !equalCells(got, []string{"a", "B", "b", "c", "e"}) {
		t.Fatalf("ascending, numbers before text, empty last: %v", got)
	}
	tb.sortBody(1, true)
	if got := names(); !equalCells(got, []string{"c", "b", "a", "B", "e"}) {
		t.Fatalf("descending, empty still last: %v", got)
	}
	tb.sortBody(0, true)
	// B and b fold equal and keep the order the previous sort left them in
	if got := names(); !equalCells(got, []string{"e", "c", "b", "B", "a"}) {
		t.Fatalf("descending, case folded, stable: %v", got)
	}
	if tb.rows[0][0] != "n" {
		t.Fatal("the header moved")
	}
}

func TestTableExpandsAPastedBlockAndReadsOutTSV(t *testing.T) {
	tb := loadTable(tableSrc)
	tb.rows[1][1] = "p\tq\r\nr\ts\tt\n"
	if !tb.expandPasted() || tb.expandPasted() {
		t.Fatal("expanded once")
	}
	want := [][]string{{"a", "b", "", ""}, {"x", "p", "q", ""}, {"y", "r", "s", "t"}}
	if len(tb.rows) != len(want) || tb.cols() != 4 {
		t.Fatalf("%v", tb.rows)
	}
	for r := range want {
		if !equalCells(tb.rows[r], want[r]) {
			t.Fatalf("%v", tb.rows)
		}
	}
	if tsv(tb.rows[1:3]) != "x\tp\tq\t\ny\tr\ts\tt" {
		t.Fatalf("%q", tsv(tb.rows[1:3]))
	}
}

// keyOn simulates one captured key on the cell at (r, col) of the open
// table, the way the state manager would report it next frame.
func keyOn(e *Editor, r, col int, code keycodes.Code, mods uint8) {
	e.applyTableKey(e.sub.tbl.ids[r][col], c.CapturedKey{Code: code, Mods: mods})
}

func TestTableMenuOperationSplicesAndUndoesAsOne(t *testing.T) {
	e := newTestEditor(t, "intro\n\n"+tableSrc+"\n")
	e.FocusAt(10)
	frame(e)
	if e.sub.kind != subTable {
		t.Fatal("no grid")
	}
	// a menu click lands as a recorded operation, applied next frame
	e.sub.tbl.op = func() { e.sub.tbl.sortBody(1, true) }
	frame(e)
	if e.src != "intro\n\n| a | b |\n| - | - |\n| y | 10 |\n| x | 2 |\n" {
		t.Fatalf("sorted: %q", e.src)
	}
	if n := len(e.hist.undo); n != 1 {
		t.Fatalf("one entry, got %d", n)
	}
	e.Undo()
	frame(e)
	if e.src != "intro\n\n"+tableSrc+"\n" || e.sub.kind != subTable || e.sub.tbl.rows[1][0] != "x" {
		t.Fatalf("undone: %q %v", e.src, e.sub.tbl.rows)
	}
}

func TestTableKeysReshapeAndWalk(t *testing.T) {
	e := newTestEditor(t, tableSrc)
	e.FocusAt(1)
	frame(e)
	// the row menu's "insert below" on x, recorded for the next frame
	e.sub.tbl.op = func() { e.sub.tbl.insertRow(2); e.sub.tbl.setFocus(2, 0, -1) }
	frame(e)
	if e.src != "| a | b |\n| - | - |\n| x | 2 |\n|  |  |\n| y | 10 |" || e.sub.tbl.focus || e.sub.tbl.focusR != 2 {
		t.Fatalf("insert below: %q focus %v", e.src, e.sub.tbl.focus)
	}
	keyOn(e, 2, 0, keycodes.ArrowDown, uint8(keycodes.ModCommand))
	if e.src != "| a | b |\n| - | - |\n| x | 2 |\n| y | 10 |\n|  |  |" {
		t.Fatalf("Ctrl+Down: %q", e.src)
	}
	frame(e)
	keyOn(e, 1, 1, keycodes.ArrowLeft, uint8(keycodes.ModCommand))
	if e.src != "| b | a |\n| - | - |\n| 2 | x |\n| 10 | y |\n|  |  |" {
		t.Fatalf("Ctrl+Left: %q", e.src)
	}
	frame(e)
	// plain arrows walk; Up from the header leaves the block, which here
	// has nothing above it
	keyOn(e, 1, 0, keycodes.ArrowUp, 0)
	if !e.sub.tbl.focus || e.sub.tbl.focusR != 0 || e.sub.tbl.focusC != 0 {
		t.Fatalf("up: %+v", e.sub.tbl)
	}
	frame(e)
	keyOn(e, 0, 1, keycodes.ArrowRight, 0) // caret at 0 of "a": not an edge
	if e.sub.tbl.focus {
		t.Fatal("Right away from the end moved")
	}
	e.sub.tbl.cursors[0][1] = c.PackCursorRange(1, 1)
	keyOn(e, 0, 1, keycodes.ArrowRight, 0)
	if !e.sub.tbl.focus || e.sub.tbl.focusR != 1 || e.sub.tbl.focusC != 0 || e.sub.tbl.focusChar != 0 {
		t.Fatalf("Right at the end wraps to the next row's first cell: %+v", e.sub.tbl)
	}
	frame(e)
	keyOn(e, 1, 0, keycodes.ArrowLeft, 0)
	if !e.sub.tbl.focus || e.sub.tbl.focusR != 0 || e.sub.tbl.focusC != 1 {
		t.Fatalf("Left at the start wraps back: %+v", e.sub.tbl)
	}
	if n := len(e.hist.undo); n != 3 {
		t.Fatalf("three structural entries, got %d", n)
	}
}

func TestTableWrapChordIsACellInsert(t *testing.T) {
	e := newTestEditor(t, tableSrc)
	e.FocusAt(1)
	frame(e)
	e.sub.tbl.cursors[1][0] = c.PackCursorRange(0, 1)
	keyOn(e, 1, 0, keycodes.KeyB, uint8(keycodes.ModCommand))
	ins := e.sub.tbl.insert
	if !ins.ok || ins.r != 1 || ins.col != 0 || ins.text != "**x**" || !e.sealNext {
		t.Fatalf("%+v", ins)
	}
	frame(e) // the emit consumes it
	if e.sub.tbl.insert.ok {
		t.Fatal("not consumed")
	}
}

func TestTablePasteIntoACellExpands(t *testing.T) {
	e := newTestEditor(t, tableSrc)
	e.FocusAt(1)
	frame(e)
	e.sub.tbl.rows[2][1] = "7\tnew"
	frame(e)
	if e.src != "| a | b |  |\n| - | - | --- |\n| x | 2 |  |\n| y | 7 | new |" {
		t.Fatalf("%q", e.src)
	}
}
