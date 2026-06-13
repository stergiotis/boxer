package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

func init() {
	registry.Register(registry.Demo{
		Name:        "tree-view",
		Category:    "Layout & widgets",
		Title:       icons.IconTreeStructure + " tree view",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindMixed,
		Description: "NodeDir / NodeLeaf showcase on biological taxonomy: a hand-coded Carnivora subtree plus a recursive renderer driven by a Go data fixture covering Animalia.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newTreeViewDemoState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoTreeView(ids, state.(*treeViewDemoState))
		},
		SourceFunc: demoTreeView,
	})
}

// =============================================================================
// Tree view — NodeDir / NodeLeaf showcase + DX example.
//
// Two sections, both themed on biological taxonomy so the data is abstract
// (no filesystem implications):
//
//   - hand-coded section: explicit NodeDir / NodeLeaf calls for a small
//     subtree (Carnivora). Shows exactly what each level looks like.
//   - data-driven section: a recursive renderer over a `taxon` struct
//     fixture covering Animalia three phyla deep. Shows the realistic
//     pattern for plugging real hierarchical data into the widgets.
//
// Both share the selection readout — clicking a leaf updates the displayed
// "selected: …" line.
// =============================================================================

// taxon is the recursion fixture for the data-driven section. A nil/empty
// Children slice means leaf; otherwise the node renders as a NodeDir.
type taxon struct {
	Name     string
	Children []taxon
}

// treeViewDemoState holds the per-window selection strings the
// hand-coded and recursive trees each write back into. The animalia
// fixture stays package-level because it's immutable read-only data.
type treeViewDemoState struct {
	selHand string
	selRec  string
}

func newTreeViewDemoState() (st *treeViewDemoState) {
	st = &treeViewDemoState{
		selHand: "(none)",
		selRec:  "(none)",
	}
	return
}

