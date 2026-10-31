package appcenter

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// The Running tab draws its two lists as recordtable grids (ADR-0348 §SD1):
// the open windows, and beneath them the tasks no open window owns. A grid
// draws a record, so each list is held as one: a column per shown column,
// carrying the value its sort compares, while the cell text comes from the
// row itself. The records are rebuilt once per read. The filter is a
// case-insensitive substring, as the app list's is; it orders nothing, it
// only leaves rows out of the order the grid draws.

const (
	runningWindowsTag = "running-windows"
	runningUnownedTag = "running-unowned"
	// unownedHeight is the first height of the panel under the windows; the
	// panel is resizable.
	unownedHeight = 160
)

// runningGrids is what the tab's grids keep between frames.
type runningGrids struct {
	widths     *colwidth.Resolver
	widthsInit bool
	// at is the read the records were built from.
	at          time.Time
	windows     arrow.RecordBatch
	unowned     arrow.RecordBatch
	windowsGrid recordtable.State
	unownedGrid recordtable.State
	windowsSort recordtable.SortState
	unownedSort recordtable.SortState
	// The filtered display orders, reused across frames.
	windowsShown []int64
	unownedShown []int64
}

// records builds the grids' records from rn, once per read.
func (inst *runningGrids) records(rn *running) {
	if inst.windows != nil && inst.at.Equal(rn.read) {
		return
	}
	inst.release()
	inst.windows = gridRecord(windowColumns, rn.view.rows)
	inst.unowned = gridRecord(unownedColumns, rn.view.unowned)
	inst.at = rn.read
}

func (inst *runningGrids) release() {
	if inst.windows != nil {
		inst.windows.Release()
		inst.windows = nil
	}
	if inst.unowned != nil {
		inst.unowned.Release()
		inst.unowned = nil
	}
}

// ensureWidths acquires the column-width resolver on the first frame; the
// store comes from the frame context. Without one, widths are not kept.
func (inst *App) ensureWidths(ctx app.FrameContextI) {
	g := &inst.running
	if g.widthsInit {
		return
	}
	g.widthsInit = true
	h, ok := ctx.(colwidth.HostI)
	if !ok {
		return
	}
	store := h.ColumnWidthStore()
	if store == nil {
		return
	}
	res, err := colwidth.New(store, colwidth.Opts{
		AppId: ctx.AppId(), InstanceKey: ctx.InstanceKey(),
		MinPoints: float64(recordtable.DragMinWidth(inst.density)),
		MaxPoints: float64(recordtable.MaxColumnWidth),
	})
	if err != nil {
		inst.logger.Warn().Err(err).Msg("appcenter: column-width resolver unavailable")
		return
	}
	if lErr := res.Load(); lErr != nil {
		inst.logger.Warn().Err(lErr).Msg("appcenter: stored column widths could not be loaded")
	}
	g.widths = res
}

// cellCtx is what a cell reads besides its row.
type cellCtx struct {
	loopP50Us int64
	now       time.Time
	closing   map[uint64]bool
}

// gridColumn is one column of a list of T: its header, its sort key — a
// string or a number, never both — and its cell.
type gridColumn[T any] struct {
	header string
	hover  string
	width  float32
	str    func(r *T) string
	num    func(r *T) int64
	cell   func(r *T, cx *cellCtx) recordtable.Cell
}

// unsampled sorts a window with no frames below every sampled one.
const unsampled = -1

func sampledOr(r *runningRow, us int64) int64 {
	if !r.sampled {
		return unsampled
	}
	return us
}

func weakCell(text string) recordtable.Cell { return recordtable.Cell{Text: text, Weak: true} }

func textCell(text string) recordtable.Cell { return recordtable.Cell{Text: text, LeftAlign: true} }

