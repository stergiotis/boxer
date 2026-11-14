package notebook

import (
	"context"
	"io/fs"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/externaledit"
	"github.com/stergiotis/boxer/public/keelson/runtime/ladingpublish"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/samples"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bgjobrow"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/editexternally"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

// faceE is which of the two ways the document is shown.
type faceE uint8

const (
	// faceRead is the cell stack over what the notebook stores.
	faceRead faceE = iota
	// faceRecords is the embedded play over the notebook's records.
	faceRecords
)

const (
	faceNameRead    = "read"
	faceNameRecords = "records"
)

func (inst faceE) String() string {
	if inst == faceRecords {
		return faceNameRecords
	}
	return faceNameRead
}

// playMaxHistory bounds the embedded play's result history.
const playMaxHistory = 25

// document is the notebook the window holds.
type document struct {
	// name is the notebook's file name and origin where it was opened from.
	name   string
	origin string
	text   []byte
	// fsys is the notebook's directory: where the blobs it names are found.
	fsys fs.FS
	// ref names the document in the lading store, nil for one that is not
	// of a snapshot.
	ref *snapshotRef
	// edited says the text is no longer the one that was opened: a buffer
	// was taken back into it.
	edited bool
}

// opened is a snapshot's notebook with the connection it was read through.
type opened struct {
	doc   *document
	store *storeConn
}

// reading is one text read and answered, off the render goroutine.
type reading struct {
	prepared *notebookview.Prepared
	check    notebookview.Check
}

// App is one window.
type App struct {
	ids  *c.WidgetIdStack
	bus  app.BusI
	log  zerolog.Logger
	face faceE

	// The embedded play is built at the first use of the Records face,
	// from what Mount kept: the environment's endpoint binds late.
	runId       string
	instanceKey uint64
	inner       *play.PlayApp
	innerErr    string
	// innerOf is the text whose records the embedded play's buffer was last
	// given.
	innerOf string

	doc     *document
	nb      *mdnotebook.Notebook
	docErr  string
	textKey string

	// open is the lane a snapshot is opened on; store is its connection,
	// made on the lane, handed over with the document and closed at
	// Unmount.
	open    bgjob.Keyed[*opened]
	opening *snapshotRef
	store   *storeConn

	// read is the lane the text is read and answered on. given is the
	// reading the view was last handed, check its report and checked
	// whether one has landed for the text on screen.
	view    *notebookview.View
	read    bgjob.Keyed[*reading]
	given   *reading
	check   notebookview.Check
	checked bool

	// collect is the window's collection (notebook_collect.go); tasks is
	// where its run is a task. run and publish stand in for the server and
	// the store's write path in a test; nil takes the real ones.
	collect collectState
	tasks   task.TaskApiI
	run     mdnotebook.RunFunc
	publish func(ladingpublish.Request) (ladingpublish.Result, error)

	// browse is the panel that lists the store's markdown files: shown
	// says it is open, seq names the listing it wants, and list is the lane
	// one is read on.
	browse struct {
		shown bool
		seq   uint64
		list  bgjob.Keyed[*listing]
	}

	// externalEdit is the notebook's text handed to the host's editor app,
	// whose saves come back as the text (ADR-0365): how prose and
	// presentation cells are edited. It ends when the window turns to
	// another document.
	externalEdit externaledit.Editing

	// handed is a text a launch carried for the snapshot being opened: put
	// in place of the snapshot's when it lands, as an edit of it.
	handed     string
	handedName string
	// scope is the window's lifetime, where a hand-over's round trip runs;
	// ownScope says the host gave none. handNote is what the last one came
	// to, written off the frame under mu, and handBusy that one is in
	// flight.
	scope    *lifetime.Scope
	ownScope bool
	mu       sync.Mutex
	handNote string
	handBusy bool

	// note is the outcome of the last gesture, shown in the status row.
	note string
	// clicked is where the face buttons write.
	clicked bool
}

var _ app.AppI = (*App)(nil)
var _ app.OperationsAppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{ids: c.NewWidgetIdStack()}
	inst.collect.tier = defaultTier()
	return
}

