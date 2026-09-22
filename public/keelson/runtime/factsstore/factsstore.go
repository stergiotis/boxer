// Package factsstore is the durable view of runtime facts per ADR-0026 §SD6.
// Capability grants from the broker, audit records from the bus, the run and
// app lifecycle trail and launches all flow through FactsStoreI. Two backends implement it: InMemoryFactsStore
// here, and the boxer.facts-backed Store in
// [github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore],
// which writes CH+leeway rows through the factsschema package.
// chstore.NewWithFallback picks between them at runtime, degrading to the
// in-memory store when ClickHouse is unreachable.
//
// App state does not flow through here any more: persist state, workingsets
// and column-width overrides live on a generated record store over their own
// table (persist.StoreBackend, ADR-0105 D3a and its Update of 2026-08-15),
// and the facts-bound state verbs were removed once each kind had moved.
//
// Row types are typed per-kind so the broker / audit / lifecycle code stays
// readable; the leeway translation lives behind the FactsStoreI boundary, and
// this package deliberately imports no leeway at all.
//
// # This is the hand-rolled lane
//
// Every kind here costs a hand-written verb, its hand-encoded leeway DML, and
// hand-composed read-back SQL. A *new* fact kind should not land that way:
// ADR-0105 §D5 puts it on a generated record store behind this facade
// instead. The name of this package is the main reason someone misses that,
// so: start at doc/explanation/facts-bound-record-stores.md, which covers
// which lane to pick, the two DTO shapes the generated lane still refuses,
// and why the keyed-lookup verbs cannot move yet.
package factsstore

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// errEmptyRunId is the sentinel returned by writers that require a
// non-empty RunId. Exported via the wrapper functions (heartbeat
// today; future kinds that also require a run anchor can reuse).
var errEmptyRunId = errors.New("factsstore: RunId is required")

// GrantRow is one approved capability grant. Maps to a boxer.facts row
// with the KindGrant + AppRefPrefix(appId) + GrantSubjectPattern /
// GrantDirection / GrantReason / GrantSticky / GrantVia memberships under
// ADR-0026 §SD6.
//
// InstanceKey is the window that asked, taken from the bus envelope
// (ADR-0191 §SD4). Zero is unattributed — a grant minted by the host, or one
// requested over a transport that carries no instance. The grant itself
// stays addressed to an app id: a cap is an app-level fact, and this records
// which window's request produced it, not who holds it.
type GrantRow struct {
	AppId       app.AppIdT
	InstanceKey uint64
	Pattern     string
	Direction   app.CapDirectionE
	Reason      string
	Sticky      bool
	GrantedVia  string
	Ts          time.Time
	ExpiresAt   time.Time // zero == no TTL
}

// AuditRow is one audited bus request. Maps to a boxer.facts row with
// KindAudit + AppRefPrefix(appId) + AuditRequestSubject / AuditResult /
// AuditLatencyMs / AuditRequestSize / AuditResponseSize.
//
// InstanceKey is the window that made the request (ADR-0191 §SD4), recorded
// by the bus client that captured it. It is the field this kind most needed:
// audit is the highest-volume app-attributed kind, and with two windows of
// one app open the app id alone cannot say which one spoke.
type AuditRow struct {
	AppId         app.AppIdT
	InstanceKey   uint64
	Subject       string
	Result        string // "ok" | "denied" | "timeout" | "error"
	LatencyMs     uint32
	RequestSizeB  uint32
	ResponseSizeB uint32
	Ts            time.Time
}

// RuntimeStartRow records one process boot: a "this run started" event
// captured at runtime entry by the carousel. Maps to a boxer.facts row
// with KindRuntimeRun + the run_id-bearing MembRuntimeRun mixed-low-card
// reference (so the row joins to its child app-lifecycle rows by run_id)
// + hostname / pid / Go version / VCS revision / modified / build-info /
// module-path memberships on the string-, u64-, symbol-, and bool-sections.
//
// Written once per process, very early. Failure to persist is logged but
// must not block the runtime — the audit trail is best-effort.
type RuntimeStartRow struct {
	RunId        string
	Hostname     string
	Pid          int
	GoVersion    string
	VcsRevision  string
	VcsModified  bool
	VcsBuildInfo string
	ModulePath   string
	Ts           time.Time
}

