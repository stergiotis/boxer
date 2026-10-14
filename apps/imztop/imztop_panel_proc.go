package imztop

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// ProcSortByE selects which column the process panel sorts on.
type ProcSortByE uint8

const (
	// ProcSortByCPU keys on the EWMA-smoothed CPU% (sampler's
	// ProcCPUSmoothed slice). Default sort key: order is stable
	// across transient spikes, matching the column the heatmap palette
	// is tinted onto.
	ProcSortByCPU ProcSortByE = iota
	// ProcSortByCPURaw keys on the raw sampler-interval CPU% value.
	// Useful when the user is hunting transient spikes that the
	// smoothed view dampens. When selected, the raw column is tinted
	// instead of the smoothed one.
	ProcSortByCPURaw
	ProcSortByMem
	ProcSortByPID
	ProcSortByUser
	ProcSortByName
	// ProcSortByIORead / ProcSortByIOWrite key on the storage-layer
	// byte rates from /proc/[pid]/io. A process whose rate is unknown
	// (unreadable io file, first sighting) sorts below every measured
	// one, idle included.
	ProcSortByIORead
	ProcSortByIOWrite
)

type procViewState struct {
	SortBy ProcSortByE
	Desc   bool
	Filter string
	Tree   bool
}

// procViewMemo is one window's cached filter+sort of a published
// snapshot. src is compared by pointer: Sampler.Latest swaps the pointer
// on every publish, so pointer identity is exactly "same sample".
type procViewMemo struct {
	src      *PublishedSnapshot
	view     procViewState
	infos    []sysmsnap.ProcInfo
	smoothed []float32
}

// defaultProcView is the sort/filter every window starts with.
func defaultProcView() (out procViewState) {
	return procViewState{SortBy: ProcSortByCPU, Desc: true}
}

// The sort key, filter and tree toggle are per-window state on *App, like
// the rest of this app's UI state (imztop.go). They used to be package
// globals behind a RWMutex, because the singleton Sampler applied them
// before publishing and so needed one view for the whole process — which
// also meant two imztop windows silently shared a sort order and a filter,
// and the tour's filter leaked into a live window. The view is applied at
// render time now, so the mutex is gone with it: these are only ever
// touched from the render thread of the window that owns them.

// setProcSort is the header click: the same key flips the direction, a new
// key starts in its own. It computes the view and sets it through
// set_proc_view, which never toggles (ADR-0329 §SD2).
//
// The cycle is recordtable.SortState's with NoUnsorted — the list always has
// an order — and each key's own first direction.
func (inst *App) setProcSort(by ProcSortByE) {
	v := inst.procView
	s := recordtable.SortState{NoUnsorted: true, Active: true, Col: procColumnOf(v.SortBy), Desc: v.Desc}
	s.ClickedFrom(procColumnOf(by), procSortDefaultDesc(by))
	v.SortBy, v.Desc = by, s.Desc
	inst.setProcViewGesture(v)
}

// setProcViewGesture sets the whole view as the person's gesture.
func (inst *App) setProcViewGesture(v procViewState) {
	gestureOr(inst, opSetProcView, SetProcViewArgs{SortBy: procSortName(v.SortBy), Order: orderName(v.Desc),
		Filter: v.Filter, Tree: v.Tree}, func() { inst.applyProcView(v) })
}

func (inst *App) setProcFilter(filter string) {
	inst.procView.Filter = filter
}

func (inst *App) toggleProcTree() {
	v := inst.procView
	v.Tree = !v.Tree
	inst.setProcViewGesture(v)
}