func itoa(n uint64) string { return strconv.FormatUint(n, 10) }

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Operations serves the catalog for this window.
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.bus = ctx.Bus()
	inst.log = ctx.Log()
	inst.runId = ctx.RunId()
	inst.instanceKey = ctx.InstanceKey()
	inst.tasks = task.ForApp(ctx)
	if inst.scope = app.LifetimeOf(ctx); inst.scope == nil {
		// A host without a lifetime scope (a bare test context).
		inst.scope, inst.ownScope = lifetime.New(nil), true
	}
	// A launch config wins over the seeds (ADR-0135 §SD7).
	handed, err := inst.handOver(ctx.LaunchConfig())
	if handed || err != nil {
		return
	}
	switch {
	case SampleSeed.Get() != "":
		err = inst.openSample(SampleSeed.Get())
	case SnapshotSeed.Get() != "":
		var ref snapshotRef
		if ref, err = parseSnapshotRef(SnapshotSeed.Get()); err == nil {
			inst.opening = &ref
		}
	}
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.externalEdit.End()
	inst.collect.job.Cancel()
	inst.browse.list.Close()
	inst.open.Close()
	inst.read.Close()
	if inst.view != nil {
		inst.view.Close()
		inst.view = nil
	}
	if inst.inner != nil {
		inst.inner.Close()
		inst.inner = nil
	}
	if inst.store != nil {
		inst.store.close()
		inst.store = nil
	}
	if inst.ownScope && inst.scope != nil {
		inst.scope.Retire()
	}
	return
}

// sampleFile is the sample's file for a name given with or without `.md`.
func sampleFile(name string) (file string, found bool) {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".md")
	for _, f := range samples.Names() {
		if strings.TrimSuffix(f, ".md") == name {
			return f, true
		}
	}
	return "", false
}

// openSample makes a sample the window's document.
func (inst *App) openSample(name string) (err error) {
	file, found := sampleFile(name)
	if !found {
		return eb.Build().Str("sample", name).Errorf("no sample of that name: the samples are minimal, report and renderers")
	}
	text, err := fs.ReadFile(samples.FS(), file)
	if err != nil {
		return
	}
	inst.opening = nil
	inst.setDocument(&document{name: file, origin: "sample " + file, text: text, fsys: samples.FS()})
	return
}

// setText puts text in place of the document's, as an edit of it: the
// records it changed are stale until the notebook is collected.
func (inst *App) setText(text []byte) (changed bool) {
	if inst.doc == nil || string(text) == string(inst.doc.text) {
		return false
	}
	doc := *inst.doc
	doc.text, doc.edited = text, true
	inst.setDocument(&doc)
	return true
}

// setDocument makes doc the window's document and reads its text.
func (inst *App) setDocument(doc *document) {
	if inst.doc != nil && inst.doc.origin != doc.origin {
		// A text handed to the editor was the other document's.
		inst.externalEdit.End()
	}
	inst.doc = doc
	inst.nb, inst.docErr = nil, ""
	inst.checked, inst.check = false, notebookview.Check{}
	inst.textKey = doc.origin + "\x00" + mdnotebook.BlobHash(doc.text)
	nb, err := mdnotebook.Read(doc.text)
	if err != nil {
		inst.docErr = err.Error()
		return
	}
	inst.nb = nb
}

// setRecords writes a play buffer into the document's record cells
// (mdnotebook.SetRecords) and reports how many statements changed.
func (inst *App) setRecords(sql string, sink string) (changed bool, err error) {
	if inst.doc == nil || inst.nb == nil {
		return false, app.RefuseOperation("no notebook is open")
	}
	text, err := mdnotebook.SetRecords(inst.nb, sql, sink)
	if err != nil {
		return false, app.RefuseOperation(err.Error())
	}
	return inst.setText(text), nil
}

// records are the notebook's record names, in document order.
func (inst *App) records() (names []string) {
	if inst.nb == nil {
		return
	}
	for i := range inst.nb.Cells {
		if cell := &inst.nb.Cells[i]; cell.Kind == mdnotebook.CellKindRecord {
			names = append(names, cell.Name)
		}
	}
	return
}

