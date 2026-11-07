// Package watchbill is the management window over the watchbill
// (ADR-0236): the queue across kinds from the list verb, one job's trail
// and the process's worker row from the introspection tables over the bus
// (ADR-0253), cancel and
// retry through the client, and the task monitor for live runs. Nothing
// here waits on a store from the frame goroutine: a poller refreshes on a
// tick and on every watchbill.changed, and each verb runs as one piece of
// work on the instance's lifetime scope and lands its outcome for the
// next frame.
package watchbill

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/watchbill/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	wb "github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexedit"
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
	// ownDrain bounds the wait for the window's goroutines where the app
	// owns its scope (a host without a lifetime: tests); a draining host
	// has its own deadline (ADR-0322).
	ownDrain = 2 * time.Second
	// stateProvenance is where the job chip's value comes from, for the
	// inspector (ADR-0046).
	stateProvenance = "app.watchbill.job.state"
)

// filters is what the window asks the list for. States on the worker,
// the kind battery in the window (F1): space-separated, case-insensitive
// patterns, all of which must match the kind.
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
	events    eventCols
	eventsFor string
	workers   workerCols
	lastError string
	lastNote  string
	refreshed time.Time
	inflight  int
	// reads says the window has a bus to read the tables through; false
	// only where the host minted none.
	reads bool
}

// verbOutcome is what one cancel or retry landed, keyed by the job it was
// asked for: pending until the store answered, then applied or not, or a
// transport failure. An async command settles from it (ADR-0358).
type verbOutcome struct {
	done    bool
	applied bool
	note    string
	err     error
}

// App is the per-window instance.
type App struct {
	ids     *c.WidgetIdStack
	logger  zerolog.Logger
	density styletokens.DensityE

	client  *wb.Client
	reads   *tableReader
	tasks   task.TaskApiI
	monitor *taskmonitor.Monitor
	machine *fsmview.Machine[string]
	chip    *fsmview.View[string]
	// mirrored is the job machine mirrors; the selection can move before
	// the frame that mirrors the new job.
	mirrored string

	// scope counts the poller and the verbs into the instance's lifetime
	// (W5): the host's, so its drain waits for them, or the app's own root
	// where the host has none, retired at Unmount. stop is the mount
	// context's cancel, which the host closes first.
	scope    *lifetime.Scope
	ownScope bool
	stop     <-chan struct{}
	unsub    func()
	dirty    chan struct{}
	// paused stops the poller's reads while the window is paused (ADR-0354).
	paused atomic.Bool
	// landed is set when the poller swapped a snapshot in, so the next
	// frame requests one repaint (W7).
	landed atomic.Bool

	// The frame's own state: the filters, the selection, the text field
	// binding, which event's error is unfolded, and whether the poller
	// runs on its tick.
	filters filters
	// sort and list are the job list's grid state (watchbill_table.go);
	// listRec is the record it draws, rebuilt when listRecKey moves.
	sort       recordtable.SortState
	list       recordtable.State
	listRec    arrow.RecordBatch
	listRecKey jobsRecKey
	// trailSort, trailList and trailRec are the trail grid's, likewise.
	trailSort   recordtable.SortState
	trailList   recordtable.State
	trailRec    arrow.RecordBatch
	trailRecKey trailRecKey
	// widths persists the columns the user dragged (ADR-0151), acquired on
	// the first frame from the host's capability; nil renders the defaults.
	widths     *colwidth.Resolver
	widthsInit bool
	// split is the list pane\'s width, kept and persisted by the window;
	// storage is where it is kept, and splitWrite its pending write.
	split     splitState
	storage   app.StorageI
	splitSeen float32
	kindDraft string
	// kindHl paints the kind battery; kindBattery is the compiled query,
	// rebuilt when kindQuery moves, so no frame compiles a regex.
	kindHl      regexedit.Cache
	kindBattery search.Battery
	kindQuery   string
	// kindH is the kind field's widget, for the catalog's editing check;
	// frameCtx the frame's context, for routing the person's gestures
	// through the catalog (ADR-0269).
	kindH      widgethandle.WidgetHandle
	frameCtx   app.FrameContextI
	selectedID string
	shownEvent int
	// autoDraft is the Auto-refresh check box's value, the render
	// goroutine's alone: the box writes it back at the next sync, so it
	// must outlive the frame. autoRefresh, under mu, is what the poller
	// reads.
	autoDraft   bool
	autoRefresh bool

	mu   sync.Mutex
	snap snapshot
	// verbs is the last cancel or retry per job, under mu.
	verbs map[string]*verbOutcome
}

