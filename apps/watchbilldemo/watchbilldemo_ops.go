package watchbilldemo

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	wb "github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
)

// The operations catalog (ADR-0269, keelson-app A1–A3): the controls are
// the window's one resource, the queries read the poller's last list, and
// enqueue is the W3 exemplar — an asynchronous, consequential command whose
// handler copies the call's OnBehalfOf onto the watchbill request, so a job
// an agent causes carries the delegation and runs under the agentic entry
// frame (ADR-0344 §SD2). Cancel and retry are not here: the Watchbill
// window's catalog has them across every kind, and a row of this demo's
// own kind is one of its rows.

const (
	resControls = "controls"

	opGetView    = "get_view"
	opListJobs   = "list_jobs"
	opSetControl = "set_control"
	opEnqueue    = "enqueue"
)

// Result bounds: what one inline list_jobs result may carry.
const (
	opsMaxJobs     = 200
	opsDefaultJobs = 50
)

// The fail control's two words: set, never toggled (A2).
const (
	endFail    = "fail"
	endSucceed = "succeed"
)

// controls is the resource's value: what Enqueue sends, comparable so the
// host can tell a change.
type controls struct {
	DurationSec float64
	Fail        bool
	Attempts    int
	Backoff     string
}

func (inst *App) controlsValue() (v controls) {
	v = controls{DurationSec: inst.durationSec, Fail: inst.simulateFail, Attempts: int(inst.maxAttempts), Backoff: backoffChoices[inst.backoffIdx]}
	return
}

// opsSnap is what queries read: the controls and the poller's last list,
// copied under the mutex.
type opsSnap struct {
	controls  controls
	jobs      []watchbillstore.Job
	lastError string
	lastNote  string
	refreshed time.Time
	inflight  int
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.controls = inst.controlsValue()
	inst.mu.Lock()
	sn.jobs = slices.Clone(inst.jobs)
	sn.lastError, sn.lastNote, sn.refreshed, sn.inflight = inst.lastError, inst.lastNote, inst.refreshed, inst.inflight
	inst.mu.Unlock()
	return
}

// Controls is the controls as get_view reports them.
type Controls struct {
	DurationSec float64 `desc:"how long a job sleeps, in seconds, 0.5 to 60"`
	End         string  `desc:"fail or succeed: how the attempt ends"`
	Attempts    int     `desc:"attempts allowed, 1 to 5"`
	Backoff     string  `desc:"none, linear or exponential, from a one-second base"`
}

// View is get_view's result: the controls and the state of the window's
// list.
type View struct {
	Controls  Controls  `desc:"what the Enqueue button sends"`
	Jobs      int       `desc:"rows in the last list, every kind and state"`
	OwnJobs   int       `desc:"rows of the demo's own kind, demo.sleep, in the last list"`
	ListedAt  time.Time `desc:"when the poller last listed the queue; zero before the first list"`
	LastError string    `desc:"the last list's or verb's error, empty when none"`
	Note      string    `desc:"the last verb's note"`
	Inflight  int       `desc:"verbs in flight"`
}

// ListJobsArgs filters the last list to the demo's own kind.
type ListJobsArgs struct {
	States []string `json:",omitzero" desc:"states to list; empty lists every state"`
	Limit  int      `json:",omitzero" desc:"rows to return, newest first as the window lists them; default 50, at most 200"`
}

// JobRow is one demo.sleep job as list_jobs returns it.
type JobRow struct {
	Id          string `desc:"the job id"`
	Subject     string `desc:"the duration, and fail when the attempt ends in error"`
	State       string `desc:"queued, running, succeeded, failed, discarded, cancel, cancelled or abandoned"`
	Attempt     uint32 `desc:"how many times it ran"`
	MaxAttempts uint32 `desc:"attempts allowed"`
	WorkerRun   string `desc:"the run that last ran it"`
	LastError   string `desc:"the last error, first line"`
	AgentCaused bool   `desc:"true when an agent's call enqueued it: the row carries the task and the call"`
}

// JobList is list_jobs' result.
type JobList struct {
	Jobs      []JobRow  `desc:"the demo's jobs the arguments select"`
	Matched   int       `desc:"how many of the demo's jobs match, whatever the limit"`
	Truncated bool      `desc:"true when more match than were returned"`
	ListedAt  time.Time `desc:"when the poller last listed the queue"`
}

