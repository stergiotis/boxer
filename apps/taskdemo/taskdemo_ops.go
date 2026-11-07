package taskdemo

import (
	"context"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// The operations catalog (ADR-0269): one resource, the spawn controls; two
// queries over the controls and the tasks this window spawned; a command
// per control, a spawn that settles when its task has started (ADR-0358),
// and a cancel by id. The Start buttons go through the same spawn command
// (ADR-0269 §SD8), so an agent and a click cannot drift.
//
// list_tasks reads the window's own record of the tasks it spawned, which
// are the monitor's rows for this producer; the taskmonitor widget keeps
// its rows private, and a host bus may show it other producers' tasks too.
// deferred: a row accessor on taskmonitor, so the query can read the pane
// as drawn.

const (
	resControls = "controls"

	opGetView    = "get_view"
	opListTasks  = "list_tasks"
	opSetControl = "set_control"
	opSpawn      = "spawn"
	opCancelTask = "cancel_task"
)

// The controls set_control names, and the values the two boolean ones take.
const (
	controlUnit          = "unit"
	controlDuration      = "duration"
	controlSimulateError = "simulate_error"
	controlSlowFrame     = "slow_frame"
)

// Result bounds: what one inline result may carry, and how many records
// the window keeps for list_tasks.
const (
	opsMaxTasks     = 200
	opsDefaultTasks = 50
	opsMaxRecords   = 200
	opsMaxReasonLen = 512
)

// The record states list_tasks reports; the first two are a running task.
const (
	stateStarting   = "starting"
	stateRunning    = "running"
	stateDone       = "done"
	stateError      = "error"
	stateCancelled  = "cancelled"
	stateNotStarted = "not started"
)

// The state filters list_tasks takes beside an exact state.
const (
	filterRunning  = "running"
	filterFinished = "finished"
)

var allStates = []string{stateStarting, stateRunning, stateDone, stateError, stateCancelled, stateNotStarted}

// Refusal reasons; each names its remedy.
const (
	unknownUnitReason    = "the unit is one of items, bytes or steps"
	unknownControlReason = "the control is one of unit, duration, simulate_error or slow_frame"
	durationRangeReason  = "the duration is between 1 and 60 seconds"
	booleanReason        = "the value is true or false"
	unknownTaskReason    = "no task of that id was spawned by this window; list_tasks names them"
	noTaskApiReason      = "this host gives the window no task API, so nothing can be spawned or cancelled here"
	limitReason          = "limit is not negative"
	unknownStateReason   = "the state is running, finished, or one of " + stateStarting + ", " + stateRunning + ", " + stateDone + ", " + stateError + ", " + stateCancelled + ", " + stateNotStarted
)

// Controls is the controls resource's value: what the next spawn uses.
type Controls struct {
	Unit          string  `desc:"the shape spawn uses when its call names none: items, bytes or steps; a Start button sets it to its shape"`
	DurationSec   float64 `desc:"the duration of the next task, 1 to 60 seconds"`
	SimulateError bool    `desc:"the next task fails at 80% of its duration"`
	SlowFrame     bool    `desc:"every frame of this window sleeps 12 ms, to trip the host's slow-window notice"`
}

func (inst *App) controls() (v Controls) {
	v = Controls{Unit: inst.unit.String(), DurationSec: inst.durationSec, SimulateError: inst.simulateError, SlowFrame: inst.slowFrame}
	return
}

// taskRecord is the window's record of one task it spawned, written by the
// worker under recMu and read by the queries' snapshot.
type taskRecord struct {
	id         task.TaskIdT
	kind       string
	title      string
	unit       task.UnitE
	durationMs int64
	state      string
	current    uint64
	total      uint64
	reason     string
}

// TaskRow is one task of list_tasks.
type TaskRow struct {
	Id         string  `desc:"the task id; empty while the task is starting or when the spawn failed"`
	Kind       string  `desc:"demo.<unit>"`
	Title      string  `desc:"the task's title, as the monitor shows it"`
	Unit       string  `desc:"items, bytes or steps"`
	DurationMs int64   `desc:"the duration the task was spawned with"`
	State      string  `desc:"starting, running, done, error, cancelled or not started"`
	Current    uint64  `desc:"the last progress reported"`
	Total      uint64  `desc:"the task's total in its unit"`
	Fraction   float64 `desc:"current over total, 0 to 1"`
	Reason     string  `desc:"why an errored task failed or a spawn did not start, cut at 512 characters; empty otherwise"`
}

func rowOf(r *taskRecord) (row TaskRow) {
	row = TaskRow{Id: string(r.id), Kind: r.kind, Title: r.title, Unit: r.unit.String(), DurationMs: r.durationMs,
		State: r.state, Current: r.current, Total: r.total, Reason: appops.CutText(r.reason, opsMaxReasonLen)}
	if r.total > 0 {
		row.Fraction = float64(r.current) / float64(r.total)
	}
	return
}

// ListTasksArgs filters the window's tasks; every field is optional.
type ListTasksArgs struct {
	State string `json:",omitzero" desc:"running (starting or running), finished (done, error, cancelled or not started), or one state exactly; empty keeps every task"`
	Limit int    `json:",omitzero" desc:"tasks to return, oldest first; default 50, at most 200"`
}

// ListTasksResult is list_tasks's result.
type ListTasksResult struct {
	Matched   int       `desc:"tasks of this window that match the arguments"`
	Tasks     []TaskRow `desc:"the first limit matches, in spawn order"`
	Truncated bool      `desc:"matched tasks were left out by the limit"`
}

// View is get_view's result.
type View struct {
	Controls Controls `desc:"what the next spawn uses"`
	Tasks    bool     `desc:"the host gives the window a task API; false where nothing can be spawned"`
	Inflight int      `desc:"rows in the monitor's In-flight pane, every producer's"`
	History  int      `desc:"rows in the monitor's History pane"`
	Spawned  int      `desc:"tasks this window has spawned and still records, at most 200"`
}

// SetControlArgs sets one control to a value; a repeat is applied.
type SetControlArgs struct {
	Control string `desc:"unit, duration, simulate_error or slow_frame"`
	Value   string `desc:"items, bytes or steps for unit; 1 to 60 for duration; true or false for the other two"`
}

// SpawnArgs names the shape to spawn.
type SpawnArgs struct {
	Unit string `json:",omitzero" desc:"items, bytes or steps; empty uses the controls' unit"`
}

// SpawnResult is spawn's result, once the task has started.
type SpawnResult struct {
	Id    string `desc:"the task id, as the monitor's row shows it"`
	Kind  string `desc:"demo.<unit>"`
	Title string `desc:"the task's title"`
}

// CancelTaskArgs names the task to cancel.
type CancelTaskArgs struct {
	Id string `desc:"a task id from list_tasks"`
}

// opsSnap is what queries read: the controls, whether tasks can run, the
// monitor's counts and a copy of the window's records.
type opsSnap struct {
	controls Controls
	tasks    bool
	inflight int
	history  int
	records  []TaskRow
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.controls = inst.controls()
	sn.tasks = inst.tasks != nil
	if inst.monitor != nil {
		sn.inflight, sn.history = inst.monitor.InflightCount(), inst.monitor.HistoryCount()
	}
	inst.recMu.Lock()
	sn.records = make([]TaskRow, 0, len(inst.records))
	for _, r := range inst.records {
		sn.records = append(sn.records, rowOf(r))
	}
	inst.recMu.Unlock()
	return
}

// record adds a task's record, dropping the oldest finished one past the
// cap; a window that spawns more than the cap of running tasks keeps them
// all until they finish.
func (inst *App) record(r *taskRecord) {
	inst.recMu.Lock()
	defer inst.recMu.Unlock()
	inst.records = append(inst.records, r)
	if len(inst.records) > opsMaxRecords {
		for i, old := range inst.records {
			if !isRunning(old.state) {
				inst.records = append(inst.records[:i], inst.records[i+1:]...)
				break
			}
		}
	}
}

// update writes a record from the worker.
func (inst *App) update(r *taskRecord, fn func(r *taskRecord)) {
	inst.recMu.Lock()
	fn(r)
	inst.recMu.Unlock()
}

// findRecord reads one record's row by id; false when the window never
// spawned it or the spawn has not returned an id yet.
func (inst *App) findRecord(id string) (row TaskRow, found bool) {
	inst.recMu.Lock()
	defer inst.recMu.Unlock()
	for _, r := range inst.records {
		if r.id != "" && string(r.id) == id {
			return rowOf(r), true
		}
	}
	return
}

func isRunning(state string) (yes bool) { return state == stateStarting || state == stateRunning }

func matchesState(filter string, state string) (yes bool) {
	switch filter {
	case "":
		return true
	case filterRunning:
		return isRunning(state)
	case filterFinished:
		return !isRunning(state)
	}
	return filter == state
}

func knownState(filter string) (yes bool) {
	if filter == "" || filter == filterRunning || filter == filterFinished {
		return true
	}
	for _, s := range allStates {
		if s == filter {
			return true
		}
	}
	return
}

// parseUnit is task.ParseUnit with a refusal for an unknown name.
func parseUnit(name string) (u task.UnitE, err error) {
	if u = task.ParseUnit(name); u == task.UnitUnspecified {
		err = app.RefuseOperation(unknownUnitReason)
	}
	return
}

func parseBool(value string) (b bool, err error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		b = true
	case "false":
	default:
		err = app.RefuseOperation(booleanReason)
	}
	return
}

