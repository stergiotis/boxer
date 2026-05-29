//go:build llm_generated_opus47

package taskmonitor

import (
	"fmt"
	"sync"
	"sync/atomic"

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
)

// DefaultMaxHistory caps the rolling history pane. Older terminal
// rows fall off the front; reads beyond this require the supervisor's
// audit trail.
const DefaultMaxHistory = 20

// Opts configures the widget at construction. Zero value is valid
// (DefaultOpen on, MaxHistory = DefaultMaxHistory).
type Opts struct {
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

// Inst is the widget instance. Construct via New, then drive
// Start / Render / Stop from the host's frame loop.
//
// Goroutine safety: ObserverI callbacks land on the bus dispatch
// goroutine (synchronous for in-proc, separate goroutine for NATS in
// M4); Render runs on the host's frame goroutine. The mutex guards
// the in-memory state shared between the two; Render snapshots
// under the lock and renders outside it.
type Inst struct {
	api      task.TaskApiI
	ids      *c.WidgetIdStack
	idPrefix string
	density  styletokens.DensityE
	opts     Opts

	mu sync.Mutex
	// inflight is keyed by TaskId; iteration order is lexicographic on
	// the nanoid (stable across frames, which is the property Render
	// relies on). Removal on terminal events uses Delete.
	inflight *containers.BinarySearchGrowingKV[task.TaskIdT, *inflightRow]
	history  []historyRow

	unsubscribe func()
	started     atomic.Bool
}

var _ task.ObserverI = (*Inst)(nil)

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
	final     string            // "done" | "error" | "cancelled"
	finalAt   int64
	reason    string
	errorText string
}

// New constructs a monitor bound to api. idPrefix scopes every widget
// id under the caller's ids stack — pass a stable short string so two
// monitors in the same panel don't collide.
func New(api task.TaskApiI, ids *c.WidgetIdStack, idPrefix string, opts Opts) (inst *Inst) {
	if opts.MaxHistory <= 0 {
		opts.MaxHistory = DefaultMaxHistory
	}
	inst = &Inst{
		api:      api,
		ids:      ids,
		idPrefix: idPrefix,
		density:  styletokens.DensityFromEnv(),
		opts:     opts,
		inflight: containers.NewBinarySearchGrowingKVOrdered[task.TaskIdT, *inflightRow](16),
	}
	return
}

// Start attaches the observer to the bus. Idempotent: a second call
// returns an error without altering state. Seeds from supervisor when
// Opts.SeedFromSupervisor is set; a failed seed is logged-by-caller
// (the returned err is best-effort) but does not block subscribing.
func (inst *Inst) Start() (err error) {
	if !inst.started.CompareAndSwap(false, true) {
		err = eh.Errorf("taskmonitor: already started")
		return
	}
	if inst.opts.SeedFromSupervisor {
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

// Stop unsubscribes. Safe to call on a non-started monitor (no-op).
func (inst *Inst) Stop() (err error) {
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
func (inst *Inst) InflightCount() (n int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n = inst.inflight.Len()
	return
}

func (inst *Inst) HistoryCount() (n int) {
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
func (inst *Inst) seedFromSnapshot(entries []task.InflightSnapshotEntry) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, e := range entries {
		created := taskcreated.TaskCreated{
			TaskId:     string(e.Id),
			Kind:       e.Kind,
			Title:      e.Title,
			OwnerAppId: string(e.OwnerAppId),
			AtNs:       e.CreatedAtMs * 1_000_000,
		}
		progress := taskprogress.TaskProgress{
			TaskId:  string(e.Id),
			Current: e.Current,
			Total:   e.Total,
			Unit:    e.Unit,
			EtaMs:   e.EtaMs,
			AtNs:    e.LastEmitMs * 1_000_000,
		}
		inst.inflight.UpsertSingle(e.Id, &inflightRow{
			created: created,
			latest:  progress,
			pending: e.State == "cancelling" || e.State == "abandoned",
		})
	}
}

// --- task.ObserverI ---------------------------------------------------

func (inst *Inst) OnCreated(cr taskcreated.TaskCreated) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.inflight.UpsertSingle(task.TaskIdT(cr.TaskId), &inflightRow{created: cr})
}

func (inst *Inst) OnProgress(p taskprogress.TaskProgress) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	row, ok := inst.inflight.Get(task.TaskIdT(p.TaskId))
	if !ok {
		return
	}
	row.latest = p
}

func (inst *Inst) OnCancel(cn taskcancel.TaskCancel) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if row, ok := inst.inflight.Get(task.TaskIdT(cn.TaskId)); ok {
		row.pending = true
		row.cancelAt = cn.AtNs / 1_000_000
		if row.latest.Note == "" {
			row.latest.Note = "cancelling…"
		}
	}
}

func (inst *Inst) OnDone(d taskdone.TaskDone) {
	inst.terminal(task.TaskIdT(d.TaskId), "done", d.AtNs/1_000_000, "", nil)
}

func (inst *Inst) OnError(e taskerror.TaskError) {
	inst.terminal(task.TaskIdT(e.TaskId), "error", e.AtNs/1_000_000, e.Reason, []byte(e.ErrorText))
}

func (inst *Inst) terminal(id task.TaskIdT, final string, atMs int64, reason string, errorBytes []byte) {
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
	if len(inst.history) > inst.opts.MaxHistory {
		inst.history = inst.history[len(inst.history)-inst.opts.MaxHistory:]
	}
}

