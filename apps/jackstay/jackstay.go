// Package jackstay is the wizard of ADR-0259 §SD7: the guided
// ClickHouse-to-ClickHouse sync, one step per page — Connect, Databases,
// Structure, Differences, Sync, Run — over the same plan document and the
// same workflow functions the `boxer jackstay` CLI uses.
//
// The wizard is built for someone who uses it rarely: every page says where
// it sits in the sequence, what the earlier steps produced and what the next
// click will do to which server, so nothing has to be remembered between
// uses. The plan file is proposed by the app, so the sync never refuses for
// want of one, and the plans used before are listed on the first page.
//
// Nothing waits on a server from the frame goroutine. Every step runs as a
// bgjob on a copy of the plan and lands a new plan value, which the frame
// takes, shows and saves. The worker goroutines never call imzero2.
package jackstay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
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
	stepRun
)

var allSteps = []stepE{stepConnect, stepDatabases, stepStructure, stepDifferences, stepSync, stepRun}

// String is the step chip's text, and so the name a headless scene clicks.
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
	case stepRun:
		return "6 Run"
	}
	return "invalid"
}

// short is the step's name without its number, for "Next: Databases".
func (inst stepE) short() (s string) {
	_, s, _ = strings.Cut(inst.String(), " ")
	return
}

// icon is the glyph that stands for the step in the brief beside its page.
func (inst stepE) icon() (glyph string) {
	switch inst {
	case stepConnect:
		return icons.PhPlugsConnected
	case stepDatabases:
		return icons.PhDatabase
	case stepStructure:
		return icons.PhTreeStructure
	case stepDifferences:
		return icons.PhGitDiff
	case stepSync:
		return icons.PhArrowsLeftRight
	case stepRun:
		return icons.PhGauge
	}
	return ""
}

// describe is the step's brief: what it does, and what it touches.
func (inst stepE) describe() (s string) {
	switch inst {
	case stepConnect:
		return "Name the two servers. The source is only read; the target is written only by the steps you confirm later: the DDL on Structure, the copy on Sync. Discovery reads the system tables of both."
	case stepDatabases:
		return "Tick the source databases to sync and say where each lands on the target. Planning the structure judges every table of those databases and writes the plan file. Nothing is written to a server."
	case stepStructure:
		return "Every table gets a verdict: what the target holds, and the DDL that would bring it in line. Applying that DDL is the first write to the target, behind a confirmation that names it."
	case stepDifferences:
		return "Each side scans every table once for leaf digests; small differing leaves are compared row by row. No row moves. The result decides what the sync proposes."
	case stepSync:
		return "Rows are relayed through this machine, chunk by chunk, and every copied chunk is verified by digest. The pre-flight shows what would move and whether the target's disks have room."
	case stepRun:
		return "The sync in flight, or the last run's outcome, with the target's disks and every table's report. A stopped sync resumes from its journal when started again."
	}
	return ""
}

// PlanEnv names a plan file the window opens at start and saves to — the
// seed that lets a headless scene reach the steps that need a saved plan
// without the file dialog.
var PlanEnv = env.NewString(env.Spec{
	Name:        "BOXER_JACKSTAY_PLAN",
	Description: "jackstay window: plan file opened at start and saved to (created on first save when missing)",
	Category:    env.CategoryDatabase,
})

// PlanDirEnv is where the window puts a plan it proposes a path for. Empty
// resolves through [resolvePlanDir].
var PlanDirEnv = env.NewPath(env.Spec{
	Name:        "BOXER_JACKSTAY_PLAN_DIR",
	Description: "jackstay window: directory for plans the window names itself; empty uses <user config dir>/boxer/jackstay",
	Category:    env.CategoryDatabase,
})

// resolvePlanDir is [PlanDirEnv] when set, else <user config dir>/boxer/jackstay,
// else a directory under the temp dir for a host with no config directory.
// A plan's journal lives beside it, so the directory should survive a
// reboot, which rules the cache directory out.
func resolvePlanDir() (dir string) {
	if dir = PlanDirEnv.Get(); dir != "" {
		return dir
	}
	if cfg, err := os.UserConfigDir(); err == nil {
		return filepath.Join(cfg, "boxer", "jackstay")
	}
	return filepath.Join(os.TempDir(), "boxer-jackstay")
}

