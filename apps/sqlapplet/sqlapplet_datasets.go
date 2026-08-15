// sqlapplet_datasets.go carries the whole after-open life of a declared applet
// dataset binding (ADR-0134 §SD4, ADR-0188 §SD3).
//
// A `datasets:` alias resolves to the newest live dataset published under it.
// Resolving at open is enough only when the data is already there, which puts
// an ordering on the reader — capture, *then* open — that nothing enforces and
// that a window has no way to recover from once it is on the wrong side of it.
// And a dataset that was there can leave: its producer retracts it, typically
// at its own Unmount, and a query naming the alias fails as an unknown table.
//
// So the binding is kept in step with the dataset service for as long as the
// applet lives. The service's events (ADR-0188 §SD3) are the fast path, and
// they are HINTS, not truth: a `published` under a pending alias makes the
// binder ask the service what the alias resolves to and bind that; a
// `published` onto the handle an alias is bound to is a republish and re-runs
// under Live; a `retracted` of that handle unbinds the alias, says so over the
// empty panes, and leaves it pending. Truth is the request/reply the binder
// makes off the render thread — at open (subscribed to the events BEFORE it
// resolves, so no publish can fall between the two), on every hint, and on a
// slow reconcile tick that re-asks for pending aliases and verifies bound
// handles in the same round trip (adhocdata.ResolveVerifyRequest). That is
// what makes the binding converge whatever the bus delivers: the in-proc bus
// loses nothing, but NATS core may drop a `published` or `retracted`, and an
// at-least-once transport may redeliver one late — a hint that is stale is
// corrected by the answer, a hint that never came is caught by the tick.
// Where the events cannot be subscribed at all (no cap, no bus), the tick
// runs at the seconds-scale poll interval instead.
//
// Off the render thread is not a preference for any of those requests: a bus
// Request waits the full request timeout when nothing serves the subject, and
// adhoc.* is unbound whenever the ad-hoc service failed to start.
//
// The render thread owns the binding side (BindDataset / UnbindDataset /
// NotifyDatasetRevision on the play instance, and the re-run and notice the
// caller drives from it); the bus goroutine and the worker own the arriving
// side. The mutex covers the handoff between them and nothing else.

package sqlapplet

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// datasetRetryInterval bounds the tick rate when events are unavailable and
// the binder can only poll for pending aliases. Seconds is the right order
// for a human who just pressed Capture; it is also a bus round-trip per tick
// per waiting window, so it does not want to be a frame.
const datasetRetryInterval = 2 * time.Second

// datasetReconcileInterval bounds the tick rate when events ARE subscribed:
// the tick then only catches what a lost or stale event would otherwise leave
// stuck, so it can be slow — one round trip per declared alias per interval.
const datasetReconcileInterval = 30 * time.Second

// datasetTargetI is what the binder drives on the render thread — the play
// instance's dataset delivery ops (apps/play/play_datasets.go).
type datasetTargetI interface {
	BindDataset(alias, handle string) error
	UnbindDataset(alias string) error
	NotifyDatasetRevision(alias string, revision uint64)
}

// datasetResolverI is the request/reply half the binder treats as truth;
// production wraps adhocdata.ResolveVerifyRequest over the app's bus, tests
// substitute a fake.
type datasetResolverI interface {
	// resolveVerify returns the alias's newest live handle (handle == "" when
	// nothing is live under it) and whether boundHandle — when non-empty —
	// is itself still live. err is a transport failure only.
	resolveVerify(alias string, boundHandle string) (handle string, boundLive bool, err error)
}

type busResolver struct{ bus app.BusI }

func (r busResolver) resolveVerify(alias string, boundHandle string) (handle string, boundLive bool, err error) {
	res, live, rErr := adhocdata.ResolveVerifyRequest(r.bus, alias, boundHandle)
	// "no live dataset under alias" is an answer, not a transport failure:
	// the service replied, and boundLive is meaningful on that reply.
	if rErr != nil {
		if errors.Is(rErr, adhocdata.ErrNoLiveDataset) {
			boundLive = live
			return
		}
		err = rErr
		return
	}
	handle = res.Handle
	boundLive = live
	return
}

// verdict is one reconciled fact about an alias, parked by the worker for the
// render thread: what the alias currently resolves to, and whether the handle
// the alias was bound to at the time of asking is still live.
type verdict struct {
	alias       string
	handle      string // newest live handle under alias; "" when none
	askedHandle string // the bound handle the question was about; "" for a pending alias
	askedLive   bool
}