var _ app.AppI = (*App)(nil)
var _ app.PausableAppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids: c.NewWidgetIdStack(), density: styletokens.ActiveDensity(),
		filters: filters{states: make(map[string]bool, len(watchbillstore.AllStates))},
		dirty:   make(chan struct{}, 1), shownEvent: -1, autoDraft: true, autoRefresh: true,
		verbs: make(map[string]*verbOutcome),
	}
	inst.machine = newJobMachine()
	inst.split = newSplitState()
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount takes the host's bus client, reads a launch config, wires the
// table reader, the task monitor and the announcements, and starts the
// poller on the lifetime scope.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.client = wb.NewClient(ctx.Bus())
	inst.client.Timeout = requestTimeout
	inst.reads = newTableReader(ctx.Bus())
	inst.tasks = task.ForApp(ctx)
	inst.stop = ctx.Cancel()
	if inst.scope = app.LifetimeOf(ctx); inst.scope == nil {
		// No host drain: the app owns a root scope and retires it at
		// Unmount.
		inst.scope, inst.ownScope = lifetime.New(nil), true
	}
	// The chip is tethered (T2): the badge opens the graph, table and
	// history, and the inspector says where the value came from.
	inst.chip = fsmview.New(inst.ids, "job-state", inst.machine, fsmview.Options[string]{
		Title: "job state", Tethered: true, AutoAnchor: true, BadgeTone: toneOf,
		Provenance: inspector.Provenance{Subject: stateProvenance, SourceApp: string(manifest.Id)},
	})
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

	inst.monitor = taskmonitor.New(inst.ids, "tm", inst.tasks, taskmonitor.Options{DefaultOpen: true})
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
	inst.scope.Go(inst.poll)
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

// Unmount stops the subscription and the monitor, and drops the job
// list's record. The poller and the verbs are on the lifetime scope: the
// host retired it before this and drains it after (ADR-0322); where the
// app owns the scope it drains it here, bounded.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
	if inst.ownScope {
		dctx, cancel := context.WithTimeout(context.Background(), ownDrain)
		defer cancel()
		if dErr := inst.scope.Drain(dctx); dErr != nil {
			inst.logger.Debug().Err(dErr).Msg("watchbill app: own scope not drained")
		}
	}
	if inst.monitor != nil {
		_ = inst.monitor.Close()
	}
	if inst.listRec != nil {
		inst.listRec.Release()
		inst.listRec = nil
	}
	if inst.trailRec != nil {
		inst.trailRec.Release()
		inst.trailRec = nil
	}
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.frameCtx = ctx
	inst.density = styletokens.ActiveDensity()
	inst.ensureWidths(ctx)
	if inst.landed.Swap(false) {
		// A snapshot landed since the last frame: one repaint, so the
		// window shows it before the next input (W7).
		c.RequestRepaint()
	}
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
		MinPoints: float64(recordtable.DragMinWidth(inst.density)),
		MaxPoints: float64(recordtable.MaxColumnWidth),
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

// SetPaused stops the poller's reads while the person has paused the window,
// and reads at once on the resume (ADR-0354 §SD2).
func (inst *App) SetPaused(paused bool) {
	inst.paused.Store(paused)
	if !paused {
		inst.markDirty()
	}
}

func (inst *App) markDirty() {
	select {
	case inst.dirty <- struct{}{}:
	default:
	}
}

