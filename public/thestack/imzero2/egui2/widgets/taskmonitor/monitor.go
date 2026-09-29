package taskmonitor

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcancel"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcreated"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskdone"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskerror"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskprogress"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
)

// DefaultMaxHistory caps the rolling history pane. Older terminal
// rows fall off the front; reads beyond this require the supervisor's
// audit trail.
const DefaultMaxHistory = 20

// Options configures the monitor. The zero value is valid (MaxHistory =
// DefaultMaxHistory); MaxHistory and DefaultOpen are re-read on every frame
// through [Monitor.Opts], SeedFromSupervisor at Start.
type Options struct {
	// MaxHistory is the rolling-window size for the history pane.
	// Zero ⇒ DefaultMaxHistory.
	MaxHistory int

	// DefaultOpen sets the initial collapsed/expanded state of both
	// the In-flight and History collapsing headers. Default true so
	// consumers see content immediately.
	DefaultOpen bool

	// SeedFromSupervisor causes Start to issue a single
	// task.list.inflight Request through the api to populate the
	// in-flight map before subscribing. When false (default), the
	// widget starts empty and fills as new task.created events
	// arrive. Set true when attaching to a long-running runtime so
	// already-running tasks become visible immediately.
	SeedFromSupervisor bool
}

// Monitor is the widget instance (semi-retained, ADR-0267). Construct via
// New, then drive Start / Render / Close from the host's frame loop; Close
// is required once Start succeeded (W14) — it releases the bus subscription.
//
// Goroutine safety: ObserverI callbacks land on the bus dispatch
// goroutine (synchronous for in-proc, separate goroutine for NATS in
// M4); Render runs on the host's frame goroutine. The mutex guards
// the in-memory state shared between the two; Render snapshots
// under the lock and renders outside it.
type Monitor struct {
	// Opts is re-read on every Render, so a change is an assignment.
	Opts Options

	api      task.TaskApiI
	ids      *c.WidgetIdStack
	scopeKey string
	density  styletokens.DensityE

	mu sync.Mutex
	// inflight is keyed by TaskId; iteration order is lexicographic on
	// the nanoid (stable across frames, which is the property Render
	// relies on). Removal on terminal events uses Delete.
	inflight *containers.BinarySearchGrowingKV[task.TaskIdT, *inflightRow]
	history  []historyRow

	unsubscribe func()
	started     atomic.Bool
}

var _ task.ObserverI = (*Monitor)(nil)

// inflightRow is the per-running-task UI state. Updated by ObserverI
// callbacks under inst.mu; read by Render under the same lock.
type inflightRow struct {
	created  taskcreated.TaskCreated
	latest   taskprogress.TaskProgress
	pending  bool // cancel requested but no terminal yet
	cancelAt int64
}

// historyRow is the per-finished-task UI state. Append-only with a
// rolling cap. errorText holds the FormatErrorWithStackS rendering
// from TaskError.Error so the row can expand a "details" pane on
// demand. Empty for non-error terminals.
type historyRow struct {
	created   taskcreated.TaskCreated
	progress  taskprogress.TaskProgress // last seen, may be zero
	final     string                    // "done" | "error" | "cancelled"
	finalAt   int64
	reason    string
	errorText string
}

// New constructs a monitor bound to api whose ids are scoped under scopeKey
// on ids (empty uses "taskmonitor"); two monitors under one stack need
// distinct keys.
func New(ids *c.WidgetIdStack, scopeKey string, api task.TaskApiI, opts Options) (inst *Monitor) {
	if scopeKey == "" {
		scopeKey = "taskmonitor"
	}
	inst = &Monitor{
		Opts:     opts,
		api:      api,
		ids:      ids,
		scopeKey: scopeKey,
		density:  styletokens.ActiveDensity(),
		inflight: containers.NewBinarySearchGrowingKVOrdered[task.TaskIdT, *inflightRow](16),
	}
	return
}

