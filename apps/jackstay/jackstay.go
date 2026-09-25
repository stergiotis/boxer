// Package jackstay is the wizard of ADR-0259 §SD7: the guided
// ClickHouse-to-ClickHouse sync, one step per page — Connect, Databases,
// Structure, Differences, Sync, Monitor — over the same plan document and the
// same workflow functions the `boxer jackstay` CLI uses.
//
// Nothing waits on a server from the frame goroutine. Every step runs as a
// bgjob on a copy of the plan and lands a new plan value, which the frame
// takes, shows and, when the plan has a file, saves. The worker goroutines
// never call imzero2.
package jackstay

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/filepicker"
)

type stepE uint8

const (
	stepConnect stepE = iota
	stepDatabases
	stepStructure
	stepDifferences
	stepSync
	stepMonitor
)

var allSteps = []stepE{stepConnect, stepDatabases, stepStructure, stepDifferences, stepSync, stepMonitor}

func (inst stepE) String() (s string) {
	switch inst {
	case stepConnect:
		return "1 Connect"
	case stepDatabases:
		return "2 Databases"
	case stepStructure:
		return "3 Structure"
	case stepDifferences:
		return "4 Differences"
	case stepSync:
		return "5 Sync"
	case stepMonitor:
		return "6 Monitor"
	}
	return "invalid"
}

// PlanEnv names a plan file the window opens at start and saves to — the
// seed that lets a headless scene reach the steps that need a saved plan
// without the file dialog.
var PlanEnv = env.NewString(env.Spec{
	Name:        "BOXER_JACKSTAY_PLAN",
	Description: "jackstay window: plan file opened at start and saved to (created on first save when missing)",
	Category:    env.CategoryDatabase,
})

// chunkLogCap bounds the chunk results the Sync and Monitor pages list.
const chunkLogCap = 200

// discovered is the Connect step's result.
type discovered struct {
	srcEp, dstEp jk.Endpoint
	src, dst     jk.Inventory
}

// stepResult is what a plan-producing step hands back to the frame.
type stepResult struct {
	plan    jk.Plan
	stale   []string
	applied []string
	skipped []string
	note    string
}

// App is the per-window instance.
type App struct {
	ids    *c.WidgetIdStack
	logger zerolog.Logger

	appCtx    context.Context
	cancelApp context.CancelFunc

	step stepE

	// Connect: bound to text inputs, so fields, never frame locals.
	srcURL, srcUser string
	dstURL, dstUser string
	discoverJob     bgjob.Runner[discovered]
	disc            *discovered

	// Databases.
	dbPick     map[string]*bool
	dbTarget   map[string]*string
	leewayOnly bool

	// The plan and its file.
	plan     *jk.Plan
	planPath string
	openDlg  *filepicker.Inst
	saveDlg  *filepicker.Inst

	// Structure and its confirmation.
	structureJob bgjob.Runner[stepResult]
	applyArmed   bool
	selected     datacatalog.TableRef

	// Differences.
	diffJob bgjob.Runner[stepResult]
	final   bool

	// Sync.
	syncJob     bgjob.Runner[stepResult]
	previewJob  bgjob.Runner[[]jk.PreflightDisk]
	preflight   []jk.PreflightDisk
	syncMode    jk.SyncModeE
	existing    jk.ExistingPolicyE
	sampleText  string
	compression string
	restart     bool
	rows, bytes atomic.Int64

	logMu    sync.Mutex
	chunkLog []jk.ChunkResult

	// Monitor: lastDisks is the latest read, kept while the next is taken.
	disks     bgjob.Keyed[jk.DiskReport]
	lastDisks *jk.DiskReport

	// The status line: the last outcome, or the last error.
	note, lastError string
	stale           []string
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	src := jk.SourceEndpoint()
	dst, _ := jk.TargetEndpoint()
	inst = &App{
		ids:         c.NewWidgetIdStack(),
		srcURL:      src.URL,
		srcUser:     src.User,
		dstURL:      dst.URL,
		dstUser:     dst.User,
		dbPick:      make(map[string]*bool, 16),
		dbTarget:    make(map[string]*string, 16),
		sampleText:  "1/100",
		compression: "zstd",
	}
	// With no target configured, the target is the source server: copying
	// between two databases of one server is a sync too, and the Databases
	// step then suggests renamed targets.
	if inst.dstURL == "" {
		inst.dstURL, inst.dstUser = src.URL, src.User
	}
	if inst.dstUser == "" {
		inst.dstUser = "default"
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.appCtx, inst.cancelApp = context.WithCancel(context.Background())
	inst.openDlg = filepicker.New("jackstay-open", filepicker.ModeOpen,
		filepicker.WithTitle("Open a jackstay plan"), filepicker.WithExtensionFilter(".json"), filepicker.WithStartAtOsHome())
	inst.saveDlg = filepicker.New("jackstay-save", filepicker.ModeSave,
		filepicker.WithTitle("Save the jackstay plan"), filepicker.WithExtensionFilter(".json"), filepicker.WithDefaultFilename("plan.json"), filepicker.WithStartAtOsHome())
	if path := PlanEnv.Get(); path != "" {
		inst.planPath = path
		if p, lerr := jk.LoadPlan(path); lerr == nil {
			inst.plan = &p
			inst.srcURL, inst.srcUser = p.Source.URL, p.Source.User
			inst.dstURL, inst.dstUser = p.Target.URL, p.Target.User
		}
	}
	return
}

// Unmount cancels whatever runs. A sync stopped this way resumes from its
// journal the next time the plan is synced.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.cancelApp != nil {
		inst.cancelApp()
	}
	inst.discoverJob.Cancel()
	inst.structureJob.Cancel()
	inst.diffJob.Cancel()
	inst.syncJob.Cancel()
	inst.previewJob.Cancel()
	inst.disks.Close()
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.takeResults()
	inst.render()
	if inst.anyRunning() {
		c.RequestRepaint()
	}
	return
}

