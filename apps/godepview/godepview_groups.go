package godepview

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	egcolor "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	lgview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
)

// This file is the architecture (altitude) view: instead of one package's
// neighborhood, it folds the whole closure into a group quotient (each
// apps/<name>, public/<area>, external module a node) and draws it whole with
// the layeredgraph widget — the quotient is tens of nodes, so unlike the package
// closure it needs no focus or cap. Forbidden app→app edges (the keelson rule)
// are painted red; the offending package pairs are listed in the detail pane.

// archIDSalt seeds the architecture canvas + sense-region ids; it is distinct
// from the neighborhood layered canvas (layeredIDSalt) so the two graphs never
// collide even across instances (the seed disambiguates open windows).
const archIDSalt uint64 = 0x2545f4914f6cdd1d

func (inst *App) archIDBase() (id uint64) { id = archIDSalt + inst.seed; return }

// groupDepthInt rounds the grouping-granularity slider to the segment count the
// quotient uses.
func (inst *App) groupDepthInt() (d int) { d = max(int(inst.groupDepth+0.5), 1); return }

// ensureArch rebuilds the group quotient (and clears its cached dot layout) only
// when the grouping signature changes, so a stable depth/showExt/hideStd costs
// neither a rebuild nor a re-layout per frame. Violations are
// grouping-independent and computed once.
func (inst *App) ensureArch() {
	if inst.idx == nil {
		return
	}
	sig := archSig{depth: inst.groupDepthInt(), showExt: inst.archExternalsShown(), hideStd: inst.graphHideStd}
	if inst.archGraph != nil && sig == inst.archSig {
		return
	}
	inst.archSig = sig
	inst.archLayout = nil
	inst.archErr = nil
	inst.archView = lgview.ViewState{}

	include := func(p *godep.PackageNode) (ok bool) {
		switch p.Class {
		case godep.ClassStdlib:
			return !sig.hideStd
		case godep.ClassExternal:
			return sig.showExt
		default:
			return true
		}
	}
	inst.archGraph = inst.idx.BuildGroupGraph(inst.man.Run.RootModulePath, godep.GroupingOpts{InternalDepth: sig.depth}, include)

	// Quotient degrees, for the groups table.
	inst.archOut = make(map[godep.GroupKey]int, len(inst.archGraph.Nodes))
	inst.archIn = make(map[godep.GroupKey]int, len(inst.archGraph.Nodes))
	for _, e := range inst.archGraph.Edges {
		inst.archOut[e.From]++
		inst.archIn[e.To]++
	}

	inst.ensureViolations()
	inst.violEdge = make(map[[2]godep.GroupKey]struct{}, len(inst.violations))
	inst.archViolGroup = make(map[godep.GroupKey]bool, len(inst.violations))
	for _, v := range inst.violations {
		inst.violEdge[[2]godep.GroupKey{v.FromGroup, v.ToGroup}] = struct{}{}
		inst.archViolGroup[v.FromGroup] = true
		inst.archViolGroup[v.ToGroup] = true
	}

	// Group cycles (SCCs of the quotient) and the edges inside them.
	inst.cycles = inst.archGraph.StronglyConnected()
	inst.cycleComp = make(map[godep.GroupKey]int)
	for ci, comp := range inst.cycles {
		for _, k := range comp {
			inst.cycleComp[k] = ci
		}
	}
	inst.cycleEdge = make(map[[2]godep.GroupKey]struct{})
	for _, e := range inst.archGraph.Edges {
		if ci, ok := inst.cycleComp[e.From]; ok {
			if cj, ok2 := inst.cycleComp[e.To]; ok2 && ci == cj {
				inst.cycleEdge[[2]godep.GroupKey{e.From, e.To}] = struct{}{}
			}
		}
	}
}

// archExternalsShown reports whether external module groups appear in the
// quotient: always in Modules view (that view is about them), otherwise per the
// show-external toggle.
func (inst *App) archExternalsShown() (ok bool) {
	return inst.archShowExt || inst.mode == viewModules
}

// archHighlightKey is the group the graph paints in the success tone — the
// focused module in Modules view, otherwise the focused group.
func (inst *App) archHighlightKey() (key godep.GroupKey) {
	if inst.mode == viewModules {
		return godep.GroupKey(inst.focusModule)
	}
	return inst.focusGroup
}

