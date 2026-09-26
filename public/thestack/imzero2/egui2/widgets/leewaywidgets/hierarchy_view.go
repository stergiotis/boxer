package leewaywidgets

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle"
	icicleview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey"
	sankeyview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// HierarchyFormE is how a Hierarchy is drawn.
type HierarchyFormE uint8

const (
	HierarchyFormTreemap HierarchyFormE = iota
	HierarchyFormIcicle
	HierarchyFormSankey
)

// HierarchyOptions are the encoding choices a hierarchy is drawn under — the
// settings the Experiments hierarchy sink declares (ADR-0257, proposed, §SD2).
type HierarchyOptions struct {
	Form HierarchyFormE
	// Separator splits an entity's label into its path.
	Separator string
	// SizeByValue sizes a leaf by its values' sum; off, every entity counts 1.
	SizeByValue bool
	// MaxDepth folds deeper levels into their ancestor; zero is no limit.
	MaxDepth int
	// ColorByBranch gives each top-level subtree its own hue; off, colour
	// follows depth.
	ColorByBranch bool
}

// treemapChrome is the height the treemap draws above its container, its
// breadcrumb bar (play's Treemap tab budgets the same).
const treemapChrome float32 = 48

// HierarchyView draws a chart model as a hierarchy. It keeps the widget and
// the computed layout across frames and rebuilds them when the model or the
// options change: the treemap's drill state and the icicle's and sankey's
// layouts are expensive to redo and meaningless to keep across a new tree.
type HierarchyView struct {
	ids   *c.WidgetIdStack
	model *ChartModel
	opts  HierarchyOptions
	h     Hierarchy

	tm      *treemap.Treemap
	tmRoot  *layout.Node
	ice     *icicle.Layout
	iceView icicleview.Renderer
	reset   bool
	sk      *sankey.Layout
	skView  sankeyview.Renderer
	err     string
}

// NewHierarchyView returns a view drawing on ids.
func NewHierarchyView(ids *c.WidgetIdStack) *HierarchyView {
	return &HierarchyView{ids: ids}
}

// Hierarchy is the tree the view last built.
func (inst *HierarchyView) Hierarchy() Hierarchy { return inst.h }

func (inst *HierarchyView) rebuild(m *ChartModel, o HierarchyOptions) {
	inst.model, inst.opts, inst.err = m, o, ""
	inst.h = BuildHierarchy(m, o.Separator, o.SizeByValue, o.MaxDepth)
	switch o.Form {
	case HierarchyFormTreemap:
		inst.tmRoot = toLayoutNode(inst.h.Root, "all")
		coloring := treemap.DepthColoring(treemap.DefaultDepthColors)
		if o.ColorByBranch {
			coloring = treemap.AncestorHueColoring(inst.tmRoot, 0.55, 0.45, 0.62, 255)
		}
		if inst.tm == nil {
			inst.tm = treemap.New(inst.ids, "leeway-hierarchy-treemap", inst.tmRoot,
				treemap.WithStatusLine(false), treemap.WithMaxNestingDepth(0), treemap.WithColoring(coloring))
		} else {
			inst.tm.SetRoot(inst.tmRoot)
			inst.tm.SetColoring(coloring)
		}
	case HierarchyFormIcicle:
		lay, err := icicle.Compute(toIcicleTree(inst.h), icicle.Options{
			Orientation: icicle.OrientIcicle, Order: icicle.OrderValueDesc,
		})
		inst.ice, inst.reset = lay, true
		if err != nil {
			inst.err = err.Error()
		}
	case HierarchyFormSankey:
		lay, err := sankey.Compute(toSankey(inst.h), sankey.Options{Align: sankey.AlignLeft})
		inst.sk = lay
		if err != nil {
			inst.err = err.Error()
		}
	}
}

