package notebook

// The live reading (ADR-0380): a notebook's records computed on the server
// when it is read, held in the process and shown as a bound notebook
// (ADR-0371 §SD6). The pane is the cell stack with both readings — the
// stored one and the live one — and a line that says which is on screen.
// The notebook window draws it as its Read face; another app embeds it by
// NewEmbedded (notebook_embed.go).
//
// A run is one job: the records are computed (mdnotebook.Compute), the
// notebook is prepared over them, and the computed records are released,
// since the prepared notebook holds its own. It replaces what is shown only
// when every record answered; a run that fails or is cancelled leaves the
// earlier reading on screen and names the record (§SD4).

import (
	"context"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bgjobrow"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

// readingE is which of a notebook's two readings is on screen.
type readingE uint8

const (
	// readingStored is what the notebook stores, read from its blobs.
	readingStored readingE = iota
	// readingLive is the records computed by the last run that answered.
	readingLive
)

const (
	readingNameStored = "stored"
	readingNameLive   = "live"
)

func (inst readingE) String() string {
	if inst == readingLive {
		return readingNameLive
	}
	return readingNameStored
}

// The bounds of one record's result in a live run (ADR-0380 §SD3). A
// record over either fails the run; it is not cut short.
const (
	// LiveMaxRows bounds a record's rows.
	LiveMaxRows int64 = 1 << 20
	// LiveMaxBytes bounds the Arrow stream the server answers a record
	// with, which is close to what the record holds in memory.
	LiveMaxBytes int64 = 256 << 20
)

// liveParallel is how many records a run asks the server for at once.
const liveParallel = 4

// liveLane is the lane a live run's statements are stamped with in the
// server's query log (logcomment.Stamp.Lane).
const liveLane = "notebook.live"

// liveReading is one run that answered, prepared to be drawn.
type liveReading struct {
	prepared *notebookview.Prepared
	check    notebookview.Check
	// textKey is the text the records were computed from.
	textKey string
	at      time.Time
	took    time.Duration
	records int
}

// liveShown is what the line says about the live reading on screen.
type liveShown struct {
	at      time.Time
	took    time.Duration
	records int
}

// paneConfig is how a pane runs and what it draws.
type paneConfig struct {
	// identity is what a run's statements are attributed to: the app id
	// of a window of its own, `<host>#<key>` for an embedded pane.
	identity string
	// scopeKey is the cell stack's widget scope.
	scopeKey string
	// run gives what stands in for the server, asked when a run starts;
	// nil, or a nil answer, sends to the server the environment names,
	// with readonly set.
	run      func() mdnotebook.RunFunc
	maxRows  int64
	maxBytes int64
	// catalog says a click goes through the host's catalog as the
	// window's operations (run_live, show_stored); false acts directly.
	catalog bool
}

// Pane is a notebook shown stored or live: the cell stack, a live run and
// the line that says which reading is on screen.
type Pane struct {
	cfg         paneConfig
	ids         *c.WidgetIdStack
	tasks       task.TaskApiI
	log         zerolog.Logger
	runId       string
	instanceKey uint64

	nb      *mdnotebook.Notebook
	fsys    fs.FS
	textKey string
	// refusal is why the records may not run live, "" when they may
	// (ADR-0380 §SD3); class is the buffer's class.
	refusal string
	class   analysis.QuerySecurityClassE

	shown readingE
	// stored is the lane the stored reading is prepared on; storedSeq is
	// bumped to prepare it again after a live reading replaced it in the
	// view, which released it. storedCheck is its check, storedChecked
	// whether one landed for the text on hand.
	stored        bgjob.Keyed[*reading]
	storedSeq     uint64
	storedCheck   notebookview.Check
	storedChecked bool
	// givenKey names what the view was last handed.
	givenKey string

	// job is the live run; runSeq counts runs started, runDone the ones
	// that ended. live is the live reading on screen, liveCheck its check;
	// lastErr is how the last run that did not answer ended.
	job       bgjob.Runner[liveReading]
	runSeq    uint64
	runDone   uint64
	live      *liveShown
	liveCheck notebookview.Check
	lastErr   string

	view       *notebookview.View
	statements notebookview.StatementsE
	recordings notebookview.RecordingsI
	clicked    bool
}

func newPane(cfg paneConfig) (inst *Pane) {
	if cfg.maxRows == 0 {
		cfg.maxRows = LiveMaxRows
	}
	if cfg.maxBytes == 0 {
		cfg.maxBytes = LiveMaxBytes
	}
	if cfg.scopeKey == "" {
		cfg.scopeKey = "notebook"
	}
	return &Pane{cfg: cfg, ids: c.NewWidgetIdStack(), statements: notebookview.StatementsShown, log: zerolog.Nop()}
}

// mount takes what the pane needs of its host's context.
func (inst *Pane) mount(ctx app.MountContextI) {
	inst.ids = ctx.Ids()
	inst.tasks = task.ForApp(ctx)
	inst.log = ctx.Log()
	inst.runId = ctx.RunId()
	inst.instanceKey = ctx.InstanceKey()
}

// close ends a run and lets go of what is shown.
func (inst *Pane) close() {
	inst.job.Cancel()
	inst.stored.Close()
	if inst.view != nil {
		inst.view.Close()
		inst.view = nil
	}
	inst.givenKey = ""
}

// setDocument makes nb the pane's notebook, nil for none. A live reading of
// another text is dropped and a run of one is cancelled: its records were
// computed from statements the text no longer holds.
func (inst *Pane) setDocument(nb *mdnotebook.Notebook, fsys fs.FS, textKey string) {
	if textKey == inst.textKey && nb == inst.nb {
		return
	}
	inst.nb, inst.fsys, inst.textKey = nb, fsys, textKey
	inst.storedChecked, inst.storedCheck = false, notebookview.Check{}
	inst.refusal, inst.class = classifyRecords(nb)
	if inst.job.Running() {
		inst.job.Cancel()
		inst.lastErr = "the text changed while the records ran; run again"
	}
	if inst.shown == readingLive {
		inst.shown, inst.live, inst.liveCheck = readingStored, nil, notebookview.Check{}
	}
}

// classifyRecords says whether nb's records may run live: their buffer must
// classify read (ADR-0132 §SD5), and a refusal names the construct that
// forced another class.
func classifyRecords(nb *mdnotebook.Notebook) (refusal string, class analysis.QuerySecurityClassE) {
	if nb == nil {
		return "no notebook is open", analysis.QuerySecurityMutating
	}
	sql, err := mdnotebook.Buffer(nb)
	if err != nil {
		return "the records do not make one buffer: " + err.Error(), analysis.QuerySecurityMutating
	}
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return "the records do not parse, so they cannot be classified: " + err.Error(), analysis.QuerySecurityMutating
	}
	class, witnesses, err := analysis.ClassifyQuerySecurity(pr)
	if err != nil {
		return "the records could not be classified: " + err.Error(), analysis.QuerySecurityMutating
	}
	if class == analysis.QuerySecurityRead {
		return "", class
	}
	var b strings.Builder
	b.WriteString("the records classify ")
	b.WriteString(class.String())
	b.WriteString(", and only a notebook whose records read runs live; collect it instead")
	for i, w := range witnesses {
		if i == 0 {
			b.WriteString(". Forced by ")
		} else {
			b.WriteString(", ")
		}
		b.WriteString(w.Describe())
		if w.Name != "" {
			b.WriteString(" ")
			b.WriteString(w.Name)
		}
	}
	return b.String(), class
}

