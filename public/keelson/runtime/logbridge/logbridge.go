// Package logbridge routes zerolog events into boxer.facts. With the
// `binary_log` build tag set (already on the project tag list), zerolog
// hands each event to its configured io.Writer as a CBOR-encoded
// indefinite-length map; Sink decodes that map, packages it as a
// factsstore.LogRow, and hands it off to a background flusher that calls
// FactsStoreI.WriteLog. The producer side never blocks on the store, so a
// stalled ClickHouse round-trip can't slow a hot logging path — overflow
// rows are dropped-oldest with a counter exposed via Dropped().
//
// Intended composition (per ADR-0026 §SD6):
//
//	sink, _ := logbridge.NewSink(store, logbridge.Config{AppId: "play"})
//	defer sink.Close()
//	tee := zerolog.MultiLevelWriter(os.Stdout, sink)
//	logger := zerolog.New(tee).With().Timestamp().Logger()
//
// MultiLevelWriter calls Sink.WriteLevel for every event, so the level
// envelope is supplied out-of-band by zerolog rather than re-extracted
// from the CBOR map.
package logbridge

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// AppIdFieldName is the zerolog context field name from which the Sink
// pulls the per-event AppId. Hosts that want N apps to share one Sink
// pre-tag each app's logger with this field via app.AppLogger; events
// without the field fall back to Config.AppId.
const AppIdFieldName = "app_id"

// Config configures a Sink. All fields are optional; sensible defaults
// apply for any zero value.
type Config struct {
	// AppId is the default tag applied when the decoded CBOR event lacks
	// an explicit "app_id" field (see AppIdFieldName). Hosts running one
	// Sink for multiple apps leave this empty and rely on per-app loggers
	// built via app.AppLogger to inject the field; single-app hosts can
	// set it once and skip the per-event tagging.
	AppId app.AppIdT

	// Capacity bounds the in-memory ring. When full, the oldest row is
	// dropped (incrementing the Dropped() counter) so producers never
	// block. Defaults to 4096.
	Capacity int

	// FlushN is the row count that triggers an immediate drain. Defaults
	// to 512. Setting FlushN >= Capacity disables size-based flushing
	// (the timer is the only flush trigger).
	FlushN int

	// FlushInterval is the wall-clock interval after which any pending
	// rows are drained even if FlushN is not reached. Defaults to 200ms.
	FlushInterval time.Duration

	// MinLevel filters out events strictly below this level before
	// decode. Defaults to zerolog.TraceLevel (no filtering). Note that
	// zerolog already filters per logger; setting MinLevel here is a
	// belt-and-braces guard for hosts that route many loggers through
	// one Sink.
	MinLevel zerolog.Level

	// TimeFieldFormat tells the decoder how to interpret the "time"
	// field when present:
	//   - TimeFormatString (default) — zerolog.TimeFieldFormat is a
	//     time-package layout string (RFC3339 by default); the field
	//     arrives in CBOR as a text string.
	//   - TimeFormatUnixMs / TimeFormatUnixNano — the field arrives as
	//     an integer.
	// If the actual CBOR type does not match, we fall back gracefully:
	// numbers are interpreted as unix-ms, strings via RFC3339.
	TimeFieldFormat TimeFormatE

	// Layout supplements TimeFieldFormat when TimeFieldFormat is
	// TimeFormatString. Defaults to time.RFC3339 to match zerolog.
	Layout string

	// TailCapacity bounds the in-memory tail buffer the Sink retains for
	// readers like the imzero2 log-viewer widget. Independent of
	// Capacity (which sizes the flush ring). Defaults to 1024 rows;
	// setting it to 0 disables tail retention. The tail buffer holds
	// the most recently decoded rows even after they have been flushed
	// to the FactsStoreI, so a tail UI sees a stable view rather than
	// the near-empty flush ring.
	TailCapacity int
}

// TimeFormatE selects how the decoder interprets the zerolog "time" field.
type TimeFormatE uint8

