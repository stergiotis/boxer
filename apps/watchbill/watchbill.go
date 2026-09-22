// Package watchbill is the management window over the watchbill
// (ADR-0236): the queue across kinds from the list verb, one job's trail
// and the process's worker row from the introspection tables over the bus
// (ADR-0253), cancel and
// retry through the client, and the task monitor for live runs. Nothing
// here waits on a store from the frame goroutine: a poller refreshes on a
// tick and on every watchbill.changed, and each verb runs in its own
// goroutine and lands its outcome for the next frame.
package watchbill

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/watchbill/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	wb "github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/taskmonitor"
)

const (
	// refreshEvery is the poller's tick; a changed announcement refreshes
	// sooner. Rows another process changes arrive at this cadence.
	refreshEvery = 2 * time.Second
	// requestTimeout bounds one verb on the bus.
	requestTimeout = 3 * time.Second
	// listLimit bounds the rows the window lists; the book is the full view.
	listLimit = 500
	// idShown is how many characters of an id a cell shows.
	idShown = 10
	// noteFrom is what cancel and retry record on the event.
	noteFrom = "from the watchbill window"
)

// filters is what the window asks the list for. States on the worker,
// the kind substring in the window.
type filters struct {
	states map[string]bool
	kind   string
}

func (f filters) stateList() (states []string) {
	for s := range f.states {
		if f.states[s] {
			states = append(states, s)
		}
	}
	sort.Strings(states)
	return
}

// snapshot is what one frame renders from.
type snapshot struct {
	jobs      []watchbillstore.Job
	events    []eventRow
	eventsFor string
	workers   []workerRow
	lastError string
	lastNote  string
	refreshed time.Time
	inflight  int
	// reads says the window has a bus to read the tables through; false
	// only where the host minted none.
	reads bool
}

// App is the per-window instance.
type App struct {
	ids     *c.WidgetIdStack
	logger  zerolog.Logger
	density styletokens.DensityE

	client  *wb.Client
	reads   *tableReader
	tasks   task.TaskApiI
	monitor *taskmonitor.Inst
	machine *fsmview.Machine[string]
	chip    *fsmview.Widget[string]

	appCtx    context.Context
	cancelApp context.CancelFunc
	unsub     func()
	dirty     chan struct{}
	wg        sync.WaitGroup

	// The frame's own state: the filters, the selection, the text field
	// binding, which event's error is unfolded, and whether the poller
	// runs on its tick.
	filters filters
	sort    tableSort
	fitted  bool
	// widths persists the columns the user dragged (ADR-0151), acquired on
	// the first frame from the host's capability; nil renders the defaults.
	widths     *colwidth.Resolver
	widthsInit bool
	widthsSeen bool
	// applyGen asks the binding to apply the resolved widths again on a
	// frame the resolver\'s own epoch would not; see renderList.
	applyGen uint32
	// split is the list pane\'s width, kept and persisted by the window;
	// storage is where it is kept, and splitWrite its pending write.
	split       splitState
	storage     app.StorageI
	splitSeen   float32
	kindDraft   string
	selectedID  string
	shownEvent  int
	autoRefresh bool

	mu   sync.Mutex
	snap snapshot
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids: c.NewWidgetIdStack(), density: styletokens.ActiveDensity(),
		filters: filters{states: make(map[string]bool, len(watchbillstore.AllStates))},
		dirty:   make(chan struct{}, 1), shownEvent: -1, autoRefresh: true,
	}
	inst.machine = newJobMachine()
	inst.split = newSplitState()
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount takes the host's bus client, reads a launch config, wires the
// table reader, the task monitor and the announcements, and starts the
// poller.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.client = wb.NewClient(ctx.Bus())
	inst.client.Timeout = requestTimeout
	inst.reads = newTableReader(ctx.Bus())
	inst.tasks = task.ForApp(ctx)
	inst.appCtx, inst.cancelApp = context.WithCancel(context.Background())
	inst.chip = fsmview.New(inst.ids, "job-state", inst.machine).Title("job state")
	inst.storage = ctx.Storage()
	if inst.storage != nil {
		if raw, found, gerr := inst.storage.Get(splitKey); gerr == nil && found {
			if w, ok := decodeSplit(raw); ok {
				inst.split.Width = w
			}
		}
	}

	if raw := ctx.LaunchConfig(); len(raw) > 0 {
		cfg, dErr := buscodec.Decode[launchcfg.WatchbillLaunch](raw)
		if dErr != nil {
			return dErr
		}
		inst.applyLaunch(cfg)
	}

	inst.monitor = taskmonitor.New(inst.tasks, inst.ids, "tm", taskmonitor.Opts{DefaultOpen: true})
	if startErr := inst.monitor.Start(); startErr != nil {
		inst.logger.Debug().Err(startErr).Msg("watchbill app: task monitor not started")
	}
	if bus := ctx.Bus(); bus != nil {
		unsub, subErr := bus.Subscribe(wb.SubjectChanged, func(*app.Msg) { inst.markDirty() })
		if subErr != nil {
			inst.logger.Debug().Err(subErr).Msg("watchbill app: not hearing watchbill.changed")
		} else {
			inst.unsub = unsub
		}
	}
	inst.wg.Add(1)
	go inst.poll()
	inst.markDirty()
	return
}

