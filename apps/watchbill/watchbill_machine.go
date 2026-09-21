package watchbill

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
)

// newJobMachine is the job's state machine (ADR-0223 §SD2) as the state
// chip draws it. The window mirrors the selected row's state into it —
// it never drives a transition itself — so the rules are documentation
// for the popup's graph, and the retry edges back to queued are what an
// operator can cause.
func newJobMachine() *fsmview.Machine[string] {
	m := fsmview.NewMachine[string](watchbillstore.StateQueued, 32,
		fsmview.WithStateOrder(watchbillstore.AllStates))
	m.AddRule(watchbillstore.StateQueued, watchbillstore.StateRunning, watchbillstore.StateCancelled).
		AddRule(watchbillstore.StateRunning, watchbillstore.StateSucceeded, watchbillstore.StateFailed, watchbillstore.StateDiscarded, watchbillstore.StateCancel, watchbillstore.StateAbandoned).
		AddRule(watchbillstore.StateFailed, watchbillstore.StateQueued).
		AddRule(watchbillstore.StateCancel, watchbillstore.StateCancelled).
		AddRule(watchbillstore.StateAbandoned, watchbillstore.StateQueued).
		AddRule(watchbillstore.StateSucceeded, watchbillstore.StateQueued).
		AddRule(watchbillstore.StateDiscarded, watchbillstore.StateQueued).
		AddRule(watchbillstore.StateCancelled, watchbillstore.StateQueued)
	m.EdgeLabel(watchbillstore.StateFailed, watchbillstore.StateQueued, "policy")
	m.EdgeLabel(watchbillstore.StateAbandoned, watchbillstore.StateQueued, "sweep")
	m.EdgeLabel(watchbillstore.StateSucceeded, watchbillstore.StateQueued, "retry")
	m.EdgeLabel(watchbillstore.StateDiscarded, watchbillstore.StateQueued, "retry")
	m.EdgeLabel(watchbillstore.StateCancelled, watchbillstore.StateQueued, "retry")
	return m
}