func (inst *Monitor) maxHistory() int {
	if inst.Opts.MaxHistory <= 0 {
		return DefaultMaxHistory
	}
	return inst.Opts.MaxHistory
}

// Start attaches the observer to the bus. Idempotent: a second call
// returns an error without altering state. Seeds from supervisor when
// Opts.SeedFromSupervisor is set; a failed seed is logged-by-caller
// (the returned err is best-effort) but does not block subscribing.
func (inst *Monitor) Start() (err error) {
	if !inst.started.CompareAndSwap(false, true) {
		err = eh.Errorf("taskmonitor: already started")
		return
	}
	if inst.Opts.SeedFromSupervisor {
		entries, lErr := inst.api.ListInflight()
		if lErr == nil {
			inst.seedFromSnapshot(entries)
		}
		// A list-inflight failure is non-fatal — observers still
		// catch every new event. Surface via the watch err below.
	}
	unsub, wErr := inst.api.WatchAll(inst)
	if wErr != nil {
		inst.started.Store(false)
		err = eh.Errorf("taskmonitor: watch all: %w", wErr)
		return
	}
	inst.unsubscribe = unsub
	return
}

// Close unsubscribes from the bus. Safe to call on a non-started monitor
// (no-op). Required after a successful Start.
func (inst *Monitor) Close() (err error) {
	if !inst.started.CompareAndSwap(true, false) {
		return
	}
	if inst.unsubscribe != nil {
		inst.unsubscribe()
		inst.unsubscribe = nil
	}
	return
}

// InflightCount + HistoryCount expose row counts for callers that
// want to render a header summary or status line outside the widget
// body.
func (inst *Monitor) InflightCount() (n int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n = inst.inflight.Len()
	return
}

func (inst *Monitor) HistoryCount() (n int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n = len(inst.history)
	return
}

// seedFromSnapshot pre-populates the in-flight map from a supervisor
// snapshot. Called by Start when Opts.SeedFromSupervisor is set. The
// snapshot entries lack the original TaskCreated payload, so we
// reconstruct a partial Created from the entry fields the supervisor
// surfaces.
func (inst *Monitor) seedFromSnapshot(entries []task.InflightSnapshotEntry) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, e := range entries {
		created := taskcreated.TaskCreated{
			TaskId:     string(e.Id),
			Kind:       e.Kind,
			Title:      e.Title,
			OwnerAppId: string(e.OwnerAppId),
			At:         time.UnixMilli(e.CreatedAtMs).UTC(),
		}
		progress := taskprogress.TaskProgress{
			TaskId:  string(e.Id),
			Current: e.Current,
			Total:   e.Total,
			Unit:    e.Unit,
			EtaMs:   e.EtaMs,
			At:      time.UnixMilli(e.LastEmitMs).UTC(),
		}
		inst.inflight.UpsertSingle(e.Id, &inflightRow{
			created: created,
			latest:  progress,
			pending: e.State == "cancelling" || e.State == "abandoned",
		})
	}
}

// --- task.ObserverI ---------------------------------------------------

func (inst *Monitor) OnCreated(cr taskcreated.TaskCreated) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.inflight.UpsertSingle(task.TaskIdT(cr.TaskId), &inflightRow{created: cr})
}

func (inst *Monitor) OnProgress(p taskprogress.TaskProgress) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	row, ok := inst.inflight.Get(task.TaskIdT(p.TaskId))
	if !ok {
		return
	}
	row.latest = p
}

func (inst *Monitor) OnCancel(cn taskcancel.TaskCancel) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if row, ok := inst.inflight.Get(task.TaskIdT(cn.TaskId)); ok {
		row.pending = true
		row.cancelAt = cn.At.UnixMilli()
		if row.latest.Note == "" {
			row.latest.Note = "cancelling…"
		}
	}
}

func (inst *Monitor) OnDone(d taskdone.TaskDone) {
	inst.terminal(task.TaskIdT(d.TaskId), "done", d.At.UnixMilli(), "", nil)
}

