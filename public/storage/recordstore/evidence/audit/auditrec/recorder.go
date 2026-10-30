package auditrec

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/zeebo/xxh3"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// ErrClosed is what a verb returns after Close.
var ErrClosed = errors.New("auditrec: the recorder is closed")

// ErrNotDurable is what a write-ahead returns, wrapped, when the rows did
// not land and the recorder was built WithRequired: the caller must not
// act.
var ErrNotDurable = errors.New("auditrec: the audit rows are not durable")

// errNoStore is the cause of a write-ahead on a recorder built without a
// store.
var errNoStore = errors.New("auditrec: the recorder has no store")

// Defaults of the options.
const (
	DefaultMaxBacklogRows = 50_000
	DefaultFlushInterval  = time.Second
	DefaultFlushTimeout   = 5 * time.Second
	DefaultForwardQueue   = 64
	DefaultForwardTimeout = 30 * time.Second
	// recentForwarded bounds the ids the backstop skips because the prompt
	// path forwarded them lately.
	recentForwarded = 1 << 16
)

// Counts are the recorder's counters: rows buffered, rows the server
// took, rows dropped — by the backlog cap or at Close — events replaced by
// an audit-invalid row, durable seals (not among the rows written), and
// events a forwarder took or that the prompt path left to the backstop.
type Counts struct {
	Buffered       uint64
	Written        uint64
	Dropped        uint64
	Invalid        uint64
	Seals          uint64
	Forwarded      uint64
	ForwardDropped uint64
}

type config struct {
	forwarder      audit.ForwarderI
	forwardQueue   int
	forwardTimeout time.Duration
	anchors        anchor.StoreI
	cell           string
	node           option.Option[string]
	origin         audit.Origin
	originBy       evidence.OriginByE
	required       bool
	observer       recordstore.WriteObserverI[uint64, time.Time]
	maxBacklog     int
	flushInterval  time.Duration
	flushTimeout   time.Duration
	sealScope      func(run string) uint64
}

// Option configures New.
type Option func(*config)

// WithForwarder hands each durable batch's audit events to f, at least
// once (the prompt path), and lets ForwardWindow re-send stored ones (the
// backstop).
func WithForwarder(f audit.ForwarderI) Option { return func(c *config) { c.forwarder = f } }

// WithForwardQueue bounds the batches the prompt path holds for a
// forwarder that has not taken them; past it a batch is left to the
// backstop.
func WithForwardQueue(n int) Option { return func(c *config) { c.forwardQueue = n } }

// WithForwardTimeout bounds one hand-off of the prompt path.
func WithForwardTimeout(d time.Duration) Option { return func(c *config) { c.forwardTimeout = d } }

// WithAnchors ships each durable seal to st as the head of the run's seal
// scope, under cell, best effort.
func WithAnchors(st anchor.StoreI, cell string) Option {
	return func(c *config) { c.anchors, c.cell = st, cell }
}

// WithNode names the node the recorder writes for: an event without a
// Node takes it, the recorder's own rows carry it, and the reads and the
// backstop see only its rows, so several writers may share one table.
func WithNode(node string) Option { return func(c *config) { c.node = option.Some(node) } }

// WithOrigin is the app and the instance an event's origin names when the
// call identity on its context names none.
func WithOrigin(app string, instance uint64) Option {
	return func(c *config) { c.origin = audit.Origin{App: app, Instance: instance} }
}

// WithOriginBy says how an event's origin was established when the context
// carries no attestation; the default, self, is a process stating its own.
func WithOriginBy(o evidence.OriginByE) Option { return func(c *config) { c.originBy = o } }

// WithRequired makes a write-ahead whose rows did not land an error
// wrapping ErrNotDurable, which the caller must not act past.
func WithRequired(required bool) Option { return func(c *config) { c.required = required } }

// WithWriteObserver attaches o to every batch the recorder builds rows
// into. It is called on the recorder's goroutines and must not call back
// into the recorder.
func WithWriteObserver(o recordstore.WriteObserverI[uint64, time.Time]) Option {
	return func(c *config) { c.observer = o }
}

