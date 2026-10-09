package trail

import (
	"context"
	"encoding/hex"
	"iter"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"github.com/zeebo/xxh3"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// RequiredEnv decides what happens to work that leaves the machine — a
// model call, a fetch — when its trail row cannot be made durable first
// (ADR-0277 §SD3).
var RequiredEnv = env.NewBool(env.Spec{
	Name:        "BOXER_TRAIL_REQUIRED",
	Default:     "false",
	Description: "refuse a model call whose audit trail rows cannot be written to boxer.facts before the request leaves the machine, and an egress fetch on a host with no durable trail; false lets the work proceed, logged and marked as not durable",
	Category:    env.CategoryE("boxer-trail"),
})

// Context is who a row came from and what it belongs to: the components
// every trail row may carry beside its own (ADR-0277 §SD1).
type Context struct {
	Origin       Origin
	Conversation option.Option[Conversation]
	Delegation   option.Option[Delegation]
}

// flushTimeout bounds one flush: the work it records is already decided by
// then, so the bound is what keeps a slow server from stalling the next.
const flushTimeout = 5 * time.Second

// MaxBacklogRows bounds the rows the recorder holds for a server that does
// not take them. Past it the oldest held batches are dropped, and logged:
// a host whose trail is down for hours must not grow without bound, and
// what is dropped is what BOXER_TRAIL_REQUIRED exists to refuse.
const MaxBacklogRows = 50_000

// Recorder is the one writer of the trail: it stamps the run, composes
// each row's natural key and components, and owns the buffer and its
// flushes. App and window come from the caller: every writer takes them
// from the bus envelope's sender ([Recorder.Origin], or [Recorder.OriginOf]
// over the sender it kept), never from a payload. It is safe for concurrent use. A nil Recorder, and one built
// without an executor, record nothing and report not durable.
//
// Rows are built into the current store under mu. A flush swaps in a fresh
// one and inserts the batch outside mu, so a slow or absent server stalls
// other flushes but never the writers; a batch the server did not take is
// held, oldest first, until a later flush lands it or MaxBacklogRows drops
// it. Reads go through a store of their own.
type Recorder struct {
	run      string
	required bool
	log      zerolog.Logger
	exec     recordstore.ExecutorI

	mu  sync.Mutex
	cur *TrailStore
	seq uint64
	// curFirst and curLast are the earliest and latest row timestamps in
	// cur, for the gap window should its batch be dropped. Under mu.
	curFirst, curLast time.Time

	// flushMu serialises flushes; backlog is the batches a flush did not
	// land, oldest first, with their row counts. Both under flushMu.
	flushMu    sync.Mutex
	backlog    []heldBatch
	maxBacklog int
	// gap is what the cap dropped since the last gap row: rows, and the
	// earliest and latest row timestamps among them. Under flushMu.
	gap gap

	// The counters of ADR-0296 §SD4 and §SD6, read through Counts.
	buffered, written, dropped, invalid, forwarded, forwardDropped atomic.Uint64

	readMu sync.Mutex
	read   *TrailStore

	// observer, readTable and forwarder are the options: an observer on
	// every store rows are built into, the table the read store scans, and
	// the forwarder the prompt path feeds (fwd is its state).
	observer  recordstore.WriteObserverI[uint64, time.Time]
	readTable string
	forwarder ForwarderI
	fwd       *forwardState

	wake chan struct{}
	stop chan struct{}
	done chan struct{}
}

// heldBatch is one swapped-out store, the rows it holds, and the earliest
// and latest row timestamps among them.
type heldBatch struct {
	store       *TrailStore
	rows        int
	first, last time.Time
}

// gap is loss not yet written back as a row.
type gap struct {
	rows        int
	first, last time.Time
}

func (inst *gap) add(b heldBatch) {
	inst.rows += b.rows
	if inst.first.IsZero() || b.first.Before(inst.first) {
		inst.first = b.first
	}
	if b.last.After(inst.last) {
		inst.last = b.last
	}
}

// Counts are the recorder's counters (ADR-0296 §SD4), over every verb:
// rows buffered, rows the server took, rows dropped — by the backlog cap
// or at Close — and audit events replaced by an audit-invalid row.
type Counts struct {
	Buffered uint64
	Written  uint64
	Dropped  uint64
	Invalid  uint64
	// Forwarded is the audit events a forwarder took, by either path;
	// ForwardDropped those the prompt path gave up on — a full queue or a
	// refused batch — and left to the backstop.
	Forwarded      uint64
	ForwardDropped uint64
}

// Counts reads the counters. Zero for a nil recorder.
func (inst *Recorder) Counts() (c Counts) {
	if inst == nil {
		return
	}
	return Counts{Buffered: inst.buffered.Load(), Written: inst.written.Load(), Dropped: inst.dropped.Load(), Invalid: inst.invalid.Load(),
		Forwarded: inst.forwarded.Load(), ForwardDropped: inst.forwardDropped.Load()}
}

// RecorderOption configures NewRecorder.
type RecorderOption func(*Recorder)

// WithWriteObserver attaches o to every store the recorder builds rows
// into (ADR-0296 §SD3): it is told each row's key when it is buffered,
// then once either the batch that made it durable or that it was
// discarded, as [recordstore.WriteObserverI] promises. It is called on
// the recorder's goroutines — a writer's under the buffer lock, a flush's
// outside it — and must not call back into the recorder.
func WithWriteObserver(o recordstore.WriteObserverI[uint64, time.Time]) RecorderOption {
	return func(inst *Recorder) { inst.observer = o }
}

// WithReadTable names the table the recorder's reads scan (ADR-0296
// §SD9 d): a storage table, or a Merge over several, while writes still go
// to the facts table. Empty reads the table written to.
func WithReadTable(table string) RecorderOption {
	return func(inst *Recorder) { inst.readTable = table }
}

// NewRecorder builds the recorder of the run named run. exec reaches the
// server holding boxer.facts; nil is a host without a durable backend.
func NewRecorder(exec recordstore.ExecutorI, run string, log zerolog.Logger, opts ...RecorderOption) (inst *Recorder) {
	inst = &Recorder{run: run, required: RequiredEnv.Get(), log: log, exec: exec, maxBacklog: MaxBacklogRows,
		wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	for _, o := range opts {
		o(inst)
	}
	if inst.forwarder != nil && exec != nil {
		inst.fwd = newForwardState()
	}
	if exec != nil {
		inst.cur = inst.newStore()
		inst.read = NewTrailStore(inst.exec, nil, TrailStoreConfig{ReadTable: inst.readTable})
	}
	go inst.flusher()
	if inst.fwd != nil {
		go inst.forwardLoop()
	}
	return
}

// newStore is a store rows are built into: the current one, and each
// batch a flush swaps out.
func (inst *Recorder) newStore() (st *TrailStore) {
	return NewTrailStore(inst.exec, nil, TrailStoreConfig{WriteObserver: inst.storeObserver()})
}

// Close flushes what is buffered, within the flush's bound, and releases
// the stores. What the server did not take by then is lost: counted as
// dropped and logged (ADR-0296 §SD4), with no gap row, since nothing runs
// afterwards.
func (inst *Recorder) Close() {
	if inst == nil {
		return
	}
	close(inst.stop)
	<-inst.done
	if inst.fwd != nil {
		close(inst.fwd.queue)
		<-inst.fwd.done
	}
	inst.mu.Lock()
	cur := inst.cur
	inst.cur = nil
	lost := 0
	if cur != nil {
		lost += cur.Buffered()
	}
	inst.mu.Unlock()
	inst.flushMu.Lock()
	for _, b := range inst.backlog {
		b.store.Close()
		lost += b.rows
	}
	inst.backlog = nil
	if inst.gap.rows > 0 {
		inst.log.Error().Int("rows", inst.gap.rows).Msg("trail: closed with a gap no row records")
		inst.gap = gap{}
	}
	inst.flushMu.Unlock()
	if lost > 0 {
		inst.dropped.Add(uint64(lost))
		inst.log.Error().Int("lost", lost).Msg("trail: closed with rows the server had not taken; they are lost")
	}
	if cur != nil {
		cur.Close()
	}
	inst.readMu.Lock()
	if inst.read != nil {
		inst.read.Close()
		inst.read = nil
	}
	inst.readMu.Unlock()
}

// Durable says rows land on boxer.facts.
func (inst *Recorder) Durable() (durable bool) {
	if inst == nil {
		return false
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cur != nil
}

// Origin is the origin of a bus message: this run, and the app and window
// the bus stamped on the envelope (ADR-0191 §SD2).
func (inst *Recorder) Origin(msg *app.Msg) (o Origin) {
	return inst.OriginOf(msg.Sender, msg.SenderInstance)
}

// OriginOf is the origin of an app's window in this run.
func (inst *Recorder) OriginOf(appId app.AppIdT, instance uint64) (o Origin) {
	o = Origin{App: string(appId), Instance: instance}
	if inst != nil {
		o.Run = inst.run
	}
	return
}

// begin opens the row keyed key with its context components, under ctx —
// the store's write observer reads the call identity off it. The caller
// holds mu and has checked the store.
func (inst *Recorder) begin(ctx context.Context, key string, at time.Time, c Context) (b *TrailEntityBuilder, id uint64) {
	id = xxh3.HashString(key)
	b = inst.cur.BeginCtx(ctx, id, at.UTC(), TrailEnvelope{NaturalKey: []byte(key)})
	o := c.Origin
	o.Id = id
	if o.Run == "" {
		o.Run = inst.run
	}
	b.AddOrigin(o)
	if c.Conversation.Has {
		v := c.Conversation.Val
		v.Id = id
		b.AddConversation(v)
	}
	if c.Delegation.Has {
		v := c.Delegation.Val
		v.Id = id
		b.AddDelegation(v)
	}
	return
}

// write runs add under the lock; without a store it does nothing. The
// verbs whose identity is a bus envelope pass context.Background().
func (inst *Recorder) write(ctx context.Context, key string, at time.Time, c Context, add func(b *TrailEntityBuilder, id uint64)) (err error) {
	if inst == nil {
		return nil
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.cur == nil {
		return nil
	}
	b, id := inst.begin(ctx, key, at, c)
	add(b, id)
	if err = b.Commit(); err != nil {
		err = eb.Build().Str("key", key).Errorf("trail: buffer row: %w", err)
		return
	}
	inst.buffered.Add(1)
	at = at.UTC()
	if inst.curFirst.IsZero() || at.Before(inst.curFirst) {
		inst.curFirst = at
	}
	if at.After(inst.curLast) {
		inst.curLast = at
	}
	return
}

// LlmCall buffers a model call's row; its natural key is the call id.
func (inst *Recorder) LlmCall(at time.Time, c Context, row LlmCall) (err error) {
	return inst.write(context.Background(), row.CallId, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "llmCall"
		b.AddLlmCall(row)
	})
}

// LlmMessage buffers one message's row, with its text when body is set;
// its natural key is the call id and the ordinal.
func (inst *Recorder) LlmMessage(at time.Time, c Context, row LlmMessage, body option.Option[LlmMessageBody]) (err error) {
	key := row.CallId + "/" + strconv.FormatUint(uint64(row.Ordinal), 10)
	return inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "llmMessage"
		b.AddLlmMessage(row)
		if body.Has {
			v := body.Val
			v.Id = id
			b.AddLlmMessageBody(v)
		}
	})
}

// AgentAction buffers one action-record row. Its natural key names the
// task, the call's key and the decision, and is unique like every event's,
// so the dispatcher's row and the final row of one call are two rows.
func (inst *Recorder) AgentAction(at time.Time, c Context, cause option.Option[Cause], row AgentAction) (err error) {
	key := "action|" + c.Delegation.Val.Task + "|" + row.Key + "|" + row.Decision + "|" + inst.unique(at)
	return inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
		if cause.Has {
			v := cause.Val
			v.Id = id
			b.AddCause(v)
		}
		row.Id, row.Kind = id, "agentAction"
		b.AddAgentAction(row)
	})
}