func (inst *Monitor) OnError(e taskerror.TaskError) {
	inst.terminal(task.TaskIdT(e.TaskId), "error", e.At.UnixMilli(), e.Reason, []byte(e.ErrorText))
}

func (inst *Monitor) terminal(id task.TaskIdT, final string, atMs int64, reason string, errorBytes []byte) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	row, ok := inst.inflight.Get(id)
	if !ok {
		return
	}
	// Done after pending cancel ⇒ user-visible terminal is "cancelled"
	// so the history pane reflects intent, not the bus terminal verb.
	if row.pending && final == "done" {
		final = "cancelled"
	}
	inst.inflight.Delete(id)
	inst.history = append(inst.history, historyRow{
		created:   row.created,
		progress:  row.latest,
		final:     final,
		finalAt:   atMs,
		reason:    reason,
		errorText: string(errorBytes),
	})
	if maxHist := inst.maxHistory(); len(inst.history) > maxHist {
		inst.history = inst.history[len(inst.history)-maxHist:]
	}
}

// --- render ----------------------------------------------------------

// Render draws the widget body. Single-threaded — the host's frame
// goroutine. Snapshots state under the lock then renders without it
// so ObserverI callbacks aren't blocked behind the egui scope.
// inflightRepaintSecs is how soon the monitor asks to be drawn again while
// a task is in flight: a few progress reports (the producers tick at tens
// of milliseconds), and well under any idle heartbeat.
const inflightRepaintSecs = 0.1

// Events is what one Render reports.
type Events struct {
	// CancelRequested lists the tasks whose Cancel was clicked this frame;
	// the monitor has already asked the api to cancel them.
	CancelRequested []task.TaskIdT
}

func (inst *Monitor) Render() (ev Events) {
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		ev = inst.render()
	}
	return
}

func (inst *Monitor) render() (ev Events) {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	inst.mu.Lock()
	inflight := make([]inflightRow, 0, inst.inflight.Len())
	// IterateValues yields *inflightRow in TaskId order; dereference to
	// snapshot a value copy so subsequent observer-callback mutations
	// don't race the render scope.
	for row := range inst.inflight.IterateValues() {
		inflight = append(inflight, *row)
	}
	history := append([]historyRow(nil), inst.history...)
	inst.mu.Unlock()

	// Progress arrives on the bus between frames; a host with a reactive
	// cadence (ADR-0062) repaints only when asked, so ask while anything
	// is in flight. Continuous hosts ignore the request.
	if len(inflight) > 0 {
		c.RequestRepaintAfter(inflightRepaintSecs)
	}
	ev.CancelRequested = inst.renderInflight(inflight)
	c.AddSpace(styletokens.PaddingOuter(inst.density))
	inst.renderHistory(history)
	return
}

func (inst *Monitor) renderInflight(rows []inflightRow) (cancelled []task.TaskIdT) {
	hdr := c.WidgetText().Text(fmt.Sprintf("In-flight (%d)", len(rows))).Keep()
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-inflight"), hdr).
		DefaultOpen(inst.Opts.DefaultOpen).KeepIter() {
		if len(rows) == 0 {
			c.Label("(no running tasks)").Send()
			return
		}
		for range c.IdScope(inst.ids.PrepareStr("tasks")) {
			for _, row := range rows {
				// One scope per task, keyed by its id (a nanoid), so the row's
				// widgets survive the list reordering around it.
				for range c.IdScope(inst.ids.PrepareStr(row.created.TaskId)) {
					if inst.renderInflightRow(row) {
						cancelled = append(cancelled, task.TaskIdT(row.created.TaskId))
					}
				}
				c.AddSpace(styletokens.PaddingInner(inst.density))
			}
		}
	}
	return
}

