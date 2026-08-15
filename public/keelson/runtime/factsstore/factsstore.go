// Package factsstore is the durable view of runtime facts per ADR-0026 §SD6.
// Capability grants from the broker, audit records from the bus, the run and
// app lifecycle trail, launches, workingsets and column-width overrides all
// flow through FactsStoreI. Two backends implement it: InMemoryFactsStore
// here, and the boxer.facts-backed Store in
// [github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore],
// which writes CH+leeway rows through the factsschema package.
// chstore.NewWithFallback picks between them at runtime, degrading to the
// in-memory store when ClickHouse is unreachable.
//
// App persist state does not flow through here any more: it lives on a
// generated record store over its own table (persist.StoreBackend, ADR-0105
// D3a), and the facts-bound state verbs were removed once that landed.
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

// GrantRow is one approved capability grant. Maps to a boxer.facts row
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

// AuditRow is one audited bus request. Maps to a boxer.facts row with
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

// WorkingsetRow records one saved app workingset (ADR-0148 §SD6): the
// launch config that would reproduce the closing window's user-authored
// state, written at the closing edge exactly as LaunchRow records the
// opening edge. Maps to a boxer.facts row with KindWorkingset +
// AppRefPrefix(appId) + RunRef(runId) + WorkingsetName + LaunchConfigKind
// + LifecycleTileKey + LifecycleStopReason, with the config bytes on the
// blob section under the LaunchConfig membership — the launch cohort's
// vocabulary, reused because the record IS the app's LaunchKind DTO
// (§SD2).
//
// Identity is (AppId, Name): the durable app id plus a caller-chosen
// name (§SD3). v1 wires exactly one name, "default". Kind is the app's
// Manifest.LaunchKind, stored as its own column because the facts wire
// carries no kind marker — readers must not sniff the bytes.
//
// TileKey and Reason are provenance, not identity: which window wrote the
// record and why it closed ("user-close" / "shutdown" / …). Rows are
// append-only; the latest row for (AppId, Name) wins and a tombstone
// (DeleteWorkingset) reads back as not-found — the persist-state semantics.
type WorkingsetRow struct {
	RunId   string
	AppId   app.AppIdT
	Name    string
	Kind    string
	Config  []byte
	TileKey uint64
	Reason  string
	Ts      time.Time
}

// Column-width tier names (ADR-0151 §SD1), most specific first. They are
// stored as their own low-cardinality column rather than being inferred
// from whether Scope is empty, so a reader never has to reconstruct the
// tier from the shape of another field.
const (
	// ColWidthTierInstance scopes an override to one table in one app;
	// Scope carries the call site's stable table tag.
	ColWidthTierInstance = "instance"
	// ColWidthTierShape scopes an override to "the same logical table"
	// wherever it appears; Scope carries the shape hash over the sorted
	// column-key set. Read-only in v1 — nothing writes this tier yet
	// (§SD1's deliberate small cut).
	ColWidthTierShape = "shape"
	// ColWidthTierColumn scopes an override to a column anywhere in the
	// app; Scope is empty. This is the tier that lets a recurring column
	// keep its width across differently-shaped ad-hoc query results.
	ColWidthTierColumn = "column"
)

// ColumnWidthRow records one table column-width override (ADR-0151,
// Update 2026-07-30). One row per entry rather than one document per app:
// the trail is the history, and last-writer-wins lands at entry
// granularity, which is what removes the cross-window race the ADR's
// original document layout could only narrow.
//
// Identity is (AppId, Tier, Scope, ColumnKey). ColumnKey is the
// blake3short of (column name, type discriminator), so a type change
// invalidates the override by construction rather than by a rule someone
// has to remember. Rows are append-only; the latest row for a key wins and
// a tombstone (DeleteColumnWidth) reads back as absent — the persist-state
// semantics, reused a third time after workingsets.
//
// Points and FontSize travel together because a width is only meaningful
// against the font it was captured at: resolution rescales proportionally
// when the current font size differs (§SD1). FontSize of 0 means "captured
// without a font reference" and disables rescaling for that entry.
//
// One backend difference is worth knowing before writing a test: "latest"
// means insertion order in InMemoryFactsStore and (Ts, id) in chstore, as
// it already does for state and workingsets. The two agree for every
// caller that lets Ts default to now, and diverge only for a caller that
// back- or post-dates a write — so a test that stamps a future Ts will see
// a tombstone lose on one backend and win on the other.
type ColumnWidthRow struct {
	AppId     app.AppIdT
	Tier      string
	Scope     string
	ColumnKey string
	Points    float64
	FontSize  float64
	Ts        time.Time
}

// ColumnWidthKey is the identity tuple of an override, extracted so the
// backends and the resolver agree on what "the same entry" means without
// each re-deriving it.
type ColumnWidthKey struct {
	Tier      string
	Scope     string
	ColumnKey string
}

// Key returns the row's identity within its app.
func (inst ColumnWidthRow) Key() (k ColumnWidthKey) {
	k = ColumnWidthKey{Tier: inst.Tier, Scope: inst.Scope, ColumnKey: inst.ColumnKey}
	return
}

