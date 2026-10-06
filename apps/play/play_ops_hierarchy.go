package play

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle"
	icicleview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// The Treemap and Icicle panes as an agent reads and sets them (ADR-0270,
// update of 2026-10-05). Both draw the shared hierarchy contract
// (play_hierarchy.go); each read lists the nodes under a focus path with
// their totals, shares and own values, which is the question either
// picture is asked, and the losses the build counted. A pin is the pane's
// own: select_treemap_node and select_icicle_frame set it and publish its
// label as selection_key together, and the pane's click goes through them.

const (
	opGetTreemap         = "get_treemap"
	opSetTreemapOptions  = "set_treemap_options"
	opSelectTreemapNode  = "select_treemap_node"
	opGetIcicle          = "get_icicle"
	opSetIcicleOptions   = "set_icicle_options"
	opSelectIcicleFrame  = "select_icicle_frame"
	opsResTreemap        = treemapPaneId
	opsResIcicle         = iciclePaneId
	treemapPaneId        = "treemap"
	iciclePaneId         = "icicle"
	hierReadDefaultLimit = 50
	hierReadMaxLimit     = 200
	// hierPathLabelMaxBytes cuts one path element a read quotes. A cut
	// label still names its node: the selects compare it with the node's
	// label cut the same way.
	hierPathLabelMaxBytes = 128
)

// HierLosses is what a hierarchy build dropped, demoted or cut.
type HierLosses struct {
	WithoutPath  int64 `json:",omitzero" desc:"rows with no usable path (stack) or id (nodes)"`
	WithoutValue int64 `json:",omitzero" desc:"rows whose value was missing, negative or not finite"`
	DuplicateIds int64 `json:",omitzero" desc:"node rows repeating an id; the first was kept"`
	Reparented   int64 `json:",omitzero" desc:"node rows whose parent names no row, drawn as roots"`
	PathsCut     int64 `json:",omitzero" desc:"paths cut at the depth cap"`
	Capped       bool  `json:",omitzero" desc:"the tree reached the 20,000-node cap; deeper values were attributed to their ancestors"`
	PastCap      int64 `json:",omitzero" desc:"rows the cap dropped outright"`
}

func hierLossesOf(st hierStats) HierLosses {
	return HierLosses{WithoutPath: int64(st.droppedPath), WithoutValue: int64(st.droppedValue),
		DuplicateIds: int64(st.droppedDup), Reparented: int64(st.reparented), PathsCut: int64(st.truncated),
		Capped: st.capped, PastCap: int64(st.droppedCapped)}
}

// HierNodeReading is one node of a treemap.
type HierNodeReading struct {
	Path      []string `desc:"labels from the result's root down to the node, each cut at 128 bytes; give it back to select or focus"`
	Total     *float64 `desc:"the node's value with everything below it"`
	Share     *float64 `json:",omitzero" desc:"total as a fraction of the whole tree"`
	Own       *float64 `json:",omitzero" desc:"a container's own value, apart from its children's"`
	Children  int32    `json:",omitzero" desc:"direct children; 0 for a leaf"`
	Colour    *float64 `json:",omitzero" desc:"the numeric colour the cell is drawn with"`
	Category  string   `json:",omitzero" desc:"the category the cell is drawn with"`
	Inherited bool     `json:",omitzero" desc:"the colour or category came from the node's descendants, not from a row"`
}

// TreemapColourReading is what the colour channel resolved to.
type TreemapColourReading struct {
	Column     string   `desc:"none (no color column), numeric or categorical"`
	Min        *float64 `json:",omitzero" desc:"the numeric ramp's low end"`
	Max        *float64 `json:",omitzero" desc:"the numeric ramp's high end"`
	Declared   bool     `json:",omitzero" desc:"the range came from color_min and color_max rather than from the result"`
	Unit       string   `json:",omitzero" desc:"color_unit"`
	Spread     string   `json:",omitzero" desc:"where the cells' colours sit along the ramp, as the pane's legend line"`
	Categories []string `json:",omitzero" desc:"the categories in the order hues were given out, at most 24"`
	Wrapped    int32    `json:",omitzero" desc:"categories past the palette, sharing a hue with an earlier one"`
	Inherited  int32    `json:",omitzero" desc:"nodes coloured from their descendants"`
	Mixed      int32    `json:",omitzero" desc:"containers whose descendants disagree, drawn by depth"`
}

