package lazypane

import "testing"

// steps runs the pure phase machine over a rendered-signal sequence and
// returns the (skip, revealed) trace.
func steps(p *Pane, rendered ...bool) (skips []bool, reveals []bool) {
	skips = make([]bool, 0, len(rendered))
	reveals = make([]bool, 0, len(rendered))
	for _, r := range rendered {
		s, rev := p.step(r)
		skips = append(skips, s)
		reveals = append(reveals, rev)
	}
	return
}

func expectTrace(t *testing.T, name string, got []bool, want []bool) {
	t.Helper()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: frame %d: got %v, want %v (full: %v)", name, i, got[i], want[i], got)
		}
	}
}

// Cold start: the r21 map is empty on frame 1, so even an active pane shows
// one placeholder frame, then reveals.
func TestColdStartRevealsAfterOneFrame(t *testing.T) {
	p := &Pane{}
	skips, reveals := steps(p, false, true, true)
	expectTrace(t, "skip", skips, []bool{true, false, false})
	expectTrace(t, "reveal", reveals, []bool{false, true, false})
}

// Steady hidden: probe never reported, placeholder every frame.
func TestSteadyHiddenSkips(t *testing.T) {
	p := &Pane{}
	skips, _ := steps(p, false, false, false, false)
	expectTrace(t, "skip", skips, []bool{true, true, true, true})
}

// Hidden→visible→hidden round trip; reveal fires exactly once per reveal.
func TestHideRevealCycle(t *testing.T) {
	p := &Pane{}
	skips, reveals := steps(p,
		false, // hidden
		true,  // placeholder was rendered → reveal
		true,  // steady body
		false, // tab switched away (body not rendered last frame)
		false, // steady hidden
		true,  // re-revealed
	)
	expectTrace(t, "skip", skips, []bool{true, false, false, true, true, false})
	expectTrace(t, "reveal", reveals, []bool{false, true, false, false, false, true})
}

// HoldFrames=2: reveal is delayed by exactly two extra placeholder frames.
func TestHoldFramesDelayReveal(t *testing.T) {
	p := &Pane{HoldFrames: 2}
	skips, reveals := steps(p,
		false, // hidden
		true,  // enters warming, hold 2
		true,  // warming, hold 1
		true,  // hold exhausted → reveal
		true,  // steady body
	)
	expectTrace(t, "skip", skips, []bool{true, true, true, false, false})
	expectTrace(t, "reveal", reveals, []bool{false, false, false, true, false})
}

// Hiding during warm-up resets the hold: the next reveal starts a fresh one.
func TestWarmingInterruptedByHideResets(t *testing.T) {
	p := &Pane{HoldFrames: 2}
	skips, reveals := steps(p,
		false, // hidden
		true,  // warming, hold 2
		false, // hidden again mid-warmup
		true,  // warming restarts, hold 2
		true,  // hold 1
		true,  // reveal
	)
	expectTrace(t, "skip", skips, []bool{true, true, true, true, true, false})
	expectTrace(t, "reveal", reveals, []bool{false, false, false, false, false, true})
}

// A live pane that stops being rendered skips immediately (its next probe
// report is what re-reveals it) — no zombie body frames.
func TestLiveToHiddenSkipsImmediately(t *testing.T) {
	p := &Pane{}
	skips, _ := steps(p, false, true, true, false)
	expectTrace(t, "skip", skips, []bool{true, false, false, true})
}
