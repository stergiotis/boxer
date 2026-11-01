package trailviewer

import (
	"fmt"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// The tree tab (ADR-0350 §SD4, proposed): for a case whose rows hang off one
// another — a task, a conversation — the held rows as an outline over the
// parent-ref → ref edges. A row whose parent-ref is empty, or names no held
// row, is a root; the nodes are in time order, oldest first, so a parent's
// children read in the order they happened. Selection is the window's: a
// click on a node selects its row, and the selected row's node is selected
// and revealed.

// treeExpandAll is the node count up to which a tree opens fully expanded.
const treeExpandAll = 200

// treeHeightFallback sizes the tree before the pane probe answers.
const treeHeightFallback = 420.0

// trailTree is the held rows as a tree: the columnar input, the rows behind
// the nodes, and the node of each row id.
type trailTree struct {
	t      tree.Tree
	rows   []*Row
	byId   map[uint64]int32
	roots  int
	orphan int
}

// buildTree builds the tree of rows, which are newest first as the cache
// holds them. A parent cycle, which a well-formed trail does not hold, is
// broken by making the node that closes it a root.
func buildTree(rows []Row) (out trailTree) {
	n := len(rows)
	out.t = tree.Tree{Labels: make([]string, n), Parents: make([]int32, n), Keys: make([]string, n)}
	out.rows = make([]*Row, n)
	out.byId = make(map[uint64]int32, n)
	byRef := make(map[string]int32, n)
	for i := range n {
		r := &rows[n-1-i]
		out.rows[i] = r
		out.byId[r.Id] = int32(i)
		out.t.Labels[i] = r.Headline
		if out.t.Labels[i] == "" {
			out.t.Labels[i] = r.Kind
		}
		out.t.Keys[i] = strconv.FormatUint(r.Id, 10)
		if r.Ref != "" {
			if _, ok := byRef[r.Ref]; !ok {
				byRef[r.Ref] = int32(i)
			}
		}
	}
	for i, r := range out.rows {
		p := int32(-1)
		if r.ParentRef != "" {
			if j, ok := byRef[r.ParentRef]; ok && j != int32(i) {
				p = j
			} else if !ok {
				out.orphan++
			}
		}
		out.t.Parents[i] = p
	}
	// Break cycles: walk each node's ancestors; a walk longer than the node
	// count, or one that returns to the node, is a cycle.
	for i := range out.t.Parents {
		cur, steps := out.t.Parents[i], 0
		for cur >= 0 && steps <= n {
			if cur == int32(i) {
				out.t.Parents[i] = -1
				break
			}
			cur, steps = out.t.Parents[cur], steps+1
		}
	}
	for _, p := range out.t.Parents {
		if p < 0 {
			out.roots++
		}
	}
	return
}

// TreeNode is one node of get_tree's answer, in depth-first order.
type TreeNode struct {
	Id        uint64 `desc:"the row's facts id; pass to get_row or select_row"`
	Depth     int    `desc:"0 for a root, one more than its parent's otherwise"`
	Parent    uint64 `desc:"the parent row's facts id; 0 for a root"`
	Kind      string `desc:"the row's trail kind"`
	Headline  string `desc:"the row's one-line summary, cut at 512 characters"`
	Status    string `desc:"the row's status or outcome"`
	ElapsedMs uint64 `desc:"elapsed milliseconds where recorded"`
}

// depthFirst is every node of the tree in depth-first pre-order, as the tab
// draws it fully expanded.
func (inst *trailTree) depthFirst() (nodes []TreeNode, err error) {
	st := &tree.State{}
	st.SetDefaultExpanded(true)
	rows, err := tree.Flatten(inst.t, st, nil)
	if err != nil {
		return
	}
	nodes = make([]TreeNode, 0, len(rows))
	for _, fr := range rows {
		r := inst.rows[fr.Node]
		node := TreeNode{Id: r.Id, Depth: int(fr.Depth), Kind: r.Kind, Headline: cutText(r.Headline, opsMaxHeadline), Status: r.Status, ElapsedMs: r.ElapsedMs}
		if p := inst.t.Parents[fr.Node]; p >= 0 {
			node.Parent = inst.rows[p].Id
		}
		nodes = append(nodes, node)
	}
	return
}

// treeLine is the tree in words: nodes, roots, and how many rows name a
// parent the window does not hold.
func (inst *trailTree) line() string {
	s := fmt.Sprintf("%d nodes, %d roots", len(inst.rows), inst.roots)
	if inst.orphan > 0 {
		s += fmt.Sprintf("; %d rows name a parent the window does not hold and are roots here", inst.orphan)
	}
	return s
}

// treeView is the tab's state: the tree built for one fill and the
// widget's expansion and selection.
type treeView struct {
	built  bool
	gen    uint64
	tr     trailTree
	state  tree.State
	paneH  float32
	selId  uint64
	hasSel bool
}

// ensure rebuilds the tree when the cache's fill moves; expansion and
// selection survive by row id.
func (inst *treeView) ensure(rows []Row, gen uint64) *trailTree {
	if inst.built && inst.gen == gen {
		return &inst.tr
	}
	first := !inst.built
	inst.tr, inst.gen, inst.built = buildTree(rows), gen, true
	if first {
		inst.state.SetDefaultExpanded(len(rows) <= treeExpandAll)
	}
	inst.hasSel = false
	return &inst.tr
}

// renderTree is the tree tab: its line, Expand all and Collapse all, and the
// outline with headline, elapsed-ms, status and kind.
func (inst *App) renderTree() {
	if !inst.caseValue.Kind.chained() {
		c.Label("The tree is drawn for a task or a conversation case, whose rows hang off one another.").Wrap().Send()
		return
	}
	tv := &inst.treeV
	tr := tv.ensure(inst.cache.rows, inst.cache.gen)
	if len(tr.rows) == 0 {
		c.Label("(no rows to draw a tree of)").Send()
		return
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label(tr.line()).Send()
		c.AddSpace(styletokens.GapPanels(inst.density))
		if c.Button(inst.ids.PrepareStr("tree-expand"), c.Atoms().Text("Expand all").Keep()).SendResp().HasPrimaryClicked() {
			tv.state.ExpandAll()
		}
		if c.Button(inst.ids.PrepareStr("tree-collapse"), c.Atoms().Text("Collapse all").Keep()).SendResp().HasPrimaryClicked() {
			tv.state.CollapseAll()
		}
	}
	// The window's selection, projected onto the tree once per change.
	selId := inst.selectionId()
	if !tv.hasSel || tv.selId != selId {
		tv.selId, tv.hasSel = selId, true
		tv.state.ClearSelection()
		if node, ok := tr.byId[selId]; ok && selId != 0 {
			tv.state.SelectOnly(node)
			tv.state.ExpandAncestors(tr.t, node)
			tv.state.Reveal(node)
		}
	}
	if _, h, ok := c.CapturePaneSize(inst.ids.ProbeSeq("tree-pane")); ok && h > 80 {
		tv.paneH = h
	}
	maxH := tv.paneH
	if maxH <= 0 {
		maxH = treeHeightFallback
	}
	rows := tr.rows
	cell := func(f func(r *Row) string) func(r tree.Row) {
		return func(tr tree.Row) {
			c.Label(f(rows[tr.Node])).Selectable(false).Truncate().Send()
		}
	}
	res := tree.Render(tree.Input{
		Ids: inst.ids, ScopeKey: "trail-tree", Tree: tr.t, State: &tv.state, MaxHeight: maxH,
		Outline: tree.Column{Header: "headline", Width: 460, Resizable: true},
		Columns: []tree.Column{
			{Header: "elapsed-ms", Width: 90, Cell: cell(func(r *Row) string {
				if r.ElapsedMs == 0 {
					return ""
				}
				return strconv.FormatUint(r.ElapsedMs, 10)
			})},
			{Header: "status", Width: 100, Cell: cell(func(r *Row) string { return r.Status })},
			{Header: "kind", Width: 110, Cell: cell(func(r *Row) string { return r.Kind })},
		},
	})
	if res.Err != nil {
		c.Label(res.Err.Error()).Wrap().Send()
		return
	}
	if res.Clicked >= 0 && int(res.Clicked) < len(rows) {
		id := rows[res.Clicked].Id
		if inst.hasSelected && inst.selected.Id == id {
			return
		}
		tv.selId = id
		inst.selectGesture(id)
	}
}
