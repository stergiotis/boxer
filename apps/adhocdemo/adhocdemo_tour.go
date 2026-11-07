// Demo-registry enrollment for adhocdemo (ADR-0057, keelson-app A8): one
// Demo draws the series the window publishes, in a seeded generation, as
// the grid the bundle view's table tab shows it in. The bundle view itself
// needs the host's ad-hoc data service on a bus — the publish is a bus
// request, and the view resolves the bundle and follows its events — and
// the tour runs with no host bus, so the tour draws the dataset the bundle
// carries rather than the view around it.

package adhocdemo

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// tourGen is the generation the tour draws; tourInstance the window key
// the bundle alias is scoped by.
const (
	tourGen      = 1
	tourInstance = 7
)

// tourNote is what the demo says about the view it does not draw.
const tourNote = "The bundle view needs the host's ad-hoc data service; the tour draws the series the bundle carries."

func init() {
	registry.Register(registry.Demo{
		Name:     "adhocdemo-series",
		Category: "Data",
		Title:    icons.PhDatabase + " Ad-hoc dataset demo — the published series",
		Stage:    [2]float32{900, 600},
		Kind:     registry.DemoKindUX,
		Description: "The series adhocdemo publishes as an ad-hoc bundle (ADR-0288), one generation, as the grid the bundle view's " +
			"table tab shows: x and a phase-shifted sine of it. The bar names the window-scoped bundle alias.",
		Init:           tourInit,
		RenderStateful: tourRenderStateful,
		SourceFunc:     (*App).series,
	})
}

// tourState is the record the tour draws and the grid's own state.
type tourState struct {
	ids    *c.WidgetIdStack
	bundle string
	rec    arrow.RecordBatch
	cols   []recordtable.Column
	sort   recordtable.SortState
	grid   recordtable.State
}

func tourInit(ids *c.WidgetIdStack) (state any) {
	st := &tourState{ids: ids, bundle: adhocdata.WindowAlias(bundleBase, tourInstance), rec: seriesRecord(tourGen)}
	for i, f := range st.rec.Schema().Fields() {
		st.cols = append(st.cols, recordtable.Column{Arrow: i, Header: f.Name, Width: 140})
	}
	state = st
	return
}

func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	st, ok := state.(*tourState)
	if !ok || st == nil {
		return
	}
	st.ids = ids
	for range c.PanelTopInside(st.ids.PrepareStr("adhoc-bar")).Resizable(false).KeepIter() {
		for range c.Horizontal().KeepIter() {
			c.Label(fmt.Sprintf("bundle %s · generation %d · %d rows", st.bundle, tourGen, st.rec.NumRows())).Send()
		}
		for rt := range c.RichTextLabel(tourNote) {
			rt.Small().Weak()
		}
	}
	for range c.PanelCentralInside().KeepIter() {
		res := recordtable.RenderGrid(recordtable.Input{
			Ids: st.ids, ScopeKey: "series", Record: st.rec, Columns: st.cols,
			Sort: &st.sort, Order: st.sort.OrderFor(st.rec, st.rec.NumRows()), State: &st.grid,
		})
		st.sort.Apply(res)
	}
}
