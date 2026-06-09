package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/componentview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pager"
)

// =============================================================================
// componentview demo — typed per-component leeway report + generic complement
//
// One real leeway drone record (marshalled into anchor's table). A pager (page
// size 1) selects the active row; the single-record report below shows that row
// two ways:
//   - the typed per-component report — each recognised component drawn by its
//     own widget (identity→toned pill badge, battery→radial gauge, tasked→tag
//     chips); registered-but-absent components are dimmed so the archetype is
//     legible.
//   - the generic leewaywidgets.Table2CardEmitter over the same row, every
//     attribute unfiltered — including the delivery-window (timeRange) section
//     that no typed renderer claims. The typed view is the specific complement;
//     the generic card is the fallback that always works.
//
// The drone schema's TableDesc is shown by the sibling "componentview schema"
// demo (schemaview).
// =============================================================================

var componentViewReg = componentview.DefaultRegistry()

type componentViewDemoState struct {
	data     *cvDroneData
	cvDriver *streamreadaccess.Driver
	emitter  *leewaywidgets.Table2CardEmitter
	comps    [][]componentview.Component
	pager    *pager.Pager
	ready    bool
	errMsg   string
}

func newComponentViewState(ids *c.WidgetIdStack) (st *componentViewDemoState) {
	st = &componentViewDemoState{}
	d := ensureCvDroneData()
	st.data = d
	if d.err != "" {
		st.errMsg = d.err
		return
	}
	for _, r := range d.rows {
		comps := []componentview.Component{
			{Kind: componentview.KindIdentity, Value: componentview.IdentityVal{Status: r.Status}},
			{Kind: componentview.KindBattery, Value: componentview.BatteryVal{Charge: r.Battery}},
		}
		if len(r.Tags) > 0 {
			comps = append(comps, componentview.Component{Kind: componentview.KindTasked, Value: componentview.TaskedVal{Tags: r.Tags}})
		}
		st.comps = append(st.comps, comps)
	}
	driver, err := newCvCardDriver(d)
	if err != nil {
		st.errMsg = "driver: " + err.Error()
		return
	}
	st.cvDriver = driver
	st.emitter = leewaywidgets.NewTable2CardEmitter(ids, leewaywidgets.ColorPaletteViridis, nil)
	// Page size 1: each page is exactly one drone, so the pager selects the
	// single record whose report is shown.
	st.pager = pager.New(c.NewWidgetIdStack(), 1).WithUnit("drones").WithPageSizeCombo(false)
	st.pager.Configure(int64(len(st.comps)))
	st.ready = true
	return
}

func init() {
	registry.Register(registry.Demo{
		Name:     "componentview",
		Category: "Leeway",
		Title:    icons.IconTag + " component view",
		Stage:    [2]float32{1200, 820},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "Typed per-component leeway report (ADR-0075) plus its generic " +
			"complement, one record at a time. A pager (page size 1) selects the " +
			"active drone; the typed report (identity→badge, battery→gauge, " +
			"tasked→chips; absent components dimmed) sits above the generic " +
			"Table2CardEmitter showing every attribute of the same row — including " +
			"the delivery-window section no typed renderer claims.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			return newComponentViewState(ids)
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			renderComponentViewDemo(ids, state.(*componentViewDemoState))
		},
	})
}

func renderComponentViewDemo(ids *c.WidgetIdStack, st *componentViewDemoState) {
	if !st.ready {
		c.Label("componentview demo unavailable: " + st.errMsg).Wrap().Send()
		return
	}

	// The pager picks the active row; the report shows only that record.
	st.pager.Render()
	start, _ := st.pager.Range()
	active := max(0, min(int(start), len(st.comps)-1))

	c.Separator().Horizontal().Send()
	for rt := range c.RichTextLabel(st.data.names[active]) {
		rt.Strong().Size(15)
	}

	disp := componentview.NewDispatcher(componentViewReg)
	disp.ShowAbsent = true
	disp.DefaultOpen = true

	for rt := range c.RichTextLabel("typed per-component report") {
		rt.Weak().Small()
	}
	disp.RenderReport(ids, st.comps[active])

	c.Separator().Horizontal().Send()
	for rt := range c.RichTextLabel("generic · Table2CardEmitter — every attribute") {
		rt.Weak().Small()
	}
	slice := st.data.rec.NewSlice(int64(active), int64(active)+1)
	if err := st.cvDriver.DriveRecordBatch(st.emitter, slice); err != nil {
		c.Label("card render error: " + err.Error()).Wrap().Send()
	}
	slice.Release()
}
