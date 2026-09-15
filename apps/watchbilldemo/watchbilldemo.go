// Package watchbilldemo is the first in-tree client of the watchbill
// protocol (ADR-0234 §M5): one window that enqueues jobs of its own kind
// through [watchbill.Client], lists the queue with a cancel and a retry on
// every row, and embeds the task monitor so a running job's progress and
// its cancel button are the task's. It registers the handler for its kind
// at init, so the process's worker drains what the window enqueues.
//
// Every bus request is answered by the worker, which may be a store read
// away; none runs on the frame goroutine. A poller refreshes the list on a
// tick and on every watchbill.changed it hears, and each verb runs in its
// own goroutine and lands its outcome under the app's mutex for the next
// frame to show.
package watchbilldemo

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/taskmonitor"
)

const (
	minDurationSec = 0.5
	maxDurationSec = 60.0
	maxAttemptsCap = 5

	// refreshEvery is the poller's tick; a changed announcement refreshes
	// sooner. Rows another process changes arrive at this cadence.
	refreshEvery = 2 * time.Second
	// requestTimeout bounds one verb; the default bus wait is what a user
	// sees as a hang when no worker serves the process.
	requestTimeout = 3 * time.Second
	// listLimit bounds the rows shown; the applet book is the full view.
	listLimit = 200
	// idShown is how many characters of an id a row shows.
	idShown = 8
)

// App is the per-window instance.
type App struct {
	ids     *c.WidgetIdStack
	logger  zerolog.Logger
	density styletokens.DensityE

	client  *watchbill.Client
	tasks   task.TaskApiI
	monitor *taskmonitor.Inst

	// appCtx ends the poller and every verb in flight at Unmount.
	appCtx    context.Context
	cancelApp context.CancelFunc
	unsub     func()
	// dirty asks the poller for a refresh now; one pending is enough.
	dirty chan struct{}
	wg    sync.WaitGroup

	// The controls, bound to stable fields.
	durationSec  float64
	simulateFail bool
	maxAttempts  float64
	backoffIdx   int

	// What the poller and the verbs produce, read by the frame.
	mu        sync.Mutex
	jobs      []watchbillstore.Job
	lastError string
	lastNote  string
	refreshed time.Time
	inflight  int
}

var _ app.AppI = (*App)(nil)

var backoffChoices = []string{watchbillstore.BackoffNone, watchbillstore.BackoffLinear, watchbillstore.BackoffExponential}

func newApp() (inst *App) {
	inst = &App{
		ids: c.NewWidgetIdStack(), density: styletokens.ActiveDensity(),
		durationSec: 3.0, maxAttempts: 1, dirty: make(chan struct{}, 1),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount takes the bus client the host minted, starts the task monitor,
// subscribes the announcements and starts the poller.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.client = watchbill.NewClient(ctx.Bus())
	inst.client.Timeout = requestTimeout
	inst.tasks = task.ForApp(ctx)
	inst.appCtx, inst.cancelApp = context.WithCancel(context.Background())

	inst.monitor = taskmonitor.New(inst.tasks, inst.ids, "tm", taskmonitor.Opts{DefaultOpen: true})
	if startErr := inst.monitor.Start(); startErr != nil {
		inst.logger.Debug().Err(startErr).Msg("watchbilldemo: task monitor not started")
	}
	if bus := ctx.Bus(); bus != nil {
		unsub, subErr := bus.Subscribe(watchbill.SubjectChanged, func(*app.Msg) { inst.markDirty() })
		if subErr != nil {
			inst.logger.Debug().Err(subErr).Msg("watchbilldemo: not hearing watchbill.changed")
		} else {
			inst.unsub = unsub
		}
	}
	inst.wg.Add(1)
	go inst.poll()
	inst.markDirty()
	return
}

// Unmount stops the poller, the subscription and the monitor. Jobs in
// flight are the worker's, not the window's, and keep running.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.cancelApp != nil {
		inst.cancelApp()
	}
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
	inst.wg.Wait()
	if inst.monitor != nil {
		_ = inst.monitor.Stop()
	}
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.density = styletokens.ActiveDensity()
	inst.render()
	return
}

// --- the bus side, off the frame goroutine --------------------------

func (inst *App) markDirty() {
	select {
	case inst.dirty <- struct{}{}:
	default:
	}
}

// poll refreshes the list on the tick and on demand until the app ends.
func (inst *App) poll() {
	defer inst.wg.Done()
	ticker := time.NewTicker(refreshEvery)
	defer ticker.Stop()
	for {
		select {
		case <-inst.appCtx.Done():
			return
		case <-ticker.C:
		case <-inst.dirty:
		}
		inst.refresh()
	}
}

