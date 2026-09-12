package nav

import (
	"cmp"
	"iter"
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
)

// Mode selects how the visible set is derived (ADR-0225 §SD2).
type Mode uint8

const (
	// ModeShowAll shows every node not hidden.
	ModeShowAll Mode = 0
	// ModeManual shows the roots and what the expansions reach.
	ModeManual Mode = 1
	// ModeFocus shows the roots and what the focus nodes reach within their
	// radius, with a relevance that decays per hop.
	ModeFocus Mode = 2
)

// Direction says which edges an expansion walks.
type Direction uint8

const (
	DirBoth Direction = 0
	DirOut  Direction = 1 // along the edge, from the expanded node outward
	DirIn   Direction = 2 // against it
)

// Node is one node of the universe: the spec graphview draws, and whether
// it is a stub — known only as a neighbour, its own neighbourhood not
// loaded (§SD1, §SD5).
type Node struct {
	Spec graphview.NodeSpec
	Stub bool
}

// Options configures a Navigator. Zero fields take the defaults noted.
// Opts is read on every derivation, so a change takes effect on the next
// Declare.
type Options struct {
	Mode Mode
	// ExpandDepth and ExpandDir are what Apply's double-click and an
	// Expand with a zero depth use: default 1 hop, both directions.
	ExpandDepth int
	ExpandDir   Direction
	// FocusRadius is the hops shown around the newest focus node, default
	// 2; FocusTailRadius around the older ones, default FocusRadius.
	FocusRadius     int
	FocusTailRadius int
	// MaxFocusNodes bounds the focus list, default 3; past it the oldest is
	// dropped unless NoAutoUnfocus is set, in which case Focus refuses.
	MaxFocusNodes int
	NoAutoUnfocus bool
	// RelevanceDecay is the factor per hop from a focus node, default 0.5.
	RelevanceDecay float32
	// Style, when set, runs per visible node during Declare with the
	// node's relevance (1 outside focus mode) and depth — hops from the
	// nearest root or focus — and may edit the spec declared for it.
	Style func(id uint64, relevance float32, depth int, spec *graphview.NodeSpec)
}

// optionsKey is the comparable part of Options the derivation depends on;
// a change derives afresh even when nothing else moved.
type optionsKey struct {
	mode            Mode
	expandDepth     int
	expandDir       Direction
	focusRadius     int
	focusTailRadius int
	maxFocusNodes   int
	noAutoUnfocus   bool
	relevanceDecay  float32
	styled          bool
}

func (inst Options) key() optionsKey {
	return optionsKey{inst.Mode, inst.ExpandDepth, inst.ExpandDir, inst.FocusRadius, inst.FocusTailRadius,
		inst.MaxFocusNodes, inst.NoAutoUnfocus, inst.RelevanceDecay, inst.Style != nil}
}

func (inst Options) withDefaults() Options {
	if inst.ExpandDepth <= 0 {
		inst.ExpandDepth = 1
	}
	if inst.FocusRadius <= 0 {
		inst.FocusRadius = 2
	}
	if inst.FocusTailRadius <= 0 {
		inst.FocusTailRadius = inst.FocusRadius
	}
	if inst.MaxFocusNodes <= 0 {
		inst.MaxFocusNodes = 3
	}
	if inst.RelevanceDecay <= 0 {
		inst.RelevanceDecay = 0.5
	}
	return inst
}

type expansion struct {
	depth int
	dir   Direction
}

type focusEntry struct {
	id        uint64
	relevance float32
}

// Navigator owns the universe, the navigation state and the derived
// declaration (ADR-0225). Construct with New and keep it; feed it, drive
// it, and read Declare every frame.
type Navigator struct {
	Opts Options

	// universe, struct-of-arrays by slot
	ids   []uint64
	slot  map[uint64]int32
	specs []graphview.NodeSpec
	stub  []bool
	edges []graphview.EdgeSpec
	// undirected adjacency in id order, with the direction per entry
	adjStart []int32
	adjTo    []int32
	adjOut   []bool // the edge leaves this node (this node is From)
	adjDirty bool
	uVer     uint32 // universe version

	// state
	initial  []uint64
	roots    map[uint64]struct{}
	expanded map[uint64]expansion
	hidden   map[uint64]struct{}
	focus    []focusEntry
	sVer     uint32 // state version

	// derived, valid while the versions and the options key hold
	derivedU, derivedS uint32
	derivedOpts        optionsKey
	derivedOk          bool
	visible            []bool
	relevance          []float32
	depth              []int32
	declNodes          []graphview.NodeSpec
	declEdges          []graphview.EdgeSpec
	pending            []uint64
	pendingSet         map[uint64]struct{}

	// scratch
	queue    []int32
	hops     []int32 // parallel to queue
	stamp    []uint32
	stampCur uint32
	keys     []uint64
}