var windowColumns = []gridColumn[runningRow]{
	{header: "app", width: 130, hover: "the app the window runs; a click opens its page under Apps",
		str: rowName,
		cell: func(r *runningRow, _ *cellCtx) recordtable.Cell {
			cl := textCell(rowName(r))
			cl.Hover = r.appId
			return cl
		}},
	{header: "window", width: 200, hover: "the window's title",
		str: func(r *runningRow) string { return r.title },
		cell: func(r *runningRow, cx *cellCtx) recordtable.Cell {
			if cx.closing[r.key] {
				cl := textCell(r.title + "  (closing)")
				cl.Weak = true
				return cl
			}
			if r.active {
				return textCell(r.title + "  (active)")
			}
			return textCell(r.title)
		}},
	{header: "p50", width: 72, hover: "median wall time of the window's Frame on the render goroutine",
		num: func(r *runningRow) int64 { return sampledOr(r, r.p50Us) },
		cell: func(r *runningRow, _ *cellCtx) recordtable.Cell {
			if !r.sampled {
				return weakCell("no frames")
			}
			return recordtable.Cell{Text: fmtUs(r.p50Us)}
		}},
	{header: "p95", width: 72, hover: "95th percentile of the same; slow ones are toned",
		num: func(r *runningRow) int64 { return sampledOr(r, r.p95Us) },
		cell: func(r *runningRow, _ *cellCtx) recordtable.Cell {
			if !r.sampled {
				return weakCell("")
			}
			cl := recordtable.Cell{Text: fmtUs(r.p95Us)}
			if r.p95Us >= slowFrameUs {
				cl.Tone, cl.Toned = color.Hex(styletokens.WarningDefault.AsHex()), true
			}
			return cl
		}},
	{header: "of loop", width: 64, hover: "p50 as a share of the render loop's median period",
		num: func(r *runningRow) int64 { return sampledOr(r, r.p50Us) },
		cell: func(r *runningRow, cx *cellCtx) recordtable.Cell {
			if !r.sampled || cx.loopP50Us <= 0 {
				return weakCell("—")
			}
			return recordtable.Cell{Text: fmt.Sprintf("%.0f%%", 100*float64(r.p50Us)/float64(cx.loopP50Us))}
		}},
	{header: "messages", width: 72, hover: "FFFI messages per Frame, on average",
		num: func(r *runningRow) int64 { return sampledOr(r, int64(math.Round(r.messages))) },
		cell: func(r *runningRow, _ *cellCtx) recordtable.Cell {
			if !r.sampled {
				return weakCell("")
			}
			return recordtable.Cell{Text: fmt.Sprintf("%.0f", r.messages)}
		}},
	{header: "tasks", width: 56, hover: "tasks in flight the window registered with the task API, a lower bound of its background work",
		num:  func(r *runningRow) int64 { return int64(r.tasks) },
		cell: func(r *runningRow, _ *cellCtx) recordtable.Cell { return recordtable.Cell{Text: fmt.Sprint(r.tasks)} }},
	{header: "oldest task", width: 220, hover: "the first of its tasks to start, and how long ago",
		str: func(r *runningRow) string { return r.oldestCreateAt },
		cell: func(r *runningRow, cx *cellCtx) recordtable.Cell {
			if r.tasks == 0 {
				return weakCell("—")
			}
			return textCell(r.oldestTitle + ", " + ageOf(r.oldestCreateAt, cx.now))
		}},
	{header: "agent tasks", width: 80, hover: "the agent tasks whose grant holds the window",
		num: func(r *runningRow) int64 { return int64(len(r.agentTasks)) },
		cell: func(r *runningRow, _ *cellCtx) recordtable.Cell {
			if len(r.agentTasks) == 0 {
				return weakCell("—")
			}
			return recordtable.Cell{Text: fmt.Sprint(len(r.agentTasks))}
		}},
}

