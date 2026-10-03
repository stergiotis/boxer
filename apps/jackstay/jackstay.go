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
// Plans and their journals live in the window's data area, which the
// runtime's fs broker owns (fs.appdata.*): the window names files and never
// holds a path. A plan comes in from elsewhere, or goes out, through the file
// dialogs (Import, Export), which the user answers.
//
// Nothing waits on a server from the frame goroutine. Every step runs as a
// bgjob on a copy of the plan and lands a new plan value, which the frame
// takes, shows and saves. The worker goroutines never call imzero2.
package jackstay

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"slices"
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
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/breadcrumbs"
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

// PlanEnv names a plan in the window's data area that it opens at start and
// saves to — the seed that lets a headless scene reach the steps that need a
// saved plan without a dialog.
var PlanEnv = env.NewString(env.Spec{
	Name:        "BOXER_JACKSTAY_PLAN",
	Description: "jackstay window: plan, by file name in the window's data area (fs.appdata), opened at start and saved to (created on first save when missing)",
	Category:    env.CategoryDatabase,
})

// recentKey is the persisted key under which the window keeps the plans it
// used, newest first.
const recentKey = "recent-plans"

// recentCap bounds the recent-plans list.
const recentCap = 8

// chunkLogCap bounds the chunk results the Run page lists.
const chunkLogCap = 200

// recentPlan is one row of the first page's "Resume a plan" list. Name is
// the plan's file in the data area; rows from before the data area held a
// path instead, and are dropped on load.
type recentPlan struct {
	Name   string    `json:"name"`
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
	saved   planSaved
}

// planSaved is what a worker's save of the plan left: the name it chose for
// a plan that had none, where the broker put it, and the save's error.
type planSaved struct {
	name, location string
	at             time.Time
	err            error
}

// fileOpE is the file gesture a [fileResult] answers.
type fileOpE uint8

const (
	fileOpOpen fileOpE = iota
	fileOpImport
	fileOpExport
)

// fileResult is what a file gesture hands back to the frame.
type fileResult struct {
	op        fileOpE
	plan      jk.Plan
	saved     planSaved
	cancelled bool
	// exported is the name the user saved an exported copy under.
	exported string
}

// preflightResult is what the Sync page shows before the operator starts:
// the disks' fit and the size of what would move.
type preflightResult struct {
	stale   []string
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
	bus    app.BusI
	// files is the window's data area, where plans and journals live.
	files *fsbroker.AppDataClient

	step stepE
	// The breadcrumb's model, refilled every frame, and its state, onto
	// which step is projected before each render.
	crumbs      breadcrumbs.Model
	crumbsState breadcrumbs.State

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

	// Structure: the row filter typed for each source table, by
	// "database.name" (ADR-0271 §SD1).
	filters map[string]*string

	// The plan and its file. planRev counts plan replacements, so a page
	// can tell a new plan from the one it last read. planName is the file in
	// the data area; planLocation is its host path, for display only.
	plan         *jk.Plan
	planRev      uint64
	planName     string
	planLocation string
	savedAt      time.Time
	fileJob      bgjob.Runner[fileResult]

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

	// The status line: the last outcome, or the last error; skipped lists
	// the tables the last diff or sync left out, with the reason.
	note, lastError string
	stale, skipped  []string
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
		filters:     make(map[string]*string, 16),
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
	inst.bus = ctx.Bus()
	inst.files = fsbroker.NewAppDataClient(inst.bus)
	inst.loadRecent()
	if name := PlanEnv.Get(); name != "" {
		if !fsbroker.ValidAppDataName(name) {
			inst.lastError = "BOXER_JACKSTAY_PLAN is not a plan file name: " + name
			return
		}
		// Named before it is read, so a missing plan is created on the
		// first save under the name the scene gave.
		inst.planName = name
		inst.startOpen(name, true)
	}
	return
}

// Unmount cancels whatever runs. A sync stopped this way resumes from its
// journal the next time the plan is synced.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.discoverJob.Cancel()
	inst.structureJob.Cancel()
	inst.diffJob.Cancel()
	inst.syncJob.Cancel()
	inst.previewJob.Cancel()
	inst.fileJob.Cancel()
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
		inst.syncJob.Running() || inst.previewJob.Running() || inst.fileJob.Running()
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
func (inst *App) adoptPlan(p *jk.Plan, saved planSaved) {
	inst.plan = p
	inst.planRev++
	inst.srcURL, inst.srcUser = p.Source.URL, p.Source.User
	inst.dstURL, inst.dstUser = p.Target.URL, p.Target.User
	inst.syncModeChosen = false
	inst.applyArmed, inst.syncArmed = false, false
	inst.preflight, inst.preflightKey = nil, ""
	inst.planName = saved.name
	inst.noteSaved(saved)
}

