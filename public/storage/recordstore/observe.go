package recordstore

import (
	"context"
	"encoding/hex"
	"errors"
	"iter"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CallOpE is which ExecutorI verb a call used.
type CallOpE uint8

const (
	CallOpUnspecified CallOpE = 0
	CallOpExec        CallOpE = 1
	CallOpQuery       CallOpE = 2
	CallOpInsert      CallOpE = 3
)

var AllCallOps = []CallOpE{CallOpUnspecified, CallOpExec, CallOpQuery, CallOpInsert}

func (inst CallOpE) String() string {
	switch inst {
	case CallOpExec:
		return "exec"
	case CallOpQuery:
		return "query"
	case CallOpInsert:
		return "insert"
	default:
		return "unspecified"
	}
}

// CallPhaseE is when an event was emitted relative to its call (ADR-0295
// §SD5). Every observer receives the done event; only a [Required] observer
// also receives the intent event, before the statement is sent.
type CallPhaseE uint8

const (
	CallPhaseUnspecified CallPhaseE = 0
	CallPhaseIntent      CallPhaseE = 1
	CallPhaseDone        CallPhaseE = 2
)

var AllCallPhases = []CallPhaseE{CallPhaseUnspecified, CallPhaseIntent, CallPhaseDone}

func (inst CallPhaseE) String() string {
	switch inst {
	case CallPhaseIntent:
		return "intent"
	case CallPhaseDone:
		return "done"
	default:
		return "unspecified"
	}
}

// CallOutcomeE is how a call ended.
type CallOutcomeE uint8

const (
	CallOutcomeUnspecified CallOutcomeE = 0
	CallOutcomeOk          CallOutcomeE = 1
	CallOutcomeError       CallOutcomeE = 2
	// CallOutcomeCanceled is a call ended by its context.
	CallOutcomeCanceled CallOutcomeE = 3
	// CallOutcomeStopped is a query whose consumer broke off the stream; the
	// event's row and batch counts are what it had received.
	CallOutcomeStopped CallOutcomeE = 4
	// CallOutcomeRefused is a call a required observer refused at intent: the
	// statement never reached the server.
	CallOutcomeRefused CallOutcomeE = 5
)

var AllCallOutcomes = []CallOutcomeE{CallOutcomeUnspecified, CallOutcomeOk, CallOutcomeError, CallOutcomeCanceled, CallOutcomeStopped, CallOutcomeRefused}

func (inst CallOutcomeE) String() string {
	switch inst {
	case CallOutcomeOk:
		return "ok"
	case CallOutcomeError:
		return "error"
	case CallOutcomeCanceled:
		return "canceled"
	case CallOutcomeStopped:
		return "stopped"
	case CallOutcomeRefused:
		return "refused"
	default:
		return "unspecified"
	}
}

// SQLDigest is the 128-bit BLAKE3 digest of a statement's text — what an event
// carries instead of the text (ADR-0295 §SD4). Unkeyed, it can be confirmed by
// guessing a low-entropy statement; [ObserveOptions.DigestKey] keys it.
type SQLDigest [16]byte

func (inst SQLDigest) String() string { return hex.EncodeToString(inst[:]) }

// CallEvent is one executor call as an observer is told it (ADR-0295 §SD4).
// It carries no SQL text and no values: a statement may name a data subject.
type CallEvent struct {
	Phase CallPhaseE
	Op    CallOpE
	// Class classifies an exec; it is unspecified for query and insert.
	Class StatementClassE
	// Table is exact for an insert and best effort otherwise ("" when the
	// statement's head names none the lexer recognises).
	Table string
	// Rows and Batches are what an insert sent, or what a query's consumer
	// received; zero for an exec, and in an intent event.
	Rows    int64
	Batches int64
	BatchId BatchIdT
	Digest  SQLDigest
	Start   time.Time
	// Duration runs from Start to the end of the call — for a query, the end
	// of its stream, so it includes the consumer's time between batches.
	Duration time.Duration
	Outcome  CallOutcomeE
	// Err is the error the caller received, for in-process use. Its text may
	// quote the statement (ClickHouse exceptions do); a durable observer
	// persists Outcome and ErrCode, not Err.
	Err error
	// ErrCode is the ClickHouse error code Err carries, 0 when none.
	ErrCode  int32
	Identity callident.CallIdentity
}

// CallObserverI receives call events. ObserveCall may be invoked from several
// goroutines at once — the decorated executor may be shared — and on the
// calling goroutine, so a slow observer slows the call; wrap one in
// [NewAsyncObserver] to decouple it.
type CallObserverI interface {
	ObserveCall(ev CallEvent) error
}

// ObserverFunc adapts a function as a CallObserverI.
type ObserverFunc func(ev CallEvent) error

var _ CallObserverI = ObserverFunc(nil)

func (inst ObserverFunc) ObserveCall(ev CallEvent) error { return inst(ev) }

// MultiObserver fans one event out to every observer in order and joins their
// errors. A nil entry is skipped.
type MultiObserver []CallObserverI

var _ CallObserverI = MultiObserver(nil)

func (inst MultiObserver) ObserveCall(ev CallEvent) (err error) {
	var errs []error
	for _, o := range inst {
		if o == nil {
			continue
		}
		if oerr := o.ObserveCall(ev); oerr != nil {
			errs = append(errs, oerr)
		}
	}
	err = errors.Join(errs...)
	return
}

// ErrObserverRefused wraps a required observer's refusal of a call's intent:
// the statement was not sent. Match with errors.Is.
var ErrObserverRefused = errors.New("a required call observer refused the call")

type requiredObserver struct{ inner CallObserverI }

func (inst requiredObserver) ObserveCall(ev CallEvent) error { return inst.inner.ObserveCall(ev) }

// Required marks o as required (ADR-0295 §SD5): it receives an intent event
// before each statement is sent, and an error from it refuses the call. Its
// failure on the done event is counted like any observer's, never returned —
// the effect has happened by then.
//
// A required observer must be passed to [ObserveExecutor] directly — only
// there can it gate — and must record synchronously. ObserveExecutor panics
// on a Required nested in a [MultiObserver] or an [AsyncObserver], where it
// would gate nothing, and on a Required over an AsyncObserver (at any depth
// of MultiObserver), which would acknowledge an intent it has not recorded.
func Required(o CallObserverI) CallObserverI { return requiredObserver{inner: o} }

// ObserveOptions parameterizes [ObserveExecutorWith].
type ObserveOptions struct {
	// DigestKey keys the SQL digest (32 bytes) so that it cannot be confirmed
	// by guessing a statement. Nil leaves the digest unkeyed. A consumer that
	// persists digests should set a deployment key; digests under different
	// keys do not compare.
	DigestKey []byte
}

// ObservingExecutor is the decorator [ObserveExecutor] returns. It is safe for
// concurrent use when its inner executor and its observers are.
type ObservingExecutor struct {
	inner          ExecutorI
	observers      []CallObserverI
	required       []CallObserverI
	digestKey      []byte
	observerErrors atomic.Uint64
}

var _ ExecutorI = (*ObservingExecutor)(nil)

// ObserveExecutor decorates inner so that every call emits one done event to
// every observer (ADR-0295 §SD4), with unkeyed digests. See
// [ObserveExecutorWith].
func ObserveExecutor(inner ExecutorI, observers ...CallObserverI) *ObservingExecutor {
	return ObserveExecutorWith(inner, ObserveOptions{}, observers...)
}

// ObserveExecutorWith decorates inner. Observers are told each call in the
// order given; a [Required] one also gates it. A nil observer is skipped. The
// batch id on the call's context is used and passed on; a call without one
// gets one minted and attached, so the inner executor stamps it.
//
// A query's event fires once, when its stream ends — exhausted, failed, or
// broken off by the consumer. A sequence that is never iterated issues no
// statement and emits nothing. Batch ownership is ExecutorI's, unchanged.
func ObserveExecutorWith(inner ExecutorI, opts ObserveOptions, observers ...CallObserverI) (inst *ObservingExecutor) {
	if len(opts.DigestKey) != 0 && len(opts.DigestKey) != 32 {
		panic("recordstore.ObserveExecutorWith: DigestKey must be 32 bytes or empty")
	}
	inst = &ObservingExecutor{inner: inner, digestKey: opts.DigestKey,
		observers: make([]CallObserverI, 0, len(observers))}
	for _, o := range observers {
		if o == nil {
			continue
		}
		if r, ok := o.(requiredObserver); ok {
			if containsAsync(r.inner) {
				panic("recordstore.ObserveExecutorWith: an AsyncObserver cannot be required — it acknowledges an intent it has not recorded")
			}
			if containsRequired(r.inner) {
				panic("recordstore.ObserveExecutorWith: Required nested inside Required")
			}
			inst.required = append(inst.required, r.inner)
		} else if containsRequired(o) {
			panic("recordstore.ObserveExecutorWith: a Required observer nested in a MultiObserver or an AsyncObserver gates nothing — pass it to ObserveExecutor directly")
		}
		inst.observers = append(inst.observers, o)
	}
	return
}

// containsAsync reports whether o is, or a MultiObserver within it holds, an
// AsyncObserver.
func containsAsync(o CallObserverI) bool {
	switch v := o.(type) {
	case *AsyncObserver:
		return true
	case MultiObserver:
		for _, e := range v {
			if containsAsync(e) {
				return true
			}
		}
	}
	return false
}

// containsRequired reports whether o is, or holds at any depth of
// MultiObserver and AsyncObserver, a Required observer.
func containsRequired(o CallObserverI) bool {
	switch v := o.(type) {
	case requiredObserver:
		return true
	case MultiObserver:
		for _, e := range v {
			if containsRequired(e) {
				return true
			}
		}
	case *AsyncObserver:
		return v.inner != nil && containsRequired(v.inner)
	}
	return false
}

// ObserverErrors counts the errors observers returned on done events, and
// required observers' as well — none of them fails a call.
func (inst *ObservingExecutor) ObserverErrors() uint64 { return inst.observerErrors.Load() }

func (inst *ObservingExecutor) Exec(ctx context.Context, sql string) (err error) {
	ev, ctx := inst.begin(ctx, CallOpExec, sql)
	ev.Class, ev.Table = ClassifyStatement(sql)
	if err = inst.intent(&ev); err != nil {
		return
	}
	err = inst.inner.Exec(ctx, sql)
	inst.done(ctx, &ev, err)
	return
}

func (inst *ObservingExecutor) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		ev, ctx := inst.begin(ctx, CallOpQuery, sql)
		ev.Table = QueryTable(sql)
		if err := inst.intent(&ev); err != nil {
			yield(nil, err)
			return
		}
		var qerr error
		stopped, ended := false, false
		defer func() {
			switch {
			case qerr != nil:
				inst.done(ctx, &ev, qerr)
			case stopped:
				ev.Outcome = CallOutcomeStopped
				inst.deliver(&ev)
			case ended:
				inst.done(ctx, &ev, nil)
			default:
				// Neither ended nor stopped: the consumer's loop body panicked
				// through the stream. The event still fires, as a failure.
				inst.done(ctx, &ev, errStreamPanicked)
			}
		}()
		for rec, err := range inst.inner.QueryArrow(ctx, sql) {
			if err != nil {
				qerr = err
				yield(nil, err)
				return
			}
			ev.Rows += rec.NumRows()
			ev.Batches++
			if !yield(rec, nil) {
				stopped = true
				return
			}
		}
		ended = true
	}
}