const (
	TimeFormatString  TimeFormatE = 0
	TimeFormatUnixMs  TimeFormatE = 1
	TimeFormatUnixNs  TimeFormatE = 2
	TimeFormatUnixSec TimeFormatE = 3
)

// Sink is a zerolog.LevelWriter that decodes each CBOR-encoded event,
// queues it, and asynchronously hands the result to FactsStoreI.WriteLog.
// Construct with NewSink; close with Close to drain the ring before
// process exit.
type Sink struct {
	store factsstore.FactsStoreI
	cfg   Config

	mu        sync.Mutex
	ring      []factsstore.LogRow
	head      int
	count     int
	notFull   *sync.Cond
	wakeFlush chan struct{}

	// Tail buffer — a SEPARATE fixed-size ring that retains the most
	// recently decoded rows for UI consumers (logviewer widget,
	// debugger). Independent of the flush ring; never drained by the
	// flusher. Drop-oldest on overflow.
	tailMu   sync.Mutex
	tailRing []factsstore.LogRow
	tailHead int
	tailLen  int

	dropped   atomic.Uint64
	decoded   atomic.Uint64
	written   atomic.Uint64
	parseErrs atomic.Uint64
	writeErrs atomic.Uint64

	stopCh chan struct{}
	doneCh chan struct{}
	closed atomic.Bool
}

var (
	_ zerolog.LevelWriter = (*Sink)(nil)
)

// NewSink wires a Sink. The flusher goroutine starts immediately so the
// caller can install the Sink as a zerolog writer and expect it to drain
// from the first event onward.
func NewSink(store factsstore.FactsStoreI, cfg Config) (inst *Sink, err error) {
	if store == nil {
		err = eh.Errorf("logbridge: store must not be nil")
		return
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 4096
	}
	if cfg.FlushN <= 0 {
		cfg.FlushN = 512
	}
	if cfg.FlushN > cfg.Capacity {
		cfg.FlushN = cfg.Capacity
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 200 * time.Millisecond
	}
	if cfg.Layout == "" {
		cfg.Layout = time.RFC3339
	}
	if cfg.TailCapacity < 0 {
		cfg.TailCapacity = 0
	} else if cfg.TailCapacity == 0 {
		cfg.TailCapacity = 1024
	}
	inst = &Sink{
		store:     store,
		cfg:       cfg,
		ring:      make([]factsstore.LogRow, cfg.Capacity),
		wakeFlush: make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
	if cfg.TailCapacity > 0 {
		inst.tailRing = make([]factsstore.LogRow, cfg.TailCapacity)
	}
	inst.notFull = sync.NewCond(&inst.mu)
	go inst.flushLoop()
	return
}

// Write satisfies io.Writer. zerolog will call Write only when its
// MultiLevelWriter wrapper is absent; the level-aware path (WriteLevel)
// is preferred. Returns len(p), nil unconditionally to honour the
// io.Writer contract — drops are bookkept on the dropped counter, not
// surfaced as errors (which zerolog would print to stderr in a tight
// loop).
func (inst *Sink) Write(p []byte) (n int, err error) {
	n = len(p)
	if inst.closed.Load() {
		return
	}
	inst.ingest(zerolog.NoLevel, p)
	return
}

// WriteLevel is the level-aware variant zerolog prefers when present.
// Applies the MinLevel filter before decoding.
func (inst *Sink) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	n = len(p)
	if inst.closed.Load() {
		return
	}
	if level != zerolog.NoLevel && level < inst.cfg.MinLevel {
		return
	}
	inst.ingest(level, p)
	return
}

func (inst *Sink) ingest(level zerolog.Level, p []byte) {
	row, ok := inst.decode(level, p)
	if !ok {
		return
	}
	inst.enqueue(row)
}