// ensureViolations computes the keelson apps-independence violations once. They
// key on apps/<name> directly (not the view's grouping depth), so they are
// stable across grouping changes and computed a single time per collection.
func (inst *App) ensureViolations() {
	if inst.violReady {
		return
	}
	inst.violReady = true
	if inst.idx == nil {
		return
	}
	inst.violations = inst.idx.SiblingViolations(inst.man.Run.RootModulePath, "apps/")
}

// renderGraphArchitecture draws the group quotient with the layeredgraph widget,
// class-coloured nodes, and forbidden app→app edges in the error tone. Clicking
// an internal group filters the package table to it; clicking an external module
// opens the modules lens on it.
func (inst *App) renderGraphArchitecture() {
	inst.ensureArch()
	if inst.archGraph == nil || len(inst.archGraph.Nodes) == 0 {
		c.Label("No groups to show — widen the grouping depth, show external, or unhide stdlib.").Send()
		return
	}
	if inst.archLayout == nil && inst.archErr == nil {
		inst.archLayout, inst.archErr = layoutGroupGraph(inst.archGraph)
	}
	if inst.archErr != nil {
		c.Label("architecture layout unavailable: " + inst.archErr.Error()).Send()
		return
	}
	lay := inst.archLayout
	if lay == nil || len(lay.Nodes) == 0 {
		return
	}
	// Legend banner: forbidden app→app edges (red) and dependency cycles (amber).
	if len(inst.violations) > 0 || len(inst.cycles) > 0 {
		for range c.Horizontal().KeepIter() {
			if n := len(inst.violations); n > 0 {
				for range c.RichTextLabelColored(egcolor.Hex(styletokens.ErrorDefault.AsHex()), egcolor.Transparent,
					fmt.Sprintf("⚠ %d app→app (red)", n)) {
				}
				c.AddSpace(inst.spaceOuter())
			}
			if n := len(inst.cycles); n > 0 {
				for range c.RichTextLabelColored(egcolor.Hex(styletokens.WarningDefault.AsHex()), egcolor.Transparent,
					fmt.Sprintf("⟳ %d cycle(s) (amber)", n)) {
				}
			}
		}
	}

	w, h := inst.paneAvail(640, 320)
	res := lgview.Render(inst.archIDBase(), lay, lgview.RenderOpts{
		CanvasW:    w,
		CanvasH:    h - 6,
		NodeFill:   inst.archNodeFill(),
		NodeText:   inst.layeredNodeText(), // dark ink on the light *Default fills
		EdgeStroke: inst.archEdgeStroke(),
		State:      &inst.archView,
	})
	if res.Clicked != "" {
		inst.onArchClick(godep.GroupKey(res.Clicked))
	}
}

// layoutGroupGraph builds the dot model from the group quotient — one box per
// group keyed by its group key, one edge per group pair (labelled with the
// crossing-import count when > 1) — and lays it out left→right.
func layoutGroupGraph(gg *godep.GroupGraph) (lay *layeredgraph.Layout, err error) {
	eng, err := goccyengine.Shared()
	if err != nil {
		return nil, err
	}
	m := layeredgraph.GraphModel{
		Nodes: make([]layeredgraph.Node, 0, len(gg.Nodes)),
		Edges: make([]layeredgraph.Edge, 0, len(gg.Edges)),
	}
	for _, n := range gg.Nodes {
		m.Nodes = append(m.Nodes, layeredgraph.Node{
			ID:    string(n.Key),
			Label: shortGroup(n.Key),
			Shape: layeredgraph.NodeShapeBox,
		})
	}
	for _, e := range gg.Edges {
		label := ""
		if e.Weight > 1 {
			label = strconv.Itoa(e.Weight)
		}
		m.Edges = append(m.Edges, layeredgraph.Edge{From: string(e.From), To: string(e.To), Label: label})
	}
	return eng.Layout(context.Background(), m, layeredgraph.LayoutOpts{
		RankDir:  layeredgraph.RankDirLeftRight,
		FontSize: 13,
	})
}

