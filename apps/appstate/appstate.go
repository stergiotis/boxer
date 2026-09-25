// Package appstate is the app-state manager window (ADR-0185 §SD6): what
// each app keeps across restarts — persisted values, saved workingsets,
// column-width overrides — read from keelson('app_state'), with a delete
// per entry and a forget per app through the runtime.appstate seam. Nothing
// here waits on a store or the bus from the frame goroutine: a poller reads
// the table on a tick and after every verb, and each verb runs in its own
// goroutine and lands its outcome for the next frame.
package appstate

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	as "github.com/stergiotis/boxer/public/keelson/runtime/appstate"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

const (
	// refreshEvery is the poller's tick; a verb refreshes sooner. State
	// another window writes arrives at this cadence.
	refreshEvery = 3 * time.Second
	// requestTimeout bounds one verb on the bus.
	requestTimeout = 5 * time.Second
)

// appSummary is one app's line in the left list.
type appSummary struct {
	appId   string
	entries int
	bytes   int64
}

// summarize groups entries by app, in app order.
func summarize(cols *entryCols) (apps []appSummary) {
	idx := make(map[string]int, 8)
	for j, appId := range cols.AppId {
		i, ok := idx[appId]
		if !ok {
			i = len(apps)
			idx[appId] = i
			apps = append(apps, appSummary{appId: appId})
		}
		apps[i].entries++
		apps[i].bytes += cols.PayloadBytes[j]
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].appId < apps[j].appId })
	return
}

// entriesOf is the indices of one app's entries.
func entriesOf(cols *entryCols, appId string) (idx []int) {
	for i, a := range cols.AppId {
		if a == appId {
			idx = append(idx, i)
		}
	}
	return
}

// shortApp is the last segment of an app id — its import path's leaf, or
// an applet's slug. The full id is on hover.
func shortApp(appId string) (s string) {
	s = appId
	if i := strings.LastIndexByte(s, '/'); i >= 0 && i < len(s)-1 {
		s = s[i+1:]
	}
	return
}

// armedForget is a forget waiting for its confirmation (ADR-0185 M3): the
// app, and how many entries it had when armed, so the confirmation names
// what the user agreed to.
type armedForget struct {
	appId   string
	entries int
}

// snapshot is what one frame renders from.
type snapshot struct {
	rows      entryCols
	lastError string
	lastNote  string
	refreshed time.Time
	inflight  int
	// reads says the window has a reader; false only where the host minted
	// no bus for it.
	reads bool
}

// App is the per-window instance.
type App struct {
	ids     *c.WidgetIdStack
	logger  zerolog.Logger
	density styletokens.DensityE

	client *as.Client
	reader entriesReaderI

	appCtx    context.Context
	cancelApp context.CancelFunc
	dirty     chan struct{}
	wg        sync.WaitGroup

	// The frame's own state.
	selected string
	armed    *armedForget

	mu   sync.Mutex
	snap snapshot
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{ids: c.NewWidgetIdStack(), density: styletokens.ActiveDensity(), dirty: make(chan struct{}, 1)}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount takes the host's bus client, reads the table through it, and starts
// the poller.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.client = as.NewClient(ctx.Bus())
	inst.client.Timeout = requestTimeout
	if inst.reader == nil {
		// A test sets its own reader before Mount; the window reads the
		// table over the bus, or nothing when the host minted none.
		if r := newTableReader(ctx.Bus()); r != nil {
			inst.reader = r
		}
	}
	inst.appCtx, inst.cancelApp = context.WithCancel(context.Background())
	inst.wg.Add(1)
	go inst.poll()
	inst.markDirty()
	return
}

// Unmount stops the poller and waits for verbs in flight.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.cancelApp != nil {
		inst.cancelApp()
	}
	inst.wg.Wait()
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.density = styletokens.ActiveDensity()
	inst.render()
	return
}

// --- the bus side, off the frame goroutine --------------------------------

