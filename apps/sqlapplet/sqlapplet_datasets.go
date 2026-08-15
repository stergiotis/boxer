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
// applet lives, driven by the service's events (ADR-0188 §SD3): a `published`
// under a pending alias binds it and re-runs the buffer; a `published` onto
// the handle an alias is bound to is a republish and re-runs under Live; a
// `retracted` of that handle unbinds the alias, says so over the empty panes,
// and waits for the next publish. The events subscription is taken BEFORE the
// open-time resolve so no publish can fall between the two. Where the events
// cannot be subscribed (no cap, no bus), the appear direction falls back to
// the seconds-scale poll it had before, off the render thread — a bus Request
// waits the full request timeout when nothing serves the subject, and adhoc.*
// is unbound whenever the ad-hoc service failed to start.
//
// The render thread owns the binding side (BindDataset / UnbindDataset /
// NotifyDatasetRevision on the play instance, and the re-run and notice the
// caller drives from it); the bus goroutine and the poll worker own the
// arriving side. The mutex covers the handoff between them and nothing else.

package sqlapplet

import (
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

// datasetRetryInterval bounds the poll rate while an alias stays unresolved
// and events are unavailable. Seconds is the right order for a human who just
// pressed Capture; it is also a bus round-trip per tick per waiting window,
// so it does not want to be a frame.
const datasetRetryInterval = 2 * time.Second

// datasetTargetI is what the binder drives on the render thread — the play
// instance's dataset delivery ops (apps/play/play_datasets.go).
type datasetTargetI interface {
	BindDataset(alias, handle string) error
	UnbindDataset(alias string) error
	NotifyDatasetRevision(alias string, revision uint64)
}

// datasetBinder keeps the declared aliases bound to live datasets for the
// life of the applet. It exists only when the applet declares datasets and
// has a bus, so the common dataset-less applet allocates nothing and costs
// nothing per frame.
type datasetBinder struct {
	bus  app.BusI
	log  zerolog.Logger
	hint string

	mu      sync.Mutex
	bound   map[string]string   // alias → handle currently bound in play
	pending map[string]struct{} // aliases with no live dataset

	// Mailbox from the arriving side to the render thread: events in
	// arrival order. Order matters — a `retracted` of the bound handle
	// followed by a `published` of the alias's successor must unbind and
	// then bind, so the log is replayed sequentially against (bound,
	// pending) rather than folded into per-kind sets. Poll results enter
	// the same log as synthetic `published` events.
	events    []adhocdata.Event
	unsub     func() // events subscription; nil in poll mode
	pollMode  bool   // events unavailable: poll for pending
	inFlight  bool
	nextAt    time.Time
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
	b = &datasetBinder{
		bus:     bus,
		log:     logger,
		hint:    hint,
		bound:   make(map[string]string, len(aliases)),
		pending: make(map[string]struct{}, len(aliases)),
	}
	unsub, subErr := adhocdata.SubscribeEvents(bus, b.onEvent)
	if subErr != nil {
		// No events for this window: appear-side polling as before, and no
		// withdrawal notice — the query says "unknown table" instead.
		logger.Debug().Err(subErr).Msg("sqlapplet: dataset events unavailable; polling for pending aliases")
		b.pollMode = true
	} else {
		b.unsub = unsub
	}
	bindings, unresolved := resolveDatasetAliases(bus, logger, aliases)
	b.mu.Lock()
	maps.Copy(b.bound, bindings)
	for _, alias := range unresolved {
		b.pending[alias] = struct{}{}
	}
	// The open-time resolve just missed; hold the first poll off for a full
	// interval rather than re-asking a question answered microseconds ago.
	b.nextAt = time.Now().Add(datasetRetryInterval)
	b.notice = renderDatasetNotice(slices.Sorted(maps.Keys(b.pending)), hint)
	b.noticeDty = true
	b.mu.Unlock()
	return
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

// sync is the render-thread half: it replays the events that arrived since
// the last frame, in order, against target, then, in poll mode, starts the
// next resolve if one is due. It reports whether anything was newly bound
// (the caller re-runs the buffer), and the notice text with whether it
// changed (the caller pushes it into play only on a change —
// SetDatasetNotice reparses, and this runs every frame).
//
// Replay rules, per event: a `published` under a pending alias binds it; a
// `published` onto the handle a bound alias holds is a republish and
// notifies the revision (ADR-0134 §SD5); a `published` under a bound alias
// onto a DIFFERENT handle is deliberately ignored — an open applet tracks
// re-captures through the stable handle and does not re-resolve to a newer
// sibling (ADR-0134, update 2026-08-01); a `retracted` of a bound handle
// unbinds its alias and returns it to pending. Because the log is ordered,
// a retract followed by a publish of the alias's successor unbinds and then
// binds within one frame; a publish followed by a retract of the same
// handle binds and then unbinds — the state after the frame is the state
// the service's own history produced. A retract of a handle nobody holds is
// a no-op, which also makes a duplicated or late `retracted` harmless: a
// handle never comes back once retracted.
func (b *datasetBinder) sync(target datasetTargetI) (bound bool, notice []byte, noticeChanged bool) {
	b.mu.Lock()
	events := b.events
	b.events = nil
	b.mu.Unlock()

	changed := false
	for _, ev := range events {
		switch ev.Op {
		case adhocdata.EventOpPublished:
			b.mu.Lock()
			_, waiting := b.pending[ev.Alias]
			held, isBound := b.bound[ev.Alias]
			b.mu.Unlock()
			switch {
			case waiting:
				// Act outside the lock: target reaches into play's client,
				// and the handoff mutex has no business spanning that.
				if bErr := target.BindDataset(ev.Alias, ev.Handle); bErr != nil {
					// A malformed handle is the service's problem, not a
					// transient one — settle the alias anyway rather than
					// retry it forever.
					b.log.Warn().Err(bErr).Str("alias", ev.Alias).Msg("sqlapplet: dataset bind rejected")
					b.mu.Lock()
					delete(b.pending, ev.Alias)
					b.mu.Unlock()
				} else {
					b.log.Info().Str("alias", ev.Alias).Str("handle", ev.Handle).Msg("sqlapplet: dataset alias bound after open")
					b.mu.Lock()
					b.bound[ev.Alias] = ev.Handle
					delete(b.pending, ev.Alias)
					b.mu.Unlock()
					bound = true
				}
				changed = true
			case isBound && held == ev.Handle:
				target.NotifyDatasetRevision(ev.Alias, ev.Revision)
			}
		case adhocdata.EventOpRetracted:
			b.mu.Lock()
			var gone []string
			for alias, h := range b.bound {
				if h == ev.Handle {
					gone = append(gone, alias)
				}
			}
			b.mu.Unlock()
			slices.Sort(gone)
			for _, alias := range gone {
				if uErr := target.UnbindDataset(alias); uErr != nil {
					b.log.Warn().Err(uErr).Str("alias", alias).Msg("sqlapplet: dataset unbind rejected")
				} else {
					b.log.Info().Str("alias", alias).Msg("sqlapplet: dataset alias unbound after retract; waiting for the next publish")
				}
				b.mu.Lock()
				delete(b.bound, alias)
				b.pending[alias] = struct{}{}
				b.mu.Unlock()
				changed = true
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
	var attempt []string
	if b.pollMode && len(b.pending) > 0 && !b.inFlight && !time.Now().Before(b.nextAt) {
		b.inFlight = true
		attempt = slices.Sorted(maps.Keys(b.pending))
	}
	b.mu.Unlock()

	if len(attempt) > 0 {
		go b.resolve(attempt)
	}
	return
}

// resolve is the poll worker (events unavailable): one blocking bus
// round-trip per still-pending alias, results parked for the next sync. A
// miss is the expected outcome here — the dataset simply has not been
// published yet — so it logs at debug, unlike the open-time miss.
func (b *datasetBinder) resolve(aliases []string) {
	landed := make(map[string]string, len(aliases))
	for _, alias := range aliases {
		res, err := adhocdata.ResolveRequest(b.bus, alias)
		if err != nil {
			b.log.Debug().Err(err).Str("alias", alias).Msg("sqlapplet: dataset alias still unresolved")
			continue
		}
		landed[alias] = res.Handle
	}
	b.mu.Lock()
	// Poll results enter the ordered log as synthetic `published` events,
	// so the render thread has one replay path.
	for _, alias := range slices.Sorted(maps.Keys(landed)) {
		b.events = append(b.events, adhocdata.Event{Op: adhocdata.EventOpPublished, Alias: alias, Handle: landed[alias]})
	}
	b.inFlight = false
	b.nextAt = time.Now().Add(datasetRetryInterval)
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
