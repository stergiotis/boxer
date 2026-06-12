package godepview

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	egcolor "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// renderLoading is shown while the collector runs off-thread. It requests a
// repaint each frame so the loop keeps ticking until done flips.
func (inst *App) renderLoading() {
	for rt := range c.RichTextLabel("Collecting Go package graph…") {
		rt.Strong()
	}
	c.AddSpace(inst.spaceTight())
	c.Label("Running go/packages over the module's transitive closure. This can take a few seconds.").Send()
	c.RequestRepaint()
}

func (inst *App) renderError(err error) {
	for rt := range c.RichTextLabel("Dependency collection failed") {
		rt.Strong()
	}
	c.AddSpace(inst.spaceTight())
	c.Label(err.Error()).Send()
	c.AddSpace(inst.spaceInner())
	c.Label("Tip: launch via the boxer wrapper so GOFLAGS carries the repo build tags, or set Config.Tags on the collector.").Send()
}

func (inst *App) renderExplorer() {
	if inst.viewDirty {
		inst.rebuildView()
	}
	inst.ensureNeighborhood()
	inst.renderControls()
	c.AddSpace(inst.spaceTight())

	// Master–detail as a three-leaf dock: the package table (master) on the
	// left, the focus-neighborhood graph top-right, and the focus detail pane
	// bottom-right. A dock leaf hands its content a bounded rect, which is what
	// lets the detail pane's ScrollArea clip+scroll and the graph fill its pane
	// (a width-pinned column would collapse a ScrollArea to its first child —
	// schemaview's hard-won idiom).
	c.UiSetMinHeight(dockMinHeight)
	for dock := range c.DockArea(inst.ids.PrepareStr("dock")) {
		root := dock.InitRoot(tabPackages)
		// Table keeps 55% of the width (eight columns); the graph and detail
		// pane split the right column evenly so the detail lists are visible
		// without scrolling. All leaves are drag-resizable and persist.
		graphLeaf := dock.Split(root, c.DockRight, 0.55, tabGraph)
		dock.Split(graphLeaf, c.DockBelow, 0.50, tabDetail)

		for range dock.Tab(tabPackages, "packages") {
			inst.renderTable()
		}
		for range dock.Tab(tabGraph, "neighborhood") {
			inst.renderNeighborhoodGraph()
		}
		for range dock.Tab(tabDetail, "detail") {
			for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
				inst.renderDetail()
			}
		}
	}
}

func (inst *App) renderControls() {
	r := &inst.man.Run
	for rt := range c.RichTextLabel(fmt.Sprintf("%s  ·  %s  ·  %d packages, %d edges  ·  scope: %s",
		r.RootModulePath, r.GoVersion, r.NumPackages, r.NumEdges, r.Scope)) {
		rt.Strong()
	}
	tagsStr := "(inherited from GOFLAGS)"
	if len(r.BuildTags) > 0 {
		tagsStr = strings.Join(r.BuildTags, ", ")
	}
	c.Label("build tags: " + tagsStr).Send()
	c.AddSpace(inst.spaceTight())
	c.Separator().Horizontal().Send()
	c.AddSpace(inst.spaceTight())

	// Row 1: text filter + class toggles.
	for range c.Horizontal().KeepIter() {
		c.Label("Filter").Send()
		c.AddSpace(inst.spaceInner())
		resp := c.TextEdit(inst.ids.PrepareStr("flt"), inst.filter, false).
			DesiredWidth(280).
			HintText("import-path substring").
			SendRespVal(&inst.filter)
		if resp.HasChanged() {
			inst.viewDirty = true
		}
		c.AddSpace(inst.spaceOuter())
		inst.classToggle("cls-std", godep.ClassStdlib, &inst.showStd)
		inst.classToggle("cls-int", godep.ClassInternal, &inst.showInt)
		inst.classToggle("cls-ext", godep.ClassExternal, &inst.showExt)
		c.AddSpace(inst.spaceOuter())
		c.Label(fmt.Sprintf("%d shown", len(inst.view))).Send()
	}
	c.AddSpace(inst.spaceTight())

	// Row 2: neighborhood (graph) controls. The focused package is shown in the
	// detail pane (with its own clear button), not here.
	for range c.Horizontal().KeepIter() {
		c.Label("Neighborhood").Send()
		c.AddSpace(inst.spaceInner())
		c.SliderF64(inst.ids.PrepareStr("depth"), inst.depth, 1, 4).
			Text("depth").
			SendRespVal(&inst.depth)
		c.AddSpace(inst.spaceOuter())
		inst.dirToggle("dir-imp", "imports ▸", godep.DirImports)
		inst.dirToggle("dir-impd", "importers ◂", godep.DirImporters)
		inst.dirToggle("dir-both", "both", godep.DirBoth)
		c.AddSpace(inst.spaceOuter())
		inst.boolToggle("hide-std", "hide stdlib", &inst.graphHideStd)
		c.AddSpace(inst.spaceOuter())
		c.Label("engine").Send()
		inst.engineToggle("eng-live", "live", false)
		inst.engineToggle("eng-layered", "layered", true)
	}
}