// viewProcs returns snap's process list filtered and sorted by this
// window's view, with the smoothed-CPU slice kept index-aligned to it.
//
// Applying the view moved off the Sampler when it stopped being
// process-wide, so this runs per frame rather than per sample. The result
// is memoised against the snapshot pointer and the view, so the actual
// work still happens once per published sample — and every panel in a
// frame shares the one result, which is what keeps the process table and
// the Proc Map showing the same set.
func (inst *App) viewProcs(snap *PublishedSnapshot) (infos []sysmsnap.ProcInfo, smoothed []float32) {
	if snap == nil {
		return
	}
	if inst.procViewMemo.src == snap && inst.procViewMemo.view == inst.procView {
		return inst.procViewMemo.infos, inst.procViewMemo.smoothed
	}
	infos, smoothed = applyProcView(snap.Procs, snap.ProcCPUSmoothed, inst.procView)
	inst.procViewMemo = procViewMemo{
		src:      snap,
		view:     inst.procView,
		infos:    infos,
		smoothed: smoothed,
	}
	return
}

// applyProcView filters and sorts the (infos, smoothed) pair per the
// supplied view and returns both — kept aligned by index so the
// renderer can read a row's raw fields from infos[i] and its smoothed
// CPU% from smoothed[i] without a PID lookup. Sort by ProcSortByCPU keys
// off the smoothed slice so the row order is stable across brief spikes
// (see procCPUEWMAAlpha); every other sort key reads from infos.
//
// The inputs are never written to. They belong to a published snapshot
// that every window reads concurrently, so the filter allocates rather
// than compacting in place as it did while the Sampler owned the slices
// outright. With no filter and nothing to reorder the inputs are returned
// as they are, which is safe because nobody mutates them.
func applyProcView(infos []sysmsnap.ProcInfo, smoothed []float32, v procViewState) (outInfos []sysmsnap.ProcInfo, outSmoothed []float32) {
	if v.Filter != "" {
		needle := strings.ToLower(v.Filter)
		wi := make([]sysmsnap.ProcInfo, 0, len(infos))
		ws := make([]float32, 0, len(smoothed))
		for i, p := range infos {
			if strings.Contains(strings.ToLower(p.Name), needle) || strings.Contains(strings.ToLower(p.Cmd), needle) {
				wi = append(wi, p)
				if i < len(smoothed) {
					ws = append(ws, smoothed[i])
				}
			}
		}
		infos = wi
		smoothed = ws
	}
	if cmpIdx := procIndexCmp(infos, smoothed, v.SortBy, v.Desc); cmpIdx != nil {
		indices := make([]int, len(infos))
		for i := range indices {
			indices[i] = i
		}
		slices.SortStableFunc(indices, cmpIdx)
		sortedInfos := make([]sysmsnap.ProcInfo, len(infos))
		sortedSmoothed := make([]float32, len(infos))
		for ni, oi := range indices {
			sortedInfos[ni] = infos[oi]
			sortedSmoothed[ni] = smoothed[oi]
		}
		infos = sortedInfos
		smoothed = sortedSmoothed
	}
	outInfos = infos
	outSmoothed = smoothed
	return
}

// procIndexCmp returns an index-keyed comparator over the parallel
// (infos, smoothed) slices. Index-keyed instead of value-keyed because
// ProcSortByCPU reads its key from smoothed[i] rather than the proc
// itself — closing over both slices lets one comparator shape serve
// every sort key without packing the value into a synthetic struct.
func procIndexCmp(infos []sysmsnap.ProcInfo, smoothed []float32, by ProcSortByE, desc bool) (cmpf func(a, b int) int) {
	switch by {
	case ProcSortByCPU:
		cmpf = func(a, b int) int { return cmp.Compare(smoothed[a], smoothed[b]) }
	case ProcSortByCPURaw:
		cmpf = func(a, b int) int { return cmp.Compare(infos[a].CPUPercent, infos[b].CPUPercent) }
	case ProcSortByMem:
		cmpf = func(a, b int) int { return cmp.Compare(infos[a].RSSBytes, infos[b].RSSBytes) }
	case ProcSortByPID:
		cmpf = func(a, b int) int { return cmp.Compare(infos[a].PID, infos[b].PID) }
	case ProcSortByUser:
		cmpf = func(a, b int) int { return cmp.Compare(infos[a].User, infos[b].User) }
	case ProcSortByName:
		cmpf = func(a, b int) int { return cmp.Compare(infos[a].Name, infos[b].Name) }
	case ProcSortByIORead:
		cmpf = func(a, b int) int { return cmp.Compare(procIOKey(&infos[a], true), procIOKey(&infos[b], true)) }
	case ProcSortByIOWrite:
		cmpf = func(a, b int) int { return cmp.Compare(procIOKey(&infos[a], false), procIOKey(&infos[b], false)) }
	}
	if cmpf != nil && desc {
		inner := cmpf
		cmpf = func(a, b int) int { return -inner(a, b) }
	}
	return
}