// noteSaved takes what a save left: the plan's name and place, the time, and
// the recent list; a failed save is the status line's error.
func (inst *App) noteSaved(saved planSaved) {
	if saved.err != nil {
		inst.lastError = "unable to save the plan: " + saved.err.Error()
		return
	}
	if saved.name == "" {
		return
	}
	inst.planName = saved.name
	if saved.location != "" {
		inst.planLocation = saved.location
	}
	if !saved.at.IsZero() {
		inst.savedAt = saved.at
	}
	inst.noteRecent()
}

// furthestStep is where the operator left off: the furthest step that has
// a result, Structure at the least, which is where an opened plan lands.
func (inst *App) furthestStep() (st stepE) {
	st = stepStructure
	for _, s := range []stepE{stepDifferences, stepSync, stepRun} {
		if done, _ := inst.stepDone(s); done {
			st = s
		}
	}
	return
}

// proposePlanName names a plan that has none, by date and servers, so the
// sync never waits on a save; taken are the names already in the data area.
func proposePlanName(src jk.Endpoint, dst jk.Endpoint, now time.Time, taken []fsbroker.AppDataEntry) (name string) {
	base := now.Format("2006-01-02") + "-" + fileToken(hostLabel(src.URL)) + "-to-" + fileToken(hostLabel(dst.URL))
	return freeName(base, ".json", taken)
}

// freeName is base+ext, or base-N+ext for the first N that is not taken.
func freeName(base string, ext string, taken []fsbroker.AppDataEntry) (name string) {
	used := make(map[string]bool, len(taken))
	for _, e := range taken {
		used[e.Name] = true
	}
	name = base + ext
	for i := 2; used[name]; i++ {
		name = base + "-" + strconv.Itoa(i) + ext
	}
	return
}

// savePlan writes p to name in files, from a worker, and reports what the
// save left. An empty name is chosen from the servers first.
func savePlan(files *fsbroker.AppDataClient, name string, p *jk.Plan) (saved planSaved) {
	if name == "" {
		taken, err := files.List()
		if err != nil {
			saved.err = err
			return
		}
		name = proposePlanName(p.Source, p.Target, time.Now(), taken)
	}
	saved.name = name
	data, err := p.Marshal()
	if err != nil {
		saved.err = err
		return
	}
	r, err := files.Write(name, data)
	if err != nil {
		saved.err = err
		return
	}
	saved.location, saved.at = r.Location, time.Unix(0, r.Entry.ModTime)
	return
}

// statPlan reports where a plan the worker did not write itself lives, and
// when it was last written.
func statPlan(files *fsbroker.AppDataClient, name string) (saved planSaved) {
	saved.name = name
	r, err := files.Stat(name)
	if err == nil && !r.NotExist {
		saved.location, saved.at = r.Location, time.Unix(0, r.Entry.ModTime)
	}
	return
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
		kept := list[:0]
		for _, r := range list {
			if r.Name != "" {
				kept = append(kept, r)
			}
		}
		inst.recent = kept
	}
}

// noteRecent moves the current plan to the front of the recent list.
func (inst *App) noteRecent() {
	if inst.plan == nil || inst.planName == "" {
		return
	}
	entry := recentPlan{Name: inst.planName, Source: hostLabel(inst.plan.Source.URL), Target: hostLabel(inst.plan.Target.URL),
		Step: inst.furthestStep().short(), At: time.Now()}
	list := make([]recentPlan, 0, recentCap)
	list = append(list, entry)
	for _, r := range inst.recent {
		if r.Name != entry.Name && len(list) < recentCap {
			list = append(list, r)
		}
	}
	inst.recent = list
	inst.storeRecent()
}

// forgetRecent drops a row whose file has gone.
func (inst *App) forgetRecent(name string) {
	kept := inst.recent[:0]
	for _, r := range inst.recent {
		if r.Name != name {
			kept = append(kept, r)
		}
	}
	inst.recent = kept
	inst.storeRecent()
}

func (inst *App) storeRecent() {
	if inst.store == nil {
		return
	}
	if data, err := json.Marshal(inst.recent); err == nil {
		if serr := inst.store.Set(recentKey, data); serr != nil {
			inst.logger.Debug().Err(serr).Msg("jackstay: recent plans not persisted")
		}
	}
}

// --- file gestures: open, import, export; each runs off the frame ------------

// errPlanGone marks an open of a plan whose file is no longer in the data
// area, so the frame drops its recent row.
var errPlanGone = errors.New("the plan file is gone")

