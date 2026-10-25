package mdblocks

import (
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdline"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// The table grid (ADR-0334 §SD3): the open table drawn through the
// deferred-body table op as a grid of single-line fields in resizable
// columns, with a handle before every row and above every column. A
// handle's menu holds the structural operations of table.go for that row
// or column; the top-left handle holds the table's. The keys walk the
// cells and reshape the grid without the menu.
//
// A menu's click lands as a response one frame late and would reshape the
// rows while they are being drawn, so it is recorded as the pending
// operation and applied at the start of the next frame, before the
// write-back that frame composes. That is the same frame the click's
// response would have arrived in anyway.

// tableKeyMask is what every cell captures unmodified: Escape leaves for a
// block selection, Enter walks down, the vertical arrows walk the rows (a
// single-line field has no use for them). The horizontal arrows join per
// frame at the field's text edges, from the caret the cell reported.
var tableKeyMask = keycodes.MaskOf(keycodes.Escape, keycodes.Enter, keycodes.ArrowUp, keycodes.ArrowDown)

// tableChordMask is what a cell captures under the command modifier, which
// a plain capture does not see: the editor's undo, redo and wraps, the
// vertical arrows for moving the row and the horizontal ones for moving
// the column. Typora's Ctrl+Enter for a row below is not here: the window
// host polls the command-Enter fetcher every frame as its sign of input,
// and whoever polls it owns the chord for the whole process. A row below
// is the handle's menu, or Enter in the last row.
var tableChordMask = keycodes.MaskOf(keycodes.KeyZ, keycodes.KeyY, keycodes.KeyB, keycodes.KeyI, keycodes.KeyK, keycodes.KeyE,
	keycodes.ArrowUp, keycodes.ArrowDown, keycodes.ArrowLeft, keycodes.ArrowRight)

const (
	// tableHandleWidth is the handle column's width, enough for one glyph
	// of a small button.
	tableHandleWidth float32 = 26
	// tableCellPad is what a field adds to the renderer's row height and
	// column width: its frame.
	tableCellPad float32 = 8
)

// cellInsert is a snippet to insert at a cell's caret on the next emit.
type cellInsert struct {
	r, col int
	text   string
	ok     bool
}

// setFocus puts the caret at the end of cell (r, col) on the next emit, or
// at char when it is not negative.
func (t *tableState) setFocus(r, col, char int) {
	t.focusR, t.focusC, t.focusChar, t.focus = r, col, char, true
}

func (t *tableState) cellOf(id uint64) (r, col int, ok bool) {
	for r := range t.ids {
		for col := range t.ids[r] {
			if t.ids[r][col] == id {
				return r, col, true
			}
		}
	}
	return 0, 0, false
}

// moveCell is Tab (next cell, Shift back) or Enter (next row, same column)
// from a cell; past the last row either adds one. Reports whether a row
// was added.
func (t *tableState) moveCell(r, col int, tab, shift bool) (added bool) {
	cols := t.cols()
	switch {
	case tab && shift:
		col--
		if col < 0 {
			col = cols - 1
			r--
		}
		if r < 0 {
			return
		}
	case tab:
		col++
		if col >= cols {
			col = 0
			r++
		}
	default:
		r++
	}
	if r >= len(t.rows) {
		t.insertRow(len(t.rows))
		added = true
	}
	t.setFocus(r, col, -1)
	return
}

// tableOp runs a structural operation as an undo entry of its own and
// splices it now, so the parse this frame derives from already holds it.
func (e *Editor) tableOp(op func()) {
	e.hist.seal()
	op()
	e.sub.rebind = true
	e.applySubWriteBack()
	e.sealNext = true
}

// applyTableKey consumes one capture from cell id. Escape is the caller's.
func (e *Editor) applyTableKey(id uint64, k c.CapturedKey) {
	t := &e.sub.tbl
	r, col, ok := t.cellOf(id)
	if !ok {
		return
	}
	cur := t.rows[r][col]
	lo, hi := c.UnpackCursorRange(t.cursors[r][col])
	last := len(t.rows) - 1
	switch {
	case k.Command():
		switch k.Code {
		case keycodes.KeyZ:
			e.Undo()
		case keycodes.KeyY:
			e.Redo()
		case keycodes.ArrowUp:
			e.tableOp(func() {
				if t.moveRow(r, -1) {
					t.setFocus(r-1, col, -1)
				}
			})
		case keycodes.ArrowDown:
			e.tableOp(func() {
				if t.moveRow(r, +1) {
					t.setFocus(r+1, col, -1)
				}
			})
		case keycodes.ArrowLeft:
			e.tableOp(func() {
				if t.moveColumn(col, -1) {
					t.setFocus(r, col-1, -1)
				}
			})
		case keycodes.ArrowRight:
			e.tableOp(func() {
				if t.moveColumn(col, +1) {
					t.setFocus(r, col+1, -1)
				}
			})
		default:
			if name, ok := chordWraps[k.Code]; ok {
				t.insert = cellInsert{r: r, col: col, text: mdline.WrapSnippet(cur, mdline.Wraps[name], lo, hi), ok: true}
				e.sealNext = true
			}
		}
	case k.Code == keycodes.Enter || k.Code == keycodes.Tab:
		if t.moveCell(r, col, k.Code == keycodes.Tab, k.Shift()) {
			// The row added is spliced now, so the parse this frame
			// derives from already holds it.
			e.hist.seal()
			e.sub.rebind = true
			e.applySubWriteBack()
		}
	case k.Code == keycodes.ArrowUp:
		if r == 0 {
			e.moveToNeighbour(-1)
		} else {
			t.setFocus(r-1, col, -1)
		}
	case k.Code == keycodes.ArrowDown:
		if r == last {
			e.moveToNeighbour(+1)
		} else {
			t.setFocus(r+1, col, -1)
		}
	case k.Code == keycodes.ArrowLeft && lo == hi && lo == 0:
		if col > 0 {
			t.setFocus(r, col-1, -1)
		} else if r > 0 {
			t.setFocus(r-1, t.cols()-1, -1)
		}
	case k.Code == keycodes.ArrowRight && lo == hi && int(lo) >= utf8.RuneCountInString(cur):
		if col < t.cols()-1 {
			t.setFocus(r, col+1, 0)
		} else if r < last {
			t.setFocus(r+1, 0, 0)
		}
	}
}

// applyTableOp runs the operation a menu recorded last frame.
func (e *Editor) applyTableOp() {
	if op := e.sub.tbl.op; op != nil {
		e.sub.tbl.op = nil
		e.tableOp(op)
	}
}

// --- rendering ---

// The handles' glyphs come from the Phosphor icon font the client loads,
// as the tree widget's disclosure glyphs do: a symbol from the text font's
// fallback chain draws at the fallback face's size and baseline.
var (
	atomsTableMenu = c.Atoms().Text(icons.PhGridFour).Keep()
	atomsColMenu   = c.Atoms().Text(icons.PhCaretDown).Keep()
	atomsRowMenu   = c.Atoms().Text(icons.PhDotsThreeVertical).Keep()
	// The menu items' labels, retained on first use: the bodies render
	// only while open, so each label is built once and kept.
	tableItemAtoms = map[string]typed.RetainedFffiHolderTyped[c.AtomsS]{}
)

func tableItem(label string) typed.RetainedFffiHolderTyped[c.AtomsS] {
	a, ok := tableItemAtoms[label]
	if !ok {
		a = c.Atoms().Text(label).Keep()
		tableItemAtoms[label] = a
	}
	return a
}

// menuItem is one item of a handle's menu; the key must be stable across
// frames, the body rendering only while the menu is open.
func (e *Editor) menuItem(key, label string, selected bool) bool {
	return c.Button(e.ids.PrepareStr(key), tableItem(label)).Frame(false).Selected(selected).SendResp().HasPrimaryClicked()
}

// ensureCellSlots sizes the per-cell widget ids and carets to the rows.
func (t *tableState) ensureCellSlots() {
	cols := t.cols()
	if len(t.ids) != len(t.rows) || (len(t.ids) > 0 && len(t.ids[0]) != cols) {
		t.ids = make([][]uint64, len(t.rows))
		t.cursors = make([][]uint64, len(t.rows))
	}
	for r := range t.rows {
		if len(t.ids[r]) != cols {
			t.ids[r] = make([]uint64, cols)
			t.cursors[r] = make([]uint64, cols)
		}
	}
}

func (e *Editor) renderTableSub() {
	t := &e.sub.tbl
	t.ensureCellSlots()
	cols := t.cols()
	rowH := markdown.TableRowHeight(false) + tableCellPad
	runes := t.widestCells()

	c.NewTableColumn().Exact(tableHandleWidth).Resizable(false).Send()
	for i := 0; i < cols; i++ {
		col := c.NewTableColumn()
		if i < cols-1 {
			col = col.Initial(markdown.TableColumnInitialWidth(runes[i], cols) + tableCellPad)
		} else {
			col = col.Remainder()
		}
		col.AtLeast(tableHandleWidth * 2).Resizable(true).ClipContents(true).Send()
	}
	for tbl := range c.NewTable(e.ids.PrepareStr("grid")).Striped(true).HeaderHeight(rowH).
		AutoShrink(false, true).Vscroll(false).ApplyWidths(t.widthEpoch).Body() {
		for hdr := range tbl.Header() {
			for range hdr.Col() {
				e.renderTableMenu()
			}
			for col := 0; col < cols; col++ {
				for range hdr.Col() {
					for range c.IdScope(e.ids.PrepareStr("col")) {
						for range c.IdScope(e.ids.PrepareSeq(uint64(col))) {
							e.renderColumnMenu(col)
						}
					}
				}
			}
		}
		for r := range t.rows {
			for row := range tbl.Row(rowH) {
				for range c.IdScope(e.ids.PrepareSeq(uint64(r))) {
					for range row.Col() {
						e.renderRowMenu(r)
					}
					for col := 0; col < cols; col++ {
						for range row.Col() {
							e.renderCell(r, col)
						}
					}
				}
			}
		}
	}
	t.focus = false
}

// renderCell is one cell's field, bound to its string, its caret reported.
func (e *Editor) renderCell(r, col int) {
	t := &e.sub.tbl
	text := t.rows[r][col]
	mask := tableKeyMask
	lo, hi := c.UnpackCursorRange(t.cursors[r][col])
	if lo == hi && lo == 0 {
		mask |= keycodes.MaskOf(keycodes.ArrowLeft)
	}
	if lo == hi && int(lo) >= utf8.RuneCountInString(text) {
		mask |= keycodes.MaskOf(keycodes.ArrowRight)
	}
	te := c.TextEdit(e.ids.PrepareSeq(uint64(col)), text, false).
		DesiredWidth(1e6).
		ReportCursor().
		CaptureKeys(uint64(mask)).
		CaptureTab().
		CaptureKeyChords(uint64(tableChordMask), uint8(keycodes.ModCommand))
	if t.insert.ok && t.insert.r == r && t.insert.col == col {
		te = te.InsertAtCursor(t.insert.text)
		t.insert = cellInsert{}
	}
	if t.focus && t.focusR == r && t.focusC == col {
		ch := t.focusChar
		if n := utf8.RuneCountInString(text); ch < 0 || ch > n {
			ch = n
		}
		te = te.SetCursor(c.PackCursorRange(ch, ch), true)
		t.cursors[r][col] = c.PackCursorRange(ch, ch)
	}
	t.ids[r][col] = te.Id()
	te.SendRespValCursor(&t.rows[r][col], &t.cursors[r][col])
	if e.sub.rebind {
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&t.rows[r][col])
	}
}

