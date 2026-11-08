package carrierclient

import (
	"fmt"
	"time"
)

// FocusFailures checks what one key press did to keyboard focus, from the
// tree before the key and the trees sampled while the step settled, in pass
// order (ADR-0363 §M2, proposed). It reports, in words for a failing step:
//
//   - focus that was on a widget falling to the window root, where a screen
//     reader announces only the window and the person loses their place;
//   - focus moving more than once for the one key, which a screen reader
//     speaks as two announcements, the first of them wrong;
//   - the focused item reappearing under a new node id, which a screen
//     reader announces again as though it were a different item.
//
// The rules are zapfast's focus trace (src/demo/focus_trace.rs). Samples
// taken within the same pass count once; a pass the sampling missed is not
// seen, so a flicker that lasted one pass between two samples can pass
// unnoticed.
func FocusFailures(before *TreeSnapshot, after []*TreeSnapshot) (failures []string) {
	if before == nil {
		return nil
	}
	prev := before
	moves := 0
	lastPass := before.GetPass()
	for _, snap := range after {
		if snap == nil || snap.GetPass() == lastPass {
			continue
		}
		lastPass = snap.GetPass()
		from, to := prev.GetFocus(), snap.GetFocus()
		if from == to {
			prev = snap
			continue
		}
		moves++
		fromNode, toNode := nodeByID(prev, from), nodeByID(snap, to)
		switch {
		case fromNode != nil && !isRoot(fromNode) && toNode != nil && isRoot(toNode):
			failures = append(failures, fmt.Sprintf("focus fell from %s to the window root at pass %d", FormatNode(fromNode), snap.GetPass()))
		case fromNode != nil && toNode != nil && sameItem(fromNode, toNode):
			failures = append(failures, fmt.Sprintf("the focused item %s came back under a new node id #%d at pass %d", FormatNode(fromNode), to, snap.GetPass()))
		}
		prev = snap
	}
	if moves > 1 {
		failures = append(failures, fmt.Sprintf("focus moved %d times for one key", moves))
	}
	return failures
}

// FocusTraceMinWindow is how long a traced key step samples the tree when
// its own settle is shorter: long enough for a dialog to close and hand
// focus back, which takes a few passes.
const FocusTraceMinWindow = 300 * time.Millisecond

// traceFocus samples the tree until window has passed, for FocusFailures.
func traceFocus(c *Client, window time.Duration, timeout time.Duration) (samples []*TreeSnapshot, err error) {
	if window < FocusTraceMinWindow {
		window = FocusTraceMinWindow
	}
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		var snap *TreeSnapshot
		if snap, err = c.Tree(timeout); err != nil {
			return samples, err
		}
		samples = append(samples, snap)
	}
	return samples, nil
}

func nodeByID(snap *TreeSnapshot, id uint64) *TreeNode {
	for _, n := range snap.GetNodes() {
		if n.GetId() == id {
			return n
		}
	}
	return nil
}

func isRoot(n *TreeNode) bool {
	return n.GetParent() == 0
}

// sameItem reports whether two nodes are what a screen reader would speak
// the same way: role, name and value.
func sameItem(a, b *TreeNode) bool {
	return a.GetRole() == b.GetRole() && a.GetName() == b.GetName() && a.GetValue() == b.GetValue()
}