var (
	// Animalia fixture — enough to span six taxonomic ranks across three
	// phyla without becoming a wall of text.
	swAnimalKingdom = taxon{
		Name: "Animalia",
		Children: []taxon{
			{
				Name: "Chordata",
				Children: []taxon{
					{
						Name: "Mammalia",
						Children: []taxon{
							{
								Name: "Carnivora",
								Children: []taxon{
									{
										Name: "Felidae",
										Children: []taxon{
											{Name: "Panthera leo"},
											{Name: "Panthera tigris"},
											{Name: "Panthera onca"},
											{Name: "Panthera pardus"},
										},
									},
									{
										Name: "Canidae",
										Children: []taxon{
											{Name: "Canis lupus"},
											{Name: "Canis latrans"},
											{Name: "Vulpes vulpes"},
										},
									},
								},
							},
							{
								Name: "Cetacea",
								Children: []taxon{
									{
										Name: "Delphinidae",
										Children: []taxon{
											{Name: "Tursiops truncatus"},
											{Name: "Orcinus orca"},
										},
									},
								},
							},
						},
					},
					{
						Name: "Aves",
						Children: []taxon{
							{
								Name: "Accipitriformes",
								Children: []taxon{
									{
										Name: "Accipitridae",
										Children: []taxon{
											{Name: "Aquila chrysaetos"},
											{Name: "Haliaeetus leucocephalus"},
										},
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "Arthropoda",
				Children: []taxon{
					{
						Name: "Insecta",
						Children: []taxon{
							{
								Name: "Coleoptera",
								Children: []taxon{
									{
										Name: "Lucanidae",
										Children: []taxon{
											{Name: "Lucanus cervus"},
										},
									},
								},
							},
							{
								Name: "Lepidoptera",
								Children: []taxon{
									{
										Name: "Nymphalidae",
										Children: []taxon{
											{Name: "Danaus plexippus"},
										},
									},
								},
							},
						},
					},
				},
			},
			{
				Name: "Mollusca",
				Children: []taxon{
					{
						Name: "Cephalopoda",
						Children: []taxon{
							{Name: "Octopus vulgaris"},
							{Name: "Sepia officinalis"},
						},
					},
				},
			},
		},
	}
)

// -----------------------------------------------------------------------------
// Top-level demo
// -----------------------------------------------------------------------------

func demoTreeView(ids *c.WidgetIdStack, st *treeViewDemoState) {
	for range c.CollapsingHeader(ids.PrepareStr("tv-hand"),
		c.WidgetText().Text("hand-coded — explicit NodeDir / NodeLeaf calls").Keep()).
		DefaultOpen(true).KeepIter() {
		swTreeHandSection(ids, st)
	}
	// Default closed so the screenshot tour stays under the ~694 px viewport
	// ceiling and still demonstrates the recursive pattern when expanded.
	for range c.CollapsingHeader(ids.PrepareStr("tv-rec"),
		c.WidgetText().Text("data-driven — recursive renderer over a Go fixture").Keep()).
		KeepIter() {
		swTreeRecursiveSection(ids, st)
	}
	for range c.CollapsingHeader(ids.PrepareStr("tv-readout"),
		c.WidgetText().Text("selection readout").Keep()).
		DefaultOpen(true).KeepIter() {
		swTreeReadoutSection(ids, st)
	}
}

// -----------------------------------------------------------------------------
// Hand-coded section — every node spelled out so the call shape is visible.
// -----------------------------------------------------------------------------

func swTreeHandSection(ids *c.WidgetIdStack, st *treeViewDemoState) {
	stdSection("subtree: Carnivora",
		"two families × multiple species, written out node by node — click a species to select it")

	for range c.NodeDir(ids.PrepareStr("h-carnivora"),
		c.WidgetText().Text("Carnivora").Keep()).SendIter() {
		for range c.NodeDir(ids.PrepareStr("h-felidae"),
			c.WidgetText().Text("Felidae").Keep()).SendIter() {
			for range c.NodeDir(ids.PrepareStr("h-panthera"),
				c.WidgetText().Text("Panthera").Keep()).SendIter() {
				swHandLeaf(ids, st, "h-leo", "Panthera leo")
				swHandLeaf(ids, st, "h-tigris", "Panthera tigris")
				swHandLeaf(ids, st, "h-onca", "Panthera onca")
				swHandLeaf(ids, st, "h-pardus", "Panthera pardus")
			}
			for range c.NodeDir(ids.PrepareStr("h-acinonyx"),
				c.WidgetText().Text("Acinonyx").Keep()).SendIter() {
				swHandLeaf(ids, st, "h-jubatus", "Acinonyx jubatus")
			}
		}
		for range c.NodeDir(ids.PrepareStr("h-canidae"),
			c.WidgetText().Text("Canidae").Keep()).SendIter() {
			for range c.NodeDir(ids.PrepareStr("h-canis"),
				c.WidgetText().Text("Canis").Keep()).SendIter() {
				swHandLeaf(ids, st, "h-lupus", "Canis lupus")
				swHandLeaf(ids, st, "h-latrans", "Canis latrans")
			}
			for range c.NodeDir(ids.PrepareStr("h-vulpes"),
				c.WidgetText().Text("Vulpes").Keep()).SendIter() {
				swHandLeaf(ids, st, "h-vulvul", "Vulpes vulpes")
			}
		}
	}
	// Tree() drains every NodeDir / NodeLeaf / NodeDirClose queued above and
	// renders them via egui_ltreeview. Without this drain the queue would
	// stay populated and the nodes would never appear. Wrap in a bounded
	// ScrollArea so the section keeps a predictable height and a second
	// tree below has room to render.
	for range c.ScrollArea().Vscroll(true).KeepIter() {
		c.UiSetMaxHeight(220)
		c.Tree(ids.PrepareStr("h-tree")).Send()
	}
}

func swHandLeaf(ids *c.WidgetIdStack, st *treeViewDemoState, idStr, name string) {
	if c.NodeLeaf(ids.PrepareStr(idStr),
		c.WidgetText().Text(name).Keep()).
		SendResp().HasNodelikeSelected() {
		st.selHand = name
	}
}

// -----------------------------------------------------------------------------
// Recursive section — drive NodeDir / NodeLeaf from a Go data structure.
// -----------------------------------------------------------------------------

func swTreeRecursiveSection(ids *c.WidgetIdStack, st *treeViewDemoState) {
	stdSection("Animalia — six ranks, three phyla, rendered by recursion",
		"renderTaxon dispatches NodeDir for inner nodes and NodeLeaf for species; click any species to select")

	renderTaxon(ids, st, swAnimalKingdom)
	// See swTreeHandSection — Tree() is the drain point that renders the
	// queued node commands. Each Tree() call empties the queue, so two
	// trees in the same demo each get their own subset of commands.
	for range c.ScrollArea().Vscroll(true).KeepIter() {
		c.UiSetMaxHeight(280)
		c.Tree(ids.PrepareStr("rec-tree")).Send()
	}
}

// renderTaxon is the canonical pattern for plugging hierarchical data into
// the Node widgets: leaves go through NodeLeaf with click-to-select, inner
// nodes open a SendIter scope and recurse over their children. The taxon
// name doubles as the stable widget id since species names are globally
// unique within Animalia — for non-unique names, thread a path or counter
// through the recursion instead.
func renderTaxon(ids *c.WidgetIdStack, st *treeViewDemoState, t taxon) {
	if len(t.Children) == 0 {
		if c.NodeLeaf(ids.PrepareStr("rec-"+t.Name),
			c.WidgetText().Text(t.Name).Keep()).
			SendResp().HasNodelikeSelected() {
			st.selRec = t.Name
		}
		return
	}
	for range c.NodeDir(ids.PrepareStr("rec-"+t.Name),
		c.WidgetText().Text(t.Name).Keep()).SendIter() {
		for _, child := range t.Children {
			renderTaxon(ids, st, child)
		}
	}
}

// -----------------------------------------------------------------------------
// Selection readout
// -----------------------------------------------------------------------------

func swTreeReadoutSection(ids *c.WidgetIdStack, st *treeViewDemoState) {
	for range c.Grid(ids.PrepareStr("tv-readout-grid")).NumColumns(2).KeepIter() {
		for rt := range c.RichTextLabel("hand-coded selection") {
			rt.Weak()
		}
		for rt := range c.RichTextLabel(st.selHand) {
			rt.Monospace()
		}
		c.EndRow()

		for rt := range c.RichTextLabel("recursive selection") {
			rt.Weak()
		}
		for rt := range c.RichTextLabel(st.selRec) {
			rt.Monospace()
		}
		c.EndRow()
	}
	c.AddSpace(padInner())
	c.Label(fmt.Sprintf("fixture size: %d species across the recursive tree",
		countLeaves(swAnimalKingdom))).Send()
}

func countLeaves(t taxon) (n int) {
	if len(t.Children) == 0 {
		return 1
	}
	for _, child := range t.Children {
		n += countLeaves(child)
	}
	return
}
