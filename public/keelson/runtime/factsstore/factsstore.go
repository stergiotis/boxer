//go:build llm_generated_opus47

// Package factsstore is the durable view of runtime facts per ADR-0026 §SD6.
// Capability grants from the broker, audit records from the bus, and state
// writes from the persist service all flow through FactsStoreI; M2.5 ships
// an in-memory implementation that proves the data shape end-to-end. A
// runtime.facts-backed implementation (writing CH+leeway rows via the
// factsschema package) lands in a later sub-phase once a live ClickHouse
// driver is wired.
//
// Row types are typed per-kind so the broker / persist / audit code stays
// readable; the leeway translation lives behind the FactsStoreI boundary.
package factsstore

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// errEmptyRunId is the sentinel returned by writers that require a
// non-empty RunId. Exported via the wrapper functions (heartbeat
// today; future kinds that also require a run anchor can reuse).
var errEmptyRunId = errors.New("factsstore: RunId is required")

// GrantRow is one approved capability grant. Maps to a runtime.facts row
// with the KindGrant + AppRefPrefix(appId) + GrantSubjectPattern /
// GrantDirection / GrantReason / GrantSticky / GrantVia memberships under
// ADR-0026 §SD6.
type GrantRow struct {
	AppId      app.AppIdT
	Pattern    string
	Direction  app.CapDirectionE
	Reason     string
	Sticky     bool
	GrantedVia string
	Ts         time.Time
	ExpiresAt  time.Time // zero == no TTL
}

// AuditRow is one audited bus request. Maps to a runtime.facts row with
// KindAudit + AppRefPrefix(appId) + AuditRequestSubject / AuditResult /
// AuditLatencyMs / AuditRequestSize / AuditResponseSize.
type AuditRow struct {
	AppId         app.AppIdT
	Subject       string
	Result        string // "ok" | "denied" | "timeout" | "error"
	LatencyMs     uint32
	RequestSizeB  uint32
	ResponseSizeB uint32
	Ts            time.Time
}

// StateRow is one persisted state write. Maps to a runtime.facts row with
// KindState + AppRefPrefix(appId) + PersistKey memberships; the value lives
// on the blob section.
type StateRow struct {
	AppId app.AppIdT
	Key   string
	Value []byte
	Ts    time.Time
}

// RuntimeStartRow records one process boot: a "this run started" event
// captured at runtime entry by the carousel. Maps to a runtime.facts row
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
// runtime.facts row with KindRuntimeHeartbeat + MembRuntimeRun
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
// runtime.facts row with KindAppLifecycle + AppRefPrefix(appId) +
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

// LogRow is one zerolog event captured by logbridge. Maps to a runtime.facts
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
type LogRow struct {
	AppId        app.AppIdT
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

// FactsStoreI is the contract implementations satisfy. Write methods
// correspond to the recorded fact kinds; LatestState / DeleteState
// support the persist service when run with a facts-backed
// StorageBackendI. All methods return errors so the CH-backed
// implementation can surface transport failures.
type FactsStoreI interface {
	WriteGrant(row GrantRow) (id uint64, err error)
	WriteAudit(row AuditRow) (id uint64, err error)
	WriteState(row StateRow) (id uint64, err error)
	WriteLog(row LogRow) (id uint64, err error)
	// WriteLogs persists a batch of log rows. Implementations should land
	// the whole batch in one transport operation (e.g. a single Arrow
	// insert) so a batching producer like logbridge is not silently
	// de-batched into one round-trip per row. ids[i] corresponds to rows[i].
	WriteLogs(rows []LogRow) (ids []uint64, err error)
	WriteRuntimeStart(row RuntimeStartRow) (id uint64, err error)
	WriteRuntimeHeartbeat(row HeartbeatRow) (id uint64, err error)
	WriteAppLifecycle(row AppLifecycleRow) (id uint64, err error)
	LatestState(appId app.AppIdT, key string) (value []byte, found bool, err error)
	DeleteState(appId app.AppIdT, key string) (err error)
}

// InMemoryFactsStore is the M2.5 backend. Stores grants / audit / state in
// six slices, monotonically id'd. LatestState scans state in reverse so
// the most recent write wins; DeleteState appends a tombstone (empty
// Value) so the read path naturally returns not-found until a subsequent
// Write.
type InMemoryFactsStore struct {
	mu         sync.RWMutex
	grants     []GrantRow
	audit      []AuditRow
	state      []stateEntry
	logs       []LogRow
	runs       []RuntimeStartRow
	heartbeats []HeartbeatRow
	lifecycles []AppLifecycleRow
	nextId     atomic.Uint64
}

type stateEntry struct {
	row       StateRow
	tombstone bool
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

func (inst *InMemoryFactsStore) WriteState(row StateRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	stored := make([]byte, len(row.Value))
	copy(stored, row.Value)
	row.Value = stored
	inst.mu.Lock()
	inst.state = append(inst.state, stateEntry{row: row})
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

func (inst *InMemoryFactsStore) LatestState(appId app.AppIdT, key string) (value []byte, found bool, err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	for i := len(inst.state) - 1; i >= 0; i-- {
		e := inst.state[i]
		if e.row.AppId != appId || e.row.Key != key {
			continue
		}
		if e.tombstone {
			return
		}
		value = make([]byte, len(e.row.Value))
		copy(value, e.row.Value)
		found = true
		return
	}
	return
}

func (inst *InMemoryFactsStore) DeleteState(appId app.AppIdT, key string) (err error) {
	inst.mu.Lock()
	inst.state = append(inst.state, stateEntry{
		row:       StateRow{AppId: appId, Key: key, Ts: time.Now()},
		tombstone: true,
	})
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

// StateRows returns a snapshot of all state-write rows (including
// tombstones-as-empty-values) ordered by insertion.
func (inst *InMemoryFactsStore) StateRows() (rows []StateRow) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]StateRow, 0, len(inst.state))
	for _, e := range inst.state {
		if e.tombstone {
			continue
		}
		rows = append(rows, e.row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Ts.Before(rows[j].Ts)
	})
	return
}