func (inst *App) anyRunning() (running bool) {
	return inst.discoverJob.Running() || inst.structureJob.Running() || inst.diffJob.Running() ||
		inst.syncJob.Running() || inst.previewJob.Running()
}

// --- endpoints and clients -------------------------------------------------

func (inst *App) endpoints() (src jk.Endpoint, dst jk.Endpoint) {
	src = jk.Endpoint{URL: jk.NormalizeEndpointURL(inst.srcURL), User: strings.TrimSpace(inst.srcUser)}
	dst = jk.Endpoint{URL: jk.NormalizeEndpointURL(inst.dstURL), User: strings.TrimSpace(inst.dstUser)}
	return
}

// clients builds the two sides' clients for ep: metadata clients keep the
// default timeout, scan clients have none and stop on the context.
func clients(src jk.Endpoint, dst jk.Endpoint, scan bool) (s *chclient.Client, d *chclient.Client) {
	var hc *http.Client
	if scan {
		hc = &http.Client{}
	}
	s = chclient.New(jk.SourceClientConfig(src), hc)
	d = chclient.New(jk.TargetClientConfig(dst), hc)
	return
}

// --- results, on the frame goroutine ----------------------------------------

func (inst *App) takeResults() {
	if r, _, ok := inst.discoverJob.TakeResult(); ok {
		inst.disc = r
		inst.seedDatabases()
		inst.note, inst.lastError = "", ""
	}
	for _, job := range []*bgjob.Runner[stepResult]{&inst.structureJob, &inst.diffJob, &inst.syncJob} {
		r, _, ok := job.TakeResult()
		if !ok {
			continue
		}
		inst.stale = r.stale
		if len(r.stale) > 0 {
			inst.lastError = "the plan no longer matches the servers; plan the structure again"
			continue
		}
		p := r.plan
		inst.plan = &p
		inst.note, inst.lastError = r.note, ""
		inst.autosave()
		// The footprint a step leaves behind is worth reading now, not at
		// the next tick.
		inst.disks.Invalidate()
	}
	if r, _, ok := inst.previewJob.TakeResult(); ok {
		inst.preflight = *r
	}
}

