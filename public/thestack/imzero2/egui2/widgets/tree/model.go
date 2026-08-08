// Package tree renders a hierarchy as an outline: one row per visible node,
// indented by depth, with a disclosure control on any node that has children
// (ADR-0176).
//
// The package is UI-free at this level. This file declares the input model,
// its validation, and the caller-owned view state; layout.go flattens a
// (Tree, State) pair into the row sequence a renderer draws, and answers
// nothing about pixels. The split mirrors icicle (ADR-0160) and sankey
// (ADR-0159): everything worth testing is decided before a binding is
// imported.
//
// # Why not egui_ltreeview
//
// imzero2 had a tree binding before this one, and nothing adopted it. Its node
// commands went into a process-global register drained by a separate Tree()
// call, so emission was decoupled from placement and two trees in one frame
// had to alternate; expansion state lived in Rust, so Go re-marshalled every
// node every frame even when the whole tree was collapsed; and a row could
// carry a label and nothing else. See ADR-0176 for the full accounting.
package tree

import "fmt"

// Tree is the input hierarchy in columnar form: two parallel slices, one entry
// per node. It is deliberately not a pointer tree — the data this widget
// renders arrives flat (a recursive query's rows, a schema's fields, a
// profile's stacks), and demanding a pointer tree would make every producer
// build one first (ADR-0176 SD1). It is the same shape [icicle.Tree] and
// play's hierarchy contract already use, minus the value column a tree has no
// use for.
type Tree struct {
	// Labels is what the renderer draws for each node.
	Labels []string
	// Parents indexes each node's parent, or -1 for a root. Several roots are
	// allowed and are laid out one after another at depth 0 — a forest is the
	// natural shape of a schema's tables or a filesystem's mount points, and
	// inventing a virtual root would state a containment nothing in the data
	// supports.
	//
	// No ordering is required: a parent may appear after its child. Siblings
	// are drawn in the order they appear here.
	Parents []int32
}

// Len is the node count, which every column shares.
func (t Tree) Len() int { return len(t.Labels) }

// Validate reports the first structural problem with t, or nil. [Flatten]
// calls it, so calling it separately is only useful to check input before
// laying out.
//
// An empty tree is valid and flattens to no rows. A tree widget with nothing
// in it is an ordinary state — an unfiltered search, a schema with no tables —
// and making the host special-case it would put an `if len(...) == 0` in front
// of every call site.
func (t Tree) Validate() error {
	n := len(t.Labels)
	if len(t.Parents) != n {
		return fmt.Errorf("tree: column lengths disagree: %d labels, %d parents", n, len(t.Parents))
	}
	for i := range n {
		p := t.Parents[i]
		if p == int32(i) {
			return fmt.Errorf("tree: node %d is its own parent", i)
		}
		if p < -1 || int(p) >= n {
			return fmt.Errorf("tree: node %d has parent %d, which is out of range [-1,%d)", i, p, n)
		}
	}
	return t.checkAcyclic()
}

// checkAcyclic walks every node to a root, memoizing the nodes already known
// to reach one. Each node is entered at most twice across the whole run, so
// this stays linear; the walk length is bounded by the node count, which is
// what catches a cycle without a separate colour array.
//
// Lifted from [icicle.Tree] deliberately rather than shared: the two packages
// agree on the column shape by convention, not by a common type, and a shared
// helper would couple a flamegraph's release to an outline's.
func (t Tree) checkAcyclic() error {
	n := len(t.Parents)
	grounded := make([]bool, n)
	path := make([]int32, 0, 16)
	for i := range n {
		if grounded[i] {
			continue
		}
		path = path[:0]
		v := int32(i)
		for v != -1 && !grounded[v] {
			if len(path) > n {
				return fmt.Errorf("tree: node %d lies on a parent cycle", i)
			}
			path = append(path, v)
			v = t.Parents[v]
		}
		for _, w := range path {
			grounded[w] = true
		}
	}
	return nil
}

// State is the view state the host owns: which nodes are open, which are
// selected, and where the keyboard cursor sits. The widget reads and mutates
// it; it keeps no hidden per-frame state of its own and leaves no authority in
// the renderer (ADR-0176 SD2).
//
// Host-owned is what makes expansion persistable, restorable and settable from
// code — the single largest thing the egui_ltreeview binding could not do,
// because its expansion lived in Rust and Go could only observe it one frame
// late.
//
// # Node indices are the identity, and that has a cost
//
// Every field keys on a node's index in [Tree.Labels]. That is the only
// identity a columnar input has, and it is stable exactly as long as the
// host's node ordering is. A host that rebuilds its Tree with the same shape
// each frame — the usual case, and what all four in-repo callers do — is fine.
// A host that reorders or reloads should reset the State, or remap it against
// whatever stable key its own data has (a path, a row id); the widget cannot
// do that remapping because it has no key column to do it with.
//
// The zero value is usable: nothing expanded, nothing selected, no cursor.
type State struct {
	// expanded holds the open interior nodes. A node absent from the map is
	// collapsed, so the zero value starts fully collapsed and a host that
	// wants otherwise calls [State.ExpandAll] or expands a path itself.
	expanded map[int32]bool
	// selected holds the selected nodes. A set rather than a single index
	// because multi-select is the ordinary case for a tree (copy these three
	// fields, delete these two rows) and retrofitting it would change every
	// caller's read.
	selected map[int32]bool
	// cursorP1 is the keyboard cursor, stored as node+1 so that the zero
	// value means "no cursor" rather than "node 0". Read it through
	// [State.Cursor], which converts back and reports -1 for none.
	//
	// The cursor is deliberately separate from the selection, and carried from
	// M1 even though nothing moves it until ADR-0177 lands: the row the
	// keyboard is on is not the row that is selected, or Shift+Down cannot
	// extend a range and Down cannot pass over a selected row without changing
	// what is selected. Every file manager makes this distinction; adding it
	// later would reshape State and every caller written against the old one.
	//
	// Pointer selection does move it: a plain or ctrl click puts it on the row
	// clicked, and a shift click leaves it on the anchor it extended from.
	cursorP1 int32
	// revealP1 is a pending [State.Reveal], stored node+1 for the same reason
	// as cursorP1. The renderer consumes it.
	revealP1 int32
	// rows is the flatten scratch the renderer reuses frame to frame. It sits
	// here because State is already the per-instance object the host retains,
	// and a row slice reallocated on every frame is the one allocation a
	// widget that exists to virtualise cannot justify. Nothing outside the
	// package reads it — the renderer hands back the same slice, borrowed.
	rows []Row
}