// AgentGrant buffers one grant event; cause names the model call that asked
// for it, when one did.
func (inst *Recorder) AgentGrant(at time.Time, c Context, cause option.Option[Cause], row AgentGrant) (err error) {
	key := "grant|" + c.Delegation.Val.Task + "|" + row.Event + "|" + inst.unique(at)
	return inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
		if cause.Has {
			v := cause.Val
			v.Id = id
			b.AddCause(v)
		}
		row.Id, row.Kind = id, "agentGrant"
		b.AddAgentGrant(row)
	})
}

// AgentCapture buffers one capture's row; cause names the model call that
// asked for the capture, when one did.
func (inst *Recorder) AgentCapture(at time.Time, c Context, cause option.Option[Cause], row AgentCapture) (err error) {
	key := "capture|" + inst.unique(at)
	return inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
		if cause.Has {
			v := cause.Val
			v.Id = id
			b.AddCause(v)
		}
		row.Id, row.Kind = id, "agentCapture"
		b.AddAgentCapture(row)
	})
}

// AgentDisclosure buffers one view's row (ADR-0287 §SD6).
func (inst *Recorder) AgentDisclosure(at time.Time, c Context, cause option.Option[Cause], row AgentDisclosure) (err error) {
	key := "disclosure|" + inst.unique(at)
	return inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
		if cause.Has {
			v := cause.Val
			v.Id = id
			b.AddCause(v)
		}
		row.Id, row.Kind = id, "agentDisclosure"
		b.AddAgentDisclosure(row)
	})
}

