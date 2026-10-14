package tally

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// stringTable is a small result table: fixed headers, rows of cells as text,
// an optional tone per row, and a click report. It is what the Diff, History,
// Find, Du and Problems tabs render their lanes into — the rows are already
// strings (gloss.FormatArrowElem did the work) — drawn by
// recordtable.RenderGrid over a record of those strings. The headers do not
// sort: the cells are formatted text, and a size or a count compared as text
// orders "10" before "9".
type stringTable struct {
	scopeKey string
	headers  []string
	widths   []float32
	rows     [][]string
	// tone colours the first cell's text per row; nil or color.Transparent
	// leaves it as it is.
	tone     []color.Color
	selected int
	// maxHeight bounds the table (0 = let the pane decide); key remembers
	// which result the selection belongs to, so a new result drops it.
	maxHeight float32
	key       string
	// res persists the column widths (ADR-0151) under scopeKey as the table
	// tag, the headers as column names and the table's own view as the
	// type; nil keeps the widths as given.
	res *colwidth.Resolver
	// grid is the grid's own state; rec the record of rows it draws, rebuilt
	// when the rows or the headers change.
	grid       recordtable.State
	rec        arrow.RecordBatch
	recRows    *[]string
	recN       int
	recHeaders string
}

// widthView is the table's view tag: the scope, so a "path" column in the
// Diff table and one in the Find table can hold different widths.
func (t *stringTable) widthView() string {
	return ";view=" + t.scopeKey
}

// columns lays the headers out for the grid: every column text, left
// aligned, seeded from widths, the first toned per row.
func (t *stringTable) columns() (cols []recordtable.Column) {
	cols = make([]recordtable.Column, len(t.headers))
	for i, h := range t.headers {
		w := tableDefaultWidth
		if i < len(t.widths) && t.widths[i] > 0 {
			w = t.widths[i]
		}
		ci := i
		cols[i] = recordtable.Column{
			Arrow: i, Header: h, Hover: h, Width: w, WidthType: "text", NoSort: true,
			Cell: func(row int64) (cell recordtable.Cell) {
				cell = recordtable.Cell{LeftAlign: true}
				if r := t.rows[row]; ci < len(r) {
					cell.Text = r[ci]
				}
				if ci == 0 && int(row) < len(t.tone) && t.tone[row] != color.Transparent {
					cell.Tone, cell.Toned = t.tone[row], true
				}
				return
			},
		}
	}
	return
}

// widthColumns keys the table's columns for the resolver: the header is the
// name, "text" plus the table's view the type.
func (t *stringTable) widthColumns() []colwidth.Column {
	fields := make([]arrow.Field, len(t.headers))
	for i, h := range t.headers {
		fields[i] = arrow.Field{Name: h, Type: arrow.BinaryTypes.String}
	}
	return recordtable.WidthColumns(arrow.NewSchema(fields, nil), t.columns(), t.widthView(), false)
}

// resetFor drops the selection when the table shows a new result.
func (t *stringTable) resetFor(key string) {
	if t.key != key {
		t.key, t.selected = key, -1
	}
}

// record returns the record of t.rows, rebuilding it when the rows or the
// headers changed since the last frame.
func (t *stringTable) record() (rec arrow.RecordBatch) {
	var first *[]string
	if len(t.rows) > 0 {
		first = &t.rows[0]
	}
	headers := strings.Join(t.headers, "\x00")
	if t.rec != nil && t.recRows == first && t.recN == len(t.rows) && t.recHeaders == headers {
		return t.rec
	}
	if t.rec != nil {
		t.rec.Release()
	}
	t.rec = recordtable.StringRecord(t.headers, t.rows)
	t.recRows, t.recN, t.recHeaders = first, len(t.rows), headers
	return t.rec
}

const (
	tableRowHeight    float32 = 22
	tableDefaultWidth float32 = 140
)

// render draws the table and returns the clicked row, -1 for none; the
// clicked row becomes the selection.
func (t *stringTable) render(ids *c.WidgetIdStack) (clicked int) {
	clicked = -1
	if len(t.headers) == 0 {
		return
	}
	res := recordtable.RenderGrid(recordtable.Input{
		Ids: ids, ScopeKey: t.scopeKey, Record: t.record(), Columns: t.columns(),
		Widths: t.res, TableTag: t.scopeKey, WidthView: t.widthView(),
		Selected: int64(t.selected), HasSelection: t.selected >= 0,
		RowHeight: tableRowHeight, NoRowNumbers: true,
		// A caller-supplied ceiling: the table takes what its rows need and
		// stops there. Du sets one so the listing beside the treemap cannot
		// outgrow it; without one the table takes the rest of its tab.
		MaxHeight: t.maxHeight,
		State:     &t.grid,
	})
	if res.Clicked >= 0 {
		clicked = int(res.Clicked)
		t.selected = clicked
	}
	return
}