// ensurePlay builds the embedded play at its first use and gives it the
// notebook's records as its buffer whenever the text changed.
func (inst *App) ensurePlay() {
	if inst.nb == nil {
		return
	}
	if inst.inner == nil && inst.innerErr == "" {
		client := play.NewClient(play.ClientConfig{
			URL:      clickhouseenv.URL.Get(),
			User:     clickhouseenv.User.Get(),
			Password: clickhouseenv.Password.Get(),
		}, nil)
		client.SetStampIdentity(inst.runId, string(AppId), inst.instanceKey)
		sql, err := mdnotebook.Buffer(inst.nb)
		if err != nil {
			inst.innerErr = err.Error()
			return
		}
		inst.inner = play.NewLivePlayApp(client, sql, playMaxHistory, nil)
		// nil storage: the buffer is the notebook's, not a draft play keeps.
		inst.inner.SetCapabilities(inst.bus, nil, inst.log)
		inst.innerOf = inst.textKey
		return
	}
	if inst.inner != nil && inst.innerOf != inst.textKey {
		inst.innerOf = inst.textKey
		if sql, err := mdnotebook.Buffer(inst.nb); err == nil && sql != inst.inner.BufferSql() {
			inst.inner.ReplaceSql(sql)
		}
	}
}

// poll lands what the lanes finished and starts what the frame needs; busy
// says a lane is still at work.
func (inst *App) poll() (busy bool) {
	if ref := inst.opening; ref != nil {
		target := *ref
		o, done, err, opening := inst.open.Demand(target.String(), func(ctx context.Context) (*opened, error) {
			sc, cErr := connectLading(ctx)
			if cErr != nil {
				return nil, cErr
			}
			doc, oErr := sc.open(target)
			if oErr != nil {
				sc.close()
				return nil, oErr
			}
			return &opened{doc: doc, store: sc}, nil
		})
		switch {
		case opening || !done:
			return true
		case err != nil || o == nil:
			inst.opening = nil
			msg := ""
			if err != nil {
				msg = "the snapshot could not be opened: " + err.Error()
			}
			switch {
			case inst.handed != "":
				// The text that was handed over is still the document: it
				// is shown with no stored result.
				inst.setDocument(&document{name: inst.handedName, origin: "handed over as " + inst.handedName, text: []byte(inst.handed), fsys: emptyFS{}})
				inst.handed = ""
				inst.note = msg
			case inst.doc == nil:
				inst.docErr = msg
			default:
				// What was open stays open.
				inst.note = msg
			}
		default:
			inst.opening = nil
			if inst.store != nil {
				inst.store.close()
			}
			inst.store = o.store
			inst.setDocument(o.doc)
			if inst.handed != "" {
				if inst.setText([]byte(inst.handed)) {
					inst.note = "the text that was handed over is shown over the snapshot's stored results; what it changed is not stored until the notebook is collected"
				}
				inst.handed = ""
			}
		}
	}
	if inst.nb == nil {
		return false
	}
	nb, fsys := inst.nb, inst.doc.fsys
	r, done, err, reading := inst.read.Demand(inst.textKey, func(context.Context) (*reading, error) {
		p := notebookview.Prepare(nb, fsys)
		return &reading{prepared: p, check: p.Check()}, nil
	})
	if reading || !done || err != nil || r == nil {
		return reading || !done
	}
	if inst.view == nil {
		inst.view = notebookview.New(inst.ids, "notebook", notebookview.Options{Statements: notebookview.StatementsFolded})
	}
	if inst.given != r {
		// The view takes the prepared notebook and releases it; the lane's
		// dispose must not release it a second time.
		inst.view.Set(r.prepared)
		inst.given = r
		inst.check, inst.checked = r.check, true
	}
	return false
}