// archNodeFill colours each group by its dominant class (internal accent /
// external info / stdlib neutral), with the focused group/module in the success
// tone so "you are here" stands out.
func (inst *App) archNodeFill() func(id string) (col egcolor.Color, ok bool) {
	highlight := string(inst.archHighlightKey())
	return func(id string) (col egcolor.Color, ok bool) {
		if highlight != "" && id == highlight {
			return egcolor.Hex(styletokens.SuccessDefault.AsHex()), true
		}
		class := ""
		if n, found := inst.archGraph.Node(godep.GroupKey(id)); found {
			class = n.Class
		}
		return egcolor.Hex(classRGBA(class)), true
	}
}

// archEdgeStroke paints a group edge by its worst property: the error tone for a
// forbidden app→app dependency, else the warning tone for an edge inside a
// dependency cycle, else the style default. At grouping depths that collapse the
// endpoints into one node the pair no longer matches an edge — the detail-pane
// lists stay authoritative (see SiblingViolations / StronglyConnected).
func (inst *App) archEdgeStroke() func(from string, to string) (col egcolor.Color, ok bool) {
	red := egcolor.Hex(styletokens.ErrorDefault.AsHex())
	amber := egcolor.Hex(styletokens.WarningDefault.AsHex())
	return func(from string, to string) (col egcolor.Color, ok bool) {
		key := [2]godep.GroupKey{godep.GroupKey(from), godep.GroupKey(to)}
		if _, bad := inst.violEdge[key]; bad {
			return red, true
		}
		if _, cyc := inst.cycleEdge[key]; cyc {
			return amber, true
		}
		return egcolor.Color{}, false
	}
}

// onArchClick selects within the active view (no mode jump — modes change only
// via the top switch or the explicit drill-through links in the detail pane).
// In Modules view a module node becomes the focused module; in Architecture
// view any group becomes the focused group.
func (inst *App) onArchClick(key godep.GroupKey) {
	n, ok := inst.archGraph.Node(key)
	if !ok {
		return
	}
	if inst.mode == viewModules {
		if n.Class == godep.ClassExternal {
			if inst.focusModule != string(key) {
				inst.traceFrom = 0 // a new module invalidates the trace
			}
			inst.focusModule = string(key)
		}
		return
	}
	inst.focusGroup = key
}

// renderArchDetail is the architecture companion in the detail pane: the
// grouping summary and the authoritative list of forbidden app→app edges, each
// a click-through to the offending importer's neighborhood.
func (inst *App) renderArchDetail() {
	inst.ensureViolations()
	groups := 0
	if inst.archGraph != nil {
		groups = len(inst.archGraph.Nodes)
	}
	for rt := range c.RichTextLabel("Architecture") {
		rt.Strong().Size(14)
	}
	c.Label(fmt.Sprintf("%d groups · grouping depth %d · %d crossing edges",
		groups, inst.groupDepthInt(), inst.archEdgeCount())).Send()

	// Focused-group block: the group a row/node click selected, with its members.
	if inst.focusGroup != "" {
		inst.renderGroupDetail()
	}
	inst.renderCyclesSection()
	inst.renderViolationsSection()
}

// renderCyclesSection lists the quotient's dependency cycles (SCCs), each a row
// of click-to-focus group chips. A cycle is the deepest entanglement signal —
// groups that mutually depend — and the per-package view cannot show it.
func (inst *App) renderCyclesSection() {
	c.AddSpace(inst.spaceTight())
	c.Separator().Horizontal().Send()
	c.AddSpace(inst.spaceTight())
	if len(inst.cycles) == 0 {
		for rt := range c.RichTextLabel("✓ No dependency cycles between groups") {
			rt.Strong()
		}
		return
	}
	for rt := range c.RichTextLabel(fmt.Sprintf("Dependency cycles (%d)", len(inst.cycles))) {
		rt.Strong()
	}
	for rt := range c.RichTextLabel("these groups mutually depend — click a group to inspect it") {
		rt.Weak().Small()
	}
	c.AddSpace(inst.spaceTight())
	for ci, comp := range inst.cycles {
		for range c.Horizontal().KeepIter() {
			for j, k := range comp {
				if j > 0 {
					c.Label("⇄").Send()
				}
				if c.SelectableLabel(inst.ids.PrepareStr(fmt.Sprintf("cyc:%d:%d", ci, j)), k == inst.focusGroup, shortGroup(k)).
					SendResp().HasPrimaryClicked() {
					inst.focusGroup = k
				}
			}
		}
	}
}

