//go:build llm_generated_opus47

package treemap

import (
	"errors"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// NavKindE classifies a navigation event by its semantic outcome.
type NavKindE uint8

const (
	// NavKindDrillIn — focus moved deeper into the tree.
	NavKindDrillIn NavKindE = iota
	// NavKindDrillUp — focus moved closer to the root.
	NavKindDrillUp
	// NavKindReset — focus jumped back to root.
	NavKindReset
	// NavKindExternal — programmatic NavigateTo that wasn't strictly drill-in or drill-up
	// (e.g., switching to a sibling subtree). Reset is reported as NavKindReset.
	NavKindExternal
)

func (inst NavKindE) String() string {
	switch inst {
	case NavKindDrillIn:
		return "NavKindDrillIn"
	case NavKindDrillUp:
		return "NavKindDrillUp"
	case NavKindReset:
		return "NavKindReset"
	case NavKindExternal:
		return "NavKindExternal"
	}
	return "NavKindE(?)"
}

// NavTriggerE records what caused a navigation event.
type NavTriggerE uint8

const (
	// NavTriggerCellClick — user clicked a drillable cell in the treemap area.
	NavTriggerCellClick NavTriggerE = iota
	// NavTriggerDrillUpCellClick — user clicked an active-path drill-up cell.
	NavTriggerDrillUpCellClick
	// NavTriggerBreadcrumbClick — user clicked a breadcrumb-bar entry.
	NavTriggerBreadcrumbClick
	// NavTriggerExternal — programmatic call (NavigateTo, DrillTo, DrillUp, Reset).
	NavTriggerExternal
)

func (inst NavTriggerE) String() string {
	switch inst {
	case NavTriggerCellClick:
		return "NavTriggerCellClick"
	case NavTriggerDrillUpCellClick:
		return "NavTriggerDrillUpCellClick"
	case NavTriggerBreadcrumbClick:
		return "NavTriggerBreadcrumbClick"
	case NavTriggerExternal:
		return "NavTriggerExternal"
	}
	return "NavTriggerE(?)"
}

// NavEvent is delivered to OnNavigate subscribers whenever the breadcrumb
// changes. From and To are independent snapshots; modifying them does not
// affect the widget's state.
type NavEvent struct {
	Kind    NavKindE
	Trigger NavTriggerE
	From    []*layout.Node // breadcrumb before the change
	To      []*layout.Node // breadcrumb after the change
}

// ErrInvalidPath is returned by NavigateTo / DrillTo when the supplied path
// either doesn't begin at root or includes an edge that's not in the tree.
var ErrInvalidPath = errors.New("treemap: navigation path is not anchored at root or breaks the parent/child chain")

// NavigateTo sets the breadcrumb to a fully-specified path. The path must
// begin with the widget's root (pointer-equal) and walk down Children edges;
// otherwise ErrInvalidPath is returned and state is unchanged.
//
// If the path matches the current breadcrumb, NavigateTo is a no-op (no
// event fires).
//
// On success a zoom animation is triggered using the same fromRect logic as
// internal navigation, and registered OnNavigate subscribers are invoked
// with NavKindExternal/NavKindDrillIn/NavKindDrillUp/NavKindReset depending on the change.
func (inst *Treemap) NavigateTo(path []*layout.Node) error {
	if !inst.validPath(path) {
		return ErrInvalidPath
	}
	if pathsEqual(inst.breadcrumb, path) {
		return nil
	}
	inst.applyNavigation(append([]*layout.Node(nil), path...), NavTriggerExternal)
	return nil
}

// DrillTo navigates to focus on node, computing the path via search from
// root. Returns ErrInvalidPath if node is not reachable from root.
func (inst *Treemap) DrillTo(node *layout.Node) error {
	path := findPath(inst.root, node, nil)
	if path == nil {
		return ErrInvalidPath
	}
	return inst.NavigateTo(path)
}

// DrillUp moves focus closer to root by `levels` (clamped to current depth).
// Returns nil even when already at root (no-op).
func (inst *Treemap) DrillUp(levels int) error {
	if levels < 0 {
		levels = 0
	}
	newLen := len(inst.breadcrumb) - levels
	if newLen < 1 {
		newLen = 1
	}
	if newLen == len(inst.breadcrumb) {
		return nil
	}
	return inst.NavigateTo(inst.breadcrumb[:newLen])
}

// Reset jumps focus back to root.
func (inst *Treemap) Reset() {
	if len(inst.breadcrumb) <= 1 {
		return
	}
	inst.applyNavigation([]*layout.Node{inst.root}, NavTriggerExternal)
}

// OnNavigate registers fn to be invoked synchronously after every successful
// navigation. The returned closure unsubscribes; calling it more than once
// is a no-op. fn must not call back into the same Treemap's mutating API
// (NavigateTo, DrillTo, DrillUp, Reset, Render) — doing so panics with a
// clear message.
func (inst *Treemap) OnNavigate(fn func(NavEvent)) (unsubscribe func()) {
	if fn == nil {
		panic("treemap: OnNavigate requires a non-nil fn")
	}
	idx := len(inst.navSubs)
	inst.navSubs = append(inst.navSubs, fn)
	unsubscribed := false
	return func() {
		if unsubscribed {
			return
		}
		unsubscribed = true
		// Clear in place rather than removing so existing index stay valid
		// while we may be iterating during dispatch.
		if idx < len(inst.navSubs) {
			inst.navSubs[idx] = nil
		}
	}
}

// applyNavigation performs the breadcrumb mutation, classifies the change
// kind, computes the from-rect for the zoom animation, fires events, and
// triggers the animation. Single point of mutation so all navigation paths
// (cell click, breadcrumb click, external) go through the same checks.
func (inst *Treemap) applyNavigation(newPath []*layout.Node, trigger NavTriggerE) {
	if inst.dispatching {
		panic("treemap: navigation API called from within an OnNavigate handler")
	}
	from := append([]*layout.Node(nil), inst.breadcrumb...)
	kind := classifyNav(from, newPath)

	// Compute fromRect for the zoom animation. The choice mirrors
	// breadcrumb-click drill-up logic: zoom emerges from where the most
	// recently focused node sits in the new layout.
	var fromRect layout.Rect
	switch kind {
	case NavKindDrillIn:
		// Last node added; in the OLD layout it was the rect of newPath[len(from)].
		// Recompute from the OLD parent's layout.
		if len(from) >= 1 {
			parent := from[len(from)-1]
			oldLayout := layout.ComputeLayoutAt(parent, inst.containerRect())
			added := newPath[len(from)]
			fromRect = oldLayout.RectOf(added)
		}
	case NavKindDrillUp, NavKindReset, NavKindExternal:
		// Use the new tail's layout to find where the OLD tail (or its
		// nearest ancestor present in newPath+1) used to live.
		newTail := newPath[len(newPath)-1]
		lay := layout.ComputeLayoutAt(newTail, inst.containerRect())
		if len(newPath) < len(from) {
			fromRect = lay.RectOf(from[len(newPath)])
		}
	}

	inst.breadcrumb = newPath
	inst.anim.Start(fromRect)

	if len(inst.navSubs) > 0 {
		ev := NavEvent{Kind: kind, Trigger: trigger, From: from, To: append([]*layout.Node(nil), newPath...)}
		inst.dispatching = true
		for _, sub := range inst.navSubs {
			if sub != nil {
				sub(ev)
			}
		}
		inst.dispatching = false
	}
}

// validPath reports whether path begins at t.root and every adjacent pair is
// a parent/child edge. Empty path is invalid.
func (inst *Treemap) validPath(path []*layout.Node) bool {
	if len(path) < 1 || path[0] != inst.root {
		return false
	}
	for i := 1; i < len(path); i++ {
		parent := path[i-1]
		child := path[i]
		found := false
		for _, ch := range parent.Children {
			if ch == child {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// pathsEqual is pointer-equality on a slice of *Node.
func pathsEqual(a, b []*layout.Node) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// classifyNav returns the NavKindE for a transition from→to, both assumed
// non-empty and rooted at the same node.
func classifyNav(from, to []*layout.Node) NavKindE {
	if len(to) == 1 && len(from) > 1 {
		return NavKindReset
	}
	// to extends from?
	if len(to) > len(from) && pathsEqual(from, to[:len(from)]) {
		return NavKindDrillIn
	}
	// from extends to?
	if len(from) > len(to) && pathsEqual(to, from[:len(to)]) {
		return NavKindDrillUp
	}
	return NavKindExternal
}

// findPath performs DFS from root looking for target, returning the
// pointer-identity path or nil if unreachable. Uses a small slice as the
// in-progress accumulator; small enough that allocation cost is negligible.
func findPath(root, target *layout.Node, acc []*layout.Node) []*layout.Node {
	acc = append(acc, root)
	if root == target {
		out := make([]*layout.Node, len(acc))
		copy(out, acc)
		return out
	}
	for _, ch := range root.Children {
		if p := findPath(ch, target, acc); p != nil {
			return p
		}
	}
	return nil
}