// setControl applies one control; it is set_control's handler and the
// restore path's validator.
func (inst *App) setControl(control string, value string) (err error) {
	switch control {
	case controlUnit:
		var u task.UnitE
		if u, err = parseUnit(value); err == nil {
			inst.unit = u
		}
	case controlDuration:
		sec, parseErr := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if parseErr != nil || sec < minDurationSec || sec > maxDurationSec {
			err = app.RefuseOperation(durationRangeReason)
			return
		}
		inst.durationSec = sec
	case controlSimulateError:
		var b bool
		if b, err = parseBool(value); err == nil {
			inst.simulateError = b
		}
	case controlSlowFrame:
		var b bool
		if b, err = parseBool(value); err == nil {
			inst.slowFrame = b
		}
	default:
		err = app.RefuseOperation(unknownControlReason)
	}
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resControls, "the spawn controls: the unit, the duration, simulate error and slow frame; what the next spawn uses",
		func(inst *App) any { return inst.controls() })
	s.Restorable(resControls, func(inst *App, v any) bool {
		ctl, ok := v.(Controls)
		if !ok {
			return false
		}
		u, err := parseUnit(ctl.Unit)
		if err != nil || ctl.DurationSec < minDurationSec || ctl.DurationSec > maxDurationSec {
			return false
		}
		inst.unit, inst.durationSec, inst.simulateError, inst.slowFrame = u, ctl.DurationSec, ctl.SimulateError, ctl.SlowFrame
		return true
	})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the spawn controls, whether the host gives the window tasks, and the monitor's counts",
		Reads:   []string{resControls}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Controls: sn.controls, Tasks: sn.tasks, Inflight: sn.inflight, History: sn.history, Spawned: len(sn.records)}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opListTasks, Version: 1,
		Summary: "list the tasks this window spawned, filtered by state, at most 200; the row names the id cancel_task takes",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ListTasksArgs) (out ListTasksResult, err error) {
			limit := in.Limit
			switch {
			case limit < 0:
				err = app.RefuseOperation(limitReason)
				return
			case limit == 0:
				limit = opsDefaultTasks
			case limit > opsMaxTasks:
				limit = opsMaxTasks
			}
			if !knownState(in.State) {
				err = app.RefuseOperation(unknownStateReason)
				return
			}
			for _, row := range sn.records {
				if !matchesState(in.State, row.State) {
					continue
				}
				out.Matched++
				if len(out.Tasks) < limit {
					out.Tasks = append(out.Tasks, row)
				}
			}
			out.Truncated = out.Matched > len(out.Tasks)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetControl, Version: 1,
		Summary: "set one spawn control to a value; a value out of its range is refused naming the range",
		Effect:  app.OperationEffectView, Writes: []string{resControls}, Agents: true,
		Gesture: "the duration slider and the two check boxes under Spawn"},
		func(inst *App, _ app.OperationCall, in SetControlArgs) (out appops.None, err error) {
			err = inst.setControl(in.Control, in.Value)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSpawn, Version: 1,
		Summary: "spawn a fake task of one shape with the controls' duration; the call settles when the task has started, with its id",
		Effect:  app.OperationEffectRun, Reads: []string{resControls}, Writes: []string{resControls}, Agents: true, Async: true,
		Gesture: "one of the three Start buttons",
		Follows: []string{"the task runs for the controls' duration and ends in the monitor's History pane; list_tasks follows it by id",
			"with simulate_error set the task fails at 80% of its duration"}},
		func(inst *App, call app.OperationCall, in SpawnArgs) (out SpawnResult, err error) {
			if inst.tasks == nil {
				err = app.RefuseOperation(noTaskApiReason)
				return
			}
			u := inst.unit
			if in.Unit != "" {
				if u, err = parseUnit(in.Unit); err != nil {
					return
				}
			}
			inst.unit = u
			rec := inst.startTask(u)
			if rec == nil {
				err = app.RefuseOperation("the window is closing; nothing starts once its scope retired")
				return
			}
			if call.Key != "" {
				if inst.spawns == nil {
					inst.spawns = make(map[string]*taskRecord)
				}
				inst.spawns[call.Key] = rec
			}
			return
		})
	appops.Settles(s, opSpawn, func(inst *App, call app.OperationCall) (out SpawnResult, settled bool, err error) {
		rec, ok := inst.spawns[call.Key]
		if !ok {
			return out, true, app.RefuseOperation("no spawn is running under that call")
		}
		inst.recMu.Lock()
		state, id, kind, title, reason := rec.state, rec.id, rec.kind, rec.title, rec.reason
		inst.recMu.Unlock()
		switch state {
		case stateStarting:
			return out, false, nil
		case stateNotStarted:
			delete(inst.spawns, call.Key)
			return out, true, app.RefuseOperation("the spawn failed: " + reason)
		}
		delete(inst.spawns, call.Key)
		out = SpawnResult{Id: string(id), Kind: kind, Title: title}
		return out, true, nil
	})
	s.Available(opSpawn, func(sn opsSnap) (ok bool, reason string) {
		if !sn.tasks {
			return false, noTaskApiReason
		}
		return true, ""
	})

	appops.Command(s, app.OperationSpec{Name: opCancelTask, Version: 1,
		Summary: "ask a running task of this window to cancel; it ends on its next tick and the monitor lists it as cancelled",
		Effect:  app.OperationEffectConsequential, Agents: true,
		Gesture: "the row's Cancel button in the monitor's In-flight pane",
		Follows: []string{"the cancel is published on the bus, where every observer of the task sees it"}},
		func(inst *App, _ app.OperationCall, in CancelTaskArgs) (out appops.None, err error) {
			if inst.tasks == nil {
				err = app.RefuseOperation(noTaskApiReason)
				return
			}
			row, found := inst.findRecord(in.Id)
			if !found {
				err = app.RefuseOperation(unknownTaskReason)
				return
			}
			if !isRunning(row.State) {
				err = app.RefuseOperation("task " + in.Id + " is " + row.State + "; cancel applies to a running task")
				return
			}
			inst.requestCancel(task.TaskIdT(in.Id))
			return
		})
	s.Available(opCancelTask, func(sn opsSnap) (ok bool, reason string) {
		if !sn.tasks {
			return false, noTaskApiReason
		}
		return true, ""
	})
	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

// requestCancel publishes the cancel off the render goroutine, as the
// monitor's own Cancel button does; nothing is sent once the scope retired,
// and then the task is gone with it.
func (inst *App) requestCancel(id task.TaskIdT) {
	tasks := inst.tasks
	inst.scope.Go(func(ctx context.Context) {
		if err := tasks.RequestCancel(id, "cancelled through the catalog"); err != nil {
			inst.logger.Warn().Err(err).Str("task_id", string(id)).Msg("taskdemo: cancel request failed")
		}
	})
}

// gestureOr routes the person's control through the catalog (ADR-0269
// §SD8), so the command log sees it as theirs, and applies it directly
// where no host serves the catalog (tour, tests).
func gestureOr[In any, Out any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, Out](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

// spawnGesture is a Start button: the spawn command as the person, which
// also sets the controls' unit to the button's shape.
func (inst *App) spawnGesture(unit task.UnitE) {
	gestureOr[SpawnArgs, SpawnResult](inst, opSpawn, SpawnArgs{Unit: unit.String()}, func() {
		inst.unit = unit
		inst.startTask(unit)
	})
}
