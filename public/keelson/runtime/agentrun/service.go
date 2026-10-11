package agentrun

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Service answers `runtime.agentrun.*` over the store (ADR-0308 §SD10):
// every call is scoped to Msg.Sender, so an app creates runs it owns and
// reaches only those. The service persists; the writer is the app's. The
// service holds the journal's invariants against a client that has them
// wrong (§SD9): entries append in chain order to an open run, a run row
// keeps its identity and seed, a deleted run is never written again.
type Service struct {
	store     StoreI
	busClient *inprocbus.Client
	unsub     func()
	closeOnce sync.Once
	log       zerolog.Logger
	// Timeout bounds one store call.
	Timeout time.Duration
	// locks serialise the check-then-write verbs per run: the bus runs
	// handlers on the publisher's goroutine, so two writers on one run
	// would otherwise race through the invariant checks.
	locksMu sync.Mutex
	locks   map[string]*sync.Mutex
}

// DefaultTimeout bounds a store call behind the service and a client's
// wait.
const DefaultTimeout = 30 * time.Second

// NewService subscribes a service over store. The caller closes it.
func NewService(bus *inprocbus.Inst, log zerolog.Logger, store StoreI) (s *Service, err error) {
	if bus == nil || store == nil {
		err = eh.Errorf("nil bus or store")
		return
	}
	s = &Service{store: store, log: log.With().Str("app", string(ServiceAppId)).Logger(), Timeout: DefaultTimeout, locks: make(map[string]*sync.Mutex)}
	s.busClient = bus.NewClient(ServiceAppId, ServiceCaps())
	if s.unsub, err = s.busClient.Subscribe(SubjectAll, s.handle); err != nil {
		_ = s.busClient.Close()
		err = eh.Errorf("subscribe: %w", err)
		return nil, err
	}
	return
}

// Close releases the subscription and the bus client; a handler still
// running finds the client closed, not gone.
func (inst *Service) Close() {
	inst.closeOnce.Do(func() {
		if inst.unsub != nil {
			inst.unsub()
		}
		_ = inst.busClient.Close()
	})
}