// errPackPlan refuses a plan whose source is a pack: the window streams no
// files through its data area, so a pack is read by the CLI only
// (ADR-0271 §SD4).
var errPackPlan = errors.New("this plan's source is a pack; run it with `boxer jackstay` on the command line")

// startOpen loads a plan from the data area and lands on the step it was left
// at. seed is the start-up open of [PlanEnv], for which a missing file is
// the plan still to be written, not an error.
func (inst *App) startOpen(name string, seed bool) {
	files := inst.files
	inst.fileJob.Start(nil, bgjob.Spec{Kind: "jackstay.open", Title: "open the plan"},
		func(ctx context.Context) (*fileResult, error) {
			p, err := jk.LoadPlanIn(files, name)
			switch {
			case errors.Is(err, fs.ErrNotExist) && seed:
				return &fileResult{op: fileOpOpen, cancelled: true}, nil
			case errors.Is(err, fs.ErrNotExist):
				return &fileResult{op: fileOpOpen, saved: planSaved{name: name, err: errPlanGone}}, nil
			case err != nil:
				return nil, err
			case p.Source.Pack != "":
				return nil, errPackPlan
			}
			return &fileResult{op: fileOpOpen, plan: p, saved: statPlan(files, name)}, nil
		})
}

// startImport asks the user for a plan file, through the broker's picker,
// and copies it into the data area under the name it had, made free.
func (inst *App) startImport() {
	bus, files := inst.bus, inst.files
	inst.fileJob.Start(nil, bgjob.Spec{Kind: "jackstay.import", Title: "import a plan"},
		func(ctx context.Context) (*fileResult, error) {
			reply, err := bus.RequestWithTimeout(fsbroker.SubjectDialogRead, nil, fsbroker.DialogTimeout)
			if err != nil {
				return nil, err
			}
			dr, err := fsbroker.UnmarshalDialogReply(reply)
			if err != nil {
				return nil, err
			}
			if !dr.Granted {
				return &fileResult{op: fileOpImport, cancelled: true}, nil
			}
			body, err := bus.RequestWithTimeout(dr.HandleSubjectPrefix+".read", nil, fsbroker.HandleOpTimeout)
			_, _ = bus.RequestWithTimeout(dr.HandleSubjectPrefix+".close", nil, fsbroker.HandleOpTimeout)
			if err != nil {
				return nil, err
			}
			p, perr := jk.ParsePlan(body)
			if perr != nil {
				// A refused read answers with a denial where the bytes
				// would be; its reason says more than the parse error.
				if r, derr := fsbroker.UnmarshalDialogReply(body); derr == nil && !r.Granted && r.Reason != "" {
					return nil, errors.New(r.Reason)
				}
				return nil, perr
			}
			if p.Source.Pack != "" {
				return nil, errPackPlan
			}
			taken, err := files.List()
			if err != nil {
				return nil, err
			}
			base, ext := importBase(dr.DisplayName)
			saved := savePlan(files, freeName(base, ext, taken), &p)
			if saved.err != nil {
				return nil, saved.err
			}
			return &fileResult{op: fileOpImport, plan: p, saved: saved}, nil
		})
}

// importBase splits a picked file's name into a data area name's base and
// extension, keeping what a name allows.
func importBase(displayName string) (base string, ext string) {
	base, ext = strings.TrimSuffix(displayName, ".json"), ".json"
	base = strings.TrimLeft(fileToken(base), ".")
	if base == "" {
		base = "imported"
	}
	if max := 100; len(base) > max {
		base = base[:max]
	}
	return
}

// startExport asks the user where to put a copy of the plan, through the
// broker's picker. The plan in the data area stays the window's plan, and the
// journal stays beside it.
func (inst *App) startExport() {
	if inst.plan == nil {
		return
	}
	data, err := inst.plan.Marshal()
	if err != nil {
		inst.lastError = err.Error()
		return
	}
	suggested := inst.planName
	if suggested == "" {
		suggested = "plan.json"
	}
	bus := inst.bus
	inst.fileJob.Start(nil, bgjob.Spec{Kind: "jackstay.export", Title: "export the plan"},
		func(ctx context.Context) (*fileResult, error) {
			req, err := fsbroker.MarshalDialogRequest(fsbroker.DialogRequest{SuggestedName: suggested})
			if err != nil {
				return nil, err
			}
			reply, err := bus.RequestWithTimeout(fsbroker.SubjectDialogWrite, req, fsbroker.DialogTimeout)
			if err != nil {
				return nil, err
			}
			dr, err := fsbroker.UnmarshalDialogReply(reply)
			if err != nil {
				return nil, err
			}
			if !dr.Granted {
				return &fileResult{op: fileOpExport, cancelled: true}, nil
			}
			ack, err := bus.RequestWithTimeout(dr.HandleSubjectPrefix+".write", data, fsbroker.HandleOpTimeout)
			_, _ = bus.RequestWithTimeout(dr.HandleSubjectPrefix+".close", nil, fsbroker.HandleOpTimeout)
			if err != nil {
				return nil, err
			}
			wr, err := fsbroker.UnmarshalDialogReply(ack)
			if err != nil {
				return nil, err
			}
			if !wr.Granted {
				return nil, errors.New("the copy was not written: " + wr.Reason)
			}
			return &fileResult{op: fileOpExport, exported: dr.DisplayName}, nil
		})
}