func (inst *Sink) enqueue(row factsstore.LogRow) {
	inst.appendTail(row)
	inst.mu.Lock()
	if inst.count == inst.cfg.Capacity {
		// Drop-oldest: advance head, count stays at capacity.
		inst.head = (inst.head + 1) % inst.cfg.Capacity
		inst.count--
		inst.dropped.Add(1)
	}
	tail := (inst.head + inst.count) % inst.cfg.Capacity
	inst.ring[tail] = row
	inst.count++
	wake := inst.count >= inst.cfg.FlushN
	inst.mu.Unlock()
	if wake {
		// Non-blocking nudge — the flusher reads at most one wake per
		// tick, drains everything; a queued wake is sufficient.
		select {
		case inst.wakeFlush <- struct{}{}:
		default:
		}
	}
}

// appendTail writes the decoded row to the retain-N tail buffer used by
// UI consumers. Independent of the flush ring; never drained.
func (inst *Sink) appendTail(row factsstore.LogRow) {
	if inst.tailRing == nil {
		return
	}
	capN := len(inst.tailRing)
	inst.tailMu.Lock()
	defer inst.tailMu.Unlock()
	if inst.tailLen == capN {
		// Drop-oldest.
		inst.tailHead = (inst.tailHead + 1) % capN
		inst.tailLen--
	}
	pos := (inst.tailHead + inst.tailLen) % capN
	inst.tailRing[pos] = row
	inst.tailLen++
}

// Tail returns a snapshot of the most recent rows in the tail buffer,
// newest LAST (consumers can range-over them in chronological order).
// If max > 0 the returned slice is capped at max rows; max == 0 means
// "everything currently retained". The returned slice is a copy — the
// caller may retain it past further enqueues.
func (inst *Sink) Tail(max int) (rows []factsstore.LogRow) {
	if inst.tailRing == nil {
		return
	}
	inst.tailMu.Lock()
	defer inst.tailMu.Unlock()
	n := inst.tailLen
	if max > 0 && n > max {
		n = max
	}
	if n == 0 {
		return
	}
	rows = make([]factsstore.LogRow, n)
	capN := len(inst.tailRing)
	// When max < tailLen, we want the LAST n rows — start that far
	// before the logical tail.
	start := inst.tailHead + (inst.tailLen - n)
	for i := 0; i < n; i++ {
		rows[i] = inst.tailRing[(start+i)%capN]
	}
	return
}

// TailLen returns the current number of rows in the tail buffer. Cheap
// to call; useful for UIs that want a row-count badge without
// allocating a snapshot.
func (inst *Sink) TailLen() (n int) {
	if inst.tailRing == nil {
		return
	}
	inst.tailMu.Lock()
	n = inst.tailLen
	inst.tailMu.Unlock()
	return
}

// TailCapacity returns the configured tail-buffer size. UIs use it to
// compute a "X / Y rows retained" indicator alongside TailLen.
func (inst *Sink) TailCapacity() (n int) {
	n = len(inst.tailRing)
	return
}

func (inst *Sink) flushLoop() {
	defer close(inst.doneCh)
	ticker := time.NewTicker(inst.cfg.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-inst.stopCh:
			inst.drain()
			return
		case <-inst.wakeFlush:
			inst.drain()
		case <-ticker.C:
			inst.drain()
		}
	}
}

// drain pulls up to FlushN rows from the ring and writes them via the
// store. We hold the lock only for the slice-copy; the writes happen
// unlocked so a slow store doesn't stall producers.
func (inst *Sink) drain() {
	for {
		inst.mu.Lock()
		take := inst.count
		if take > inst.cfg.FlushN {
			take = inst.cfg.FlushN
		}
		if take == 0 {
			inst.mu.Unlock()
			return
		}
		batch := make([]factsstore.LogRow, take)
		for i := 0; i < take; i++ {
			batch[i] = inst.ring[(inst.head+i)%inst.cfg.Capacity]
			inst.ring[(inst.head+i)%inst.cfg.Capacity] = factsstore.LogRow{}
		}
		inst.head = (inst.head + take) % inst.cfg.Capacity
		inst.count -= take
		inst.mu.Unlock()
		// Land the whole batch in one store call. WriteLogs ships it as a
		// single insert; the prior per-row loop turned a FlushN-sized batch
		// into FlushN round-trips (one ClickHouse part each), defeating the
		// ring's batching.
		_, werr := inst.store.WriteLogs(batch)
		if werr != nil {
			// Store-write failures are their own counter, NOT parseErrs: a
			// ClickHouse outage must not read as "wrong zerolog build tag".
			inst.writeErrs.Add(uint64(len(batch)))
			continue
		}
		inst.written.Add(uint64(len(batch)))
	}
}

