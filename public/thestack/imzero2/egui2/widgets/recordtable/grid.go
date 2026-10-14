package recordtable

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
)

const (
	// DefaultColumnWidth seeds a column that names no Width: what it shows
	// before the fit frame measures it, and what a column the fit never
	// reaches keeps.
	DefaultColumnWidth float32 = 120
	// DefaultRowHeight is the row height when Input.RowHeight is zero.
	DefaultRowHeight float32 = 18
	// RowNumColWidth is the fixed width of the leading "#" column. It is not
	// resizable, so it never acquires an override; it takes part in the
	// resolver's column list only to keep indices aligned with the EtColumn
	// sequence and the positional width read-back.
	RowNumColWidth float32 = 44
	// FitMaxRunes caps what a fit-frame cell measures ([FitRunes]), so a long
	// value never sizes its column to a paragraph.
	FitMaxRunes = 50
	// fitMaxRows bounds what the fit frame emits: the visible-range gate is
	// off for it.
	fitMaxRows = 200
	// defaultScopeKey is the scope an empty Input.ScopeKey falls back to.
	defaultScopeKey = "recordtable"
)

// Column is one grid column: which record column it shows and how.
type Column struct {
	// Arrow is the record column the cells come from. A negative index is a
	// column the host computes (Cell is then required); it is never sortable.
	Arrow int
	// Header is the caption; empty means the Arrow field name.
	Header string
	// Sub are small, weak lines under the caption — a type tag, a unit.
	Sub []string
	// Hover is the caption's hover text; empty means "name — type".
	Hover string
	// Width seeds the column before the fit frame and without an override;
	// zero means [DefaultColumnWidth].
	Width float32
	// WidthType discriminates the column's stored width (ADR-0151 §SD1): its
	// identity is the Arrow field name (or Header for a computed column) and
	// this, with Input.WidthView appended. Empty means the Arrow type's
	// String(), or "computed".
	WidthType string
	// Cell renders one record row's cell; nil means the un-glossed value
	// (gloss.FormatArrowElem), left-aligned when [StringLike]. Cell sets
	// Text and the look; the grid owns Selected and Untruncated.
	Cell func(row int64) Cell
	// NoSort keeps the header a label: a click does not sort by it.
	NoSort bool
}

// Input is one frame of a grid over an Arrow record.
type Input struct {
	Ids *c.WidgetIdStack
	// ScopeKey tells two grids under one host apart; empty means a package
	// default.
	ScopeKey string
	// Record holds the rows; nil draws the headers over no rows.
	Record arrow.RecordBatch
	// Columns are drawn in order after the "#" column.
	Columns []Column
	// Widths, when set, resolves each column's width from the user's stored
	// overrides and captures their drags (ADR-0151). The host acquires it from
	// the frame context's colwidth.HostI, with Opts.MinPoints = DragMinWidth
	// and Opts.MaxPoints = MaxColumnWidth, and may share one across grids.
	Widths *colwidth.Resolver
	// TableTag is the instance-tier scope of the stored widths; empty means
	// ScopeKey, then the package default. Keep it stable: changing it orphans
	// every width stored under the old one.
	TableTag string
	// WidthView is appended to every column's width type, the "#" column's
	// included. A host that renders one column two ways gives each grid its
	// own (play: ";view=row" and ";view=attr"), so the column tier, which
	// crosses tables, does not carry a width fitted to one into the other.
	WidthView string
	// Sort marks the sorted column's header; nil leaves every header a label.
	// The grid does not apply a click to it — see Result.SortClicked.
	Sort *SortState
	// Order is the display→record permutation, nil for record order —
	// typically Sort.OrderFor(Record, Record.NumRows()), or a filter's rows.
	Order []int64
	// PageStart and PageEnd window the display positions drawn, end
	// exclusive; a zero PageEnd draws to the end.
	PageStart int64
	PageEnd   int64
	// Selected is the selected record row, -1 for none. It is highlighted
	// where it is drawn, so it survives a sort.
	Selected int64
	// Refit asks egui_table to fit the columns to their content this frame,
	// on top of the fit the grid asks for itself when its column set changes.
	// Play asks on every new result, so the widths belong to the result on
	// screen; a grid over a polled table should not, or the columns move
	// under the reader on every poll.
	Refit bool
	// CellsKey, when non-nil, lets the grid replay the previous frame's cell
	// bytes instead of rebuilding them: on a frame whose CellsKey equals the
	// last one, with the same window, selection, sort and columns, and no
	// cell clicked, hovered or focused, the capture is sent again. It must
	// name everything the grid cannot see — the record, the order, whatever
	// a Column.Cell reads — and be comparable with ==. A key that misses an
	// input shows stale cells; nil, the default, rebuilds every frame.
	CellsKey any
	// RowHeight is zero for [DefaultRowHeight].
	RowHeight float32
	// State is required and kept at one address across frames.
	State *State
}