// TreemapReading is get_treemap's result.
type TreemapReading struct {
	Drawn    PaneDraw              `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Input    string                `json:",omitzero" desc:"folded (a stack array per row) or nodes (id, parent, value)"`
	Cells    int64                 `json:",omitzero" desc:"nodes in the tree"`
	Total    *float64              `json:",omitzero" desc:"the whole tree's value"`
	Unit     string                `json:",omitzero" desc:"the value's unit"`
	Losses   HierLosses            `desc:"what the build dropped or cut"`
	ColourBy string                `desc:"what the fill encodes: value or category (the color column), or depth"`
	Show     string                `desc:"how deep the cells nest: drill, 3 deep, 4 deep or full"`
	Colour   *TreemapColourReading `json:",omitzero" desc:"the colour channel"`
	Pinned   string                `json:",omitzero" desc:"the pinned leaf's label, which selection_key carries"`
	Drilled  []string              `json:",omitzero" desc:"the container the person drilled into, as a path; absent at the top"`
	Focus    *HierNodeReading      `json:",omitzero" desc:"the node the list is of: the focus asked for, else the drilled container, else the top"`
	Nodes    []HierNodeReading     `json:",omitzero" desc:"the focus's children, largest total first"`
	More     int32                 `json:",omitzero" desc:"children past the ones listed; raise limit or focus deeper"`
}

// GetTreemapArgs is get_treemap's argument.
type GetTreemapArgs struct {
	Focus []string `json:",omitzero" desc:"the path of a container whose children to list; empty lists the drilled container's, or the top's"`
	Limit int32    `json:",omitzero" desc:"children to list, 50 by default and at most 200"`
}

// SetTreemapOptionsArgs is set_treemap_options' argument.
type SetTreemapOptionsArgs struct {
	ColourBy *string `json:",omitzero" desc:"value or category (whichever the color column is), or depth"`
	Show     *string `json:",omitzero" desc:"drill, 3 deep, 4 deep or full"`
}

// SelectTreemapNodeArgs is select_treemap_node's argument.
type SelectTreemapNodeArgs struct {
	Path  []string `json:",omitzero" desc:"the leaf's path, as get_treemap gives it"`
	Label string   `json:",omitzero" desc:"or the leaf's label; the pin and selection_key are a label"`
	Clear bool     `json:",omitzero" desc:"drop the pin; selection_key becomes empty"`
}

// treemapOpsView is what get_treemap reads: the driver's tree, shared
// (every slice and node is replaced by a rebuild, never edited), and the
// settings as they stand.
type treemapOpsView struct {
	root      *layout.Node
	synthetic bool
	idxOf     map[*layout.Node]int32
	tree      hierTree
	effNum    []float64
	effKey    []string
	color     treemapColorInfo
	spread    string
	stats     hierStats
	inherited int
	mixed     int
	colorMode treemapColorModeE
	nesting   treemapNestingE
	selected  string
	drilled   []string
}

var treemapNestingNames = []string{"drill", "3 deep", "4 deep", "full"}

func (n treemapNestingE) name() string {
	if int(n) < len(treemapNestingNames) {
		return treemapNestingNames[n]
	}
	return ""
}

func hierColorKindName(k hierColorKindE) string {
	switch k {
	case hierColorNumeric:
		return "numeric"
	case hierColorCategorical:
		return "categorical"
	}
	return "none"
}

// colourByName is what the fill encodes, as the colour bar names it.
func (inst *treemapDriver) colourByName() string {
	if inst.color.kind == hierColorNone || inst.colorMode == treemapColorDepth {
		return "depth"
	}
	return treemapColorDataLabel(inst.color.kind)
}

// treemapSynthetic reports whether root is the container a forest is
// wrapped in, which no row describes.
func treemapSynthetic(root *layout.Node, idxOf map[*layout.Node]int32) bool {
	if root == nil {
		return false
	}
	_, known := idxOf[root]
	return !known
}

// opsView copies what get_treemap reads.
func (inst *treemapDriver) opsView() (v treemapOpsView) {
	v = treemapOpsView{root: inst.root, idxOf: inst.idxOf, tree: inst.tree, effNum: inst.effColorNum,
		effKey: inst.effColorKey, color: inst.color, spread: inst.quantileLine(), stats: inst.stats,
		inherited: inst.inherited, mixed: inst.mixed, colorMode: inst.colorMode, nesting: inst.nesting,
		selected: inst.selected}
	v.synthetic = treemapSynthetic(inst.root, inst.idxOf)
	if inst.tm != nil && inst.root != nil {
		// The breadcrumb runs from the root to the drilled container; a
		// forest's container is no part of a path.
		crumb := inst.tm.Breadcrumb()
		if len(crumb) > 1 {
			if v.synthetic {
				crumb = crumb[1:]
			}
			for _, n := range crumb {
				v.drilled = append(v.drilled, hierPathLabel(n.Name))
			}
		}
	}
	return
}

// hierPathLabel is one path element as a read quotes it.
func hierPathLabel(s string) string { return truncateBytes(s, hierPathLabelMaxBytes) }

// hierLabelNames reports whether a label given back by a caller names a
// node's label: equal, or equal to it cut as reads cut it.
func hierLabelNames(given, label string) bool {
	return given == label || given == hierPathLabel(label)
}

// hierMatchLabel is the one of n labels given names: an exact match wins,
// and a cut label counts only when no other label cuts to the same text,
// so a short id never stands for a longer one whose cut equals it.
// ambiguous is the number of cut matches when there is no single one.
func hierMatchLabel(given string, n int, label func(i int) string) (at int, ambiguous int) {
	at = -1
	cut := -1
	for i := 0; i < n; i++ {
		l := label(i)
		if l == given {
			return i, 0
		}
		if given == hierPathLabel(l) {
			if cut < 0 {
				cut = i
			}
			ambiguous++
		}
	}
	if ambiguous == 1 {
		return cut, 0
	}
	return -1, ambiguous
}

// treemapTops is the result's own roots: the root, or a forest's roots.
func treemapTops(root *layout.Node, synthetic bool) []*layout.Node {
	if root == nil {
		return nil
	}
	if synthetic {
		return root.Children
	}
	return []*layout.Node{root}
}

// treemapResolve walks path from the result's roots. An empty path is the
// root itself, the forest container included.
func treemapResolve(root *layout.Node, synthetic bool, path []string) (n *layout.Node, ok bool) {
	if len(path) == 0 {
		return root, root != nil
	}
	level := treemapTops(root, synthetic)
	for _, name := range path {
		n = nil
		for _, c := range level {
			if hierLabelNames(name, c.Name) {
				n = c
				break
			}
		}
		if n == nil {
			return nil, false
		}
		level = n.Children
	}
	return n, true
}

// treemapLeafByLabel finds a node by its label, depth first; leaf reports
// whether the first one found has no children.
func treemapLeafByLabel(root *layout.Node, label string) (n *layout.Node, found bool) {
	var container *layout.Node
	stack := []*layout.Node{root}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == nil {
			continue
		}
		if hierLabelNames(label, cur.Name) {
			if len(cur.Children) == 0 {
				return cur, true
			}
			if container == nil {
				container = cur
			}
		}
		stack = append(stack, cur.Children...)
	}
	return container, container != nil
}

// pinFor is the label a select pins, checked against the tree drawn.
func (inst *treemapDriver) pinFor(in SelectTreemapNodeArgs) (label string, err error) {
	if in.Clear {
		if len(in.Path) > 0 || in.Label != "" {
			return "", app.RefuseOperation("clear drops the pin; give it alone, or give a path or a label without it")
		}
		return "", nil
	}
	if inst.root == nil {
		return "", app.RefuseOperation("the treemap has no cells to pin: run a hierarchy result, and show_pane treemap")
	}
	var n *layout.Node
	switch {
	case len(in.Path) > 0:
		var ok bool
		if n, ok = treemapResolve(inst.root, treemapSynthetic(inst.root, inst.idxOf), in.Path); !ok {
			return "", app.RefuseOperation("no node at " + strconv.Quote(strings.Join(in.Path, " › ")) + "; get_treemap lists the paths")
		}
	case in.Label != "":
		var ok bool
		if n, ok = treemapLeafByLabel(inst.root, in.Label); !ok {
			return "", app.RefuseOperation("no cell is labelled " + strconv.Quote(in.Label))
		}
	default:
		return "", app.RefuseOperation("name the leaf to pin by its path or its label, or clear the pin")
	}
	if len(n.Children) > 0 {
		return "", app.RefuseOperation(strconv.Quote(n.Name) + " is a container; a click on it drills, and only a leaf pins")
	}
	return n.Name, nil
}

// selectTreemapNode is select_treemap_node: the pin and selection_key
// together.
func (inst *PlayApp) selectTreemapNode(in SelectTreemapNodeArgs, writer string) (err error) {
	label, err := inst.treemapDriver.pinFor(in)
	if err != nil {
		return
	}
	inst.treemapDriver.selected = label
	inst.graph.setSignalRawFrom(signalSelectionKey, label, writer)
	return
}

// setOptions is set_treemap_options on the driver.
func (inst *treemapDriver) setOptions(in SetTreemapOptionsArgs) (err error) {
	if in.ColourBy == nil && in.Show == nil {
		return noOptionsRefusal(treemapPaneId, "colour_by", "show")
	}
	mode := inst.colorMode
	if in.ColourBy != nil {
		switch name := strings.TrimSpace(*in.ColourBy); {
		case name == "depth":
			mode = treemapColorDepth
		case inst.color.kind == hierColorNone:
			return app.RefuseOperation("the result has no `color` column, so the cells are coloured by depth only")
		case name == treemapColorDataLabel(inst.color.kind):
			mode = treemapColorData
		default:
			return app.RefuseOperation("colour_by is " + treemapColorDataLabel(inst.color.kind) + " or depth for this result")
		}
	}
	nest := inst.nesting
	if in.Show != nil {
		i := slices.Index(treemapNestingNames, strings.TrimSpace(*in.Show))
		if i < 0 {
			return app.RefuseOperation("show is one of " + strings.Join(treemapNestingNames, ", "))
		}
		nest = treemapNestingE(i)
	}
	inst.colorMode, inst.nesting = mode, nest
	return
}

// treemapView is the Treemap's view for the snapshot.
func (inst *PlayApp) treemapView() treemapOpsView { return inst.treemapDriver.opsView() }

// nodeReading reads one node of the shared tree.
func (v *treemapOpsView) nodeReading(n *layout.Node, path []string, whole float64) (r HierNodeReading) {
	total := n.TotalSize()
	r = HierNodeReading{Path: path, Total: finite(total), Children: int32(len(n.Children))}
	if whole > 0 {
		r.Share = finite(total / whole)
	}
	if len(n.Children) > 0 && n.Size > 0 {
		r.Own = finite(n.Size)
	}
	i, known := v.idxOf[n]
	if !known {
		return
	}
	own := false
	switch v.tree.ColorKind {
	case hierColorNumeric:
		if int(i) < len(v.effNum) {
			r.Colour = finite(v.effNum[i])
		}
		own = int(i) < len(v.tree.ColorNum) && finite(v.tree.ColorNum[i]) != nil
		r.Inherited = r.Colour != nil && !own
	case hierColorCategorical:
		if int(i) < len(v.effKey) {
			r.Category = opsLabel(v.effKey[i])
		}
		own = int(i) < len(v.tree.ColorKey) && v.tree.ColorKey[i] != ""
		r.Inherited = r.Category != "" && !own
	}
	return
}

// hierLimit is a read's limit, defaulted and capped.
func hierLimit(l int32) int {
	if l <= 0 {
		return hierReadDefaultLimit
	}
	return min(int(l), hierReadMaxLimit)
}

// treemapReading is get_treemap.
func treemapReading(sn *opsSnap, in GetTreemapArgs) (out TreemapReading, err error) {
	d, readable, err := paneDrawOf(sn, treemapPaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.treemap
	out = TreemapReading{Drawn: d, ColourBy: "depth", Show: v.nesting.name(), Pinned: v.selected}
	if v.color.kind != hierColorNone && v.colorMode == treemapColorData {
		out.ColourBy = treemapColorDataLabel(v.color.kind)
	}
	if !readable || v.root == nil {
		return
	}
	out.Input, out.Cells, out.Unit = v.stats.mode.String(), int64(v.stats.nodes), v.stats.unit
	out.Losses = hierLossesOf(v.stats)
	whole := v.root.TotalSize()
	out.Total = finite(whole)
	if v.color.kind != hierColorNone {
		c := &TreemapColourReading{Column: hierColorKindName(v.color.kind), Unit: v.color.unit,
			Inherited: int32(v.inherited), Mixed: int32(v.mixed)}
		if v.color.kind == hierColorNumeric {
			c.Min, c.Max, c.Declared, c.Spread = finite(v.color.min), finite(v.color.max), v.color.declared, v.spread
		} else {
			n := min(len(v.color.catOrder), 24)
			for _, k := range v.color.catOrder[:n] {
				c.Categories = append(c.Categories, opsLabel(k))
			}
			c.Wrapped = int32(v.color.wrapped)
		}
		out.Colour = c
	}
	out.Drilled = v.drilled
	focusPath := in.Focus
	if len(focusPath) == 0 {
		focusPath = v.drilled
	}
	focus, ok := treemapResolve(v.root, v.synthetic, focusPath)
	if !ok {
		return out, app.RefuseOperation("no node at " + strconv.Quote(strings.Join(focusPath, " › ")) + "; focus takes a path the reading lists")
	}
	path := slices.Clone(focusPath)
	if len(path) == 0 && !v.synthetic {
		path = []string{hierPathLabel(focus.Name)}
	}
	fr := v.nodeReading(focus, path, whole)
	out.Focus = &fr
	children := focus.Children
	type sized struct {
		n     *layout.Node
		total float64
	}
	all := make([]sized, 0, len(children))
	for _, c := range children {
		all = append(all, sized{c, c.TotalSize()})
	}
	slices.SortStableFunc(all, func(a, b sized) int { return cmp.Compare(b.total, a.total) })
	limit, used := hierLimit(in.Limit), 0
	for i, c := range all {
		cp := append(slices.Clone(path), hierPathLabel(c.n.Name))
		used += 96
		for _, l := range cp {
			used += len(l)
		}
		if i >= limit || used > opsSampleMaxBytes {
			out.More = int32(len(all) - i)
			break
		}
		out.Nodes = append(out.Nodes, v.nodeReading(c.n, cp, whole))
	}
	return
}

// treemapOptionsDigest is the treemap resource: the colour and nesting
// switches and the pin.
func treemapOptionsDigest(p *PlayApp) string {
	d := p.treemapDriver
	if d == nil {
		return ""
	}
	return "colour=" + strconv.Itoa(int(d.colorMode)) + "|show=" + strconv.Itoa(int(d.nesting)) + "|pin=" + d.selected
}

// ---- Icicle ----

// IcicleFrameReading is one frame of the icicle.
type IcicleFrameReading struct {
	Path  []string `desc:"labels from the root frame down to this one, each cut at 128 bytes; give it back to select or focus"`
	Frame int32    `desc:"the frame's number in this layout; select_icicle_frame takes it beside the path when sibling labels agree"`
	Total *float64 `desc:"the frame's value with everything below it"`
	Self  *float64 `desc:"the frame's own value, which no child covers"`
	Share *float64 `json:",omitzero" desc:"total as a fraction of the whole"`
	Depth int32    `desc:"0 for a root frame"`
}

// IcicleReading is get_icicle's result.
type IcicleReading struct {
	Drawn       PaneDraw             `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Input       string               `json:",omitzero" desc:"folded (a stack array per row) or nodes (id, parent, value)"`
	Frames      int64                `json:",omitzero" desc:"frames laid out"`
	Depth       int32                `json:",omitzero" desc:"rows of frames"`
	Total       *float64             `json:",omitzero" desc:"the whole width's value"`
	Unit        string               `json:",omitzero" desc:"the value's unit"`
	Pruned      int64                `json:",omitzero" desc:"frames prune left out"`
	PrunedValue *float64             `json:",omitzero" desc:"the value of the frames prune left out"`
	Losses      HierLosses           `desc:"what the build dropped or cut"`
	Orientation string               `desc:"icicle (roots on top) or flame (roots at the bottom)"`
	Order       string               `desc:"sibling order: value, name or input"`
	ColourBy    string               `desc:"branch, label or depth"`
	Prune       string               `desc:"off, 0.1% or 1%: frames under that share of the total are left out"`
	Labels      bool                 `desc:"whether frames carry their labels"`
	Pinned      []string             `json:",omitzero" desc:"the pinned frame's path; selection_key carries its label"`
	By          string               `json:",omitzero" desc:"total: the focus's children by total; self: every frame under the focus by its own value"`
	Focus       *IcicleFrameReading  `json:",omitzero" desc:"the frame the list is under; absent at the top"`
	List        []IcicleFrameReading `json:",omitzero" desc:"the frames, largest first"`
	More        int32                `json:",omitzero" desc:"frames past the ones listed"`
}

