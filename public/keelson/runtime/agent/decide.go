package agent

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// The decision table (ADR-0283 §SD2; ADR-0310 §SD11 step 8): the rows the
// dispatcher's check, its window-verb and launch checks, and the analytics
// surface's Classify functions share, in the dispatcher's order, so the
// picture the surface draws cannot drift from the decision a call meets.
// What the moment of the call adds — another task's hold, a pause, the
// budget, consent, suggest mode — follows in the dispatcher alone and is
// not the surface's.

// decisionE is one row's verdict.
type decisionE uint8

const (
	// decisionGranted lets the call go on to what the moment decides.
	decisionGranted decisionE = iota
	// decisionAboveCeiling: the coordinator's settings refuse it whatever
	// the grant holds.
	decisionAboveCeiling
	// decisionNotGranted: no grant entry covers the instance, the app or
	// the desktop.
	decisionNotGranted
	// decisionNotGrantedOperation: an entry covers the instance, not the
	// operation.
	decisionNotGrantedOperation
	// decisionObserveOnly: covered in a mode too low for the effect.
	decisionObserveOnly
	// decisionSpent: a launch whose count is used up; the surface shows the
	// grant, the calls its use.
	decisionSpent
)

// cellStatus is the row as the surface shows it.
func (inst decisionE) cellStatus() (s CellStatusE) {
	switch inst {
	case decisionAboveCeiling:
		return CellStatusAboveCeiling
	case decisionNotGranted, decisionNotGrantedOperation:
		return CellStatusNotGranted
	case decisionObserveOnly:
		return CellStatusObserveOnly
	}
	return CellStatusGranted
}

// operationFacts are what decides an operation call as far as the grant and
// the ceiling go: whether an entry covers the instance, whether it names
// the operation, its mode, and the operation's effect.
type operationFacts struct {
	covered  bool
	coversOp bool
	mode     ModeE
	effect   app.OperationEffectE
}

// decideOperation is the dispatcher's order, from the ceiling down to the
// mode (ADR-0280, ADR-0269 §SD5).
func decideOperation(c *Ceiling, f operationFacts) (d decisionE) {
	switch {
	case c.refuseEffect(f.effect) != "":
		return decisionAboveCeiling
	case !f.covered:
		return decisionNotGranted
	case !f.coversOp:
		return decisionNotGrantedOperation
	case f.effect != app.OperationEffectNone && f.mode == ModeObserve:
		return decisionObserveOnly
	}
	return decisionGranted
}

// decideLaunch decides opening a window of an app: granted says whether the
// grant names a launch for it, count and used its allowance.
func decideLaunch(c *Ceiling, granted bool, count int, used int) (d decisionE) {
	switch {
	case c.refuseLaunch() != "":
		return decisionAboveCeiling
	case !granted || count == 0:
		return decisionNotGranted
	case used >= count:
		return decisionSpent
	}
	return decisionGranted
}

// decideArrange decides arranging the desktop under the task's desktop
// mode (ADR-0276 §SD4).
func decideArrange(c *Ceiling, desktop ModeE) (d decisionE) {
	switch {
	case c.refuseDesktop() != "":
		return decisionAboveCeiling
	case desktop == ModeUnspecified:
		return decisionNotGranted
	case desktop != ModeAct:
		return decisionObserveOnly
	}
	return decisionGranted
}

// decideWindowVerb decides raising or placing a window: covered says an
// entry names it, mode the entry's mode.
func decideWindowVerb(c *Ceiling, covered bool, mode ModeE) (d decisionE) {
	switch {
	case !covered:
		return decisionNotGranted
	case mode == ModeAct && c.refuseMode(ModeAct) != "":
		return decisionAboveCeiling
	case mode != ModeAct:
		return decisionObserveOnly
	}
	return decisionGranted
}