// boolToggle is a framed on/off button bound to *on.
func (inst *App) boolToggle(id string, label string, on *bool) {
	if c.Button(inst.ids.PrepareStr(id), c.Atoms().Text(label).Keep()).
		Selected(*on).
		Frame(true).
		SendResp().HasPrimaryClicked() {
		*on = !*on
	}
}

// engineToggle is one segment of the live/layered graph-engine switch.
func (inst *App) engineToggle(id string, label string, layered bool) {
	if c.Button(inst.ids.PrepareStr(id), c.Atoms().Text(label).Keep()).
		Selected(inst.useLayered == layered).
		Frame(true).
		SendResp().HasPrimaryClicked() {
		inst.useLayered = layered
	}
}

func (inst *App) classToggle(id string, class string, on *bool) {
	if c.Button(inst.ids.PrepareStr(id), c.Atoms().Text(class).Keep()).
		Selected(*on).
		Frame(true).
		SendResp().HasPrimaryClicked() {
		*on = !*on
		inst.viewDirty = true
	}
}

func (inst *App) dirToggle(id string, label string, dir godep.Direction) {
	if c.Button(inst.ids.PrepareStr(id), c.Atoms().Text(label).Keep()).
		Selected(inst.dir == dir).
		Frame(true).
		SendResp().HasPrimaryClicked() {
		inst.dir = dir
	}
}

// renderNeighborhoodGraph draws the focused package's import neighborhood with
// the selected engine. The full closure is never drawn — only the focus node's
// bounded local neighborhood (depth + direction + cap), which is what keeps
// thousands of nodes legible (ADR-0064 SD5). The neighborhood is computed once
// per change by ensureNeighborhood; this only renders inst.graphReached.
func (inst *App) renderNeighborhoodGraph() {
	if inst.focus == 0 || inst.idx == nil {
		c.Label("Select a package — click a table row, a graph node, or a detail-pane entry.").Send()
		return
	}
	if inst.graphTruncated > 0 {
		hint := "depth / direction"
		if !inst.graphHideStd {
			hint += " / hide stdlib"
		}
		for rt := range c.RichTextLabel(fmt.Sprintf("neighborhood capped at %d nodes — narrow with %s", len(inst.graphReached), hint)) {
			rt.Weak().Small()
		}
	}
	if inst.useLayered {
		inst.renderGraphLayered()
		return
	}
	inst.renderGraphLive()
}

// renderGraphLive draws the neighborhood with the egui_graphs Graph widget
// (hierarchical layout, live pan/zoom/drag). It reads the cached reached set;
// the collected production-import graph is acyclic, so the hierarchical layout
// is well-defined (ADR-0064 SD10).
func (inst *App) renderGraphLive() {
	reached := inst.graphReached

	// Emit in sorted id order: Go map iteration is randomized, and a stable
	// emission order keeps the layout (and tour captures) reproducible.
	ids := make([]uint64, 0, len(reached))
	for id := range reached {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		c.GraphNode(id, inst.nodeLabel(id)).Color(inst.nodeColor(id)).Send()
	}
	// Every directed edge whose endpoints are both in the neighborhood,
	// emitted once (from the importer side). Iterating each node's forward
	// Imports covers in- and out-edges alike, with no duplicates.
	for _, id := range ids {
		p, ok := inst.idx.Node(id)
		if !ok {
			continue
		}
		for _, to := range p.Imports {
			if _, in := reached[to]; in {
				// Empty label suppresses the widget's default "edge N"
				// caption — an import edge needs no per-edge text.
				c.GraphEdge(id, to).Label("").Send()
			}
		}
	}

	_, h := inst.paneAvail(640, 320)
	c.Graph(inst.ids.PrepareStr("dep-graph")).
		Height(h - 6).
		Layout(uint8(c.GraphLayoutHierarchical)).
		LayoutOrientation(uint8(c.GraphHierarchicalOrientationLeftRight)).
		LayoutCenterParent(true).
		FitToScreen(true).
		FitPadding(0.12).
		ZoomAndPan(true).
		DraggingEnabled(true).
		HoverEnabled(true).
		NodeClickingEnabled(true).
		NodeSelectionEnabled(true).
		LabelsAlways(true).
		Send()

	// Drain last frame's events; a node click re-focuses the graph.
	for _, e := range c.FetchGraphEvents() {
		switch e.Kind {
		case c.GraphEventKindNodeClick, c.GraphEventKindNodeDoubleClick:
			inst.focus = e.KeyA
		}
	}
}

