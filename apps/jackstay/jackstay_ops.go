package jackstay

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/fsmops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
)

// The window's operations catalog (ADR-0269): the plan's phase, as the state
// chip draws it — plan_state and plan_machine, the operations every app's
// mounted machine offers. Nothing here moves the plan; the steps that write
// to a server stay behind the window's own confirmations.

// opsSnap is empty: the mounted machine is read on the render goroutine and
// no query reads a snapshot.
type opsSnap struct{}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet(func(inst *App) opsSnap { return opsSnap{} })
	fsmops.Mount(s, "plan", "the plan's phase, from discovering the servers to the sync", func(inst *App) opfsm.SourceI {
		if inst.phaseMachine == nil {
			return nil
		}
		return inst.phaseMachine
	}, fsmops.Options{History: 16})
	return
}()

// Operations serves the catalog for this window.
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)
