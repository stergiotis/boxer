// Package adhocdemo dogfoods ADR-0134: it generates a computed series,
// publishes it as an ephemeral encrypted dataset over the adhoc.publish
// capability, and embeds a SQL applet that queries it by the stable alias
// `items`. A Regenerate button republishes fresh data under the same
// handle; the embedded applet runs Live, so it re-queries and shows the
// new rows. The applet is a committed, gated, classified document — the
// ADR-0132 §SD8 embedder shape — bound to the ephemeral handle pre-mount.
package adhocdemo

import (
	"bytes"
	"fmt"
	"math"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// datasetAlias is the stable alias the applet buffer names; the embedder
// binds it to the ephemeral handle pre-mount (ADR-0134 §SD4).
const datasetAlias = "items"

// itemsDoc is the embedded applet document: an introspection-endpoint
// applet declaring the `items` dataset and selecting it ordered by x.
const itemsDoc = "---\n" +
	"type: reference\n" +
	"status: draft\n" +
	"title: Ad-hoc items\n" +
	"icon: \"\U0001F4E6\"\n" +
	"endpoint: introspection\n" +
	"datasets: [items]\n" +
	"tabs: [table]\n" +
	"---\n\n" +
	"# Ad-hoc items\n\n" +
	"The host publishes a computed series and binds it to `items`.\n\n" +
	"```sql\nSELECT * FROM keelson('items') ORDER BY x\n```\n"

// App is the dogfood embedder.
type App struct {
	ids   *c.WidgetIdStack
	bus   app.BusI
	runId string
	log   zerolog.Logger

	inner  *play.PlayApp
	handle string

	mu            sync.Mutex
	gen           int
	revision      uint64
	rows          uint64
	busy          bool
	pendingRev    uint64
	pendingNotify bool
	statusErr     string
}

var _ app.AppI = (*App)(nil)

func (inst *App) Manifest() app.Manifest { return manifest }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.bus = ctx.Bus()
	inst.runId = ctx.RunId()
	inst.log = ctx.Log()

	def, perr := sqlapplet.ParseDocSource(string(ManifestId), "items.md", []byte(itemsDoc))
	if perr != nil {
		inst.statusErr = "parse applet: " + perr.Error()
		return
	}
	if def == nil {
		inst.statusErr = "applet document has no SQL buffer"
		return
	}

	res, pubErr := adhocdata.PublishRequest(inst.bus, adhocdata.PublishInput{Alias: datasetAlias, ArrowIPCStream: inst.series(0)})
	if pubErr != nil {
		inst.statusErr = "publish: " + pubErr.Error()
		return
	}
	inst.handle = res.Handle
	inst.revision = res.Revision
	inst.rows = res.Rows

	inner, embErr := sqlapplet.NewEmbedded(def, sqlapplet.EmbedConfig{
		StampAppId: string(ManifestId) + "#" + def.Slug,
		RunId:      inst.runId,
		Bus:        inst.bus,
		Log:        inst.log,
		Bindings:   map[string]string{datasetAlias: res.Handle},
	})
	if embErr != nil {
		inst.statusErr = "embed applet: " + embErr.Error()
		return
	}
	// Run Live so a republish (Regenerate) re-queries without an explicit
	// Run — the ADR-0134 §SD5 freshness path via NotifyDatasetRevision.
	inner.SetLiveMain(true)
	inst.inner = inner
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.mu.Lock()
	if inst.pendingNotify && inst.inner != nil {
		// Deliver the revision bump on the render thread (NotifyDatasetRevision
		// touches PlayApp state the render loop reads).
		inst.inner.NotifyDatasetRevision(datasetAlias, inst.pendingRev)
		inst.revision = inst.pendingRev
		inst.pendingNotify = false
	}
	inner := inst.inner
	handle := inst.handle
	revision := inst.revision
	rows := inst.rows
	busy := inst.busy
	statusErr := inst.statusErr
	inst.mu.Unlock()

	for range c.PanelTopInside(inst.ids.PrepareStr("adhoc-bar")).Resizable(false).KeepIter() {
		for range c.Horizontal().KeepIter() {
			if inner != nil {
				label := "Regenerate"
				if busy {
					label = "Regenerating…"
				}
				if c.Button(inst.ids.PrepareStr("regen"), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked() && !busy {
					go inst.regenerate()
				}
			}
			if handle != "" {
				c.Label(fmt.Sprintf("handle %s · rev %d · %d rows", handle, revision, rows)).Send()
			}
		}
	}

	if inner == nil {
		for range c.PanelCentralInside().KeepIter() {
			if statusErr != "" {
				c.Label("ad-hoc demo unavailable: " + statusErr).Send()
			} else {
				c.Label("preparing…").Send()
			}
		}
		return
	}
	return inner.Render()
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.inner != nil {
		inst.inner.Close()
		inst.inner = nil
	}
	if inst.handle != "" {
		if rerr := adhocdata.RetractRequest(inst.bus, inst.handle); rerr != nil {
			inst.log.Debug().Err(rerr).Msg("adhocdemo: retract on unmount")
		}
		inst.handle = ""
	}
	return
}

// regenerate republishes a fresh series under the same handle and flags a
// revision notification for the render thread to deliver. Runs off the
// render thread — bus.Request is synchronous and would stall the frame.
func (inst *App) regenerate() {
	inst.mu.Lock()
	if inst.busy || inst.handle == "" {
		inst.mu.Unlock()
		return
	}
	inst.busy = true
	inst.gen++
	gen := inst.gen
	handle := inst.handle
	inst.mu.Unlock()

	res, err := adhocdata.PublishRequest(inst.bus, adhocdata.PublishInput{
		Alias: datasetAlias, Handle: handle, ArrowIPCStream: inst.series(gen),
	})

	inst.mu.Lock()
	inst.busy = false
	if err != nil {
		inst.log.Warn().Err(err).Msg("adhocdemo: regenerate failed")
	} else {
		inst.rows = res.Rows
		inst.pendingRev = res.Revision
		inst.pendingNotify = true
	}
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
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	if werr := w.Write(rec); werr != nil {
		inst.log.Warn().Err(werr).Msg("adhocdemo: encode series")
	}
	if cerr := w.Close(); cerr != nil {
		inst.log.Warn().Err(cerr).Msg("adhocdemo: close series stream")
	}
	return buf.Bytes()
}