func (e *Editor) renderTableMenu() {
	t := &e.sub.tbl
	for range c.MenuButton(atomsTableMenu).KeepIter() {
		if e.menuItem("add-row", "Add row", false) {
			t.op = func() { t.insertRow(len(t.rows)); t.setFocus(len(t.rows)-1, 0, -1) }
		}
		if e.menuItem("add-col", "Add column", false) {
			t.op = func() { t.insertColumn(t.cols()) }
		}
		if e.menuItem("copy-table", "Copy table as TSV", false) {
			t.op = func() { e.copyRequest = tsv(t.rows) }
		}
		if e.menuItem("reset-widths", "Reset column widths", false) {
			t.op = func() { t.widthEpoch++ }
		}
	}
}

func (e *Editor) renderColumnMenu(col int) {
	t := &e.sub.tbl
	for range c.MenuButton(atomsColMenu).KeepIter() {
		if e.menuItem("ins-left", "Insert column left", false) {
			t.op = func() { t.insertColumn(col); t.setFocus(0, col, -1) }
		}
		if e.menuItem("ins-right", "Insert column right", false) {
			t.op = func() { t.insertColumn(col + 1); t.setFocus(0, col+1, -1) }
		}
		if col > 0 && e.menuItem("move-left", "Move column left", false) {
			t.op = func() { t.moveColumn(col, -1) }
		}
		if col < t.cols()-1 && e.menuItem("move-right", "Move column right", false) {
			t.op = func() { t.moveColumn(col, +1) }
		}
		c.Separator().Send()
		cur := alignOf(t.align[col])
		for _, a := range [...]struct {
			key, label string
			al         alignE
		}{{"al-left", "Align left", alignLeft}, {"al-center", "Align center", alignCenter}, {"al-right", "Align right", alignRight}, {"al-none", "Align default", alignNone}} {
			if e.menuItem(a.key, a.label, cur == a.al) {
				t.op = func() { t.setAlign(col, a.al) }
			}
		}
		if len(t.rows) > 2 {
			c.Separator().Send()
			if e.menuItem("sort-asc", "Sort ascending", false) {
				t.op = func() { t.sortBody(col, false) }
			}
			if e.menuItem("sort-desc", "Sort descending", false) {
				t.op = func() { t.sortBody(col, true) }
			}
		}
		if t.cols() > 1 {
			c.Separator().Send()
			if e.menuItem("del-col", "Delete column", false) {
				t.op = func() {
					t.deleteColumn(col)
					t.setFocus(0, min(col, t.cols()-1), -1)
				}
			}
		}
	}
}

func (e *Editor) renderRowMenu(r int) {
	t := &e.sub.tbl
	for range c.MenuButton(atomsRowMenu).KeepIter() {
		if r > 0 && e.menuItem("ins-above", "Insert row above", false) {
			t.op = func() { t.insertRow(r); t.setFocus(r, 0, -1) }
		}
		if e.menuItem("ins-below", "Insert row below", false) {
			t.op = func() { t.insertRow(r + 1); t.setFocus(r+1, 0, -1) }
		}
		if r > 1 && e.menuItem("move-up", "Move row up", false) {
			t.op = func() { t.moveRow(r, -1) }
		}
		if r > 0 && r < len(t.rows)-1 && e.menuItem("move-down", "Move row down", false) {
			t.op = func() { t.moveRow(r, +1) }
		}
		if e.menuItem("copy-row", "Copy row as TSV", false) {
			t.op = func() { e.copyRequest = tsv(t.rows[r : r+1]) }
		}
		if len(t.rows) > 1 {
			c.Separator().Send()
			if e.menuItem("del-row", "Delete row", false) {
				t.op = func() {
					t.deleteRow(r)
					t.setFocus(min(r, len(t.rows)-1), 0, -1)
				}
			}
		}
	}
}