// HeartbeatRow records one runtime liveness tick. Maps to a
// boxer.facts row with KindRuntimeHeartbeat + MembRuntimeRun
// mixed-LCR(run_id). Periodic; the carousel emits one every N seconds
// while the process is alive. Readers compute liveness from the gap
// between the latest heartbeat ts and now (or the next runtime-start
// timestamp). RunId is required; an empty value is rejected by the
// writers. Failure to persist is logged but must not block the runtime.
type HeartbeatRow struct {
	RunId string
	Ts    time.Time
}

// AppLifecyclePhaseE distinguishes a tile-open ("started") from a
// tile-close ("stopped"). The chstore writer encodes this as a low-card
// symbol attribute under MembLifecyclePhase so simple queries on
// "phase = 'started' AND app = X" require only one column scan.
type AppLifecyclePhaseE uint8

const (
	AppLifecyclePhaseUnspecified AppLifecyclePhaseE = 0
	AppLifecyclePhaseStarted     AppLifecyclePhaseE = 1
	AppLifecyclePhaseStopped     AppLifecyclePhaseE = 2
)

// String returns the canonical wire value used by chstore + InMemoryFactsStore.
func (inst AppLifecyclePhaseE) String() (s string) {
	switch inst {
	case AppLifecyclePhaseStarted:
		s = "started"
	case AppLifecyclePhaseStopped:
		s = "stopped"
	default:
		s = "unspecified"
	}
	return
}

// AppLifecycleRow records one open/close of a dock tile. Maps to a
// boxer.facts row with KindAppLifecycle + AppRefPrefix(appId) +
// RunRef(runId) + LifecyclePhase + optional LifecycleStopReason +
// LifecycleTileKey. RunId is required and ties the row back to the
// runtime-start row of the same process. TileKey lets two concurrent
// tiles for the same AppId be distinguished in the audit trail.
//
// StopReason is conventional, not enumerated — current values:
// "user-close" (user clicked × Close), "mount-error" (Mount returned
// error and the tile was reaped), "shutdown" (process exit reaped a
// still-open tile). Empty for "started" rows.
type AppLifecycleRow struct {
	RunId      string
	AppId      app.AppIdT
	TileKey    uint64
	Phase      AppLifecyclePhaseE
	StopReason string
	Ts         time.Time
}

// LaunchRow records one accepted `windowhost.open` request (ADR-0135
// §SD6): which app asked which app to open, with which typed config.
// Maps to a boxer.facts row with KindLaunch + AppRefPrefix(target) +
// RunRef(runId) + LaunchCaller + LifecycleTileKey + LaunchConfigKind +
// the raw config bytes on the blob section. TileKey is the opened
// window's key — the same value the app-lifecycle "started" row written
// in the same Open carries, so the two rows join on one column.
//
// CallerAppId is attributed by the host from the bus envelope
// (Msg.Sender), never from the request payload — the request DTO
// deliberately has no caller field. Refused opens write no row; they
// surface in the caller's reply and the host log.
type LaunchRow struct {
	RunId       string
	CallerAppId app.AppIdT
	TargetAppId app.AppIdT
	TileKey     uint64
	ConfigKind  string // empty for a plain open
	Config      []byte // raw facts-CBOR config bytes; nil for a plain open
	Ts          time.Time
}

// LogFieldKindE discriminates the runtime type of a LogField's value. Drives
// the typed-section fan-out in chstore.WriteLog — fields decoded from
// zerolog's CBOR wire format land in i64 / u64 / f64 / string / bool / blob
// / time sections respectively. Unknown kinds round-trip through Str.
type LogFieldKindE uint8

