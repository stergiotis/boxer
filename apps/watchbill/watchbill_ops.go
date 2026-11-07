package watchbill

import (
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/fsmops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	wb "github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
)

// The window's operations catalog (ADR-0269 M4): a table-shaped participant
// beside play, so the contract is not drawn around one app. The filters and
// the selection are view state; cancel and retry write to the durable job
// store, outside the window, so they are consequential, and they are
// asynchronous: the store answers off the frame, and the call settles with
// what it said (ADR-0358). The person's own pills, row clicks and buttons
// go through the same handlers.

const (
	resFilters   = "filters"
	resSelection = "selection"

	opListJobs  = "list_jobs"
	opSetFilter = "set_filters"
	opSelect    = "select_job"
	opCancel    = "cancel_job"
	opRetry     = "retry_job"
)

// opsListCap bounds the rows list_jobs returns; the window lists more.
const opsListCap = 200

// opsSnap is what queries read: copies taken after the command stage.
type opsSnap struct {
	jobs     []watchbillstore.Job
	states   []string
	inflight int
}

// JobRow is one job as list_jobs returns it.
type JobRow struct {
	Id         string    `desc:"the job id"`
	Kind       string    `desc:"the job kind"`
	Subject    string    `desc:"what the job works on, as its requester named it"`
	Queue      string    `desc:"the queue"`
	State      string    `desc:"queued, running, succeeded, failed, discarded, cancel, cancelled or abandoned (ADR-0223 §SD2)"`
	Attempt    uint32    `desc:"how many times it ran"`
	RunAfter   time.Time `desc:"when it may run next"`
	FinishedAt time.Time `desc:"when it finished; zero while it has not"`
	LastError  string    `desc:"the last error, as the job reported it"`
}

// ListJobsArgs is list_jobs' argument: its own filter over the rows the
// window holds, not the person's filter row (A2).
type ListJobsArgs struct {
	States []string `json:",omitzero" desc:"states to list; empty lists every state among the held rows"`
	Kind   string   `json:",omitzero" desc:"a regex battery over the kind: space-separated case-insensitive patterns, all of which must match; empty matches every kind"`
	Limit  int      `json:",omitzero" desc:"rows to return, at most 200; 0 returns 200"`
}

// JobList is list_jobs' result.
type JobList struct {
	Jobs      []JobRow `desc:"the jobs the arguments select, newest first as the window lists them"`
	Truncated bool     `desc:"true when more jobs match than were returned"`
	Held      int      `desc:"how many rows the window holds; the window's state filter bounds them, and set_filters widens it"`
	HeldFor   []string `desc:"the window's state filter the held rows were read under; empty means every state"`
}

// SetFiltersArgs is set_filters' argument.
type SetFiltersArgs struct {
	States []string `json:",omitzero" desc:"states to show; empty shows every state"`
	Kind   string   `json:",omitzero" desc:"a regex battery over the job kind: space-separated case-insensitive patterns, all of which must match; empty matches every kind"`
}

// JobArgs names one job.
type JobArgs struct {
	Id string `desc:"the job id"`
}