// recentKey is the persisted key under which the window keeps the plans it
// used, newest first.
const recentKey = "recent-plans"

// recentCap bounds the recent-plans list.
const recentCap = 8

// chunkLogCap bounds the chunk results the Run page lists.
const chunkLogCap = 200

// recentPlan is one row of the first page's "Resume a plan" list.
type recentPlan struct {
	Path   string    `json:"path"`
	Source string    `json:"source"`
	Target string    `json:"target"`
	Step   string    `json:"step"`
	At     time.Time `json:"at"`
}

// discovered is the Connect step's result.
type discovered struct {
	srcEp, dstEp jk.Endpoint
	src, dst     jk.Inventory
}

// same reports whether both endpoints are one server, in which case the
// target databases must be renamed copies.
func (inst *discovered) same() (same bool) {
	return jk.IsSameServer(inst.srcEp, inst.dstEp, inst.src.Server, inst.dst.Server)
}

// stepResult is what a plan-producing step hands back to the frame.
type stepResult struct {
	plan    jk.Plan
	stale   []string
	applied []string
	skipped []string
	note    string
}

// preflightResult is what the Sync page shows before the operator starts:
// the disks' fit and the size of what would move.
type preflightResult struct {
	disks   []jk.PreflightDisk
	tables  int
	rows    int64
	bytes   uint64
	skipped []string
}

// App is the per-window instance.
type App struct {
	ids    *c.WidgetIdStack
	logger zerolog.Logger
	store  app.StorageI

	appCtx    context.Context
	cancelApp context.CancelFunc

	step stepE

	// Connect: bound to text inputs, so fields, never frame locals.
	srcURL, srcUser string
	dstURL, dstUser string
	discoverJob     bgjob.Runner[discovered]
	disc            *discovered
	recent          []recentPlan

	// Databases.
	dbPick     map[string]*bool
	dbTarget   map[string]*string
	leewayOnly bool

	// The plan and its file. planRev counts plan replacements, so a page
	// can tell a new plan from the one it last read.
	plan     *jk.Plan
	planRev  uint64
	planPath string
	savedAt  time.Time
	openDlg  *filepicker.Inst
	saveDlg  *filepicker.Inst

	// Structure and its confirmation.
	structureJob bgjob.Runner[stepResult]
	applyArmed   bool
	selected     datacatalog.TableRef

	// Differences.
	diffJob bgjob.Runner[stepResult]
	final   bool

	// Sync. syncModeChosen is set once the operator picks a mode; until
	// then the page follows the recommendation.
	syncJob        bgjob.Runner[stepResult]
	previewJob     bgjob.Runner[preflightResult]
	preflight      *preflightResult
	preflightKey   string
	syncMode       jk.SyncModeE
	syncModeChosen bool
	existing       jk.ExistingPolicyE
	sampleText     string
	compression    string
	restart        bool
	syncArmed      bool
	rows, bytes    atomic.Int64

	// Run: the last run's timing, kept on the frame goroutine.
	syncStarted, syncFinished time.Time
	syncWasRunning            bool

	logMu    sync.Mutex
	chunkLog []jk.ChunkResult

	// Run: lastDisks is the latest read, kept while the next is taken.
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
	inst.store = ctx.Storage()
	inst.appCtx, inst.cancelApp = context.WithCancel(context.Background())
	inst.openDlg = filepicker.New("jackstay-open", filepicker.ModeOpen,
		filepicker.WithTitle("Open a jackstay plan"), filepicker.WithExtensionFilter(".json"), filepicker.WithStartAtOsHome())
	inst.saveDlg = filepicker.New("jackstay-save", filepicker.ModeSave,
		filepicker.WithTitle("Save the jackstay plan as"), filepicker.WithExtensionFilter(".json"), filepicker.WithDefaultFilename("plan.json"), filepicker.WithStartAtOsHome())
	inst.loadRecent()
	if path := PlanEnv.Get(); path != "" {
		inst.planPath = path
		if p, lerr := jk.LoadPlan(path); lerr == nil {
			inst.adoptPlan(&p, path)
			inst.step = inst.resumeStep()
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

// hostLabel is the short name of a server for chips, captions and file
// names: the URL's host and port, or the string itself when it is not a URL.
func hostLabel(s string) (label string) {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return strings.TrimSuffix(s, "/")
	}
	return u.Host
}

// fileToken makes a host label safe for a file name.
func fileToken(s string) (t string) {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
			return r
		}
		return '-'
	}, s)
}

