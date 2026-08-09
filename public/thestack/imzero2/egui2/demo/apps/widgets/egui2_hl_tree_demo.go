package widgets

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "tree",
		Category: "Layout & widgets",
		Title:    icons.IconTreeStructure + " tree outline",
		Stage:    [2]float32{1024, 700},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "widgets/tree over a biological taxonomy: a columnar (Labels + Parents) hierarchy rendered as a virtualised outline. " +
			"Expansion, selection and the keyboard cursor are Go state the host owns — so expand-all, collapse-all and reveal-a-node are ordinary calls, " +
			"and the rows carry a host column beside the name. Click a row to select it, ctrl-click to add, the arrow or a double-click to fold.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newTreeDemoState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoTree(ids, state.(*treeDemoState))
		},
		SourceFunc: demoTree,
	})
}

// =============================================================================
// tree outline — widgets/tree showcase + DX example (ADR-0176 M4).
//
// Themed on biological taxonomy so the data is abstract (no filesystem
// implications) and genuinely hierarchical: Animalia down to species across
// three phyla.
//
// What the demo is trying to show, in the order the sections appear:
//
//   - a hierarchy arrives as two parallel columns, not a pointer tree, and a
//     producer that *has* a pointer tree flattens it once (buildTaxonTree);
//   - expansion and selection live in a caller-owned State, so "expand all",
//     "collapse all" and "reveal this node" are calls rather than wishes —
//     which is the thing the egui_ltreeview binding this replaced could not do
//     at all, its expansion living in Rust;
//   - a row is not just a label: the host column carries a species count.
// =============================================================================

// taxon is the nested source fixture. It is deliberately NOT what the widget
// takes — see buildTaxonTree, which flattens it once at init. Hierarchies in
// this repo arrive flat (a recursive CTE's rows, a profile's stacks), and the
// columnar input is the shape that serves them; a producer holding a pointer
// tree pays one walk.
type taxon struct {
	Name     string
	Children []taxon
}

// treeDemoState is the per-window view state. The tree.State is the whole of
// it — there is no shadow copy of what is open, because the widget reads and
// writes this one.
type treeDemoState struct {
	st tree.State
	// revealTarget is the node the "reveal" button jumps to: a species buried
	// two phyla and four ranks away from anything on screen once everything is
	// collapsed.
	revealTarget int32
	lastAction   string
}

func newTreeDemoState() (st *treeDemoState) {
	st = &treeDemoState{revealTarget: taxonNodeOf("Danaus plexippus"), lastAction: "(nothing yet)"}
	// Open the first two ranks so the demo lands on something legible rather
	// than on three collapsed roots.
	st.st.SetExpanded(0, true)
	for i := range taxonTree.Len() {
		if taxonTree.Parents[i] == 0 {
			st.st.SetExpanded(int32(i), true)
		}
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
											{Name: "Acinonyx jubatus"},
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

	// The columnar form the widget takes, plus the per-node species count the
	// host column shows. Built once: the fixture is immutable, and a demo that
	// rebuilt its hierarchy every frame would be showing the wrong lesson.
	taxonTree   tree.Tree
	taxonLeaves []int
)

func init() {
	taxonTree, taxonLeaves = buildTaxonTree(swAnimalKingdom)
}

// buildTaxonTree walks the nested fixture once into [tree.Tree]'s two columns,
// and counts each node's species (leaf descendants) on the way back up.
//
// This is the whole of "how do I get my data in": append a label, append the
// parent's index, recurse. Parents may appear in any order and several roots
// are allowed, so a forest needs no invented root.
func buildTaxonTree(root taxon) (t tree.Tree, leaves []int) {
	var walk func(parent int32, tx taxon) (node int32, n int)
	walk = func(parent int32, tx taxon) (node int32, n int) {
		node = int32(len(t.Labels))
		t.Labels = append(t.Labels, tx.Name)
		t.Parents = append(t.Parents, parent)
		// A taxon name is unique in this fixture, so it doubles as the key.
		// This demo never rebuilds and would work without one — the column is
		// here because a host that DOES rebuild needs it, and a demo that
		// leaves it out reads as though it were optional in general.
		t.Keys = append(t.Keys, tx.Name)
		leaves = append(leaves, 0)
		if len(tx.Children) == 0 {
			leaves[node] = 1
			return node, 1
		}
		for _, child := range tx.Children {
			_, cn := walk(node, child)
			n += cn
		}
		leaves[node] = n
		return node, n
	}
	walk(-1, root)
	return t, leaves
}

// taxonNodeOf resolves a species name to its node index, or -1. Only used to
// pin the reveal target at construction; names are unique in this fixture.
func taxonNodeOf(name string) int32 {
	for i, l := range taxonTree.Labels {
		if l == name {
			return int32(i)
		}
	}
	return -1
}

// -----------------------------------------------------------------------------
// Top-level demo
// -----------------------------------------------------------------------------

func demoTree(ids *c.WidgetIdStack, st *treeDemoState) {
	treeControlsSection(ids, st)
	treeOutlineSection(ids, st)
	treeReadoutSection(st)
}

// -----------------------------------------------------------------------------
// Controls — the point of host-owned state
// -----------------------------------------------------------------------------

// treeControlsSection drives the outline from code. Every button here is one
// call on the State the host already holds; none of it was expressible against
// a tree whose expansion lived on the other side of the FFI.
func treeControlsSection(ids *c.WidgetIdStack, st *treeDemoState) {
	stdSection("driving the outline from Go",
		"expansion, selection and the reveal request are all fields of a caller-owned tree.State")

	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("tv-expand"), c.Atoms().Text("expand all").Keep()).
			SendResp().HasPrimaryClicked() {
			st.st.ExpandAll()
			st.lastAction = "expand all"
		}
		c.AddSpace(gapInline())
		if c.Button(ids.PrepareStr("tv-collapse"), c.Atoms().Text("collapse all").Keep()).
			SendResp().HasPrimaryClicked() {
			st.st.CollapseAll()
			st.lastAction = "collapse all"
		}
		c.AddSpace(gapInline())
		// Reveal is the one that needs the widget's help: opening the
		// ancestors has to happen before the frame's flatten, and the scroll
		// needs the row index that same flatten assigns, so it is a request
		// the render consumes rather than two calls made here.
		if c.Button(ids.PrepareStr("tv-reveal"), c.Atoms().Text("reveal Danaus plexippus").Keep()).
			SendResp().HasPrimaryClicked() {
			st.st.Reveal(st.revealTarget)
			st.st.SelectOnly(st.revealTarget)
			st.lastAction = "reveal Danaus plexippus"
		}
		c.AddSpace(gapInline())
		if c.Button(ids.PrepareStr("tv-clear"), c.Atoms().Text("clear selection").Keep()).
			SendResp().HasPrimaryClicked() {
			st.st.ClearSelection()
			st.lastAction = "clear selection"
		}
	}
	c.AddSpace(padInner())
}

