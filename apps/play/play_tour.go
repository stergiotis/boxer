package play

// play_tour.go enrols one seeded scene into the imzero2 demo registry
// (ADR-0057, keelson-app A8), so the central TestDriver and the
// design-system review tier see the playground drawn without window
// chrome. A whole-app carousel registration is not a tour entry; a
// full-window app owes a seeded demo like any other.
//
// The scene is the window a person meets after one run: a small
// aggregate in the editor and its result under it, the Table tab in
// front. The result is seeded the way the catalog tests seed theirs — a
// record built in process and handed to the main lane as a finished run —
// so no engine is needed and the rows are fixed. The capture is still not
// byte-stable: the status bar reports the run's wall-clock elapsed time
// and when it executed, both taken at seeding, so the Demo is flagged
// NonDeterministic and the TestDriver skips it under
// IMZERO2_SCREENSHOT_DETERMINISTIC. Screenshot scaffolding only.

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// tourSQL is the buffer the scene opens with and the statement its seeded
// result is recorded as having run, so the result reads as fresh (`rows`),
// not stale. It names a table no endpoint of the tour has; nothing runs it.
const tourSQL = `SELECT region, product, sum(amount) AS revenue, count() AS orders
FROM sales
GROUP BY region, product
ORDER BY revenue DESC`

// tourResult builds the rows the scene shows: few enough to fit the Table
// without a pager, typed across the three kinds the grid captions
// differently (text, a float, an integer). The caller owns the record.
func tourResult(mem memory.Allocator) arrow.RecordBatch {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "region", Type: arrow.BinaryTypes.String},
		{Name: "product", Type: arrow.BinaryTypes.String},
		{Name: "revenue", Type: arrow.PrimitiveTypes.Float64},
		{Name: "orders", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	regions := array.NewStringBuilder(mem)
	products := array.NewStringBuilder(mem)
	revenue := array.NewFloat64Builder(mem)
	orders := array.NewInt64Builder(mem)
	defer regions.Release()
	defer products.Release()
	defer revenue.Release()
	defer orders.Release()
	regions.AppendValues([]string{"north", "west", "north", "east", "west", "east"}, nil)
	products.AppendValues([]string{"pump", "valve", "valve", "pump", "gasket", "gasket"}, nil)
	revenue.AppendValues([]float64{184250.5, 121780, 97310.25, 88400, 23175.75, 19860}, nil)
	orders.AppendValues([]int64{412, 377, 298, 205, 1193, 968}, nil)
	cols := []arrow.Array{regions.NewArray(), products.NewArray(), revenue.NewArray(), orders.NewArray()}
	defer func() {
		for _, col := range cols {
			col.Release()
		}
	}()
	return array.NewRecordBatch(schema, cols, 6)
}

func init() {
	registry.Register(registry.Demo{
		Name:     "play-after-a-run",
		Category: "Data",
		Title:    icons.PhDatabase + " play — a result under its query",
		Stage:    [2]float32{1200, 800},
		Flags:    registry.DemoFlagNonDeterministic | registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindUX,
		Description: "The SQL playground after one run: the statement in the editor with its parameter row, " +
			"the result in the Table tab below, the query-state chip reading rows, and the status bar with the " +
			"row count. The result is seeded in process rather than fetched, so the window has no endpoint; the " +
			"top bar and the panes that probe one draw their no-endpoint state.",
		Init:           playTourInit,
		RenderStateful: playTourRender,
		SourceFunc:     (*PlayApp).render,
	})
}

// playTourInit builds a client-less playground over the tour buffer and
// lands the seeded result on its main lane as a finished run, the way the
// catalog tests do (mainLane.finish). lastSentSql is set to the buffer so
// the staleness witness reads the result as the buffer's own.
func playTourInit(_ *c.WidgetIdStack) (state any) {
	mem := memory.NewGoAllocator()
	inst := NewPlayApp(nil, newLiveQueryGraph(nil, mem, 4), tourSQL, nil)
	rec := tourResult(mem)
	inst.graph.mainLane.finish(tourSQL, nil, time.Now(), rec, rec.Schema(), rec.NumRows(), Summary{}, nil, runstream.Terminal{})
	inst.lastSentSql = tourSQL
	return inst
}

// playTourRender draws the playground into the host scope. The Table tab
// is raised every frame rather than once at Init: the TestDriver renders
// every Demo through one shared dock state and captures them in name
// order, so a tab raised once would be whatever a prior capture left.
// PlayApp draws on its own per-instance salted id stacks (NewPlayApp), so
// the host stack is not taken.
func playTourRender(_ *c.WidgetIdStack, state any) {
	inst, ok := state.(*PlayApp)
	if !ok || inst == nil {
		return
	}
	_ = inst.ActivateTab(tablePaneId)
	_ = inst.render()
}