const (
	labelRead      = "Read"
	labelRecords   = "Records"
	labelTakeBack  = "Take the buffer back"
	labelCollect   = "Collect…"
	labelOpen      = "Open…"
	tipOpen        = "List the markdown files of the lading store, the newest snapshot of each mount, and open one."
	labelCollectGo = "Collect and publish"
	tipCollect     = "Run every record once on the server and publish the notebook with its stored results as a new snapshot. Asks for the confidentiality tier first."
	tipTier        = "The tier is your statement of how confidential the stored results are: the window cannot know what the records read. The store refuses a tier above its ceiling."
	tipTakeBack    = "Write the buffer's statements into the notebook's record cells. Prose and presentation cells are kept. A record whose statement changed is stale until the notebook is collected."
)

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.landCollect()
	inst.landHandOver()
	if inst.poll() || inst.collect.job.Running() {
		c.RequestRepaint()
	}
	inst.renderToolbar(ctx)
	inst.renderBrowse(ctx)
	inst.renderCollect(ctx)
	inst.renderStatus()
	c.Separator().Send()
	switch {
	case inst.doc == nil && inst.opening != nil:
		c.Label("Opening the notebook from the snapshot…").Send()
	case inst.doc == nil:
		inst.renderEmpty(ctx)
	case inst.nb == nil:
		c.Label("The text could not be read as a notebook: " + inst.docErr).Wrap().Send()
	case inst.face == faceRecords:
		err = inst.renderRecords(ctx)
	default:
		inst.renderRead()
	}
	return
}

// gesture runs a command of the catalog as the person, or directly where no
// host serves the catalog (a tour, a test): one path for a click and an
// agent (ADR-0269 §SD8).
func gesture[In any](ctx app.FrameContextI, op string, in In, direct func()) {
	if _, gErr := appops.Gesture[In, appops.None](ctx, op, in); gErr != nil {
		direct()
	}
}

func (inst *App) renderToolbar(ctx app.FrameContextI) {
	for range c.HorizontalTop().KeepIter() {
		for i, f := range []struct {
			face  faceE
			label string
		}{{faceRead, labelRead}, {faceRecords, labelRecords}} {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				if c.RadioButton(inst.ids.PrepareStr("face"), c.Atoms().Text(f.label).Keep(), inst.face == f.face).SendRespVal(&inst.clicked).HasPrimaryClicked() {
					face := f.face
					gesture(ctx, opShowFace, ShowFaceArgs{Face: face.String()}, func() { inst.face = face })
				}
			}
		}
		if inst.face == faceRecords && inst.inner != nil {
			for range c.HoverText(tipTakeBack).KeepIter() {
				if c.Button(inst.ids.PrepareStr("takeBack"), c.Atoms().Text(labelTakeBack).Keep()).Kind(c.ButtonKindPrimary).
					SendResp().HasPrimaryClicked() {
					sql := inst.inner.BufferSql()
					gesture(ctx, opSetRecords, SetRecordsArgs{Sql: sql}, func() { inst.takeBack(sql) })
				}
			}
		}
		// Edit in the host's editor app: the text is handed over and each
		// save replaces it, unless it was changed here meanwhile. Drawn
		// only where the host registers one editor app (ADR-0365).
		if _, ok := externaledit.Editor(); ok && inst.doc != nil && inst.bus != nil {
			if text, replace := editexternally.Render(editexternally.Input{
				Ids: inst.ids, ScopeKey: "editExternally", Editing: &inst.externalEdit, Bus: inst.bus,
				Name: inst.doc.name, Text: string(inst.doc.text), Disabled: inst.collect.job.Running(),
			}); replace {
				if inst.setText([]byte(text)) {
					inst.note = "the editor's save is the notebook's text; what it changed is not stored until the notebook is collected"
				}
			}
		}
		inst.renderEditInMdedit()
		if !inst.browse.shown {
			for range c.HoverText(tipOpen).KeepIter() {
				if c.Button(inst.ids.PrepareStr("open"), c.Atoms().Text(labelOpen).Keep()).SendResp().HasPrimaryClicked() {
					inst.browse.shown = true
				}
			}
		}
		if inst.nb != nil && !inst.collect.armed && !inst.collect.job.Running() {
			for range c.HoverText(tipCollect).KeepIter() {
				if c.Button(inst.ids.PrepareStr("collect"), c.Atoms().Text(labelCollect).Keep()).SendResp().HasPrimaryClicked() {
					inst.collect.armed = true
				}
			}
		}
	}
}