// SetControlArgs sets one or more controls; a field left empty leaves its
// control as it is.
type SetControlArgs struct {
	DurationSec float64 `json:",omitzero" desc:"how long a job sleeps, in seconds, 0.5 to 60; 0 leaves it"`
	End         string  `json:",omitzero" desc:"fail or succeed: whether the attempt ends in error; empty leaves it"`
	Attempts    int     `json:",omitzero" desc:"attempts allowed, 1 to 5; 0 leaves it"`
	Backoff     string  `json:",omitzero" desc:"none, linear or exponential; empty leaves it"`
}

// EnqueueOutcome is what enqueue settles with once the worker accepted the
// job into the store.
type EnqueueOutcome struct {
	Id          string `desc:"the job id"`
	State       string `desc:"the job's state as the store accepted it"`
	Subject     string `desc:"the subject the job carries"`
	AgentCaused bool   `desc:"true when the call carried an on-behalf-of context: the worker attested it, and the row carries the task and the call"`
}

// enqueueOutcome is what one enqueue landed, keyed by the call that asked
// (ADR-0358 §SD1); done is false while the worker has not answered.
type enqueueOutcome struct {
	done bool
	job  watchbillstore.Job
	err  error
}

func endOf(fail bool) (end string) {
	if fail {
		return endFail
	}
	return endSucceed
}

func rowOf(j watchbillstore.Job) (r JobRow) {
	r = JobRow{Id: j.ID, Subject: j.Subject, State: j.State, Attempt: j.Attempt, MaxAttempts: j.MaxAttempts,
		WorkerRun: j.WorkerRun, LastError: firstLine(j.LastError), AgentCaused: j.AgentCaused()}
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resControls, "what the Enqueue button sends: duration, how the attempt ends, attempts allowed, backoff",
		func(inst *App) any { return inst.controlsValue() })
	s.Restorable(resControls, func(inst *App, v any) bool {
		cv, ok := v.(controls)
		if ok {
			ok = inst.applyControls(cv)
		}
		return ok
	})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the controls and the state of the window's list: how many rows, when, any error, verbs in flight",
		Reads:   []string{resControls}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			own := 0
			for _, j := range sn.jobs {
				if j.Kind == KindSleep {
					own++
				}
			}
			out = View{Controls: Controls{DurationSec: sn.controls.DurationSec, End: endOf(sn.controls.Fail), Attempts: sn.controls.Attempts, Backoff: sn.controls.Backoff},
				Jobs: len(sn.jobs), OwnJobs: own, ListedAt: sn.refreshed, LastError: sn.lastError, Note: sn.lastNote, Inflight: sn.inflight}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opListJobs, Version: 1,
		Summary: "list the demo's own jobs, kind " + KindSleep + ", from the window's last list, by state, at most 200; keelson('" + wb.TableJobs + "') is the full view across kinds",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ListJobsArgs) (out JobList, err error) {
			for _, st := range in.States {
				if !slices.Contains(watchbillstore.AllStates, st) {
					err = app.RefuseOperation("no state " + st + "; the states are " + strings.Join(watchbillstore.AllStates, ", "))
					return
				}
			}
			limit := in.Limit
			switch {
			case limit < 0:
				err = app.RefuseOperation("limit is not negative")
				return
			case limit == 0:
				limit = opsDefaultJobs
			case limit > opsMaxJobs:
				limit = opsMaxJobs
			}
			out.ListedAt = sn.refreshed
			for _, j := range sn.jobs {
				if j.Kind != KindSleep || (len(in.States) > 0 && !slices.Contains(in.States, j.State)) {
					continue
				}
				out.Matched++
				if len(out.Jobs) < limit {
					out.Jobs = append(out.Jobs, rowOf(j))
				} else {
					out.Truncated = true
				}
			}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetControl, Version: 1,
		Summary: "set the controls Enqueue sends: duration, how the attempt ends, attempts allowed, backoff; a field left empty keeps its control",
		Effect:  app.OperationEffectDocument, Writes: []string{resControls}, Agents: true,
		Gesture: "the duration and attempts sliders, the backoff radio buttons and the fail check box", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetControlArgs) (out appops.None, err error) {
			next := inst.controlsValue()
			if in.DurationSec != 0 {
				if in.DurationSec < minDurationSec || in.DurationSec > maxDurationSec {
					err = app.RefuseOperation(fmt.Sprintf("the duration is between %g and %g seconds", minDurationSec, maxDurationSec))
					return
				}
				next.DurationSec = in.DurationSec
			}
			switch in.End {
			case "":
			case endFail:
				next.Fail = true
			case endSucceed:
				next.Fail = false
			default:
				err = app.RefuseOperation("end is " + endFail + " or " + endSucceed)
				return
			}
			if in.Attempts != 0 {
				if in.Attempts < 1 || in.Attempts > maxAttemptsCap {
					err = app.RefuseOperation(fmt.Sprintf("attempts is between 1 and %d", maxAttemptsCap))
					return
				}
				next.Attempts = in.Attempts
			}
			if in.Backoff != "" {
				if !slices.Contains(backoffChoices, in.Backoff) {
					err = app.RefuseOperation("the backoff is one of " + strings.Join(backoffChoices, ", "))
					return
				}
				next.Backoff = in.Backoff
			}
			inst.applyControls(next)
			return
		})

	// Enqueue asks the worker off the frame and settles when the store
	// accepted the job (ADR-0358). It writes a row outside the window, so
	// the person confirms an agent's call every time; the handler copies
	// the call's OnBehalfOf onto the request (ADR-0344 §SD2, W3).
	appops.Command(s, app.OperationSpec{Name: opEnqueue, Version: 1,
		Summary: "enqueue one " + KindSleep + " job with the controls' policy; settles when the worker accepted it into the store, with the job's id and state",
		Effect:  app.OperationEffectConsequential, Reads: []string{resControls}, Agents: true, Async: true, Gesture: "the Enqueue job button"},
		func(inst *App, call app.OperationCall, _ appops.None) (out appops.None, err error) {
			if inst.client == nil {
				err = app.RefuseOperation("the window has no bus to reach a worker through; open it under the windowed host")
				return
			}
			inst.enqueueAs(call)
			return
		})
	appops.Settles(s, opEnqueue, settleEnqueue)
	s.Available(opEnqueue, func(sn opsSnap) (ok bool, reason string) {
		if sn.inflight > 0 {
			return false, "a request is in flight; wait for it to settle"
		}
		return true, ""
	})
	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