// State is what a grid keeps between frames. The zero value is ready.
type State struct {
	// sig is the column identity the widths were last fitted to; a change
	// asks egui_table to fit again, once.
	sig string
	// widthsSeen is set after the first width report: that report is the
	// crate's first-show fit, not a drag.
	widthsSeen bool
	// applyGen moves the epoch the binding applies widths under, so the fit
	// frame re-applies stored overrides egui_table's first show replaced.
	applyGen uint32
	// cells is the replay cache Input.CellsKey turns on.
	cells cellsCache
}

// Result is what one frame of the grid produced. Its readbacks — the clicks
// and the widths — describe the previous frame's response, one frame late.
type Result struct {
	// Clicked is the record row whose cell was primary-clicked, -1 for none.
	Clicked int64
	// SortClicked is the Arrow column whose header was clicked, -1 for none.
	// The host applies it — Sort.Clicked(SortClicked) — or routes it first
	// (play sends it through set_table_options as the person's gesture).
	SortClicked int
	// Err is set when the input is unusable; the grid then draws a short
	// message in its place.
	Err error
	// WidthsErr is a failed write of captured widths; the resolver keeps
	// them and the next frame retries.
	WidthsErr error
}

// RenderGrid draws in.Record as an egui_table grid: a "#" column of record
// row numbers, then in.Columns with their resolved widths, sortable headers
// and inset, full-cell click targets ([Chrome]). Widths come from the user's
// stored overrides, else egui_table's fit to the content on the first frame
// with rows, on any frame the column set changes, and on any frame the host
// asks (Input.Refit); a drag is captured back to the resolver.
func RenderGrid(in Input) (res Result) {
	res.Clicked, res.SortClicked = -1, -1
	scopeKey := in.ScopeKey
	if scopeKey == "" {
		scopeKey = defaultScopeKey
	}
	for range c.IdScope(in.Ids.PrepareStr(scopeKey)) {
		res.Err = in.validate()
		if res.Err != nil {
			for rt := range c.RichTextLabel("recordtable: " + res.Err.Error()) {
				rt.Weak()
			}
			return
		}
		in.render(&res, scopeKey)
	}
	return
}

func (in *Input) validate() (err error) {
	if in.State == nil {
		return errors.New("no State")
	}
	if in.CellsKey != nil && !reflect.TypeOf(in.CellsKey).Comparable() {
		return errors.New("CellsKey of type " + reflect.TypeOf(in.CellsKey).String() + " is not comparable")
	}
	ncols := 0
	if in.Record != nil {
		ncols = int(in.Record.NumCols())
	}
	for i := range in.Columns {
		col := &in.Columns[i]
		switch {
		case col.Arrow < 0 && col.Cell == nil:
			return errors.New("column " + strconv.Itoa(i) + " is computed but has no Cell")
		case in.Record != nil && col.Arrow >= ncols:
			return errors.New("column " + strconv.Itoa(i) + " names Arrow column " + strconv.Itoa(col.Arrow) + " of " + strconv.Itoa(ncols))
		}
	}
	return
}

// displayRows is the first display position drawn and how many follow.
func (in *Input) displayRows() (start int64, n int64) {
	if in.Record == nil {
		return
	}
	total := in.Record.NumRows()
	if in.Order != nil {
		total = int64(len(in.Order))
	}
	end := total
	if in.PageEnd > 0 {
		end = min(in.PageEnd, total)
	}
	start = min(max(in.PageStart, 0), end)
	n = end - start
	return
}

