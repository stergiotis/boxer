//go:build llm_generated_opus47

package supervisor

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcancel"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcreated"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskdone"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskerror"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskprogress"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// DefaultHeartbeatThresholdMs is the no-emission gap after which an
// in-flight task is promoted to InflightStateAbandoned.
const DefaultHeartbeatThresholdMs int64 = 30_000

// DefaultHeartbeatTickMs is the watchdog's scan cadence. Coarser than
// the threshold by ~6x so a barely-late producer doesn't get marked
// abandoned in the first window.
const DefaultHeartbeatTickMs int64 = 5_000

// Opts configures the supervisor at construction. All fields have
// sensible defaults; Opts{} is a valid zero value.
type Opts struct {
	HeartbeatThresholdMs int64
	HeartbeatTickMs      int64
	ListSubject          string
	NowFn                func() time.Time
}

// InflightEntry is the Go-facing in-memory row exposed by
// InflightSnapshot. Carries the raw wire payloads so the caller can
// reach for any field the projected wire shape (InflightSnapshotEntry)
// omits.
type InflightEntry struct {
	Created    taskcreated.TaskCreated
	Progress   taskprogress.TaskProgress // zero value when no progress observed yet
	State      InflightStateE
	LastEmitMs int64
}

type entry struct {
	created    taskcreated.TaskCreated
	progress   taskprogress.TaskProgress
	state      InflightStateE
	lastEmitMs int64
}

// Supervisor is the audit + watchdog hub. Construct with New, drive
// with Start / Stop; the embedded task.ObserverI methods are wired by
// Start via task.WatchAll and should not be invoked by callers
// directly (they are exported only because the interface is).
type Supervisor struct {
	bus   app.BusI
	facts factsstore.FactsStoreI
	log   zerolog.Logger
	nowFn func() time.Time

	heartbeatThresholdMs int64
	heartbeatTickMs      int64
	listSubject          string

	mu        sync.Mutex
	inflight  map[task.TaskIdT]*entry
	persisted atomic.Uint64

	started atomic.Bool

	unsubWatch func()
	unsubList  func()

	stopCh chan struct{}
	doneCh chan struct{}
}

var _ task.ObserverI = (*Supervisor)(nil)

// New constructs a Supervisor against the provided bus + facts store.
// A nil facts store is permitted — the supervisor still maintains the
// in-flight map and serves list-inflight requests; audit rows are
// dropped. Useful for tests that want lifecycle visibility without
// asserting on persisted rows.
func New(bus app.BusI, facts factsstore.FactsStoreI, log zerolog.Logger, opts Opts) (inst *Supervisor) {
	thresh := opts.HeartbeatThresholdMs
	if thresh <= 0 {
		thresh = DefaultHeartbeatThresholdMs
	}
	tick := opts.HeartbeatTickMs
	if tick <= 0 {
		tick = DefaultHeartbeatTickMs
	}
	listSubject := opts.ListSubject
	if listSubject == "" {
		listSubject = task.SubjectListInflight
	}
	nowFn := opts.NowFn
	if nowFn == nil {
		nowFn = time.Now
	}
	inst = &Supervisor{
		bus:                  bus,
		facts:                facts,
		log:                  log,
		nowFn:                nowFn,
		heartbeatThresholdMs: thresh,
		heartbeatTickMs:      tick,
		listSubject:          listSubject,
		inflight:             make(map[task.TaskIdT]*entry),
	}
	return
}

// Start subscribes to task.> and listSubject, then launches the
// heartbeat watchdog goroutine. Idempotent: a second call returns an
// error without restarting. Bus errors during subscribe roll back any
// partial state.
func (inst *Supervisor) Start() (err error) {
	if !inst.started.CompareAndSwap(false, true) {
		err = eh.Errorf("supervisor: already started")
		return
	}
	if inst.bus == nil {
		inst.started.Store(false)
		err = eh.Errorf("supervisor: nil bus")
		return
	}

	inst.stopCh = make(chan struct{})
	inst.doneCh = make(chan struct{})

	inst.unsubWatch, err = task.WatchAll(inst.bus, inst)
	if err != nil {
		inst.started.Store(false)
		err = eh.Errorf("supervisor: watch all: %w", err)
		return
	}
	inst.unsubList, err = inst.bus.Subscribe(inst.listSubject, inst.handleListRequest)
	if err != nil {
		inst.unsubWatch()
		inst.unsubWatch = nil
		inst.started.Store(false)
		err = eh.Errorf("supervisor: subscribe %s: %w", inst.listSubject, err)
		return
	}

	go inst.heartbeatLoop()
	return
}