// GetIcicleArgs is get_icicle's argument.
type GetIcicleArgs struct {
	Focus []string `json:",omitzero" desc:"the path of a frame to list under; empty lists from the top"`
	By    string   `json:",omitzero" desc:"total (default): the focus's children by total; self: every frame under the focus by its own value, the hot frames of a profile"`
	Limit int32    `json:",omitzero" desc:"frames to list, 50 by default and at most 200"`
}

// SetIcicleOptionsArgs is set_icicle_options' argument.
type SetIcicleOptionsArgs struct {
	Orientation *string `json:",omitzero" desc:"icicle or flame"`
	Order       *string `json:",omitzero" desc:"value, name or input"`
	ColourBy    *string `json:",omitzero" desc:"branch, label or depth"`
	Prune       *string `json:",omitzero" desc:"off, 0.1% or 1%"`
	Labels      *bool   `json:",omitzero" desc:"draw the frames' labels"`
}

// SelectIcicleFrameArgs is select_icicle_frame's argument.
type SelectIcicleFrameArgs struct {
	Path  []string `json:",omitzero" desc:"the frame's path, as get_icicle gives it"`
	Frame *int32   `json:",omitzero" desc:"the frame's number, as get_icicle gives it, to tell apart siblings whose labels agree; checked against path when both are given"`
	Label string   `json:",omitzero" desc:"or the frame's label, when one frame carries it"`
	Clear bool     `json:",omitzero" desc:"drop the pin; selection_key becomes empty"`
}

