// Package appcenter is the app center window (ADR-0260): the registered apps
// on the left, and one page per app on the right, assembled from the
// introspection tables that name it — what its windows hold, what it keeps,
// what it did this run, which jobs, datasets and model calls are its, how
// much of its code ran, and which ADRs its code cites.
//
// Nothing here waits on the bus from the frame goroutine: a poller reads
// the window-wide tables once and on Refresh, the selected app's tables on a
// tick and on every selection change, and lands each as a snapshot.
package appcenter

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appcenter/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

const (
	// refreshEvery is the selected page's tick; a selection change reads
	// sooner. The window-wide tables (apps, ADRs, citations) are read once
	// and on Refresh: they change with a build, not with a frame.
	refreshEvery = 3 * time.Second
	// readTimeout bounds one pass over a page's lenses.
	readTimeout = 10 * time.Second
)

// global is what every page shares.
type global struct {
	apps     lens[appCols]
	adrs     lens[adrCols]
	coderefs lens[coderefCols]
	coverage lens[coverageStatusCols]
	read     time.Time
}

// page is one app's lenses.
type page struct {
	appId    string
	coverage lens[coveragePkgCols]
	caps     lens[capCols]
	tasks    lens[taskCols]
	events   lens[eventCols]
	state    lens[stateCols]
	llm      lens[llmCols]
	jobs     lens[jobCols]
	datasets lens[datasetCols]
	runs     lens[runCols]
	logs     lens[logCols]
	audit    lens[auditCols]
	frames   lens[frameCols]
	read     time.Time
}

// snapshot is what one frame renders from.
type snapshot struct {
	global global
	page   page
	// reads says the window has a reader; false only where the host minted
	// no bus for it.
	reads     bool
	lastError string
	// openNote is the last open request's refusal; it stays until the next
	// open, since the poller's pass says nothing about it.
	openNote string
}

// App is the per-window instance.
type App struct {
	ids     *c.WidgetIdStack
	logger  zerolog.Logger
	density styletokens.DensityE
	bus     app.BusI

	reader readerI

	appCtx    context.Context
	cancelApp context.CancelFunc
	dirty     chan struct{}
	wg        sync.WaitGroup

	// The frame's own state.
	selected string
	filter   string

	mu sync.Mutex
	// want is the selection the poller reads for; the frame writes it.
	want string
	// wantGlobal asks the poller to re-read the window-wide tables.
	wantGlobal bool
	snap       snapshot
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{ids: c.NewWidgetIdStack(), density: styletokens.ActiveDensity(), dirty: make(chan struct{}, 1)}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount takes the host's bus client, opens on the app a launch config
// names (ADR-0260 §SD1), and starts the poller.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.bus = ctx.Bus()
	if inst.reader == nil {
		// A test sets its own reader before Mount.
		if r := newBusReader(inst.bus); r != nil {
			inst.reader = r
		}
	}
	if raw := ctx.LaunchConfig(); len(raw) > 0 {
		cfg, dErr := buscodec.Decode[launchcfg.AppCenterLaunch](raw)
		if dErr != nil {
			inst.logger.Warn().Err(dErr).Msg("appcenter: launch config does not decode; opening on the list")
		} else {
			inst.selectApp(cfg.AppId)
		}
	}
	inst.mu.Lock()
	inst.wantGlobal = true
	inst.mu.Unlock()
	inst.appCtx, inst.cancelApp = context.WithCancel(context.Background())
	inst.wg.Add(1)
	go inst.poll()
	inst.markDirty()
	return
}

// Unmount stops the poller and waits for requests in flight.
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

// selectApp changes the page and asks for its lenses now.
func (inst *App) selectApp(appId string) {
	inst.selected = appId
	inst.mu.Lock()
	inst.want = appId
	inst.mu.Unlock()
	inst.markDirty()
}

// refreshAll re-reads the window-wide tables and the page.
func (inst *App) refreshAll() {
	inst.mu.Lock()
	inst.wantGlobal = true
	inst.mu.Unlock()
	inst.markDirty()
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
		inst.refresh()
		select {
		case <-inst.appCtx.Done():
			return
		case <-ticker.C:
		case <-inst.dirty:
		}
	}
}

// refresh reads what is wanted and lands it. The window-wide tables come
// first so a page never renders against a list it has not seen.
func (inst *App) refresh() {
	inst.mu.Lock()
	appId, wantGlobal := inst.want, inst.wantGlobal
	inst.wantGlobal = false
	g := inst.snap.global
	p := inst.snap.page
	inst.mu.Unlock()

	if inst.reader == nil {
		inst.mu.Lock()
		inst.snap.reads = false
		inst.snap.lastError = "this window has no bus to read the introspection tables through, so there is nothing to show"
		inst.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(inst.appCtx, readTimeout)
	defer cancel()
	if wantGlobal {
		g = readGlobal(ctx, inst.reader, g)
	}
	if appId != "" {
		if p.appId != appId {
			// A new page starts unread rather than showing the last app's rows
			// under this app's name.
			p = page{appId: appId}
		}
		p = readPage(ctx, inst.reader, p)
	} else {
		p = page{}
	}
	if inst.appCtx.Err() != nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.snap.reads = true
	inst.snap.lastError = ""
	inst.snap.global = g
	// The selection may have moved while the page was read; land only a
	// page that is still wanted, and let the next pass read the new one.
	if inst.want == p.appId {
		inst.snap.page = p
	}
}

// readGlobal reads the window-wide tables.
func readGlobal(ctx context.Context, r readerI, g global) global {
	readInto(ctx, r, tableApps, appsSql, "", &g.apps)
	readInto(ctx, r, tableAdr, adrSql, "", &g.adrs)
	readInto(ctx, r, tableCoderef, coderefSql, "", &g.coderefs)
	readInto(ctx, r, tableCoverageStatus, coverageStatusSql, "", &g.coverage)
	g.read = time.Now()
	return g
}

// readPage reads one app's lenses, each bound to its id.
func readPage(ctx context.Context, r readerI, p page) page {
	id := p.appId
	readInto(ctx, r, tableCoveragePkgs, coveragePkgsSql, id, &p.coverage)
	readInto(ctx, r, tableClientCaps, capsSql, id, &p.caps)
	readInto(ctx, r, tableTasks, tasksSql, id, &p.tasks)
	readInto(ctx, r, tableRunEvents, eventsSql, id, &p.events)
	readInto(ctx, r, tableAppState, stateSql, id, &p.state)
	readInto(ctx, r, llm.TableCalls, llmSql, id, &p.llm)
	readInto(ctx, r, watchbill.TableJobs, jobsSql, id, &p.jobs)
	readInto(ctx, r, adhocdata.CatalogTableName, datasetsSql, id, &p.datasets)
	readInto(ctx, r, providers.TableAppRuns, runsSql, id, &p.runs)
	readInto(ctx, r, providers.TableAppLogs, logsSql, id, &p.logs)
	readInto(ctx, r, providers.TableAppAudit, auditSql, id, &p.audit)
	readInto(ctx, r, tableFrameTimes, framesSql, id, &p.frames)
	p.read = time.Now()
	return p
}

// openApp asks the window host to open target, plainly.
func (inst *App) openApp(target string) {
	inst.request(target, "", nil)
}

func (inst *App) snapshot() (s snapshot) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.snap
}