// --- render ----------------------------------------------------------

// Render draws the widget body. Single-threaded — the host's frame
// goroutine. Snapshots state under the lock then renders without it
// so ObserverI callbacks aren't blocked behind the egui scope.
func (inst *Inst) Render() {
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

	inst.renderInflight(inflight)
	c.AddSpace(styletokens.PaddingOuter(inst.density))
	inst.renderHistory(history)
}

func (inst *Inst) renderInflight(rows []inflightRow) {
	hdr := c.WidgetText().Text(fmt.Sprintf("In-flight (%d)", len(rows))).Keep()
	for range c.CollapsingHeader(inst.ids.PrepareStr(inst.idPrefix+":hdr-inflight"), hdr).
		DefaultOpen(inst.opts.DefaultOpen).KeepIter() {
		if len(rows) == 0 {
			c.Label("(no running tasks)").Send()
			return
		}
		for _, row := range rows {
			inst.renderInflightRow(row)
			c.AddSpace(styletokens.PaddingInner(inst.density))
		}
	}
}

func (inst *Inst) renderInflightRow(row inflightRow) {
	c.Label(row.created.Title).Send()
	c.ProgressBar(progressFraction(row.latest)).Send()
	c.Label(humanizeRow(row.latest, row.pending)).Send()

	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("id %s · kind %s", row.created.TaskId, row.created.Kind)).Send()
		if !row.pending {
			if c.Button(inst.ids.PrepareStr(inst.idPrefix+":cancel-"+row.created.TaskId),
				c.Atoms().Text("Cancel").Keep()).
				SendResp().HasPrimaryClicked() {
				id := task.TaskIdT(row.created.TaskId)
				go func() {
					_ = inst.api.RequestCancel(id, "user clicked cancel")
				}()
			}
		}
	}
}

func (inst *Inst) renderHistory(rows []historyRow) {
	hdr := c.WidgetText().Text(fmt.Sprintf("History (%d)", len(rows))).Keep()
	for range c.CollapsingHeader(inst.ids.PrepareStr(inst.idPrefix+":hdr-history"), hdr).
		DefaultOpen(inst.opts.DefaultOpen).KeepIter() {
		if len(rows) == 0 {
			c.Label("(no finished tasks yet)").Send()
			return
		}
		// Newest-first so the most recent terminal is at the top —
		// matches the user's mental model after clicking Cancel.
		for i := len(rows) - 1; i >= 0; i-- {
			inst.renderHistoryRow(rows[i], i)
			c.AddSpace(styletokens.PaddingInner(inst.density))
		}
	}
}

func (inst *Inst) renderHistoryRow(row historyRow, idx int) {
	label := fmt.Sprintf("[%s] %s · %s",
		row.final, row.created.Title, humanizeRow(row.progress, false))
	if row.reason != "" {
		label = label + " — " + row.reason
	}
	c.Label(label).Send()

	// Error details: collapsing block carrying the
	// FormatErrorWithStackS rendering from TaskError.Error. Plain
	// text v1 — see EXPLANATION on why we don't decode a structured
	// chain here.
	if row.errorText != "" {
		errId := inst.ids.PrepareStr(fmt.Sprintf("%s:err-%d", inst.idPrefix, idx))
		errHdr := c.WidgetText().Text("details").Keep()
		for range c.CollapsingHeader(errId, errHdr).DefaultOpen(false).KeepIter() {
			c.Label(row.errorText).Send()
		}
	}
}

// progressFraction returns a [0..1] float for the progress bar.
// Indeterminate tasks (Total=0) render as 0.0; the humanized note
// carries the "indeterminate" signal.
func progressFraction(p taskprogress.TaskProgress) (frac float32) {
	if p.Total == 0 {
		return
	}
	if p.Current >= p.Total {
		frac = 1
		return
	}
	frac = float32(float64(p.Current) / float64(p.Total))
	return
}

// humanizeRow composes a visible string from a wire TaskProgress.
// Lightweight inverse of the estimator's emission gate: percent or
// raw count + optional ETA + optional note. Observers re-humanize
// per their own locale; this is the widget's house format.
func humanizeRow(p taskprogress.TaskProgress, pending bool) (s string) {
	if pending {
		s = "cancelling…"
		return
	}
	if p.AtNs == 0 {
		s = "starting…"
		return
	}
	var head string
	switch {
	case p.Unit == "bytes" && p.Total > 0:
		head = fmt.Sprintf("%s / %s", humanize.IBytes(p.Current), humanize.IBytes(p.Total))
	case p.Unit == "bytes":
		head = humanize.IBytes(p.Current)
	case p.Total > 0:
		pct := int(float64(p.Current) * 100.0 / float64(p.Total))
		head = fmt.Sprintf("%d%%", pct)
	default:
		head = fmt.Sprintf("%d %s", p.Current, p.Unit)
	}
	if p.EtaMs > 0 {
		head = head + " · " + formatDurationMs(p.EtaMs) + " left"
	}
	if p.Note != "" {
		head = head + " · " + p.Note
	}
	s = head
	return
}

func formatDurationMs(ms int64) (s string) {
	switch {
	case ms < 1000:
		s = fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		s = fmt.Sprintf("%ds", ms/1000)
	default:
		s = fmt.Sprintf("%dm%02ds", ms/60_000, (ms%60_000)/1000)
	}
	return
}
