package treemap

import (
	"fmt"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// AnimStateE is the lifecycle state of the zoom animation. The transition
// table (animTransitions) is the single source of truth for valid state
// changes; every transition() call is checked against it and panics on
// programmer error so misuse fails loudly during development.
type AnimStateE uint8

const (
	// AnimStateIdle — no animation in flight.
	AnimStateIdle AnimStateE = iota
	// AnimStateRunning — animation is active; renderBounds is interpolated.
	AnimStateRunning
)

func (inst AnimStateE) String() string {
	switch inst {
	case AnimStateIdle:
		return "AnimStateIdle"
	case AnimStateRunning:
		return "AnimStateRunning"
	}
	return fmt.Sprintf("AnimStateE(%d)", uint8(inst))
}

// animTransitions maps each state to the set of states it can validly
// transition to. Self-loops are explicit (AnimStateRunning→AnimStateRunning lets a
// new Start() retrigger mid-animation).
var animTransitions = map[AnimStateE][]AnimStateE{
	AnimStateIdle:    {AnimStateRunning},
	AnimStateRunning: {AnimStateIdle, AnimStateRunning},
}

const animDoneEps = 0.002

// animMachine encapsulates the zoom-from-rect animation state. It owns the
// from-rect, the egui tween target flag, and the current interpolated value
// — fields that previously lived loose on Treemap as animActive/animTarget/
// animFromRect/animT.
type animMachine struct {
	state    AnimStateE
	target   bool        // egui tween target — flipped on every Start so the underlying value alternates direction
	fromRect layout.Rect // starting rect for the interpolation
	t        float64     // egui's animated value, written by AnimateBoolWithTimeBind via databinding
}

// transition validates the state change against animTransitions and updates
// state, panicking on any disallowed transition (programmer error per the
// validation policy).
func (inst *animMachine) transition(to AnimStateE) {
	for _, allowed := range animTransitions[inst.state] {
		if allowed == to {
			inst.state = to
			return
		}
	}
	panic(fmt.Sprintf("treemap: invalid anim transition %v → %v", inst.state, to))
}

// State returns the current state. Exposed for tests and observability.
func (inst *animMachine) State() AnimStateE { return inst.state }

// IsRunning reports whether an animation is currently in flight.
func (inst *animMachine) IsRunning() bool { return inst.state == AnimStateRunning }

// Start initiates a new zoom animation from fromRect to the full container.
// Idempotent at the state level (Running→Running is allowed) so callers can
// retrigger mid-animation without bookkeeping.
func (inst *animMachine) Start(fromRect layout.Rect) {
	inst.transition(AnimStateRunning)
	inst.fromRect = fromRect
	inst.target = !inst.target
}

// TPtr returns a pointer to the egui-driven tween value so the renderer can
// register it as a databinding target. The pointer must not be retained
// across frames (animMachine may relocate in memory if the owner is reset).
func (inst *animMachine) TPtr() *float64 { return &inst.t }

// Target returns the current egui tween target. egui drives inst.t toward
// (target?1.0:0.0); the renderer flips target on each Start so the same
// physical motion (small→large) corresponds to alternating egui values.
func (inst *animMachine) Target() bool { return inst.target }

// Tick computes the current animation progress as a 0..1 effective value
// (always 0 at start, 1 at end regardless of egui's tween direction) and
// transitions to AnimStateIdle when the animation completes. Returns the
// effective t and whether the animation is still running this frame.
func (inst *animMachine) Tick() (effT float64, running bool) {
	if inst.state == AnimStateIdle {
		return 0, false
	}
	if inst.target {
		effT = inst.t
	} else {
		effT = 1.0 - inst.t
	}
	if effT >= 1.0-animDoneEps {
		inst.transition(AnimStateIdle)
		return 1.0, false
	}
	return effT, true
}

// FromRect returns the rect the current animation is interpolating from.
// Undefined when state is AnimStateIdle.
func (inst *animMachine) FromRect() layout.Rect { return inst.fromRect }