// recordRow maps a display position to the record row drawn there, -1 when
// Order names a row the record does not have.
func (in *Input) recordRow(pos int64) (row int64) {
	row = pos
	if in.Order != nil {
		row = in.Order[pos]
	}
	if row < 0 || row >= in.Record.NumRows() {
		return -1
	}
	return
}

// WidthColumns names the grid's columns for the resolver, index-aligned with
// the EtColumn sequence: the "#" column first, then in.Columns. A host passes
// it to colwidth.Resolver.Clear or ClearAll for a reset gesture.
func (in *Input) WidthColumns() (cols []colwidth.Column) {
	var schema *arrow.Schema
	if in.Record != nil {
		schema = in.Record.Schema()
	}
	return WidthColumns(schema, in.Columns, in.WidthView)
}

// WidthColumns names columns for the resolver as [RenderGrid] does, from the
// schema alone: the "#" column, then each column's Arrow field name (a
// computed column's Header) and width type, view appended to every type.
// Identity is the raw field name rather than the caption, which a host may
// derive and change; the type takes part so a column that changes type drops
// its stored width.
func WidthColumns(schema *arrow.Schema, columns []Column, view string) (cols []colwidth.Column) {
	cols = make([]colwidth.Column, 0, len(columns)+1)
	cols = append(cols, colwidth.Column{Name: "#", Type: "rownum" + view})
	for i := range columns {
		col := &columns[i]
		name, typ := col.Header, col.WidthType
		if col.Arrow >= 0 && schema != nil && col.Arrow < schema.NumFields() {
			f := schema.Field(col.Arrow)
			name = f.Name
			if typ == "" {
				typ = f.Type.String()
			}
		}
		if typ == "" {
			typ = "computed"
		}
		cols = append(cols, colwidth.Column{Name: name, Type: typ + view})
	}
	return
}

func widthSignature(cols []colwidth.Column) (sig string) {
	var sb strings.Builder
	for _, col := range cols {
		sb.WriteString(col.Name)
		sb.WriteByte(0)
		sb.WriteString(col.Type)
		sb.WriteByte(1)
	}
	return sb.String()
}

func (in *Input) field(col *Column) (f arrow.Field, ok bool) {
	if col.Arrow < 0 || in.Record == nil {
		return
	}
	return in.Record.Schema().Field(col.Arrow), true
}