func (inst *Service) handle(msg *app.Msg) {
	if msg.Reply == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), inst.Timeout)
	defer cancel()
	owner := string(msg.Sender)
	switch msg.Subject {
	case SubjectCreate:
		req, err := decode[wireRunRequest](msg.Payload)
		if err != nil {
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: err.Error()})
			return
		}
		r := runOfWire(req.Run)
		// The owner is the sender, whatever the request says, and the
		// seed digest follows the row as stored.
		r.OwnerApp = owner
		r.Digest = RunDigestSeed(r)
		r.Head, r.LastOrdinal = r.Digest, 0
		if r.State == StateUnspecified {
			r.State = StateOpen
		}
		if r.State == StateDeleted {
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: "a run is not created deleted"})
			return
		}
		r.Version = 1
		inst.ack(msg.Reply, inst.store.CreateRun(ctx, r))
	case SubjectUpdate:
		req, err := decode[wireRunRequest](msg.Payload)
		if err != nil {
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: err.Error()})
			return
		}
		r := runOfWire(req.Run)
		unlock := inst.lockRun(r.Id)
		was, why := inst.owned(ctx, r.Id, owner)
		if why == "" {
			r, why = inst.updated(ctx, was, r)
		}
		if why != "" {
			unlock()
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: why})
			return
		}
		err = inst.store.UpdateRun(ctx, r)
		unlock()
		inst.ack(msg.Reply, err)
	case SubjectGet:
		req, err := decode[wireIdRequest](msg.Payload)
		if err != nil {
			inst.reply(msg.Reply, wireRunReply{wireAck: wireAck{V: wireVersion, Reason: err.Error()}})
			return
		}
		r, found, err := inst.store.GetRun(ctx, req.Id)
		if err != nil {
			inst.reply(msg.Reply, wireRunReply{wireAck: wireAck{V: wireVersion, Reason: err.Error()}})
			return
		}
		// Another app's run is not found, not refused: it is not the
		// sender's to know of.
		if found && r.OwnerApp != owner {
			found, r = false, Run{}
		}
		inst.reply(msg.Reply, wireRunReply{wireAck: wireAck{V: wireVersion, Ok: true}, Found: found, Run: wireOfRun(r)})
	case SubjectList:
		req, err := decode[wireIdRequest](msg.Payload)
		if err != nil {
			inst.reply(msg.Reply, wireRunReply{wireAck: wireAck{V: wireVersion, Reason: err.Error()}})
			return
		}
		runs, err := inst.store.ListRuns(ctx, owner, req.Limit)
		if err != nil {
			inst.reply(msg.Reply, wireRunReply{wireAck: wireAck{V: wireVersion, Reason: err.Error()}})
			return
		}
		rep := wireRunReply{wireAck: wireAck{V: wireVersion, Ok: true}}
		for _, r := range runs {
			rep.Runs = append(rep.Runs, wireOfRun(r))
		}
		inst.reply(msg.Reply, rep)
	case SubjectAppend:
		req, err := decode[wireAppendRequest](msg.Payload)
		if err != nil {
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: err.Error()})
			return
		}
		entries := make([]Entry, 0, len(req.Entries))
		for _, w := range req.Entries {
			entries = append(entries, entryOfWire(w))
		}
		if len(entries) == 0 {
			inst.ack(msg.Reply, nil)
			return
		}
		unlock := inst.lockRun(entries[0].Run)
		if why := inst.appendable(ctx, entries, owner); why != "" {
			unlock()
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: why})
			return
		}
		err = inst.store.Append(ctx, entries)
		unlock()
		inst.ack(msg.Reply, err)
	case SubjectEntries:
		req, err := decode[wireIdRequest](msg.Payload)
		if err != nil {
			inst.reply(msg.Reply, wireEntriesReply{wireAck: wireAck{V: wireVersion, Reason: err.Error()}})
			return
		}
		if _, why := inst.owned(ctx, req.Id, owner); why != "" {
			inst.reply(msg.Reply, wireEntriesReply{wireAck: wireAck{V: wireVersion, Reason: why}})
			return
		}
		entries, err := inst.store.Entries(ctx, req.Id, req.From)
		if err != nil {
			inst.reply(msg.Reply, wireEntriesReply{wireAck: wireAck{V: wireVersion, Reason: err.Error()}})
			return
		}
		rep := wireEntriesReply{wireAck: wireAck{V: wireVersion, Ok: true}}
		for _, e := range entries {
			rep.Entries = append(rep.Entries, wireOfEntry(e))
		}
		inst.reply(msg.Reply, rep)
	case SubjectTombstone:
		req, err := decode[wireIdRequest](msg.Payload)
		if err != nil {
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: err.Error()})
			return
		}
		unlock := inst.lockRun(req.Id)
		r, why := inst.owned(ctx, req.Id, owner)
		if why == "" && !inst.headIs(ctx, r, KindDeleted) {
			why = "a tombstone follows the deleted entry"
		}
		if why != "" {
			unlock()
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: why})
			return
		}
		err = inst.store.Tombstone(ctx, req.Id)
		unlock()
		inst.ack(msg.Reply, err)
	case SubjectErase:
		req, err := decode[wireEraseRequest](msg.Payload)
		if err != nil {
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: err.Error()})
			return
		}
		eraser, ok := inst.store.(EraserI)
		if !ok {
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: "the store does not erase"})
			return
		}
		unlock := inst.lockRun(req.Id)
		r, why := inst.owned(ctx, req.Id, owner)
		if why == "" {
			why = inst.erasable(ctx, r, req.Ordinals)
		}
		if why != "" {
			unlock()
			inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: why})
			return
		}
		err = eraser.Erase(ctx, req.Id, req.Ordinals)
		unlock()
		inst.ack(msg.Reply, err)
	default:
		inst.reply(msg.Reply, wireAck{V: wireVersion, Reason: "no such verb"})
	}
}

// lockRun takes the run's lock and returns its release.
func (inst *Service) lockRun(id string) (unlock func()) {
	inst.locksMu.Lock()
	l, ok := inst.locks[id]
	if !ok {
		l = &sync.Mutex{}
		inst.locks[id] = l
	}
	inst.locksMu.Unlock()
	l.Lock()
	return l.Unlock
}

// owned is the run row when the sender may touch it, else why not: it
// does not exist or is another app's.
func (inst *Service) owned(ctx context.Context, id string, owner string) (r Run, why string) {
	r, found, err := inst.store.GetRun(ctx, id)
	switch {
	case err != nil:
		return r, err.Error()
	case !found, r.OwnerApp != owner:
		return r, "no such run"
	}
	return
}