var errStreamPanicked = errors.New("the query stream ended by a panic in its consumer")

// InsertArrow observes a non-empty insert. An empty one is passed through
// without an event: ExecutorI makes it a no-op that sends no request, so there
// is no call to record — as with a query sequence that is never iterated.
func (inst *ObservingExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	if len(records) == 0 {
		return inst.inner.InsertArrow(ctx, table, records)
	}
	ev, ctx := inst.begin(ctx, CallOpInsert, "INSERT INTO "+table)
	ev.Table = table
	if err = inst.intent(&ev); err != nil {
		return
	}
	ev.Batches = int64(len(records))
	for _, rec := range records {
		ev.Rows += rec.NumRows()
	}
	err = inst.inner.InsertArrow(ctx, table, records)
	inst.done(ctx, &ev, err)
	return
}

// begin opens the event of one call and returns the context the inner call
// runs under, which carries the event's batch id.
func (inst *ObservingExecutor) begin(ctx context.Context, op CallOpE, sql string) (ev CallEvent, out context.Context) {
	id, ok := BatchIdFrom(ctx)
	if !ok {
		id = NewBatchId()
		ctx = WithBatchId(ctx, id)
	}
	ci, _ := callident.CallIdentityFrom(ctx)
	ev = CallEvent{Op: op, BatchId: id, Digest: inst.digest(sql), Start: time.Now(), Identity: ci}
	out = ctx
	return
}