var (
	icicleOrientNames = []string{"icicle", "flame"}
	icicleOrderNames  = []string{"value", "name", "input"}
	icicleColourNames = []string{"branch", "label", "depth"}
	iciclePruneNames  = []string{"off", "0.1%", "1%"}
)

// icicleOpsView is what get_icicle reads: the layout, shared (Compute
// returns a new one and nothing edits it), and the settings.
type icicleOpsView struct {
	layout     *icicle.Layout
	stats      hierStats
	orient     icicle.OrientationE
	order      icicle.OrderE
	colorBy    icicleview.ColorModeE
	prune      iciclePruneE
	hideLabels bool
	pinned     int32
}

func nameAt(names []string, i int) string {
	if i >= 0 && i < len(names) {
		return names[i]
	}
	return ""
}

func (inst *IcicleDriver) opsView() icicleOpsView {
	v := icicleOpsView{layout: inst.layout, stats: inst.stats, orient: inst.orient, order: inst.order,
		colorBy: inst.colorBy, prune: inst.prune, hideLabels: inst.hideLabels, pinned: -1}
	if inst.layout != nil && !inst.selected.None() && int(inst.selected.Node) < len(inst.layout.Nodes) {
		v.pinned = inst.selected.Node
	}
	return v
}

// icicleView is the Icicle's view for the snapshot.
func (inst *PlayApp) icicleView() icicleOpsView { return inst.icicleDriver.opsView() }