// seedDatabases gives every source database a checkbox and a target name. When
// both endpoints are one server, the target name defaults to a suffixed copy,
// since a table cannot be synced onto itself.
func (inst *App) seedDatabases() {
	same := inst.disc.srcEp.URL == inst.disc.dstEp.URL ||
		(inst.disc.src.Server.UUID != "" && inst.disc.src.Server.UUID == inst.disc.dst.Server.UUID)
	for _, db := range inst.disc.src.UserDatabases() {
		if _, has := inst.dbPick[db]; !has {
			v := false
			inst.dbPick[db] = &v
		}
		if _, has := inst.dbTarget[db]; !has {
			t := db
			if same {
				t = db + "_jackstay"
			}
			inst.dbTarget[db] = &t
		}
	}
}

func (inst *App) autosave() {
	if inst.plan == nil || inst.planPath == "" {
		return
	}
	if err := inst.plan.Save(inst.planPath); err != nil {
		inst.lastError = "unable to save the plan: " + err.Error()
	}
}

// clonePlan copies the plan for a worker; the frame keeps drawing the
// original until the worker's result replaces it.
func (inst *App) clonePlan() (p jk.Plan, ok bool) {
	if inst.plan == nil {
		return
	}
	var err error
	p, err = inst.plan.Clone()
	if err != nil {
		inst.lastError = err.Error()
		return
	}
	return p, true
}

// --- the steps' jobs; each worker touches only its own copy -----------------

func (inst *App) startDiscover() {
	srcEp, dstEp := inst.endpoints()
	if srcEp.URL == "" || dstEp.URL == "" {
		inst.lastError = "both a source and a target server are needed"
		return
	}
	src, dst := clients(srcEp, dstEp, false)
	inst.discoverJob.StartReporting(nil, bgjob.Spec{Kind: "jackstay.discover", Title: "discover servers"},
		func(ctx context.Context, report bgjob.Reporter) (*discovered, error) {
			report(0, 0, "reading system tables on both servers")
			s, d, err := jk.DiscoverBoth(ctx, src, dst)
			if err != nil {
				return nil, err
			}
			return &discovered{srcEp: srcEp, dstEp: dstEp, src: s, dst: d}, nil
		})
}

func (inst *App) selection() (sel jk.Selection) {
	sel.LeewayOnly = inst.leewayOnly
	for _, db := range inst.disc.src.UserDatabases() {
		if p := inst.dbPick[db]; p != nil && *p {
			sel.Databases = append(sel.Databases, db)
			if t := strings.TrimSpace(*inst.dbTarget[db]); t != "" && t != db {
				if sel.DatabaseMap == nil {
					sel.DatabaseMap = make(map[string]string, 4)
				}
				sel.DatabaseMap[db] = t
			}
		}
	}
	return
}

func (inst *App) startStructure() {
	if inst.disc == nil {
		return
	}
	sel := inst.selection()
	if len(sel.Databases) == 0 {
		inst.lastError = "choose at least one database"
		return
	}
	srcEp, dstEp := inst.disc.srcEp, inst.disc.dstEp
	var old *jk.Plan
	if p, ok := inst.clonePlan(); ok {
		old = &p
	}
	src, dst := clients(srcEp, dstEp, false)
	inst.applyArmed = false
	inst.structureJob.StartReporting(nil, bgjob.Spec{Kind: "jackstay.structure", Title: "plan the structure"},
		func(ctx context.Context, report bgjob.Reporter) (*stepResult, error) {
			report(0, 0, "judging every selected table")
			p, err := jk.PlanStructure(ctx, src, dst, srcEp, dstEp, sel, old, time.Now())
			if err != nil {
				return nil, err
			}
			return &stepResult{plan: p, note: "structure planned"}, nil
		})
	inst.step = stepStructure
}

func (inst *App) startApply() {
	p, ok := inst.clonePlan()
	if !ok {
		return
	}
	src, dst := clients(p.Source, p.Target, false)
	ddl := chclient.New(jk.DDLClientConfig(jk.TargetClientConfig(p.Target)), nil)
	inst.applyArmed = false
	inst.structureJob.StartReporting(nil, bgjob.Spec{Kind: "jackstay.apply", Title: "apply the DDL"},
		func(ctx context.Context, report bgjob.Reporter) (*stepResult, error) {
			report(0, 0, "rechecking, then running the DDL on the target")
			applied, after, stale, err := jk.ApplyDDLStep(ctx, src, dst, ddl, &p, time.Now())
			if err != nil {
				return nil, err
			}
			return &stepResult{plan: after, stale: stale, applied: applied, note: plural(len(applied), "statement") + " ran on the target"}, nil
		})
}