// procIOKey is a process's read or write rate as a sort key, -1 when the
// rate is unknown so unmeasured rows never interleave with measured ones.
func procIOKey(p *sysmsnap.ProcInfo, read bool) (key int64) {
	if !p.IOKnown {
		return -1
	}
	if read {
		return int64(p.IOReadBytesPerSec)
	}
	return int64(p.IOWriteBytesPerSec)
}

// procIORateText formats a read or write rate for the process table, with
// an en dash for a rate that was not measured.
func procIORateText(p *sysmsnap.ProcInfo, rate uint64) (text string) {
	if !p.IOKnown {
		return "–"
	}
	return humanBytes(rate) + "/s"
}

func (inst *App) renderProcPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("Processes")
	view := inst.procView
	// One filter+sort for the whole frame, shared with the Proc Map so the
	// two panels never disagree about which processes are in view.
	procs, smoothed := inst.viewProcs(snap)

	// Tour mode sets this window's filter directly via the scene setup
	// callback (imztop_tour.go) and never types into the TextEdit, so
	// without this sync the captured PNG would show an empty filter
	// box while the proc list is filtered. Interactive mode keeps
	// procFilterDraft persistent so the user's keystrokes accumulate.
	if imzero2env.ScreenshotDir.Get() != "" {
		inst.procFilterDraft = view.Filter
	}

	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("%d procs", len(procs))).Send()
		c.AddSpace(inst.spaceOuter())
		c.Label("Filter").Send()
		c.AddSpace(inst.spaceInner())
		// procFilterDraft must be a persistent field on inst — the
		// SendRespVal binding writes the response asynchronously
		// between frames (one-frame lag per the FFFI databinding
		// reset rule). A local var would be reset to view.Filter
		// every frame and the typed text would never accumulate.
		inst.procFilterH = widgethandle.Make(inst.ids.PrepareStr("proc-filter").Derive())
		resp := c.TextEdit(inst.ids.PrepareStr("proc-filter"), inst.procFilterDraft, false).
			DesiredWidth(220).
			HintText("name or cmd substring").
			SendRespVal(&inst.procFilterDraft)
		if resp.HasChanged() {
			inst.setProcFilter(inst.procFilterDraft)
		}
		c.AddSpace(inst.spaceOuter())
		treeLabel := "tree " + icons.PhCaretRight
		if view.Tree {
			treeLabel = "tree " + icons.PhCaretDown
		}
		if c.Button(inst.ids.PrepareStr("proc-tree-tgl"), c.Atoms().Text(treeLabel).Keep()).
			Selected(view.Tree).
			Frame(true).
			SendResp().HasPrimaryClicked() {
			inst.toggleProcTree()
		}
	}
	c.AddSpace(inst.spaceInner())

	rec, order, depth := inst.procTableRecord(snap, procs, smoothed, view)
	// The header shows this window's sort; imztop orders the rows itself
	// (applyProcView, then the tree), so the grid only draws the glyph and
	// reports the click.
	inst.procSort = recordtable.SortState{NoUnsorted: true}
	if col := procColumnOf(view.SortBy); col >= 0 {
		inst.procSort.Active, inst.procSort.Col, inst.procSort.Desc = true, col, view.Desc
	}
	res := recordtable.RenderGrid(recordtable.Input{
		Ids: inst.ids, ScopeKey: "proc-tbl", Record: rec,
		Columns: inst.procGridColumns(procs, smoothed, depth, view),
		Sort:    &inst.procSort, Order: order,
		RowHeight: procRowHeight, NoRowNumbers: true,
		State: &inst.procGrid,
	})
	if res.SortClicked >= 0 {
		s := inst.procSort
		s.Apply(res)
		v := inst.procView
		v.SortBy, v.Desc = procColumns[s.Col].key, s.Desc
		inst.setProcViewGesture(v)
	}
}