// icicleFrameLabels is a frame's path as reads give it.
func icicleFrameLabels(lay *icicle.Layout, i int) (path []string) {
	if lay == nil {
		return nil
	}
	for _, j := range lay.PathTo(i) {
		path = append(path, hierPathLabel(lay.Nodes[j].Label))
	}
	return
}

// icicleFrameByPath walks path down the layout's pre-order: a frame's
// children follow it, before the next frame at its depth or above.
func icicleFrameByPath(lay *icicle.Layout, path []string) (at int, ok bool) {
	if lay == nil || len(path) == 0 {
		return -1, false
	}
	parent := int32(-1)
	at = -1
	for _, name := range path {
		found := -1
		for j := at + 1; j < len(lay.Nodes); j++ {
			n := &lay.Nodes[j]
			if parent >= 0 && n.Depth <= lay.Nodes[parent].Depth {
				break
			}
			if n.Parent == parent && hierLabelNames(name, n.Label) {
				found = j
				break
			}
		}
		if found < 0 {
			return -1, false
		}
		at, parent = found, int32(found)
	}
	return at, true
}

// icicleSubtree is the pre-order range of frame i and everything below it.
func icicleSubtree(lay *icicle.Layout, i int) (lo, hi int) {
	lo, hi = i, i+1
	for hi < len(lay.Nodes) && lay.Nodes[hi].Depth > lay.Nodes[i].Depth {
		hi++
	}
	return
}