func (in *Input) render(res *Result, scopeKey string) {
	ids, st := in.Ids, in.State
	density := styletokens.ActiveDensity()
	pad := CellPadX(density)
	floor := DragMinWidth(density)
	start, n := in.displayRows()
	tag := in.TableTag
	if tag == "" {
		tag = scopeKey
	}

	// A width the user set outranks the seed (ADR-0151); the seed is the
	// default for columns they have not touched.
	cols := in.WidthColumns()
	seeds := make([]float64, 0, len(cols))
	seeds = append(seeds, float64(RowNumColWidth))
	for i := range in.Columns {
		w := in.Columns[i].Width
		if w <= 0 {
			w = DefaultColumnWidth
		}
		seeds = append(seeds, float64(w))
	}
	resolved := seeds
	if in.Widths != nil {
		// Font size 0: the grid has no single text size to attribute a width
		// to; zero disables rescaling, the documented meaning.
		resolved = in.Widths.Resolve(tag, cols, 0, append([]float64(nil), seeds...))
	}

	// egui_table keeps its own width per column *position* and fits to
	// content only on a table's first show — so without a fit of our own a
	// changed column set inherits the old widths by position. The fit waits
	// for rows unless the host asks: fitted on an empty grid, a column would
	// be as wide as its header and no more. It is one frame: fitting
	// continuously would move the columns while the user reads.
	refit := in.Refit
	if sig := widthSignature(cols); sig != st.sig && n > 0 {
		st.sig, refit = sig, true
	}
	if refit {
		if in.Widths != nil {
			// The read-back lags, so the fit's result lands a report after
			// this frame; without the settle window it reads as a drag and
			// every fitted column acquires an override nobody asked for.
			in.Widths.MarkReseed(tag)
		}
		// The binding applies Go's widths only when the epoch moves, and the
		// stored widths were applied on an earlier frame whose first-show
		// fit replaced them; a generation of the grid's own re-applies them.
		st.applyGen++
	}

	c.EtColumn(float32(resolved[0])).Resizable(false).Send()
	for i := range in.Columns {
		col := c.EtColumn(float32(resolved[i+1])).
			Resizable(true).
			RangeMinMax(floor, MaxColumnWidth)
		// A column carrying an override sits the fit out, or every stored
		// drag would be measured away.
		if refit && resolved[i+1] == seeds[i+1] {
			col = col.AutoSizeThisFrame(true)
		}
		col.Send()
	}

	rowHeight := in.RowHeight
	if rowHeight <= 0 {
		rowHeight = DefaultRowHeight
	}
	et := c.EndETable(ids.PrepareStr("table"), uint64(n), rowHeight, 1, 1).
		Striped(true).
		FillPane(true)
	if in.Widths != nil {
		et = et.ApplyWidths(in.Widths.Epoch(tag) + st.applyGen<<16)
	}
	if in.Selected >= 0 {
		for local := range n {
			if in.recordRow(start+local) == in.Selected {
				et = et.SelectedRow(uint64(local))
				break
			}
		}
	}

	in.renderHeaders(res, et, pad, refit)

	// Only the rows and columns egui_table will draw — except on the fit
	// frame, which measures cells and needs them present: the range read
	// back there is the previous frame's, which may have had no rows.
	rowLo, rowHi := int64(0), n
	if rb, re, _, _, _, ok := et.VisibleRange(); ok && !refit {
		rowLo, rowHi = min(int64(rb), n), min(int64(re), n)
	}
	if refit {
		rowHi = min(rowHi, fitMaxRows)
	}

	// Nothing on a page changes between most frames, so with a CellsKey the
	// cells are captured once and replayed until one of their inputs moves
	// or a cell has a response the live path would read. The host sees the
	// same bytes either way.
	chrome := Chrome{Ids: ids}
	if in.CellsKey != nil {
		key := in.cellsKey(cols, et, start, n, rowLo, rowHi, refit, pad)
		if raw := st.cells.retained(key, c.CurrentApplicationState.StateManager); raw != nil {
			et.SendWithRawCells(raw)
			in.captureWidths(res, et, cols, tag)
			return
		}
		st.cells.beginCapture(key)
		chrome.Note = st.cells.note
	} else {
		st.cells.drop()
	}
	in.renderCells(res, et, &chrome, pad, start, rowLo, rowHi, refit)
	if in.CellsKey != nil {
		st.cells.endCapture(et.CellsBytes())
	}
	et.Send()
	in.captureWidths(res, et, cols, tag)
}

func (in *Input) cellsKey(cols []colwidth.Column, et c.EndETableFluid, start, n, rowLo, rowHi int64, refit bool, pad float32) (key cellsKey) {
	key = cellsKey{
		content: in.CellsKey, pageStart: start, pageN: n, rowLo: rowLo, rowHi: rowHi,
		selected: in.Selected, refit: refit, pad: pad,
		columns: foldColumns(cols, func(pos int) int {
			if pos == 0 {
				return -1
			}
			return in.Columns[pos-1].Arrow
		}, func(pos uint32) bool {
			vis, _ := et.ColVisible(pos)
			return vis
		}),
	}
	if in.Sort != nil {
		key.sortActive, key.sortCol, key.sortDesc = in.Sort.Active, in.Sort.Col, in.Sort.Desc
	}
	return
}