// procRowHeight is the per-row height in the process table; chosen to leave
// ~7 px of vertical breathing room around the default label font (egui dark,
// ~14 px) while staying compact enough for 1000+ rows on a typical laptop
// screen.
const procRowHeight float32 = 24.0

// procColumn is one process-table column: its header, seed width, the sort
// key a header click selects (procSortNoneSentinel: not sortable), and the
// value the record holds for it — an integer, a float or a string, exactly
// one of them, mirroring what procIndexCmp compares.
type procColumn struct {
	header string
	width  float32
	key    ProcSortByE
	hover  string
	i64    func(r procRow) int64
	f64    func(r procRow) float64
	str    func(r procRow) string
	text   func(r procRow) string
}

// procRow is what a column reads for one row: the process, its smoothed
// CPU% and its depth in tree mode.
type procRow struct {
	p        *sysmsnap.ProcInfo
	smoothed float32
	depth    int
}

func cpuText(pct float32) string { return strconv.FormatFloat(float64(pct), 'f', 1, 32) }

var procColumns = []procColumn{
	{header: "PID", width: 70, key: ProcSortByPID, hover: "process id",
		i64: func(r procRow) int64 { return int64(r.p.PID) }, text: func(r procRow) string { return strconv.FormatUint(uint64(r.p.PID), 10) }},
	{header: "User", width: 110, key: ProcSortByUser, hover: "the process's owner",
		str: func(r procRow) string { return r.p.User }, text: func(r procRow) string { return r.p.User }},
	// "CPU%~" carries the trailing tilde as a visual hint that the column
	// shows smoothed (EWMA) values rather than the raw sampler-interval
	// reading of the plain "CPU%" beside it. Only the column driving the
	// order is tinted (procGridColumns).
	{header: "CPU%~", width: 70, key: ProcSortByCPU, hover: "CPU%, smoothed (EWMA): the default order, steady across brief spikes",
		f64: func(r procRow) float64 { return float64(r.smoothed) }, text: func(r procRow) string { return cpuText(r.smoothed) }},
	{header: "CPU%", width: 70, key: ProcSortByCPURaw, hover: "CPU% over the last sampler interval",
		f64: func(r procRow) float64 { return float64(r.p.CPUPercent) }, text: func(r procRow) string { return cpuText(r.p.CPUPercent) }},
	{header: "RSS", width: 90, key: ProcSortByMem, hover: "resident memory",
		i64: func(r procRow) int64 { return int64(r.p.RSSBytes) }, text: func(r procRow) string { return humanBytes(r.p.RSSBytes) }},
	{header: "VSZ", width: 90, key: procSortNoneSentinel, hover: "virtual memory size",
		i64: func(r procRow) int64 { return int64(r.p.VMSizeBytes) }, text: func(r procRow) string { return humanBytes(r.p.VMSizeBytes) }},
	{header: "Read/s", width: 100, key: ProcSortByIORead, hover: "storage read rate; – where it was not measured, sorted below every measured rate",
		i64: func(r procRow) int64 { return procIOKey(r.p, true) }, text: func(r procRow) string { return procIORateText(r.p, r.p.IOReadBytesPerSec) }},
	{header: "Write/s", width: 100, key: ProcSortByIOWrite, hover: "storage write rate; – where it was not measured, sorted below every measured rate",
		i64: func(r procRow) int64 { return procIOKey(r.p, false) }, text: func(r procRow) string { return procIORateText(r.p, r.p.IOWriteBytesPerSec) }},
	{header: "S", width: 50, key: procSortNoneSentinel, hover: "process state",
		str: func(r procRow) string { return string(r.p.State) }, text: func(r procRow) string { return string(r.p.State) }},
	{header: "Name", width: 120, key: ProcSortByName, hover: "process name; indented under its parent in tree mode",
		str: func(r procRow) string { return r.p.Name }, text: func(r procRow) string { return treeIndent(r.depth, r.p.Name) }},
	{header: "Cmd", width: 400, key: procSortNoneSentinel, hover: "command line",
		str: func(r procRow) string { return r.p.Cmd }, text: func(r procRow) string { return r.p.Cmd }},
}