// SortColumnWidths orders rows by (Tier, Scope, ColumnKey). Both backends
// call it so they agree on ordering without either trusting ClickHouse's
// collation — the same reason SortWorkingsets exists.
func SortColumnWidths(rows []ColumnWidthRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Tier != rows[j].Tier {
			return rows[i].Tier < rows[j].Tier
		}
		if rows[i].Scope != rows[j].Scope {
			return rows[i].Scope < rows[j].Scope
		}
		return rows[i].ColumnKey < rows[j].ColumnKey
	})
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
// correspond to the recorded fact kinds; the workingset and column-width
// verbs add latest-wins reads and tombstone deletes over their trails. All
// methods return errors so the CH-backed implementation can surface
// transport failures.
//
// App persist state is not here: it lives on the generated store behind
// persist.StoreBackend (ADR-0105 D3a), and the facts-bound state verbs it
// replaced were removed once it had no callers (ADR-0105, 2026-08-15).
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
	// WriteWorkingset appends one saved workingset record (ADR-0148 §SD6).
	// Append-only: a second write for the same (AppId, Name) supersedes the
	// first without erasing it, so the row trail is the history.
	WriteWorkingset(row WorkingsetRow) (id uint64, err error)
	// LatestWorkingset returns the most recent non-tombstoned record for
	// (appId, name). kind is read back as its own column, never sniffed from
	// the bytes — the facts wire has no kind marker (ADR-0135 Update). A
	// missing record is found=false with no error.
	LatestWorkingset(appId app.AppIdT, name string) (cfg []byte, kind string, found bool, err error)
	// ListWorkingsets returns the latest non-tombstoned record for every
	// (AppId, Name) the store holds — the set a restore would find, not the
	// write trail (ADR-0148 §SD7). A key whose newest row is a tombstone is
	// absent, exactly as LatestWorkingset reports it. Ts is the winning
	// row's write time; the trail itself stays a boxer.facts query, since
	// history-as-rows is the ADR's stance rather than a method. Rows come
	// back ordered by AppId then Name — see [SortWorkingsets]. No filter
	// arguments: the result is bounded by (participating apps × names),
	// which v1 caps at one name per app.
	ListWorkingsets() (rows []WorkingsetRow, err error)
	// DeleteWorkingset appends a tombstone for (appId, name); subsequent
	// LatestWorkingset calls read back found=false until the next write.
	DeleteWorkingset(appId app.AppIdT, name string) (err error)
	// WriteColumnWidth appends one table column-width override
	// (ADR-0151, Update 2026-07-30). Append-only: a later write for the
	// same (AppId, Tier, Scope, ColumnKey) supersedes the earlier one
	// without erasing it.
	WriteColumnWidth(row ColumnWidthRow) (id uint64, err error)
	// ListColumnWidths returns the latest non-tombstoned override for
	// every key belonging to appId — the whole override set a resolver
	// loads at once, since resolution walks three tiers per column and a
	// per-key read would be one round-trip per column per frame. Rows come
	// back ordered by [SortColumnWidths]. A cleared override is absent,
	// not present-and-zero.
	ListColumnWidths(appId app.AppIdT) (rows []ColumnWidthRow, err error)
	// DeleteColumnWidth tombstones one override key. Clearing a key that
	// was never written is not an error; the tombstone simply becomes the
	// latest row for a key that had none.
	DeleteColumnWidth(appId app.AppIdT, tier string, scope string, columnKey string) (err error)
}

// InMemoryFactsStore is the M2.5 backend. Stores each kind in its own
// slice, monotonically id'd. The workingset and column-width trails are
// read latest-wins by a reverse scan; a delete appends a tombstone so the
// read path naturally returns not-found until a subsequent write.
type InMemoryFactsStore struct {
	mu         sync.RWMutex
	grants     []GrantRow
	audit      []AuditRow
	logs       []LogRow
	runs       []RuntimeStartRow
	heartbeats []HeartbeatRow
	lifecycles []AppLifecycleRow
	launches   []LaunchRow
	// workingsets is the append-only workingset trail (ADR-0148 §SD6),
	// read latest-wins by a reverse scan.
	workingsets []workingsetEntry
	// colWidths is the append-only column-width override trail
	// (ADR-0151), collapsed latest-wins per key by ListColumnWidths.
	colWidths []colWidthEntry
	nextId    atomic.Uint64
}

type workingsetEntry struct {
	row       WorkingsetRow
	tombstone bool
}