// runnable says whether a run may start, and why not.
func (inst *Pane) runnable() (reason string) {
	switch {
	case inst.nb == nil:
		return "no notebook is open"
	case inst.job.Running():
		return "a run is under way; wait for it to end or cancel it"
	case inst.refusal != "":
		return inst.refusal
	case len(recordNames(inst.nb)) == 0:
		return "the notebook has no record"
	}
	return ""
}

func recordNames(nb *mdnotebook.Notebook) (names []string) {
	if nb == nil {
		return
	}
	for i := range nb.Cells {
		if cell := &nb.Cells[i]; cell.Kind == mdnotebook.CellKindRecord {
			names = append(names, cell.Name)
		}
	}
	return
}

// startRun starts a live run. A refusal names what stands in the way;
// nothing has run then.
func (inst *Pane) startRun() (err error) {
	if reason := inst.runnable(); reason != "" {
		return app.RefuseOperation(reason)
	}
	var run mdnotebook.RunFunc
	if inst.cfg.run != nil {
		run = inst.cfg.run()
	}
	if run == nil {
		run = serverRun(chclient.ConfigFromEnv(), inst.cfg.identity, inst.runId, inst.instanceKey, inst.cfg.maxBytes)
	}
	nb, fsys, textKey := inst.nb, inst.fsys, inst.textKey
	total := uint64(len(recordNames(nb)))
	opts := mdnotebook.ComputeOptions{Parallel: liveParallel, MaxRows: inst.cfg.maxRows, MaxBytes: inst.cfg.maxBytes}
	inst.runSeq++
	inst.lastErr = ""
	inst.job.StartReporting(inst.tasks, bgjob.Spec{Kind: "notebook.live", Title: "Run the notebook live"},
		func(ctx context.Context, report bgjob.Reporter) (*liveReading, error) {
			began := time.Now()
			var answered uint64
			report(0, total, "computing the records")
			opts.Done = func(record string) {
				// Called once per record under Compute's lock.
				answered++
				report(answered, total, "record "+record+" answered")
			}
			records, cErr := mdnotebook.Compute(ctx, nb, run, opts)
			if cErr != nil {
				return nil, cErr
			}
			p := notebookview.PrepareFrom(nb, notebookview.Source{Files: fsys, Records: records})
			for _, r := range records {
				r.Release()
			}
			if ctx.Err() != nil {
				p.Release()
				return nil, ctx.Err()
			}
			return &liveReading{prepared: p, check: p.Check(), textKey: textKey, at: time.Now(), took: time.Since(began), records: len(records)}, nil
		})
	return nil
}