// procColumnOf is the column a sort key selects, -1 for none.
func procColumnOf(by ProcSortByE) (col int) {
	for i, pc := range procColumns {
		if pc.key == by {
			return i
		}
	}
	return -1
}

// procSchema is the record's shape: one field per column, named by its
// header, typed by the value it holds.
var procSchema = func() *arrow.Schema {
	fields := make([]arrow.Field, len(procColumns))
	for i, pc := range procColumns {
		var typ arrow.DataType = arrow.BinaryTypes.String
		switch {
		case pc.i64 != nil:
			typ = arrow.PrimitiveTypes.Int64
		case pc.f64 != nil:
			typ = arrow.PrimitiveTypes.Float64
		}
		fields[i] = arrow.Field{Name: pc.header, Type: typ}
	}
	return arrow.NewSchema(fields, nil)
}()

// procTable is the process table's record and row order for one published
// sample under one view: rebuilt with the view-applied process list, so once
// per sample like the list itself.
type procTable struct {
	src   *PublishedSnapshot
	view  procViewState
	rec   arrow.RecordBatch
	order []int64
	depth []int
}

// procTableRecord returns the record of procs (row i is procs[i]), the
// display order — nil in flat mode, the PPID forest in tree mode — and each
// record row's tree depth.
func (inst *App) procTableRecord(snap *PublishedSnapshot, procs []sysmsnap.ProcInfo, smoothed []float32, view procViewState) (rec arrow.RecordBatch, order []int64, depth []int) {
	t := &inst.procTbl
	if t.rec != nil && t.src == snap && t.view == view {
		return t.rec, t.order, t.depth
	}
	if t.rec != nil {
		t.rec.Release()
	}
	depth = make([]int, len(procs))
	if view.Tree {
		// Tree mode reorders the (already filtered + sorted) rows into a
		// PPID forest, depth-first; it is built over all procs, since a
		// visible row's depth depends on the whole forest.
		treeOrder, treeDepth := buildProcOrder(procs)
		order = make([]int64, len(treeOrder))
		for k, idx := range treeOrder {
			order[k] = int64(idx)
			depth[idx] = treeDepth[k]
		}
	}
	mem := memory.DefaultAllocator
	arrs := make([]arrow.Array, len(procColumns))
	for ci, pc := range procColumns {
		switch {
		case pc.i64 != nil:
			b := array.NewInt64Builder(mem)
			for i := range procs {
				b.Append(pc.i64(procRowAt(procs, smoothed, depth, i)))
			}
			arrs[ci] = b.NewArray()
			b.Release()
		case pc.f64 != nil:
			b := array.NewFloat64Builder(mem)
			for i := range procs {
				b.Append(pc.f64(procRowAt(procs, smoothed, depth, i)))
			}
			arrs[ci] = b.NewArray()
			b.Release()
		default:
			b := array.NewStringBuilder(mem)
			for i := range procs {
				b.Append(pc.str(procRowAt(procs, smoothed, depth, i)))
			}
			arrs[ci] = b.NewArray()
			b.Release()
		}
	}
	rec = array.NewRecordBatch(procSchema, arrs, int64(len(procs)))
	for _, a := range arrs {
		a.Release()
	}
	*t = procTable{src: snap, view: view, rec: rec, order: order, depth: depth}
	return
}

