// Package adhocdemo dogfoods ad-hoc bundles (ADR-0288 (proposed), over
// ADR-0240's datasets): it generates a computed series, publishes it as a
// bundle — the rows and an applet document that reads them as `items` —
// through play.PublishBundleE, and shows it through a sqlapplet bundle
// view, the receiver's one constructor. A Regenerate button republishes
// the bundle; the view follows it on the frame it is drawn, like any
// receiver of a bundle another app published.
package adhocdemo

import (
	"fmt"
	"math"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
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

// App is the dogfood embedder.
type App struct {
	ids   *c.WidgetIdStack
	bus   app.BusI
	runId string
	log   zerolog.Logger

	bundle string
	view   *sqlapplet.BundleView

	mu        sync.Mutex
	gen       int
	busy      bool
	statusErr string

	// The tree half (ADR-0222 §SD7), under the same lock: the mount the
	// published tree lives in — held so a republish writes another snapshot
	// of it rather than minting a second mount — and what the button shows.
	treeMount identifier.TaggedId
	treeGen   int
	treeBusy  bool
	treeNote  string
	treeErr   string
}

var _ app.AppI = (*App)(nil)

func (inst *App) Manifest() app.Manifest { return manifest }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.bus = ctx.Bus()
	inst.runId = ctx.RunId()
	inst.log = ctx.Log()

	// The window's own bundle alias (ADR-0288 §SD3): a second adhocdemo
	// window publishes its own.
	inst.bundle = adhocdata.WindowAlias(bundleBase, ctx.InstanceKey())
	if _, pubErr := play.PublishBundleE(inst.bus, inst.itemsSpec(0)); pubErr != nil {
		inst.statusErr = "publish: " + pubErr.Error()
		return
	}
	inst.view = sqlapplet.NewBundleView(inst.bundle, sqlapplet.BundleViewConfig{Bus: inst.bus, Log: inst.log,
		RunId: inst.runId, StampAppId: string(ManifestId) + "#items", InstanceKey: ctx.InstanceKey(), Operable: true})
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.mu.Lock()
	view := inst.view
	busy := inst.busy
	statusErr := inst.statusErr
	inst.mu.Unlock()
	var revision uint64
	if view != nil {
		revision = view.Revision()
	}

	for range c.PanelTopInside(inst.ids.PrepareStr("adhoc-bar")).Resizable(false).KeepIter() {
		for range c.Horizontal().KeepIter() {
			if view != nil {
				label := "Regenerate"
				if busy {
					label = "Regenerating…"
				}
				if c.Button(inst.ids.PrepareStr("regen"), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked() && !busy {
					go inst.regenerate()
				}
			}
			if revision > 0 {
				c.Label(fmt.Sprintf("bundle %s · rev %d", inst.bundle, revision)).Send()
			}
			c.Separator().Vertical().Send()
			// The tree half: the same computation as a file tree rather
			// than as rows, published into the lading store and browsed in
			// tally (ADR-0222). Off the render thread — it walks a tree and
			// writes rows.
			treeBusy, treeNote, treeErr := inst.treeState()
			label := "Publish tree & browse in tally"
			if treeBusy {
				label = "Publishing tree…"
			}
			if c.Button(inst.ids.PrepareStr("publish-tree"), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked() && !treeBusy {
				go inst.publishTree()
			}
			if treeNote != "" {
				c.Label(treeNote).Send()
			}
			if treeErr != "" {
				c.Label("tree: " + treeErr).Send()
			}
		}
	}

	if view == nil {
		for range c.PanelCentralInside().KeepIter() {
			if statusErr != "" {
				c.Label("ad-hoc demo unavailable: " + statusErr).Send()
			} else {
				c.Label("Preparing…").Send()
			}
		}
		return
	}
	// This app's own frame context, which is the one it holds: the view's
	// embedded play draws under this app's identity — and per ADR-0155 §SD3
	// that identity is exactly what column-width state keys on.
	return view.Frame(ctx)
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.view != nil {
		inst.view.Close()
		inst.view = nil
	}
	// The bundle is not retracted here: the runtime retracts what this
	// window published when the host closes its bus client (ADR-0240 §SD5).
	return
}

// regenerate republishes the bundle with a fresh series. Runs off the
// render thread — bus.Request is synchronous and would stall the frame.
// The view learns of the new revision from the service's event, like any
// other receiver.
func (inst *App) regenerate() {
	inst.mu.Lock()
	if inst.busy || inst.view == nil {
		inst.mu.Unlock()
		return
	}
	inst.busy = true
	inst.gen++
	gen := inst.gen
	inst.mu.Unlock()

	if _, err := play.PublishBundleE(inst.bus, inst.itemsSpec(gen)); err != nil {
		inst.log.Warn().Err(err).Msg("adhocdemo: regenerate failed")
	}

	inst.mu.Lock()
	inst.busy = false
	inst.mu.Unlock()
}

// series renders generation gen of the computed series as an Arrow IPC
// stream: x = 0..N-1 (Int64), y = a phase-shifted sine (Float64), so each
// Regenerate visibly changes the values.
func (inst *App) series(gen int) []byte {
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
	rec := rb.NewRecordBatch()
	defer rec.Release()
	stream, err := adhocdata.EncodeRecord(rec)
	if err != nil {
		inst.log.Warn().Err(err).Msg("adhocdemo: encode series")
	}
	return stream
}