func (inst *ObservingExecutor) digest(sql string) (d SQLDigest) {
	h := blake3.New(len(d), inst.digestKey)
	_, _ = h.Write([]byte(sql))
	h.Sum(d[:0])
	return
}

// intent offers the call to the required observers. A refusal is delivered to
// every observer as a done event with outcome refused, and returned wrapped in
// ErrObserverRefused.
func (inst *ObservingExecutor) intent(ev *CallEvent) (err error) {
	if len(inst.required) == 0 {
		return
	}
	ev.Phase = CallPhaseIntent
	for _, o := range inst.required {
		if oerr := o.ObserveCall(*ev); oerr != nil {
			err = eh.Errorf("%w: %w", ErrObserverRefused, oerr)
			break
		}
	}
	if err != nil {
		ev.Outcome = CallOutcomeRefused
		ev.Err = err
		ev.Duration = time.Since(ev.Start)
		inst.deliver(ev)
	}
	return
}

// done closes ev on the call's result and delivers it.
func (inst *ObservingExecutor) done(ctx context.Context, ev *CallEvent, err error) {
	switch {
	case err == nil:
		ev.Outcome = CallOutcomeOk
	case ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		ev.Outcome = CallOutcomeCanceled
	default:
		ev.Outcome = CallOutcomeError
	}
	ev.Err = err
	ev.ErrCode = errCode(err)
	inst.deliver(ev)
}

func (inst *ObservingExecutor) deliver(ev *CallEvent) {
	ev.Phase = CallPhaseDone
	if ev.Duration == 0 {
		ev.Duration = time.Since(ev.Start)
	}
	for _, o := range inst.observers {
		if o.ObserveCall(*ev) != nil {
			inst.observerErrors.Add(1)
		}
	}
}