// procRowAt is record row i's view of the process list. A process without a
// smoothed reading shows its raw CPU%.
func procRowAt(procs []sysmsnap.ProcInfo, smoothed []float32, depth []int, i int) (r procRow) {
	r = procRow{p: &procs[i], smoothed: procs[i].CPUPercent, depth: depth[i]}
	if i < len(smoothed) {
		r.smoothed = smoothed[i]
	}
	return
}

// procGridColumns lays the columns out for the grid. The CPU column that
// drives the order is tinted with the heatmap palette, so the row order reads
// as keyed off that value; when neither CPU column is the sort, both stay
// plain — the heatmap already carries the load story.
func (inst *App) procGridColumns(procs []sysmsnap.ProcInfo, smoothed []float32, depth []int, view procViewState) (cols []recordtable.Column) {
	cols = make([]recordtable.Column, len(procColumns))
	for i, pc := range procColumns {
		tint := (pc.key == ProcSortByCPU || pc.key == ProcSortByCPURaw) && view.SortBy == pc.key
		cols[i] = recordtable.Column{
			Arrow: i, Header: pc.header, Hover: pc.hover, Width: pc.width,
			NoSort: pc.key == procSortNoneSentinel, DescFirst: procSortDefaultDesc(pc.key),
			Cell: func(row int64) (cell recordtable.Cell) {
				r := procRowAt(procs, smoothed, depth, int(row))
				cell = recordtable.Cell{Text: pc.text(r), LeftAlign: true}
				if tint {
					pct := r.p.CPUPercent
					if pc.key == ProcSortByCPU {
						pct = r.smoothed
					}
					cell.Fill, cell.Filled = inst.cpuPercentBgColor(pct), true
				}
				return
			},
		}
	}
	return
}

// procSortNoneSentinel marks columns that are not sortable. Picked well
// outside the defined ProcSortByE range so a normal const cannot alias it.
const procSortNoneSentinel ProcSortByE = 255

// treeIndent prefixes a process name with a depth-proportional tree marker.
// Depth 0 (a forest root) is unindented; deeper rows get one "  " per level
// plus a "└ " elbow, so the column reads as a parent → child hierarchy.
func treeIndent(depth int, name string) string {
	if depth <= 0 {
		return name
	}
	return strings.Repeat("  ", depth-1) + "└ " + name
}

// buildProcOrder reorders infos into a PPID forest, depth-first, returning the
// output-row → infos-index permutation alongside each row's tree depth.
// Siblings keep infos' incoming order, so the active sort is preserved within
// each parent. A process whose parent is absent from infos (the table is
// truncated to the top-N by CPU, or it's a real root like PID 1) starts a new
// root. A visited guard makes PID-reuse cycles safe; any node the walk misses
// is appended at depth 0.
func buildProcOrder(infos []sysmsnap.ProcInfo) (order, depth []int) {
	n := len(infos)
	idxByPID := make(map[uint32]int, n)
	for i := range infos {
		idxByPID[infos[i].PID] = i
	}
	children := make(map[int][]int)
	var roots []int
	for i := range infos {
		if pi, ok := idxByPID[infos[i].PPID]; ok && pi != i {
			children[pi] = append(children[pi], i)
		} else {
			roots = append(roots, i)
		}
	}
	order = make([]int, 0, n)
	depth = make([]int, 0, n)
	visited := make([]bool, n)
	type frame struct{ idx, d int }
	stack := make([]frame, 0, n)
	for i := len(roots) - 1; i >= 0; i-- {
		stack = append(stack, frame{roots[i], 0})
	}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[f.idx] {
			continue
		}
		visited[f.idx] = true
		order = append(order, f.idx)
		depth = append(depth, f.d)
		ch := children[f.idx]
		for i := len(ch) - 1; i >= 0; i-- {
			if !visited[ch[i]] {
				stack = append(stack, frame{ch[i], f.d + 1})
			}
		}
	}
	for i := range infos {
		if !visited[i] {
			order = append(order, i)
			depth = append(depth, 0)
		}
	}
	return
}