// paneAvail returns the current dock pane's available size (one-frame-lagged),
// capturing it for next frame. Falls back to fbW/fbH when the capture is
// NaN/too-small (e.g. the first frame before any capture lands).
func (inst *App) paneAvail(fbW float32, fbH float32) (w float32, h float32) {
	avail := c.CurrentApplicationState.StateManager.GetAvailableSize()
	c.CaptureAvailableSize()
	w, h = fbW, fbH
	if avail.W == avail.W && avail.W > 80 { // == rejects NaN
		w = avail.W
	}
	if avail.H == avail.H && avail.H > 80 {
		h = avail.H
	}
	return
}

func (inst *App) renderTable() {
	type colDef struct {
		title string
		w     float32
	}
	var cols [numCols]colDef
	cols[colImportPath] = colDef{"Import path", 380}
	cols[colName] = colDef{"Name", 120}
	cols[colClass] = colDef{"Class", 78}
	cols[colModule] = colDef{"Module", 200}
	cols[colFiles] = colDef{"Files", 52}
	cols[colImports] = colDef{"Out", 52}
	cols[colImportedBy] = colDef{"In", 52}
	cols[colWasm] = colDef{"WASM", 72} // wasi js freestanding glyphs (ADR-0080)

	for i := range numCols {
		c.EtColumn(cols[i].w).Resizable(true).Send()
	}

	et := c.EndETable(inst.ids.PrepareStr("dep-tbl"),
		uint64(len(inst.view)), 20.0, 1, 0)

	// Clickable headers → column sort. Headers are deferred blocks like
	// cells, so a Button inside one is interactive.
	for ci := range numCols {
		title := cols[ci].title
		if inst.sortCol == ci {
			if inst.sortDesc {
				title += " ▼"
			} else {
				title += " ▲"
			}
		}
		for range et.Headers(0, uint32(ci)) {
			if c.Button(inst.ids.PrepareSeq(hdrSeqBase+uint64(ci)), c.Atoms().Text(title).Keep()).
				Frame(false).
				SendResp().HasPrimaryClicked() {
				inst.toggleSort(ci)
			}
		}
	}

	// Rows. Only column 0 carries an interactive widget (the selectable
	// path); the rest are plain labels, so the per-row id cost is one
	// PrepareSeq, not a per-cell string allocation.
	for row, pi := range inst.view {
		p := &inst.man.Packages[pi]
		for range et.Cells(uint64(row), colImportPath) {
			if c.SelectableLabel(inst.ids.PrepareSeq(rowSeqBase+uint64(row)), p.Id == inst.focus, p.ImportPath).
				SendResp().HasPrimaryClicked() {
				inst.focus = p.Id
			}
		}
		for range et.Cells(uint64(row), colName) {
			c.Label(p.Name).Send()
		}
		for range et.Cells(uint64(row), colClass) {
			c.Label(p.Class).Send()
		}
		for range et.Cells(uint64(row), colModule) {
			c.Label(p.ModulePath).Send()
		}
		for range et.Cells(uint64(row), colFiles) {
			c.Label(fmt.Sprintf("%d", p.NumGoFiles)).Send()
		}
		for range et.Cells(uint64(row), colImports) {
			c.Label(fmt.Sprintf("%d", p.NumImports)).Send()
		}
		for range et.Cells(uint64(row), colImportedBy) {
			c.Label(fmt.Sprintf("%d", p.NumImportedBy)).Send()
		}
		for range et.Cells(uint64(row), colWasm) {
			inst.renderWasmCell(p.ImportPath)
		}
	}

	if inst.focus != 0 {
		for row, pi := range inst.view {
			if inst.man.Packages[pi].Id == inst.focus {
				et = et.SelectedRow(uint64(row))
				break
			}
		}
	}
	et.Striped(true).Send()
}

