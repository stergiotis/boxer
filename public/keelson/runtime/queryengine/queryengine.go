// Package queryengine is the engine-adapter contract of
// [ADR-0144]: the roles an execution engine plays in a query system
// assembled from the toolbelt, and the types those roles exchange.
//
// # Three roles, not one interface
//
// Engines differ far less in *capability* than a per-engine plugin would
// suggest, and far more in *lifecycle* than capability data can express. A
// broker-spawned one-shot worker has no process list and nothing to kill; a
// server has both; a cluster streaming results onto a topic is not a server
// with pieces missing — it submits, then subscribes, and the result arrives
// from a member nobody has chosen yet. So the contract is split by role:
//
//   - [DeliveryI] — every engine implements it. An engine is a source of
//     result frames ([runstream]), and a synchronous response is the
//     degenerate case of that stream rather than an exception to it.
//   - [ObservationI] — optional. Inflight progress of a run, observable by
//     somebody other than the connection holder (R8).
//   - [ControlI] — optional. Cancellation addressed by run id (R11).
//
// A consumer discovers what an engine can do by asking for the interface —
// an ordinary type assertion — rather than by consulting a table of engine
// names:
//
//	if ctl, ok := eng.(queryengine.ControlI); ok {
//		err = ctl.Kill(ctx, runID)
//	}
//
// An engine that cannot observe or cancel says so by not implementing the
// method set, which is a fact about the type rather than a runtime answer of
// "unsupported" every caller has to handle.
//
// # What this package does not decide
//
// No placement maps, no cluster rosters, no balancing, no taxonomy of
// engine classes. Those are site policy and belong to whoever deploys the
// system; boxer supplies the seams and publishes its own data through the
// introspection registry (E5). The one member-facing thing here is
// [SelectMember], which is the determinism R4 requires and not a balancer.
//
// [ADR-0144]: https://github.com/stergiotis/boxer/blob/main/doc/adr/0144-query-engine-adapters.md
package queryengine