// Close stops the flusher and drains any pending rows synchronously.
// Subsequent Write/WriteLevel calls are no-ops. Safe to call once.
func (inst *Sink) Close() (err error) {
	if inst.closed.Swap(true) {
		return
	}
	close(inst.stopCh)
	<-inst.doneCh
	return
}

// Dropped returns the running count of rows discarded due to ring
// overflow. A nonzero value means the configured Capacity was too small
// for the observed log rate — host should raise Capacity or tighten
// MinLevel.
func (inst *Sink) Dropped() (n uint64) {
	n = inst.dropped.Load()
	return
}

// Decoded returns the running count of CBOR events successfully decoded.
func (inst *Sink) Decoded() (n uint64) {
	n = inst.decoded.Load()
	return
}

// Written returns the running count of rows successfully passed to the
// store. Equal to Decoded() less any store-write errors.
func (inst *Sink) Written() (n uint64) {
	n = inst.written.Load()
	return
}

// ParseErrors returns the running count of CBOR decode failures only.
// A persistently nonzero value usually means a mismatched zerolog build
// tag (JSON wire format despite `binary_log`). Store-write failures are
// tracked separately by WriteErrors so a ClickHouse outage is not
// misread as a decode/build-tag problem.
func (inst *Sink) ParseErrors() (n uint64) {
	n = inst.parseErrs.Load()
	return
}

// WriteErrors returns the running count of rows the store rejected
// (FactsStoreI.WriteLogs returned an error). Distinct from ParseErrors:
// a nonzero value points at the store/transport (e.g. ClickHouse
// unreachable), not the decode path.
func (inst *Sink) WriteErrors() (n uint64) {
	n = inst.writeErrs.Load()
	return
}

// decode parses one CBOR-encoded zerolog event into a LogRow. The well-
// known envelope keys (level, message, time, caller, error, stack) are
// extracted into typed fields; every other key becomes a typed LogField
// driven by the Go type fxamacker/cbor decoded the value into.
func (inst *Sink) decode(level zerolog.Level, p []byte) (row factsstore.LogRow, ok bool) {
	var raw map[string]any
	if err := cbor.Unmarshal(p, &raw); err != nil {
		inst.parseErrs.Add(1)
		return
	}
	inst.decoded.Add(1)
	row.AppId = inst.cfg.AppId
	if level != zerolog.NoLevel {
		row.Level = level.String()
	}
	for k, v := range raw {
		switch k {
		case zerolog.LevelFieldName:
			if row.Level == "" {
				row.Level = asString(v)
			}
		case zerolog.MessageFieldName:
			row.Message = asString(v)
		case zerolog.TimestampFieldName:
			row.Ts = inst.decodeTime(v)
		case zerolog.CallerFieldName:
			row.Caller = asString(v)
		case zerolog.ErrorFieldName:
			// Try the structured shape first — eh.MarshalError emits
			// {streams:[...]} when wired via zerolog.ErrorMarshalFunc.
			// On match we keep both forms: ErrorContext for the
			// detail-pane tree renderer, plus a flat Summary in row.Error
			// so the table column and any string-only consumers still
			// work. On non-match (plain-string error), fall through to
			// the existing asString lift unchanged.
			if ctx := decodeErrorContext(v); ctx != nil {
				row.ErrorContext = ctx
				row.Error = ctx.Summary()
			} else {
				row.Error = asString(v)
			}
		case zerolog.ErrorStackFieldName:
			row.Stack = asString(v)
		case "service":
			row.Service = asString(v)
		case AppIdFieldName:
			// Per-event tag wins over the Sink default — lets one Sink
			// serve many apps when each app's logger was built via
			// app.AppLogger.
			row.AppId = app.AppIdT(asString(v))
		default:
			row.Fields = append(row.Fields, makeField(k, v))
		}
	}
	if row.Ts.IsZero() {
		row.Ts = time.Now().UTC()
	}
	ok = true
	return
}