// serverRun sends a statement to the server cfg names — the environment's,
// as for collection — with readonly set and the run's identity stamped in the
// query log. A body is read up to one byte past maxBytes, so a result over
// the bound fails without being held whole.
func serverRun(cfg chclient.Config, identity string, runId string, instanceKey uint64, maxBytes int64) mdnotebook.RunFunc {
	client := chclient.New(cfg, nil)
	// readonly=2 has the server refuse a write or DDL and still admit the
	// settings sent beside the statement, as play's read path does.
	settings := map[string]string{"readonly": "2"}
	if stamp := logcomment.Marshal(logcomment.Stamp{RunId: runId, App: identity, Instance: instanceKey, Lane: liveLane}); stamp != "" {
		settings["log_comment"] = stamp
	}
	return func(ctx context.Context, statement string) (stream []byte, err error) {
		body, err := client.QuerySettings(ctx, statement, settings)
		if err != nil {
			return nil, err
		}
		defer body.Close()
		return io.ReadAll(io.LimitReader(body, maxBytes+1))
	}
}

// showStored returns the view to the stored reading. The live reading is
// let go: running again computes it anew.
func (inst *Pane) showStored() (err error) {
	switch {
	case inst.nb == nil:
		return app.RefuseOperation("no notebook is open")
	case inst.shown == readingStored:
		return nil
	}
	inst.shown, inst.live, inst.liveCheck = readingStored, nil, notebookview.Check{}
	inst.storedSeq++
	return nil
}

// landRun takes a run that ended.
func (inst *Pane) landRun() {
	if res, _, ok := inst.job.TakeResult(); ok {
		inst.runDone = inst.runSeq
		if res.textKey != inst.textKey {
			res.prepared.Release()
			inst.lastErr = "the text changed while the records ran; run again"
			return
		}
		inst.give(res.prepared, "live\x00"+itoa(inst.runSeq))
		inst.shown = readingLive
		inst.live = &liveShown{at: res.at, took: res.took, records: res.records}
		inst.liveCheck = res.check
		inst.lastErr = ""
		return
	}
	if inst.runDone < inst.runSeq {
		if snap := inst.job.Snapshot(); snap.State == bgjob.StateFailed || snap.State == bgjob.StateIdle {
			inst.runDone = inst.runSeq
			if inst.lastErr == "" {
				inst.lastErr = "the run was cancelled"
				if snap.Err != nil {
					inst.lastErr = snap.Err.Error()
				}
			}
		}
	}
}