// Stop tears down subscriptions and waits for the heartbeat goroutine
// to exit. Idempotent: calling on a non-started supervisor returns
// nil. Returns after the heartbeat goroutine has stopped so callers
// can rely on no further audit writes happening after Stop returns.
func (inst *Supervisor) Stop() (err error) {
	if !inst.started.CompareAndSwap(true, false) {
		return
	}
	close(inst.stopCh)
	<-inst.doneCh
	if inst.unsubWatch != nil {
		inst.unsubWatch()
		inst.unsubWatch = nil
	}
	if inst.unsubList != nil {
		inst.unsubList()
		inst.unsubList = nil
	}
	return
}

// PersistedCount returns the number of audit rows written since
// construction. Includes abandoned-promotion rows. Atomic — safe to
// call from any goroutine.
func (inst *Supervisor) PersistedCount() (n uint64) {
	n = inst.persisted.Load()
	return
}

// InflightSnapshot returns a copy of the current in-flight map. Each
// entry's payloads are by value so the caller may retain them; the
// snapshot itself is not live (subsequent supervisor mutations are not
// reflected). Order is unspecified but stable per call.
func (inst *Supervisor) InflightSnapshot() (entries []InflightEntry) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	entries = make([]InflightEntry, 0, len(inst.inflight))
	for _, e := range inst.inflight {
		entries = append(entries, InflightEntry{
			Created:    e.created,
			Progress:   e.progress,
			State:      e.state,
			LastEmitMs: e.lastEmitMs,
		})
	}
	return
}

// --- task.ObserverI ---------------------------------------------------

// identityFields builds the run_id / instance_id / task_* fields every
// audit row carries. Centralised so all five verbs (created / progress
// / cancel / done / error / abandoned) emit the same identity columns
// — readers can join across them without verb-specific knowledge.
func (inst *Supervisor) identityFields(taskId task.TaskIdT, kind, title string, ownerTileKey uint64, ownerRunId string) (fields []factsstore.LogField) {
	fields = []factsstore.LogField{
		{Name: "task_id", Kind: factsstore.LogFieldKindString, Str: string(taskId)},
	}
	if kind != "" {
		fields = append(fields, factsstore.LogField{
			Name: "task_kind", Kind: factsstore.LogFieldKindString, Str: kind,
		})
	}
	if title != "" {
		fields = append(fields, factsstore.LogField{
			Name: "task_title", Kind: factsstore.LogFieldKindString, Str: title,
		})
	}
	if ownerRunId != "" {
		fields = append(fields, factsstore.LogField{
			Name: "run_id", Kind: factsstore.LogFieldKindString, Str: ownerRunId,
		})
	}
	if ownerTileKey != 0 {
		fields = append(fields, factsstore.LogField{
			Name: "instance_id", Kind: factsstore.LogFieldKindUint, Uint: ownerTileKey,
		})
	}
	return
}

func (inst *Supervisor) OnCreated(c taskcreated.TaskCreated) {
	createdAtMs := c.At.UnixMilli()
	taskId := task.TaskIdT(c.TaskId)
	inst.mu.Lock()
	inst.inflight[taskId] = &entry{
		created:    c,
		state:      InflightStateRunning,
		lastEmitMs: createdAtMs,
	}
	inst.mu.Unlock()

	inst.writeAudit(factsstore.LogRow{
		AppId:   app.AppIdT(c.OwnerAppId),
		Level:   "info",
		Message: "task.created",
		Service: LogService,
		Ts:      time.UnixMilli(createdAtMs).UTC(),
		Fields:  inst.identityFields(taskId, c.Kind, c.Title, c.OwnerTileKey, c.OwnerRunId),
	})
}

func (inst *Supervisor) OnProgress(p taskprogress.TaskProgress) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	e, ok := inst.inflight[task.TaskIdT(p.TaskId)]
	if !ok {
		return
	}
	e.progress = p
	e.lastEmitMs = p.At.UnixMilli()
	// Progress emission proves the producer is alive — clear any
	// pending abandoned label so the snapshot reflects recovery.
	if e.state == InflightStateAbandoned {
		e.state = InflightStateRunning
	}
}