// takeFileResult lands a finished file gesture.
func (inst *App) takeFileResult() {
	r, _, ok := inst.fileJob.TakeResult()
	if !ok || r.cancelled {
		return
	}
	switch r.op {
	case fileOpOpen, fileOpImport:
		if errors.Is(r.saved.err, errPlanGone) {
			inst.lastError = "unable to open the plan: " + r.saved.name + " is no longer in the plan store"
			inst.forgetRecent(r.saved.name)
			return
		}
		p := r.plan
		inst.adoptPlan(&p, r.saved)
		inst.disc = nil
		inst.note, inst.lastError = "plan opened", ""
		if r.op == fileOpImport {
			inst.note = "plan imported as " + r.saved.name
		}
		inst.step = inst.furthestStep()
	case fileOpExport:
		inst.note, inst.lastError = "a copy of the plan was saved as "+r.exported, ""
	}
}

// --- results, on the frame goroutine ----------------------------------------

func (inst *App) takeResults() {
	inst.takeFileResult()
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
		inst.skipped = r.skipped
		inst.noteSaved(r.saved)
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
			s, d, err := jk.DiscoverBoth(ctx, jk.ServerSource(src), dst)
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
	for ref, f := range inst.filters {
		db, _, _ := strings.Cut(ref, ".")
		if fs := strings.TrimSpace(*f); fs != "" && slices.Contains(sel.Databases, db) {
			if sel.Filters == nil {
				sel.Filters = make(map[string]string, 4)
			}
			sel.Filters[ref] = fs
		}
	}
	return
}