// IsExpanded reports whether node is open. Leaves are never open; the
// renderer asks this only of nodes that have children.
func (s *State) IsExpanded(node int32) bool { return s.expanded[node] }

// SetExpanded opens or closes node. Expanding a leaf is harmless and has no
// effect on the flattened rows, so a host driving expansion from a search
// result does not have to check for children first.
func (s *State) SetExpanded(node int32, open bool) {
	if !open {
		delete(s.expanded, node)
		return
	}
	if s.expanded == nil {
		s.expanded = make(map[int32]bool, 16)
	}
	s.expanded[node] = true
}

// ToggleExpanded flips node's open state and returns the new one.
func (s *State) ToggleExpanded(node int32) (open bool) {
	open = !s.expanded[node]
	s.SetExpanded(node, open)
	return
}

// ExpandAll opens every node in t, including leaves — see [State.SetExpanded]
// on why that is harmless. Cheap enough to call on a whole tree; it is the
// usual response to "expand all" and to a filter that should reveal its hits.
func (s *State) ExpandAll(t Tree) {
	if s.expanded == nil {
		s.expanded = make(map[int32]bool, t.Len())
	}
	for i := range t.Len() {
		s.expanded[int32(i)] = true
	}
}

// CollapseAll closes every node, leaving only the roots visible.
func (s *State) CollapseAll() { clear(s.expanded) }

// ExpandAncestors opens every ancestor of node so that node itself becomes
// visible, without touching node's own open state. This is what "reveal this
// row" means — after a search hit, or restoring a selection — and doing it by
// hand needs the parent walk the host would rather not write.
func (s *State) ExpandAncestors(t Tree, node int32) {
	if node < 0 || int(node) >= t.Len() {
		return
	}
	// Bounded by the node count: Validate rejects cycles, but a host may call
	// this before validating, and an unbounded walk on a cyclic tree would
	// hang rather than misdraw.
	for i, p := 0, t.Parents[node]; p != -1 && i <= t.Len(); i++ {
		s.SetExpanded(p, true)
		p = t.Parents[p]
	}
}

// IsSelected reports whether node is selected.
func (s *State) IsSelected(node int32) bool { return s.selected[node] }

// SetSelected adds or removes node from the selection.
func (s *State) SetSelected(node int32, on bool) {
	if !on {
		delete(s.selected, node)
		return
	}
	if s.selected == nil {
		s.selected = make(map[int32]bool, 8)
	}
	s.selected[node] = true
}

// SelectOnly replaces the whole selection with node — the plain-click
// behaviour, as against the ctrl-click [State.SetSelected] models.
func (s *State) SelectOnly(node int32) {
	clear(s.selected)
	s.SetSelected(node, true)
}

// ClearSelection deselects everything.
func (s *State) ClearSelection() { clear(s.selected) }

// SelectionLen is how many nodes are selected.
func (s *State) SelectionLen() int { return len(s.selected) }

// Selection appends the selected node indices to dst and returns it. Order is
// map order and therefore arbitrary — sort it if the host shows the selection
// as a list. Appending to a caller slice keeps a per-frame read allocation-free.
func (s *State) Selection(dst []int32) []int32 {
	for n := range s.selected {
		dst = append(dst, n)
	}
	return dst
}

// Cursor is the node the keyboard is on, or -1 for none.
func (s *State) Cursor() int32 { return s.cursorP1 - 1 }

// SetCursor moves the keyboard cursor; any negative node clears it. It does
// not change the selection — see the cursorP1 field's documentation for why
// the two are separate.
func (s *State) SetCursor(node int32) {
	if node < 0 {
		s.cursorP1 = 0
		return
	}
	s.cursorP1 = node + 1
}

// Reveal asks the next render to bring node into view: open its ancestors and
// scroll its row to the middle of the viewport. It is a one-shot request the
// render consumes, not a persistent setting, so a host sets it on an event —
// a search hit, a restored selection, a jump from another panel — and does not
// have to remember to clear it.
//
// A request rather than a pair of calls the host makes itself because both
// halves are the renderer's: the ancestors must open *before* the frame's
// flatten, and the scroll needs the row index that same flatten produces. A
// negative node cancels a pending reveal.
//
// Only one reveal can be pending; a second call replaces the first. Revealing
// a node that is not in the tree opens nothing and scrolls nowhere.
func (s *State) Reveal(node int32) {
	if node < 0 {
		s.revealP1 = 0
		return
	}
	s.revealP1 = node + 1
}

// takeReveal returns the pending reveal target and clears it, or -1.
func (s *State) takeReveal() (node int32) {
	node = s.revealP1 - 1
	s.revealP1 = 0
	return
}
