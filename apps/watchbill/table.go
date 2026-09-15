package watchbill

import (
	"cmp"
	"fmt"
	"sort"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// The job list follows play's Table pane (apps/play, renderMasterTable):
// a fixed row-number column, the density's tight inset on every cell,
// monospace text truncated to the column, every cell a frameless
// full-width button so a click anywhere selects the row, the selection
// painted by egui_table itself, and header buttons that cycle a sort. What
// play does beyond this — widths that persist, a re-fit when the column
// set changes — is not needed for a fixed set of nine columns.

const (
	jobRowHeight     = 18.0
	rowNumColWidth   = 44.0
	colMinWidth      = 40.0
	colMaxWidth      = 900.0
	seqCellBase      = 0x01000000
	seqCellColStride = 0x00010000
	seqHeaderBase    = 0x00800000
	seqFrameSalt     = 0x00400000
	// fitMaxRunes caps what the fit frame measures, so a long error never
	// sizes its column to a paragraph.
	fitMaxRunes = 48
	// fitMaxRows bounds what the fit frame emits, since the range gate is
	// off for it.
	fitMaxRows = 200
)

// sortDirE is a column's sort: none, ascending, descending — the cycle a
// header click walks, as play's does.
type sortDirE uint8

const (
	sortNone sortDirE = iota
	sortAsc
	sortDesc
)

// tableSort is which column, which way.
type tableSort struct {
	col int
	dir sortDirE
}

func (inst *tableSort) clicked(col int) {
	if inst.col != col {
		inst.col, inst.dir = col, sortAsc
		return
	}
	inst.dir = (inst.dir + 1) % 3
}

func (inst tableSort) glyph(col int) (s string) {
	if inst.col != col {
		return ""
	}
	switch inst.dir {
	case sortAsc:
		return " ▲"
	case sortDesc:
		return " ▼"
	default:
		return ""
	}
}

// jobColumn is one column: its header, width, alignment, the cell text,
// and the key the sort compares — a string or a number, never both.
type jobColumn struct {
	header  string
	width   float32
	numeric bool
	text    func(j watchbillstore.Job) string
	strKey  func(j watchbillstore.Job) string
	numKey  func(j watchbillstore.Job) float64
	tone    func(j watchbillstore.Job) (color.Color, bool)
	hover   string
}

var jobColumns = []jobColumn{
	{header: "id", width: 120, text: func(j watchbillstore.Job) string { return j.ID }, strKey: func(j watchbillstore.Job) string { return j.ID }, hover: "the job id, the run's task id"},
	{header: "kind", width: 150, text: func(j watchbillstore.Job) string { return j.Kind }, strKey: func(j watchbillstore.Job) string { return j.Kind }, hover: "the handler's kind"},
	{header: "subject", width: 170, text: func(j watchbillstore.Job) string { return j.Subject }, strKey: func(j watchbillstore.Job) string { return j.Subject }, hover: "the row in the consumer's own table that says what to do"},
	{header: "queue", width: 80, text: func(j watchbillstore.Job) string { return j.Queue }, strKey: func(j watchbillstore.Job) string { return j.Queue }, hover: "the queue a worker drains"},
	{header: "state", width: 96, text: func(j watchbillstore.Job) string { return j.State }, strKey: func(j watchbillstore.Job) string { return j.State }, tone: stateTone, hover: "the row's state (ADR-0223 §SD2)"},
	{header: "attempt", width: 72, numeric: true, text: func(j watchbillstore.Job) string { return fmt.Sprintf("%d/%d", j.Attempt, j.MaxAttempts) }, numKey: func(j watchbillstore.Job) float64 { return float64(j.Attempt) }, hover: "attempts made of attempts allowed"},
	{header: "run after", width: 160, text: func(j watchbillstore.Job) string { return when(j.RunAfter) }, numKey: func(j watchbillstore.Job) float64 { return float64(j.RunAfter.UnixNano()) }, hover: "when the job became or becomes due"},
	{header: "worker run", width: 130, text: func(j watchbillstore.Job) string { return j.WorkerRun }, strKey: func(j watchbillstore.Job) string { return j.WorkerRun }, hover: "the run that holds or last changed the row"},
	{header: "last error", width: 300, text: func(j watchbillstore.Job) string { return firstLine(j.LastError) }, strKey: func(j watchbillstore.Job) string { return j.LastError }, hover: "the last attempt's error, first line"},
}

// stateTone colours the state cell the way its chip is toned.
func stateTone(j watchbillstore.Job) (col color.Color, ok bool) {
	var def styletokens.RGBA8
	switch j.State {
	case watchbillstore.StateQueued:
		def = styletokens.InfoDefault
	case watchbillstore.StateRunning:
		def = styletokens.AccentDefault
	case watchbillstore.StateSucceeded:
		def = styletokens.SuccessDefault
	case watchbillstore.StateFailed, watchbillstore.StateCancel:
		def = styletokens.WarningDefault
	case watchbillstore.StateDiscarded, watchbillstore.StateAbandoned:
		def = styletokens.ErrorDefault
	default:
		return
	}
	return color.Hex(def.AsHex()), true
}

// sortJobs is the view permutation the header sort asks for: a stable
// sort over a copy, the listed order when no column is active.
func sortJobs(jobs []watchbillstore.Job, s tableSort) (out []watchbillstore.Job) {
	out = append([]watchbillstore.Job(nil), jobs...)
	if s.dir == sortNone || s.col < 0 || s.col >= len(jobColumns) {
		return
	}
	col := jobColumns[s.col]
	less := func(a, b watchbillstore.Job) (n int) {
		if col.numKey != nil {
			return cmp.Compare(col.numKey(a), col.numKey(b))
		}
		return cmp.Compare(col.strKey(a), col.strKey(b))
	}
	sort.SliceStable(out, func(i, k int) bool {
		n := less(out[i], out[k])
		if s.dir == sortDesc {
			return n > 0
		}
		return n < 0
	})
	return
}

func (inst *App) renderList(jobs []watchbillstore.Job) {
	pad := styletokens.PaddingTight(inst.density)
	rows := sortJobs(jobs, inst.sort)

	// The fit waits for rows: fitted on an empty list, a column would be
	// as wide as its header and no more. The table itself is emitted from
	// the first frame, rows or not, so the panel around it takes its size
	// from the table rather than from a placeholder.
	refit := !inst.fitted && len(rows) > 0
	if refit {
		inst.fitted = true
		// The binding applies Go's widths only when the epoch it is handed
		// changes, and the resolver moves its epoch only when a resolved
		// width changes — so the stored widths were applied once, on the
		// empty first frame, where egui_table's own first-show fit overrode
		// them. The fit frame needs them applied again: a generation of the
		// window's own, added to the epoch, is what asks for that.
		inst.applyGen++
	}
	truncate := !refit

	// A width the user set outranks the default (ADR-0151); the two lists
	// stay index-aligned with the EtColumn sequence, the "#" gutter included
	// so the positional read-back lines up. A column carrying an override
	// sits the re-fit out, or every stored drag would be measured away.
	cols := jobColumnKeys()
	resolved := make([]float64, 0, len(cols))
	resolved = append(resolved, rowNumColWidth)
	for _, col := range jobColumns {
		resolved = append(resolved, float64(col.width))
	}
	if inst.widths != nil {
		resolved = inst.widths.Resolve(jobsTableTag, cols, 0, resolved)
		if refit {
			inst.widths.MarkReseed(jobsTableTag)
		}
	}

	c.EtColumn(float32(resolved[0])).Resizable(false).Send()
	for i, col := range jobColumns {
		et := c.EtColumn(float32(resolved[i+1])).Resizable(true).RangeMinMax(colMinWidth, colMaxWidth)
		if refit && resolved[i+1] == float64(col.width) {
			et = et.AutoSizeThisFrame(true)
		}
		et.Send()
	}
	et := c.EndETable(inst.ids.PrepareStr(jobsTableTag), uint64(len(rows)), jobRowHeight, 1, 1).Striped(true).FillPane(true)
	if inst.widths != nil {
		et = et.ApplyWidths(inst.widths.Epoch(jobsTableTag) + inst.applyGen<<16)
	}
	for i, j := range rows {
		if j.ID == inst.selectedID {
			et = et.SelectedRow(uint64(i))
			break
		}
	}

	// Headers: the row-number gutter, then a frameless button per column
	// that cycles its sort, with the column's meaning on hover.
	if vis, _ := et.ColVisible(0); vis {
		for range et.Headers(0, 0) {
			inst.cellInset(seqHeaderBase, pad, func() {
				for rt := range c.RichTextLabel("#") {
					rt.Weak().Monospace()
				}
			})
		}
	}
	for i, col := range jobColumns {
		pos := uint32(i + 1)
		if vis, _ := et.ColVisible(pos); !vis {
			continue
		}
		for range et.Headers(0, pos) {
			inst.cellInset(seqHeaderBase+uint64(pos), pad, func() {
				for range c.HoverText(col.hover + " — click to sort").KeepIter() {
					btn := c.Button(inst.ids.PrepareSeq(seqHeaderBase+seqCellColStride+uint64(pos)),
						c.Atoms().BeginRichText(col.header+inst.sort.glyph(i)).Strong().Monospace().End().Keep()).
						Frame(false)
					if truncate {
						btn = btn.Truncate()
					}
					if btn.SendResp().HasPrimaryClicked() {
						inst.sort.clicked(i)
					}
				}
			})
		}
	}

	// Cells: only the rows and columns egui_table will draw — except on
	// the fit frame, which measures cells and needs them present: the
	// range read back there is the previous frame's, and the previous
	// frame had no rows, so the gate would cut every cell exactly when the
	// widths are decided.
	rowLo, rowHi := 0, len(rows)
	if rb, re, _, _, _, ok := et.VisibleRange(); ok && !refit {
		rowLo, rowHi = min(int(rb), rowHi), min(int(re), rowHi)
	}
	if refit {
		rowHi = min(rowHi, fitMaxRows)
	}
	clicked := ""
	for row := rowLo; row < rowHi; row++ {
		j := rows[row]
		selected := j.ID == inst.selectedID
		rowBase := uint64(seqCellBase) + uint64(row)*seqCellColStride
		if vis, _ := et.ColVisible(0); vis {
			for range et.Cells(uint64(row), 0) {
				if inst.selectableCell(rowBase, pad, fmt.Sprintf("%d", row+1), true, selected, false, color.Transparent, false, truncate) {
					clicked = j.ID
				}
			}
		}
		for i, col := range jobColumns {
			pos := uint32(i + 1)
			if vis, _ := et.ColVisible(pos); !vis {
				continue
			}
			var tone color.Color
			toned := false
			if col.tone != nil {
				tone, toned = col.tone(j)
			}
			text := col.text(j)
			if refit {
				text = fitRunes(text, fitMaxRunes)
			}
			for range et.Cells(uint64(row), pos) {
				if inst.selectableCell(rowBase+uint64(pos), pad, text, false, selected, !col.numeric, tone, toned, truncate) {
					clicked = j.ID
				}
			}
		}
	}
	et.Send()
	inst.captureWidths(et, cols)
	if clicked != "" {
		inst.select_(clicked)
	}
}

// jobsTableTag scopes the list's stored widths and is the etable's id, one
// constant so the two cannot drift apart.
const jobsTableTag = "jobs"

// jobColumnKeys is the resolver's column identity, index-aligned with the
// EtColumn sequence: the "#" gutter, then the columns. The identity is the
// header and a type word, the way play keys its grids.
func jobColumnKeys() (cols []colwidth.Column) {
	cols = make([]colwidth.Column, 0, len(jobColumns)+1)
	cols = append(cols, colwidth.Column{Name: "#", Type: "rownum"})
	for _, col := range jobColumns {
		typ := "text"
		if col.numeric {
			typ = "number"
		}
		cols = append(cols, colwidth.Column{Name: col.header, Type: typ})
	}
	return
}

// captureWidths reads back what egui_table settled on last frame and hands
// it to the resolver, which keeps a drag and lets a fit pass; the flush
// writes what the debounce released.
func (inst *App) captureWidths(et c.EndETableFluid, cols []colwidth.Column) {
	if inst.widths == nil {
		return
	}
	now := time.Now()
	if fetched, ok := et.ColumnWidths(); ok {
		widths := make([]float64, len(fetched))
		for i, w := range fetched {
			widths[i] = float64(w)
		}
		firstShow := !inst.widthsSeen
		inst.widthsSeen = true
		inst.widths.Observe(jobsTableTag, cols, widths, 0, firstShow, now)
	}
	if _, err := inst.widths.Flush(now); err != nil {
		inst.logger.Warn().Err(err).Msg("watchbill app: storing column widths failed; will retry")
	}
}

// cellInset is play's: a frame with the tight inset on both sides, its
// content clipped to the cell, so a header lines up with its values.
func (inst *App) cellInset(id uint64, pad float32, body func()) {
	fr := c.Frame(inst.ids.PrepareSeq(id^seqFrameSalt)).InnerMarginSides(pad, pad, 0, 0)
	for range fr.KeepIter() {
		c.UiClipToMaxRect()
		body()
	}
}

// selectableCell is play's: a frameless, truncated, monospace button
// stretched across the cell, so a click anywhere in the cell — the blank
// part of a short value included — selects the row. Strings sit left,
// numbers centre.
func (inst *App) selectableCell(id uint64, pad float32, text string, weak bool, selected bool, leftAlign bool, tone color.Color, toned bool, truncate bool) (clicked bool) {
	emit := func() {
		var rt c.RichTextScope
		if toned {
			rt = c.Atoms().BeginRichTextColored(tone, color.Transparent, text).Monospace()
		} else {
			rt = c.Atoms().BeginRichText(text).Monospace()
		}
		if weak {
			rt = rt.Weak()
		}
		btn := c.Button(inst.ids.PrepareSeq(id), rt.End().Keep()).Frame(false).Selected(selected)
		if truncate {
			btn = btn.Truncate()
		}
		clicked = btn.SendResp().HasPrimaryClicked()
	}
	inst.cellInset(id, pad, func() {
		if leftAlign {
			for range c.UiWithLayout().MainDirTopDown().CrossAlignMin().CrossJustify(true).KeepIter() {
				emit()
			}
		} else {
			for range c.VerticalCenteredJustified().KeepIter() {
				emit()
			}
		}
	})
	return
}

// fitRunes cuts s to at most n runes, with an ellipsis when it cut.
func fitRunes(s string, n int) (out string) {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