func (inst *App) toggleSort(col int) {
	if inst.sortCol == col {
		inst.sortDesc = !inst.sortDesc
	} else {
		inst.sortCol = col
		// Numeric columns default to descending (biggest first); text ascending.
		inst.sortDesc = col == colFiles || col == colImports || col == colImportedBy || col == colWasm
	}
	inst.viewDirty = true
}

func (inst *App) classVisible(class string) (ok bool) {
	switch class {
	case godep.ClassStdlib:
		return inst.showStd
	case godep.ClassInternal:
		return inst.showInt
	case godep.ClassExternal:
		return inst.showExt
	}
	return true
}

func (inst *App) rebuildView() {
	inst.view = inst.view[:0]
	needle := strings.ToLower(strings.TrimSpace(inst.filter))
	for i := range inst.man.Packages {
		p := &inst.man.Packages[i]
		if !inst.classVisible(p.Class) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(p.ImportPath), needle) {
			continue
		}
		inst.view = append(inst.view, i)
	}
	inst.sortView()
	inst.viewDirty = false
}

func (inst *App) sortView() {
	pk := inst.man.Packages
	less := func(a, b int) bool {
		pa, pb := &pk[a], &pk[b]
		switch inst.sortCol {
		case colName:
			return pa.Name < pb.Name
		case colClass:
			return pa.Class < pb.Class
		case colModule:
			return pa.ModulePath < pb.ModulePath
		case colFiles:
			return pa.NumGoFiles < pb.NumGoFiles
		case colImports:
			return pa.NumImports < pb.NumImports
		case colImportedBy:
			return pa.NumImportedBy < pb.NumImportedBy
		case colWasm:
			return wasmCompileCount(pa.ImportPath) < wasmCompileCount(pb.ImportPath)
		default:
			return pa.ImportPath < pb.ImportPath
		}
	}
	sort.SliceStable(inst.view, func(i, j int) bool {
		if inst.sortDesc {
			return less(inst.view[j], inst.view[i])
		}
		return less(inst.view[i], inst.view[j])
	})
}

// nodeLabel is a short, readable label for a graph node (last two path
// segments) — full import paths are too long to render inside a node.
func (inst *App) nodeLabel(id uint64) (label string) {
	p, ok := inst.idx.Node(id)
	if !ok {
		return fmt.Sprintf("#%d", id)
	}
	return shortPath(p.ImportPath)
}

func (inst *App) nodeColor(id uint64) (col egcolor.Color) {
	class := ""
	if p, ok := inst.idx.Node(id); ok {
		class = p.Class
	}
	return egcolor.Hex(classRGBA(class))
}

func classRGBA(class string) (rgba uint32) {
	switch class {
	case godep.ClassInternal:
		return styletokens.AccentDefault.AsHex()
	case godep.ClassExternal:
		return styletokens.InfoDefault.AsHex()
	default: // stdlib / unknown
		return styletokens.NeutralDefault.AsHex()
	}
}

func shortPath(p string) (s string) {
	if p == "" {
		return "(unknown)"
	}
	segs := strings.Split(p, "/")
	if len(segs) <= 2 {
		return p
	}
	return ".../" + strings.Join(segs[len(segs)-2:], "/")
}

// maxDetailListRows bounds how many entries each detail list renders (sorted by
// path); the rest collapse into a "+N more" note. A hub package's importer set
// can be thousands — the table is where you browse those, not this pane.
const maxDetailListRows = 80

// ensureDetail rebuilds the focused package's path-sorted import / importer
// lists, but only when the focus changes (the lists are focus-only, independent
// of the graph's depth/dir/hideStd), so a hub package's large importer set is
// sorted once per selection, not once per frame.
func (inst *App) ensureDetail() {
	if inst.detailReady && inst.detailFocus == inst.focus {
		return
	}
	inst.detailReady = true
	inst.detailFocus = inst.focus
	inst.detailImports = nil
	inst.detailImporters = nil
	if inst.focus == 0 || inst.idx == nil {
		return
	}
	if p, ok := inst.idx.Node(inst.focus); ok {
		inst.detailImports = inst.sortedByPath(p.Imports)
	}
	inst.detailImporters = inst.sortedByPath(inst.idx.Importers(inst.focus))
}