func (inst *App) refresh() {
	jobs, err := inst.client.List(nil, nil, listLimit)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err != nil {
		inst.lastError = "list: " + err.Error()
		return
	}
	inst.jobs, inst.refreshed = jobs, time.Now()
}

// enqueue asks the worker for a job with the controls' policy.
func (inst *App) enqueue() {
	req := watchbill.Request{
		Kind:        KindSleep,
		Subject:     Subject(time.Duration(inst.durationSec*float64(time.Second)), inst.simulateFail),
		MaxAttempts: uint32(inst.maxAttempts),
		Backoff:     backoffChoices[inst.backoffIdx],
		BackoffBase: time.Second,
	}
	inst.verb("enqueue", func() (note string, err error) {
		job, err := inst.client.Enqueue(req)
		if err != nil {
			return "", err
		}
		return "enqueued " + short(job.ID) + " as " + job.State, nil
	})
}

func (inst *App) cancel(id string) {
	inst.verb("cancel", func() (note string, err error) {
		ok, err := inst.client.Cancel(id, "from the demo window")
		if err != nil {
			return "", err
		}
		if !ok {
			return "cancel did not apply to " + short(id), nil
		}
		return "cancel asked for " + short(id), nil
	})
}

func (inst *App) retry(id string) {
	inst.verb("retry", func() (note string, err error) {
		ok, err := inst.client.Retry(id, "from the demo window")
		if err != nil {
			return "", err
		}
		if !ok {
			return "retry did not apply to " + short(id), nil
		}
		return "retried " + short(id), nil
	})
}

// verb runs one request in its own goroutine and lands its outcome.
func (inst *App) verb(name string, fn func() (note string, err error)) {
	if inst.client == nil || inst.appCtx == nil || inst.appCtx.Err() != nil {
		return
	}
	inst.mu.Lock()
	inst.inflight++
	inst.mu.Unlock()
	inst.wg.Add(1)
	go func() {
		defer inst.wg.Done()
		note, err := fn()
		inst.mu.Lock()
		inst.inflight--
		if err != nil {
			inst.lastError = name + ": " + err.Error()
		} else {
			inst.lastError = ""
			inst.lastNote = note
		}
		inst.mu.Unlock()
		inst.markDirty()
	}()
}

// snapshot is what one frame renders from.
func (inst *App) snapshot() (jobs []watchbillstore.Job, lastError string, lastNote string, refreshed time.Time, inflight int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.jobs, inst.lastError, inst.lastNote, inst.refreshed, inst.inflight
}

// --- render ----------------------------------------------------------

func (inst *App) render() {
	for range c.PanelTopInside(inst.ids.PrepareStr("topbar")).Resizable(false).KeepIter() {
		c.Label("Durable job demo — enqueue a demo.sleep job; a worker in this process claims it, runs it as a task, and the row records every transition").Send()
	}
	for range c.PanelCentralInside().KeepIter() {
		for range c.ScrollArea().Vscroll(true).KeepIter() {
			inst.renderControls()
			c.AddSpace(styletokens.PaddingOuter(inst.density))
			inst.renderJobs()
			c.AddSpace(styletokens.PaddingOuter(inst.density))
			if inst.monitor != nil {
				inst.monitor.Render()
			}
		}
	}
}

func (inst *App) renderControls() {
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-enqueue"), c.WidgetText().Text("Enqueue").Keep()).DefaultOpen(true).KeepIter() {
		_ = c.SliderF64(inst.ids.PrepareStr("duration"), inst.durationSec, minDurationSec, maxDurationSec).Text("duration (s)").SendRespVal(&inst.durationSec)
		_ = c.SliderF64(inst.ids.PrepareStr("attempts"), inst.maxAttempts, 1, maxAttemptsCap).Text("max attempts").SendRespVal(&inst.maxAttempts)
		for range c.HorizontalTop().KeepIter() {
			c.Label("Backoff").Send()
			for i, b := range backoffChoices {
				var clicked bool
				if c.RadioButton(inst.ids.PrepareSeq(uint64(i)), c.Atoms().Text(b).Keep(), inst.backoffIdx == i).SendRespVal(&clicked).HasPrimaryClicked() {
					inst.backoffIdx = i
				}
			}
		}
		_ = c.Checkbox(inst.ids.PrepareStr("fail"), inst.simulateFail, "Fail at the end (exercises the retry policy)").SendRespVal(&inst.simulateFail)
		for range c.Horizontal().KeepIter() {
			if c.Button(inst.ids.PrepareStr("enqueue"), c.Atoms().Text("Enqueue job").Keep()).SendResp().HasPrimaryClicked() {
				inst.enqueue()
			}
			if c.Button(inst.ids.PrepareStr("refresh"), c.Atoms().Text("Refresh").Keep()).SendResp().HasPrimaryClicked() {
				inst.markDirty()
			}
		}
	}
}

