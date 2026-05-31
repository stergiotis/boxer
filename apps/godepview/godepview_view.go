package godepview

import (
	"fmt"
	"slices"
	"sort"
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
	inst.renderControls()
	c.AddSpace(inst.spaceTight())
	inst.renderGraph()
	c.AddSpace(inst.spaceTight())
	c.Separator().Horizontal().Send()
	c.AddSpace(inst.spaceTight())
	inst.renderTable()
}

func (inst *App) renderControls() {
	r := &inst.man.Run
	for rt := range c.RichTextLabel(fmt.Sprintf("%s  ·  %s  ·  %d packages, %d edges  ·  scope: %s",
		r.RootModulePath, r.GoVersion, r.NumPackages, r.NumEdges, r.Scope)) {
		rt.Strong()
	}
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

	// Row 2: neighborhood (graph) controls.
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
		if inst.focus != 0 {
			focusPath := fmt.Sprintf("#%d", inst.focus)
			if p, ok := inst.idx.Node(inst.focus); ok {
				focusPath = p.ImportPath
			}
			c.Label("focus: " + focusPath).Send()
			c.AddSpace(inst.spaceInner())
			if c.Button(inst.ids.PrepareStr("clear-focus"), c.Atoms().Text("clear").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.focus = 0
			}
		} else {
			c.Label("focus: (none — click a row or graph node)").Send()
		}
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

// renderGraph draws the focused package's import neighborhood. The full
// closure is never drawn — only the focus node's local neighborhood (depth
// + direction), which is what keeps thousands of nodes legible (ADR-0064
// SD5). The collected production-import graph is acyclic, so the
// hierarchical layout is always well-defined (ADR-0064 SD10).
func (inst *App) renderGraph() {
	if inst.focus == 0 || inst.idx == nil {
		c.Label("Select a package below to view its dependency neighborhood.").Send()
		return
	}
	depth := max(int(inst.depth+0.5), 1)
	reached := inst.idx.Neighborhood(inst.focus, depth, inst.dir)

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

	c.Graph(inst.ids.PrepareStr("dep-graph")).
		Height(360).
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
		inst.sortDesc = col == colFiles || col == colImports || col == colImportedBy
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