// sortedByPath copies ids and sorts them by import path for a stable, readable
// list order.
func (inst *App) sortedByPath(ids []uint64) (out []uint64) {
	out = append([]uint64(nil), ids...)
	slices.SortFunc(out, func(a uint64, b uint64) int {
		return strings.Compare(inst.pathOf(a), inst.pathOf(b))
	})
	return
}

// pathOf returns a package id's import path, or a "#id" placeholder if the id
// is not in this collection.
func (inst *App) pathOf(id uint64) (path string) {
	if p, ok := inst.idx.Node(id); ok {
		return p.ImportPath
	}
	return "#" + strconv.FormatUint(id, 10)
}

// renderDetail is the focus pane: the focused package's metadata plus its direct
// Imports and Imported-by lists. The lists are click-to-focus, so they are the
// navigation that still works when the neighborhood is too large to graph — the
// graph is capped, but these lists are complete (only display-bounded).
func (inst *App) renderDetail() {
	inst.ensureDetail()
	if inst.focus == 0 || inst.idx == nil {
		for rt := range c.RichTextLabel("No package focused. Click a table row, a graph node, or a list entry.") {
			rt.Weak()
		}
		return
	}
	p, ok := inst.idx.Node(inst.focus)
	if !ok {
		for rt := range c.RichTextLabel(fmt.Sprintf("focused id #%d not found in this collection", inst.focus)) {
			rt.Weak()
		}
		return
	}

	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(p.ImportPath) {
			rt.Strong().Size(14)
		}
		c.AddSpace(inst.spaceInner())
		if c.Button(inst.ids.PrepareStr("detail-clear"), c.Atoms().Text("clear").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.focus = 0
		}
	}
	for range c.Horizontal().KeepIter() {
		c.Label("class: " + p.Class).Send()
		c.AddSpace(inst.spaceInner())
		c.Label("module: " + p.ModulePath).Send()
	}
	for range c.Horizontal().KeepIter() {
		c.Label("name: " + p.Name).Send()
		c.AddSpace(inst.spaceInner())
		c.Label(fmt.Sprintf("files: %d", p.NumGoFiles)).Send()
		c.AddSpace(inst.spaceInner())
		c.Label(fmt.Sprintf("out: %d  in: %d", p.NumImports, p.NumImportedBy)).Send()
	}
	inst.renderWasmDetail(p.ImportPath)
	if p.Dir != "" {
		for rt := range c.RichTextLabel(p.Dir) {
			rt.Weak().Small()
		}
	}

	c.AddSpace(inst.spaceTight())
	c.Separator().Horizontal().Send()
	c.AddSpace(inst.spaceTight())
	inst.renderDepList("imp", fmt.Sprintf("Imports (%d)", len(inst.detailImports)), inst.detailImports)
	c.AddSpace(inst.spaceInner())
	inst.renderDepList("impby", fmt.Sprintf("Imported by (%d)", len(inst.detailImporters)), inst.detailImporters)
}

// renderDepList renders one path-sorted, click-to-focus dependency list,
// bounded to maxDetailListRows with a "+N more" trailer.
func (inst *App) renderDepList(idkey string, title string, sorted []uint64) {
	for rt := range c.RichTextLabel(title) {
		rt.Strong()
	}
	if len(sorted) == 0 {
		for rt := range c.RichTextLabel("— none") {
			rt.Weak()
		}
		return
	}
	shown := min(len(sorted), maxDetailListRows)
	for i := range shown {
		id := sorted[i]
		if c.SelectableLabel(inst.ids.PrepareStr(idkey+":"+strconv.FormatUint(id, 10)), id == inst.focus, inst.pathOf(id)).
			SendResp().HasPrimaryClicked() {
			inst.focus = id
		}
	}
	if len(sorted) > shown {
		for rt := range c.RichTextLabel(fmt.Sprintf("… +%d more (browse in the table)", len(sorted)-shown)) {
			rt.Weak().Small()
		}
	}
}
