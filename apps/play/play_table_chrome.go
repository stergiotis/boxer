package play

import (
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// The Table pane's sort and cell chrome live in widgets/recordtable, which
// other apps' grids share; play keeps its own names for them.

// tableSortState is the Table pane's header-click sort.
type tableSortState = recordtable.SortState

// chrome is the cell layer both Table grids draw through. Its Note hands every
// cell button's wire id to the master table's cells cache, which gates a
// replay on their responses.
func (inst *PlayApp) chrome() *recordtable.Chrome {
	if inst.tableChrome.Ids != inst.ids {
		inst.tableChrome = recordtable.Chrome{Ids: inst.ids, Note: inst.masterCells.note}
	}
	return &inst.tableChrome
}

// headerCell insets a header cell the way body cells are inset
// (recordtable.Chrome.HeaderCell).
func (inst *PlayApp) headerCell(id uint64, cellPadX float32, body func()) {
	inst.chrome().HeaderCell(id, cellPadX, body)
}

// colDragMinWidth is the narrowest a resizable column may get at play's
// density (recordtable.DragMinWidth): handed both to egui_table and to the
// width resolver, so the drag floor and the stored floor agree.
func (inst *PlayApp) colDragMinWidth() float32 {
	return recordtable.DragMinWidth(inst.density)
}

// selectableCell renders one grid cell as a full-cell selectable button
// (recordtable.Chrome.SelectableCell) and returns true on a primary click.
//
// selBg paints the accent band behind a selected cell. The per-DB-row grid
// leaves it off: its whole-row highlight comes from et.SelectedRow. The
// per-attribute grid turns it on: there one source DB row explodes to several
// egui_table rows, which a single SelectedRow index cannot cover, so each
// selected cell paints its own band — together highlighting the whole entity.
//
// tone colours the text per ADR-0186's inline face; gloss.ToneNeutral leaves
// it alone. link makes the cell a hyperlink (a gloss/url cell). truncate is
// false only on the frame a grid re-fits its columns.
func (inst *PlayApp) selectableCell(id uint64, cellPadX float32, text string, weak bool, selected, selBg, leftAlign bool, tone gloss.ToneE, link string, truncate bool) (clicked bool) {
	cell := recordtable.Cell{
		Text: text, Weak: weak, Selected: selected, SelectedFill: selBg,
		LeftAlign: leftAlign, Link: link, Untruncated: !truncate,
	}
	cell.Tone, cell.Toned = toneColor(tone)
	return inst.chrome().SelectableCell(id, cellPadX, cell)
}