// filterText is the bound text of a table's row filter, seeded from the
// plan's filter the first time the table is shown.
func (inst *App) filterText(pt *jk.PlanTable) (text *string) {
	key := pt.Source.String()
	text = inst.filters[key]
	if text == nil {
		f := pt.Filter
		text = &f
		inst.filters[key] = text
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
	files, name := inst.files, inst.planName
	src, dst := clients(srcEp, dstEp, false)
	inst.applyArmed = false
	inst.structureJob.StartReporting(nil, bgjob.Spec{Kind: "jackstay.structure", Title: "plan the structure"},
		func(ctx context.Context, report bgjob.Reporter) (*stepResult, error) {
			report(0, 0, "judging every selected table")
			p, err := jk.PlanStructure(ctx, jk.ServerSource(src), dst, srcEp, dstEp, sel, old, time.Now())
			if err != nil {
				return nil, err
			}
			return &stepResult{plan: p, note: "structure planned", saved: savePlan(files, name, &p)}, nil
		})
	inst.step = stepStructure
}

func (inst *App) startApply() {
	p, ok := inst.clonePlan()
	if !ok {
		return
	}
	src, dst := clients(p.Source, p.Target, false)
	dstCfg := jk.TargetClientConfig(p.Target)
	files, name := inst.files, inst.planName
	inst.applyArmed = false
	inst.structureJob.StartReporting(nil, bgjob.Spec{Kind: "jackstay.apply", Title: "apply the DDL"},
		func(ctx context.Context, report bgjob.Reporter) (*stepResult, error) {
			report(0, 0, "rechecking, then running the DDL on the target")
			guards, err := jk.DDLGuardSettings(ctx, dst)
			if err != nil {
				return nil, err
			}
			ddl := chclient.New(jk.DDLClientConfig(dstCfg, guards), nil)
			applied, after, stale, err := jk.ApplyDDLStep(ctx, jk.ServerSource(src), dst, ddl, &p, time.Now())
			if err != nil {
				return nil, err
			}
			r := &stepResult{plan: after, stale: stale, applied: applied, note: plural(len(applied), "statement") + " ran on the target"}
			if len(stale) == 0 {
				r.saved = savePlan(files, name, &r.plan)
			}
			return r, nil
		})
}

func (inst *App) startDiff() {
	p, ok := inst.clonePlan()
	if !ok {
		return
	}
	scanS, scanD := clients(p.Source, p.Target, true)
	final := inst.final
	files, name := inst.files, inst.planName
	inst.diffJob.StartReporting(nil, bgjob.Spec{Kind: "jackstay.diff", Title: "compare content"},
		func(ctx context.Context, report bgjob.Reporter) (*stepResult, error) {
			report(0, 0, "rechecking the plan")
			opts := jk.DiffOptionsAll{Final: final, Chunking: jk.DefaultChunkingOptions(), Diff: jk.DefaultDiffOptions(),
				Progress: func(i int, n int, pt *jk.PlanTable) {
					note := "done"
					if pt != nil {
						note = pt.Source.String()
					}
					report(uint64(i), uint64(n), note)
				}}
			fresh, skipped, stale, err := jk.DiffStep(ctx, jk.ServerSource(scanS), scanD, &p, opts, time.Now)
			if err != nil || len(stale) > 0 {
				return &stepResult{stale: stale}, err
			}
			return &stepResult{plan: fresh, skipped: skipped, note: "content compared", saved: savePlan(files, name, &fresh)}, nil
		})
}

// syncRequest is the Sync page's choices as the engine takes them.
func (inst *App) syncRequest() (req jk.SyncRequest, err error) {
	req = jk.SyncRequest{TableSync: jk.TableSync{Mode: inst.syncMode, Existing: inst.existing},
		Restart: inst.restart, Chunking: jk.DefaultChunkingOptions(), Headroom: 1.5}
	if req.Mode == jk.SyncModeSample {
		req.SampleNum, req.SampleDen, err = parseFraction(inst.sampleText)
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
	req, err := inst.syncRequest()
	if err != nil {
		inst.lastError = err.Error()
		return
	}
	inst.lastError = ""
	src, dst := clients(p.Source, p.Target, false)
	inst.previewJob.StartReporting(nil, bgjob.Spec{Kind: "jackstay.preflight", Title: "pre-flight"},
		func(ctx context.Context, report bgjob.Reporter) (*preflightResult, error) {
			report(0, 0, "rechecking the plan, reading the target's disks")
			prep, err := jk.PrepareSyncStep(ctx, jk.ServerSource(src), dst, &p, req)
			if err != nil {
				return nil, err
			}
			out := preflightResult{stale: prep.Stale, tables: len(prep.Chosen), rows: prep.ExpectedRows, skipped: prep.Skipped, disks: prep.Disks}
			for _, g := range prep.Disks {
				out.bytes += g.Need
			}
			return &out, nil
		})
}

func (inst *App) startSync() {
	if inst.planName == "" {
		inst.lastError = "plan the structure first: the sync journal lives beside the plan"
		return
	}
	p, ok := inst.clonePlan()
	if !ok {
		return
	}
	req, err := inst.syncRequest()
	if err != nil {
		inst.lastError = err.Error()
		return
	}
	files, planName, compression := inst.files, inst.planName, inst.compression
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
			prep, err := jk.PrepareSyncStep(ctx, jk.ServerSource(scanS), scanD, &p, req)
			if err != nil || len(prep.Stale) > 0 {
				return &stepResult{stale: prep.Stale}, err
			}
			if len(prep.Chosen) == 0 {
				return &stepResult{plan: prep.Plan, skipped: prep.Skipped, note: "nothing to sync", saved: savePlan(files, planName, &prep.Plan)}, nil
			}

			expected := uint64(prep.ExpectedRows)
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
			opts.BeforeTable = func(pt *jk.PlanTable) {
				s := pt.Source.String()
				current.Store(&s)
			}
			out, err := jk.RunSyncIn(ctx, jk.ServerSource(scanS), scanD, &prep, files, planName, req.Restart, opts, time.Now)
			if err != nil {
				return nil, err
			}
			note := "sync done; compare the content again to confirm"
			if out.Failed > 0 {
				note = plural(out.Failed, "chunk") + " not synced; see the tables' problems"
			}
			// RunSyncIn saved the plan; what is left to learn is where.
			return &stepResult{plan: prep.Plan, skipped: prep.Skipped, note: note, saved: statPlan(files, planName)}, nil
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
	rep, done, err, busy := inst.disks.Demand(key, func(ctx context.Context) (jk.DiskReport, error) {
		return jk.ReadDisks(ctx, chclient.New(jk.TargetClientConfig(target), nil), refs)
	})
	if done && err == nil {
		inst.lastDisks = &rep
	}
	if busy {
		c.RequestRepaint()
	}
	return
}

func durationSeconds(s uint64) (d time.Duration) {
	return time.Duration(s) * time.Second
}
