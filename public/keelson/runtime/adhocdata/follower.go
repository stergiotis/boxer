package adhocdata

import (
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// follower.go keeps a consumer's declared dataset aliases bound to live
// datasets for as long as the consumer lives (ADR-0240 §SD6; the mechanism
// of ADR-0188 §SD3, lifted out of the applet host).
//
// An alias resolves to the newest live dataset published under it.
// Resolving once at open is enough only when the data is already there,
// which puts an ordering on the reader — capture, *then* open — that
// nothing enforces and that a window has no way to recover from once it is
// on the wrong side of it. And a dataset that was there can leave: the
// runtime retracts it when its publisher's window closes, and a query
// naming the alias fails as an unknown table.
//
// So the binding is kept in step with the dataset service. The service's
// events are the fast path, and they are HINTS, not truth: a `published`
// under a pending alias makes the follower ask the service what the alias
// resolves to and bind that; a `published` onto the handle an alias is
// bound to is a republish and notifies the revision; a `retracted` of that
// handle unbinds the alias and leaves it pending. Truth is the request/reply
// the follower makes off the caller's thread — at open (subscribed to the
// events BEFORE it resolves, so no publish can fall between the two), on
// every hint, and on a slow reconcile tick that re-asks for pending aliases
// and verifies bound handles in the same round trip (ResolveVerifyRequest).
// That is what makes the binding converge whatever the bus delivers: the
// in-proc bus loses nothing, but NATS core may drop an event, and an
// at-least-once transport may redeliver one late — a stale hint is
// corrected by the answer, a hint that never came is caught by the tick.
// Where the events cannot be subscribed at all (no cap, no bus), the tick
// runs at the seconds-scale poll interval instead.
//
// Off the caller's thread is not a preference for any of those requests: a
// bus Request waits the full request timeout when nothing serves the
// subject, and adhoc.* is unbound whenever the ad-hoc service failed to
// start.
//
// The caller's thread owns the binding side — [TargetI] and whatever it
// does with a new binding; the bus goroutine and the worker own the
// arriving side. The mutex covers the handoff between them and nothing
// else.

const (
	// DefaultPollInterval bounds the tick rate when events are unavailable
	// and the follower can only poll for pending aliases. Seconds is the
	// right order for a human who just pressed Capture; it is also a bus
	// round-trip per tick per waiting consumer, so it does not want to be a
	// frame.
	DefaultPollInterval = 2 * time.Second
	// DefaultReconcileInterval bounds the tick rate when events ARE
	// subscribed: the tick then only catches what a lost or stale event
	// would otherwise leave stuck, so it can be slow — one round trip per
	// declared alias per interval.
	DefaultReconcileInterval = 30 * time.Second
)

// TargetI is what the follower drives on the caller's thread: the
// consumer's alias → handle rewrite (play's dataset delivery ops are the
// shipped implementation).
type TargetI interface {
	BindDataset(alias, handle string) error
	UnbindDataset(alias string) error
	NotifyDatasetRevision(alias string, revision uint64)
}

// EventsModeE says how the follower uses the service's events. It exists
// for fault injection — the headless lane shows the reconcile working
// against a running host — and is not an operating knob.
type EventsModeE uint8

const (
	// EventsOn subscribes and acts on every event (the default).
	EventsOn EventsModeE = iota
	// EventsDrop subscribes but discards every event — what a slow consumer
	// on NATS core experiences — leaving the reconcile tick alone to keep
	// the binding in step.
	EventsDrop
	// EventsOff does not subscribe at all: the pre-events poll.
	EventsOff
)

// ParseEventsMode maps "on" / "drop" / "off" to the mode; anything else is
// EventsOn.
func ParseEventsMode(s string) (m EventsModeE) {
	switch s {
	case "drop":
		return EventsDrop
	case "off":
		return EventsOff
	}
	return EventsOn
}

// FollowerConfig parameterises a Follower.
type FollowerConfig struct {
	// Bus is the consumer's client; it needs Pub adhoc.resolve and, for the
	// fast path, Sub adhoc.event.>. nil builds no follower.
	Bus app.BusI
	Log zerolog.Logger
	// Aliases are the declared aliases to keep bound. Empty builds no
	// follower.
	Aliases []string
	// Reconcile overrides DefaultReconcileInterval; zero keeps it.
	Reconcile time.Duration
	// Poll overrides DefaultPollInterval; zero keeps it.
	Poll time.Duration
	// Events is the fault-injection mode; the zero value is EventsOn.
	Events EventsModeE
}

// resolverI is the request/reply half the follower treats as truth;
// production wraps ResolveVerifyRequest over the consumer's bus, tests
// substitute a fake.
type resolverI interface {
	// resolveVerify returns the alias's newest live handle (handle == ""
	// when nothing is live under it) with its revision, and whether
	// boundHandle — when non-empty — is itself still live. err is a
	// transport failure only.
	resolveVerify(alias string, boundHandle string) (handle string, revision uint64, boundLive bool, err error)
}

type busResolver struct{ bus app.BusI }

func (r busResolver) resolveVerify(alias string, boundHandle string) (handle string, revision uint64, boundLive bool, err error) {
	res, live, rErr := ResolveVerifyRequest(r.bus, alias, boundHandle)
	// "no live dataset under alias" is an answer, not a transport failure:
	// the service replied, and boundLive is meaningful on that reply.
	if rErr != nil {
		if errors.Is(rErr, ErrNoLiveDataset) {
			boundLive = live
			return
		}
		err = rErr
		return
	}
	handle = res.Handle
	revision = res.Revision
	boundLive = live
	return
}

// verdict is one reconciled fact about an alias, parked by the worker for
// the caller's thread: what the alias currently resolves to (and at which
// revision), and whether the handle the alias was bound to at the time of
// asking is still live.
type verdict struct {
	alias       string
	handle      string // newest live handle under alias; "" when none
	revision    uint64 // revision of that handle
	askedHandle string // the bound handle the question was about; "" for a pending alias
	askedLive   bool
}

// Follower keeps declared aliases bound to live datasets for the life of a
// consumer. Build one with [NewFollower]; drive it from the caller's thread
// with [Follower.Sync]; release it with [Follower.Close].
type Follower struct {
	resolver resolverI
	log      zerolog.Logger
	interval time.Duration

	mu       sync.Mutex
	bound    map[string]string   // alias → handle currently bound
	revision map[string]uint64   // alias → last revision seen of the bound handle; 0 = unknown
	pending  map[string]struct{} // aliases with no live dataset

	// Mailbox from the arriving side to the caller's thread, in arrival
	// order: hints from the bus and verdicts from the worker. Order matters
	// — a `retracted` of the bound handle followed by the verdict that binds
	// its successor must unbind and then bind — so the log is replayed
	// sequentially against (bound, pending) rather than folded into sets.
	events   []Event
	verdicts []verdict
	// dirty marks aliases whose hint asked for a resolve before the next
	// tick would have.
	dirty    map[string]struct{}
	unsub    func() // events subscription; nil when unavailable
	inFlight bool
	nextAt   time.Time

	// pendingDirty is set whenever the pending set changed since the last
	// Sync reported it, so the caller re-renders its notice only then.
	pendingDirty bool
}

// NewFollower subscribes to dataset events, then resolves the declared
// aliases at open — in that order — and returns the follower together with
// the bindings the consumer applies at construction. nil (and nil
// bindings) when there is nothing declared or no bus to bind against.
func NewFollower(cfg FollowerConfig) (f *Follower, bindings map[string]string) {
	if len(cfg.Aliases) == 0 || cfg.Bus == nil {
		return
	}
	f = newFollowerWith(busResolver{bus: cfg.Bus}, cfg.Log)
	if cfg.Reconcile > 0 {
		f.interval = cfg.Reconcile
	}
	poll := cfg.Poll
	if poll <= 0 {
		poll = DefaultPollInterval
	}
	handler := f.onEvent
	if cfg.Events == EventsDrop {
		// Fault injection: the subscription is real, the events go
		// nowhere, the tick does the work.
		handler = func(Event) {}
		cfg.Log.Warn().Msg("adhocdata: follower discards dataset events; the reconcile tick alone keeps bindings in step")
	}
	var subErr error
	if cfg.Events == EventsOff {
		subErr = eh.Errorf("events disabled")
	} else {
		f.unsub, subErr = SubscribeEvents(cfg.Bus, handler)
	}
	if subErr != nil {
		// No hints for this consumer: the tick alone keeps the binding in
		// step, at the seconds-scale poll interval.
		cfg.Log.Debug().Err(subErr).Msg("adhocdata: dataset events unavailable; polling for declared aliases")
		f.interval = poll
	}
	bindings, unresolved := resolveAliases(cfg.Bus, cfg.Log, cfg.Aliases)
	f.seed(bindings, unresolved)
	return
}

// newFollowerWith builds a follower over an explicit resolver with no
// events subscription; production goes through NewFollower, tests seed it
// directly.
func newFollowerWith(resolver resolverI, logger zerolog.Logger) (f *Follower) {
	f = &Follower{
		resolver: resolver,
		log:      logger,
		interval: DefaultReconcileInterval,
		bound:    make(map[string]string),
		revision: make(map[string]uint64),
		pending:  make(map[string]struct{}),
		dirty:    make(map[string]struct{}),
	}
	return
}

// seed installs the open-time outcome: what resolved, what did not.
func (f *Follower) seed(bindings map[string]string, unresolved []string) {
	f.mu.Lock()
	maps.Copy(f.bound, bindings)
	for _, alias := range unresolved {
		f.pending[alias] = struct{}{}
	}
	// The open-time resolve just asked; hold the first tick off for a full
	// interval rather than re-asking a question answered microseconds ago.
	f.nextAt = time.Now().Add(f.interval)
	f.pendingDirty = true
	f.mu.Unlock()
}

// onEvent is the bus-goroutine half: it appends to the log and returns.
// Nothing here reaches the target, and nothing here decides — the decision
// needs the (bound, pending) state as it stands when the event is applied,
// which is the caller's thread's, in order.
func (f *Follower) onEvent(ev Event) {
	f.mu.Lock()
	f.events = append(f.events, ev)
	f.mu.Unlock()
}

// Sync is the caller's-thread half: it replays what arrived since the last
// call, in order, against target, then starts a worker round if a hint
// asked for one or the tick is due. It reports whether anything was newly
// bound (the caller re-runs its query), and whether the pending set changed
// since the last call (the caller re-renders whatever says what it is
// waiting for; the first call after construction reports true).
//
// Replay rules. A `published` under a pending alias marks it dirty — the
// worker asks the service and the answer binds. A `published` onto the
// handle a bound alias holds is a republish and notifies the revision. A
// `published` under a bound alias onto a DIFFERENT handle is deliberately
// ignored — an open consumer tracks re-captures through the stable handle
// and does not re-resolve to a newer sibling. A `retracted` of a bound
// handle unbinds its alias and returns it to pending; of a handle nobody
// holds it is a no-op, which also makes a duplicated or late `retracted`
// harmless — a handle never comes back once retracted. A verdict binds a
// pending alias to the handle it names, and for a bound alias either
// confirms the binding (its handle is live — whatever else is under the
// alias) or replaces it (unbind, then bind the successor if there is one).
// A verdict about a handle the alias no longer holds is stale and ignored.
func (f *Follower) Sync(target TargetI) (bound bool, pendingChanged bool) {
	f.mu.Lock()
	events := f.events
	verdicts := f.verdicts
	f.events, f.verdicts = nil, nil
	f.mu.Unlock()

	for _, ev := range events {
		switch ev.Op {
		case EventOpPublished:
			f.mu.Lock()
			_, waiting := f.pending[ev.Alias]
			held, isBound := f.bound[ev.Alias]
			if waiting {
				f.dirty[ev.Alias] = struct{}{}
			}
			if isBound && held == ev.Handle && ev.Revision > f.revision[ev.Alias] {
				f.revision[ev.Alias] = ev.Revision
			}
			f.mu.Unlock()
			if isBound && held == ev.Handle {
				target.NotifyDatasetRevision(ev.Alias, ev.Revision)
			}
		case EventOpRetracted:
			f.unbindHandle(target, ev.Handle)
		}
	}
	for _, v := range verdicts {
		f.mu.Lock()
		held, isBound := f.bound[v.alias]
		_, waiting := f.pending[v.alias]
		known := f.revision[v.alias]
		f.mu.Unlock()
		switch {
		case waiting && v.handle != "":
			if f.bindAlias(target, v.alias, v.handle, v.revision) {
				bound = true
			}
		case isBound && v.askedHandle == held && !v.askedLive:
			// Our handle has left; the successor, if any, replaces it.
			f.unbindHandle(target, held)
			if v.handle != "" && v.handle != held {
				if f.bindAlias(target, v.alias, v.handle, v.revision) {
					bound = true
				}
			}
		case isBound && v.askedHandle == held && v.handle == held && v.revision > known:
			// Same handle, newer revision: a republish whose hint was lost
			// (or that landed before the first tick, when the revision was
			// unknown — then it is only recorded, since the consumer ran at
			// open against data at least that fresh).
			f.mu.Lock()
			f.revision[v.alias] = v.revision
			f.mu.Unlock()
			if known != 0 {
				target.NotifyDatasetRevision(v.alias, v.revision)
			}
		}
	}

	f.mu.Lock()
	pendingChanged = f.pendingDirty
	f.pendingDirty = false
	var askPending []string
	askBound := map[string]string{}
	if !f.inFlight {
		due := !time.Now().Before(f.nextAt)
		if due {
			askPending = slices.Sorted(maps.Keys(f.pending))
			maps.Copy(askBound, f.bound)
		} else {
			for alias := range f.dirty {
				if _, waiting := f.pending[alias]; waiting {
					askPending = append(askPending, alias)
				}
			}
			slices.Sort(askPending)
		}
		clear(f.dirty)
		if len(askPending) > 0 || len(askBound) > 0 {
			f.inFlight = true
			if due {
				f.nextAt = time.Now().Add(f.interval)
			}
		}
	}
	f.mu.Unlock()

	if len(askPending) > 0 || len(askBound) > 0 {
		go f.reconcile(askPending, askBound)
	}
	return
}

// bindAlias binds on the caller's thread and records the outcome. A
// malformed handle is the service's problem, not a transient one — the
// alias settles out of pending anyway rather than being retried forever.
func (f *Follower) bindAlias(target TargetI, alias string, handle string, revision uint64) (bound bool) {
	f.mu.Lock()
	delete(f.pending, alias)
	f.pendingDirty = true
	f.mu.Unlock()
	if bErr := target.BindDataset(alias, handle); bErr != nil {
		f.log.Warn().Err(bErr).Str("alias", alias).Msg("adhocdata: dataset bind rejected")
		return
	}
	f.log.Info().Str("alias", alias).Str("handle", handle).Uint64("revision", revision).Msg("adhocdata: dataset alias bound")
	f.mu.Lock()
	f.bound[alias] = handle
	f.revision[alias] = revision
	f.mu.Unlock()
	bound = true
	return
}

// unbindHandle returns every alias bound to handle to pending, on the
// caller's thread; false when nothing held it.
func (f *Follower) unbindHandle(target TargetI, handle string) (changed bool) {
	f.mu.Lock()
	var gone []string
	for alias, h := range f.bound {
		if h == handle {
			gone = append(gone, alias)
		}
	}
	f.mu.Unlock()
	slices.Sort(gone)
	for _, alias := range gone {
		if uErr := target.UnbindDataset(alias); uErr != nil {
			f.log.Warn().Err(uErr).Str("alias", alias).Msg("adhocdata: dataset unbind rejected")
		} else {
			f.log.Info().Str("alias", alias).Msg("adhocdata: dataset alias unbound; waiting for the next publish")
		}
		f.mu.Lock()
		delete(f.bound, alias)
		delete(f.revision, alias)
		f.pending[alias] = struct{}{}
		f.pendingDirty = true
		f.mu.Unlock()
		changed = true
	}
	return
}

// reconcile is the worker: one blocking round trip per alias, verdicts
// parked for the next Sync. A miss for a pending alias is the expected
// outcome — the dataset simply has not been published yet — so it logs at
// debug, unlike the open-time miss.
func (f *Follower) reconcile(pending []string, bound map[string]string) {
	var out []verdict
	for _, alias := range pending {
		handle, rev, _, err := f.resolver.resolveVerify(alias, "")
		if err != nil {
			f.log.Debug().Err(err).Str("alias", alias).Msg("adhocdata: dataset alias still unresolved")
			continue
		}
		if handle == "" {
			continue
		}
		out = append(out, verdict{alias: alias, handle: handle, revision: rev})
	}
	for _, alias := range slices.Sorted(maps.Keys(bound)) {
		held := bound[alias]
		handle, rev, live, err := f.resolver.resolveVerify(alias, held)
		if err != nil {
			f.log.Debug().Err(err).Str("alias", alias).Msg("adhocdata: dataset binding not verified this round")
			continue
		}
		out = append(out, verdict{alias: alias, handle: handle, revision: rev, askedHandle: held, askedLive: live})
	}
	f.mu.Lock()
	f.verdicts = append(f.verdicts, out...)
	f.inFlight = false
	f.mu.Unlock()
}

// Close releases the events subscription. The host closes the instance's
// bus client at the closing edge as well (ADR-0188 §SD1); releasing here
// keeps the follower honest on hosts that do not. A poll still in flight
// finishes against a closed consumer, parks a verdict nobody reads, and is
// collected with the struct.
func (f *Follower) Close() {
	f.mu.Lock()
	unsub := f.unsub
	f.unsub = nil
	f.mu.Unlock()
	if unsub != nil {
		unsub()
	}
}

// Pending is the sorted set of aliases without a live dataset — what the
// consumer says it is waiting for.
func (f *Follower) Pending() (aliases []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	aliases = slices.Sorted(maps.Keys(f.pending))
	return
}

// resolveAliases maps each declared alias to the newest live dataset
// published under it, and returns the ones that missed. A miss binds
// nothing rather than failing the consumer's open: it keeps the miss
// pending rather than stranding the consumer on the wrong side of a
// capture-then-open ordering.
func resolveAliases(bus app.BusI, logger zerolog.Logger, aliases []string) (bindings map[string]string, unresolved []string) {
	bindings = make(map[string]string, len(aliases))
	for _, alias := range aliases {
		res, err := ResolveRequest(bus, alias)
		if err != nil {
			logger.Warn().Err(err).Str("alias", alias).Msg("adhocdata: dataset alias unresolved at open")
			unresolved = append(unresolved, alias)
			continue
		}
		bindings[alias] = res.Handle
	}
	return
}
