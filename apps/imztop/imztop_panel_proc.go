package imztop

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
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
)

type procViewState struct {
	SortBy ProcSortByE
	Desc   bool
	Filter string
	Tree   bool
}

var (
	procViewMu sync.RWMutex
	procView   = procViewState{SortBy: ProcSortByCPU, Desc: true}
)

func loadProcView() (out procViewState) {
	procViewMu.RLock()
	out = procView
	procViewMu.RUnlock()
	return
}

func setProcSort(by ProcSortByE) {
	procViewMu.Lock()
	if procView.SortBy == by {
		procView.Desc = !procView.Desc
	} else {
		procView.SortBy = by
		procView.Desc = by != ProcSortByPID && by != ProcSortByUser && by != ProcSortByName
	}
	procViewMu.Unlock()
}

func setProcFilter(filter string) {
	procViewMu.Lock()
	procView.Filter = filter
	procViewMu.Unlock()
}

func toggleProcTree() {
	procViewMu.Lock()
	procView.Tree = !procView.Tree
	procViewMu.Unlock()
}

// applyProcView filters and sorts the (infos, smoothed) pair per the
// supplied view and returns both — kept aligned by index so the
// renderer can read a row's raw fields from infos[i] and its smoothed
// CPU% from smoothed[i] without a PID lookup. Called by the Sampler
// before publishing; the renderer never mutates the returned slices.
// Sort by ProcSortByCPU keys off the smoothed slice so the row order
// is stable across brief spikes (see procCPUEWMAAlpha); every other
// sort key reads from infos.
func applyProcView(infos []sysmsnap.ProcInfo, smoothed []float32, v procViewState) (outInfos []sysmsnap.ProcInfo, outSmoothed []float32) {
	if v.Filter != "" {
		needle := strings.ToLower(v.Filter)
		wi := infos[:0]
		ws := smoothed[:0]
		for i, p := range infos {
			if strings.Contains(strings.ToLower(p.Name), needle) || strings.Contains(strings.ToLower(p.Cmd), needle) {
				wi = append(wi, p)
				ws = append(ws, smoothed[i])
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
	}
	if cmpf != nil && desc {
		inner := cmpf
		cmpf = func(a, b int) int { return -inner(a, b) }
	}
	return
}

func (inst *App) renderProcPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("Processes")
	view := loadProcView()

	// Tour mode sets procView.Filter directly via the scene setup
	// callback (imztop_tour.go) and never types into the TextEdit, so
	// without this sync the captured PNG would show an empty filter
	// box while the proc list is filtered. Interactive mode keeps
	// procFilterDraft persistent so the user's keystrokes accumulate.
	if imzero2env.ScreenshotDir.Get() != "" {
		inst.procFilterDraft = view.Filter
	}

	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("%d procs", len(snap.Procs))).Send()
		c.AddSpace(inst.spaceOuter())
		c.Label("Filter").Send()
		c.AddSpace(inst.spaceInner())
		// procFilterDraft must be a persistent field on inst — the
		// SendRespVal binding writes the response asynchronously
		// between frames (one-frame lag per the FFFI databinding
		// reset rule). A local var would be reset to view.Filter
		// every frame and the typed text would never accumulate.
		resp := c.TextEdit(inst.ids.PrepareStr("proc-filter"), inst.procFilterDraft, false).
			DesiredWidth(220).
			HintText("name or cmd substring").
			SendRespVal(&inst.procFilterDraft)
		if resp.HasChanged() {
			setProcFilter(inst.procFilterDraft)
		}
		c.AddSpace(inst.spaceOuter())
		treeLabel := "tree ▸"
		if view.Tree {
			treeLabel = "tree ▾"
		}
		if c.Button(inst.ids.PrepareStr("proc-tree-tgl"), c.Atoms().Text(treeLabel).Keep()).
			Selected(view.Tree).
			Frame(true).
			SendResp().HasPrimaryClicked() {
			toggleProcTree()
		}
	}
	c.AddSpace(inst.spaceInner())

	c.EtColumn(70.0).Resizable(true).Send()  // PID
	c.EtColumn(110.0).Resizable(true).Send() // User
	c.EtColumn(70.0).Resizable(true).Send()  // CPU%~ (smoothed)
	c.EtColumn(70.0).Resizable(true).Send()  // CPU%  (raw)
	c.EtColumn(90.0).Resizable(true).Send()  // RSS
	c.EtColumn(90.0).Resizable(true).Send()  // VSZ
	c.EtColumn(50.0).Resizable(true).Send()  // State
	c.EtColumn(120.0).Resizable(true).Send() // Name
	c.EtColumn(400.0).Resizable(true).Send() // Cmd

	et := c.EndETable(inst.ids.PrepareStr("proc-tbl"),
		uint64(len(snap.Procs)),
		procRowHeight,
		1, // numStickyHeaders: keep the sortable header row pinned during scroll
		0, // numStickyCols: no frozen leading columns
	).Striped(true)

	inst.renderProcHeader(et, view)

	// Tree mode reorders the (already filtered + sorted) rows into a PPID
	// forest, depth-first; flat mode renders snap.Procs as-is (depth 0). The
	// reordering is render-only — srcIdx maps an output row back to its index
	// in snap.Procs / ProcCPUSmoothed, so per-row data and the smoothed-tint
	// lookup stay correct in both modes.
	var treeOrder, treeDepth []int
	if view.Tree {
		treeOrder, treeDepth = buildProcOrder(snap.Procs)
	}
	for row := range snap.Procs {
		srcIdx, d := row, 0
		if view.Tree {
			srcIdx, d = treeOrder[row], treeDepth[row]
		}
		p := snap.Procs[srcIdx]
		r := uint64(row)
		for range et.Cells(r, 0) {
			procCellLabel(fmt.Sprintf("%d", p.PID))
		}
		for range et.Cells(r, 1) {
			procCellLabel(p.User)
		}
		smoothedPct := p.CPUPercent
		if srcIdx < len(snap.ProcCPUSmoothed) {
			smoothedPct = snap.ProcCPUSmoothed[srcIdx]
		}
		for range et.Cells(r, 2) {
			// CPU%~ — smoothed. Tinted with the heatmap palette only
			// when the smoothed column is driving the sort, so the
			// background colour reads as a visual hint "this is the
			// value the row order is keying off". Same idiom on the
			// raw column below.
			inst.renderProcCPUCell(r, smoothedPct, view.SortBy == ProcSortByCPU, 0x6000)
		}
		for range et.Cells(r, 3) {
			// CPU% — raw sampler-interval value. Tinted only when the
			// user has clicked the raw header (sort key flipped to
			// ProcSortByCPURaw). When neither CPU column is the active
			// sort, both stay untinted — the heatmap above already
			// carries the colour-coded load story.
			inst.renderProcCPUCell(r, p.CPUPercent, view.SortBy == ProcSortByCPURaw, 0x6100)
		}
		for range et.Cells(r, 4) {
			procCellLabel(humanBytes(p.RSSBytes))
		}
		for range et.Cells(r, 5) {
			procCellLabel(humanBytes(p.VMSizeBytes))
		}
		for range et.Cells(r, 6) {
			procCellLabel(string(p.State))
		}
		for range et.Cells(r, 7) {
			procCellLabel(treeIndent(d, p.Name))
		}
		for range et.Cells(r, 8) {
			procCellLabel(p.Cmd)
		}
	}

	et.Send()
}