// VerbOutcome is what cancel_job and retry_job settle with, once the store
// answered.
type VerbOutcome struct {
	Id      string `desc:"the job id"`
	Applied bool   `desc:"true when the store applied the verb; false when the job was in a state it does not apply to, or does not exist"`
	Note    string `desc:"the outcome as the window's status row shows it"`
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet(func(inst *App) opsSnap {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return opsSnap{jobs: slices.Clone(inst.snap.jobs), states: inst.filters.stateList(), inflight: inst.snap.inflight}
	})
	// The kind text is read where the person's typing lands, the bound
	// draft, so the write-back is attributed to the person.
	s.Resource(resFilters, "the state and kind filters", func(inst *App) any {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return strings.Join(inst.filters.stateList(), ",") + "|" + inst.kindDraft
	})
	s.Resource(resSelection, "the selected job", func(inst *App) any {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return inst.selectedID
	})
	s.Editing(resFilters, func(inst *App) bool { return appops.WidgetEditing(inst.kindH) })
	// The selected job's state machine, as the state chip draws it:
	// job_state and job_machine. The machine mirrors whichever job is
	// selected, so its steps would mix jobs and are left out; until a frame
	// mirrors the selected job, it reports none.
	fsmops.Mount(s, "job", "the selected job", func(inst *App) opfsm.SourceI {
		inst.mu.Lock()
		selected := inst.selectedID
		inst.mu.Unlock()
		if selected == "" || inst.machine == nil || inst.mirrored != selected {
			return nil
		}
		return inst.machine
	}, fsmops.Options{})

	appops.Query(s, app.OperationSpec{Name: opListJobs, Version: 1,
		Summary: "list jobs among the rows the window holds, by state and a kind battery; keelson('" + wb.TableJobs + "') is the full view",
		Agents:  true, Untrusted: true},
		func(sn opsSnap, in ListJobsArgs) (out JobList, err error) {
			for _, st := range in.States {
				if !slices.Contains(watchbillstore.AllStates, st) {
					return out, app.RefuseOperation("no state " + st + "; the states are " + strings.Join(watchbillstore.AllStates, ", "))
				}
			}
			limit := in.Limit
			if limit <= 0 || limit > opsListCap {
				limit = opsListCap
			}
			out = JobList{Held: len(sn.jobs), HeldFor: sn.states}
			for _, j := range visible(sn.jobs, search.ParseQuery(in.Kind)) {
				if len(in.States) > 0 && !slices.Contains(in.States, j.State) {
					continue
				}
				if len(out.Jobs) == limit {
					out.Truncated = true
					break
				}
				out.Jobs = append(out.Jobs, JobRow{Id: j.ID, Kind: j.Kind, Subject: j.Subject, Queue: j.Queue, State: j.State,
					Attempt: j.Attempt, RunAfter: j.RunAfter, FinishedAt: j.FinishedAt, LastError: j.LastError})
			}
			return
		})
	appops.Command(s, app.OperationSpec{Name: opSetFilter, Version: 1, Summary: "set the state and kind filters",
		Effect: app.OperationEffectView, Writes: []string{resFilters}, Agents: true, Gesture: "the state pills, the kind field and Clear"},
		func(inst *App, call app.OperationCall, in SetFiltersArgs) (appops.None, error) {
			for _, st := range in.States {
				if !slices.Contains(watchbillstore.AllStates, st) {
					return appops.None{}, app.RefuseOperation("no state " + st + "; the states are " + strings.Join(watchbillstore.AllStates, ", "))
				}
			}
			inst.setFilters(in.States, in.Kind)
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opSelect, Version: 1, Summary: "select a job to show its trail",
		Effect: app.OperationEffectView, Writes: []string{resSelection}, Agents: true, Gesture: "clicking a row"},
		func(inst *App, call app.OperationCall, in JobArgs) (appops.None, error) {
			inst.select_(in.Id)
			return appops.None{}, nil
		})
	// Cancel and retry ask the store off the frame and settle with its
	// answer (ADR-0358): a job the window knows to be ineligible is refused
	// before the round trip, with the states the verb applies to.
	appops.Command(s, app.OperationSpec{Name: opCancel, Version: 1, Summary: "cancel a queued or running job in the durable store",
		Effect: app.OperationEffectConsequential, Agents: true, Async: true, Gesture: "the Cancel button"},
		func(inst *App, call app.OperationCall, in JobArgs) (appops.None, error) {
			if job, found := findJob(inst.snapshot().jobs, in.Id); found && !cancellable(job.State) {
				return appops.None{}, app.RefuseOperation("job " + short(in.Id) + " is " + job.State + "; cancel applies to queued or running jobs")
			}
			inst.cancel(in.Id)
			return appops.None{}, nil
		})
	appops.SettlesRaw(s, opCancel, settleVerb)
	s.Available(opCancel, verbAvailable)
	appops.Command(s, app.OperationSpec{Name: opRetry, Version: 1, Summary: "retry a finished job: back in the queue with its attempts reset",
		Effect: app.OperationEffectConsequential, Agents: true, Async: true, Gesture: "the Retry button"},
		func(inst *App, call app.OperationCall, in JobArgs) (appops.None, error) {
			if job, found := findJob(inst.snapshot().jobs, in.Id); found && !watchbillstore.IsFinal(job.State) {
				return appops.None{}, app.RefuseOperation("job " + short(in.Id) + " is " + job.State + "; retry applies to a finished job: " + strings.Join(watchbillstore.FinalStates, ", "))
			}
			inst.retry(in.Id)
			return appops.None{}, nil
		})
	appops.SettlesRaw(s, opRetry, settleVerb)
	s.Available(opRetry, verbAvailable)
	return
}()

// Operations serves the catalog for this window.
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

// cancellable says a cancel applies to the state.
func cancellable(state string) (yes bool) {
	return state == watchbillstore.StateQueued || state == watchbillstore.StateRunning
}

// settleVerb settles cancel_job and retry_job from the outcome the verb
// landed for the job the call named (ADR-0358 §SD1): pending until the
// store answered, then the outcome, or the transport's error.
func settleVerb(inst *App, _ app.OperationCall, args []byte) (settled bool, result []byte, err error) {
	in, err := buscodec.Decode[JobArgs](args)
	if err != nil {
		return true, nil, err
	}
	o, found := inst.outcome(in.Id)
	switch {
	case !found:
		return true, nil, app.RefuseOperation("no cancel or retry was asked for job " + short(in.Id))
	case !o.done:
		return false, nil, nil
	case o.err != nil:
		return true, nil, o.err
	}
	result, err = buscodec.Encode(VerbOutcome{Id: in.Id, Applied: o.applied, Note: o.note})
	return true, result, err
}

// verbAvailable gates the verbs while one is in flight (ADR-0358): a
// second ask before the store answered would race the first.
func verbAvailable(sn opsSnap) (ok bool, reason string) {
	if sn.inflight > 0 {
		return false, "a cancel or retry is in flight; wait for it to settle"
	}
	return true, ""
}

// setFilters replaces the filters; the list is read again. Running before
// the window draws, the kind field shows the new text without an override.
func (inst *App) setFilters(states []string, kind string) {
	inst.mu.Lock()
	for k := range inst.filters.states {
		delete(inst.filters.states, k)
	}
	for _, st := range states {
		inst.filters.states[st] = true
	}
	inst.filters.kind, inst.kindDraft = kind, kind
	inst.mu.Unlock()
	inst.markDirty()
}

// gesture routes one of the person's gestures through the catalog, so it
// is logged like an agent's call (ADR-0269 §SD8). Where no host serves the
// catalog, fallback applies it directly.
func gesture[In any](inst *App, op string, in In, fallback func()) {
	if inst.frameCtx == nil {
		fallback()
		return
	}
	if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err != nil {
		fallback()
	}
}