// datasetBinder keeps the declared aliases bound to live datasets for the
// life of the applet. It exists only when the applet declares datasets and
// has a bus, so the common dataset-less applet allocates nothing and costs
// nothing per frame.
type datasetBinder struct {
	resolver datasetResolverI
	log      zerolog.Logger
	hint     string
	interval time.Duration

	mu      sync.Mutex
	bound   map[string]string   // alias → handle currently bound in play
	pending map[string]struct{} // aliases with no live dataset

	// Mailbox from the arriving side to the render thread, in arrival order:
	// hints from the bus and verdicts from the worker. Order matters — a
	// `retracted` of the bound handle followed by the verdict that binds
	// its successor must unbind and then bind — so the log is replayed
	// sequentially against (bound, pending) rather than folded into sets.
	events   []adhocdata.Event
	verdicts []verdict
	// dirty marks aliases whose hint asked for a resolve before the next
	// tick would have.
	dirty    map[string]struct{}
	unsub    func() // events subscription; nil when unavailable
	inFlight bool
	nextAt   time.Time

	notice    []byte
	noticeDty bool
}

// newDatasetBinder subscribes to dataset events, then resolves the declared
// aliases at open — in that order — and returns the binder together with the
// bindings the embedder applies at construction. nil (and nil bindings) when
// there is nothing declared or no bus to bind against.
func newDatasetBinder(bus app.BusI, logger zerolog.Logger, hint string, aliases []string) (b *datasetBinder, bindings map[string]string) {
	if len(aliases) == 0 || bus == nil {
		return
	}
	b = newDatasetBinderWith(busResolver{bus: bus}, logger, hint)
	unsub, subErr := adhocdata.SubscribeEvents(bus, b.onEvent)
	if subErr != nil {
		// No hints for this window: the tick alone keeps the binding in
		// step, at the seconds-scale poll interval.
		logger.Debug().Err(subErr).Msg("sqlapplet: dataset events unavailable; polling for declared aliases")
		b.interval = datasetRetryInterval
	} else {
		b.unsub = unsub
	}
	bindings, unresolved := resolveDatasetAliases(bus, logger, aliases)
	b.seed(bindings, unresolved)
	return
}

// newDatasetBinderWith builds a binder over an explicit resolver with no
// events subscription; production goes through newDatasetBinder, tests seed
// it directly.
func newDatasetBinderWith(resolver datasetResolverI, logger zerolog.Logger, hint string) (b *datasetBinder) {
	b = &datasetBinder{
		resolver: resolver,
		log:      logger,
		hint:     hint,
		interval: datasetReconcileInterval,
		bound:    make(map[string]string),
		pending:  make(map[string]struct{}),
		dirty:    make(map[string]struct{}),
	}
	return
}

// seed installs the open-time outcome: what resolved, what did not.
func (b *datasetBinder) seed(bindings map[string]string, unresolved []string) {
	b.mu.Lock()
	maps.Copy(b.bound, bindings)
	for _, alias := range unresolved {
		b.pending[alias] = struct{}{}
	}
	// The open-time resolve just asked; hold the first tick off for a full
	// interval rather than re-asking a question answered microseconds ago.
	b.nextAt = time.Now().Add(b.interval)
	b.notice = renderDatasetNotice(slices.Sorted(maps.Keys(b.pending)), b.hint)
	b.noticeDty = true
	b.mu.Unlock()
}

// onEvent is the bus-goroutine half: it appends to the log and returns.
// Nothing here reaches play, and nothing here decides — the decision needs
// the (bound, pending) state as it stands when the event is applied, which
// is the render thread's, in order.
func (b *datasetBinder) onEvent(ev adhocdata.Event) {
	b.mu.Lock()
	b.events = append(b.events, ev)
	b.mu.Unlock()
}