// pinFor is the frame a select pins, checked against the layout drawn.
func (inst *IcicleDriver) pinFor(in SelectIcicleFrameArgs) (hit icicleview.Hit, err error) {
	if in.Clear {
		if len(in.Path) > 0 || in.Frame != nil || in.Label != "" {
			err = app.RefuseOperation("clear drops the pin; give it alone, or give a path or a label without it")
		}
		return
	}
	lay := inst.layout
	if lay == nil {
		err = app.RefuseOperation("the icicle has no frames laid out to pin: run a hierarchy result, and show_pane icicle")
		return
	}
	switch {
	case in.Frame != nil:
		i := int(*in.Frame)
		if i < 0 || i >= len(lay.Nodes) {
			err = app.RefuseOperation("no frame " + strconv.Itoa(i) + " in the layout drawn; get_icicle lists the frames")
			return
		}
		if len(in.Path) > 0 && !slices.Equal(in.Path, icicleFrameLabels(lay, i)) {
			err = app.ConflictOperation("frame " + strconv.Itoa(i) + " is no longer at " + strconv.Quote(strings.Join(in.Path, " › ")) + "; the layout changed, read get_icicle again")
			return
		}
		return icicleview.NodeHit(i), nil
	case len(in.Path) > 0:
		i, ok := icicleFrameByPath(lay, in.Path)
		if !ok {
			err = app.RefuseOperation("no frame at " + strconv.Quote(strings.Join(in.Path, " › ")) + "; get_icicle lists the paths")
			return
		}
		return icicleview.NodeHit(i), nil
	case in.Label != "":
		found, count := -1, 0
		for j := range lay.Nodes {
			if hierLabelNames(in.Label, lay.Nodes[j].Label) {
				if found < 0 {
					found = j
				}
				count++
			}
		}
		switch {
		case count == 0:
			err = app.RefuseOperation("no frame is labelled " + strconv.Quote(in.Label) + "; prune may have left it out")
		case count > 1:
			err = app.RefuseOperation(strconv.Itoa(count) + " frames are labelled " + strconv.Quote(in.Label) + "; name one by its path")
		default:
			hit = icicleview.NodeHit(found)
		}
		return
	}
	err = app.RefuseOperation("name the frame to pin by its path or its label, or clear the pin")
	return
}