// --- the plan file and the recent list ----------------------------------------

// adoptPlan makes p the window's plan: the endpoints follow it, the pages
// re-read it, and the file it came from is remembered.
func (inst *App) adoptPlan(p *jk.Plan, path string) {
	inst.plan = p
	inst.planRev++
	inst.planPath = path
	inst.srcURL, inst.srcUser = p.Source.URL, p.Source.User
	inst.dstURL, inst.dstUser = p.Target.URL, p.Target.User
	inst.syncModeChosen = false
	inst.applyArmed, inst.syncArmed = false, false
	inst.preflight, inst.preflightKey = nil, ""
	if path != "" {
		if st, err := os.Stat(path); err == nil {
			inst.savedAt = st.ModTime()
		}
		inst.noteRecent()
	}
}

// resumeStep is the page an opened plan lands on: the furthest step that
// has a result, which is where the operator left off.
func (inst *App) resumeStep() (st stepE) {
	st = stepStructure
	for _, s := range []stepE{stepDifferences, stepSync, stepRun} {
		if done, _ := inst.stepDone(s); done {
			st = s
		}
	}
	return
}

// proposePlanPath names a plan file for a plan that has none, under the plan
// directory, by date and servers, so the sync never waits on a save.
func (inst *App) proposePlanPath(src jk.Endpoint, dst jk.Endpoint) (path string) {
	dir := resolvePlanDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		inst.lastError = "unable to create the plan directory: " + err.Error()
		return ""
	}
	base := time.Now().Format("2006-01-02") + "-" + fileToken(hostLabel(src.URL)) + "-to-" + fileToken(hostLabel(dst.URL))
	path = filepath.Join(dir, base+".json")
	for i := 2; ; i++ {
		if _, err := os.Stat(path); err != nil {
			return path
		}
		path = filepath.Join(dir, base+"-"+strconv.Itoa(i)+".json")
	}
}

func (inst *App) autosave() {
	if inst.plan == nil || inst.planPath == "" {
		return
	}
	if err := inst.plan.Save(inst.planPath); err != nil {
		inst.lastError = "unable to save the plan: " + err.Error()
		return
	}
	inst.savedAt = time.Now()
	inst.noteRecent()
}

// loadRecent reads the recent-plans list; a host without the persist
// capability leaves it empty, which the first page shows as no list.
func (inst *App) loadRecent() {
	if inst.store == nil {
		return
	}
	data, found, err := inst.store.Get(recentKey)
	if err != nil || !found {
		return
	}
	var list []recentPlan
	if json.Unmarshal(data, &list) == nil {
		inst.recent = list
	}
}

// noteRecent moves the current plan to the front of the recent list.
func (inst *App) noteRecent() {
	if inst.plan == nil || inst.planPath == "" {
		return
	}
	step := stepStructure
	for _, s := range []stepE{stepDifferences, stepSync, stepRun} {
		if done, _ := inst.stepDone(s); done {
			step = s
		}
	}
	entry := recentPlan{Path: inst.planPath, Source: hostLabel(inst.plan.Source.URL), Target: hostLabel(inst.plan.Target.URL),
		Step: step.short(), At: time.Now()}
	list := make([]recentPlan, 0, recentCap)
	list = append(list, entry)
	for _, r := range inst.recent {
		if r.Path != entry.Path && len(list) < recentCap {
			list = append(list, r)
		}
	}
	inst.recent = list
	if inst.store == nil {
		return
	}
	if data, err := json.Marshal(list); err == nil {
		if serr := inst.store.Set(recentKey, data); serr != nil {
			inst.logger.Debug().Err(serr).Msg("jackstay: recent plans not persisted")
		}
	}
}

