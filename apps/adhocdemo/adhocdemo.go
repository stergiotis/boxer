// Package adhocdemo dogfoods ad-hoc bundles (ADR-0288, over
// ADR-0240's datasets): it generates a computed series, publishes it as a
// bundle — the rows and an applet document that reads them as `items` —
// through play.PublishBundle, and shows it through a sqlapplet bundle
// view, the receiver's one constructor. A Regenerate button republishes
// the bundle; the view follows it on the frame it is drawn, like any
// receiver of a bundle another app published.
//
// Every publish is a task owned by the window on a bgjob runner (W2): the
// first one starts at Mount and the view is built on the frame its result
// lands, so the render goroutine never waits on the bus (W1).
package adhocdemo

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadhoc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bgjobrow"
)

// datasetAlias is the name the bundle's document reads its rows by.
const datasetAlias = "items"

// bundleBase is the bundle's alias before the window's instance makes it
// this window's own (adhocdata.WindowAlias).
const bundleBase = "adhocdemo_items"

// itemsSpec is the bundle generation gen publishes: the series, and SQL
// that reads it ordered by x on the table.
func (inst *App) itemsSpec(gen int) play.BundleSpec {
	return play.BundleSpec{Alias: inst.bundle, Title: "Ad-hoc items", Summary: "a computed series published as an ad-hoc bundle",
		Sql: "SELECT * FROM keelson('" + datasetAlias + "') ORDER BY x", Tabs: []string{"table"},
		Prose:    "The host publishes a computed series as a bundle; Regenerate republishes it.",
		Datasets: []adhocdata.BundleDatasetInput{{LocalName: datasetAlias, ArrowIPCStream: inst.series(gen)}}}
}

// App is the dogfood embedder. Everything on it is the render goroutine's:
// the runners hand their results to Frame, which is the one writer.
type App struct {
	ids   *c.WidgetIdStack
	bus   app.BusI
	runId string
	// instance is this window's key, the published tree's origin with
	// runId (ADR-0330 §SD3).
	instance uint64
	log      zerolog.Logger
	// tasks runs the publishes as keelson tasks owned by this window.
	tasks task.TaskApiI

	bundle string
	view   *sqlapplet.BundleView

	// gen is the generation the last bundle publish carried; bundleJob
	// publishes one generation at a time, first at Mount and again on
	// Regenerate, and Frame takes the result. publishedGen is the
	// generation whose publish landed, -1 before the first; a regenerate
	// call settles when it reaches the generation the call asked for.
	gen          int
	publishedGen int
	bundleJob    bgjob.Runner[adhocdata.BundleResult]
	// publish is the bundle publish, play.PublishBundle on the bus when
	// nil; a test lands one without a bus.
	publish func(spec play.BundleSpec) (res adhocdata.BundleResult, err error)

	// frameCtx is the current frame's context, through which the buttons
	// go to the catalog (A3); nil outside a frame.
	frameCtx app.FrameContextI
	// pendingGen and pendingTree are the generations each agent's call
	// waits for, by the call's key, until the call settles (ADR-0358).
	pendingGen  map[string]int
	pendingTree map[string]int

	// The tree half (ADR-0222 §SD7): the mount the published tree lives in
	// — held so a republish writes another snapshot of it rather than
	// minting a second mount — the generation last published, and what
	// the button's note shows.
	treeMount identifier.TaggedId
	treeGen   int
	treeNote  string
	treeJob   bgjob.Runner[ladingadhoc.PublishResult]
	// treeLandedGen, treeEntries and treeExpires are the last tree
	// publish that landed, for the settle and get_view.
	treeLandedGen int
	treeEntries   uint64
	treeExpires   time.Time
	// publishTreeFn is the tree publish and the tally open, publishAndOpen
	// when nil; a test lands one without a server.
	publishTreeFn func(ctx context.Context, gen int, mount identifier.TaggedId) (res ladingadhoc.PublishResult, err error)
}

func newApp() (inst *App) {
	inst = &App{publishedGen: -1}
	return
}

var _ app.AppI = (*App)(nil)

func (inst *App) Manifest() app.Manifest { return manifest }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.bus = ctx.Bus()
	inst.runId = ctx.RunId()
	inst.instance = ctx.InstanceKey()
	inst.log = ctx.Log()
	inst.tasks = task.ForApp(ctx)

	// The window's own bundle alias (ADR-0288 §SD3): a second adhocdemo
	// window publishes its own. The first publish is a task like every
	// later one; the view is built when its result lands.
	inst.bundle = adhocdata.WindowAlias(bundleBase, ctx.InstanceKey())
	inst.startPublish()
	return
}

// startPublish publishes the current generation as a task; false when a
// publish is already in flight. PublishBundle is a synchronous bus request
// and the series is built inside the task, so neither touches the frame.
func (inst *App) startPublish() (started bool) {
	gen := inst.gen
	return inst.bundleJob.Start(inst.tasks, bgjob.Spec{Kind: "adhocdemo.publish", Title: "publish the series"},
		func(context.Context) (res *adhocdata.BundleResult, err error) {
			publish := inst.publish
			if publish == nil {
				publish = func(spec play.BundleSpec) (adhocdata.BundleResult, error) { return play.PublishBundle(inst.bus, spec) }
			}
			r, err := publish(inst.itemsSpec(gen))
			if err != nil {
				return
			}
			return &r, nil
		})
}

