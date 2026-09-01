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

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

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
	// Keys optionally identifies each node across rebuilds, and is what lets a
	// [State] outlive one. Leave it nil and the State files everything under
	// node indices, which are stable only as long as the host's ordering is —
	// for a host that filters or re-parses, one keystroke.
	//
	// A key is the host's own name for the thing the row shows: a section id,
	// a category name, an index path, a slug. It has to be unique, because it
	// IS the identity — two nodes sharing a key share one expansion entry and
	// one selection entry, and read as one node to everything in State. That
	// is not checked: the check is a map build of every key on every frame,
	// which is a per-frame cost for a host bug that shows up the first time
	// the two rows are opened.
	//
	// When present it must have one entry per node; [Tree.Validate] rejects a
	// short or long column rather than filing part of the tree by key and the
	// rest by index.
	Keys []string
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
		return eb.Build().Int("labels", n).Int("parents", len(t.Parents)).Errorf("column lengths disagree")
	}
	if t.Keys != nil && len(t.Keys) != n {
		return eb.Build().Int("labels", n).Int("keys", len(t.Keys)).Errorf("column lengths disagree")
	}
	for i := range n {
		p := t.Parents[i]
		if p == int32(i) {
			return eb.Build().Int("node", i).Errorf("node is its own parent")
		}
		if p < -1 || int(p) >= n {
			return eb.Build().Int("node", i).Int32("parent", p).Int("nodes", n).Errorf("node parent is out of range")
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
				return eb.Build().Int("node", i).Errorf("node lies on a parent cycle")
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
// # What a node is filed under
//
// Every entry is filed under a node's identity, and which identity that is
// depends on the input. A [Tree] carrying a Keys column files under the key; a
// Tree without one files under the node's index in [Tree.Labels], which is the
// only identity a columnar input has on its own and which is stable exactly as
// long as the host's node ordering is. For a host that filters or re-parses,
// that is one keystroke — so a State that has to survive a rebuild wants Keys,
// and every in-repo adopter supplies them.
//
// The binding is taken from the Tree that [Flatten] — and therefore [Render] —
// is called with. A host that rebuilds its Tree and then mutates the State by
// node index before the next render should call [State.Bind] first, or the
// write lands under the key the previous build gave that index.
//
// # A node with no record follows the default
//
// Expansion is three-valued: open, closed, or no record at all. A node with no
// record is drawn per [State.SetDefaultExpanded], which starts false — so the
// zero value is a fully collapsed tree, and a host that never touches the
// default sees exactly the behaviour it always had.
//
// The default is what a default-OPEN host needs, and it is not expressible
// without the third value: "absent means collapsed" makes "the reader closed
// this" and "this node is new" the same entry, so a section closed before a
// filter keystroke and a section that has never been seen cannot be told
// apart. Three of the four adopters are default-open, which is why it is here.
//
// The zero value is usable: nothing expanded, nothing selected, no cursor.
type State struct {
	// expanded / expandedKeys hold the nodes whose open state DIFFERS from
	// defaultExpanded — [State.SetExpanded] drops an entry that agrees with
	// it, so the map stays bounded by what the reader actually changed and a
	// later change of default still moves everything untouched.
	//
	// Only one of the pair is ever populated: which one is decided by the
	// bound Tree, not by the call, and a host does not mix the two modes.
	expanded     map[int32]bool
	expandedKeys map[string]bool
	// defaultExpanded is how a node with no record is drawn.
	defaultExpanded bool
	// selected / selectedKeys hold the selected nodes, on the same either-or
	// basis. A set rather than a single index because multi-select is the
	// ordinary case for a tree (copy these three fields, delete these two
	// rows) and retrofitting it would change every caller's read.
	selected     map[int32]bool
	selectedKeys map[string]bool
	// cursor is the keyboard cursor.
	//
	// It is deliberately separate from the selection, and carried from M1 even
	// though nothing moves it until ADR-0177 lands: the row the keyboard is on
	// is not the row that is selected, or Shift+Down cannot extend a range and
	// Down cannot pass over a selected row without changing what is selected.
	// Every file manager makes this distinction; adding it later would reshape
	// State and every caller written against the old one.
	//
	// Pointer selection does move it: a plain or ctrl click puts it on the row
	// clicked, and a shift click leaves it on the anchor it extended from.
	cursor nodeRef
	// keyFrameID is the id of the capture Frame the renderer wrapped the table
	// in last frame, and the key r26 captures are read back under. Kept here
	// rather than recomputed because captures are one frame late: the id has
	// to survive from the frame that registered it to the frame that reads it.
	// Zero until the tree has rendered once, which is what gates the key pass.
	keyFrameID uint64
	// lastVisibleRows is how many rows the etable drew last frame — what
	// PageUp and PageDown move by, so a page matches what the reader sees
	// rather than a guess.
	lastVisibleRows int
	// reveal is a pending [State.Reveal], which the renderer consumes.
	reveal nodeRef
	// keys is the identity column of the last-bound Tree, borrowed. Empty
	// means this State files by node index. It is re-read on every bind rather
	// than compared for identity: hosts rebuild their columns in place, so the
	// same backing array routinely holds a different tree.
	keys []string
	// rows is the flatten scratch the renderer reuses frame to frame. It sits
	// here because State is already the per-instance object the host retains,
	// and a row slice reallocated on every frame is the one allocation a
	// widget that exists to virtualise cannot justify. Nothing outside the
	// package reads it — the renderer hands back the same slice, borrowed.
	rows []Row
}

// nodeRef remembers one node — a cursor, a pending reveal — under whichever
// identity the State was bound to when it was set, so that it survives a
// rebuild exactly as far as the rest of the State does.
type nodeRef struct {
	// idxP1 is the node index plus one when the ref was taken unbound, so that
	// the zero value means "none" rather than "node 0".
	idxP1 int32
	// key is the node's key when it was taken bound, and keyed says which of
	// the two fields to read — an empty string is a usable key, so its absence
	// cannot stand for "none".
	key   string
	keyed bool
}

// Bind points the State at t's identity column, which is what the entries
// filed after it are keyed on. [Flatten] and [Render] do it themselves every
// frame; a host needs it only when it rebuilds its Tree and then writes to the
// State by node index before the next render, since until it is called an
// index still means whatever the previous build called it.
//
// The first bind of a State that has entries filed by index moves them over to
// the matching keys. Without that, a host which seeds a fresh State — expand
// this node, select that one — before its first render would write by index,
// and the entries would be invisible from the moment the binding arrived: a
// silent loss on the one frame a host is most likely to get this wrong. It is
// only ever the FIRST bind, because a bound State has nothing filed by index.
//
// It is otherwise cheap: it borrows t's Keys slice and reads nothing.
func (s *State) Bind(t Tree) {
	wasKeyed := s.keyed()
	s.keys = t.Keys
	if wasKeyed || !s.keyed() {
		return
	}
	s.rekeyByIndex(s.expanded, &s.expandedKeys)
	s.rekeyByIndex(s.selected, &s.selectedKeys)
	s.rekeyRef(&s.cursor)
	s.rekeyRef(&s.reveal)
}

// rekeyByIndex moves byIdx's entries into byKey under the freshly bound
// column, dropping any index the column does not reach.
func (s *State) rekeyByIndex(byIdx map[int32]bool, byKey *map[string]bool) {
	for node, v := range byIdx {
		if k, ok := s.key(node); ok {
			if *byKey == nil {
				*byKey = make(map[string]bool, len(byIdx))
			}
			(*byKey)[k] = v
		}
	}
	clear(byIdx)
}

// rekeyRef does the same for a single remembered node.
func (s *State) rekeyRef(r *nodeRef) {
	if r.keyed || r.idxP1 == 0 {
		return
	}
	s.refSet(r, r.idxP1-1)
}

// keyed reports whether this State files by key. An empty Keys column is not
// keyed: a host whose filter has emptied its tree has nothing to file, and the
// binding returns as soon as its nodes do.
func (s *State) keyed() bool { return len(s.keys) > 0 }

// key resolves node to the key it is filed under. ok is false when the State
// is unbound, and when node is outside the bound column — which is a stale
// index, and is dropped rather than filed somewhere arbitrary.
func (s *State) key(node int32) (k string, ok bool) {
	if !s.keyed() || node < 0 || int(node) >= len(s.keys) {
		return "", false
	}
	return s.keys[node], true
}

// indexOfKey is the reverse lookup, or -1 when the bound tree has no such
// node. Linear, and deliberately so: it is wanted by the cursor and by a
// pending reveal, both of which are read at most a few times per frame, where
// a map would cost a rebuild on every one of them.
func (s *State) indexOfKey(k string) (node int32) {
	for i := range s.keys {
		if s.keys[i] == k {
			return int32(i)
		}
	}
	return -1
}

// lookup reads node's entry from whichever of the two maps is in play.
// recorded distinguishes a stored false from no entry at all, which is what
// makes expansion three-valued.
func (s *State) lookup(byIdx map[int32]bool, byKey map[string]bool, node int32) (v bool, recorded bool) {
	if s.keyed() {
		k, ok := s.key(node)
		if !ok {
			return false, false
		}
		v, recorded = byKey[k]
		return
	}
	v, recorded = byIdx[node]
	return
}

// record stores node's entry, creating the map on first use. The maps are
// taken by pointer because that first use has to be visible on the State.
func (s *State) record(byIdx *map[int32]bool, byKey *map[string]bool, node int32, v bool) {
	if s.keyed() {
		k, ok := s.key(node)
		if !ok {
			return
		}
		if *byKey == nil {
			*byKey = make(map[string]bool, 16)
		}
		(*byKey)[k] = v
		return
	}
	if node < 0 {
		return
	}
	if *byIdx == nil {
		*byIdx = make(map[int32]bool, 16)
	}
	(*byIdx)[node] = v
}

// forget drops node's entry, leaving it to follow the default.
func (s *State) forget(byIdx map[int32]bool, byKey map[string]bool, node int32) {
	if s.keyed() {
		if k, ok := s.key(node); ok {
			delete(byKey, k)
		}
		return
	}
	delete(byIdx, node)
}

// refSet points r at node under the current binding; a negative node clears it.
func (s *State) refSet(r *nodeRef, node int32) {
	*r = nodeRef{}
	if node < 0 {
		return
	}
	if k, ok := s.key(node); ok {
		r.key, r.keyed = k, true
		return
	}
	r.idxP1 = node + 1
}

// refGet resolves r against the bound tree, or -1 when it names nothing —
// including a keyed ref whose node the current tree no longer has.
func (s *State) refGet(r nodeRef) (node int32) {
	if r.keyed {
		return s.indexOfKey(r.key)
	}
	return r.idxP1 - 1
}

// IsExpanded reports whether node is open: its recorded state if it has one,
// and [State.SetDefaultExpanded]'s value otherwise. Leaves are never drawn
// open; the renderer asks this only of nodes that have children.
func (s *State) IsExpanded(node int32) bool {
	if v, recorded := s.lookup(s.expanded, s.expandedKeys, node); recorded {
		return v
	}
	return s.defaultExpanded
}

// SetExpanded opens or closes node. Expanding a leaf is harmless and has no
// effect on the flattened rows, so a host driving expansion from a search
// result does not have to check for children first.
//
// Setting a node to the current default drops its record rather than storing
// it, which keeps the map bounded by what the reader actually changed and
// leaves a later [State.SetDefaultExpanded] free to move it.
func (s *State) SetExpanded(node int32, open bool) {
	if open == s.defaultExpanded {
		s.forget(s.expanded, s.expandedKeys, node)
		return
	}
	s.record(&s.expanded, &s.expandedKeys, node, open)
}

// ToggleExpanded flips node's open state and returns the new one.
func (s *State) ToggleExpanded(node int32) (open bool) {
	open = !s.IsExpanded(node)
	s.SetExpanded(node, open)
	return
}

// SetDefaultExpanded sets how a node with no record of its own is drawn. It
// starts false, which is a fully collapsed tree.
//
// It is a default and not a seed: it moves every node the reader has not
// touched, at any point, and changing it back moves them back. A host whose
// outline should start open — the usual case for a document, a schema, a
// config registry — sets it true once and then stores only what was closed.
//
// It does not clear anything. [State.ExpandAll] and [State.CollapseAll] are
// the two that do.
func (s *State) SetDefaultExpanded(open bool) { s.defaultExpanded = open }

// ExpandAll opens everything: it sets the default open and drops every record,
// so nodes that do not exist yet are open too when they arrive.
//
// That last part is the difference from a loop over the current nodes, and it
// is usually what "expand all" means — a filter that widens, or a document
// that gains a section, should not bring back rows the reader has just
// finished opening. A host that means "open what is on screen now, and leave
// the default alone" writes the loop over [State.SetExpanded] instead.
func (s *State) ExpandAll() {
	s.defaultExpanded = true
	clear(s.expanded)
	clear(s.expandedKeys)
}

// CollapseAll closes everything, leaving only the roots visible: the default
// goes closed and every record is dropped. The mirror of [State.ExpandAll],
// with the same caveat about nodes that arrive later.
func (s *State) CollapseAll() {
	s.defaultExpanded = false
	clear(s.expanded)
	clear(s.expandedKeys)
}

// ExpandAncestors opens every ancestor of node so that node itself becomes
// visible, without touching node's own open state. This is what "reveal this
// row" means — after a search hit, or restoring a selection — and doing it by
// hand needs the parent walk the host would rather not write. It binds to t,
// so a host may call it straight after a rebuild.
func (s *State) ExpandAncestors(t Tree, node int32) {
	s.Bind(t)
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
func (s *State) IsSelected(node int32) bool {
	v, _ := s.lookup(s.selected, s.selectedKeys, node)
	return v
}

// SetSelected adds or removes node from the selection.
func (s *State) SetSelected(node int32, on bool) {
	if !on {
		s.forget(s.selected, s.selectedKeys, node)
		return
	}
	s.record(&s.selected, &s.selectedKeys, node, true)
}

// SelectOnly replaces the whole selection with node — the plain-click
// behaviour, as against the ctrl-click [State.SetSelected] models.
func (s *State) SelectOnly(node int32) {
	s.ClearSelection()
	s.SetSelected(node, true)
}

// ClearSelection deselects everything.
func (s *State) ClearSelection() {
	clear(s.selected)
	clear(s.selectedKeys)
}

// SelectionLen is how many nodes are selected — including, on a keyed State,
// nodes the bound tree does not currently have. A selection outliving the
// rows it was made on is the point of a key: a filtered-out row comes back
// selected. [State.Selection] yields only the ones that are there.
func (s *State) SelectionLen() int {
	if s.keyed() {
		return len(s.selectedKeys)
	}
	return len(s.selected)
}

// Selection appends the selected nodes to dst, in ascending node order, and
// returns it. Appending to a caller slice keeps a per-frame read
// allocation-free.
//
// Ordered rather than map order because a host that renders the selection as
// a line of text gets a readout that changes between frames with nothing
// having changed otherwise — which reads as a flickering widget, and which no
// polling assertion in a headless scene can wait on.
//
// On a keyed State a selected node whose key is not in the bound tree has no
// index to report and is skipped; see [State.SelectionLen].
func (s *State) Selection(dst []int32) []int32 {
	if s.keyed() {
		for i := range s.keys {
			if s.selectedKeys[s.keys[i]] {
				dst = append(dst, int32(i))
			}
		}
		return dst
	}
	base := len(dst)
	for n := range s.selected {
		dst = append(dst, n)
	}
	slices.Sort(dst[base:])
	return dst
}

// Cursor is the node the keyboard is on, or -1 for none — including when the
// node it was put on is no longer in the tree.
func (s *State) Cursor() int32 { return s.refGet(s.cursor) }

// SetCursor moves the keyboard cursor; any negative node clears it. It does
// not change the selection — see the cursor field's documentation for why the
// two are separate.
func (s *State) SetCursor(node int32) { s.refSet(&s.cursor, node) }

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
//
// # It opens the ancestors in THIS State, which a host that owns expansion overwrites
//
// The half that opens the ancestors writes into this State, and a host that
// keeps its own expansion map and rewrites the State from it each frame — the
// shape every adopter had before Keys — undoes that write on the very next
// frame. Each half is correct on its own and the composition silently is not:
// the reveal opens the path, the sync closes it again, and the scroll lands on
// a row that is no longer there.
//
// A keyed State does not have the problem, because it IS the host's store and
// there is nothing to rewrite. A host that keeps its own map anyway should
// open the path in that map instead, which [State.ExpandAncestors] is exported
// for.
func (s *State) Reveal(node int32) { s.refSet(&s.reveal, node) }

// PendingReveal is the node a [State.Reveal] is waiting for, or -1 when none
// is pending or the node has left the tree. The next [Render] consumes it.
//
// It exists so that a host — or its test — can see that a reveal was asked
// for. Without it the request is write-only from outside the package, and
// asserting on it means threading a return value through the host's own code
// to say what it just told the widget.
func (s *State) PendingReveal() (node int32) { return s.refGet(s.reveal) }

// takeReveal returns the pending reveal target and clears it, or -1.
func (s *State) takeReveal() (node int32) {
	node = s.refGet(s.reveal)
	s.reveal = nodeRef{}
	return
}