func (inst *App) markDirty() {
	select {
	case inst.dirty <- struct{}{}:
	default:
	}
}

func (inst *App) poll() {
	defer inst.wg.Done()
	ticker := time.NewTicker(refreshEvery)
	defer ticker.Stop()
	for {
		select {
		case <-inst.appCtx.Done():
			return
		case <-ticker.C:
		case <-inst.dirty:
		}
		inst.refresh()
	}
}

// refresh reads the table and lands it as one snapshot. A failed read keeps
// the rows the window had; the error says why.
func (inst *App) refresh() {
	var rows entryCols
	var readErr string
	if inst.reader == nil {
		readErr = "this window has no bus to read keelson('app_state') through, so there is nothing to list"
	} else if got, err := inst.reader.entries(inst.appCtx); err != nil {
		readErr = "read: " + err.Error()
	} else {
		rows = got
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.snap.reads = inst.reader != nil
	if readErr != "" {
		inst.snap.lastError = readErr
		return
	}
	if strings.HasPrefix(inst.snap.lastError, "read: ") || inst.snap.lastError == "" {
		inst.snap.lastError = ""
	}
	inst.snap.rows = rows
	inst.snap.refreshed = time.Now()
}

// deleteEntry clears one row, named as the table names it.
func (inst *App) deleteEntry(r entryRow) {
	inst.verb("delete", func() (note string, err error) {
		res, err := inst.client.Delete(app.AppIdT(r.AppId), r.Kind, r.Key, r.EntityId)
		if err != nil {
			return "", err
		}
		if !res.Ok {
			return describe(res, ""), nil
		}
		return "cleared " + r.Kind + " " + r.Key + " of " + shortApp(r.AppId), nil
	})
}

// armForget arms the confirmation; nothing is cleared yet.
func (inst *App) armForget(s appSummary) {
	inst.armed = &armedForget{appId: s.appId, entries: s.entries}
}

// cancelForget drops the armed confirmation.
func (inst *App) cancelForget() {
	inst.armed = nil
}

// confirmForget sends the forget the user confirmed, and disarms.
func (inst *App) confirmForget() {
	armed := inst.armed
	inst.armed = nil
	if armed == nil {
		return
	}
	inst.verb("forget", func() (note string, err error) {
		res, err := inst.client.Forget(app.AppIdT(armed.appId))
		if err != nil {
			return "", err
		}
		return describe(res, "forgot "+shortApp(armed.appId)), nil
	})
}

// describe is a result's one-line note. A refusal or a partial clear
// travels as an error note: the reason is what the user must read.
func describe(res as.Result, done string) (note string) {
	if !res.Ok {
		return "not cleared: " + res.Reason
	}
	parts := make([]string, 0, len(res.Outcomes))
	for _, o := range res.Outcomes {
		parts = append(parts, o.Kind+" ×"+strconv.FormatUint(o.Cleared, 10))
	}
	if len(parts) == 0 {
		return done + " (nothing was stored)"
	}
	return done + ": " + strings.Join(parts, ", ")
}

// verb runs one request in its own goroutine and lands its outcome, then
// asks the poller to re-read so the list shows what the store now holds.
func (inst *App) verb(name string, fn func() (note string, err error)) {
	if inst.client == nil || inst.appCtx == nil || inst.appCtx.Err() != nil {
		return
	}
	inst.mu.Lock()
	inst.snap.inflight++
	inst.mu.Unlock()
	inst.wg.Add(1)
	go func() {
		defer inst.wg.Done()
		note, err := fn()
		inst.mu.Lock()
		inst.snap.inflight--
		switch {
		case err != nil:
			inst.snap.lastError = name + ": " + err.Error()
		case strings.HasPrefix(note, "not cleared: "):
			inst.snap.lastError = name + " " + note
		default:
			inst.snap.lastError = ""
			inst.snap.lastNote = note
		}
		inst.mu.Unlock()
		inst.markDirty()
	}()
}

func (inst *App) snapshot() (s snapshot) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.snap
}
