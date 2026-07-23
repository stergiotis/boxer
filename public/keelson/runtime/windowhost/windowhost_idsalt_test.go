package windowhost

import (
	"fmt"
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// TestPerWindowSaltNoSequenceAlias guards the cross-window widget-id collision
// fix. It reproduces the fleet id derivation for many concurrently-open
// windows — the per-window salt (windowhostInstanceSalt), an app-level named
// scope, then a run of PrepareSeq(index) option ids (a selector.Segmented tab
// bar) — and asserts every derived id is globally unique across the frame.
//
// Regression: the per-window salt was appIds.PrepareSeq(w.key), which lives in
// the SAME makeHighEntropy sequence space as the option indices. Under the XOR
// id stack the salt and an option index are interchangeable, so window w's tab
// i and window w' tab j aliased whenever {w.key, i} matched {w'.key, j} as a
// set — two open windows of the same app shared tab-bar ids (window 1 tab 2 ==
// window 2 tab 1, etc.), colliding in the global seenIds registry and sharing
// egui widget state. Swapping the salt to a domain-separated high-entropy value
// (windowhostInstanceSalt) moves it clear of the sequence space.
func TestPerWindowSaltNoSequenceAlias(t *testing.T) {
	const nWindows = 24
	const nTabs = 16
	seen := make(map[uint64]string, nWindows*nTabs)
	for key := uint64(1); key <= nWindows; key++ {
		ids := c.NewWidgetIdStack()
		// windowhost salts appIds around the app's Frame ...
		for range c.IdScope(ids.PrepareHighEntropy(windowhostInstanceSalt(WindowKeyT(key)))) {
			// ... the app opens a named scope (e.g. a selector "tabs" bar) ...
			for range c.IdScope(ids.PrepareStr("tabs")) {
				// ... and derives one id per option via PrepareSeq(index).
				for i := range uint64(nTabs) {
					id := ids.PrepareSeq(i).Derive()
					where := fmt.Sprintf("window=%d tab=%d", key, i)
					if prev, ok := seen[id]; ok {
						t.Fatalf("cross-window widget-id collision %#016x: %s vs %s", id, prev, where)
					}
					seen[id] = where
				}
			}
		}
	}
}

// TestWindowhostInstanceSaltDistinct is a fast sanity check that the salt
// itself is unique per window key over the range a long-lived host reaches.
func TestWindowhostInstanceSaltDistinct(t *testing.T) {
	seen := make(map[uint64]uint64, 4096)
	for key := uint64(1); key <= 4096; key++ {
		salt := windowhostInstanceSalt(WindowKeyT(key))
		if prev, ok := seen[salt]; ok {
			t.Fatalf("salt collision %#016x for keys %d and %d", salt, prev, key)
		}
		seen[salt] = key
	}
}