// Render draws the model in a w×h box.
func (inst *HierarchyView) Render(m *ChartModel, o HierarchyOptions, w, h float32) {
	if m == nil || m.Empty() {
		c.Label("The batch has no entities to arrange.").Send()
		return
	}
	if inst.model != m || inst.opts != o {
		inst.rebuild(m, o)
	}
	if inst.h.Leaves == 0 {
		c.Label("No entity has a positive weight to size it by.").Send()
		return
	}
	if inst.err != "" {
		c.Label("The hierarchy cannot be drawn in this form: " + inst.err).Send()
		return
	}
	switch o.Form {
	case HierarchyFormTreemap:
		inst.tm.SetContainerSize(w, max(h-treemapChrome, 120))
		inst.tm.Render()
	case HierarchyFormIcicle:
		col := icicleview.ColorByDepth
		if o.ColorByBranch {
			col = icicleview.ColorByBranch
		}
		_, _, _ = inst.iceView.Show(inst.ids, "##leewayicicle", w, h, inst.ice,
			icicleview.Opts{Color: col, ResetView: inst.reset})
		inst.reset = false
	case HierarchyFormSankey:
		_, _, _ = inst.skView.Show(inst.ids, "##leewaysankey", w, h, inst.sk, sankeyview.Opts{})
	}
}

// toLayoutNode is the treemap's tree: sizes on leaves only, since the
// hierarchy carries its weight at the leaves and a parent's own size would
// count twice.
func toLayoutNode(n *HierarchyNode, name string) *layout.Node {
	out := &layout.Node{Name: name}
	if len(n.Children) == 0 {
		out.Size = n.Weight
	}
	for _, ch := range n.Children {
		out.Children = append(out.Children, toLayoutNode(ch, ch.Name))
	}
	return out
}

// toIcicleTree is the icicle's parent-indexed tree, the root's children as
// its roots; a node's own value is its weight when it is a leaf.
func toIcicleTree(h Hierarchy) (t icicle.Tree) {
	var walk func(n *HierarchyNode, parent int32)
	walk = func(n *HierarchyNode, parent int32) {
		id := int32(len(t.Labels))
		t.Labels = append(t.Labels, n.Name)
		t.Parents = append(t.Parents, parent)
		self := 0.0
		if len(n.Children) == 0 {
			self = n.Weight
		}
		t.Self = append(t.Self, self)
		for _, ch := range n.Children {
			walk(ch, id)
		}
	}
	for _, ch := range h.Root.Children {
		walk(ch, -1)
	}
	return t
}

// toSankey draws the hierarchy as flows level by level: a node per tree node
// in the column of its depth, a link per parent-child edge carrying the
// child's weight, and a root, "all", whose flows into the top level are the
// first split. Without the root a top-level leaf has no link and a sankey
// draws it with no height. The alluvial mode keeps every level its own
// column, which a longest-path layout would not for a leaf above the deepest
// level.
func toSankey(h Hierarchy) (d sankey.Diagram) {
	const rootID = "\x00all"
	d.Mode = sankey.ModeAlluvial
	d.Nodes = append(d.Nodes, sankey.Node{ID: rootID, Label: "all", Stage: 0})
	// Order is the node's rank in a depth-first walk, not among its
	// siblings: a column holds the children of every parent in the column
	// before it, and ranking them by sibling index interleaves those families
	// and crosses every ribbon. Depth-first keeps each family together, in
	// its parent's place.
	order := 0
	var walk func(n *HierarchyNode, depth int)
	walk = func(n *HierarchyNode, depth int) {
		for _, ch := range n.Children {
			d.Nodes = append(d.Nodes, sankey.Node{ID: ch.Path, Label: ch.Name, Stage: depth, Order: float64(order)})
			order++
			walk(ch, depth+1)
		}
	}
	walk(h.Root, 1)
	for _, top := range h.Root.Children {
		if top.Weight > 0 {
			d.Links = append(d.Links, sankey.Link{Source: rootID, Target: top.Path, Value: top.Weight})
		}
	}
	for _, f := range h.Flows() {
		if f.Weight > 0 {
			d.Links = append(d.Links, sankey.Link{Source: f.From, Target: f.To, Value: f.Weight})
		}
	}
	return d
}
