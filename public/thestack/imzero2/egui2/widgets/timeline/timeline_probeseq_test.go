package timeline

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// TestProbeSeqIsPerInstance guards the r21 slot against the shape the r18
// retirement removed. Embedders pass a CONSTANT scopeKey ("play-timeline"), so
// c.ProbeSeq(scopeKey, role) alone is identical in two windows of one app —
// both timelines would arm and read one pane probe, and each would render at
// whichever pane captured last, exactly as they did through r18.
//
// The two derivations below are the two ways an instance is separated: a stack
// with its own base salt (play's per-PlayApp mk() stacks) and a stack under a
// pushed per-window scope (windowhost's IdScope around Frame).
func TestProbeSeqIsPerInstance(t *testing.T) {
	const scope = "play-timeline"

	t.Run("base-salted stacks", func(t *testing.T) {
		mk := func(salt uint64) *c.WidgetIdStack {
			s := c.NewWidgetIdStack()
			s.SetBaseSalt(salt)
			return s
		}
		a := New(mk(0x1111_2222_3333_4444), scope, nil)
		b := New(mk(0x5555_6666_7777_8888), scope, nil)
		if a.probeSeq("timeline-pane") == b.probeSeq("timeline-pane") {
			t.Fatalf("two instances share one probe slot: %#016x", a.probeSeq("timeline-pane"))
		}
	})

	t.Run("per-window id scope", func(t *testing.T) {
		// Both instances are built from an unsalted stack, as an app taking
		// ctx.Ids() is: the separation has to come from the scope the host
		// pushes around Frame, which is why the salt is derived on first use
		// and not in New.
		seq := func(windowSalt uint64) uint64 {
			ids := c.NewWidgetIdStack()
			inst := New(ids, scope, nil)
			var got uint64
			for range c.IdScope(ids.PrepareHighEntropy(windowSalt)) {
				got = inst.probeSeq("timeline-pane")
			}
			return got
		}
		if s1, s2 := seq(0xaaaa_bbbb_cccc_dddd), seq(0x0f0f_0f0f_0f0f_0f0f); s1 == s2 {
			t.Fatalf("two windows share one probe slot: %#016x", s1)
		}
	})

	t.Run("stable across frames and distinct per role", func(t *testing.T) {
		inst := New(c.NewWidgetIdStack(), scope, nil)
		first := inst.probeSeq("timeline-pane")
		if again := inst.probeSeq("timeline-pane"); again != first {
			t.Fatalf("probe seq moved between calls: %#016x then %#016x", first, again)
		}
		if other := inst.probeSeq("some-other-role"); other == first {
			t.Fatalf("two roles share one slot: %#016x", other)
		}
	})
}