// forgetRecent drops a row whose file has gone.
func (inst *App) forgetRecent(path string) {
	kept := inst.recent[:0]
	for _, r := range inst.recent {
		if r.Path != path {
			kept = append(kept, r)
		}
	}
	inst.recent = kept
	if inst.store != nil {
		if data, err := json.Marshal(inst.recent); err == nil {
			_ = inst.store.Set(recentKey, data)
		}
	}
}

// openPlan loads a plan file and lands on the step it was left at.
func (inst *App) openPlan(path string) {
	p, err := jk.LoadPlan(path)
	if err != nil {
		inst.lastError = "unable to open the plan: " + err.Error()
		if os.IsNotExist(err) {
			inst.forgetRecent(path)
		}
		return
	}
	inst.adoptPlan(&p, path)
	inst.disc = nil
	inst.note, inst.lastError = "plan opened", ""
	inst.step = inst.resumeStep()
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
		inst.planRev++
		inst.note, inst.lastError = r.note, ""
		inst.autosave()
		if !inst.syncModeChosen {
			inst.syncMode, _ = inst.recommendMode()
		}
		// The footprint a step leaves behind is worth reading now, not at
		// the next tick.
		inst.disks.Invalidate()
	}
	if r, _, ok := inst.previewJob.TakeResult(); ok {
		inst.preflight = r
	}
	running := inst.syncJob.Running()
	if inst.syncWasRunning && !running {
		inst.syncFinished = time.Now()
	}
	inst.syncWasRunning = running
}