const (
	// procRowHeight is the per-row height in the process table; chosen
	// to leave ~7 px of vertical breathing room around the default
	// label font (egui dark, ~14 px) while staying compact enough for
	// 1000+ rows on a typical laptop screen.
	procRowHeight float32 = 24.0
	// procCellPadX is the horizontal padding inside each process-table
	// cell, applied on BOTH sides of the cell's content. Without the
	// trailing pad, long values (PID, RSS, VSZ, …) butt right up
	// against the next column's resize line; without the leading pad,
	// they butt against this column's resize line. 6 px gives enough
	// breathing room at the IDS body font for both ends while staying
	// compact at narrow column widths (PID/CPU%/State columns are
	// 50–70 px).
	procCellPadX float32 = 6.0
)

// procCellLabel renders a plain text label inside a process-table cell
// with symmetric leading + trailing horizontal pad so the text never
// touches either of the column's resize handles. Use this for every
// text cell that doesn't need a colour-coded value (those wrap
// RichTextLabel / TintedScope directly with the same pad pattern).
func procCellLabel(text string) {
	for range c.Horizontal().KeepIter() {
		c.AddSpace(procCellPadX)
		c.Label(text).Send()
		c.AddSpace(procCellPadX)
	}
}

// renderProcCPUCell renders one of the process table's CPU% cells.
// When tint is true the cell background is mapped through the heatmap
// palette via cpuPercentBgColor(pct), so the row visually advertises
// "this column drives the sort"; when false the cell is a plain
// padded label, identical to every other text cell. baseSeq must
// differ between the smoothed and raw columns so the TintedScope
// widget id stays unique within the frame even if both columns are
// ever simultaneously asked to tint (the contract is one-or-none
// today, but a future toggle could change that).
func (inst *App) renderProcCPUCell(r uint64, pct float32, tint bool, baseSeq uint64) {
	text := fmt.Sprintf("%.1f", pct)
	if !tint {
		procCellLabel(text)
		return
	}
	bg := inst.cpuPercentBgColor(pct)
	cellId := inst.ids.PrepareSeq(baseSeq + r)
	for range c.TintedScope(cellId, bg).KeepIter() {
		for range c.Horizontal().KeepIter() {
			c.AddSpace(procCellPadX)
			c.Label(text).Send()
			c.AddSpace(procCellPadX)
		}
	}
}