// WithMaxBacklog bounds the rows held for a server that does not take
// them; past it the oldest held batches are dropped and written back as
// one gap row.
func WithMaxBacklog(rows int) Option { return func(c *config) { c.maxBacklog = rows } }

// WithFlushInterval is how often the recorder flushes on its own; zero
// flushes only on FlushSoon, Flush, a write-ahead and Close.
func WithFlushInterval(d time.Duration) Option { return func(c *config) { c.flushInterval = d } }

// WithFlushTimeout bounds one flush, and so Close.
func WithFlushTimeout(d time.Duration) Option { return func(c *config) { c.flushTimeout = d } }

// WithSealScope is the anchor scope of a run's seal stream; the default
// hashes the scheme's name and the run.
func WithSealScope(f func(run string) uint64) Option { return func(c *config) { c.sealScope = f } }

// Recorder writes audit events, and any other rows of its store, in
// sealed batches. It is safe for concurrent use; a nil Recorder, and one
// built without a store, record nothing and report not durable.
//
// Rows are built into the current batch under one lock. A flush swaps in
// a fresh batch and inserts outside that lock, so a slow or absent server
// stalls flushes but never writers; a batch the server did not take is
// held, oldest first, with its seal, until a later flush lands it or the
// backlog cap drops it. Each batch the server takes carries one seal
// naming its rows by the digests of their canonical items, chained to the
// run's previous durable seal.
type Recorder[B BatchI] struct {
	st     StoreI[B]
	run    string
	scheme evidence.Scheme
	log    zerolog.Logger
	cfg    config
	cols   Columns

	mu                sync.Mutex
	open              bool
	closed            bool
	cur               B
	seq               uint64
	curFirst, curLast time.Time
	curRows           []evidence.SealedRow
	curEvents         []audit.Event

	flushMu  sync.Mutex
	backlog  []heldBatch[B]
	gap      gap
	lastSeal evidence.SealedRow

	buffered, written, dropped, invalid, seals, forwarded, forwardDropped atomic.Uint64

	// readMu serialises the store's scans; stClosed, under it, is set once
	// Close released the store.
	readMu   sync.Mutex
	stClosed bool

	queue   chan []audit.Event
	fwdDone chan struct{}
	recent  *recentIds

	closeOnce sync.Once
	wake      chan struct{}
	stop      chan struct{}
	done      chan struct{}
}

// heldBatch is one swapped-out batch: its rows, their time window, the
// rows with their digests for its seal, the events to forward once it
// lands, and the seal once it is in the batch (sealId zero until then).
type heldBatch[B BatchI] struct {
	batch       B
	rows        int
	first, last time.Time
	sealed      []evidence.SealedRow
	events      []audit.Event
	sealId      uint64
	sealDigest  string
}

// gap is loss not yet written back as a row.
type gap struct {
	rows        int
	first, last time.Time
}

func (inst *gap) add(rows int, first, last time.Time) {
	inst.rows += rows
	if inst.first.IsZero() || first.Before(inst.first) {
		inst.first = first
	}
	if last.After(inst.last) {
		inst.last = last
	}
}