// renderViolationsSection lists the forbidden app→app edges (the keelson rule),
// each a click-through to the offending importer's neighborhood.
func (inst *App) renderViolationsSection() {
	c.AddSpace(inst.spaceTight())
	c.Separator().Horizontal().Send()
	c.AddSpace(inst.spaceTight())
	if len(inst.violations) == 0 {
		for rt := range c.RichTextLabel("✓ No forbidden app→app dependencies") {
			rt.Strong()
		}
		c.Label("Each apps/<name> tree imports no other app's packages.").Send()
		return
	}
	for rt := range c.RichTextLabel(fmt.Sprintf("Forbidden app→app edges (%d)", len(inst.violations))) {
		rt.Strong()
	}
	for rt := range c.RichTextLabel("keelson apps should not depend on each other — click to inspect the importing package") {
		rt.Weak().Small()
	}
	c.AddSpace(inst.spaceTight())
	shown := min(len(inst.violations), maxDetailListRows)
	for i := range shown {
		v := inst.violations[i]
		label := fmt.Sprintf("%s ▶ %s", shortPath(inst.pathOf(v.FromPkg)), shortPath(inst.pathOf(v.ToPkg)))
		if c.SelectableLabel(inst.ids.PrepareSeq(violSeqBase+uint64(i)), false, label).
			SendResp().HasPrimaryClicked() {
			inst.focus = v.FromPkg
			inst.mode = viewPackages // jump to the offender's neighborhood
		}
	}
	if len(inst.violations) > shown {
		for rt := range c.RichTextLabel(fmt.Sprintf("… +%d more", len(inst.violations)-shown)) {
			rt.Weak().Small()
		}
	}
}

// renderGroupDetail shows the focused group's class, size, and quotient degree,
// then its member packages — each a click-through that opens the Packages view
// focused on that package.
func (inst *App) renderGroupDetail() {
	n, ok := inst.archGraph.Node(inst.focusGroup)
	if !ok {
		return
	}
	c.AddSpace(inst.spaceTight())
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(string(n.Key)) {
			rt.Strong()
		}
		c.AddSpace(inst.spaceInner())
		if c.Button(inst.ids.PrepareStr("grp-clear"), c.Atoms().Text("clear").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.focusGroup = ""
		}
	}
	c.Label(fmt.Sprintf("class: %s · packages: %d · depends on %d groups · used by %d groups",
		n.Class, n.NumPkgs, inst.archOut[n.Key], inst.archIn[n.Key])).Send()
	members := inst.sortedByPath(n.MemberIDs)
	shown := min(len(members), maxDetailListRows)
	for i := range shown {
		id := members[i]
		if c.SelectableLabel(inst.ids.PrepareStr("gm:"+strconv.FormatUint(id, 10)), false, shortPath(inst.pathOf(id))).
			SendResp().HasPrimaryClicked() {
			inst.focus = id
			inst.mode = viewPackages
		}
	}
	if len(members) > shown {
		for rt := range c.RichTextLabel(fmt.Sprintf("… +%d more", len(members)-shown)) {
			rt.Weak().Small()
		}
	}
}

// archEdgeCount reports the number of crossing group edges (0 when the quotient
// is not yet built).
func (inst *App) archEdgeCount() (n int) {
	if inst.archGraph != nil {
		n = len(inst.archGraph.Edges)
	}
	return
}

// violSeqBase is the id seq base for the violations list — disjoint from the
// table seq bases.
const violSeqBase uint64 = 0xD000_0000

// Groups-table column indices (the Architecture master view).
const (
	gcolGroup = iota
	gcolClass
	gcolPkgs
	gcolOut
	gcolIn
	gcolViol
	numGroupCols
)

// groupRowSeqBase is the id seq base for the groups table's rows — disjoint from
// every other table/list base.
const groupRowSeqBase uint64 = 0xE000_0000