// poll is the poller, on the lifetime scope: it ends when the scope's
// context ends or the host closes the mount context's cancel (W5).
func (inst *App) poll(ctx context.Context) {
	ticker := time.NewTicker(refreshEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			inst.storeSplit()
			return
		case <-inst.stop:
			inst.storeSplit()
			return
		case <-ticker.C:
			inst.storeSplit()
			if !inst.autoRefreshOn() {
				continue
			}
		case <-inst.dirty:
		}
		if inst.paused.Load() {
			continue
		}
		inst.refresh(ctx)
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
func (inst *App) refresh(ctx context.Context) {
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
	inst.mu.Unlock()
	inst.landed.Store(true)
}

func firstNonEmpty(a, b string) (s string) {
	if a != "" {
		return a
	}
	return b
}

func (inst *App) cancel(id string) {
	inst.verb("cancel", id, func() (applied bool, note string, err error) {
		ok, err := inst.client.Cancel(id, noteFrom)
		if err != nil {
			return false, "", err
		}
		if !ok {
			return false, "cancel did not apply to " + short(id), nil
		}
		return true, "cancel asked for " + short(id), nil
	})
}

func (inst *App) retry(id string) {
	inst.verb("retry", id, func() (applied bool, note string, err error) {
		ok, err := inst.client.Retry(id, noteFrom)
		if err != nil {
			return false, "", err
		}
		if !ok {
			return false, "retry did not apply to " + short(id), nil
		}
		return true, "retried " + short(id), nil
	})
}

// verb runs one request on the lifetime scope and lands its outcome: in
// the status row, and under the job's id for the async command that
// asked (ADR-0358). A scope that retired starts nothing.
func (inst *App) verb(name string, id string, fn func() (applied bool, note string, err error)) {
	if inst.client == nil || inst.scope == nil || inst.scope.IsRetired() {
		return
	}
	inst.mu.Lock()
	inst.snap.inflight++
	inst.verbs[id] = &verbOutcome{}
	inst.mu.Unlock()
	if !inst.scope.Go(func(context.Context) {
		applied, note, err := fn()
		inst.mu.Lock()
		inst.snap.inflight--
		if err != nil {
			inst.snap.lastError = name + ": " + err.Error()
		} else {
			inst.snap.lastError = ""
			inst.snap.lastNote = note
		}
		inst.verbs[id] = &verbOutcome{done: true, applied: applied, note: note, err: err}
		inst.mu.Unlock()
		inst.markDirty()
	}) {
		inst.mu.Lock()
		inst.snap.inflight--
		delete(inst.verbs, id)
		inst.mu.Unlock()
	}
}

// outcome is the last verb's outcome for id; pending is true while it has
// not landed, and found false when none was asked.
func (inst *App) outcome(id string) (o verbOutcome, found bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	v, found := inst.verbs[id]
	if found {
		o = *v
	}
	return
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

// visibleJobs applies the kind battery, the window's own filter (F1); the
// battery is compiled when the query moves, not per frame.
func (inst *App) visibleJobs(jobs []watchbillstore.Job) (out []watchbillstore.Job) {
	q := strings.TrimSpace(inst.filters.kind)
	if q != inst.kindQuery {
		inst.kindQuery, inst.kindBattery = q, search.ParseQuery(q)
	}
	return visible(jobs, inst.kindBattery)
}

// visible keeps the jobs whose kind every pattern of the battery hits; an
// empty battery keeps them all.
func visible(jobs []watchbillstore.Job, battery search.Battery) (out []watchbillstore.Job) {
	if battery.IsZero() {
		return jobs
	}
	for _, j := range jobs {
		if kindMatches(battery, j.Kind) {
			out = append(out, j)
		}
	}
	return
}

func kindMatches(battery search.Battery, kind string) (ok bool) {
	for i := range battery.Patterns {
		if !battery.Patterns[i].Matches(kind) {
			return false
		}
	}
	return true
}

// kindLiteral says a token of the compiled kind battery did not compile
// and matches literally, so the row can say so.
func (inst *App) kindLiteral() (literal bool) {
	for i := range inst.kindBattery.Patterns {
		if inst.kindBattery.Patterns[i].Literal {
			return true
		}
	}
	return false
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