// give hands the view a prepared notebook, which it owns from here.
func (inst *Pane) give(p *notebookview.Prepared, key string) {
	if inst.view == nil {
		inst.view = notebookview.New(inst.ids, inst.cfg.scopeKey, notebookview.Options{Statements: inst.statements, Recordings: inst.recordings})
	}
	inst.view.Set(p)
	inst.givenKey = key
}

// poll lands a run and the stored reading, and starts what the frame
// needs; busy says work is under way.
func (inst *Pane) poll() (busy bool) {
	inst.landRun()
	busy = inst.job.Running()
	if inst.nb == nil {
		return
	}
	nb, fsys := inst.nb, inst.fsys
	key := inst.textKey + "\x00stored\x00" + itoa(inst.storedSeq)
	r, done, err, preparing := inst.stored.Demand(key, func(context.Context) (*reading, error) {
		p := notebookview.Prepare(nb, fsys)
		return &reading{prepared: p, check: p.Check()}, nil
	})
	if preparing || !done || err != nil || r == nil {
		return busy || preparing || !done
	}
	inst.storedCheck, inst.storedChecked = r.check, true
	if inst.shown == readingStored && inst.givenKey != key {
		inst.give(r.prepared, key)
	}
	return
}

// LiveState is what a pane shows and how its last run ended.
type LiveState struct {
	Reading  string    `desc:"the reading on screen: stored, what the notebook stores; or live, the records computed by the last run that answered"`
	Running  bool      `json:",omitzero" desc:"a run is under way"`
	RunAt    time.Time `json:",omitzero" desc:"when the live reading on screen was computed"`
	TookMs   int64     `json:",omitzero" desc:"how long its run took, in milliseconds"`
	Records  int       `json:",omitzero" desc:"how many records it computed"`
	LastErr  string    `json:",omitzero" desc:"how the last run that did not answer ended: the record it failed on and why. The reading on screen is the earlier one"`
	Refusal  string    `json:",omitzero" desc:"why the notebook may not run live; empty when it may"`
	Class    string    `desc:"the security class of the notebook's records read as one buffer"`
	Problems []string  `json:",omitzero" desc:"of the reading on screen: what its text and its records disagree on"`
	Refused  []string  `json:",omitzero" desc:"of the reading on screen: presentation cells with no answer"`
}

// State is the pane's state as a host or an agent reads it.
func (inst *Pane) State() (st LiveState) {
	st = LiveState{Reading: inst.shown.String(), Running: inst.job.Running(), LastErr: inst.lastErr, Refusal: inst.refusal, Class: inst.class.String()}
	ck := inst.storedCheck
	if inst.shown == readingLive && inst.live != nil {
		st.RunAt, st.TookMs, st.Records = inst.live.at, inst.live.took.Milliseconds(), inst.live.records
		ck = inst.liveCheck
	}
	st.Problems, st.Refused = ck.Problems, ck.Refused
	return
}

const (
	labelRunLive    = "Run live"
	labelRunAgain   = "Run again"
	labelShowStored = "Show stored"
	tipRunLive      = "Compute every record on the server now and show the notebook over the results. Nothing is stored, and the reading on screen stays until every record has answered."
	tipShowStored   = "Show what the notebook stores again. The live reading is let go; running again computes it anew."
)

// paneGesture is a click on the pane: through the host's catalog where the
// pane is the window's own, else directly.
func (inst *Pane) paneGesture(ctx app.FrameContextI, op string, direct func()) {
	if inst.cfg.catalog && ctx != nil {
		gesture(ctx, op, appops.None{}, direct)
		return
	}
	direct()
}