// sync is the render-thread half: it replays what arrived since the last
// frame, in order, against target, then starts a worker round if a hint
// asked for one or the tick is due. It reports whether anything was newly
// bound (the caller re-runs the buffer), and the notice text with whether
// it changed (the caller pushes it into play only on a change —
// SetDatasetNotice reparses, and this runs every frame).
//
// Replay rules. A `published` under a pending alias marks it dirty — the
// worker asks the service and the answer binds. A `published` onto the
// handle a bound alias holds is a republish and notifies the revision
// (ADR-0134 §SD5). A `published` under a bound alias onto a DIFFERENT
// handle is deliberately ignored — an open applet tracks re-captures through
// the stable handle and does not re-resolve to a newer sibling (ADR-0134,
// update 2026-08-01). A `retracted` of a bound handle unbinds its alias and
// returns it to pending; of a handle nobody holds it is a no-op, which also
// makes a duplicated or late `retracted` harmless — a handle never comes back
// once retracted. A verdict binds a pending alias to the handle it names,
// and for a bound alias either confirms the binding (its handle is live —
// whatever else is under the alias) or replaces it (unbind, then bind the
// successor if there is one). A verdict about a handle the alias no longer
// holds is stale and ignored.
func (b *datasetBinder) sync(target datasetTargetI) (bound bool, notice []byte, noticeChanged bool) {
	b.mu.Lock()
	events := b.events
	verdicts := b.verdicts
	b.events, b.verdicts = nil, nil
	b.mu.Unlock()

	changed := false
	for _, ev := range events {
		switch ev.Op {
		case adhocdata.EventOpPublished:
			b.mu.Lock()
			_, waiting := b.pending[ev.Alias]
			held, isBound := b.bound[ev.Alias]
			if waiting {
				b.dirty[ev.Alias] = struct{}{}
			}
			b.mu.Unlock()
			if isBound && held == ev.Handle {
				target.NotifyDatasetRevision(ev.Alias, ev.Revision)
			}
		case adhocdata.EventOpRetracted:
			if b.unbindHandle(target, ev.Handle) {
				changed = true
			}
		}
	}
	for _, v := range verdicts {
		b.mu.Lock()
		held, isBound := b.bound[v.alias]
		_, waiting := b.pending[v.alias]
		b.mu.Unlock()
		switch {
		case waiting && v.handle != "":
			if b.bindAlias(target, v.alias, v.handle) {
				bound = true
			}
			changed = true
		case isBound && v.askedHandle == held && !v.askedLive:
			// Our handle has left; the successor, if any, replaces it.
			b.unbindHandle(target, held)
			changed = true
			if v.handle != "" && v.handle != held {
				if b.bindAlias(target, v.alias, v.handle) {
					bound = true
				}
			}
		}
	}

	b.mu.Lock()
	if changed {
		b.notice = renderDatasetNotice(slices.Sorted(maps.Keys(b.pending)), b.hint)
		b.noticeDty = true
	}
	notice, noticeChanged = b.notice, b.noticeDty
	b.noticeDty = false
	var askPending []string
	askBound := map[string]string{}
	if !b.inFlight {
		due := !time.Now().Before(b.nextAt)
		if due {
			askPending = slices.Sorted(maps.Keys(b.pending))
			maps.Copy(askBound, b.bound)
		} else {
			for alias := range b.dirty {
				if _, waiting := b.pending[alias]; waiting {
					askPending = append(askPending, alias)
				}
			}
			slices.Sort(askPending)
		}
		clear(b.dirty)
		if len(askPending) > 0 || len(askBound) > 0 {
			b.inFlight = true
			if due {
				b.nextAt = time.Now().Add(b.interval)
			}
		}
	}
	b.mu.Unlock()

	if len(askPending) > 0 || len(askBound) > 0 {
		go b.reconcile(askPending, askBound)
	}
	return
}

// bindAlias binds on the render thread and records the outcome. A malformed
// handle is the service's problem, not a transient one — the alias settles
// out of pending anyway rather than being retried forever.
func (b *datasetBinder) bindAlias(target datasetTargetI, alias string, handle string) (bound bool) {
	if bErr := target.BindDataset(alias, handle); bErr != nil {
		b.log.Warn().Err(bErr).Str("alias", alias).Msg("sqlapplet: dataset bind rejected")
		b.mu.Lock()
		delete(b.pending, alias)
		b.mu.Unlock()
		return
	}
	b.log.Info().Str("alias", alias).Str("handle", handle).Msg("sqlapplet: dataset alias bound after open")
	b.mu.Lock()
	b.bound[alias] = handle
	delete(b.pending, alias)
	b.mu.Unlock()
	bound = true
	return
}