var unownedColumns = []gridColumn[unownedTask]{
	{header: "task", width: 220, hover: "the task's title",
		str:  func(u *unownedTask) string { return u.title },
		cell: func(u *unownedTask, _ *cellCtx) recordtable.Cell { return textCell(u.title) }},
	{header: "kind", width: 120, hover: "the task's kind",
		str:  func(u *unownedTask) string { return u.kind },
		cell: func(u *unownedTask, _ *cellCtx) recordtable.Cell { return textCell(u.kind) }},
	{header: "state", width: 90, hover: "running, cancelling or abandoned",
		str:  func(u *unownedTask) string { return u.state },
		cell: func(u *unownedTask, _ *cellCtx) recordtable.Cell { return textCell(u.state) }},
	{header: "owner", width: 120, hover: "the app that started it; empty for a runtime service",
		str: func(u *unownedTask) string { return u.ownerAppId },
		cell: func(u *unownedTask, _ *cellCtx) recordtable.Cell {
			cl := textCell(shortApp(u.ownerAppId))
			cl.Hover = u.ownerAppId
			return cl
		}},
	{header: "window", width: 80, hover: "the closed window that started it; — for none",
		num:  func(u *unownedTask) int64 { return int64(u.ownerKey) },
		cell: func(u *unownedTask, _ *cellCtx) recordtable.Cell { return recordtable.Cell{Text: windowOf(u.ownerKey)} }},
	{header: "age", width: 80, hover: "how long ago it started",
		str: func(u *unownedTask) string { return u.createdAt },
		cell: func(u *unownedTask, cx *cellCtx) recordtable.Cell {
			return recordtable.Cell{Text: ageOf(u.createdAt, cx.now)}
		}},
}

// gridRecord holds rows' sort keys as a record, row i being rows[i]; one
// field per column, named by its header.
func gridRecord[T any](cols []gridColumn[T], rows []T) (rec arrow.RecordBatch) {
	mem := memory.DefaultAllocator
	fields := make([]arrow.Field, 0, len(cols))
	arrs := make([]arrow.Array, 0, len(cols))
	for _, col := range cols {
		if col.num != nil {
			b := array.NewInt64Builder(mem)
			for i := range rows {
				b.Append(col.num(&rows[i]))
			}
			arrs = append(arrs, b.NewArray())
			b.Release()
			fields = append(fields, arrow.Field{Name: col.header, Type: arrow.PrimitiveTypes.Int64})
			continue
		}
		b := array.NewStringBuilder(mem)
		for i := range rows {
			b.Append(col.str(&rows[i]))
		}
		arrs = append(arrs, b.NewArray())
		b.Release()
		fields = append(fields, arrow.Field{Name: col.header, Type: arrow.BinaryTypes.String})
	}
	rec = array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, int64(len(rows)))
	for _, a := range arrs {
		a.Release()
	}
	return
}

// gridColumns lays cols out for the grid over rows. A number sorts
// descending first: the interesting end of a time or a count is the top.
func gridColumns[T any](cols []gridColumn[T], rows []T, cx *cellCtx) (out []recordtable.Column) {
	out = make([]recordtable.Column, 0, len(cols))
	for i, col := range cols {
		typ := "text"
		if col.num != nil {
			typ = "number"
		}
		out = append(out, recordtable.Column{
			Arrow: i, Header: col.header, Hover: col.hover, Width: col.width, WidthType: typ, DescFirst: col.num != nil,
			Cell: func(row int64) recordtable.Cell { return col.cell(&rows[row], cx) },
		})
	}
	return
}

// filterFold is the filter as rows are compared with it.
func filterFold(filter string) string { return strings.ToLower(strings.TrimSpace(filter)) }

func containsFold(s, f string) bool { return strings.Contains(strings.ToLower(s), f) }

// windowMatches says whether the folded filter f is a substring of the
// window's app id, app name, title or one of its tasks' titles.
func windowMatches(r *runningRow, f string) bool {
	if f == "" || containsFold(r.appId, f) || containsFold(r.display, f) || containsFold(r.title, f) {
		return true
	}
	for _, t := range r.taskTitles {
		if containsFold(t, f) {
			return true
		}
	}
	return false
}

// unownedMatches says whether f is a substring of the task's title, kind or
// owner.
func unownedMatches(u *unownedTask, f string) bool {
	return f == "" || containsFold(u.title, f) || containsFold(u.kind, f) || containsFold(u.ownerAppId, f)
}

