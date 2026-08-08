package carrierclient

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// locate.go resolves a human-written locator against a tree snapshot — the
// first rungs of the ADR-0127 §SD4 anchor ladder, which ADR-0154 §SD6 reuses so
// one trace vocabulary serves both executors.
//
// The ordering is the point. An exact node id is a pure function of the
// widget's path down the id stack, so it survives a resize, a re-run and an
// unrelated sibling insertion; a name is readable but may be ambiguous; a
// position survives nothing and is the rung of last resort, for painter-only
// widgets that have no node at all. Each rung is *checked* rather than
// guessed: an ambiguous match is an error, not a silent pick of the first hit,
// because the silent pick is exactly how a driver ends up asserting against the
// wrong widget.

// Locator selects one node of a snapshot. Fields are ANDed; leave a field zero
// to ignore it.
type Locator struct {
	// ID matches a node exactly. Set it and nothing else is consulted.
	ID uint64
	// Name matches the accessible name.
	Name string
	// NameContains matches a substring of the accessible name — for labels
	// carrying a live count or value that would defeat an exact match.
	NameContains string
	// Value and ValueContains match the accessible value, exactly or by
	// substring.
	//
	// They exist because a large class of widgets has no accessible *name* at
	// all: egui puts a Label's text in the value slot and leaves the label
	// slot empty, so a plain label, a monospace readout or a tree row is
	// unreachable by Name however it is spelled. Interactive widgets carry a
	// name and are matched by it; static text is matched here.
	//
	// A separate pair rather than widening Name: a Name locator that silently
	// started matching values could turn a resolving anchor in an existing
	// trace into an ambiguous one, which fails the run. Opting in cannot.
	//
	// Pair them with Role: "label". egui emits a `text_run` child under some —
	// not all — of its labels, carrying the same text as the label itself, so
	// a bare value anchor resolves on one label and reports an ambiguity on
	// the next for no reason the trace author can see.
	Value         string
	ValueContains string
	// Role matches the AccessKit role ("button", "check_box", "text_input"…).
	Role string
	// Nth picks among several matches, 0-based. Without it, several matches is
	// an error; with it, the caller has said the ambiguity is expected and
	// which one it wants.
	Nth int
	// HasNth distinguishes "Nth: 0" from "Nth unset".
	HasNth bool
}

// Matches reports whether one node satisfies the locator, ignoring Nth.
func (inst Locator) Matches(n *TreeNode) bool {
	if inst.ID != 0 {
		return n.GetId() == inst.ID
	}
	if inst.Name != "" && n.GetName() != inst.Name {
		return false
	}
	if inst.NameContains != "" && !strings.Contains(n.GetName(), inst.NameContains) {
		return false
	}
	if inst.Value != "" && n.GetValue() != inst.Value {
		return false
	}
	if inst.ValueContains != "" && !strings.Contains(n.GetValue(), inst.ValueContains) {
		return false
	}
	if inst.Role != "" && n.GetRole() != inst.Role {
		return false
	}
	// A locator with no criterion at all would match every node; that is a
	// caller bug rather than a wildcard.
	return inst.Name != "" || inst.NameContains != "" ||
		inst.Value != "" || inst.ValueContains != "" || inst.Role != ""
}

// Resolve returns the single node the locator selects.
//
// Ambiguity is an error unless Nth was given. Hidden nodes are skipped: they
// are laid out but not on screen, so actuating one would succeed on the wire
// and do nothing a viewer could see.
func Resolve(snap *TreeSnapshot, loc Locator) (node *TreeNode, err error) {
	if snap == nil {
		return nil, eb.Build().Errorf("no tree snapshot to resolve against")
	}
	var hits []*TreeNode
	for _, n := range snap.GetNodes() {
		if n.GetFlags()&FlagHidden != 0 {
			continue
		}
		if loc.Matches(n) {
			hits = append(hits, n)
		}
	}
	switch {
	case len(hits) == 0:
		return nil, eb.Build().
			Str("name", loc.Name).
			Str("nameContains", loc.NameContains).
			Str("role", loc.Role).
			Uint64("id", loc.ID).
			Int("nodes", len(snap.GetNodes())).
			Errorf("no node matches the locator")
	case loc.HasNth:
		if loc.Nth < 0 || loc.Nth >= len(hits) {
			return nil, eb.Build().Int("nth", loc.Nth).Int("matches", len(hits)).
				Errorf("locator index is out of range")
		}
		return hits[loc.Nth], nil
	case len(hits) > 1:
		names := make([]string, 0, len(hits))
		for _, h := range hits {
			names = append(names, h.GetRole()+":"+h.GetName())
		}
		return nil, eb.Build().Int("matches", len(hits)).
			Str("candidates", strings.Join(names, ", ")).
			Errorf("locator is ambiguous — add a role, or an explicit index")
	default:
		return hits[0], nil
	}
}

// FindByName is the common case: the one visible node with this accessible
// name, or nil.
func FindByName(snap *TreeSnapshot, name string) (node *TreeNode) {
	n, err := Resolve(snap, Locator{Name: name})
	if err != nil {
		return nil
	}
	return n
}

// Center returns a node's midpoint in logical points — the coordinate fallback
// for a painter-only child of a node, and what a `pos`-based step resolves to.
func Center(n *TreeNode) (x, y float32) {
	return n.GetX() + n.GetW()/2, n.GetY() + n.GetH()/2
}

// Path renders a node's ancestry as "root > … > node", for error messages and
// for the readable half of a trace step.
// nodeCentre is where a synthetic pointer aims at a resolved node — the last
// rung of the ladder, reached from an anchor rather than from a literal. A
// zero-sized node (one that was never laid out) yields its origin, which is
// honest and misses, rather than a guess that lands somewhere arbitrary.
func nodeCentre(n *TreeNode) (x, y float32) {
	return n.GetX() + n.GetW()/2, n.GetY() + n.GetH()/2
}

func Path(snap *TreeSnapshot, n *TreeNode) (path string) {
	byID := make(map[uint64]*TreeNode, len(snap.GetNodes()))
	for _, x := range snap.GetNodes() {
		byID[x.GetId()] = x
	}
	var parts []string
	// Bounded by the node count: a cycle in the parent links would otherwise
	// spin here, and this runs on an error path where hanging is the worst
	// possible behaviour.
	for cur, hops := n, 0; cur != nil && hops <= len(byID); hops++ {
		label := cur.GetName()
		if label == "" {
			label = cur.GetRole()
		}
		parts = append([]string{label}, parts...)
		if cur.GetParent() == 0 {
			break
		}
		cur = byID[cur.GetParent()]
	}
	return strings.Join(parts, " > ")
}