// visibleGroupRows is the filtered display order of the quotient's groups
// (indices into archGraph.Nodes), honouring the shared filter box against the
// group key. The quotient is small, so this is recomputed per frame rather than
// cached.
func (inst *App) visibleGroupRows() (rows []int) {
	if inst.archGraph == nil {
		return nil
	}
	needle := strings.ToLower(strings.TrimSpace(inst.filter))
	rows = make([]int, 0, len(inst.archGraph.Nodes))
	for i := range inst.archGraph.Nodes {
		if needle == "" || strings.Contains(strings.ToLower(string(inst.archGraph.Nodes[i].Key)), needle) {
			rows = append(rows, i)
		}
	}
	return rows
}

// renderGroupsTable is the Architecture master: every group in the current
// quotient with its class, package count, out/in quotient degree, and a
// violation flag. Selecting a row focuses the group (highlighted in the graph,
// expanded in the detail pane). Rows follow the quotient's node order (by key),
// filtered by the shared filter box.
func (inst *App) renderGroupsTable() {
	inst.ensureArch()
	if inst.archGraph == nil || len(inst.archGraph.Nodes) == 0 {
		c.Label("No groups — widen the grouping depth, show external, or unhide stdlib.").Send()
		return
	}
	type colDef struct {
		title string
		w     float32
	}
	var cols [numGroupCols]colDef
	cols[gcolGroup] = colDef{"Group", 300}
	cols[gcolClass] = colDef{"Class", 78}
	cols[gcolPkgs] = colDef{"Pkgs", 56}
	cols[gcolOut] = colDef{"Out", 52}    // groups this one depends on
	cols[gcolIn] = colDef{"In", 52}      // groups that depend on this one
	cols[gcolViol] = colDef{"flags", 52} // ⚠ app→app violation · ⟳ in a cycle

	for i := range numGroupCols {
		c.EtColumn(cols[i].w).Resizable(true).Send()
	}

	rows := inst.visibleGroupRows()
	et := c.EndETable(inst.ids.PrepareStr("grp-tbl"), uint64(len(rows)), 20.0, 1, 0)
	for ci := range numGroupCols {
		for range et.Headers(0, uint32(ci)) {
			c.Label(cols[ci].title).Send()
		}
	}

	for dr, ni := range rows {
		n := &inst.archGraph.Nodes[ni]
		for range et.Cells(uint64(dr), gcolGroup) {
			if c.SelectableLabel(inst.ids.PrepareSeq(groupRowSeqBase+uint64(ni)), n.Key == inst.focusGroup, string(n.Key)).
				SendResp().HasPrimaryClicked() {
				inst.focusGroup = n.Key
			}
		}
		for range et.Cells(uint64(dr), gcolClass) {
			c.Label(n.Class).Send()
		}
		for range et.Cells(uint64(dr), gcolPkgs) {
			c.Label(fmt.Sprintf("%d", n.NumPkgs)).Send()
		}
		for range et.Cells(uint64(dr), gcolOut) {
			c.Label(fmt.Sprintf("%d", inst.archOut[n.Key])).Send()
		}
		for range et.Cells(uint64(dr), gcolIn) {
			c.Label(fmt.Sprintf("%d", inst.archIn[n.Key])).Send()
		}
		for range et.Cells(uint64(dr), gcolViol) {
			marks := ""
			if inst.archViolGroup[n.Key] {
				marks += "⚠"
			}
			if _, cyc := inst.cycleComp[n.Key]; cyc {
				marks += "⟳"
			}
			if marks != "" {
				col := egcolor.Hex(styletokens.WarningDefault.AsHex())
				if inst.archViolGroup[n.Key] {
					col = egcolor.Hex(styletokens.ErrorDefault.AsHex())
				}
				for range c.RichTextLabelColored(col, egcolor.Transparent, marks) {
				}
			}
		}
	}

	if inst.focusGroup != "" {
		for dr, ni := range rows {
			if inst.archGraph.Nodes[ni].Key == inst.focusGroup {
				et = et.SelectedRow(uint64(dr))
				break
			}
		}
	}
	et.Striped(true).Send()
}

// shortGroup is the architecture node label: the last two segments of a group
// key, since a full module path or deep internal path is too long for a box.
func shortGroup(k godep.GroupKey) (s string) {
	s = string(k)
	if s == "" {
		return "(root)"
	}
	segs := strings.Split(s, "/")
	if len(segs) <= 2 {
		return s
	}
	return strings.Join(segs[len(segs)-2:], "/")
}
