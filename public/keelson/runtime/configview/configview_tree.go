package configview

// configview_tree.go builds the inspector's hierarchy (ADR-0176 M3), the port
// from a per-category CollapsingHeader over hand-laid rows to the native tree
// widget. It keeps the two things the widget cannot know: the stable key each
// node's expansion is filed under, and which spec a var row shows.
//
// The reshape a fixed-height row forces. A var used to be two lines — the
// quick-scan signals on one, the description wrapped underneath. A tree row is
// one line, so the row became three columns: the name and its chips, the
// value, and the description. The description is no longer wrapped but
// truncated with the full text on hover, which is the trade; what it buys is
// that names, values and descriptions now line up down the pane instead of
// each row being its own ragged block.

import (
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// varNode is the per-node metadata carried beside the columnar tree. One entry
// per node, indexed the same way.
type varNode struct {
	// key identifies the node across frames — a category name for a section
	// row, a variable name for a var row. The tree widget keys its state on
	// node indices, and this inspector rebuilds its hierarchy on every filter
	// keystroke, so an index means a different node one frame to the next.
	key string
	// spec is the variable this row shows. Zero on a category row, which is
	// what isVar distinguishes.
	spec  env.Spec
	isVar bool
}

// buildTree rebuilds the hierarchy from the filtered, category-bucketed specs
// into the App's scratch slices. Category rows are roots in bucket order,
// which applyFilter has already sorted; each holds its variables.
//
// The category label carries the set/total count the CollapsingHeader title
// carried, so a collapsed section still says how much of it is configured.
func (inst *App) buildTree(buckets []bucket) {
	labels := inst.navLabels[:0]
	parents := inst.navParents[:0]
	nodes := inst.navNodes[:0]

	for _, b := range buckets {
		setCount := 0
		for _, s := range b.specs {
			if isSet(s) {
				setCount++
			}
		}
		head := int32(len(labels))
		labels = append(labels, categoryLabel(b.cat, setCount, len(b.specs)))
		parents = append(parents, -1)
		nodes = append(nodes, varNode{key: string(b.cat)})

		for _, s := range b.specs {
			labels = append(labels, s.Name)
			parents = append(parents, head)
			nodes = append(nodes, varNode{key: s.Name, spec: s, isVar: true})
		}
	}

	inst.navLabels, inst.navParents, inst.navNodes = labels, parents, nodes
}

// navTree is the columnar view of the last [App.buildTree], borrowed: valid
// until the next build.
func (inst *App) navTree() tree.Tree {
	return tree.Tree{Labels: inst.navLabels, Parents: inst.navParents}
}

// syncTree projects the App's own state onto the tree widget's. The App stays
// the authority because its keys — a category name, a variable name — survive
// the rebuild that every filter keystroke triggers, where a node index does
// not.
//
// Categories start closed, which is what the CollapsingHeader navigator did:
// the registry is long and an operator arrives looking for one variable.
// [App.expandedCat] pre-opens one, which is how the demo capture pins a scene
// without depending on persisted egui memory.
func (inst *App) syncTree() {
	sel := int32(-1)
	for i := range inst.navNodes {
		n, node := int32(i), &inst.navNodes[i]
		open := inst.expanded[node.key] ||
			(!node.isVar && inst.expandedCat != "" && node.key == string(inst.expandedCat))
		inst.navState.SetExpanded(n, open)
		if node.isVar && node.key == inst.selected {
			sel = n
		}
	}
	if sel < 0 {
		inst.navState.ClearSelection()
		return
	}
	inst.navState.SelectOnly(sel)
}

// applyTree writes a frame's tree interaction back onto the App's own state.
func (inst *App) applyTree(res tree.Result) {
	if n := res.Toggled; n >= 0 && int(n) < len(inst.navNodes) {
		inst.setExpanded(inst.navNodes[n].key, inst.navState.IsExpanded(n))
	}
	n := res.Clicked
	if n < 0 || int(n) >= len(inst.navNodes) {
		return
	}
	if inst.navNodes[n].isVar {
		inst.selected = inst.navNodes[n].key
		return
	}
	// A category row names a grouping and has nothing to show on its own, so
	// its click does the other thing a tree row can do — the way clicking a
	// CollapsingHeader's title did before. Suppressed when the widget already
	// toggled this row on the same frame, so a double-click nets two toggles
	// rather than three.
	if res.Toggled < 0 {
		inst.setExpanded(inst.navNodes[n].key, !inst.navState.IsExpanded(n))
	}
}

// setExpanded records a section's open state. Closed is the default, so a
// closed section is an absent entry rather than a false one.
func (inst *App) setExpanded(key string, open bool) {
	if !open {
		delete(inst.expanded, key)
		return
	}
	if inst.expanded == nil {
		inst.expanded = make(map[string]bool, 8)
	}
	inst.expanded[key] = true
}