// applyLaunch selects and filters as the config asks (ADR-0236 §SD4).
func (inst *App) applyLaunch(cfg launchcfg.WatchbillLaunch) {
	inst.selectedID = cfg.JobId
	inst.kindDraft = cfg.Kind
	inst.filters.kind = cfg.Kind
	if cfg.State != "" {
		inst.filters.states[cfg.State] = true
	}
}

// Unmount stops the poller, the subscription and the monitor.
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
	inst.ensureWidths(ctx)
	inst.render()
	return
}

// ensureWidths acquires the column-width resolver once, on the first frame:
// the capability rides the frame context (ADR-0155 §SD1), so it cannot be
// picked up in Mount. A host without it leaves the resolver nil, and the
// list renders its defaults with every drag still usable, none kept.
func (inst *App) ensureWidths(ctx app.FrameContextI) {
	if inst.widthsInit {
		return
	}
	inst.widthsInit = true
	h, ok := ctx.(colwidth.HostI)
	if !ok {
		return
	}
	store := h.ColumnWidthStore()
	if store == nil {
		return
	}
	res, err := colwidth.New(store, colwidth.Opts{
		AppId: ctx.AppId(), InstanceKey: ctx.InstanceKey(),
		MinPoints: colMinWidth, MaxPoints: colMaxWidth,
	})
	if err != nil {
		inst.logger.Warn().Err(err).Msg("watchbill app: column-width resolver unavailable")
		return
	}
	if lErr := res.Load(); lErr != nil {
		inst.logger.Warn().Err(lErr).Msg("watchbill app: stored column widths could not be loaded")
	}
	inst.widths = res
}

// --- the bus side, off the frame goroutine --------------------------------

func (inst *App) markDirty() {
	select {
	case inst.dirty <- struct{}{}:
	default:
	}
}

func (inst *App) poll() {
	defer inst.wg.Done()
	ticker := time.NewTicker(refreshEvery)
	defer ticker.Stop()
	for {
		select {
		case <-inst.appCtx.Done():
			inst.storeSplit()
			return
		case <-ticker.C:
			inst.storeSplit()
			if !inst.autoRefreshOn() {
				continue
			}
		case <-inst.dirty:
		}
		inst.refresh()
	}
}

// storeSplit writes the split when a drag changed it since the last tick —
// the tick is the debounce — off the frame goroutine, since a storage
// write is a bus request.
func (inst *App) storeSplit() {
	if inst.storage == nil {
		return
	}
	inst.mu.Lock()
	w := inst.split.Width
	changed := w != inst.splitSeen
	inst.splitSeen = w
	inst.mu.Unlock()
	if !changed {
		return
	}
	if err := inst.storage.Set(splitKey, encodeSplit(w)); err != nil {
		inst.logger.Debug().Err(err).Msg("watchbill app: split not stored")
	}
}

func (inst *App) autoRefreshOn() (on bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.autoRefresh
}

