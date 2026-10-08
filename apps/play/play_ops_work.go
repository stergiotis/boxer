package play

// Work control and the remaining commands (ADR-0270, update of 2026-10-05):
// stop a run or a projection the task started, publish a projection as data
// with the person's confirmation, delete a signal, and set the run options.
//
// A task stops only work it caused. The run and the projection each record
// the task that asked for them; the person's Cancel stops either, whoever
// started it. A job handle the host's cancel service stops (ADR-0269 §SD3)
// would serve every lane at once, but that service withdraws queued calls
// and proposals only, so the commands are play's own.

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

const (
	opCancelRun         = "cancel_run"
	opCancelProjection  = "cancel_projection"
	opPublishProjection = "publish_projection"
	opDeleteSignal      = "delete_signal"
	opSetRunOptions     = "set_run_options"

	opsResRunOptions = "run_options"
)

// DeleteSignalArgs is delete_signal's argument.
type DeleteSignalArgs struct {
	Name string `desc:"the signal name, as get_state lists it"`
}

// SetRunOptionsArgs is set_run_options' argument. A field left out keeps
// the person's setting.
type SetRunOptionsArgs struct {
	Conditions *bool `json:",omitzero" desc:"turn on or off the rewrite that adds one column per condition of an information-retrieval query's WHERE, so the result says which condition held for each row; get_state's conditions says whether the endpoint offers it and its setting"`
}

// callTask is the task a call is made for, empty for the person's.
func callTask(call app.OperationCall) string {
	if call.OnBehalfOf == nil {
		return ""
	}
	return call.OnBehalfOf.Task
}

// cancelRun stops the main run in flight, or withdraws a run an agent asked
// for that has not started. task is the caller's; empty is the person, who
// may stop any run.
func (inst *PlayApp) cancelRun(task string) (err error) {
	if !inst.graph.MainLoading() {
		if inst.requestRun && inst.agentRunRequested && (task == "" || (inst.agentDriven != nil && inst.agentDriven.Task == task)) {
			inst.requestRun, inst.agentRunRequested = false, false
			inst.requestSubquery, inst.requestStatement = false, 0
			return
		}
		return app.RefuseOperation("no run is in flight")
	}
	if task != "" {
		obo, running := inst.graph.mainLane.RunningAgent()
		if running && (obo == nil || obo.Task != task) {
			return app.RefuseOperation("the run in flight is not this task's; only the person cancels it")
		}
	}
	inst.graph.CancelMain()
	return
}

// personCancelRun is the top bar's Cancel.
func (inst *PlayApp) personCancelRun() {
	playGesture(inst, opCancelRun, appops.None{}, func() { inst.graph.CancelMain() })
}

// cancelProjection stops the Projection pane's run, or withdraws a compute
// asked for that the pane has not started. task as for cancelRun.
func (inst *PlayApp) cancelProjection(task string) (err error) {
	pj := inst.projector
	if pj == nil {
		return app.RefuseOperation("this window has no Projection pane")
	}
	if task != "" && pj.runTask != task {
		if pj.computeRequested || projectorBusy(pj.Snapshot().status) {
			return app.RefuseOperation("the projection is not this task's; only the person cancels it")
		}
	}
	if pj.computeRequested {
		pj.computeRequested = false
		return
	}
	if !projectorBusy(pj.Snapshot().status) {
		return app.RefuseOperation("no projection is running")
	}
	pj.Cancel()
	return
}

func projectorBusy(st projectorStatusE) bool {
	return st == projectorStatusExtracting || st == projectorStatusRunning
}

// personCancelProjection is the pane's Cancel button.
func (inst *PlayApp) personCancelProjection() {
	playGesture(inst, opCancelProjection, appops.None{}, func() { _ = inst.cancelProjection("") })
}

// projectionPublishNow is what a publish round reads, taken from the run and
// the result the pane is fed. release gives back the record.
func (inst *PlayApp) projectionPublishNow() (in projectionPublishInput, release func(), err error) {
	release = func() {}
	pj := inst.projector
	if pj == nil {
		return in, release, app.RefuseOperation("this window has no Projection pane")
	}
	if inst.bus == nil || inst.projPublish == nil {
		return in, release, app.RefuseOperation("this window has no bus to publish datasets on")
	}
	snap := pj.Snapshot()
	if snap.status != projectorStatusDone || snap.result == nil {
		return in, release, app.RefuseOperation("no projection to publish: compute_projection runs one, get_projection says when it is done")
	}
	if publishing, _, _, _ := inst.projPublish.status(); publishing {
		return in, release, app.ConflictOperation("a publish is in flight; get_projection says when it lands")
	}
	// The positions live in the widget, which holds the run's layout only
	// once the pane has drawn it.
	if pj.builtVersion != snap.version {
		return in, release, app.RefuseOperation("the run's layout has not been drawn yet: show_pane projection, then publish")
	}
	rec, _ := inst.selectionRecord(inst.resolvedTabNode(projectionPaneId))
	if rec == nil {
		return in, release, app.RefuseOperation("the result the Projection pane is fed is gone; compute_projection again")
	}
	res := snap.result
	for _, r := range res.rows {
		if r < 0 || r >= rec.NumRows() {
			rec.Release()
			return in, release, app.RefuseOperation("the result the Projection pane is fed moved since the run; compute_projection again")
		}
	}
	_, x, y := pj.view.PositionColumns(nil, nil, nil)
	in = projectionPublishInput{rec: rec, res: res, depth: pj.explainDepth, perCluster: pj.explainPerCluster, x: x, y: y}
	return in, rec.Release, nil
}