type colWidthEntry struct {
	row       ColumnWidthRow
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

// WriteWorkingset appends one saved workingset record (ADR-0148 §SD6).
// Config bytes are defensively copied so the composing app can recycle
// its buffer.
func (inst *InMemoryFactsStore) WriteWorkingset(row WorkingsetRow) (id uint64, err error) {
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
	inst.workingsets = append(inst.workingsets, workingsetEntry{row: row})
	inst.mu.Unlock()
	return
}

// LatestWorkingset scans the trail in reverse so the most recent write for
// (appId, name) wins; a tombstone reached first reads back as not-found.
// The kind comes off the stored column, never from the bytes.
func (inst *InMemoryFactsStore) LatestWorkingset(appId app.AppIdT, name string) (cfg []byte, kind string, found bool, err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	for i := len(inst.workingsets) - 1; i >= 0; i-- {
		e := inst.workingsets[i]
		if e.row.AppId != appId || e.row.Name != name {
			continue
		}
		if e.tombstone {
			return
		}
		cfg = make([]byte, len(e.row.Config))
		copy(cfg, e.row.Config)
		kind = e.row.Kind
		found = true
		return
	}
	return
}

// ListWorkingsets walks the trail once in reverse, so the first entry seen
// for a key is its newest, and reports the winners (ADR-0148 §SD7). A key
// whose newest entry is a tombstone is skipped but still consumed, which is
// what keeps a deleted record from being resurrected by the write that
// preceded its tombstone.
//
// With ClickHouse down this is the store the runtime uses, so the answer is
// then this process's own saves only — ADR-0148's documented degradation.
func (inst *InMemoryFactsStore) ListWorkingsets() (rows []WorkingsetRow, err error) {
	type wsKey struct {
		appId app.AppIdT
		name  string
	}
	inst.mu.RLock()
	seen := make(map[wsKey]struct{}, len(inst.workingsets))
	rows = make([]WorkingsetRow, 0, len(inst.workingsets))
	for i := len(inst.workingsets) - 1; i >= 0; i-- {
		e := inst.workingsets[i]
		k := wsKey{appId: e.row.AppId, name: e.row.Name}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		if e.tombstone {
			continue
		}
		row := e.row
		if e.row.Config != nil {
			cp := make([]byte, len(e.row.Config))
			copy(cp, e.row.Config)
			row.Config = cp
		}
		rows = append(rows, row)
	}
	inst.mu.RUnlock()
	SortWorkingsets(rows)
	return
}

// SortWorkingsets orders rows by AppId then Name — the ListWorkingsets
// ordering (ADR-0148 §SD7). Both backends call it rather than each trusting
// its own collation, so a caller comparing an in-memory answer with a
// ClickHouse one sees the same sequence.
func SortWorkingsets(rows []WorkingsetRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].AppId != rows[j].AppId {
			return rows[i].AppId < rows[j].AppId
		}
		return rows[i].Name < rows[j].Name
	})
}

// DeleteWorkingset appends a tombstone for (appId, name), so history
// stays in the trail.
func (inst *InMemoryFactsStore) DeleteWorkingset(appId app.AppIdT, name string) (err error) {
	inst.mu.Lock()
	inst.workingsets = append(inst.workingsets, workingsetEntry{
		row:       WorkingsetRow{AppId: appId, Name: name, Ts: time.Now().UTC()},
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

// Launches returns a snapshot of recorded app-launch rows in insertion
// order.
func (inst *InMemoryFactsStore) Launches() (rows []LaunchRow) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]LaunchRow, len(inst.launches))
	copy(rows, inst.launches)
	return
}

// Workingsets returns a snapshot of recorded workingset rows in insertion
// order, tombstones excluded (they carry no config to inspect).
func (inst *InMemoryFactsStore) Workingsets() (rows []WorkingsetRow) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]WorkingsetRow, 0, len(inst.workingsets))
	for _, e := range inst.workingsets {
		if e.tombstone {
			continue
		}
		rows = append(rows, e.row)
	}
	return
}

// WriteColumnWidth appends one override to the trail (ADR-0151).
func (inst *InMemoryFactsStore) WriteColumnWidth(row ColumnWidthRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	if row.Ts.IsZero() {
		row.Ts = time.Now().UTC()
	}
	inst.mu.Lock()
	inst.colWidths = append(inst.colWidths, colWidthEntry{row: row})
	inst.mu.Unlock()
	return
}

// ListColumnWidths collapses the trail to the latest entry per key for
// appId. The scan runs in reverse and keeps the first sighting of each
// key, so a tombstone reached first suppresses the key entirely rather
// than letting an older surviving write show through — the same ordering
// trap the CH backend has to spell out as HAVING argMax(is_tomb) = 0.
func (inst *InMemoryFactsStore) ListColumnWidths(appId app.AppIdT) (rows []ColumnWidthRow, err error) {
	rows = []ColumnWidthRow{}
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	seen := make(map[ColumnWidthKey]struct{}, len(inst.colWidths))
	for i := len(inst.colWidths) - 1; i >= 0; i-- {
		e := inst.colWidths[i]
		if e.row.AppId != appId {
			continue
		}
		k := e.row.Key()
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		if e.tombstone {
			continue
		}
		rows = append(rows, e.row)
	}
	SortColumnWidths(rows)
	return
}

// DeleteColumnWidth appends a tombstone for one override key.
func (inst *InMemoryFactsStore) DeleteColumnWidth(appId app.AppIdT, tier string, scope string, columnKey string) (err error) {
	inst.mu.Lock()
	inst.colWidths = append(inst.colWidths, colWidthEntry{
		row: ColumnWidthRow{
			AppId: appId, Tier: tier, Scope: scope, ColumnKey: columnKey,
			Ts: time.Now().UTC(),
		},
		tombstone: true,
	})
	inst.mu.Unlock()
	return
}