// shownOrder is the display order of the rows keep admits: the sort's order,
// or the record's when order is nil, filtered into buf. It is never nil, so
// a filter that admits nothing draws no rows rather than all of them.
func shownOrder(order []int64, n int, keep func(i int64) bool, buf []int64) []int64 {
	if buf == nil {
		buf = make([]int64, 0, n)
	}
	buf = buf[:0]
	for pos := range int64(n) {
		i := pos
		if order != nil {
			i = order[pos]
		}
		if keep(i) {
			buf = append(buf, i)
		}
	}
	return buf
}

// renderRunningTab is the Running tab: the filter and Close all above the
// windows, and the tasks no open window owns in a panel beneath them.
func (inst *App) renderRunningTab(snap snapshot) {
	rn := &snap.running
	v := &rn.view
	g := &inst.running
	f := filterFold(inst.runFilter)
	cx := &cellCtx{loopP50Us: v.loopP50Us, now: time.Now(), closing: inst.closing}
	read := !rn.read.IsZero()
	if read {
		g.records(rn)
		inst.pruneClosing(v)
		g.windowsShown = shownOrder(g.windowsSort.OrderFor(g.windows, g.windows.NumRows()), len(v.rows),
			func(i int64) bool { return windowMatches(&v.rows[i], f) }, g.windowsShown)
		g.unownedShown = shownOrder(g.unownedSort.OrderFor(g.unowned, g.unowned.NumRows()), len(v.unowned),
			func(i int64) bool { return unownedMatches(&v.unowned[i], f) }, g.unownedShown)
	}
	if read && len(g.unownedShown) > 0 {
		for range c.PanelBottomInside(inst.ids.PrepareStr("unowned")).Resizable(true).DefaultSize(unownedHeight).KeepIter() {
			inst.renderUnowned(v, cx)
		}
	}
	for range c.PanelCentralInside().KeepIter() {
		inst.renderRunning(rn, cx)
	}
}

func (inst *App) renderRunning(rn *running, cx *cellCtx) {
	v := &rn.view
	g := &inst.running
	weak("Every open window of this host. Frame time is the window's Frame on the render goroutine, " +
		"which every window shares; tasks are the ones the window registered with the task API, so the count is a lower bound. " +
		"The apps share one process, so memory and CPU are not shown per window: imztop shows the process's.")
	c.AddSpace(styletokens.PaddingInner(inst.density))
	h := hostOf()
	for range c.HorizontalTop().KeepIter() {
		// Typing commits per keystroke inside the frame (ADR-0345 §SD3); the
		// handle lets the running_filter resource say the person is typing.
		inst.runFilterH = widgethandle.Make(inst.ids.PrepareStr("run-filter").Derive())
		c.TextEdit(inst.ids.PrepareStr("run-filter"), inst.runFilter, false).
			HintText("filter windows and tasks").
			DesiredWidth(240).
			SendRespVal(&inst.runFilter)
		// A host that wires none shows no Close all (ADR-0348 §SD4).
		if h != nil && !rn.read.IsZero() {
			inst.renderCloseAll(h, v)
		}
	}
	if rn.read.IsZero() {
		c.Label("Reading…").Send()
		return
	}
	if !inst.lensReady("running-windows", rn.windows.state, rn.windows.note, len(v.rows), "No window is open.") {
		return
	}
	if rn.frames.state != lensStateOk {
		inst.lensReady("running-frames", rn.frames.state, rn.frames.note, 1, "")
	}
	if rn.tasks.state != lensStateOk {
		inst.lensReady("running-tasks", rn.tasks.state, rn.tasks.note, 1, "")
	}
	head := fmt.Sprintf("%d of %d window(s).", len(g.windowsShown), len(v.rows))
	if v.loopP50Us > 0 {
		head += " The render loop's median period is " + fmtUs(v.loopP50Us) + "."
	}
	c.Label(head).Send()
	res := recordtable.RenderGrid(recordtable.Input{
		Ids: inst.ids, ScopeKey: runningWindowsTag, TableTag: runningWindowsTag,
		Record: g.windows, Columns: gridColumns(windowColumns, v.rows, cx),
		Widths: g.widths, Sort: &g.windowsSort, Order: g.windowsShown,
		NoRowNumbers: true, State: &g.windowsGrid,
	})
	inst.gridResult("windows", res)
	g.windowsSort.Apply(res)
	if res.Clicked >= 0 && res.Clicked < int64(len(v.rows)) {
		// The app's own page under Apps, through the catalog (ADR-0345 §SD3).
		inst.selectGesture(v.rows[res.Clicked].appId)
	}
}