// HttpFetch buffers one egress fetch's row.
func (inst *Recorder) HttpFetch(at time.Time, c Context, row HttpFetch) (err error) {
	key := "fetch|" + inst.unique(at)
	return inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "httpFetch"
		b.AddHttpFetch(row)
	})
}

// AdhocDataset buffers one bundle operation's row (ADR-0288
// §SD5); cause names the model call whose reply asked for the operation,
// when the dispatcher recorded one.
func (inst *Recorder) AdhocDataset(at time.Time, c Context, cause option.Option[Cause], row AdhocDataset) (err error) {
	key := "adhoc|" + inst.unique(at)
	return inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
		if cause.Has {
			v := cause.Val
			v.Id = id
			b.AddCause(v)
		}
		row.Id, row.Kind = id, "adhocDataset"
		b.AddAdhocDataset(row)
	})
}

// Event buffers one audit event (ADR-0296 §SD2), the one verb whose row
// carries the call identity on ctx: the origin from its vouched part when
// one is there, else from c, else the run alone; the principal and the
// purpose from its claims, replacing whatever row carried. With no
// principal the row's PrincipalBy becomes none. A row outside the bounds
// of [AuditEvent.Validate] is replaced by an audit-invalid row naming the
// attempted domain and action, and logged; the call does not fail for it.
// The row's natural key names the domain, the action and the subject.
func (inst *Recorder) Event(ctx context.Context, at time.Time, c Context, row AuditEvent) (err error) {
	if inst == nil {
		return nil
	}
	if ci, ok := callident.CallIdentityFrom(ctx); ok {
		if ci.Origin != (callident.Origin{}) {
			c.Origin = Origin{Run: ci.Origin.Run, App: ci.Origin.App, Instance: ci.Origin.Instance}
		}
		row.Principal = option.None[string]()
		if ci.Claims.Principal != "" {
			row.Principal = option.Some(ci.Claims.Principal)
		}
		row.Purpose = option.None[string]()
		if ci.Claims.Purpose != "" {
			row.Purpose = option.Some(ci.Claims.Purpose)
		}
	} else {
		row.Principal = option.None[string]()
		row.Purpose = option.None[string]()
	}
	if !row.Principal.Has {
		row.PrincipalBy = PrincipalByNone
	}
	if verr := row.Validate(); verr != nil {
		inst.log.Error().Err(verr).Str("domain", bounded(row.Domain)).Str("action", bounded(row.Action)).Msg("trail: an audit event is outside its bounds; an audit-invalid row is written in its place")
		row = inst.invalidEvent(row, verr)
		inst.invalid.Add(1)
	}
	return inst.writeEvent(ctx, at, c, row)
}