func (inst *Supervisor) OnCancel(c taskcancel.TaskCancel) {
	cancelAtMs := c.At.UnixMilli()
	taskId := task.TaskIdT(c.TaskId)
	inst.mu.Lock()
	var owner app.AppIdT
	var kind, title, runId string
	var tileKey uint64
	if e, ok := inst.inflight[taskId]; ok {
		e.state = InflightStateCancelling
		e.lastEmitMs = cancelAtMs
		owner = app.AppIdT(e.created.OwnerAppId)
		kind = e.created.Kind
		title = e.created.Title
		tileKey = e.created.OwnerTileKey
		runId = e.created.OwnerRunId
	}
	inst.mu.Unlock()

	fields := inst.identityFields(taskId, kind, title, tileKey, runId)
	if c.Reason != "" {
		fields = append(fields, factsstore.LogField{
			Name: "reason", Kind: factsstore.LogFieldKindString, Str: c.Reason,
		})
	}
	inst.writeAudit(factsstore.LogRow{
		AppId:   owner,
		Level:   "info",
		Message: "task.cancel",
		Service: LogService,
		Ts:      time.UnixMilli(cancelAtMs).UTC(),
		Fields:  fields,
	})
}

func (inst *Supervisor) OnDone(d taskdone.TaskDone) {
	doneAtMs := d.At.UnixMilli()
	taskId := task.TaskIdT(d.TaskId)
	inst.mu.Lock()
	var kind, title, runId string
	var owner app.AppIdT
	var startedMs int64
	var tileKey uint64
	if e, ok := inst.inflight[taskId]; ok {
		kind = e.created.Kind
		title = e.created.Title
		owner = app.AppIdT(e.created.OwnerAppId)
		startedMs = e.created.At.UnixMilli()
		tileKey = e.created.OwnerTileKey
		runId = e.created.OwnerRunId
		delete(inst.inflight, taskId)
	}
	inst.mu.Unlock()

	fields := inst.identityFields(taskId, kind, title, tileKey, runId)
	if startedMs > 0 {
		fields = append(fields, factsstore.LogField{
			Name: "duration_ms", Kind: factsstore.LogFieldKindInt, Int: doneAtMs - startedMs,
		})
	}
	if len(d.Result) > 0 {
		fields = append(fields, factsstore.LogField{
			Name: "result_bytes", Kind: factsstore.LogFieldKindUint, Uint: uint64(len(d.Result)),
		})
	}
	inst.writeAudit(factsstore.LogRow{
		AppId:   owner,
		Level:   "info",
		Message: "task.done",
		Service: LogService,
		Ts:      time.UnixMilli(doneAtMs).UTC(),
		Fields:  fields,
	})
}

func (inst *Supervisor) OnError(e taskerror.TaskError) {
	errAtMs := e.At.UnixMilli()
	taskId := task.TaskIdT(e.TaskId)
	inst.mu.Lock()
	var kind, title, runId string
	var owner app.AppIdT
	var startedMs int64
	var tileKey uint64
	if ent, ok := inst.inflight[taskId]; ok {
		kind = ent.created.Kind
		title = ent.created.Title
		owner = app.AppIdT(ent.created.OwnerAppId)
		startedMs = ent.created.At.UnixMilli()
		tileKey = ent.created.OwnerTileKey
		runId = ent.created.OwnerRunId
		delete(inst.inflight, taskId)
	}
	inst.mu.Unlock()

	fields := inst.identityFields(taskId, kind, title, tileKey, runId)
	if startedMs > 0 {
		fields = append(fields, factsstore.LogField{
			Name: "duration_ms", Kind: factsstore.LogFieldKindInt, Int: errAtMs - startedMs,
		})
	}
	if e.ErrorText != "" {
		// TaskError.ErrorText is plain-text v1 — FormatErrorWithStackS
		// rendering from the producer's handle, capturing the chain
		// in human-readable form. A future structured-chain wire
		// (CBOR streams envelope) would land as a separate column;
		// "error_text" stays the canonical readable surface.
		fields = append(fields, factsstore.LogField{
			Name: "error_text", Kind: factsstore.LogFieldKindString, Str: e.ErrorText,
		})
	}
	inst.writeAudit(factsstore.LogRow{
		AppId:   owner,
		Level:   "error",
		Message: "task.error",
		Error:   e.Reason,
		Service: LogService,
		Ts:      time.UnixMilli(errAtMs).UTC(),
		Fields:  fields,
	})
}

