package recordtable

import (
	"errors"
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
	// this. Empty means the Arrow type's String(), or "computed". A host
	// rendering one column two ways appends a view tag, as play does with
	// ";view=row".
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
	// Sort marks the sorted column's header; nil leaves every header a label.
	// The grid does not apply a click to it — see Result.SortClicked.
	Sort *SortState
	// Order is the display→record permutation, nil for record order —
	// typically Sort.OrderFor(Record, Record.NumRows()), or a filter's rows.
	Order []int64
	// PageStart and PageLen window the display positions drawn; a zero
	// PageLen draws to the end.
	PageStart int64
	PageLen   int64
	// Selected is the selected record row, -1 for none. It is highlighted
	// where it is drawn, so it survives a sort.
	Selected int64
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
// with rows and on any frame the column set changes; a drag is captured back
// to the resolver.
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

// displayRows is how many rows the grid draws, and the first display position.
func (in *Input) displayRows() (start int64, n int64) {
	if in.Record == nil {
		return
	}
	total := in.Record.NumRows()
	if in.Order != nil {
		total = int64(len(in.Order))
	}
	start = min(max(in.PageStart, 0), total)
	n = total - start
	if in.PageLen > 0 {
		n = min(n, in.PageLen)
	}
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

func (in *Input) field(col *Column) (f arrow.Field, ok bool) {
	if col.Arrow < 0 || in.Record == nil {
		return
	}
	return in.Record.Schema().Field(col.Arrow), true
}

// widthColumns names the columns for the resolver, index-aligned with the
// EtColumn sequence: the "#" column first, then in.Columns.
func (in *Input) widthColumns() (cols []colwidth.Column) {
	cols = make([]colwidth.Column, 0, len(in.Columns)+1)
	cols = append(cols, colwidth.Column{Name: "#", Type: "rownum"})
	for i := range in.Columns {
		col := &in.Columns[i]
		name, typ := col.Header, col.WidthType
		if f, ok := in.field(col); ok {
			name = f.Name
			if typ == "" {
				typ = f.Type.String()
			}
		}
		if typ == "" {
			typ = "computed"
		}
		cols = append(cols, colwidth.Column{Name: name, Type: typ})
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

func (in *Input) render(res *Result, scopeKey string) {
	ids, st := in.Ids, in.State
	density := styletokens.ActiveDensity()
	pad := CellPadX(density)
	floor := DragMinWidth(density)
	chrome := Chrome{Ids: ids}
	start, n := in.displayRows()
	tag := in.TableTag
	if tag == "" {
		tag = scopeKey
	}

	// A width the user set outranks the seed (ADR-0151); the seed is the
	// default for columns they have not touched.
	cols := in.widthColumns()
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
	// for rows: fitted on an empty grid, a column would be as wide as its
	// header and no more. It is one frame: fitting continuously would move
	// the columns while the user reads.
	refit := false
	if sig := widthSignature(cols); sig != st.sig && n > 0 {
		st.sig, refit = sig, true
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

	in.renderHeaders(res, et, &chrome, pad, refit)

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
	et.Send()
	in.captureWidths(res, et, cols, tag)
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
func (in *Input) renderHeaders(res *Result, et c.EndETableFluid, chrome *Chrome, pad float32, refit bool) {
	ids := in.Ids
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