func (inst *App) renderProcHeader(et c.EndETableFluid, view procViewState) {
	// "CPU%~" carries the trailing tilde as a visual hint that the
	// column shows smoothed (EWMA) values rather than the raw
	// sampler-interval reading shown by the plain "CPU%" header
	// immediately to its right. The two columns are independently
	// sortable; only the active sort's column is tinted (see
	// renderProcCPUCell), giving the user a colour cue for which
	// value drives the row order.
	headers := []struct {
		label string
		key   ProcSortByE
		seq   uint64
	}{
		{"PID", ProcSortByPID, 0x600},
		{"User", ProcSortByUser, 0x601},
		{"CPU%~", ProcSortByCPU, 0x602},
		{"CPU%", ProcSortByCPURaw, 0x603},
		{"RSS", ProcSortByMem, 0x604},
		{"VSZ", procSortNoneSentinel, 0x605},
		{"S", procSortNoneSentinel, 0x606},
		{"Name", ProcSortByName, 0x607},
		{"Cmd", procSortNoneSentinel, 0x608},
	}
	for col, h := range headers {
		colu := uint32(col)
		hc := h
		for range et.Headers(0, colu) {
			label := hc.label
			if hc.key != procSortNoneSentinel && view.SortBy == hc.key {
				if view.Desc {
					label += " ▼"
				} else {
					label += " ▲"
				}
			}
			selected := hc.key != procSortNoneSentinel && view.SortBy == hc.key
			for range c.Horizontal().KeepIter() {
				c.AddSpace(procCellPadX)
				if c.Button(inst.ids.PrepareSeq(hc.seq), c.Atoms().Text(label).Keep()).
					Selected(selected).
					FrameWhenInactive(false).
					Frame(false).
					SendResp().HasPrimaryClicked() {
					if hc.key != procSortNoneSentinel {
						setProcSort(hc.key)
					}
				}
				c.AddSpace(procCellPadX)
			}
		}
	}
}

// procSortNoneSentinel marks header columns that are not sortable. Picked
// well outside the defined ProcSortByE range so a normal const cannot
// alias it.
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