func (inst *App) renderJobs() {
	jobs, lastError, lastNote, refreshed, inflight := inst.snapshot()
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-jobs"), c.WidgetText().Text("Jobs").Keep()).DefaultOpen(true).KeepIter() {
		for range c.IdScope(inst.ids.PrepareStr("status")) {
			switch {
			case lastError != "":
				badge.New(inst.ids.PrepareStr("err"), lastError).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
				if strings.Contains(lastError, "timeout") {
					c.Label("No worker answered: the process's watchbill service needs a live ClickHouse (hostboot Services.Watchbill).").Send()
				}
			case inflight > 0:
				badge.New(inst.ids.PrepareStr("busy"), fmt.Sprintf("%d request(s) in flight", inflight)).Tone(badge.ToneInfo).Variant(badge.VariantSoft).Send()
			case lastNote != "":
				badge.New(inst.ids.PrepareStr("note"), lastNote).Tone(badge.ToneSuccess).Variant(badge.VariantSoft).Send()
			}
		}
		if refreshed.IsZero() {
			c.Label("Waiting for the first list…").Send()
		} else {
			c.Label(fmt.Sprintf("%d job(s), listed %s ago", len(jobs), time.Since(refreshed).Round(time.Second))).Send()
		}
		if len(jobs) == 0 {
			return
		}
		for range c.Grid(inst.ids.PrepareStr("jobs")).NumColumns(8).Striped(true).KeepIter() {
			for _, h := range []string{"id", "kind", "subject", "state", "attempt", "worker run", "last error", ""} {
				strong(h)
			}
			c.EndRow()
			for _, j := range jobs {
				inst.renderJobRow(j)
				c.EndRow()
			}
		}
	}
}

func (inst *App) renderJobRow(j watchbillstore.Job) {
	for range c.IdScope(inst.ids.PrepareStr(j.ID)) {
		mono(short(j.ID))
		c.Label(j.Kind).Send()
		c.Label(j.Subject).Send()
		badge.New(inst.ids.PrepareStr("state"), j.State).Tone(toneOf(j.State)).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
		mono(fmt.Sprintf("%d/%d", j.Attempt, j.MaxAttempts))
		mono(short(j.WorkerRun))
		c.Label(firstLine(j.LastError)).Send()
		for range c.Horizontal().KeepIter() {
			cancellable := j.State == watchbillstore.StateQueued || j.State == watchbillstore.StateRunning
			if c.Button(inst.ids.PrepareStr("cancel"), c.Atoms().Text("Cancel").Keep()).Small().SendResp().HasPrimaryClicked() && cancellable {
				inst.cancel(j.ID)
			}
			if c.Button(inst.ids.PrepareStr("retry"), c.Atoms().Text("Retry").Keep()).Small().SendResp().HasPrimaryClicked() && watchbillstore.IsFinal(j.State) {
				inst.retry(j.ID)
			}
		}
	}
}

func toneOf(state string) (tone badge.ToneE) {
	switch state {
	case watchbillstore.StateQueued:
		return badge.ToneInfo
	case watchbillstore.StateRunning:
		return badge.TonePrimary
	case watchbillstore.StateSucceeded:
		return badge.ToneSuccess
	case watchbillstore.StateFailed, watchbillstore.StateCancel:
		return badge.ToneWarning
	case watchbillstore.StateDiscarded, watchbillstore.StateAbandoned:
		return badge.ToneError
	default:
		return badge.ToneNeutral
	}
}

func short(id string) (s string) {
	if len(id) <= idShown {
		return id
	}
	return id[:idShown] + "…"
}

func firstLine(s string) (line string) {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const maxLen = 80
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}

// strong and mono are one styled label each.
func strong(text string) {
	c.LabelAtoms(c.Atoms().BeginRichText(text).Strong().End().Keep()).Send()
}

func mono(text string) {
	c.LabelAtoms(c.Atoms().BeginRichText(text).Monospace().End().Keep()).Send()
}