// renderCollect draws the confirmation of a collection, where the person
// states the tier, and the row of a running one.
func (inst *App) renderCollect(ctx app.FrameContextI) {
	cs := &inst.collect
	if cs.job.Running() {
		bgjobrow.Render(bgjobrow.Input{Job: &cs.job, Ids: inst.ids, ScopeKey: "collectJob", Cancel: true, Title: "Collecting", RateUnit: "records"})
		return
	}
	if !cs.armed || inst.nb == nil {
		return
	}
	if reason := inst.collectable(); reason != "" {
		for range c.HorizontalTop().KeepIter() {
			c.Label("Not collected: " + reason + ".").Wrap().Send()
			if c.Button(inst.ids.PrepareStr("collectDismiss"), c.Atoms().Text("Close").Keep()).SendResp().HasPrimaryClicked() {
				cs.armed = false
			}
		}
		return
	}
	c.Label("Collecting runs the " + itoa(uint64(len(inst.records()))) + " records on the server and publishes the notebook with its stored results to the lading store, " +
		"where everyone who reads the store reads them. State how confidential what the records read is:").Wrap().Send()
	for range c.HorizontalTop().KeepIter() {
		for i, tier := range collectTiers {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				if c.RadioButton(inst.ids.PrepareStr("tier"), c.Atoms().Text(tier.String()).Keep(), cs.tier == tier).SendRespVal(&inst.clicked).HasPrimaryClicked() {
					cs.tier = tier
				}
			}
		}
		for range c.HoverText(tipTier).KeepIter() {
			if c.Button(inst.ids.PrepareStr("collectGo"), c.Atoms().Text(labelCollectGo).Keep()).Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked() {
				tier := cs.tier
				gesture(ctx, opCollectNotebook, CollectArgs{Tier: tier.String()}, func() {
					if sErr := inst.startCollect(tier); sErr != nil {
						inst.note = "not collected: " + sErr.Error()
					}
				})
			}
		}
		if c.Button(inst.ids.PrepareStr("collectCancel"), c.Atoms().Text("Cancel").Keep()).SendResp().HasPrimaryClicked() {
			cs.armed = false
		}
	}
}

// openSnapshot makes the window open the notebook ref names; the read runs
// on a lane and lands in a later frame.
func (inst *App) openSnapshot(ref snapshotRef) {
	inst.opening = &ref
	inst.browse.shown = false
	inst.note = "opening " + ref.String()
}

// renderBrowse draws the panel that lists the store's markdown files, the
// newest snapshot of each mount.
func (inst *App) renderBrowse(ctx app.FrameContextI) {
	b := &inst.browse
	if !b.shown {
		return
	}
	l, done, err, busy := b.list.Demand(itoa(b.seq), func(ctx context.Context) (*listing, error) {
		sc, cErr := connectLading(ctx)
		if cErr != nil {
			return nil, cErr
		}
		defer sc.close()
		return sc.list(ctx)
	})
	for range c.HorizontalTop().KeepIter() {
		c.Label("Open from the lading store").Send()
		if c.Button(inst.ids.PrepareStr("browseRefresh"), c.Atoms().Text("Refresh").Keep()).SendResp().HasPrimaryClicked() {
			b.seq++
		}
		if c.Button(inst.ids.PrepareStr("browseClose"), c.Atoms().Text("Close").Keep()).SendResp().HasPrimaryClicked() {
			b.shown = false
		}
	}
	switch {
	case busy || !done:
		c.RequestRepaint()
		c.Label("Reading the lading store…").Send()
	case err != nil:
		c.Label("The lading store could not be read: " + err.Error()).Wrap().Send()
	case l == nil || len(l.files) == 0:
		c.Label("The lading store holds no markdown file. Collecting a notebook puts it there, and so does taking a folder of notebooks into the store.").Wrap().Send()
	default:
		line := itoa(uint64(len(l.files))) + " markdown files, the newest snapshot of each mount; one that holds a record is a notebook"
		if l.truncated {
			line = "the first " + line + " (the store holds more)"
		}
		c.Label(line).Send()
		for range c.ScrollArea().Vscroll(true).MaxHeight(220).AutoShrink(false, true).KeepIter() {
			for i := range l.files {
				f := l.files[i]
				for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
					for range c.HorizontalTop().KeepIter() {
						if c.Button(inst.ids.PrepareStr("stored"), c.Atoms().Text(f.mount+" · "+f.ref.path).Keep()).SendResp().HasPrimaryClicked() {
							ref := f.ref
							gesture(ctx, opOpenSnapshot, OpenSnapshotArgs{Snapshot: ref.String()}, func() { inst.openSnapshot(ref) })
						}
						c.Label("snapshot of " + f.ref.snap.UTC().Format("2006-01-02 15:04") + " UTC").Send()
					}
				}
			}
		}
	}
	c.Separator().Send()
}