// writeEvent buffers an audit event row and, when a forwarder is
// attached, keeps its entity until the row is durable.
func (inst *Recorder) writeEvent(ctx context.Context, at time.Time, c Context, row AuditEvent) (err error) {
	key := "event|" + row.Domain + "|" + row.Action + "|" + strconv.FormatUint(row.Subject, 10) + "|" + inst.unique(at)
	return inst.write(ctx, key, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "auditEvent"
		b.AddAuditEvent(row)
		if inst.fwd != nil {
			ent := &TrailEntity{ID: id, Ts: at.UTC(), TrailEnvelope: TrailEnvelope{NaturalKey: []byte(key)}, AuditEvent: option.Some(row)}
			o := c.Origin
			o.Id = id
			if o.Run == "" {
				o.Run = inst.run
			}
			ent.Origin = option.Some(o)
			if c.Conversation.Has {
				v := c.Conversation.Val
				v.Id = id
				ent.Conversation = option.Some(v)
			}
			if c.Delegation.Has {
				v := c.Delegation.Val
				v.Id = id
				ent.Delegation = option.Some(v)
			}
			inst.keepForForward(ent)
		}
	})
}

// invalidEvent is the row written in place of one that failed validation
// (ADR-0296 §SD4): the recorder's own domain, naming what was attempted
// and why it was refused, keeping the subject and whatever principal the
// context vouched for. Built from bounded values, so it needs no check.
func (inst *Recorder) invalidEvent(row AuditEvent, verr error) (out AuditEvent) {
	out = AuditEvent{
		Domain: TrailDomain, Action: ActionAuditInvalid, Outcome: OutcomeFailed,
		PrincipalBy: PrincipalByNone, Subject: row.Subject, Retention: RetentionTrail,
		AttrKeys:   []string{"domain", "action", "reason"},
		AttrValues: []string{bounded(row.Domain), bounded(row.Action), bounded(verr.Error())},
	}
	if row.Principal.Has && checkValue("principal", row.Principal.Val) == nil {
		out.Principal = row.Principal
		switch row.PrincipalBy {
		case PrincipalBySystem, PrincipalByEnv, PrincipalByOs:
			out.PrincipalBy = row.PrincipalBy
		default:
			out.PrincipalBy = PrincipalByEnv
		}
	}
	return
}

