package trail

import (
	"context"
	"encoding/hex"
	"iter"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/zeebo/xxh3"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec"
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

// MaxBacklogRows bounds the rows the recorder holds for a server that does
// not take them. Past it the oldest held batches are dropped, and logged:
// a host whose trail is down for hours must not grow without bound, and
// what is dropped is what BOXER_TRAIL_REQUIRED exists to refuse.
const MaxBacklogRows = auditrec.DefaultMaxBacklogRows

// Recorder is the one writer of the trail: it stamps the run and composes
// each row's natural key and components, and writes them through the
// shared writer of audit events, auditrec, over the trail store's binding
// (ADR-0330 §SD8), which owns the buffer, its flushes, the backlog and the
// gap row, the seals and their anchoring, write-ahead, the reads of audit
// events and forwarding. App and window come from the caller: every writer
// takes them from the bus envelope's sender ([Recorder.Origin], or
// [Recorder.OriginOf] over the sender it kept), never from a payload. It
// is safe for concurrent use. A nil Recorder, and one built without an
// executor, record nothing and report not durable.
//
// The recorder flushes when asked — Flush, FlushSoon, a write-ahead — and
// at Close, and on an interval only WithFlushInterval. After Close a verb
// returns auditrec.ErrClosed.
type Recorder struct {
	run      string
	required bool
	log      zerolog.Logger
	cfg      recorderConfig
	// notDurable is why the recorder was built without a durable store,
	// "" when it has one.
	notDurable string

	// rec is the shared writer, nil when it could not be built; st the
	// binding it writes through, nil without an executor.
	rec *auditrec.Recorder[*auditBatch]
	st  *auditStore
	// seq makes the unique part of a natural key.
	seq atomic.Uint64
}

// recorderConfig is what the options set before the writer is built.
type recorderConfig struct {
	flushInterval time.Duration
	observer      recordstore.WriteObserverI[uint64, time.Time]
	readTable     string
	forwarder     audit.ForwarderI
	originBy      evidence.OriginByE
	anchors       anchor.StoreI
	maxBacklog    int
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
	// Seals is the durable flushes, each sealed (ADR-0327 §SD5); a seal is
	// not among the rows Written counts.
	Seals uint64
}

// Counts reads the counters. Zero for a nil recorder.
func (inst *Recorder) Counts() (c Counts) {
	if inst == nil {
		return
	}
	a := inst.rec.Counts()
	return Counts{Buffered: a.Buffered, Written: a.Written, Dropped: a.Dropped, Invalid: a.Invalid,
		Forwarded: a.Forwarded, ForwardDropped: a.ForwardDropped, Seals: a.Seals}
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
	return func(inst *Recorder) { inst.cfg.observer = o }
}

// WithFlushInterval makes the recorder also flush on its own every d,
// beside Flush, FlushSoon, a write-ahead and Close: for rows nothing asks
// to land, such as the bus requests AsAuditSink records. Zero, the
// default, flushes only when asked.
func WithFlushInterval(d time.Duration) RecorderOption {
	return func(inst *Recorder) { inst.cfg.flushInterval = d }
}

// WithReadTable names the table the recorder's reads scan (ADR-0296
// §SD9 d): a storage table, or a Merge over several, while writes still go
// to the facts table. Empty reads the table written to.
func WithReadTable(table string) RecorderOption {
	return func(inst *Recorder) { inst.cfg.readTable = table }
}

// WithOriginBy says how an audit event's origin is established when the
// context carries no attestation (ADR-0327 §SD6): the host's own recorder
// writes as host; the default, self, is a process stating its own origin.
// A context attested through evidence.WithAttestation overrides it, as the
// audit subject's handler does for an app's row.
func WithOriginBy(o evidence.OriginByE) RecorderOption {
	return func(inst *Recorder) { inst.cfg.originBy = o }
}

// WithForwarder attaches f (ADR-0296 §SD6). The prompt path hands f each
// durable batch's audit events, on a goroutine of the recorder's own,
// behind a queue of DefaultForwardQueue batches. The backstop is
// [Recorder.ForwardWindow], which the caller schedules.
func WithForwarder(f audit.ForwarderI) RecorderOption {
	return func(inst *Recorder) { inst.cfg.forwarder = f }
}

// WithAnchors ships each durable seal to the anchor store as the head of
// the run's seal stream (ADR-0327 §SD5, §SD7), best effort: a refused or
// failed append is logged and the seal stays unanchored. The cell is the
// host's own.
func WithAnchors(st anchor.StoreI) RecorderOption {
	return func(inst *Recorder) { inst.cfg.anchors = st }
}

// DefaultForwardQueue bounds the batches the prompt path holds for a
// forwarder that has not taken them yet; past it a batch is dropped from
// the prompt path, counted, and left to the backstop.
const DefaultForwardQueue = auditrec.DefaultForwardQueue

// TrailSealScheme is the scheme the recorder's seals and row digests are
// under.
var TrailSealScheme = evidence.NewScheme("trail-seal/v1")

// SealScope is the anchor scope of a run's seal stream (ADR-0327 §SD7):
// the n-th durable seal is the scope's head at ordinal n.
func SealScope(run string) uint64 { return xxh3.HashString("trail-seal|" + run) }

// NewRecorder builds the recorder of the run named run. exec reaches the
// server holding boxer.facts; nil is a host without a durable backend.
func NewRecorder(exec recordstore.ExecutorI, run string, log zerolog.Logger, opts ...RecorderOption) (inst *Recorder) {
	inst = &Recorder{run: run, required: RequiredEnv.Get(), log: log, cfg: recorderConfig{maxBacklog: MaxBacklogRows}}
	for _, o := range opts {
		o(inst)
	}
	ropts := []auditrec.Option{
		auditrec.WithFlushInterval(inst.cfg.flushInterval),
		auditrec.WithMaxBacklog(inst.cfg.maxBacklog),
		auditrec.WithOriginBy(inst.cfg.originBy),
		auditrec.WithRequired(inst.required),
		auditrec.WithSealScope(SealScope),
	}
	if inst.cfg.observer != nil {
		ropts = append(ropts, auditrec.WithWriteObserver(inst.cfg.observer))
	}
	if inst.cfg.forwarder != nil {
		ropts = append(ropts, auditrec.WithForwarder(inst.cfg.forwarder), auditrec.WithForwardQueue(DefaultForwardQueue))
	}
	if inst.cfg.anchors != nil {
		ropts = append(ropts, auditrec.WithAnchors(inst.cfg.anchors, ""))
	}
	// A nil interface, not a nil *auditStore, is a recorder without a store.
	var st auditrec.StoreI[*auditBatch]
	if exec == nil {
		inst.notDurable = notDurableNoBackend
	} else {
		var err error
		if inst.st, err = newAuditStore(exec, inst.cfg.readTable); err != nil {
			log.Error().Err(err).Msg("trail: the store binding could not be built; the trail records nothing")
			inst.notDurable = "the store binding could not be built: " + err.Error()
			return
		}
		st = inst.st
	}
	rec, err := auditrec.New(st, run, TrailSealScheme, log, ropts...)
	if err != nil {
		log.Error().Err(err).Msg("trail: the recorder could not be built; the trail records nothing")
		if inst.st != nil {
			inst.st.Close()
			inst.st = nil
		}
		why := "the recorder could not be built: " + err.Error()
		if run == "" {
			why = notDurableNoRun
		}
		if inst.notDurable == "" {
			inst.notDurable = why
		} else {
			inst.notDurable += "; " + why
		}
		return
	}
	inst.rec = rec
	return
}

// Why a trail is not durable, as NotDurable says it.
const (
	notDurableNoBackend = "this host has no durable backend for boxer.facts"
	notDurableNoRun     = "the host has no run id, so the trail has no seal stream"
	notDurableClosed    = "the recorder is closed"
	notDurableNil       = "the host has no audit trail"
)

// NotDurable says why rows written through the recorder do not land on
// boxer.facts, "" when they do: no backend (a host without ClickHouse), no
// run id (a host whose run identity did not start), a store that could not
// be built, or a closed recorder. It is the state Durable reports false
// for, with its reason, for a host to show the person and an agent.
func (inst *Recorder) NotDurable() (why string) {
	if inst == nil {
		return notDurableNil
	}
	if inst.notDurable != "" {
		return inst.notDurable
	}
	if !inst.rec.Durable() {
		return notDurableClosed
	}
	return ""
}

// Close flushes what is buffered, within the flush's bound, and releases
// the stores. What the server did not take by then is lost: counted as
// dropped and logged (ADR-0296 §SD4), with no gap row, since nothing runs
// afterwards. It is idempotent.
func (inst *Recorder) Close() {
	if inst == nil {
		return
	}
	inst.rec.Close()
}

// Durable says rows land on boxer.facts.
func (inst *Recorder) Durable() (durable bool) {
	if inst == nil {
		return false
	}
	return inst.rec.Durable()
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

// write buffers one row keyed key with c's components through the shared
// writer, add contributing the row's own; without a store it does
// nothing. The verbs whose identity is a bus envelope pass
// context.Background().
func (inst *Recorder) write(ctx context.Context, key string, at time.Time, c Context, add func(b *TrailEntityBuilder, id uint64)) (err error) {
	if inst == nil {
		return nil
	}
	o := c.Origin
	if o.Run == "" {
		o.Run = inst.run
	}
	_, err = inst.rec.Write(ctx, key, at, func(b *auditBatch, id uint64) ([]byte, error) {
		return b.row(ctx, id, at, key, o, c, func(bld *TrailEntityBuilder) { add(bld, id) })
	})
	if err != nil {
		err = eb.Build().Str("key", key).Errorf("trail: buffer row: %w", err)
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

// LadingPublish buffers one publish's row (ADR-0341 §SD5, proposed).
func (inst *Recorder) LadingPublish(at time.Time, c Context, row LadingPublish) (err error) {
	key := "ladingpublish|" + inst.unique(at)
	return inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "ladingPublish"
		b.AddLadingPublish(row)
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

// Event buffers one audit event (ADR-0296 §SD2) through the shared
// writer's event verb, the one verb whose row carries the call identity on
// ctx, by [audit.Stamp] and [audit.OriginOf]: the run is always this
// recorder's; the app and the instance the call identity's origin when it
// names them, else c's; the principal and the purpose from its claims,
// replacing whatever row carried. With no principal the row's PrincipalBy
// becomes none and its PrincipalVia absent. A row outside the bounds of
// [AuditEvent.Validate] is replaced by an audit-invalid row naming the
// attempted domain and action, and logged; the call does not fail for it.
// The row's natural key names the domain, the action and the subject, and
// carries c's conversation and delegation.
func (inst *Recorder) Event(ctx context.Context, at time.Time, c Context, row AuditEvent) (err error) {
	if inst == nil {
		return nil
	}
	return inst.rec.EventFrom(withRowContext(ctx, c), at, c.Origin, row)
}

// EventWriteAhead is Event, then the row made durable before the caller
// acts (ADR-0327 §SD8, the write-ahead ADR-0296 deferred): durable says it
// landed; refuse is set when it did not and BOXER_TRAIL_REQUIRED says such
// work must not proceed.
func (inst *Recorder) EventWriteAhead(ctx context.Context, at time.Time, c Context, row AuditEvent) (durable bool, refuse error) {
	if inst == nil {
		return false, nil
	}
	if err := inst.Event(ctx, at, c, row); err != nil {
		return false, err
	}
	return inst.WriteAhead(ctx)
}

// unique is a key part no other row of this run shares.
func (inst *Recorder) unique(at time.Time) (s string) {
	if inst == nil {
		return ""
	}
	n := inst.seq.Add(1)
	return inst.run + "|" + strconv.FormatInt(at.UnixNano(), 36) + "|" + strconv.FormatUint(n, 36)
}

// Flush lands what is buffered, and what earlier flushes could not, each
// batch sealed; after a loss it writes the audit-gap row and lands it in
// the same call. It returns nil only when every row buffered before the
// call is on the server; on failure the rows are held for the next flush.
func (inst *Recorder) Flush(ctx context.Context) (err error) {
	if inst == nil {
		return nil
	}
	return inst.rec.Flush(ctx)
}

// WriteAhead makes what is buffered durable before work leaves the machine
// (ADR-0277 §SD3). durable says it landed. refuse is set when it did not
// and BOXER_TRAIL_REQUIRED says such work must not proceed; otherwise the
// gap is logged and the work goes ahead.
func (inst *Recorder) WriteAhead(ctx context.Context) (durable bool, refuse error) {
	if !inst.Durable() {
		if inst != nil && inst.required {
			why := inst.NotDurable()
			return false, eb.Build().Str("env", RequiredEnv.Spec().Name).Str("why", why).Errorf("the audit trail could not be written and %s is set: %s", RequiredEnv.Spec().Name, why) //boxer:lint disable=CS013 reason="shape 1 and 3: the services send this refusal across the bus as text, and the variable's name is what the reader changes"
		}
		return false, nil
	}
	durable, err := inst.rec.WriteAhead(ctx)
	if err != nil {
		return false, eb.Build().Str("env", RequiredEnv.Spec().Name).Errorf("the audit trail could not be written and %s is set: %w", RequiredEnv.Spec().Name, err) //boxer:lint disable=CS013 reason="shape 1 and 3: the services send this refusal across the bus as text, and the variable's name is what the reader changes"
	}
	return durable, nil
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
	inst.rec.FlushSoon()
}

// Scan reads rows through one of the store's scans — pick it in scan, e.g.
// st.ScanLlmMessage(ctx, opts) — and returns the entities, each with every
// component its row carries. Nil without a store or after Close. Buffered
// rows are not seen until a flush; a scan does not hold up writers.
func (inst *Recorder) Scan(scan func(st *TrailStore) iter.Seq2[*TrailEntity, error]) (ents []*TrailEntity, err error) {
	if inst == nil || inst.st == nil {
		return nil, nil
	}
	for ent, serr := range inst.st.scan(scan) {
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