func (inst *App) startDiff() {
	p, ok := inst.clonePlan()
	if !ok {
		return
	}
	metaS, metaD := clients(p.Source, p.Target, false)
	scanS, scanD := clients(p.Source, p.Target, true)
	final := inst.final
	inst.diffJob.StartReporting(nil, bgjob.Spec{Kind: "jackstay.diff", Title: "compare content"},
		func(ctx context.Context, report bgjob.Reporter) (*stepResult, error) {
			report(0, 0, "rechecking the plan")
			fresh, stale, err := jk.Recheck(ctx, metaS, metaD, &p, time.Now())
			if err != nil || len(stale) > 0 {
				return &stepResult{stale: stale}, err
			}
			opts := jk.DiffOptionsAll{Final: final, Chunking: jk.DefaultChunkingOptions(), Diff: jk.DefaultDiffOptions(),
				Progress: func(i int, n int, pt *jk.PlanTable) {
					note := "done"
					if pt != nil {
						note = pt.Source.String()
					}
					report(uint64(i), uint64(n), note)
				}}
			skipped, err := jk.DiffStep(ctx, scanS, scanD, &fresh, opts, time.Now)
			if err != nil {
				return nil, err
			}
			return &stepResult{plan: fresh, skipped: skipped, note: "content compared"}, nil
		})
}

func (inst *App) tableSync() (ts jk.TableSync, err error) {
	ts = jk.TableSync{Mode: inst.syncMode, Existing: inst.existing}
	if ts.Mode == jk.SyncModeSample {
		ts.SampleNum, ts.SampleDen, err = parseFraction(inst.sampleText)
	}
	return
}

func (inst *App) startPreview() {
	p, ok := inst.clonePlan()
	if !ok {
		return
	}
	ts, err := inst.tableSync()
	if err != nil {
		inst.lastError = err.Error()
		return
	}
	src, dst := clients(p.Source, p.Target, false)
	inst.previewJob.StartReporting(nil, bgjob.Spec{Kind: "jackstay.preflight", Title: "pre-flight"},
		func(ctx context.Context, report bgjob.Reporter) (*[]jk.PreflightDisk, error) {
			report(0, 0, "reading the target's disks")
			chosen, _, err := jk.PrepareSync(ctx, src, &p, ts, nil, jk.DefaultChunkingOptions())
			if err != nil {
				return nil, err
			}
			rep, err := jk.ReadDisks(ctx, dst, targetsOf(chosen))
			if err != nil {
				return nil, err
			}
			out := jk.Preflight(chosen, &rep, 1.5)
			return &out, nil
		})
}

