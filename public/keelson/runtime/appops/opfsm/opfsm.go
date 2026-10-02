// Package opfsm is the vocabulary a state machine speaks in an app's
// operations catalog: plain values a machine reports and the operations
// that [fsmops.Mount] declares return. It has no dependencies, so a widget
// can report itself without importing the operations runtime.
package opfsm

import "time"

// Edge is a transition the machine allows, with what causes it.
type Edge struct {
	From  string `desc:"the state it leaves"`
	To    string `desc:"the state it enters"`
	Label string `desc:"what causes it, when the machine names it"`
}

// Step is one transition the machine went through.
type Step struct {
	From string    `desc:"the state it left"`
	To   string    `desc:"the state it entered"`
	At   time.Time `desc:"when"`
}

// View is a machine as it stands: its current state, every state and
// allowed transition, and its latest steps, oldest first.
type View struct {
	Current string
	States  []string
	Edges   []Edge
	History []Step
}

// SourceI is a state machine that reports itself. OpsCurrent is cheap: it
// is read every frame to notice a change. OpsView is read only when a
// snapshot is taken for an agent, with at most history steps.
type SourceI interface {
	OpsCurrent() (state string)
	OpsView(history int) (v View)
}

// State is what the state query returns.
type State struct {
	Current string `desc:"the current state; empty when there is none to report"`
	Next    []Edge `desc:"the transitions allowed from it"`
	History []Step `desc:"the latest transitions, oldest first"`
}

// Machine is what the machine query returns.
type Machine struct {
	States []string `desc:"every state"`
	Edges  []Edge   `desc:"every allowed transition"`
}

// StateOf is the state query's answer over a view.
func StateOf(v View) (s State) {
	s = State{Current: v.Current, History: v.History}
	for _, e := range v.Edges {
		if e.From == v.Current {
			s.Next = append(s.Next, e)
		}
	}
	return
}