// collectTiers are the tiers the confirmation offers, lowest first.
var collectTiers = []agentrun.TierE{agentrun.TierClear, agentrun.TierGreen, agentrun.TierAmber, agentrun.TierAmberStrict, agentrun.TierRed}

// takeBack is the person's take-back where no host serves the catalog.
func (inst *App) takeBack(sql string) {
	changed, err := inst.setRecords(sql, "")
	inst.noteTakeBack(changed, err)
}

func (inst *App) noteTakeBack(changed bool, err error) {
	switch {
	case err != nil:
		inst.note = "not taken back: " + err.Error()
	case changed:
		inst.note = "the buffer's statements are in the text; the records they changed are stale until the notebook is collected"
	default:
		inst.note = "the buffer holds the notebook's statements as they are; nothing changed"
	}
}

func (inst *App) renderStatus() {
	for range c.HorizontalTop().KeepIter() {
		switch {
		case inst.doc == nil:
			badge.New(inst.ids.PrepareStr("state"), "nothing open").Tone(badge.ToneNeutral).Send()
		case inst.nb == nil:
			badge.New(inst.ids.PrepareStr("state"), "not a notebook").Tone(badge.ToneError).Send()
		case !inst.checked:
			badge.New(inst.ids.PrepareStr("state"), "reading").Tone(badge.ToneInfo).Send()
		case inst.check.Sealed():
			badge.New(inst.ids.PrepareStr("state"), "sealed").Tone(badge.ToneSuccess).Send()
		default:
			badge.New(inst.ids.PrepareStr("state"), "not sealed").Tone(badge.ToneWarning).Send()
		}
		if inst.doc != nil {
			if inst.doc.edited {
				badge.New(inst.ids.PrepareStr("edited"), "edited").Tone(badge.ToneWarning).
					Tooltip("The text is no longer the one that was opened: a buffer was taken back into it.").Send()
			}
			c.Label(inst.doc.origin).Send()
			if inst.checked {
				c.Label(checkLine(inst.records(), inst.check)).Send()
			}
		}
	}
	if inst.note != "" {
		c.Label(inst.note).Wrap().Send()
	}
}

// checkLine is the check in one line: what a scene reads.
func checkLine(records []string, ck notebookview.Check) string {
	return "check: " + strconv.Itoa(len(records)) + " records · " +
		strconv.Itoa(len(ck.Problems)) + " problems · " +
		strconv.Itoa(len(ck.Stale)+len(ck.Derived)) + " stale · " +
		strconv.Itoa(len(ck.Refused)) + " cells refused"
}

func (inst *App) renderEmpty(ctx app.FrameContextI) {
	if inst.docErr != "" {
		c.Label(inst.docErr).Wrap().Send()
	}
	c.Label("No notebook is open. A notebook is opened from a lading snapshot, where its stored results lie beside it: Open… lists the store. " +
		"The samples are at hand too, and collecting one puts it in the store:").Wrap().Send()
	for range c.HorizontalTop().KeepIter() {
		for i, file := range samples.Names() {
			name := strings.TrimSuffix(file, ".md")
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				if c.Button(inst.ids.PrepareStr("sample"), c.Atoms().Text("Open sample "+name).Keep()).SendResp().HasPrimaryClicked() {
					gesture(ctx, opOpenSample, OpenSampleArgs{Name: name}, func() {
						if oErr := inst.openSample(name); oErr != nil {
							inst.note = oErr.Error()
						}
					})
				}
			}
		}
	}
}

func (inst *App) renderRead() {
	if inst.view == nil || inst.given == nil {
		c.Label("Reading the notebook's stored results…").Send()
		return
	}
	// The view draws into the Ui it is given and scrolls by it; the scroll
	// area is the host's to provide.
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		inst.view.Render()
	}
}

func (inst *App) renderRecords(ctx app.FrameContextI) (err error) {
	inst.ensurePlay()
	if inst.inner == nil {
		c.Label("The notebook's records do not make a buffer: " + inst.innerErr).Wrap().Send()
		return
	}
	return inst.inner.Frame(ctx)
}