func (inst *App) startSync() {
	if inst.planPath == "" {
		inst.lastError = "save the plan first: the sync journal lives beside it"
		return
	}
	p, ok := inst.clonePlan()
	if !ok {
		return
	}
	ts, err := inst.tableSync()
	if err != nil {
		inst.lastError = err.Error()
		return
	}
	planPath, restart, compression := inst.planPath, inst.restart, inst.compression
	metaS, metaD := clients(p.Source, p.Target, false)
	scanS, scanD := clients(p.Source, p.Target, true)
	inst.rows.Store(0)
	inst.bytes.Store(0)
	inst.logMu.Lock()
	inst.chunkLog = inst.chunkLog[:0]
	inst.logMu.Unlock()
	inst.syncJob.StartReporting(nil, bgjob.Spec{Kind: "jackstay.sync", Title: "sync"},
		func(ctx context.Context, report bgjob.Reporter) (*stepResult, error) {
			report(0, 0, "rechecking the plan")
			fresh, stale, err := jk.Recheck(ctx, metaS, metaD, &p, time.Now())
			if err != nil || len(stale) > 0 {
				return &stepResult{stale: stale}, err
			}
			chosen, skipped, err := jk.PrepareSync(ctx, scanS, &fresh, ts, nil, jk.DefaultChunkingOptions())
			if err != nil {
				return nil, err
			}
			if len(chosen) == 0 {
				return &stepResult{plan: fresh, skipped: skipped, note: "nothing to sync"}, nil
			}
			run, _, err := jk.BeginRun(&fresh, restart, time.Now())
			if err != nil {
				return nil, err
			}
			if err = fresh.Save(planPath); err != nil {
				return nil, err
			}
			j, err := jk.OpenJournal(jk.JournalPath(planPath), run.RunId)
			if err != nil {
				return nil, err
			}
			defer func() { _ = j.Close() }()

			expected := uint64(jk.ExpectedRows(chosen))
			var current atomic.Pointer[string]
			stopFeed := make(chan struct{})
			go func() {
				t := time.NewTicker(250 * time.Millisecond)
				defer t.Stop()
				for {
					select {
					case <-stopFeed:
						return
					case <-t.C:
						note := ""
						if s := current.Load(); s != nil {
							note = *s
						}
						report(uint64(max(inst.rows.Load(), 0)), expected, note)
					}
				}
			}()
			defer close(stopFeed)

			opts := jk.DefaultSyncOptions()
			opts.Compression = compression
			opts.Rows, opts.Bytes = &inst.rows, &inst.bytes
			opts.Progress = func(r jk.ChunkResult) {
				if r.Status == jk.ChunkStatusDone {
					inst.rows.Add(int64(r.Rows))
				}
				inst.logChunk(r)
			}
			floor := jk.DefaultFreeFloor()
			opts.BeforeChunk = func(ctx context.Context, pt *jk.PlanTable) error {
				return floor.WaitForFree(ctx, scanD, pt.Target, func(low []jk.DiskInfo) {
					s := "waiting: a target disk is below the free-space floor"
					current.Store(&s)
				})
			}
			failed, err := jk.SyncStep(ctx, scanS, scanD, chosen, j, opts, time.Now, jk.SyncHooks{
				BeforeTable: func(pt *jk.PlanTable) {
					s := pt.Source.String()
					current.Store(&s)
				},
				AfterTable: func(pt *jk.PlanTable) { _ = fresh.Save(planPath) },
			})
			if err != nil {
				_ = fresh.Save(planPath)
				return nil, err
			}
			note := "sync done; run the diff to compare"
			if failed > 0 {
				note = plural(failed, "chunk") + " not synced; see the tables' problems"
			}
			return &stepResult{plan: fresh, skipped: skipped, note: note}, nil
		})
	inst.step = stepMonitor
}

// logChunk is called from the sync worker.
func (inst *App) logChunk(r jk.ChunkResult) {
	inst.logMu.Lock()
	defer inst.logMu.Unlock()
	if len(inst.chunkLog) == chunkLogCap {
		copy(inst.chunkLog, inst.chunkLog[1:])
		inst.chunkLog = inst.chunkLog[:chunkLogCap-1]
	}
	inst.chunkLog = append(inst.chunkLog, r)
}

func (inst *App) chunkLogSnapshot() (out []jk.ChunkResult) {
	inst.logMu.Lock()
	defer inst.logMu.Unlock()
	return append(out, inst.chunkLog...)
}

// demandDisks reads the target's disks and the footprint of the plan's
// tables, again every five seconds while the Monitor page is shown, and keeps
// the last good read in lastDisks.
func (inst *App) demandDisks() (err error) {
	if inst.plan == nil {
		return
	}
	target := inst.plan.Target
	refs := make([]datacatalog.TableRef, 0, len(inst.plan.Tables))
	for _, t := range inst.plan.Tables {
		if t.Verdict.IsSyncable() {
			refs = append(refs, t.Target)
		}
	}
	key := target.URL + "|" + time.Now().Truncate(5*time.Second).Format(time.RFC3339)
	rep, done, err, _ := inst.disks.Demand(key, func(ctx context.Context) (jk.DiskReport, error) {
		return jk.ReadDisks(ctx, chclient.New(jk.TargetClientConfig(target), nil), refs)
	})
	if done && err == nil {
		inst.lastDisks = &rep
	}
	return
}

func targetsOf(tables []*jk.PlanTable) (refs []datacatalog.TableRef) {
	refs = make([]datacatalog.TableRef, 0, len(tables))
	for _, pt := range tables {
		refs = append(refs, pt.Target)
	}
	return
}

func durationSeconds(s uint64) (d time.Duration) {
	return time.Duration(s) * time.Second
}