// New is the recorder of the run named run over st, its seals and row
// digests under scheme. run names the seal stream and is every row's
// origin run; mint one per process. A nil st records nothing.
func New[B BatchI](st StoreI[B], run string, scheme evidence.Scheme, log zerolog.Logger, opts ...Option) (inst *Recorder[B], err error) {
	if run == "" {
		return nil, eh.Errorf("auditrec: a recorder needs a run")
	}
	if scheme.IsZero() {
		return nil, eh.Errorf("auditrec: a recorder needs a scheme")
	}
	inst = &Recorder[B]{st: st, run: run, scheme: scheme, log: log,
		cfg: config{forwardQueue: DefaultForwardQueue, forwardTimeout: DefaultForwardTimeout, maxBacklog: DefaultMaxBacklogRows,
			flushInterval: DefaultFlushInterval, flushTimeout: DefaultFlushTimeout},
		wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	for _, o := range opts {
		o(&inst.cfg)
	}
	if inst.cfg.sealScope == nil {
		name := scheme.Name()
		inst.cfg.sealScope = func(run string) uint64 { return xxh3.HashString(name + "|" + run) }
	}
	if st != nil {
		inst.cols = st.Columns()
		inst.cur = st.NewBatch(inst.cfg.observer)
		inst.open = true
		if inst.cfg.forwarder != nil {
			inst.queue = make(chan []audit.Event, max(1, inst.cfg.forwardQueue))
			inst.fwdDone = make(chan struct{})
			inst.recent = newRecentIds(recentForwarded)
			go inst.forwardLoop()
		}
	}
	go inst.flusher()
	return
}

// Run is the run the recorder writes under: its seal stream.
func (inst *Recorder[B]) Run() string {
	if inst == nil {
		return ""
	}
	return inst.run
}

// SealScope is the anchor scope of run's seal stream.
func (inst *Recorder[B]) SealScope(run string) uint64 { return inst.cfg.sealScope(run) }

// Counts reads the counters; zero for a nil recorder.
func (inst *Recorder[B]) Counts() (c Counts) {
	if inst == nil {
		return
	}
	return Counts{Buffered: inst.buffered.Load(), Written: inst.written.Load(), Dropped: inst.dropped.Load(), Invalid: inst.invalid.Load(),
		Seals: inst.seals.Load(), Forwarded: inst.forwarded.Load(), ForwardDropped: inst.forwardDropped.Load()}
}

// Durable says rows reach a server: the recorder has a store and is not
// closed.
func (inst *Recorder[B]) Durable() (durable bool) {
	if inst == nil {
		return false
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.open && !inst.closed
}

// Event buffers one audit event recorded under ctx: the identity on ctx
// by audit.Stamp, the origin by audit.OriginOf — the run always the
// recorder's, the app and the instance the call identity's, else
// WithOrigin's — the node WithNode's when the row names none. A row
// outside the bounds is replaced by audit.Invalid's row and logged; the
// call does not fail for it, nor for a server reason. After Close it
// returns ErrClosed.
func (inst *Recorder[B]) Event(ctx context.Context, at time.Time, row audit.AuditEvent) (err error) {
	if inst == nil {
		return nil
	}
	return inst.EventFrom(ctx, at, inst.cfg.origin, row)
}

// EventFrom is Event with def, in place of WithOrigin's, as the app and
// the instance the origin names when the call identity on ctx names none:
// for a store whose callers know the origin per call, as the trail's do
// from a bus envelope.
func (inst *Recorder[B]) EventFrom(ctx context.Context, at time.Time, def audit.Origin, row audit.AuditEvent) (err error) {
	if inst == nil {
		return nil
	}
	o := audit.OriginOf(ctx, inst.run, def)
	row = audit.Stamp(ctx, row, inst.cfg.originBy)
	if inst.cfg.node.Has && !row.Node.Has {
		row.Node = inst.cfg.node
	}
	if verr := row.Validate(); verr != nil {
		inst.log.Error().Err(verr).Str("domain", audit.Bounded(row.Domain)).Str("action", audit.Bounded(row.Action)).Msg("auditrec: an audit event is outside its bounds; an audit-invalid row is written in its place")
		row = audit.Invalid(row, verr, inst.cfg.originBy)
		inst.invalid.Add(1)
	}
	return inst.writeEvent(ctx, at, o, row, false)
}

// EventWriteAhead is Event, then WriteAhead: durable says every row
// buffered before the call, the event among them, is on the server. An
// event whose write-ahead failed stays buffered and lands later.
func (inst *Recorder[B]) EventWriteAhead(ctx context.Context, at time.Time, row audit.AuditEvent) (durable bool, err error) {
	if inst == nil {
		return false, nil
	}
	if err = inst.Event(ctx, at, row); err != nil {
		return false, err
	}
	return inst.WriteAhead(ctx)
}

// WriteAhead makes what is buffered durable before the caller acts.
// durable says it landed. When it did not, err wraps ErrNotDurable and
// the cause if the recorder was built WithRequired, and is nil otherwise,
// the failure logged; after Close it is ErrClosed.
func (inst *Recorder[B]) WriteAhead(ctx context.Context) (durable bool, err error) {
	if inst == nil {
		return false, nil
	}
	inst.mu.Lock()
	open, closed := inst.open, inst.closed
	inst.mu.Unlock()
	var cause error
	switch {
	case closed:
		return false, ErrClosed
	case !open:
		cause = errNoStore
	default:
		if cause = inst.Flush(ctx); cause == nil {
			return true, nil
		}
	}
	if inst.cfg.required {
		return false, eh.Errorf("auditrec: write-ahead: %w: %w", ErrNotDurable, cause)
	}
	inst.log.Warn().Err(cause).Msg("auditrec: write-ahead failed; the caller proceeds without a durable row")
	return false, nil
}

// Write buffers one row that add builds into the current batch under key:
// add is handed the batch and the row's id, buffers the row, and returns
// its canonical item, which the batch's seal names. It is how a store's
// kinds other than the audit event share the recorder's buffer, seals and
// backlog. id is the hash of key; zero when nothing was recorded.
func (inst *Recorder[B]) Write(ctx context.Context, key string, at time.Time, add func(b B, id uint64) (item []byte, err error)) (id uint64, err error) {
	if inst == nil {
		return 0, nil
	}
	return inst.write(at, xxh3.HashString(key), false, add, option.None[audit.Event]())
}

// writeEvent buffers an audit event under a key of this run. own is set
// for the recorder's own rows, which are written after Close began.
func (inst *Recorder[B]) writeEvent(ctx context.Context, at time.Time, o audit.Origin, row audit.AuditEvent, own bool) (err error) {
	at = at.UTC()
	key := "event|" + row.Domain + "|" + row.Action + "|" + strconv.FormatUint(row.Subject, 10) + "|" + inst.unique(at)
	id := xxh3.HashString(key)
	o.Id = id
	row.Id, row.Kind = id, "auditEvent"
	ev := audit.Event{Id: id, At: at, Origin: o, Row: row}
	_, err = inst.write(at, id, own, func(b B, id uint64) ([]byte, error) {
		return b.Event(ctx, id, at, key, o, row)
	}, option.Some(ev))
	return
}

// write runs add under the lock and notes the row for the batch's seal.
func (inst *Recorder[B]) write(at time.Time, id uint64, own bool, add func(b B, id uint64) ([]byte, error), ev option.Option[audit.Event]) (out uint64, err error) {
	at = at.UTC()
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed && !own {
		return 0, ErrClosed
	}
	if !inst.open {
		return 0, nil
	}
	item, err := add(inst.cur, id)
	if err != nil {
		inst.dropped.Add(1)
		return 0, eb.Build().Uint64("id", id).Errorf("auditrec: buffer row: %w", err)
	}
	inst.buffered.Add(1)
	inst.curRows = append(inst.curRows, evidence.SealedRow{Id: id, Digest: inst.scheme.RowDigest(item)})
	if inst.curFirst.IsZero() || at.Before(inst.curFirst) {
		inst.curFirst = at
	}
	if at.After(inst.curLast) {
		inst.curLast = at
	}
	if ev.Has && inst.queue != nil {
		inst.curEvents = append(inst.curEvents, ev.Val)
	}
	return id, nil
}

// unique is a key part no other row of this run shares.
func (inst *Recorder[B]) unique(at time.Time) string {
	inst.mu.Lock()
	inst.seq++
	n := inst.seq
	inst.mu.Unlock()
	return inst.run + "|" + strconv.FormatInt(at.UnixNano(), 36) + "|" + strconv.FormatUint(n, 36)
}

// Flush lands what is buffered and what earlier flushes could not, each
// batch with its seal; after a loss it writes the gap row and lands it in
// the same call. It returns nil only when every row buffered before the
// call is on the server; on failure the rows are held for the next.
func (inst *Recorder[B]) Flush(ctx context.Context) (err error) {
	if inst == nil {
		return nil
	}
	inst.flushMu.Lock()
	defer inst.flushMu.Unlock()
	inst.mu.Lock()
	if !inst.open {
		inst.mu.Unlock()
		return nil
	}
	inst.swapLocked()
	inst.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, inst.cfg.flushTimeout)
	defer cancel()
	err = inst.drain(ctx)
	inst.capBacklog()
	if err == nil && inst.gap.rows > 0 {
		inst.writeGap()
		inst.mu.Lock()
		if inst.open {
			inst.swapLocked()
		}
		inst.mu.Unlock()
		err = inst.drain(ctx)
	}
	return
}

// FlushSoon wakes the background flusher.
func (inst *Recorder[B]) FlushSoon() {
	if inst == nil {
		return
	}
	select {
	case inst.wake <- struct{}{}:
	default:
	}
}

// swapLocked moves a non-empty current batch onto the backlog and opens a
// fresh one. The caller holds mu.
func (inst *Recorder[B]) swapLocked() {
	if n := inst.cur.Buffered(); n > 0 {
		inst.backlog = append(inst.backlog, heldBatch[B]{batch: inst.cur, rows: n, first: inst.curFirst, last: inst.curLast, sealed: inst.curRows, events: inst.curEvents})
		inst.cur = inst.st.NewBatch(inst.cfg.observer)
		inst.curFirst, inst.curLast = time.Time{}, time.Time{}
		inst.curRows, inst.curEvents = nil, nil
	}
}

// drain lands the held batches, oldest first, each with its seal, and
// stops at the first the server does not take. The caller holds flushMu.
func (inst *Recorder[B]) drain(ctx context.Context) (err error) {
	for len(inst.backlog) > 0 {
		b := &inst.backlog[0]
		if err = inst.seal(b); err != nil {
			return
		}
		if ferr := b.batch.Flush(ctx); ferr != nil {
			return eh.Errorf("auditrec: flush: %w", ferr)
		}
		b.batch.Close()
		inst.written.Add(uint64(b.rows))
		n := inst.seals.Add(1)
		inst.lastSeal = evidence.SealedRow{Id: b.sealId, Digest: b.sealDigest}
		inst.forwardPrompt(b.events)
		inst.backlog = inst.backlog[1:]
		inst.anchorSeal(ctx, n, b.sealDigest)
	}
	return
}

// seal puts b's seal into its batch, chained to the last durable seal,
// once: a batch retried after a failed flush keeps the seal it has, since
// nothing lands ahead of it. The caller holds flushMu.
func (inst *Recorder[B]) seal(b *heldBatch[B]) (err error) {
	if b.sealId != 0 {
		return nil
	}
	at := time.Now().UTC()
	key := "seal|" + inst.unique(at)
	id := xxh3.HashString(key)
	spec := SealSpec{Scheme: inst.scheme, Stream: inst.run, Prev: inst.lastSeal.Id, PrevDigest: inst.lastSeal.Digest, Rows: b.sealed}
	digest, err := b.batch.Seal(id, at, key, audit.Origin{Id: id, Run: inst.run}, spec)
	if err != nil {
		return eh.Errorf("auditrec: seal: %w", err)
	}
	b.sealId, b.sealDigest = id, digest
	return nil
}

// anchorSeal ships the n-th durable seal. The caller holds flushMu.
func (inst *Recorder[B]) anchorSeal(ctx context.Context, n uint64, digest string) {
	if inst.cfg.anchors == nil {
		return
	}
	h := anchor.Head{Cell: inst.cfg.cell, Scope: inst.cfg.sealScope(inst.run), Ordinal: n, Scheme: inst.scheme.Name(), Digest: digest}
	if _, err := inst.cfg.anchors.Append(ctx, h); err != nil {
		inst.log.Warn().Err(err).Uint64("ordinal", n).Msg("auditrec: the seal could not be anchored")
	}
}

// capBacklog drops the oldest held batches past the cap, keeping the
// newest. The caller holds flushMu.
func (inst *Recorder[B]) capBacklog() {
	var held int
	for _, b := range inst.backlog {
		held += b.rows
	}
	var dropped int
	for held > inst.cfg.maxBacklog && len(inst.backlog) > 1 {
		b := inst.backlog[0]
		b.batch.Close()
		inst.backlog = inst.backlog[1:]
		held -= b.rows
		dropped += b.rows
		inst.gap.add(b.rows, b.first, b.last)
	}
	if dropped > 0 {
		inst.dropped.Add(uint64(dropped))
		inst.log.Error().Int("dropped", dropped).Int("held", held).Msg("auditrec: the server has not taken the audit rows; the oldest are dropped")
	}
}

// writeGap buffers the gap row for what the cap dropped. The caller holds
// flushMu.
func (inst *Recorder[B]) writeGap() {
	g := inst.gap
	inst.gap = gap{}
	row := audit.Gap(g.rows, g.first, g.last, inst.cfg.originBy, inst.cfg.node)
	o := audit.OriginOf(context.Background(), inst.run, inst.cfg.origin)
	if err := inst.writeEvent(context.Background(), time.Now(), o, row, true); err != nil {
		inst.log.Error().Err(err).Int("rows", g.rows).Msg("auditrec: the audit-gap row could not be buffered")
	}
}

// flusher lands buffered rows on its interval, on a wake-up, and at Close.
func (inst *Recorder[B]) flusher() {
	defer close(inst.done)
	var tick <-chan time.Time
	if inst.cfg.flushInterval > 0 {
		t := time.NewTicker(inst.cfg.flushInterval)
		defer t.Stop()
		tick = t.C
	}
	flush := func() {
		if err := inst.Flush(context.Background()); err != nil {
			inst.log.Warn().Err(err).Msg("auditrec: flush (rows stay buffered for the next flush)")
		}
	}
	for {
		select {
		case <-tick:
			flush()
		case <-inst.wake:
			flush()
		case <-inst.stop:
			flush()
			return
		}
	}
}

// Close refuses further verbs, runs the final flush within its bound,
// drains the forward queue, and releases the batches and the store. What
// the server did not take by then is lost: counted as dropped and logged,
// with no gap row, since nothing runs afterwards. It is idempotent.
func (inst *Recorder[B]) Close() {
	if inst == nil {
		return
	}
	inst.closeOnce.Do(inst.close)
}

func (inst *Recorder[B]) close() {
	inst.mu.Lock()
	inst.closed = true
	inst.mu.Unlock()
	close(inst.stop)
	<-inst.done
	inst.flushMu.Lock()
	inst.mu.Lock()
	lost := 0
	if inst.open {
		lost += inst.cur.Buffered()
		inst.cur.Close()
		inst.open = false
	}
	inst.mu.Unlock()
	// No flush runs from here on, so nothing feeds the queue.
	if inst.queue != nil {
		close(inst.queue)
		<-inst.fwdDone
	}
	for _, b := range inst.backlog {
		b.batch.Close()
		lost += b.rows
	}
	inst.backlog = nil
	if inst.gap.rows > 0 {
		inst.log.Error().Int("rows", inst.gap.rows).Msg("auditrec: closed with a gap no row records")
		inst.gap = gap{}
	}
	inst.flushMu.Unlock()
	if lost > 0 {
		inst.dropped.Add(uint64(lost))
		inst.log.Error().Int("lost", lost).Msg("auditrec: closed with rows the server had not taken; they are lost")
	}
	if inst.st != nil {
		inst.readMu.Lock()
		inst.st.Close()
		inst.stClosed = true
		inst.readMu.Unlock()
	}
}