// selectIcicleFrame is select_icicle_frame: the pin and selection_key
// together. The click's zoom stays the click's: the value axis is the
// plot's own camera.
func (inst *PlayApp) selectIcicleFrame(in SelectIcicleFrameArgs, writer string) (err error) {
	d := inst.icicleDriver
	hit, err := d.pinFor(in)
	if err != nil {
		return
	}
	d.selected = hit
	inst.graph.setSignalRawFrom(signalSelectionKey, d.selectedLabel(), writer)
	return
}

// setOptions is set_icicle_options on the driver; a geometry change
// re-lays the tree out on the next frame, keeping the pin by its path.
func (inst *IcicleDriver) setOptions(in SetIcicleOptionsArgs) (err error) {
	if in.Orientation == nil && in.Order == nil && in.ColourBy == nil && in.Prune == nil && in.Labels == nil {
		return noOptionsRefusal(iciclePaneId, "orientation", "order", "colour_by", "prune", "labels")
	}
	pick := func(p *string, names []string, what string) (i int, err error) {
		if i = slices.Index(names, strings.TrimSpace(*p)); i < 0 {
			err = app.RefuseOperation(what + " is one of " + strings.Join(names, ", "))
		}
		return
	}
	orient, order, colorBy, prune := inst.orient, inst.order, inst.colorBy, inst.prune
	if in.Orientation != nil {
		i, e := pick(in.Orientation, icicleOrientNames, "orientation")
		if e != nil {
			return e
		}
		orient = icicle.OrientationE(i)
	}
	if in.Order != nil {
		i, e := pick(in.Order, icicleOrderNames, "order")
		if e != nil {
			return e
		}
		order = icicle.OrderE(i)
	}
	if in.ColourBy != nil {
		i, e := pick(in.ColourBy, icicleColourNames, "colour_by")
		if e != nil {
			return e
		}
		colorBy = icicleview.ColorModeE(i)
	}
	if in.Prune != nil {
		i, e := pick(in.Prune, iciclePruneNames, "prune")
		if e != nil {
			return e
		}
		prune = iciclePruneE(i)
	}
	inst.orient, inst.order, inst.colorBy, inst.prune = orient, order, colorBy, prune
	if in.Labels != nil {
		inst.hideLabels = !*in.Labels
	}
	return
}

func (v *icicleOpsView) frameReading(i int) IcicleFrameReading {
	n := &v.layout.Nodes[i]
	r := IcicleFrameReading{Path: icicleFrameLabels(v.layout, i), Frame: int32(i), Total: finite(n.Total), Self: finite(n.Self), Depth: n.Depth}
	if t := v.layout.Report.Total; t > 0 {
		r.Share = finite(n.Total / t)
	}
	return r
}