// bounded is s as an attribute value: valid UTF-8, non-empty, under the
// bound.
func bounded(s string) (out string) {
	out = strings.ToValidUTF8(s, "\uFFFD")
	if out == "" {
		return "-"
	}
	if utf8.RuneCountInString(out) > MaxValueRunes {
		r := []rune(out)
		out = string(r[:MaxValueRunes])
	}
	return
}

// unique is a key part no other row of this run shares.
func (inst *Recorder) unique(at time.Time) (s string) {
	if inst == nil {
		return ""
	}
	inst.mu.Lock()
	inst.seq++
	n := inst.seq
	inst.mu.Unlock()
	return inst.run + "|" + strconv.FormatInt(at.UnixNano(), 36) + "|" + strconv.FormatUint(n, 36)
}

// Flush lands what is buffered, and what earlier flushes could not. It
// returns nil only when every row buffered before the call is on the
// server; on failure the rows are held for the next flush.
func (inst *Recorder) Flush(ctx context.Context) (err error) {
	if inst == nil {
		return nil
	}
	inst.flushMu.Lock()
	defer inst.flushMu.Unlock()
	inst.mu.Lock()
	if inst.cur == nil {
		inst.mu.Unlock()
		return nil
	}
	inst.swapLocked()
	inst.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()
	for len(inst.backlog) > 0 {
		b := inst.backlog[0]
		if _, ferr := b.store.Flush(ctx); ferr != nil {
			err = eh.Errorf("trail: flush: %w", ferr)
			break
		}
		b.store.Close()
		inst.written.Add(uint64(b.rows))
		inst.backlog = inst.backlog[1:]
	}
	inst.capBacklog()
	if err == nil && inst.gap.rows > 0 {
		inst.writeGap()
	}
	return
}

// swapLocked moves a non-empty current store onto the backlog and opens a
// fresh one. The caller holds mu.
func (inst *Recorder) swapLocked() {
	if n := inst.cur.Buffered(); n > 0 {
		inst.backlog = append(inst.backlog, heldBatch{store: inst.cur, rows: n, first: inst.curFirst, last: inst.curLast})
		inst.cur = inst.newStore()
		inst.curFirst, inst.curLast = time.Time{}, time.Time{}
	}
}

// writeGap writes the audit-gap row for what the cap dropped (ADR-0296
// §SD4), now that the server takes rows again: the count and the window
// of the lost rows' timestamps, in the recorder's own domain. It rides the
// next flush. The caller holds flushMu.
func (inst *Recorder) writeGap() {
	g := inst.gap
	inst.gap = gap{}
	row := AuditEvent{
		Domain: TrailDomain, Action: ActionAuditGap, Outcome: OutcomeFailed,
		PrincipalBy: PrincipalByNone, Retention: RetentionTrail,
		AttrKeys:   []string{"rows", "from", "to"},
		AttrValues: []string{strconv.Itoa(g.rows), g.first.Format(time.RFC3339Nano), g.last.Format(time.RFC3339Nano)},
	}
	if err := inst.writeEvent(context.Background(), time.Now(), Context{}, row); err != nil {
		inst.log.Error().Err(err).Int("rows", g.rows).Msg("trail: the audit-gap row could not be buffered")
		return
	}
	inst.FlushSoon()
}