// personPublishProjection is the pane's publish button; direct is the
// round without a host serving the catalog.
func (inst *PlayApp) personPublishProjection(direct func()) {
	playGesture(inst, opPublishProjection, appops.None{}, func() {
		inst.projPublishQuiet = false
		direct()
	})
}

// personDeleteSignal is the Signals section's ×.
func (inst *PlayApp) personDeleteSignal(name SignalID) {
	playGesture(inst, opDeleteSignal, DeleteSignalArgs{Name: string(name)}, func() { inst.graph.deleteSignal(name) })
}

// conditionsOffered says whether the window's endpoint has the conditions
// rewrite to turn on.
func (inst *PlayApp) conditionsOffered() bool {
	return inst.client != nil && inst.client.conditionsPass.Apply != nil
}

// conditionsState is get_state's conditions: empty when not offered.
func (inst *PlayApp) conditionsState() string {
	switch {
	case !inst.conditionsOffered():
		return ""
	case inst.exposeConditions:
		return "on"
	}
	return "off"
}

func addWorkOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Command(s, app.OperationSpec{Name: opCancelRun, Version: 1,
		Summary: "stop the run this task started, while it is in flight",
		Effect:  app.OperationEffectRun, Writes: []string{opsResResult}, Agents: true,
		Gesture: "the Cancel button beside the spinner",
		Follows: []string{"the run ends cancelled; get_state's result says so once it lands",
			"a run the person started, or another task, is refused: only the person cancels it",
			"a run asked for in this frame that has not started is withdrawn"}},
		func(inst *PlayLauncher, call app.OperationCall, in appops.None) (appops.None, error) {
			if inst.inner == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			task := callTask(call)
			if task == "" && call.Writer != opwire.WriterPerson {
				return appops.None{}, app.RefuseOperation("a cancel through the catalog is an agent's, and carries its context")
			}
			return appops.None{}, inst.inner.cancelRun(task)
		})
	appops.Command(s, app.OperationSpec{Name: opCancelProjection, Version: 1,
		Summary: "stop the Projection pane's run this task started",
		Effect:  app.OperationEffectView, Writes: []string{opsResProjection}, Agents: true,
		Gesture: "the Cancel button of the Projection pane",
		Follows: []string{"get_projection reports cancelling, then cancelled",
			"a projection the person started, or another task, is refused: only the person cancels it"}},
		func(inst *PlayLauncher, call app.OperationCall, in appops.None) (appops.None, error) {
			if inst.inner == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			task := callTask(call)
			if task == "" && call.Writer != opwire.WriterPerson {
				return appops.None{}, app.RefuseOperation("a cancel through the catalog is an agent's, and carries its context")
			}
			return appops.None{}, inst.inner.cancelProjection(task)
		})
	// Consequential: the datasets leave the window, onto the bus, where any
	// window can read them; the person confirms each publish.
	appops.Command(s, app.OperationSpec{Name: opPublishProjection, Version: 1,
		Summary: "publish the finished projection as two ad-hoc datasets, one row per entity and one per cluster rule, and bind them in this window as keelson('projection') and keelson('projection_rules')",
		Effect:  app.OperationEffectConsequential, Reads: []string{opsResProjection}, Writes: []string{opsResDatasets}, Agents: true,
		Gesture: "the publish as dataset button of the Projection pane",
		Follows: []string{"get_projection's published names the handles and row counts once the publish lands, or why it failed",
			"for an agent the scaffold query is not inserted into the buffer; write one with set_sql over keelson('projection')"}},
		func(inst *PlayLauncher, call app.OperationCall, in appops.None) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			pin, release, err := p.projectionPublishNow()
			if err != nil {
				return appops.None{}, err
			}
			defer release()
			p.projPublishQuiet = call.Writer != opwire.WriterPerson
			p.publishProjection(pin)
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opDeleteSignal, Version: 1,
		Summary: "delete one held signal, so a parameter reading it falls back to unfilled or its default",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResSignals}, Agents: true,
		Gesture: "the × of a signal in the Signals section",
		Follows: []string{"Live reruns a query that reads the signal",
			"a signal a pane publishes (tl_from, vp_min_x, gv_selection, …) is refused, as set_signal refuses it"}},
		func(inst *PlayLauncher, call app.OperationCall, in DeleteSignalArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			name := SignalID(strings.TrimSpace(in.Name))
			if name == "" {
				return appops.None{}, app.RefuseOperation("a signal needs a name")
			}
			if call.Writer != opwire.WriterPerson {
				if reason := paneOutputRefusal(name); reason != "" {
					return appops.None{}, app.RefuseOperation(reason)
				}
				if _, held := p.graph.signals().Get(name); !held {
					return appops.None{}, app.RefuseOperation("nothing holds a signal " + string(name))
				}
			}
			p.graph.deleteSignal(name)
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
	s.Resource(opsResRunOptions, "the run options: the conditions rewrite", func(inst *PlayLauncher) any {
		if inst.inner == nil {
			return false
		}
		return inst.inner.exposeConditions
	})
	appops.Command(s, app.OperationSpec{Name: opSetRunOptions, Version: 1,
		Summary: "set the run options: the conditions rewrite, which adds one column per WHERE condition",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResRunOptions}, Agents: true,
		Follows: []string{"the next run ships the rewritten statement; validate_sql and trace_rewrite show it"}},
		func(inst *PlayLauncher, call app.OperationCall, in SetRunOptionsArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			if in.Conditions == nil {
				return appops.None{}, nil
			}
			if !p.conditionsOffered() {
				return appops.None{}, app.RefuseOperation("this window's endpoint offers no conditions rewrite")
			}
			p.exposeConditions = *in.Conditions
			p.client.SetExposeConditions(*in.Conditions)
			return appops.None{}, nil
		})
}