func (inst *Monitor) renderInflightRow(row inflightRow) (cancelClicked bool) {
	in := progressInput(row.latest, row.pending)
	in.Title = row.created.Title
	in.Ids, in.ScopeKey, in.Cancel = inst.ids, "job", !row.pending
	if jobprogress.Render(in).CancelClicked {
		cancelClicked = true
		id := task.TaskIdT(row.created.TaskId)
		go func() {
			_ = inst.api.RequestCancel(id, "user clicked cancel")
		}()
	}
	for rt := range c.RichTextLabel(fmt.Sprintf("id %s · kind %s", row.created.TaskId, row.created.Kind)) {
		rt.Small().Weak()
	}
	return
}

func (inst *Monitor) renderHistory(rows []historyRow) {
	hdr := c.WidgetText().Text(fmt.Sprintf("History (%d)", len(rows))).Keep()
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-history"), hdr).
		DefaultOpen(inst.Opts.DefaultOpen).KeepIter() {
		if len(rows) == 0 {
			c.Label("(no finished tasks yet)").Send()
			return
		}
		// Newest-first so the most recent terminal is at the top —
		// matches the user's mental model after clicking Cancel.
		for range c.IdScope(inst.ids.PrepareStr("history")) {
			for i := len(rows) - 1; i >= 0; i-- {
				for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
					inst.renderHistoryRow(rows[i])
				}
				c.AddSpace(styletokens.PaddingInner(inst.density))
			}
		}
	}
}

func (inst *Monitor) renderHistoryRow(row historyRow) {
	label := fmt.Sprintf("[%s] %s · %s",
		row.final, row.created.Title, jobprogress.StatusLine(progressInput(row.progress, false)))
	if row.reason != "" {
		label = label + " — " + row.reason
	}
	c.Label(label).Send()

	// Error details: collapsing block carrying the
	// FormatErrorWithStackS rendering from TaskError.Error. Plain
	// text v1 — see EXPLANATION on why we don't decode a structured
	// chain here.
	if row.errorText != "" {
		errHdr := c.WidgetText().Text("details").Keep()
		for range c.CollapsingHeader(inst.ids.PrepareStr("err"), errHdr).DefaultOpen(false).KeepIter() {
			c.Label(row.errorText).Send()
		}
	}
}

// progressInput maps a wire TaskProgress onto the shared job-progress
// row: the producer's estimate (task/estimator — the same Holt smoothing
// jobprogress's own callers use, ADR-0247) supplies rate and ETA, and a
// count-shaped task (indeterminate, or measured in bytes) leads with its
// amount instead of a bare percentage.
func progressInput(p taskprogress.TaskProgress, pending bool) (in jobprogress.Input) {
	in.Fraction = -1
	if p.At.IsZero() {
		in.Note = "starting…"
		if pending {
			in.Note = "cancelling…"
		}
		return
	}
	defer func() {
		if pending {
			// Hold the bar where it was; the figures no longer describe a
			// running task.
			in.Rate, in.EtaMs, in.Note = 0, 0, "cancelling…"
		}
	}()
	if p.Total > 0 {
		in.Fraction = float32(float64(min(p.Current, p.Total)) / float64(p.Total))
		in.EtaMs = p.EtaMs
	}
	switch {
	case p.Unit == "bytes" && p.Total > 0:
		in.Amount = fmt.Sprintf("%s / %s · %d%%", humanize.IBytes(p.Current), humanize.IBytes(p.Total), int(in.Fraction*100))
	case p.Unit == "bytes":
		in.Amount = humanize.IBytes(p.Current)
	case p.Unit == "steps" && p.Total > 0:
		in.Amount = fmt.Sprintf("step %d of %d", p.Current, p.Total)
	case p.Total == 0:
		in.Amount = fmt.Sprintf("%s %s", humanize.Comma(int64(p.Current)), p.Unit)
	}
	if p.Unit != "steps" {
		// A rate of steps says nothing a reader can use.
		in.Rate = p.ThroughputPerSec
		in.RateUnit = p.Unit
	}
	in.Note = p.Note
	return
}
