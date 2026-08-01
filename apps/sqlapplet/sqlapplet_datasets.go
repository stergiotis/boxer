// sqlapplet_datasets.go carries the open-time and after-open halves of a
// declared applet dataset binding (ADR-0134 §SD4).
//
// A `datasets:` alias resolves to the newest live dataset published under it.
// Resolving at open is enough only when the data is already there, which puts
// an ordering on the reader — capture, *then* open — that nothing enforces and
// that a window has no way to recover from once it is on the wrong side of it.
// So the miss is not terminal: the aliases that did not resolve are retried
// off the render thread until they do, and the applet says what it is waiting
// for in the meantime.
//
// Off the render thread is not a preference. A bus Request waits the full
// request timeout when nothing serves the subject (inprocbus's default is
// seconds), and adhoc.* is unbound whenever the ad-hoc service failed to
// start — so a resolve on the frame loop would freeze the UI for the whole
// timeout, on the exact configuration where it can never succeed.

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

// datasetRetryInterval bounds the retry rate while an alias stays unresolved.
// The interval is the reader's wait between publishing a dataset and the open
// window picking it up, so it wants to be short; it is also a bus round-trip
// per tick per waiting window, so it does not want to be a frame. Seconds is
// the right order for a human who just pressed Capture.
const datasetRetryInterval = 2 * time.Second

// datasetRebinder tracks the declared aliases that have not resolved yet and
// keeps trying. It exists only when at least one alias missed at open, so the
// common case — everything bound at open — allocates nothing and costs nothing
// per frame.
//
// The render thread owns the binding side (BindDataset, and the re-run and
// notice the caller drives from it); a worker goroutine owns the resolving
// side. The mutex covers the handoff between them and nothing else.
type datasetRebinder struct {
	bus  app.BusI
	log  zerolog.Logger
	hint string

	mu       sync.Mutex
	pending  map[string]struct{} // aliases still unresolved
	resolved map[string]string   // alias → handle, awaiting the render thread
	inFlight bool
	nextAt   time.Time
	// notice is the current strip text; noticeDirty marks it as not yet
	// pushed into play. Construction leaves it dirty so the first frame shows
	// the notice without waiting for a resolve attempt to land.
	notice      []byte
	noticeDirty bool
}

// newDatasetRebinder returns a rebinder for the aliases that did not resolve
// at open, or nil when there are none, or when there is no bus to retry
// against (a host without a capability bus can never resolve them).
func newDatasetRebinder(bus app.BusI, logger zerolog.Logger, hint string, unresolved []string) (r *datasetRebinder) {
	if len(unresolved) == 0 || bus == nil {
		return nil
	}
	r = &datasetRebinder{
		bus:     bus,
		log:     logger,
		hint:    hint,
		pending: make(map[string]struct{}, len(unresolved)),
		// The caller just tried and missed; hold the first retry off for a
		// full interval rather than spending a round-trip re-asking a
		// question answered microseconds ago.
		nextAt:      time.Now().Add(datasetRetryInterval),
		noticeDirty: true,
	}
	for _, alias := range unresolved {
		r.pending[alias] = struct{}{}
	}
	r.notice = renderDatasetNotice(slices.Sorted(maps.Keys(r.pending)), hint)
	return
}

// sync is the render-thread half: it binds whatever the worker resolved since
// the last frame through bind (in practice PlayApp.BindDataset), then starts
// the next attempt if one is due. It reports whether anything newly bound
// (the caller re-runs the buffer), the notice text and whether it changed
// (the caller pushes it into play only on a change — SetDatasetNotice
// reparses, and this runs every frame), and whether nothing is pending any
// more (the caller drops the rebinder).
func (r *datasetRebinder) sync(bind func(alias string, handle string) error) (bound bool, notice []byte, noticeChanged bool, done bool) {
	r.mu.Lock()
	landed := r.resolved
	r.resolved = nil
	r.mu.Unlock()

	// Bind outside the lock: bind reaches into play's client, and the handoff
	// mutex has no business spanning that.
	settled := make([]string, 0, len(landed))
	for alias, handle := range landed {
		if bErr := bind(alias, handle); bErr != nil {
			// A malformed handle is the service's problem, not a transient
			// one — settle the alias anyway rather than retry it forever.
			r.log.Warn().Err(bErr).Str("alias", alias).Msg("sqlapplet: dataset bind rejected")
		} else {
			r.log.Info().Str("alias", alias).Str("handle", handle).Msg("sqlapplet: dataset alias bound after open")
			bound = true
		}
		settled = append(settled, alias)
	}

	r.mu.Lock()
	if len(settled) > 0 {
		for _, alias := range settled {
			delete(r.pending, alias)
		}
		r.notice = renderDatasetNotice(slices.Sorted(maps.Keys(r.pending)), r.hint)
		r.noticeDirty = true
	}
	notice, noticeChanged = r.notice, r.noticeDirty
	r.noticeDirty = false
	done = len(r.pending) == 0
	var attempt []string
	if !done && !r.inFlight && !time.Now().Before(r.nextAt) {
		r.inFlight = true
		attempt = slices.Sorted(maps.Keys(r.pending))
	}
	r.mu.Unlock()

	if len(attempt) > 0 {
		go r.resolve(attempt)
	}
	return
}

// resolve is the worker half: one blocking bus round-trip per still-pending
// alias, results parked for the next sync. A miss is the expected outcome
// here — the dataset simply has not been published yet — so it logs at debug,
// unlike the open-time miss that starts this whole path.
func (r *datasetRebinder) resolve(aliases []string) {
	landed := make(map[string]string, len(aliases))
	for _, alias := range aliases {
		res, err := adhocdata.ResolveRequest(r.bus, alias)
		if err != nil {
			r.log.Debug().Err(err).Str("alias", alias).Msg("sqlapplet: dataset alias still unresolved")
			continue
		}
		landed[alias] = res.Handle
	}
	r.mu.Lock()
	if r.resolved == nil {
		r.resolved = make(map[string]string, len(landed))
	}
	maps.Copy(r.resolved, landed)
	r.inFlight = false
	r.nextAt = time.Now().Add(datasetRetryInterval)
	r.mu.Unlock()
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
	noun, verb := "dataset", "has"
	if len(aliases) > 1 {
		noun, verb = "datasets", "have"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Waiting for %s %s.** Nothing %s been published under that alias yet, so this applet has nothing to query.",
		noun, strings.Join(quoted, ", "), verb)
	if hint != "" {
		b.WriteString(" ")
		b.WriteString(hint)
	}
	b.WriteString(" The window binds it and re-runs by itself once it appears — no need to reopen.")
	return []byte(b.String())
}
