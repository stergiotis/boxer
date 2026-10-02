// Package fsmops mounts a state machine into an app's operations catalog,
// so every app that holds one offers an agent the same operations for it.
package fsmops

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
)

// Options configures a mount.
type Options struct {
	// History is how many latest steps the state query returns; 0 leaves
	// the history out, for a machine whose steps do not belong together.
	History int
}

// Mount declares, under name, a resource name_state holding the machine's
// current state and two queries: name_state, the current state with the
// transitions allowed from it and the latest steps, and name_machine, every
// state and allowed transition. summary says what the machine tracks.
// source returns the machine of an instance, or nil when it has none to
// report.
func Mount[A any, S any](set *appops.Set[A, S], name string, summary string, source func(inst A) opfsm.SourceI, opts Options) {
	stateName, machineName := name+"_state", name+"_machine"
	set.Resource(stateName, summary+": its current state", func(inst A) any {
		if m := source(inst); m != nil {
			return m.OpsCurrent()
		}
		return ""
	})
	set.Mount(name, func(inst A) any {
		if m := source(inst); m != nil {
			return m.OpsView(opts.History)
		}
		return opfsm.View{}
	})
	appops.MountedQuery(set, name, app.OperationSpec{Name: stateName, Version: 1,
		Summary: "the current state of " + summary + ", the transitions allowed from it and the latest ones",
		Reads:   []string{stateName}, Agents: true},
		func(v any, in appops.None) (out opfsm.State, err error) {
			view, _ := v.(opfsm.View)
			out = opfsm.StateOf(view)
			return
		})
	appops.MountedQuery(set, name, app.OperationSpec{Name: machineName, Version: 1,
		Summary: "every state and allowed transition of " + summary,
		Reads:   []string{stateName}, Agents: true},
		func(v any, in appops.None) (out opfsm.Machine, err error) {
			view, _ := v.(opfsm.View)
			out = opfsm.Machine{States: view.States, Edges: view.Edges}
			return
		})
}