// takeResults moves what the runners finished into the window, once each,
// at the top of Frame.
func (inst *App) takeResults() {
	if _, _, ok := inst.bundleJob.TakeResult(); ok {
		// The runner takes one run at a time, so the run that landed is
		// the generation last asked for.
		inst.publishedGen = inst.gen
		// The view reads the bundle through the bus; a window without
		// one (a test landing a publish through the seam) has no view.
		if inst.view == nil && inst.bus != nil {
			inst.view = sqlapplet.NewBundleView(inst.bundle, sqlapplet.BundleViewConfig{Bus: inst.bus, Log: inst.log,
				RunId: inst.runId, StampAppId: string(ManifestId) + "#items", InstanceKey: inst.instance, Operable: true})
		}
	}
	if r, _, ok := inst.treeJob.TakeResult(); ok {
		inst.treeMount = r.Mount
		inst.treeLandedGen, inst.treeEntries, inst.treeExpires = inst.treeGen, r.Entries, r.ExpiresAt
		inst.treeNote = fmt.Sprintf("browsing %d entries as mount %016x · expires %s",
			r.Entries, r.Mount.Value(), r.ExpiresAt.UTC().Format("2006-01-02"))
	}
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.frameCtx = ctx
	defer func() { inst.frameCtx = nil }()
	inst.takeResults()
	view := inst.view
	var revision uint64
	if view != nil {
		revision = view.Revision()
	}

	for range c.PanelTopInside(inst.ids.PrepareStr("adhoc-bar")).Resizable(false).KeepIter() {
		for range c.Horizontal().KeepIter() {
			if view != nil {
				if c.Button(inst.ids.PrepareStr("regen"), c.Atoms().Text("Regenerate").Keep()).
					Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked() {
					inst.regenerateGesture()
				}
				inst.jobRow(&inst.bundleJob, "regen-job", "republishing the series…")
				inst.failedNote("regen-err", "regenerate", &inst.bundleJob)
			}
			if revision > 0 {
				c.Label(fmt.Sprintf("bundle %s · rev %d", inst.bundle, revision)).Send()
			}
			c.Separator().Vertical().Send()
			// The tree half: the same computation as a file tree rather
			// than as rows, published into the lading store and browsed in
			// tally (ADR-0222). A task — it connects, walks a tree and
			// writes rows.
			if c.Button(inst.ids.PrepareStr("publish-tree"), c.Atoms().Text("Publish tree & browse in tally").Keep()).SendResp().HasPrimaryClicked() {
				inst.publishTreeGesture()
			}
			if !inst.jobRow(&inst.treeJob, "tree-job", "publishing the tree…") && inst.treeNote != "" {
				c.Label(inst.treeNote).Send()
			}
			inst.failedNote("tree-err", "tree", &inst.treeJob)
		}
	}

	if view == nil {
		// The first publish has not landed: say what the pane waits on,
		// or why it never will (L4).
		for range c.PanelCentralInside().KeepIter() {
			if snap := inst.bundleJob.Snapshot(); snap.State == bgjob.StateFailed {
				inst.failedNote("publish-err", "publish", &inst.bundleJob)
				if c.Button(inst.ids.PrepareStr("publish-retry"), c.Atoms().Text("Retry").Keep()).SendResp().HasPrimaryClicked() {
					inst.startPublish()
				}
			} else {
				c.Label("Preparing — waiting on the bundle publish").Send()
				inst.jobRow(&inst.bundleJob, "publish-job", "publishing the series…")
			}
		}
		return
	}
	// This app's own frame context, which is the one it holds: the view's
	// embedded play draws under this app's identity — and per ADR-0155 §SD3
	// that identity is exactly what column-width state keys on.
	return view.Frame(ctx)
}

// jobRow draws a runner's progress row while it runs (W4) and reports
// whether it did; the row keeps frames coming while it draws (W7).
func (inst *App) jobRow(job bgjobrow.JobI, key string, note string) (running bool) {
	return bgjobrow.Render(bgjobrow.Input{Job: job, Ids: inst.ids, ScopeKey: key, Inline: true, Note: note}).Running
}

// failedNote shows a runner's last failure as a badge (W8): the outcome
// reaches the window, not only the log.
func (inst *App) failedNote(key string, what string, job bgjobrow.JobI) {
	if snap := job.Snapshot(); snap.State == bgjob.StateFailed && snap.Err != nil {
		badge.New(inst.ids.PrepareStr(key), what+" failed: "+snap.Err.Error()).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
	}
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.bundleJob.Cancel()
	inst.treeJob.Cancel()
	if inst.view != nil {
		inst.view.Close()
		inst.view = nil
	}
	// The bundle is not retracted here: the runtime retracts what this
	// window published when the host closes its bus client (ADR-0240 §SD5).
	return
}

// regenerate republishes the bundle with a fresh series, as a task. The
// view learns of the new revision from the service's event, like any
// other receiver. A click while a publish runs is dropped: the runner
// takes one run at a time.
func (inst *App) regenerate() {
	if inst.bundleJob.Running() {
		return
	}
	inst.gen++
	inst.startPublish()
}

// series renders generation gen of the computed series as an Arrow IPC
// stream, the form a bundle dataset travels in.
func (inst *App) series(gen int) []byte {
	rec := seriesRecord(gen)
	defer rec.Release()
	stream, err := adhocdata.EncodeRecord(rec)
	if err != nil {
		inst.log.Warn().Err(err).Msg("adhocdemo: encode series")
	}
	return stream
}

// seriesRecord is generation gen of the computed series: x = 0..N-1
// (Int64), y = a phase-shifted sine (Float64), so each Regenerate visibly
// changes the values. The caller releases the record.
func seriesRecord(gen int) (rec arrow.RecordBatch) {
	const n = 24
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "x", Type: arrow.PrimitiveTypes.Int64},
		{Name: "y", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	xb := rb.Field(0).(*array.Int64Builder)
	yb := rb.Field(1).(*array.Float64Builder)
	for x := range n {
		xb.Append(int64(x))
		yb.Append(math.Sin(float64(x)*0.4 + float64(gen)*0.6))
	}
	return rb.NewRecordBatch()
}
