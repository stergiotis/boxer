package fsmview

import (
	"slices"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
)

var _ opfsm.SourceI = (*Machine[string])(nil)

// OpsCurrent is the current state's label.
func (inst *Machine[T]) OpsCurrent() (state string) {
	return inst.Label(inst.Current())
}

// OpsView reports the machine by its states' labels: the current state,
// every state and allowed transition, and at most history latest steps,
// oldest first.
func (inst *Machine[T]) OpsView(history int) (v opfsm.View) {
	v.Current = inst.OpsCurrent()
	for s := range inst.States() {
		v.States = append(v.States, inst.Label(s))
	}
	for e, label := range inst.Edges() {
		v.Edges = append(v.Edges, opfsm.Edge{From: inst.Label(e.From), To: inst.Label(e.To), Label: label})
	}
	if history > 0 {
		for t := range inst.HistoryReverse() {
			if len(v.History) == history {
				break
			}
			v.History = append(v.History, opfsm.Step{From: inst.Label(t.From), To: inst.Label(t.To), At: t.At})
		}
		slices.Reverse(v.History)
	}
	return
}