// -----------------------------------------------------------------------------
// The outline
// -----------------------------------------------------------------------------

func treeOutlineSection(ids *c.WidgetIdStack, st *treeDemoState) {
	stdSection("Animalia — six ranks, three phyla",
		"one row per visible node; collapsed subtrees and off-screen rows build nothing at all")

	res := tree.Render(tree.Input{
		Ids:      ids,
		ScopeKey: "taxa",
		Tree:     taxonTree,
		State:    &st.st,
		Outline: tree.Column{
			Header:    "taxon",
			Width:     420,
			Resizable: true,
		},
		Columns: []tree.Column{{
			Header: "species",
			Width:  90,
			Cell:   treeSpeciesCell,
		}},
		MaxHeight: 300,
		Striped:   true,
	})
	if res.Err != nil {
		return
	}
	if res.Toggled >= 0 {
		st.lastAction = "toggled " + taxonTree.Labels[res.Toggled]
	}
	if res.Clicked >= 0 {
		st.lastAction = "clicked " + taxonTree.Labels[res.Clicked]
	}
	if res.Activated >= 0 {
		st.lastAction = "activated " + taxonTree.Labels[res.Activated]
	}
	c.AddSpace(padInner())
	c.Label(fmt.Sprintf("%d nodes in the fixture, %d rows on screen",
		taxonTree.Len(), len(res.Rows))).Send()
}

// treeSpeciesCell draws the host column: how many species sit under this node.
// Blank on a species itself, where the answer is "it is one" and repeating it
// down every leaf row would be noise.
func treeSpeciesCell(r tree.Row) {
	node := r.Node
	if taxonLeaves[node] <= 1 {
		return
	}
	c.Label(strconv.Itoa(taxonLeaves[node])).Selectable(false).Truncate().Send()
}

// -----------------------------------------------------------------------------
// Selection readout
// -----------------------------------------------------------------------------

// treeReadoutSection prints what is selected. It is also what the ADR-0176 M4
// headless scene asserts against: a driver cannot read a tree row directly
// (egui gives a Label no accessible name, only a value), so the scene watches
// this line change instead.
func treeReadoutSection(st *treeDemoState) {
	stdSection("selection readout", "ctrl-click adds to the selection; shift-click extends from the last click")

	sel := st.st.Selection(nil)
	names := make([]string, 0, len(sel))
	for _, n := range sel {
		names = append(names, taxonTree.Labels[n])
	}
	// Sorted because the selection is a set and map order is arbitrary — an
	// unsorted readout would flicker between frames with nothing having
	// changed, which is exactly what a driver waiting on this text cannot
	// tolerate.
	sort.Strings(names)
	text := "(nothing)"
	if len(names) > 0 {
		text = fmt.Sprintf("%v", names)
	}
	c.Label("selected: " + text).Send()
	c.Label("last action: " + st.lastAction).Send()
}