// capBacklog drops the oldest held batches past maxBacklog rows. The caller
// holds flushMu.
func (inst *Recorder) capBacklog() {
	var held int
	for _, b := range inst.backlog {
		held += b.rows
	}
	var dropped int
	for held > inst.maxBacklog && len(inst.backlog) > 1 {
		b := inst.backlog[0]
		b.store.Close()
		inst.backlog = inst.backlog[1:]
		held -= b.rows
		dropped += b.rows
		inst.gap.add(b)
	}
	if dropped > 0 {
		inst.dropped.Add(uint64(dropped))
		inst.log.Error().Int("dropped", dropped).Int("held", held).Msg("trail: the server has not taken the trail's rows; the oldest are dropped")
	}
}

// WriteAhead makes what is buffered durable before work leaves the machine
// (ADR-0277 §SD3). durable says it landed. refuse is set when it did not
// and BOXER_TRAIL_REQUIRED says such work must not proceed; otherwise the
// gap is logged and the work goes ahead.
func (inst *Recorder) WriteAhead(ctx context.Context) (durable bool, refuse error) {
	var why string
	switch {
	case !inst.Durable():
		why = "this host has no durable backend for boxer.facts"
	default:
		if err := inst.Flush(ctx); err != nil {
			why = err.Error()
		} else {
			return true, nil
		}
	}
	if inst != nil && inst.required {
		return false, eb.Build().Str("env", RequiredEnv.Spec().Name).Str("why", why).Errorf("the audit trail could not be written and %s is set: %s", RequiredEnv.Spec().Name, why) //boxer:lint disable=CS013 reason="shape 1 and 3: the services send this refusal across the bus as text, and the variable's name is what the reader changes"
	}
	if inst != nil && inst.Durable() {
		inst.log.Warn().Str("reason", why).Msg("trail: write-ahead failed; the work proceeds without a durable row")
	}
	return false, nil
}

// Admit says whether work that leaves the machine without a write-ahead —
// an egress fetch, whose rows are flushed behind it — may proceed: it may
// not where BOXER_TRAIL_REQUIRED is set and the host has no durable backend.
func (inst *Recorder) Admit() (refuse error) {
	if inst != nil && inst.required && !inst.Durable() {
		return eb.Build().Str("env", RequiredEnv.Spec().Name).Errorf("the audit trail is not durable on this host and %s is set", RequiredEnv.Spec().Name) //boxer:lint disable=CS013 reason="shape 1 and 3: the services send this refusal across the bus as text, and the variable's name is what the reader changes"
	}
	return nil
}

// FlushSoon wakes the background flusher: for rows nothing waits on.
func (inst *Recorder) FlushSoon() {
	if inst == nil {
		return
	}
	select {
	case inst.wake <- struct{}{}:
	default:
	}
}

// flusher lands buffered rows once per wake-up and at Close.
func (inst *Recorder) flusher() {
	defer close(inst.done)
	flush := func() {
		if err := inst.Flush(context.Background()); err != nil {
			inst.log.Warn().Err(err).Msg("trail: flush (rows stay buffered for the next flush)")
		}
	}
	for {
		select {
		case <-inst.wake:
			flush()
		case <-inst.stop:
			flush()
			return
		}
	}
}

// Scan reads rows through one of the store's scans — pick it in scan, e.g.
// st.ScanLlmMessage(ctx, opts) — and returns the entities, each with every
// component its row carries. Nil without a store. Buffered rows are not
// seen until a flush; a scan does not hold up writers.
func (inst *Recorder) Scan(scan func(st *TrailStore) iter.Seq2[*TrailEntity, error]) (ents []*TrailEntity, err error) {
	if inst == nil {
		return nil, nil
	}
	inst.readMu.Lock()
	defer inst.readMu.Unlock()
	if inst.read == nil {
		return nil, nil
	}
	for ent, serr := range scan(inst.read) {
		if serr != nil {
			return nil, eh.Errorf("trail: scan: %w", serr)
		}
		if ent != nil {
			ents = append(ents, ent)
		}
	}
	return
}

// ContentDigest is the digest of a message's content, a tool catalog or a
// plan (ADR-0277 §SD2): 128 bits of BLAKE3 over the exact bytes, hex. It
// answers "the same text" across rows without keeping the text; it is
// identity for correlation, and anyone holding a candidate text can test
// for it.
func ContentDigest(s string) (d string) {
	sum := blake3.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

// ToolKey is a coordinator's key for the index-th tool call of a model
// call's reply (ADR-0277 §SD6). The provider's own id for the tool call is
// recorded but is never a key: nothing obliges a provider to mint ids that
// differ across replies.
func ToolKey(modelCall string, index int) (key string) {
	return modelCall + "#" + strconv.Itoa(index)
}