// applyControls puts a controls value on the bound fields; false when the
// backoff is not one of the choices.
func (inst *App) applyControls(v controls) (ok bool) {
	idx := slices.Index(backoffChoices, v.Backoff)
	if idx < 0 {
		return false
	}
	inst.durationSec, inst.simulateFail, inst.maxAttempts, inst.backoffIdx = v.DurationSec, v.Fail, float64(v.Attempts), idx
	return true
}

// settleEnqueue settles an enqueue from the outcome the verb landed for the
// call that asked (ADR-0358 §SD1): pending until the worker answered, then
// the job as the store accepted it, or the error.
func settleEnqueue(inst *App, call app.OperationCall) (out EnqueueOutcome, settled bool, err error) {
	inst.mu.Lock()
	o, found := inst.enqueues[call.Key]
	inst.mu.Unlock()
	switch {
	case !found:
		return out, true, app.RefuseOperation("no enqueue was asked under call " + call.Key)
	case !o.done:
		return out, false, nil
	case o.err != nil:
		return out, true, o.err
	}
	inst.mu.Lock()
	delete(inst.enqueues, call.Key)
	inst.mu.Unlock()
	out = EnqueueOutcome{Id: o.job.ID, State: o.job.State, Subject: o.job.Subject,
		AgentCaused: call.OnBehalfOf != nil && call.OnBehalfOf.Task != ""}
	return out, true, nil
}

// gesture routes one of the person's controls through the catalog, so the
// command log sees it as theirs (ADR-0269 §SD8), and applies it directly
// where no host serves the catalog (tour, tests).
func gesture[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

func (inst *App) enqueueGesture() {
	gesture(inst, opEnqueue, appops.None{}, inst.enqueue)
}