// asString returns a stringified representation of v, suitable for the
// envelope text fields. CBOR primitives round-trip through the matching
// Go type; anything else (arrays/maps) is stringified via fmt at the
// caller.
func asString(v any) (s string) {
	switch t := v.(type) {
	case string:
		s = t
	case []byte:
		s = string(t)
	case nil:
		// leave empty
	default:
		s = stringify(v)
	}
	return
}

// makeField type-dispatches a CBOR-decoded value into a tagged LogField.
// fxamacker/cbor decodes: text string→string, byte string→[]byte,
// unsigned int→uint64, negative int→int64, float→float64, bool→bool,
// nil→nil, array→[]any, map→map[any]any. The latter two collapse to
// their stringified form so they still land in the string section.
func makeField(name string, v any) (f factsstore.LogField) {
	f.Name = name
	switch t := v.(type) {
	case string:
		f.Kind = factsstore.LogFieldKindString
		f.Str = t
	case []byte:
		f.Kind = factsstore.LogFieldKindBytes
		f.Bytes = t
	case bool:
		f.Kind = factsstore.LogFieldKindBool
		f.Bool = t
	case int64:
		f.Kind = factsstore.LogFieldKindInt
		f.Int = t
	case int:
		f.Kind = factsstore.LogFieldKindInt
		f.Int = int64(t)
	case uint64:
		f.Kind = factsstore.LogFieldKindUint
		f.Uint = t
	case uint:
		f.Kind = factsstore.LogFieldKindUint
		f.Uint = uint64(t)
	case float64:
		f.Kind = factsstore.LogFieldKindFloat
		f.Float = t
	case float32:
		f.Kind = factsstore.LogFieldKindFloat
		f.Float = float64(t)
	case time.Time:
		f.Kind = factsstore.LogFieldKindTime
		f.Time = t
	case nil:
		f.Kind = factsstore.LogFieldKindString
		// Str is "" — readers can distinguish from a present empty
		// string only by the absence of the field in queries.
	default:
		f.Kind = factsstore.LogFieldKindUnknown
		f.Str = stringify(v)
	}
	return
}

func (inst *Sink) decodeTime(v any) (ts time.Time) {
	switch t := v.(type) {
	case string:
		parsed, err := time.Parse(inst.cfg.Layout, t)
		if err == nil {
			ts = parsed
		}
	case int64:
		ts = unixToTime(uint64(t), inst.cfg.TimeFieldFormat)
	case uint64:
		ts = unixToTime(t, inst.cfg.TimeFieldFormat)
	case float64:
		ts = unixToTime(uint64(t), inst.cfg.TimeFieldFormat)
	case time.Time:
		ts = t
	}
	return
}

func unixToTime(v uint64, fmt TimeFormatE) (ts time.Time) {
	switch fmt {
	case TimeFormatUnixSec:
		ts = time.Unix(int64(v), 0).UTC()
	case TimeFormatUnixNs:
		ts = time.Unix(0, int64(v)).UTC()
	default:
		// TimeFormatString fell through with a numeric value — treat as ms.
		// TimeFormatUnixMs is the explicit case for the same branch.
		ts = time.UnixMilli(int64(v)).UTC()
	}
	return
}