// unbindHandle returns every alias bound to handle to pending, on the render
// thread; false when nothing held it.
func (b *datasetBinder) unbindHandle(target datasetTargetI, handle string) (changed bool) {
	b.mu.Lock()
	var gone []string
	for alias, h := range b.bound {
		if h == handle {
			gone = append(gone, alias)
		}
	}
	b.mu.Unlock()
	slices.Sort(gone)
	for _, alias := range gone {
		if uErr := target.UnbindDataset(alias); uErr != nil {
			b.log.Warn().Err(uErr).Str("alias", alias).Msg("sqlapplet: dataset unbind rejected")
		} else {
			b.log.Info().Str("alias", alias).Msg("sqlapplet: dataset alias unbound; waiting for the next publish")
		}
		b.mu.Lock()
		delete(b.bound, alias)
		b.pending[alias] = struct{}{}
		b.mu.Unlock()
		changed = true
	}
	return
}

// reconcile is the worker: one blocking round trip per alias, verdicts
// parked for the next sync. A miss for a pending alias is the expected
// outcome — the dataset simply has not been published yet — so it logs at
// debug, unlike the open-time miss.
func (b *datasetBinder) reconcile(pending []string, bound map[string]string) {
	var out []verdict
	for _, alias := range pending {
		handle, _, err := b.resolver.resolveVerify(alias, "")
		if err != nil {
			b.log.Debug().Err(err).Str("alias", alias).Msg("sqlapplet: dataset alias still unresolved")
			continue
		}
		if handle == "" {
			continue
		}
		out = append(out, verdict{alias: alias, handle: handle})
	}
	for _, alias := range slices.Sorted(maps.Keys(bound)) {
		held := bound[alias]
		handle, live, err := b.resolver.resolveVerify(alias, held)
		if err != nil {
			b.log.Debug().Err(err).Str("alias", alias).Msg("sqlapplet: dataset binding not verified this round")
			continue
		}
		out = append(out, verdict{alias: alias, handle: handle, askedHandle: held, askedLive: live})
	}
	b.mu.Lock()
	b.verdicts = append(b.verdicts, out...)
	b.inFlight = false
	b.mu.Unlock()
}

// close releases the events subscription. The host closes the instance's
// bus client at the closing edge as well (ADR-0188 §SD1); releasing here
// keeps the binder honest on hosts that do not.
func (b *datasetBinder) close() {
	b.mu.Lock()
	unsub := b.unsub
	b.unsub = nil
	b.mu.Unlock()
	if unsub != nil {
		unsub()
	}
}

// pendingAliases is the sorted set of aliases without a live dataset.
func (b *datasetBinder) pendingAliases() (aliases []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	aliases = slices.Sorted(maps.Keys(b.pending))
	return
}

// resolveDatasetAliases maps each declared alias to the newest live dataset
// published under it, and returns the ones that missed. A miss binds nothing
// rather than failing the mount: the applet still opens, and the binder keeps
// the miss pending rather than stranding the window on the wrong side of a
// capture-then-open ordering. Blocking bus round-trips in Mount follow the
// adhocdemo precedent — Mount is not the frame loop; the loop is empty for
// the common dataset-less applet.
func resolveDatasetAliases(bus app.BusI, logger zerolog.Logger, aliases []string) (bindings map[string]string, unresolved []string) {
	if len(aliases) == 0 || bus == nil {
		return nil, nil
	}
	bindings = make(map[string]string, len(aliases))
	for _, alias := range aliases {
		res, err := adhocdata.ResolveRequest(bus, alias)
		if err != nil {
			logger.Warn().Err(err).Str("alias", alias).Msg("sqlapplet: dataset alias unresolved at open")
			unresolved = append(unresolved, alias)
			continue
		}
		bindings[alias] = res.Handle
	}
	return
}

// renderDatasetNotice builds the markdown play shows over the empty result
// panes. It names the aliases, because that is the identifier the failing
// query reports and the catalog lists, and appends the author's
// `datasets_hint` — the only part that can say how to produce the data. An
// empty alias set renders nothing: the condition has cleared.
func renderDatasetNotice(aliases []string, hint string) (md []byte) {
	if len(aliases) == 0 {
		return nil
	}
	quoted := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		quoted = append(quoted, "`"+alias+"`")
	}
	noun, verb := "dataset", "is"
	if len(aliases) > 1 {
		noun, verb = "datasets", "are"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Waiting for %s %s.** No dataset %s live under that alias — none published yet, or its producer withdrew it — so this applet has nothing to query.",
		noun, strings.Join(quoted, ", "), verb)
	if hint != "" {
		b.WriteString(" ")
		b.WriteString(hint)
	}
	b.WriteString(" The window binds it and re-runs by itself once it appears — no need to reopen.")
	return []byte(b.String())
}