// renderCloseAll is the one action: a button naming how many of the shown
// windows it closes, and its confirmation, which names them.
func (inst *App) renderCloseAll(h HostI, v *runningView) {
	if inst.closeArmed != nil {
		c.Label("Close " + armedNames(v, inst.closeArmed) + "?").Send()
		if c.Button(inst.ids.PrepareStr("close-confirm"), c.Atoms().Text(icons.PhX+" "+closeCount(len(inst.closeArmed))).Keep()).SendResp().HasPrimaryClicked() {
			inst.closeAll(h)
		}
		if c.Button(inst.ids.PrepareStr("close-keep"), c.Atoms().Text("Keep").Keep()).SendResp().HasPrimaryClicked() {
			inst.disarmCloseAll()
		}
		return
	}
	keys := inst.closable(v, inst.running.windowsShown)
	for range c.EnabledUi(len(keys) > 0).KeepIter() {
		for range c.HoverText("Closes every window the filter shows, this one last, after asking").KeepIter() {
			if c.Button(inst.ids.PrepareStr("close-all"), c.Atoms().Text(fmt.Sprintf("%s Close %d shown", icons.PhX, len(keys))).Keep()).SendResp().HasPrimaryClicked() {
				inst.armCloseAll(keys)
			}
		}
	}
}

// closeCount is the confirm button's label.
func closeCount(n int) string {
	if n == 1 {
		return "Close 1 window"
	}
	return fmt.Sprintf("Close %d windows", n)
}

// armedNamesShown bounds the titles a confirmation names.
const armedNamesShown = 3

// armedNames names the armed windows by title, the first few and a count.
func armedNames(v *runningView, keys []uint64) string {
	titles := make([]string, 0, armedNamesShown)
	for _, key := range keys {
		if len(titles) == armedNamesShown {
			break
		}
		for i := range v.rows {
			if v.rows[i].key == key {
				titles = append(titles, v.rows[i].title)
				break
			}
		}
	}
	s := strings.Join(titles, ", ")
	if more := len(keys) - len(titles); more > 0 {
		s += fmt.Sprintf(" and %d more", more)
	}
	return s
}

func (inst *App) renderUnowned(v *runningView, cx *cellCtx) {
	g := &inst.running
	strong("Not owned by an open window")
	weak("A closed window's work still finishing, which its window's drain waits for, and runtime services' tasks, which no window owns.")
	res := recordtable.RenderGrid(recordtable.Input{
		Ids: inst.ids, ScopeKey: runningUnownedTag, TableTag: runningUnownedTag,
		Record: g.unowned, Columns: gridColumns(unownedColumns, v.unowned, cx),
		Widths: g.widths, Sort: &g.unownedSort, Order: g.unownedShown,
		NoRowNumbers: true, State: &g.unownedGrid,
	})
	inst.gridResult("unowned tasks", res)
	g.unownedSort.Apply(res)
}

// gridResult logs what a grid refused or failed to store.
func (inst *App) gridResult(which string, res recordtable.Result) {
	if res.Err != nil {
		inst.logger.Warn().Err(res.Err).Str("grid", which).Msg("appcenter: the running grid refused its input")
	}
	if res.WidthsErr != nil {
		inst.logger.Warn().Err(res.WidthsErr).Str("grid", which).Msg("appcenter: storing column widths failed; will retry")
	}
}