const (
	LogFieldKindUnknown LogFieldKindE = 0
	LogFieldKindString  LogFieldKindE = 1
	LogFieldKindInt     LogFieldKindE = 2
	LogFieldKindUint    LogFieldKindE = 3
	LogFieldKindFloat   LogFieldKindE = 4
	LogFieldKindBool    LogFieldKindE = 5
	LogFieldKindBytes   LogFieldKindE = 6
	LogFieldKindTime    LogFieldKindE = 7
)

// LogField carries one zerolog context field. The tagged-union layout
// (Kind + per-type slots) lets the chstore fan-out dispatch with no
// reflect / interface allocation; the active slot is the one whose Kind
// constant the caller set, all others are zero. logbridge produces these
// at CBOR-decode time.
type LogField struct {
	Name  string
	Kind  LogFieldKindE
	Str   string
	Int   int64
	Uint  uint64
	Float float64
	Bool  bool
	Bytes []byte
	Time  time.Time
}

// LogErrorFact is one node of a boxer-formatted error chain. Mirrors
// the wire shape eh.MarshalError emits: msg + optional stack-frame
// triple (source/line/function) + optional structured-data CBOR
// blob plus its diagnostic notation (cbor.Diagnose output) + the
// id/parentId pair that links facts into a tree.
//
// Source/Line/Function may be empty for the leading message-only
// fact in a stack stream — eh emits one fact per error message
// (carrying msg) and one fact per stack frame (carrying source/line/
// function) at each frame position. Data may be nil when the leaf
// error wasn't built via eb.Build().
type LogErrorFact struct {
	Msg      string
	Source   string
	Line     string
	Function string
	Data     []byte
	DataDiag string
	Id       uint64
	ParentId uint64
}

// LogErrorStream is one stream from a boxer error decode. Name is
// either "no-stack" (errors without stack info) or "stack-N" (the
// Nth distinct stack trace seen in the error chain) — eh's
// gatherFactsAndStacks dedupes shared stacks across wrapped errors,
// so a 5-level wrap that all happened in the same goroutine is one
// stream with 5 message facts plus the frame facts.
type LogErrorStream struct {
	Name  string
	Facts []LogErrorFact
}

// LogErrorContext is the typed projection of eh.MarshalError's
// structured output. Populated by the logbridge decoder when the
// event's `error` envelope field decodes as the {streams:[...]}
// shape; nil for events whose error was a plain string (or absent).
//
// Consumers (the logviewer detail pane) walk Streams to render
// per-stack collapsing sections; LogRow.Error still carries a flat
// summary so the table column has something to display.
type LogErrorContext struct {
	Streams []LogErrorStream
}

// Summary walks the structured chain and returns a flat one-line
// representation suitable for table columns and console fallbacks.
// Strategy: prefer the first non-empty Msg encountered; the chain's
// outermost wrap is the most recently emitted (`%w`-prefixed) text
// and reads as the canonical error string. Returns "" when no fact
// carries a message — in practice that means the chain was nil or
// only contained frame-only facts (impossible for valid eh output
// but defended against here).
func (inst *LogErrorContext) Summary() (s string) {
	if inst == nil {
		return
	}
	for _, st := range inst.Streams {
		for _, f := range st.Facts {
			if f.Msg != "" {
				s = f.Msg
				return
			}
		}
	}
	return
}

// LogRow is one zerolog event captured by logbridge. Maps to a boxer.facts
// row with KindLog + AppRefPrefix(appId) + LogLevel / LogMessage / LogCaller
// / LogError / LogStack / LogService memberships on the structured envelope
// plus one MembLogField mixed-membership per Fields entry. AppId is empty
// for runtime-internal log lines that don't belong to any app.
//
// ErrorContext is the structured projection of a boxer-style wrapped
// error chain (.Err(boxerErr) when zerolog.ErrorMarshalFunc is
// eh.MarshalError). Nil for events where the error field was a
// plain string or absent. The flat Error string is always populated
// from the chain's outermost message, regardless of ErrorContext, so
// the table-column readers don't need to know about the structured
// form.
//
// InstanceKey is the window the line came from (ADR-0191 §SD4). The host
// pre-tags each window's logger with an `instance_id` field, so these rows
// already carried the value inside Fields; the column is a promotion of that
// field, which is why lines written before it stay readable through the
// field. Zero for a runtime-internal line, matching an empty AppId.
type LogRow struct {
	AppId        app.AppIdT
	InstanceKey  uint64
	Level        string
	Message      string
	Caller       string
	Error        string
	Stack        string
	Service      string
	Fields       []LogField
	Ts           time.Time
	ErrorContext *LogErrorContext
}