// --- heartbeat watchdog ----------------------------------------------

func (inst *Supervisor) heartbeatLoop() {
	defer close(inst.doneCh)
	ticker := time.NewTicker(time.Duration(inst.heartbeatTickMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-inst.stopCh:
			return
		case <-ticker.C:
			inst.scanAbandoned()
		}
	}
}

func (inst *Supervisor) scanAbandoned() {
	nowMs := inst.nowFn().UnixMilli()
	var promoted []taskcreated.TaskCreated
	inst.mu.Lock()
	for _, e := range inst.inflight {
		if e.state == InflightStateAbandoned {
			continue
		}
		if nowMs-e.lastEmitMs > inst.heartbeatThresholdMs {
			e.state = InflightStateAbandoned
			promoted = append(promoted, e.created)
		}
	}
	inst.mu.Unlock()

	for _, c := range promoted {
		fields := inst.identityFields(task.TaskIdT(c.TaskId), c.Kind, c.Title, c.OwnerTileKey, c.OwnerRunId)
		fields = append(fields, factsstore.LogField{
			Name: "threshold_ms", Kind: factsstore.LogFieldKindInt, Int: inst.heartbeatThresholdMs,
		})
		inst.writeAudit(factsstore.LogRow{
			AppId:   app.AppIdT(c.OwnerAppId),
			Level:   "warn",
			Message: "task.abandoned",
			Service: LogService,
			Ts:      time.UnixMilli(nowMs).UTC(),
			Fields:  fields,
		})
	}
}

// --- list-inflight request/reply -------------------------------------

func (inst *Supervisor) handleListRequest(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("supervisor: list-inflight request without reply")
		return
	}
	reply := inst.buildSnapshotReply()
	b, err := task.MarshalInflightSnapshotReply(reply)
	if err != nil {
		inst.log.Warn().Err(err).Msg("supervisor: marshal snapshot reply")
		return
	}
	err = inst.bus.Publish(msg.Reply, b)
	if err != nil {
		inst.log.Warn().Err(err).Str("inbox", msg.Reply).Msg("supervisor: publish snapshot reply")
	}
}

func (inst *Supervisor) buildSnapshotReply() (reply task.InflightSnapshotReply) {
	nowMs := inst.nowFn().UnixMilli()
	inst.mu.Lock()
	reply.Entries = make([]task.InflightSnapshotEntry, 0, len(inst.inflight))
	for _, e := range inst.inflight {
		entry := task.InflightSnapshotEntry{
			Id:          task.TaskIdT(e.created.TaskId),
			Kind:        e.created.Kind,
			Title:       e.created.Title,
			OwnerAppId:  app.AppIdT(e.created.OwnerAppId),
			State:       e.state.String(),
			CreatedAtMs: e.created.At.UnixMilli(),
			LastEmitMs:  e.lastEmitMs,
		}
		if !e.progress.At.IsZero() {
			entry.Current = e.progress.Current
			entry.Total = e.progress.Total
			entry.Unit = e.progress.Unit
			entry.EtaMs = e.progress.EtaMs
		}
		reply.Entries = append(reply.Entries, entry)
	}
	inst.mu.Unlock()
	reply.AtMs = nowMs
	return
}

// --- audit helpers ----------------------------------------------------

// writeAudit funnels every persistable event into FactsStoreI.WriteLog.
// A nil store is tolerated — the supervisor still maintains in-memory
// state and serves snapshots; WriteLog failures are logged but never
// surfaced to the bus dispatch path (an audit failure must not stall
// the producer or other observers).
func (inst *Supervisor) writeAudit(row factsstore.LogRow) {
	if inst.facts == nil {
		return
	}
	_, err := inst.facts.WriteLog(row)
	if err != nil {
		inst.log.Warn().Err(err).Str("message", row.Message).Msg("supervisor: write audit row")
		return
	}
	inst.persisted.Add(1)
}