// New returns an empty navigator.
func New(o Options) *Navigator {
	return &Navigator{
		Opts:       o,
		slot:       make(map[uint64]int32, 64),
		roots:      make(map[uint64]struct{}, 8),
		expanded:   make(map[uint64]expansion, 8),
		hidden:     make(map[uint64]struct{}, 8),
		pendingSet: make(map[uint64]struct{}, 8),
		adjDirty:   true,
		derivedS:   math.MaxUint32,
	}
}

// --- universe ---------------------------------------------------------

// AddNodes adds or replaces nodes by id. A node re-added with Stub false
// is loaded from then on; its spec is replaced either way.
func (n *Navigator) AddNodes(nodes []Node) {
	for i := range nodes {
		nd := &nodes[i]
		if s, ok := n.slot[nd.Spec.Id]; ok {
			n.specs[s] = nd.Spec
			n.stub[s] = nd.Stub
			continue
		}
		s := int32(len(n.ids))
		n.slot[nd.Spec.Id] = s
		n.ids = append(n.ids, nd.Spec.Id)
		n.specs = append(n.specs, nd.Spec)
		n.stub = append(n.stub, nd.Stub)
	}
	n.touchUniverse()
}

// AddEdges adds edges; an edge whose ref — from, to, id — is already known
// has its spec replaced. Edges to unknown ids are kept and take effect
// once the ids arrive.
func (n *Navigator) AddEdges(edges []graphview.EdgeSpec) {
	for i := range edges {
		e := &edges[i]
		if j := n.findEdge(refOf(e)); j >= 0 {
			n.edges[j] = *e
			continue
		}
		n.edges = append(n.edges, *e)
	}
	n.touchUniverse()
}

// RemoveNodes drops nodes and every edge at them. State that names a
// removed id is left alone; it takes effect again if the id returns.
func (n *Navigator) RemoveNodes(ids []uint64) {
	for _, id := range ids {
		s, ok := n.slot[id]
		if !ok {
			continue
		}
		last := int32(len(n.ids) - 1)
		delete(n.slot, id)
		if s != last {
			n.ids[s] = n.ids[last]
			n.specs[s] = n.specs[last]
			n.stub[s] = n.stub[last]
			n.slot[n.ids[s]] = s
		}
		n.ids = n.ids[:last]
		n.specs = n.specs[:last]
		n.stub = n.stub[:last]
		n.edges = slices.DeleteFunc(n.edges, func(e graphview.EdgeSpec) bool { return e.From == id || e.To == id })
	}
	n.touchUniverse()
}

// RemoveEdges drops edges by ref.
func (n *Navigator) RemoveEdges(refs []graphview.EdgeRef) {
	for _, ref := range refs {
		if j := n.findEdge(ref); j >= 0 {
			n.edges = slices.Delete(n.edges, j, j+1)
		}
	}
	n.touchUniverse()
}

// Clear empties the universe and the state, keeping the options.
func (n *Navigator) Clear() {
	n.ids = n.ids[:0]
	clear(n.slot)
	n.specs = n.specs[:0]
	n.stub = n.stub[:0]
	n.edges = n.edges[:0]
	n.initial = n.initial[:0]
	clear(n.roots)
	clear(n.expanded)
	clear(n.hidden)
	n.focus = n.focus[:0]
	n.touchUniverse()
	n.touchState()
}

// NodeCount and EdgeCount size the universe.
func (n *Navigator) NodeCount() int { return len(n.ids) }
func (n *Navigator) EdgeCount() int { return len(n.edges) }