// renderCells emits the cells of display rows [rowLo, rowHi) of the window.
// Each cell is scoped by its record row and keyed by its column position, so
// a cell keeps its id across a sort and a page turn.
func (in *Input) renderCells(res *Result, et c.EndETableFluid, chrome *Chrome, pad float32, start, rowLo, rowHi int64, refit bool) {
	ids := in.Ids
	for local := rowLo; local < rowHi; local++ {
		row := in.recordRow(start + local)
		if row < 0 {
			continue
		}
		selected := row == in.Selected
		if vis, _ := et.ColVisible(0); vis {
			for range et.Cells(uint64(local), 0) {
				for range c.IdScope(ids.PrepareSeq(uint64(row))) {
					if chrome.SelectableCell(0, pad, Cell{Text: strconv.FormatInt(row+1, 10), Selected: selected, Untruncated: refit}) {
						res.Clicked = row
					}
				}
			}
		}
		for i := range in.Columns {
			pos := uint32(i + 1)
			if vis, _ := et.ColVisible(pos); !vis {
				continue
			}
			cell := in.cell(&in.Columns[i], row)
			cell.Selected, cell.Untruncated = selected, refit
			if refit {
				cell.Text = FitRunes(cell.Text, FitMaxRunes)
			}
			for range et.Cells(uint64(local), pos) {
				for range c.IdScope(ids.PrepareSeq(uint64(row))) {
					if chrome.SelectableCell(uint64(pos), pad, cell) {
						res.Clicked = row
					}
				}
			}
		}
	}
}

func (in *Input) cell(col *Column, row int64) (cell Cell) {
	if col.Cell != nil {
		return col.Cell(row)
	}
	arr := in.Record.Column(col.Arrow)
	return Cell{Text: gloss.FormatArrowElem(arr, row), LeftAlign: StringLike(arr.DataType())}
}

// renderHeaders draws the "#" gutter and one header per column: a frameless
// button that reports a sort click, or a label for a column that does not
// sort. The physical name and the type stay on hover, so a caption never
// hides the name it stands for.
func (in *Input) renderHeaders(res *Result, et c.EndETableFluid, pad float32, refit bool) {
	ids := in.Ids
	chrome := Chrome{Ids: ids}
	for range c.IdScope(ids.PrepareStr("header")) {
		if vis, _ := et.ColVisible(0); vis {
			for range et.Headers(0, 0) {
				chrome.HeaderCell(0, pad, func() {
					for rt := range c.RichTextLabel("#") {
						rt.Weak().Monospace()
					}
				})
			}
		}
		for i := range in.Columns {
			col := &in.Columns[i]
			pos := uint32(i + 1)
			if vis, _ := et.ColVisible(pos); !vis {
				continue
			}
			f, hasField := in.field(col)
			name, hover := col.Header, col.Hover
			if hasField {
				if name == "" {
					name = f.Name
				}
				if hover == "" {
					hover = f.Name + " — " + f.Type.String()
				}
			}
			sortable := in.Sort != nil && hasField && !col.NoSort
			for range et.Headers(0, pos) {
				chrome.HeaderCell(uint64(pos), pad, func() {
					if sortable {
						for range c.HoverText(hover + " — click to sort").KeepIter() {
							btn := c.Button(ids.PrepareSeq(uint64(pos)),
								c.Atoms().BeginRichText(name+in.Sort.Glyph(col.Arrow)).Strong().Monospace().End().Keep()).
								Frame(false)
							if !refit {
								btn = btn.Truncate()
							}
							if btn.SendResp().HasPrimaryClicked() {
								res.SortClicked = col.Arrow
							}
						}
					} else if hover != "" {
						for range c.HoverText(hover).KeepIter() {
							for rt := range c.RichTextLabel(name) {
								rt.Strong().Monospace()
							}
						}
					} else {
						for rt := range c.RichTextLabel(name) {
							rt.Strong().Monospace()
						}
					}
					for _, sub := range col.Sub {
						for rt := range c.RichTextLabel(sub) {
							rt.Small().Weak().Monospace()
						}
					}
				})
			}
		}
	}
}

// captureWidths feeds the widths egui_table settled on back into the resolver
// and lets its debounce write them. The first report is the crate's
// first-show fit, not the user's choice; passing firstShow on it stops the
// seed being frozen as an override.
func (in *Input) captureWidths(res *Result, et c.EndETableFluid, cols []colwidth.Column, tag string) {
	if in.Widths == nil {
		return
	}
	now := time.Now()
	if fetched, ok := et.ColumnWidths(); ok {
		widths := make([]float64, len(fetched))
		for i, w := range fetched {
			widths[i] = float64(w)
		}
		firstShow := !in.State.widthsSeen
		in.State.widthsSeen = true
		in.Widths.Observe(tag, cols, widths, 0, firstShow, now)
	}
	_, res.WidthsErr = in.Widths.Flush(now)
}