// seedDatabases gives every source database a checkbox and a target name. When
// both endpoints are one server, the target name defaults to a suffixed copy,
// since a table cannot be synced onto itself.
func (inst *App) seedDatabases() {
	same := inst.disc.same()
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

// --- what the steps have produced ----------------------------------------------

// stepDone reports whether a step has a result, and a few words of it for
// the step bar.
func (inst *App) stepDone(st stepE) (done bool, summary string) {
	p := inst.plan
	switch st {
	case stepConnect:
		if inst.disc != nil {
			s := hostLabel(inst.disc.srcEp.URL) + " → " + hostLabel(inst.disc.dstEp.URL)
			if inst.disc.same() {
				s = hostLabel(inst.disc.srcEp.URL) + ", one server"
			}
			return true, s
		}
		if p != nil {
			return true, hostLabel(p.Source.URL) + " → " + hostLabel(p.Target.URL)
		}
	case stepDatabases:
		if p != nil {
			n := len(p.Selection.Databases)
			s := plural(n, "database")
			if len(p.Selection.DatabaseMap) > 0 {
				s += ", renamed"
			}
			return true, s
		}
	case stepStructure:
		if p != nil {
			s := plural(len(p.Tables), "table")
			if p.HasPendingDDL() {
				s += ", DDL pending"
			}
			return true, s
		}
	case stepDifferences:
		if p != nil {
			same, differ := inst.diffCounts()
			if same+differ > 0 {
				s := strconv.Itoa(same) + " identical"
				if differ > 0 {
					s = plural(differ, "table") + " differ"
					if differ == 1 {
						s = "1 table differs"
					}
				}
				return true, s
			}
		}
	case stepSync:
		if p != nil && p.SyncRun != nil {
			mode := ""
			for _, t := range p.Tables {
				if t.Sync != nil {
					mode = t.Sync.Mode.String()
					break
				}
			}
			return true, mode + ", " + p.SyncRun.StartedAt.Local().Format("Jan 2 15:04")
		}
	case stepRun:
		if p != nil {
			for _, t := range p.Tables {
				if t.SyncReport != nil {
					return true, "last run " + t.SyncReport.FinishedAt.Local().Format("Jan 2 15:04")
				}
			}
		}
	}
	return false, ""
}

// stepLocked reports why a step cannot be visited yet; empty means it can.
func (inst *App) stepLocked(st stepE) (reason string) {
	switch st {
	case stepDatabases:
		if inst.disc == nil && inst.plan != nil {
			return "discover the servers again to change the databases of an opened plan"
		}
		if inst.disc == nil {
			return "discover the servers first"
		}
	case stepStructure:
		if inst.plan == nil {
			return "plan the structure on the Databases step first, or open a plan"
		}
	case stepDifferences, stepSync, stepRun:
		if inst.plan == nil {
			return "plan the structure first"
		}
	}
	return ""
}

// diffCounts tallies the compared tables.
func (inst *App) diffCounts() (same int, differ int) {
	if inst.plan == nil {
		return
	}
	for _, t := range inst.plan.Tables {
		if t.Diff == nil {
			continue
		}
		if t.Diff.IsIdentical() {
			same++
		} else {
			differ++
		}
	}
	return
}

// recommendMode is the sync mode the plan's state calls for, and why: repair
// after a comparison that found differences, full otherwise.
func (inst *App) recommendMode() (mode jk.SyncModeE, why string) {
	same, differ := inst.diffCounts()
	switch {
	case differ > 0:
		return jk.SyncModeRepair, "the last comparison found differences in " + plural(differ, "table") + "; repair makes only those leaves equal"
	case same > 0:
		return jk.SyncModeFull, "every compared table is identical, so there is nothing to repair; a full copy fills empty target tables only"
	}
	return jk.SyncModeFull, "no comparison has been run; full copies every chunk, and refuses a target table that already holds rows"
}

// existingTargets counts the plan's syncable tables that already exist on
// the target, which is where the existing-rows policy matters.
func (inst *App) existingTargets() (n int) {
	if inst.plan == nil {
		return
	}
	for _, t := range inst.plan.Tables {
		if t.Verdict.IsSyncable() && t.Verdict != jk.VerdictCreate {
			n++
		}
	}
	return
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

func (inst *App) pickedCount() (n int) {
	for _, p := range inst.dbPick {
		if p != nil && *p {
			n++
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
	if inst.planPath == "" {
		inst.planPath = inst.proposePlanPath(srcEp, dstEp)
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

// settingsKey names the Sync page's choices, so a change re-runs the
// pre-flight once and a repeat does not.
func (inst *App) settingsKey() (key string) {
	return inst.syncMode.String() + "|" + inst.existing.String() + "|" + inst.sampleText + "|" + strconv.FormatUint(inst.planRev, 10)
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
	inst.lastError = ""
	src, dst := clients(p.Source, p.Target, false)
	inst.previewJob.StartReporting(nil, bgjob.Spec{Kind: "jackstay.preflight", Title: "pre-flight"},
		func(ctx context.Context, report bgjob.Reporter) (*preflightResult, error) {
			report(0, 0, "reading the target's disks")
			chosen, skipped, err := jk.PrepareSync(ctx, src, &p, ts, nil, jk.DefaultChunkingOptions())
			if err != nil {
				return nil, err
			}
			out := preflightResult{tables: len(chosen), rows: jk.ExpectedRows(chosen), skipped: skipped}
			if len(chosen) == 0 {
				return &out, nil
			}
			rep, err := jk.ReadDisks(ctx, dst, targetsOf(chosen))
			if err != nil {
				return nil, err
			}
			out.disks = jk.Preflight(chosen, &rep, 1.5)
			for _, g := range out.disks {
				out.bytes += g.Need
			}
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
	inst.syncArmed = false
	inst.syncStarted, inst.syncFinished = time.Now(), time.Time{}
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
			note := "sync done; compare the content again to confirm"
			if failed > 0 {
				note = plural(failed, "chunk") + " not synced; see the tables' problems"
			}
			return &stepResult{plan: fresh, skipped: skipped, note: note}, nil
		})
	inst.step = stepRun
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
// tables, again every five seconds while the Run page is shown, and keeps
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