// Known reports whether the universe carries the id.
func (n *Navigator) Known(id uint64) bool {
	_, ok := n.slot[id]
	return ok
}

// IsStub reports whether the node is a stub, false for an unknown id.
func (n *Navigator) IsStub(id uint64) bool {
	s, ok := n.slot[id]
	return ok && n.stub[s]
}

func refOf(e *graphview.EdgeSpec) graphview.EdgeRef {
	return graphview.EdgeRef{From: e.From, To: e.To, Id: e.Id}
}

func (n *Navigator) findEdge(ref graphview.EdgeRef) int {
	for j := range n.edges {
		if refOf(&n.edges[j]) == ref {
			return j
		}
	}
	return -1
}

func (n *Navigator) touchUniverse() {
	n.uVer++
	n.adjDirty = true
}

func (n *Navigator) touchState() { n.sVer++ }

// ensureAdjacency rebuilds the undirected adjacency, neighbours in id
// order, with the direction per entry. Edges at unknown ids and self-loops
// contribute nothing to a walk.
func (n *Navigator) ensureAdjacency() {
	if !n.adjDirty {
		return
	}
	n.adjDirty = false
	cnt := len(n.ids)
	n.adjStart = growTo(n.adjStart, cnt+1)
	clear(n.adjStart)
	type end struct {
		from, to int32
	}
	ends := make([]end, 0, len(n.edges))
	for i := range n.edges {
		e := &n.edges[i]
		f, okF := n.slot[e.From]
		t, okT := n.slot[e.To]
		if !okF || !okT || f == t {
			continue
		}
		ends = append(ends, end{f, t})
		n.adjStart[f+1]++
		n.adjStart[t+1]++
	}
	for i := 0; i < cnt; i++ {
		n.adjStart[i+1] += n.adjStart[i]
	}
	total := int(n.adjStart[cnt])
	n.adjTo = growTo(n.adjTo, total)
	n.adjOut = growTo(n.adjOut, total)
	cur := make([]int32, cnt)
	copy(cur, n.adjStart[:cnt])
	for _, e := range ends {
		n.adjTo[cur[e.from]], n.adjOut[cur[e.from]] = e.to, true
		cur[e.from]++
		n.adjTo[cur[e.to]], n.adjOut[cur[e.to]] = e.from, false
		cur[e.to]++
	}
	// Id order per node, so a walk is a function of the topology alone.
	for s := 0; s < cnt; s++ {
		lo, hi := n.adjStart[s], n.adjStart[s+1]
		idx := make([]int32, hi-lo)
		for i := range idx {
			idx[i] = lo + int32(i)
		}
		slices.SortFunc(idx, func(a, b int32) int {
			if r := cmp.Compare(n.ids[n.adjTo[a]], n.ids[n.adjTo[b]]); r != 0 {
				return r
			}
			return cmp.Compare(boolInt(n.adjOut[b]), boolInt(n.adjOut[a]))
		})
		to := make([]int32, len(idx))
		out := make([]bool, len(idx))
		for i, a := range idx {
			to[i], out[i] = n.adjTo[a], n.adjOut[a]
		}
		copy(n.adjTo[lo:hi], to)
		copy(n.adjOut[lo:hi], out)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- state ------------------------------------------------------------

// SetInitial names the nodes Reset returns to: roots in every mode, and
// the focus list in focus mode. It does not apply them; call Reset.
func (n *Navigator) SetInitial(ids []uint64) {
	n.initial = append(n.initial[:0], ids...)
}

// Reset clears the state and applies the initial nodes.
func (n *Navigator) Reset() {
	clear(n.roots)
	clear(n.expanded)
	clear(n.hidden)
	n.focus = n.focus[:0]
	o := n.Opts.withDefaults()
	for _, id := range n.initial {
		n.roots[id] = struct{}{}
		if o.Mode == ModeFocus {
			n.pushFocus(id, 1, o)
		}
	}
	n.touchState()
}

// Show makes the node a root: visible in every mode until Hide or Close.
// It reports false for an unknown id.
func (n *Navigator) Show(id uint64) bool {
	if !n.Known(id) {
		return false
	}
	delete(n.hidden, id)
	n.roots[id] = struct{}{}
	n.touchState()
	return true
}

// Hide keeps the node out of the picture and out of every walk until Show;
// it stays a root or an expansion for when it is shown again.
func (n *Navigator) Hide(id uint64) {
	delete(n.roots, id)
	n.hidden[id] = struct{}{}
	n.touchState()
}

// Close is Collapse plus Hide.
func (n *Navigator) Close(id uint64) {
	delete(n.expanded, id)
	n.Hide(id)
}

// IsHidden reports whether the node is hidden.
func (n *Navigator) IsHidden(id uint64) bool {
	_, ok := n.hidden[id]
	return ok
}

// Expand records an expansion of the node to depth hops in dir; a zero
// depth takes Options.ExpandDepth. It takes effect while the node is
// visible, and replaces an earlier expansion of the same node. It reports
// false for an unknown id.
func (n *Navigator) Expand(id uint64, depth int, dir Direction) bool {
	if !n.Known(id) {
		return false
	}
	if depth <= 0 {
		depth = n.Opts.withDefaults().ExpandDepth
	}
	n.expanded[id] = expansion{depth: depth, dir: dir}
	n.touchState()
	return true
}

// Collapse removes the node's expansion; what only it reached leaves the
// picture. It reports whether there was one.
func (n *Navigator) Collapse(id uint64) bool {
	if _, ok := n.expanded[id]; !ok {
		return false
	}
	delete(n.expanded, id)
	n.touchState()
	return true
}

// Expanded reports whether the node carries an expansion.
func (n *Navigator) Expanded(id uint64) bool {
	_, ok := n.expanded[id]
	return ok
}

// Focus appends the node to the focus list with the given relevance (zero
// takes 1); a node already listed moves to the end. Past MaxFocusNodes
// the oldest is dropped, or, under NoAutoUnfocus, the call refuses. It
// reports whether the node is focused afterwards.
func (n *Navigator) Focus(id uint64, relevance float32) bool {
	if !n.Known(id) {
		return false
	}
	if !n.pushFocus(id, relevance, n.Opts.withDefaults()) {
		return false
	}
	n.touchState()
	return true
}

func (n *Navigator) pushFocus(id uint64, relevance float32, o Options) bool {
	if relevance <= 0 {
		relevance = 1
	}
	if i := slices.IndexFunc(n.focus, func(f focusEntry) bool { return f.id == id }); i >= 0 {
		n.focus = slices.Delete(n.focus, i, i+1)
	} else if len(n.focus) >= o.MaxFocusNodes {
		if o.NoAutoUnfocus {
			return false
		}
		n.focus = slices.Delete(n.focus, 0, 1)
	}
	n.focus = append(n.focus, focusEntry{id: id, relevance: relevance})
	return true
}

// Unfocus removes the node from the focus list.
func (n *Navigator) Unfocus(id uint64) {
	if i := slices.IndexFunc(n.focus, func(f focusEntry) bool { return f.id == id }); i >= 0 {
		n.focus = slices.Delete(n.focus, i, i+1)
		n.touchState()
	}
}

// ClearFocus empties the focus list.
func (n *Navigator) ClearFocus() {
	n.focus = n.focus[:0]
	n.touchState()
}

// FocusNodes yields the focus list, oldest first.
func (n *Navigator) FocusNodes() iter.Seq[uint64] {
	return func(yield func(uint64) bool) {
		for _, f := range n.focus {
			if !yield(f.id) {
				return
			}
		}
	}
}

// IsFocused reports whether the node is on the focus list.
func (n *Navigator) IsFocused(id uint64) bool {
	return slices.IndexFunc(n.focus, func(f focusEntry) bool { return f.id == id }) >= 0
}

// Apply is the one gesture wired by default (§SD4): a node double-click
// expands a node that is not expanded and collapses one that is, in manual
// mode, and focuses it in focus mode. Every other event is ignored.
func (n *Navigator) Apply(ev graphview.Event) {
	if ev.Kind != graphview.EventKindNodeDoubleClick {
		return
	}
	switch n.Opts.Mode {
	case ModeManual:
		if n.Expanded(ev.Node) {
			n.Collapse(ev.Node)
		} else {
			n.Expand(ev.Node, 0, n.Opts.ExpandDir)
		}
	case ModeFocus:
		n.Focus(ev.Node, 1)
	}
}

// --- derived ----------------------------------------------------------

// Declare returns the visible nodes and the edges between them, in
// universe order, styled by Options.Style. The slices are the navigator's
// and valid until the next call that changes the universe or the state.
func (n *Navigator) Declare() (nodes []graphview.NodeSpec, edges []graphview.EdgeSpec) {
	n.ensureDerived()
	return n.declNodes, n.declEdges
}

// Pending returns, in id order, the stubs the last derivation wanted to
// expand: the ids to load and answer with AddNodes and AddEdges.
func (n *Navigator) Pending() []uint64 {
	n.ensureDerived()
	return n.pending
}

// Visible reports whether the node is in the picture.
func (n *Navigator) Visible(id uint64) bool {
	n.ensureDerived()
	s, ok := n.slot[id]
	return ok && n.visible[s]
}

// Relevance is the node's relevance: by distance from the focus nodes in
// focus mode, 1 for any other visible node, 0 when not visible.
func (n *Navigator) Relevance(id uint64) float32 {
	n.ensureDerived()
	s, ok := n.slot[id]
	if !ok || !n.visible[s] {
		return 0
	}
	return n.relevance[s]
}

// Depth is the node's hops from the nearest root or focus node, or -1 when
// not visible.
func (n *Navigator) Depth(id uint64) int {
	n.ensureDerived()
	s, ok := n.slot[id]
	if !ok || !n.visible[s] {
		return -1
	}
	return int(n.depth[s])
}

// HiddenNeighbours counts a visible node's neighbours that are known and
// not in the picture — the number a "+" badge carries. 0 for a node not
// visible.
func (n *Navigator) HiddenNeighbours(id uint64) int {
	n.ensureDerived()
	s, ok := n.slot[id]
	if !ok || !n.visible[s] {
		return 0
	}
	count := 0
	lastTo := int32(-1)
	for a := n.adjStart[s]; a < n.adjStart[s+1]; a++ {
		to := n.adjTo[a]
		if to == lastTo {
			continue // parallel edges count the neighbour once
		}
		lastTo = to
		if !n.visible[to] {
			count++
		}
	}
	return count
}

func (n *Navigator) ensureDerived() {
	k := n.Opts.key()
	if n.derivedOk && n.derivedU == n.uVer && n.derivedS == n.sVer && n.derivedOpts == k {
		return
	}
	n.derive()
	n.derivedOk, n.derivedU, n.derivedS, n.derivedOpts = true, n.uVer, n.sVer, k
}

// derive computes the visible set, relevance and depth from the universe
// and the state (§SD2), then the declaration and the pending list.
func (n *Navigator) derive() {
	n.ensureAdjacency()
	o := n.Opts.withDefaults()
	cnt := len(n.ids)
	n.visible = growTo(n.visible, cnt)
	n.relevance = growTo(n.relevance, cnt)
	n.depth = growTo(n.depth, cnt)
	n.stamp = growTo(n.stamp, cnt)
	clear(n.visible)
	clear(n.relevance)
	for i := range n.depth {
		n.depth[i] = -1
	}
	n.pending = n.pending[:0]
	clear(n.pendingSet)

	reach := func(s int32, d int32, rel float32) {
		n.visible[s] = true
		n.relevance[s] = max(n.relevance[s], rel)
		if n.depth[s] < 0 || d < n.depth[s] {
			n.depth[s] = d
		}
	}
	isHidden := func(s int32) bool {
		_, h := n.hidden[n.ids[s]]
		return h
	}

	switch o.Mode {
	case ModeShowAll:
		for s := range n.ids {
			if !isHidden(int32(s)) {
				reach(int32(s), 0, 1)
			}
		}
	default:
		// Roots first, in id order.
		n.keys = sortedKeys(n.keys[:0], n.roots)
		for _, id := range n.keys {
			if s, ok := n.slot[id]; ok && !isHidden(s) {
				reach(s, 0, 1)
			}
		}
		if o.Mode == ModeFocus {
			for k, f := range n.focus {
				s, ok := n.slot[f.id]
				if !ok || isHidden(s) {
					continue
				}
				radius := o.FocusTailRadius
				if k == len(n.focus)-1 {
					radius = o.FocusRadius
				}
				n.walk(s, radius, DirBoth, f.relevance, o.RelevanceDecay, reach, isHidden)
			}
		} else {
			// Expansions take effect while their node is visible, which
			// another expansion may be what makes it; iterate until no
			// entry can newly apply.
			keys := sortedKeys(n.keys[:0], n.expanded)
			done := make([]bool, len(keys))
			for progressed := true; progressed; {
				progressed = false
				for i, id := range keys {
					if done[i] {
						continue
					}
					s, ok := n.slot[id]
					if !ok || isHidden(s) || !n.visible[s] {
						continue
					}
					e := n.expanded[id]
					n.walk(s, e.depth, e.dir, 1, 1, reach, isHidden)
					done[i] = true
					progressed = true
				}
			}
		}
	}

	// Declaration in universe order; edges with both ends visible.
	n.declNodes = n.declNodes[:0]
	for s := range n.ids {
		if !n.visible[s] {
			continue
		}
		spec := n.specs[s]
		if o.Style != nil {
			o.Style(n.ids[s], n.relevance[s], int(n.depth[s]), &spec)
		}
		n.declNodes = append(n.declNodes, spec)
	}
	n.declEdges = n.declEdges[:0]
	for i := range n.edges {
		e := &n.edges[i]
		f, okF := n.slot[e.From]
		t, okT := n.slot[e.To]
		if okF && okT && n.visible[f] && n.visible[t] {
			n.declEdges = append(n.declEdges, *e)
		}
	}
	slices.Sort(n.pending)
}

// walk is one breadth-first expansion from start to at most depth hops,
// following edges per dir and never entering a hidden node. Every node
// reached is made visible at relevance rel·decay^hops. A stub with hops
// left, the start included, goes on the pending list.
func (n *Navigator) walk(start int32, depth int, dir Direction, rel, decay float32,
	reach func(s int32, d int32, rel float32), isHidden func(s int32) bool) {
	n.stampCur++
	if n.stampCur == 0 {
		clear(n.stamp)
		n.stampCur = 1
	}
	n.queue = append(n.queue[:0], start)
	n.hops = append(n.hops[:0], 0)
	n.stamp[start] = n.stampCur
	reach(start, 0, rel)
	if n.stub[start] && depth > 0 {
		n.addPending(n.ids[start])
	}
	for head := 0; head < len(n.queue); head++ {
		s := n.queue[head]
		d := n.hops[head]
		if int(d) >= depth {
			continue
		}
		f := rel * powf(decay, int(d)+1)
		for a := n.adjStart[s]; a < n.adjStart[s+1]; a++ {
			to := n.adjTo[a]
			switch dir {
			case DirOut:
				if !n.adjOut[a] {
					continue
				}
			case DirIn:
				if n.adjOut[a] {
					continue
				}
			}
			if isHidden(to) {
				continue
			}
			if n.stamp[to] == n.stampCur {
				continue
			}
			n.stamp[to] = n.stampCur
			reach(to, d+1, f)
			if n.stub[to] && int(d)+1 < depth {
				n.addPending(n.ids[to])
			}
			n.queue = append(n.queue, to)
			n.hops = append(n.hops, d+1)
		}
	}
}

func (n *Navigator) addPending(id uint64) {
	if _, dup := n.pendingSet[id]; dup {
		return
	}
	n.pendingSet[id] = struct{}{}
	n.pending = append(n.pending, id)
}

// sortedKeys appends the map's keys to dst in ascending order.
func sortedKeys[V any](dst []uint64, m map[uint64]V) []uint64 {
	for k := range m {
		dst = append(dst, k)
	}
	slices.Sort(dst)
	return dst
}

func powf(b float32, e int) float32 {
	r := float32(1)
	for range e {
		r *= b
	}
	return r
}

// growTo returns s resliced to n, reallocating only when the capacity is
// short; the contents are unspecified.
func growTo[T any](s []T, n int) []T {
	if cap(s) < n {
		s = make([]T, n)
	}
	return s[:n]
}