// refresh reads the list under the current state filter, the selected
// job's trail and the worker row, and lands them as one snapshot.
func (inst *App) refresh() {
	inst.mu.Lock()
	states := inst.filters.stateList()
	selected := inst.selectedID
	inst.mu.Unlock()

	var next snapshot
	next.reads = inst.reads != nil
	jobs, err := inst.client.List(states, nil, listLimit)
	if err != nil {
		next.lastError = "list: " + err.Error()
	} else {
		next.jobs = jobs
	}
	if inst.reads != nil {
		ctx := inst.appCtx
		if selected != "" {
			if evs, eerr := inst.reads.events(ctx, selected); eerr != nil {
				next.lastError = firstNonEmpty(next.lastError, "trail: "+eerr.Error())
			} else {
				next.events, next.eventsFor = evs, selected
			}
		}
		if ws, werr := inst.reads.workers(ctx); werr != nil {
			next.lastError = firstNonEmpty(next.lastError, "workers: "+werr.Error())
		} else {
			next.workers = ws
		}
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	next.lastNote, next.inflight = inst.snap.lastNote, inst.snap.inflight
	if err != nil {
		// A failed list keeps the rows we had; the error says why.
		next.jobs = inst.snap.jobs
		next.refreshed = inst.snap.refreshed
	} else {
		next.refreshed = time.Now()
	}
	if next.eventsFor == "" && inst.snap.eventsFor == selected {
		next.events, next.eventsFor = inst.snap.events, inst.snap.eventsFor
	}
	inst.snap = next
}

func firstNonEmpty(a, b string) (s string) {
	if a != "" {
		return a
	}
	return b
}

func (inst *App) cancel(id string) {
	inst.verb("cancel", func() (note string, err error) {
		ok, err := inst.client.Cancel(id, noteFrom)
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
		ok, err := inst.client.Retry(id, noteFrom)
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
	inst.snap.inflight++
	inst.mu.Unlock()
	inst.wg.Add(1)
	go func() {
		defer inst.wg.Done()
		note, err := fn()
		inst.mu.Lock()
		inst.snap.inflight--
		if err != nil {
			inst.snap.lastError = name + ": " + err.Error()
		} else {
			inst.snap.lastError = ""
			inst.snap.lastNote = note
		}
		inst.mu.Unlock()
		inst.markDirty()
	}()
}

// select_ changes the selection; the poller fetches its trail.
func (inst *App) select_(id string) {
	inst.mu.Lock()
	changed := inst.selectedID != id
	inst.selectedID = id
	inst.shownEvent = -1
	inst.mu.Unlock()
	if changed {
		inst.markDirty()
	}
}

// clearFilters drops every state pill and the kind text; the list is
// re-read, and the text field is told its binding changed under it.
func (inst *App) clearFilters() {
	inst.mu.Lock()
	changed := len(inst.filters.stateList()) > 0 || inst.filters.kind != ""
	for k := range inst.filters.states {
		delete(inst.filters.states, k)
	}
	inst.filters.kind = ""
	inst.kindDraft = ""
	inst.mu.Unlock()
	if changed {
		c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&inst.kindDraft)
		inst.markDirty()
	}
}

// toggleState flips one state chip; the list is re-read.
func (inst *App) toggleState(state string) {
	inst.mu.Lock()
	inst.filters.states[state] = !inst.filters.states[state]
	inst.mu.Unlock()
	inst.markDirty()
}

func (inst *App) snapshot() (s snapshot) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.snap
}

// visibleJobs applies the kind substring, the window's own filter.
func (inst *App) visibleJobs(jobs []watchbillstore.Job) (out []watchbillstore.Job) {
	needle := strings.ToLower(strings.TrimSpace(inst.filters.kind))
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

func findJob(jobs []watchbillstore.Job, id string) (job watchbillstore.Job, found bool) {
	for _, j := range jobs {
		if j.ID == id {
			return j, true
		}
	}
	return
}

func short(id string) (s string) {
	if len(id) <= idShown {
		return id
	}
	return id[:idShown] + "…"
}