import (
	"context"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/runid"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Request is one run submitted to an engine.
//
// It carries what an engine needs to execute a statement and nothing about
// where that statement should run — placement was decided before this
// (the E2 dispatch seam), and an engine adapter is already bound to its
// engine.
type Request struct {
	// RunID is the client-minted identity of the run (R7), from
	// [runid.Mint]. It is what live progress, query_log facts, pins and a
	// cancellation all name, so an engine that can register it should be
	// given one. Optional, because an engine may have nowhere to put it —
	// a one-shot worker's system tables die with the process — but it must
	// satisfy [runid.Valid] when set.
	RunID string
	// SQL is the finalized outgoing statement: what the engine will
	// actually receive, after every client-side rewrite. Adapters do not
	// rewrite it, with the single exception of the prelude an engine needs
	// to bind Inputs.
	SQL string
	// Format is the ClickHouse FORMAT the delivered bytes are in
	// ("ArrowStream", "JSONEachRow", …). Empty takes the engine's default,
	// and then [Result.ContentType] is the only thing that says what came
	// back.
	Format string
	// Params binds `{name:Type}` placeholders by BARE name — "limit", not
	// "param_limit". Adapters apply their engine's prefixing. Typed
	// substitution stays the engine's job, as on the HTTP interface.
	Params map[string]string
	// Settings are per-run engine settings, passed through verbatim. They
	// are the escape hatch for everything this struct deliberately does not
	// model (`log_comment`, `replace_running_query`, a session setting),
	// and an adapter REJECTS a key it owns itself rather than letting a run
	// carry two identities or two formats.
	Settings map[string]string
	// Inputs binds in-memory datasets as temporary tables, keyed by table
	// name; each value is Arrow IPC in the `Arrow` file format (with
	// footer). An engine that cannot accept them refuses the request rather
	// than dropping them, because the alternative is a query failing
	// against the engine with "unknown table" and the reason being
	// invisible.
	Inputs map[string][]byte
	// Cap is the row limit the request declares on ITSELF, and is what
	// makes a truncated result recognisable as truncated (R9). Zero
	// declares none. A cap the engine applies by its own default, or that
	// a quota imposes, is not visible from here and is not claimed.
	Cap RowCap
	// Sensitivity is what the run TOUCHES, and is the R6 sensitivity axis
	// of the E9 dispatch label. A run naming sealed data is
	// [SensitivityConfined] and may only be executed where its plaintext is
	// allowed to go; everything else is ordinary. An engine that may not
	// serve confined runs refuses one rather than executing it.
	//
	// The label is derived by whoever knows what the statement names — a
	// registry lookup, not a guess over SQL text — and rides here so the
	// engine can refuse independently of whatever placed the run.
	Sensitivity SensitivityE
	// Cacheable is the caller asserting that the statement is
	// deterministic, so an engine holding a result cache may answer from it
	// instead of executing. The assertion is the caller's to get right —
	// now(), rand(), a network function and a mutable dictionary each make
	// it false — and it is advisory in the same way [OnProgress] is: an
	// engine without a cache ignores it, and [Result.CacheHit] reports what
	// actually happened rather than what was asked for.
	Cacheable bool
	// OnProgress, when set, receives inflight observations for engines that
	// can deliver them on the request's own channel. Advisory in the
	// strongest sense: it may never fire, it is not a substitute for the
	// terminal frame, and its absence says nothing about the run (R8).
	// Called from whatever goroutine the engine reads on — keep it cheap
	// and safe to call concurrently with the caller's own draining.
	//
	// It is a callback and not a progress frame for a reason worth knowing:
	// on the ClickHouse HTTP interface the ticks arrive inside the
	// still-open response-header block, which is to say BEFORE the stream
	// they would have to be frames of exists. Making them frames would mean
	// replaying them after the headers completed, by which time the only
	// thing they had — liveness — is gone. Frames are how a party that is
	// NOT the connection holder sees progress; that is [ObservationI].
	OnProgress func(p runstream.Progress)
}

// Validate reports whether the request is well-formed for any engine.
// Adapters call it and then add whatever their own wire requires.
func (inst Request) Validate() (err error) {
	if inst.SQL == "" {
		err = eh.Errorf("queryengine: request carries no statement")
		return
	}
	if inst.RunID != "" && !runid.Valid(inst.RunID) {
		err = eb.Build().Str("runId", inst.RunID).
			Errorf("queryengine: run id is not safe as a subject token or SQL literal")
		return
	}
	return
}

// SensitivityE says what a run touches, and therefore where it may execute
// — the R6 axis that is independent of execution mode, so "async" never
// implies "less protected".
//
// The zero value is [SensitivityOrdinary], NOT an unknown that denies. That
// is a deliberate asymmetry with the statement-kind classifier, which
// treats unknown as mutating (R5 default-deny), and the two answer
// different questions: whether a statement mutates is undecidable from
// unparseable SQL and must fail closed, whereas whether a run touches
// sealed data is decided by a binding this process performed. A run that
// names nothing sealed is ordinary because nothing sealed was bound into
// it, not because nobody looked.
type SensitivityE uint8

const (
	// SensitivityOrdinary is a run that touches no sealed data. It may
	// execute wherever it was placed.
	SensitivityOrdinary SensitivityE = iota
	// SensitivityConfined is a run whose inputs must not leave this box.
	// Only an engine allowed to see that plaintext may execute it, and the
	// engine refuses rather than trusting whoever routed it there.
	SensitivityConfined
)

var AllSensitivities = []SensitivityE{SensitivityOrdinary, SensitivityConfined}

func (inst SensitivityE) String() (name string) {
	switch inst {
	case SensitivityConfined:
		name = "confined"
	default:
		name = "ordinary"
	}
	return
}

// RowCap is a row limit a request declared on itself.
//
// Only the request's own settings can be read here. A limit a server
// applies by default, or a quota, comes back short with nothing on the wire
// to say so — see [RowCap.TerminalFor] for what is and is not claimed.
type RowCap struct {
	// MaxResultRows is `max_result_rows`; zero means unset.
	MaxResultRows uint64
	// Breaks is true when `result_overflow_mode` is `break`. ClickHouse
	// defaults to `throw`, which raises instead of truncating and is
	// already loud, so only `break` can produce a silently short result.
	Breaks bool
}

// TerminalFor decides how a run that completed its transfer ended: capped
// against the declared limit, or whole.
//
// The comparison is deliberately `>=` and the wording deliberately hedged.
// With `result_overflow_mode = break` the engine stops at a block boundary,
// so a result can exceed the cap slightly; and a result that is complete at
// exactly the cap is indistinguishable from one cut there. Reporting the
// ambiguous case as possibly capped is the direction R9 asks for — a reader
// told "this may be a prefix" can check, whereas a reader silently handed a
// prefix cannot.
func (inst RowCap) TerminalFor(resultRows uint64) (t runstream.Terminal) {
	if inst.MaxResultRows == 0 || !inst.Breaks || resultRows < inst.MaxResultRows {
		t = runstream.Complete()
		return
	}
	t = runstream.Truncated("reached max_result_rows=" + strconv.FormatUint(inst.MaxResultRows, 10) +
		" with result_overflow_mode=break — the result may be a prefix")
	return
}

// Summary is what an engine reports about a finished run's cost. Every
// field is best-effort: an engine fills what it knows and leaves the rest
// zero, and zero means "not reported" rather than "none".
type Summary struct {
	ReadRows        uint64
	ReadBytes       uint64
	WrittenRows     uint64
	WrittenBytes    uint64
	TotalRowsToRead uint64
	ResultRows      uint64
	ResultBytes     uint64
	ElapsedNs       uint64
	MemoryUsage     uint64
}

// Progress narrows a summary to the inflight observation shape, which is
// the subset both progress producers can actually see.
func (inst Summary) Progress() (p runstream.Progress) {
	p = runstream.Progress{
		ReadRows:        inst.ReadRows,
		ReadBytes:       inst.ReadBytes,
		TotalRowsToRead: inst.TotalRowsToRead,
		ElapsedNs:       inst.ElapsedNs,
		MemoryUsage:     inst.MemoryUsage,
	}
	return
}

// Result is what an engine reports about the delivered stream as a whole,
// beside the frames themselves — the things a consumer cannot compute for
// itself because only the engine was there.
//
// It is returned with the stream rather than after it, so a consumer
// relaying the body (setting a content type, say) has the answer before the
// first byte.
type Result struct {
	// ContentType is the media type of the delivered bytes as the engine
	// reported it. Empty when the engine did not say — derive it from
	// [Request.Format] then, and know that is an assumption.
	ContentType string
	// Summary is the run's counters as known when delivery began. On a
	// synchronous engine that is the response header, which may predate the
	// last byte; treat it as a report, not as an audit.
	Summary Summary
	// CacheHit is true when the engine answered from a result cache and no
	// execution happened. Advisory: an engine without a cache always
	// reports false, which is not evidence of a miss.
	CacheHit bool
}

// StreamI is one run's frames, in order, pulled by the consumer.
//
// The stream ends with exactly one terminal frame saying how the run ended.
// A stream that ends WITHOUT one is a producer that died mid-answer, and
// that is the case [runstream.Collector] reports as [runstream.ErrIncomplete]
// — absence is the safe reading, so nothing has to go right for a partial
// result to be recognised as partial (R9).
//
// Sequence numbers start at 1 and strictly increase, so a collector can
// tell a duplicate or a reordered delivery from a fresh frame.
type StreamI interface {
	// Next advances to the next frame. ok is false once the stream is
	// exhausted, which says nothing on its own about how the run ended —
	// consult the terminal frame, or its absence.
	Next() (f runstream.Frame[[]byte], ok bool)
	// Err reports a transport failure that ended the stream early. It is
	// informational: a live adapter that observed a failure also yields a
	// failed terminal frame, and the frame is what the contract makes
	// authoritative.
	Err() (err error)
	// Close releases the stream's resources. It must be called even by a
	// consumer that drained to the end, and is safe to call more than once.
	Close() (err error)
}

// DeliveryI is the role every engine implements: a source of result frames.
//
// The bytes of a data frame belong to the CONSUMER — an adapter neither
// retains nor reuses them — so a frame may be kept, appended to a
// collector, or handed on without copying.
type DeliveryI interface {
	// Deliver submits req and returns its result stream. It returns once
	// the engine has accepted the run and said what it is delivering; the
	// frames themselves arrive as the consumer pulls them.
	//
	// err is non-nil only when the request was REJECTED before execution:
	// it is malformed, or it asks for something this engine does not do.
	// Those are the caller's to fix, and st is nil.
	//
	// Everything else is an outcome of the run and arrives as a terminal
	// frame — including a statement the engine refused, a worker that died,
	// and a transport that never connected. That split is what lets a
	// consumer stop branching on which engine ran the query: outcomes are
	// read in one place, and the one thing a caller must not do is ignore
	// the terminal.
	Deliver(ctx context.Context, req Request) (st StreamI, res Result, err error)
}

// ObservationI is the optional role of an engine whose inflight runs are
// visible to somebody other than the party holding the result path (R8) —
// a second window, ops tooling, a dashboard watching a run it did not
// issue.
//
// Two properties are required of an implementation, and both are refusals:
//
//   - It never synthesises a terminal state. A run disappearing from an
//     engine's view is ambiguous — finished, killed, or failed — and
//     guessing would put a wrong outcome into the one place R9 makes
//     authoritative.
//   - It never deregisters a run on its own, for the same reason. That
//     belongs to whoever holds the result path, at the moment it delivers
//     the terminal.
//
// [queryprogress.Poller] implements this against a server's process list.
//
// [queryprogress.Poller]: https://pkg.go.dev/github.com/stergiotis/boxer/public/keelson/runtime/queryprogress#Poller
type ObservationI interface {
	// Watch registers a run for observation. Where the observations go —
	// a bus subject family, typically — is the implementation's business
	// and its consumers'; this contract covers only the registration.
	Watch(runID string) (err error)
	// Unwatch deregisters a run. The caller decides when: at the terminal.
	Unwatch(runID string)
}

// ControlI is the optional role of an engine a run can be cancelled on
// (R11).
//
// Addressing is by run id, which is the same key observation uses, so a
// party that can watch a run can also stop it without holding its
// connection.
type ControlI interface {
	// Kill asks the engine to stop the run named by runID.
	//
	// It is a request, not a guarantee, and a nil error is NOT evidence
	// that anything was stopped: a run that had already finished, or that
	// never existed, is indistinguishable from one that was killed. As
	// everywhere else in this contract, terminal truth comes from the
	// result path.
	Kill(ctx context.Context, runID string) (err error)
}