// icicleReading is get_icicle.
func icicleReading(sn *opsSnap, in GetIcicleArgs) (out IcicleReading, err error) {
	d, readable, err := paneDrawOf(sn, iciclePaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.icicle
	out = IcicleReading{Drawn: d, Orientation: nameAt(icicleOrientNames, int(v.orient)),
		Order: nameAt(icicleOrderNames, int(v.order)), ColourBy: nameAt(icicleColourNames, int(v.colorBy)),
		Prune: nameAt(iciclePruneNames, int(v.prune)), Labels: !v.hideLabels}
	by := strings.TrimSpace(in.By)
	if by == "" {
		by = "total"
	}
	if by != "total" && by != "self" {
		return out, app.RefuseOperation("by is total or self")
	}
	lay := v.layout
	if !readable || lay == nil {
		return
	}
	if v.pinned >= 0 {
		out.Pinned = icicleFrameLabels(lay, int(v.pinned))
	}
	out.Input, out.Unit, out.Losses = v.stats.mode.String(), v.stats.unit, hierLossesOf(v.stats)
	out.Frames, out.Depth, out.Total = int64(len(lay.Nodes)), int32(lay.Report.Rows), finite(lay.Report.Total)
	if lay.Report.Pruned > 0 {
		out.Pruned, out.PrunedValue = int64(lay.Report.Pruned), finite(lay.Report.PrunedValue)
	}
	out.By = by
	// The candidates: the roots, or the focus's subtree.
	lo, hi, parent := 0, len(lay.Nodes), int32(-1)
	if len(in.Focus) > 0 {
		f, ok := icicleFrameByPath(lay, in.Focus)
		if !ok {
			return out, app.RefuseOperation("no frame at " + strconv.Quote(strings.Join(in.Focus, " › ")) + "; focus takes a path the reading lists")
		}
		fr := v.frameReading(f)
		out.Focus = &fr
		lo, hi = icicleSubtree(lay, f)
		lo++
		parent = int32(f)
	}
	var picks []int
	for j := lo; j < hi; j++ {
		if by == "self" || lay.Nodes[j].Parent == parent {
			picks = append(picks, j)
		}
	}
	key := func(j int) float64 {
		if by == "self" {
			return lay.Nodes[j].Self
		}
		return lay.Nodes[j].Total
	}
	slices.SortStableFunc(picks, func(a, b int) int { return cmp.Compare(key(b), key(a)) })
	limit, used := hierLimit(in.Limit), 0
	for i, j := range picks {
		r := v.frameReading(j)
		used += 96
		for _, l := range r.Path {
			used += len(l)
		}
		if i >= limit || used > opsSampleMaxBytes {
			out.More = int32(len(picks) - i)
			break
		}
		out.List = append(out.List, r)
	}
	return
}

// icicleOptionsDigest is the icicle resource: the four switches, the
// labels box and the pin.
func icicleOptionsDigest(p *PlayApp) string {
	d := p.icicleDriver
	if d == nil {
		return ""
	}
	pin := ""
	if !d.selected.None() {
		pin = strconv.Itoa(int(d.selected.Node))
	}
	return "o=" + strconv.Itoa(int(d.orient)) + "|ord=" + strconv.Itoa(int(d.order)) + "|c=" + strconv.Itoa(int(d.colorBy)) +
		"|p=" + strconv.Itoa(int(d.prune)) + "|hide=" + strconv.FormatBool(d.hideLabels) + "|pin=" + pin
}

func addHierarchyOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[TreemapReading, GetTreemapArgs, SetTreemapOptionsArgs]{
		pane:     treemapPaneId,
		resource: "the Treemap pane's settings: the colour and nesting switches and the pinned leaf",
		digest:   treemapOptionsDigest,
		get:      opGetTreemap,
		getSummary: "read what the Treemap pane last drew: the whole and the children of a container with their totals, " +
			"shares, own values and colours, the colour channel, the pinned leaf, the drill, and what the build dropped",
		set:        opSetTreemapOptions,
		setSummary: "set what the Treemap's fill encodes (value or category, or depth) and how deep its cells nest",
		gesture:    "the colour and show switches above the treemap",
		follows:    []string{"the pane draws with the new settings from its next frame; the result is not rerun"},
		read:       treemapReading,
		apply:      func(p *PlayApp, in SetTreemapOptionsArgs) error { return p.treemapDriver.setOptions(in) },
	})
	appops.Command(s, app.OperationSpec{Name: opSelectTreemapNode, Version: 1,
		Summary: "pin a leaf of the Treemap, or drop the pin; its label becomes selection_key",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResSignals, opsResTreemap}, Agents: true,
		Gesture: "clicking a leaf of the treemap",
		Follows: []string{"Live reruns a query that reads selection_key", "a new result drops the pin and empties selection_key"}},
		func(inst *PlayLauncher, call app.OperationCall, in SelectTreemapNodeArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if err := p.selectTreemapNode(in, paneSignalWriter(call, treemapPaneId)); err != nil {
				return appops.None{}, err
			}
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
	addPaneOps(s, paneOpsSpec[IcicleReading, GetIcicleArgs, SetIcicleOptionsArgs]{
		pane:     iciclePaneId,
		resource: "the Icicle pane's settings: orientation, order, colour, prune, labels and the pinned frame",
		digest:   icicleOptionsDigest,
		get:      opGetIcicle,
		getSummary: "read what the Icicle pane last laid out: the frames under a path by total, or every frame by its own value, " +
			"the total, what prune left out, the settings, the pinned frame, and what the build dropped",
		set:        opSetIcicleOptions,
		setSummary: "set the Icicle's orientation, sibling order, colouring, prune threshold or labels",
		gesture:    "the switches and the hide labels box above the icicle",
		follows: []string{"the pane draws with the new settings from its next frame; the result is not rerun",
			"prune changes the frames and the pruned value get_icicle reports"},
		read:  icicleReading,
		apply: func(p *PlayApp, in SetIcicleOptionsArgs) error { return p.icicleDriver.setOptions(in) },
	})
	appops.Command(s, app.OperationSpec{Name: opSelectIcicleFrame, Version: 1,
		Summary: "pin a frame of the Icicle, or drop the pin; its label becomes selection_key",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResSignals, opsResIcicle}, Agents: true,
		Gesture: "clicking a frame of the icicle",
		Follows: []string{"Live reruns a query that reads selection_key",
			"the pin does not zoom the value axis, which a click does",
			"a new result drops the pin and empties selection_key"}},
		func(inst *PlayLauncher, call app.OperationCall, in SelectIcicleFrameArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if err := p.selectIcicleFrame(in, paneSignalWriter(call, iciclePaneId)); err != nil {
				return appops.None{}, err
			}
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
}