// RunEventRow is one entry of the runtime's own trail, flattened for
// reading: what happened, when, and which window it belonged to
// (ADR-0191 §SD7). It is a *view* over the recorded kinds rather than a kind
// of its own — nothing writes a RunEventRow.
//
// Detail is a rendered one-liner, not structured data: the row's own
// attribute values joined in written order. A consumer wanting a specific
// field reads the kind's own membership; this column exists so a trail can be
// read without knowing twelve encodings.
type RunEventRow struct {
	Ts          time.Time
	Kind        string
	AppId       app.AppIdT
	InstanceKey uint64
	RunId       string
	Detail      string
	// Source names the table the row came from, because the two answer
	// slightly different questions and a reader should be able to tell them
	// apart: "facts" is the append-only trail, "persiststate" is app state.
	Source string
	// FactId is the row's id within its table. Fact ids restart at 1 in every
	// process, so it identifies a row within a run and nothing wider.
	FactId string
}

// Run-event source names, as they appear in RunEventRow.Source.
const (
	RunEventSourceFacts   = "facts"
	RunEventSourcePersist = "persiststate"
)

// RunEventFilter selects the trail of one run. RunId is required: the whole
// point of the view is a run's own history, and an unfiltered read of an
// append-only table has no bound.
//
// Since is the run's start. It exists because three kinds still on disk from
// before ADR-0191 carry no run id at all, and the only thing that can place
// them is their timestamp — so a reader asks for "rows naming this run, plus
// unattributed rows since it started". A caller that wants only the exactly
// attributed rows passes the zero time.
type RunEventFilter struct {
	RunId string
	Since time.Time
	Limit uint32
}

// RunEventReaderI is the optional capability a facts store may offer: read
// back the trail as rows (ADR-0191 §SD7).
//
// It is deliberately NOT part of FactsStoreI. Every writer must implement
// that interface, and a reader that only the ClickHouse-backed store can
// answer would force the in-memory store and every test fake to carry a
// method they cannot serve. Consumers type-assert, the ADR-0155 §SD1
// optional-capability pattern, and treat absence as an empty trail.
type RunEventReaderI interface {
	ListRunEvents(filter RunEventFilter) (rows []RunEventRow, err error)
}

// AppLaunchStat is one app's aggregated launch history: how many times it has
// been opened, how recently, and the exponentially-decayed weight the two fold
// into (ADR-0214 §SD8).
//
// Score is computed store-side rather than here so the decay runs over the
// whole trail without shipping a row per launch. LastTs is carried alongside
// because "most recent" and "most frecent" are different orders and the
// launcher wants both: the menu's recents list is the first, its ranking bonus
// the second.
type AppLaunchStat struct {
	AppId  app.AppIdT
	Opens  uint64
	LastTs time.Time
	Score  float64
}

// AppLaunchHistoryReaderI is the optional capability behind the launcher's
// ranking (ADR-0214 §SD7). Like [RunEventReaderI] it is deliberately NOT part
// of [FactsStoreI]: only the ClickHouse-backed store can answer it, and
// requiring it of every writer would put a method the in-memory store cannot
// serve on every test fake. Consumers type-assert and treat absence as "no
// history", which is the correct reading — a run with no server has no trail
// to rank by, and the launcher falls back to authored-metadata ordering.
//
// This is the read ADR-0158 §SD10 named as ranking's blocker. The write half
// has existed since ADR-0026: every window open writes an app-lifecycle
// `started` row. What was missing is exactly this — an aggregate ACROSS runs,
// which [Store.LifecyclesByRun] deliberately refuses to offer because a
// run-anchored reader is what its own consumer needed.
type AppLaunchHistoryReaderI interface {
	// AppLaunchStats returns one row per app that has ever been opened, in
	// Score-descending order. halfLife sets the decay: a launch that old
	// counts half as much as one just now. A non-positive halfLife is an
	// error rather than a silent default — the caller owns the tuning knob.
	//
	// Apps with no launches are absent rather than present with a zero score,
	// so a caller can tell "never opened" from "opened long ago".
	AppLaunchStats(ctx context.Context, halfLife time.Duration, limit uint32) (stats []AppLaunchStat, err error)
}

