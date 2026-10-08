package trail

import (
	"context"
	"encoding/hex"
	"iter"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/zeebo/xxh3"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
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

// Recorder is the one writer of the trail: it stamps the origin, composes
// each row's natural key and components, and owns the buffer and its
// flushes. It is safe for concurrent use. A nil Recorder, and one built
// without an executor, record nothing and report not durable.
type Recorder struct {
	run      string
	required bool
	log      zerolog.Logger

	mu    sync.Mutex
	store *TrailStore
	seq   uint64

	wake chan struct{}
	stop chan struct{}
	done chan struct{}
}

// NewRecorder builds the recorder of the run named run. exec reaches the
// server holding boxer.facts; nil is a host without a durable backend.
func NewRecorder(exec recordstore.ExecutorI, run string, log zerolog.Logger) (inst *Recorder) {
	inst = &Recorder{run: run, required: RequiredEnv.Get(), log: log,
		wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	if exec != nil {
		inst.store = NewTrailStore(exec, nil, TrailStoreConfig{})
	}
	go inst.flusher()
	return
}

// Close flushes what is buffered and releases the store.
func (inst *Recorder) Close() {
	if inst == nil {
		return
	}
	close(inst.stop)
	<-inst.done
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.store != nil {
		inst.store.Close()
		inst.store = nil
	}
}

// Durable says rows land on boxer.facts.
func (inst *Recorder) Durable() (durable bool) {
	if inst == nil {
		return false
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.store != nil
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

// begin opens the row keyed key with its context components. The caller
// holds mu and has checked the store.
func (inst *Recorder) begin(key string, at time.Time, c Context) (b *TrailEntityBuilder, id uint64) {
	id = xxh3.HashString(key)
	b = inst.store.Begin(id, at.UTC(), TrailEnvelope{NaturalKey: []byte(key)})
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

// write runs add under the lock; without a store it does nothing.
func (inst *Recorder) write(key string, at time.Time, c Context, add func(b *TrailEntityBuilder, id uint64)) (err error) {
	if inst == nil {
		return nil
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.store == nil {
		return nil
	}
	b, id := inst.begin(key, at, c)
	add(b, id)
	if err = b.Commit(); err != nil {
		err = eh.Errorf("trail: buffer row %q: %w", key, err)
	}
	return
}

// LlmCall buffers a model call's row; its natural key is the call id.
func (inst *Recorder) LlmCall(at time.Time, c Context, row LlmCall) (err error) {
	return inst.write(row.CallId, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "llmCall"
		b.AddLlmCall(row)
	})
}

// LlmMessage buffers one message's row, with its text when body is set;
// its natural key is the call id and the ordinal.
func (inst *Recorder) LlmMessage(at time.Time, c Context, row LlmMessage, body option.Option[LlmMessageBody]) (err error) {
	key := row.CallId + "/" + strconv.FormatUint(uint64(row.Ordinal), 10)
	return inst.write(key, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "llmMessage"
		b.AddLlmMessage(row)
		if body.Has {
			v := body.Val
			v.Id = id
			b.AddLlmMessageBody(v)
		}
	})
}

// AgentAction buffers one action-record row. Its natural key is task, key,
// decision and time, so the dispatcher's row and the final row of one call
// are two rows.
func (inst *Recorder) AgentAction(at time.Time, c Context, cause option.Option[Cause], row AgentAction) (err error) {
	key := c.Delegation.Val.Task + "|" + row.Key + "|" + row.Decision + "|" + strconv.FormatInt(at.UnixNano(), 10)
	return inst.write(key, at, c, func(b *TrailEntityBuilder, id uint64) {
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
	return inst.write(key, at, c, func(b *TrailEntityBuilder, id uint64) {
		if cause.Has {
			v := cause.Val
			v.Id = id
			b.AddCause(v)
		}
		row.Id, row.Kind = id, "agentGrant"
		b.AddAgentGrant(row)
	})
}

// AgentCapture buffers one capture's row.
func (inst *Recorder) AgentCapture(at time.Time, c Context, row AgentCapture) (err error) {
	key := "capture|" + inst.unique(at)
	return inst.write(key, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "agentCapture"
		b.AddAgentCapture(row)
	})
}

// AgentDisclosure buffers one view's row (ADR-0287 §SD6).
func (inst *Recorder) AgentDisclosure(at time.Time, c Context, cause option.Option[Cause], row AgentDisclosure) (err error) {
	key := "disclosure|" + inst.unique(at)
	return inst.write(key, at, c, func(b *TrailEntityBuilder, id uint64) {
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
	return inst.write(key, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "httpFetch"
		b.AddHttpFetch(row)
	})
}

// AdhocDataset buffers one bundle operation's row (ADR-0288
// §SD5); cause names the model call whose reply asked for the operation,
// when the dispatcher recorded one.
func (inst *Recorder) AdhocDataset(at time.Time, c Context, cause option.Option[Cause], row AdhocDataset) (err error) {
	key := "adhoc|" + inst.unique(at)
	return inst.write(key, at, c, func(b *TrailEntityBuilder, id uint64) {
		if cause.Has {
			v := cause.Val
			v.Id = id
			b.AddCause(v)
		}
		row.Id, row.Kind = id, "adhocDataset"
		b.AddAdhocDataset(row)
	})
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

// Flush lands what is buffered. On failure the rows stay buffered and the
// next flush ships them.
func (inst *Recorder) Flush(ctx context.Context) (err error) {
	if inst == nil {
		return nil
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.store == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()
	if _, err = inst.store.Flush(ctx); err != nil {
		err = eh.Errorf("trail: flush: %w", err)
	}
	return
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
		return false, eh.Errorf("the audit trail could not be written and %s is set: %s", RequiredEnv.Spec().Name, why)
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
		return eh.Errorf("the audit trail is not durable on this host and %s is set", RequiredEnv.Spec().Name)
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
// seen until a flush.
func (inst *Recorder) Scan(scan func(st *TrailStore) iter.Seq2[*TrailEntity, error]) (ents []*TrailEntity, err error) {
	if inst == nil {
		return nil, nil
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.store == nil {
		return nil, nil
	}
	for ent, serr := range scan(inst.store) {
		if serr != nil {
			return nil, eh.Errorf("trail: scan: %w", serr)
		}
		if ent != nil {
			ents = append(ents, ent)
		}
	}
	return
}

// LlmCalls reads the durable call rows since a point in time, oldest first,
// up to limit; nil without a store.
func (inst *Recorder) LlmCalls(ctx context.Context, since time.Time, limit int) (ents []*TrailEntity, err error) {
	opts := recordstore.ScanOpts{
		ExtraPredicate: TrailColOrder + " >= fromUnixTimestamp64Nano(" + strconv.FormatInt(since.UTC().UnixNano(), 10) + ")",
		Limit:          limit,
	}
	return inst.Scan(func(st *TrailStore) iter.Seq2[*TrailEntity, error] { return st.ScanLlmCall(ctx, opts) })
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