// updated is the run row an update leaves, from the stored row and the
// request: identity, seed, head and last ordinal are the store's, the
// label can only rise, a deleted run is not written again, the state
// moves only along open ⇄ closed, open → stranded, and any of those →
// deleted behind a deleted entry, and the request is the next version
// over the stored row: a writer whose row another writer moved since is
// refused, so a stale close never takes a run that was reopened.
func (inst *Service) updated(ctx context.Context, was Run, req Run) (r Run, why string) {
	if was.State == StateDeleted {
		return r, "the run is deleted"
	}
	if req.Version != was.Version+1 {
		return r, eb.Build().Uint32("version", req.Version).Uint32("stored", was.Version).Errorf("the run row moved since the writer read it").Error()
	}
	switch {
	case req.State == was.State:
	case req.State == StateDeleted:
		if !inst.headIs(ctx, was, KindDeleted) {
			return r, "a deleted run ends with its deleted entry"
		}
	case was.State == StateOpen && (req.State == StateClosed || req.State == StateStranded):
	case was.State == StateClosed && req.State == StateOpen:
	default:
		return r, "the run does not move from " + was.State.String() + " to " + req.State.String()
	}
	r = req
	r.Id, r.Cell, r.Conversation, r.OwnerApp, r.Coordinator, r.Created = was.Id, was.Cell, was.Conversation, was.OwnerApp, was.Coordinator, was.Created
	r.Digest, r.Head, r.LastOrdinal = was.Digest, was.Head, was.LastOrdinal
	r.Label = was.Label.Join(req.Label)
	r.Version = was.Version + 1
	return
}

// headIs says the run's last entry is of kind; the read is bounded to
// the last ordinal.
func (inst *Service) headIs(ctx context.Context, r Run, kind KindE) bool {
	if r.LastOrdinal == 0 {
		return false
	}
	entries, err := inst.store.Entries(ctx, r.Id, r.LastOrdinal)
	if err != nil || len(entries) == 0 {
		return false
	}
	return entries[len(entries)-1].Kind == kind
}

// erasable is why the ordinals may not be erased, empty when they may:
// the chain must hold an erased entry naming each, so the erasure is on
// record before the content goes (ADR-0320 §SD4), and none may be a head
// entry.
func (inst *Service) erasable(ctx context.Context, r Run, ordinals []uint32) (why string) {
	if len(ordinals) == 0 {
		return "nothing to erase"
	}
	entries, err := inst.store.Entries(ctx, r.Id, 0)
	if err != nil {
		return err.Error()
	}
	named := make(map[uint32]bool)
	for _, e := range entries {
		if e.Kind == KindErased {
			named[e.Target] = true
		}
	}
	for _, ord := range ordinals {
		switch {
		case ord == 0 || ord > uint32(len(entries)):
			return eb.Build().Uint32("ordinal", ord).Errorf("no such entry").Error()
		case entries[ord-1].Kind == KindHead:
			return eb.Build().Uint32("ordinal", ord).Errorf("a head entry is not content").Error()
		case !named[ord]:
			return eb.Build().Uint32("ordinal", ord).Errorf("an erasure follows the erased entry naming it").Error()
		}
	}
	return
}

// appendable is why the batch may not be appended, empty when it may: one
// run per batch, the sender's, open (or taking its deleted entry), the
// ordinals contiguous from the row's last, each entry chained to the one
// before from the row's head, of a known kind, aimed right, and with its
// digests matching its bodies.
func (inst *Service) appendable(ctx context.Context, entries []Entry, owner string) (why string) {
	if len(entries) == 0 {
		return
	}
	id := entries[0].Run
	r, why := inst.owned(ctx, id, owner)
	if why != "" {
		return
	}
	if r.State == StateDeleted || inst.headIs(ctx, r, KindDeleted) {
		return "the run is deleted"
	}
	prevOrd, prevDigest := r.LastOrdinal, r.Head
	if prevDigest == "" {
		prevDigest = r.Digest
	}
	for _, e := range entries {
		switch {
		case e.Run != id:
			return "one run per append"
		case e.Ordinal != prevOrd+1 || e.Prev != prevOrd:
			return eb.Build().Uint32("ordinal", e.Ordinal).Uint32("last", prevOrd).Errorf("the entry does not follow the run's last").Error()
		case r.State != StateOpen && e.Kind != KindDeleted:
			return "the run is " + r.State.String()
		case !e.BodiesMatch() || (e.Body != "" && e.BodyDigest == "") || (e.Calls != "" && e.CallsDigest == ""):
			return "the entry's digests are not its bodies'"
		}
		if err := checkEntry(e, e.Ordinal); err != nil {
			return err.Error()
		}
		form, err := e.CanonicalForm()
		if err != nil {
			return err.Error()
		}
		if e.Digest != ChainDigest(prevDigest, form) {
			return eb.Build().Uint32("ordinal", e.Ordinal).Errorf("the entry is not chained to the run's head").Error()
		}
		prevOrd, prevDigest = e.Ordinal, e.Digest
	}
	return
}

func (inst *Service) ack(subject string, err error) {
	if err != nil {
		inst.reply(subject, wireAck{V: wireVersion, Reason: err.Error()})
		return
	}
	inst.reply(subject, wireAck{V: wireVersion, Ok: true})
}

func (inst *Service) reply(subject string, v any) {
	if err := buscodec.Reply(inst.busClient.Publish, subject, v); err != nil {
		inst.log.Warn().Err(err).Str("subject", subject).Msg("reply failed")
	}
}