// FactsStoreI is the contract implementations satisfy. Write methods
// correspond to the recorded fact kinds. All methods return errors so the
// CH-backed implementation can surface transport failures.
//
// App state is not here, in any of its kinds. Persist state, workingsets
// and column-width overrides live on the generated state store behind
// persist.StoreBackend (ADR-0105 D3a, and its Update of 2026-08-15 extending
// D3a to every state-shaped kind); the rule the two tables split on is
// *trail on `boxer.facts`, state on `boxer.persiststate`*. Workingset and
// column-width rows written here before that move stay readable as trail
// and are no longer read.
type FactsStoreI interface {
	WriteGrant(row GrantRow) (id uint64, err error)
	WriteAudit(row AuditRow) (id uint64, err error)
	WriteLog(row LogRow) (id uint64, err error)
	// WriteLogs persists a batch of log rows. Implementations should land
	// the whole batch in one transport operation (e.g. a single Arrow
	// insert) so a batching producer like logbridge is not silently
	// de-batched into one round-trip per row. ids[i] corresponds to rows[i].
	WriteLogs(rows []LogRow) (ids []uint64, err error)
	WriteRuntimeStart(row RuntimeStartRow) (id uint64, err error)
	WriteRuntimeHeartbeat(row HeartbeatRow) (id uint64, err error)
	WriteAppLifecycle(row AppLifecycleRow) (id uint64, err error)
	WriteLaunch(row LaunchRow) (id uint64, err error)
}

// InMemoryFactsStore is the M2.5 backend. Stores each kind in its own
// slice, monotonically id'd.
type InMemoryFactsStore struct {
	mu         sync.RWMutex
	grants     []GrantRow
	audit      []AuditRow
	logs       []LogRow
	runs       []RuntimeStartRow
	heartbeats []HeartbeatRow
	lifecycles []AppLifecycleRow
	launches   []LaunchRow
	nextId     atomic.Uint64
}

var _ FactsStoreI = (*InMemoryFactsStore)(nil)

// NewInMemoryFactsStore returns an empty store.
func NewInMemoryFactsStore() (inst *InMemoryFactsStore) {
	inst = &InMemoryFactsStore{}
	return
}

func (inst *InMemoryFactsStore) WriteGrant(row GrantRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	inst.mu.Lock()
	inst.grants = append(inst.grants, row)
	inst.mu.Unlock()
	return
}

func (inst *InMemoryFactsStore) WriteAudit(row AuditRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	inst.mu.Lock()
	inst.audit = append(inst.audit, row)
	inst.mu.Unlock()
	return
}

// WriteLog appends one captured zerolog event. Fields and Bytes payloads
// are defensively copied so the caller (typically logbridge's decode loop
// reusing scratch buffers) can recycle its inputs.
func (inst *InMemoryFactsStore) WriteLog(row LogRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	if len(row.Fields) > 0 {
		copied := make([]LogField, len(row.Fields))
		for i, f := range row.Fields {
			cf := f
			if f.Kind == LogFieldKindBytes && f.Bytes != nil {
				cf.Bytes = make([]byte, len(f.Bytes))
				copy(cf.Bytes, f.Bytes)
			}
			copied[i] = cf
		}
		row.Fields = copied
	}
	inst.mu.Lock()
	inst.logs = append(inst.logs, row)
	inst.mu.Unlock()
	return
}