// renderLine draws the line that says which reading is on screen, with
// the gestures that change it (ADR-0380 §SD4).
func (inst *Pane) renderLine(ctx app.FrameContextI) {
	if inst.job.Running() {
		bgjobrow.Render(bgjobrow.Input{Job: &inst.job, Ids: inst.ids, ScopeKey: inst.cfg.scopeKey + "-run", Cancel: true, Title: "Running the notebook live", RateUnit: "records"})
	}
	for range c.HorizontalTop().KeepIter() {
		switch {
		case inst.shown == readingLive && inst.live != nil:
			badge.New(inst.ids.PrepareStr("reading"), "live").Tone(badge.TonePrimary).
				Tooltip("The records were computed on the server when this was run. Nothing of it is stored.").Send()
			c.Label("computed " + inst.live.at.Format("2006-01-02 15:04:05") + " · " + itoa(uint64(inst.live.records)) + " records in " + inst.live.took.Round(time.Millisecond).String()).Send()
		default:
			badge.New(inst.ids.PrepareStr("reading"), "stored").Tone(badge.ToneNeutral).
				Tooltip("What the notebook stores, computed when it was collected. No server is asked.").Send()
			if inst.storedChecked {
				c.Label(storedLine(inst.storedCheck)).Send()
			}
		}
		reason := inst.runnable()
		label := labelRunLive
		if inst.shown == readingLive {
			label = labelRunAgain
		}
		tip := tipRunLive
		if reason != "" && !inst.job.Running() {
			tip = "Not run live: " + reason + "."
		}
		if !inst.job.Running() {
			for range c.EnabledUi(reason == "").KeepIter() {
				for range c.HoverText(tip).KeepIter() {
					if c.Button(inst.ids.PrepareStr("runLive"), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked() {
						inst.paneGesture(ctx, opRunLive, func() {
							if sErr := inst.startRun(); sErr != nil {
								inst.lastErr = sErr.Error()
							}
						})
					}
				}
			}
		}
		if inst.shown == readingLive {
			for range c.HoverText(tipShowStored).KeepIter() {
				if c.Button(inst.ids.PrepareStr("showStored"), c.Atoms().Text(labelShowStored).Keep()).SendResp().HasPrimaryClicked() {
					inst.paneGesture(ctx, opShowStored, func() { _ = inst.showStored() })
				}
			}
		}
	}
	if inst.lastErr != "" {
		c.Label("The last run did not answer: " + inst.lastErr + ". The reading on screen is the earlier one.").Wrap().Send()
	}
}

// storedLine is the stored reading's state in a few words.
func storedLine(ck notebookview.Check) string {
	switch {
	case ck.Sealed():
		return "sealed"
	case len(ck.Problems) == 0 && len(ck.Refused) == 0:
		return "stale: a record was edited since collection"
	}
	return "not sealed"
}

// renderBody draws the cell stack and its controls.
func (inst *Pane) renderBody() {
	if inst.view == nil || inst.givenKey == "" {
		c.Label("Reading the notebook's stored results…").Send()
		return
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label("statements").Send()
		for i, mode := range statementModes {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				if c.RadioButton(inst.ids.PrepareStr("statements"), c.Atoms().Text(mode.label).Keep(), inst.statements == mode.value).SendRespVal(&inst.clicked).HasPrimaryClicked() {
					inst.statements = mode.value
				}
			}
		}
		if c.Button(inst.ids.PrepareStr("foldAll"), c.Atoms().Text("Fold all").Keep()).SendResp().HasPrimaryClicked() {
			inst.view.FoldAll(true)
		}
		if c.Button(inst.ids.PrepareStr("unfoldAll"), c.Atoms().Text("Unfold all").Keep()).SendResp().HasPrimaryClicked() {
			inst.view.FoldAll(false)
		}
	}
	inst.view.Opts.Statements = inst.statements
	inst.view.Opts.Recordings = inst.recordings
	// The view draws into the Ui it is given and scrolls by it; the scroll
	// area is the host's to provide.
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		inst.view.Render()
	}
}

// frame draws the pane: the line, then the cell stack.
func (inst *Pane) frame(ctx app.FrameContextI) {
	if inst.poll() {
		c.RequestRepaint()
	}
	inst.renderLine(ctx)
	inst.renderBody()
}
