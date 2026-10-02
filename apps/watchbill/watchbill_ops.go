package watchbill

import (
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/fsmops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
)

// The window's operations catalog (ADR-0269 M4): a table-shaped participant
// beside play, so the contract is not drawn around one app. The filters and
// the selection are view state; cancel and retry write to the durable job
// store, outside the window, so they are consequential. The person's own
// pills, row clicks and buttons go through the same handlers.

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
	kind     string
	selected string
}

// JobRow is one job as list_jobs returns it.
type JobRow struct {
	Id         string    `desc:"the job id"`
	Kind       string    `desc:"the job kind"`
	Subject    string    `desc:"what the job works on, as its requester named it"`
	Queue      string    `desc:"the queue"`
	State      string    `desc:"queued, leased, done, failed, cancelled or dead"`
	Attempt    uint32    `desc:"how many times it ran"`
	RunAfter   time.Time `desc:"when it may run next"`
	FinishedAt time.Time `desc:"when it finished; zero while it has not"`
	LastError  string    `desc:"the last error, as the job reported it"`
}

// JobList is list_jobs' result.
type JobList struct {
	Jobs      []JobRow `desc:"the jobs the filters show, newest first as the window lists them"`
	Truncated bool     `desc:"true when more jobs match than were returned"`
	States    []string `desc:"the state filter; empty shows every state"`
	Kind      string   `desc:"the kind filter, a substring"`
	Selected  string   `desc:"the selected job's id, empty when none"`
}

// SetFiltersArgs is set_filters' argument.
type SetFiltersArgs struct {
	States []string `desc:"states to show; empty shows every state"`
	Kind   string   `desc:"a substring of the job kind; empty matches every kind"`
}

// JobArgs names one job.
type JobArgs struct {
	Id string `desc:"the job id"`
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet(func(inst *App) opsSnap {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return opsSnap{jobs: slices.Clone(inst.snap.jobs), states: inst.filters.stateList(),
			kind: inst.filters.kind, selected: inst.selectedID}
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
	// selected, so its steps would mix jobs and are left out.
	fsmops.Mount(s, "job", "the selected job", func(inst *App) opfsm.SourceI {
		inst.mu.Lock()
		selected := inst.selectedID
		inst.mu.Unlock()
		if selected == "" || inst.machine == nil {
			return nil
		}
		return inst.machine
	}, fsmops.Options{})

	appops.Query(s, app.OperationSpec{Name: opListJobs, Version: 1, Summary: "list the jobs the filters show",
		Reads: []string{resFilters, resSelection}, Agents: true, Untrusted: true},
		func(sn opsSnap, in appops.None) (out JobList, err error) {
			out = JobList{States: sn.states, Kind: sn.kind, Selected: sn.selected}
			for _, j := range visible(sn.jobs, sn.kind) {
				if len(out.Jobs) == opsListCap {
					out.Truncated = true
					break
				}
				out.Jobs = append(out.Jobs, JobRow{Id: j.ID, Kind: j.Kind, Subject: j.Subject, Queue: j.Queue, State: j.State,
					Attempt: j.Attempt, RunAfter: j.RunAfter, FinishedAt: j.FinishedAt, LastError: j.LastError})
			}
			return
		})
	appops.Command(s, app.OperationSpec{Name: opSetFilter, Version: 1, Summary: "set the state and kind filters",
		Effect: app.OperationEffectView, Writes: []string{resFilters}, Agents: true, Gesture: "the state pills and the kind field"},
		func(inst *App, call app.OperationCall, in SetFiltersArgs) (appops.None, error) {
			for _, st := range in.States {
				if !slices.Contains(watchbillstore.AllStates, st) {
					return appops.None{}, app.RefuseOperation("no state " + st)
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
	appops.Command(s, app.OperationSpec{Name: opCancel, Version: 1, Summary: "cancel a job in the durable store",
		Effect: app.OperationEffectConsequential, Agents: true, Gesture: "the Cancel button"},
		func(inst *App, call app.OperationCall, in JobArgs) (appops.None, error) {
			inst.cancel(in.Id)
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opRetry, Version: 1, Summary: "retry a failed or dead job",
		Effect: app.OperationEffectConsequential, Agents: true, Gesture: "the Retry button"},
		func(inst *App, call app.OperationCall, in JobArgs) (appops.None, error) {
			inst.retry(in.Id)
			return appops.None{}, nil
		})
	return
}()

// Operations serves the catalog for this window.
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

// visible applies the kind substring, as visibleJobs does for the window.
func visible(jobs []watchbillstore.Job, kind string) (out []watchbillstore.Job) {
	needle := strings.ToLower(strings.TrimSpace(kind))
	if needle == "" {
		return jobs
	}
	for _, j := range jobs {
		if strings.Contains(strings.ToLower(j.Kind), needle) {
			out = append(out, j)
		}
	}
	return
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