// WriteLogs appends a batch of captured zerolog events. Each row is
// defensively copied via WriteLog. WriteLog never errors in the in-memory
// store, so the loop runs to completion; ids[i] corresponds to rows[i].
func (inst *InMemoryFactsStore) WriteLogs(rows []LogRow) (ids []uint64, err error) {
	if len(rows) == 0 {
		return
	}
	ids = make([]uint64, len(rows))
	for i := range rows {
		ids[i], err = inst.WriteLog(rows[i])
		if err != nil {
			return
		}
	}
	return
}

// WriteRuntimeStart appends one process-boot record.
func (inst *InMemoryFactsStore) WriteRuntimeStart(row RuntimeStartRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	if row.Ts.IsZero() {
		row.Ts = time.Now().UTC()
	}
	inst.mu.Lock()
	inst.runs = append(inst.runs, row)
	inst.mu.Unlock()
	return
}

// WriteRuntimeHeartbeat appends one liveness-tick record. Empty RunId
// is rejected so the audit trail can rely on every heartbeat being
// joinable back to a runtime-start row.
func (inst *InMemoryFactsStore) WriteRuntimeHeartbeat(row HeartbeatRow) (id uint64, err error) {
	if row.RunId == "" {
		err = errEmptyRunId
		return
	}
	id = inst.nextId.Add(1)
	if row.Ts.IsZero() {
		row.Ts = time.Now().UTC()
	}
	inst.mu.Lock()
	inst.heartbeats = append(inst.heartbeats, row)
	inst.mu.Unlock()
	return
}

// WriteAppLifecycle appends one app-tile open/close record.
func (inst *InMemoryFactsStore) WriteAppLifecycle(row AppLifecycleRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	if row.Ts.IsZero() {
		row.Ts = time.Now().UTC()
	}
	inst.mu.Lock()
	inst.lifecycles = append(inst.lifecycles, row)
	inst.mu.Unlock()
	return
}

// WriteLaunch appends one accepted app-launch record. Config bytes are
// defensively copied so the caller can recycle its buffer.
func (inst *InMemoryFactsStore) WriteLaunch(row LaunchRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	if row.Ts.IsZero() {
		row.Ts = time.Now().UTC()
	}
	if row.Config != nil {
		cp := make([]byte, len(row.Config))
		copy(cp, row.Config)
		row.Config = cp
	}
	inst.mu.Lock()
	inst.launches = append(inst.launches, row)
	inst.mu.Unlock()
	return
}

// Grants returns a snapshot of all written grants, ordered by insertion.
func (inst *InMemoryFactsStore) Grants() (rows []GrantRow) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]GrantRow, len(inst.grants))
	copy(rows, inst.grants)
	return
}

// AuditRows returns a snapshot of all audit rows ordered by insertion.
func (inst *InMemoryFactsStore) AuditRows() (rows []AuditRow) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]AuditRow, len(inst.audit))
	copy(rows, inst.audit)
	return
}

// Logs returns a snapshot of all captured log rows in insertion order.
func (inst *InMemoryFactsStore) Logs() (rows []LogRow) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]LogRow, len(inst.logs))
	copy(rows, inst.logs)
	return
}

// Runs returns a snapshot of recorded runtime-start rows.
func (inst *InMemoryFactsStore) Runs() (rows []RuntimeStartRow) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]RuntimeStartRow, len(inst.runs))
	copy(rows, inst.runs)
	return
}

// Heartbeats returns a snapshot of recorded heartbeat rows.
func (inst *InMemoryFactsStore) Heartbeats() (rows []HeartbeatRow) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]HeartbeatRow, len(inst.heartbeats))
	copy(rows, inst.heartbeats)
	return
}

// Lifecycles returns a snapshot of recorded app-lifecycle rows.
func (inst *InMemoryFactsStore) Lifecycles() (rows []AppLifecycleRow) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]AppLifecycleRow, len(inst.lifecycles))
	copy(rows, inst.lifecycles)
	return
}

// Launches returns a snapshot of recorded app-launch rows in insertion
// order.
func (inst *InMemoryFactsStore) Launches() (rows []LaunchRow) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]LaunchRow, len(inst.launches))
	copy(rows, inst.launches)
	return
}
