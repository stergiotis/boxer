// Demo-registry enrollment for jackstay (ADR-0057, keelson-app A8): the
// Structure page over a seeded plan between two made-up servers, drawn
// without window chrome so the screenshot driver and the design-system
// review tier see it. The plan is built in memory, as the package's tests
// build theirs, so the scene needs no server, no bus and no data area; the
// page reads only the plan and this window's selection, and nothing in it
// is time-dependent, so the capture is byte-stable.

package jackstay

import (
	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "jackstay-structure",
		Category: "Tools",
		Title:    icons.PhArrowsLeftRight + " jackstay — structure",
		Stage:    [2]float32{1200, 800},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindUX,
		Description: "jackstay's Structure page over a seeded plan: the breadcrumb, the plan line, one verdict " +
			"badge per table with the DDL the target needs, the selected table's reasons, notes, copy columns, " +
			"row filter and DDL, and the arm-and-confirm control that would run the DDL on the target.",
		Init:           tourInit,
		RenderStateful: tourRenderStateful,
		SourceFunc:     (*App).renderStructure,
	})
}

// tourState is the per-Demo state: the App bound to the host id stack, with
// its plan and selection seeded.
type tourState struct {
	app *App
}

func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.ids = ids
	inst.step = stepStructure
	p := tourPlan()
	inst.plan = &p
	inst.selected = p.Tables[1].Source // the extend verdict: DDL, a note and the filter row
	return &tourState{app: inst}
}

// tourPlan is a plan the Structure page can judge without a server: five
// tables, one per verdict the page draws differently.
func tourPlan() (p jk.Plan) {
	ref := func(db string, name string) datacatalog.TableRef {
		return datacatalog.TableRef{Database: db, Name: name}
	}
	p = jk.Plan{
		FormatVersion: jk.PlanFormatVersion,
		Source:        jk.Endpoint{URL: "http://source.example:8123/", User: "default"},
		Target:        jk.Endpoint{URL: "http://replica.example:8123/", User: "default"},
		Selection:     jk.Selection{Databases: []string{"shop"}},
		Tables: []jk.PlanTable{
			{
				Source: ref("shop", "orders"), Target: ref("shop", "orders"),
				Engine: "MergeTree", SortingKey: "(customer_id, ts)", Rows: 1204311, Bytes: 92 << 20,
				TableVerdict: jk.TableVerdict{Verdict: jk.VerdictIdentical,
					CopyColumns: []string{"id", "customer_id", "ts", "amount", "status"}},
			},
			{
				Source: ref("shop", "customers"), Target: ref("shop", "customers"),
				Engine: "MergeTree", SortingKey: "id", Rows: 48213, Bytes: 6 << 20,
				Filter: "created >= toDate('2026-01-01')",
				TableVerdict: jk.TableVerdict{Verdict: jk.VerdictExtend,
					Notes:       []string{"engine differs: MergeTree on the source, ReplacingMergeTree on the target"},
					DDL:         []string{"ALTER TABLE shop.customers ADD COLUMN segment LowCardinality(String) AFTER email"},
					CopyColumns: []string{"id", "email", "segment", "created"}},
			},
			{
				Source: ref("shop", "events"), Target: ref("shop", "events"),
				Engine: "MergeTree", SortingKey: "(ts, id)", Rows: 9812004, Bytes: 1340 << 20,
				TableVerdict: jk.TableVerdict{Verdict: jk.VerdictCreate,
					DDL: []string{"CREATE TABLE IF NOT EXISTS shop.events (ts DateTime, id UInt64, kind LowCardinality(String), " +
						"payload String) ENGINE = MergeTree ORDER BY (ts, id)"},
					CopyColumns: []string{"ts", "id", "kind", "payload"}, Leeway: true},
			},
			{
				Source: ref("shop", "daily_revenue"), Target: ref("shop", "daily_revenue"),
				Engine: "View",
				TableVerdict: jk.TableVerdict{Verdict: jk.VerdictUnsupported,
					Reasons: []string{"the source is a view, which holds no rows a copy can move"}},
			},
			{
				Source: ref("shop", "legacy_prices"), Target: ref("shop", "legacy_prices"),
				Engine: "MergeTree", SortingKey: "sku", Rows: 3120, Bytes: 1 << 18,
				TableVerdict: jk.TableVerdict{Verdict: jk.VerdictIncompatible,
					Reasons: []string{"column amount is Decimal(18, 2) on the source and Float64 on the target"}},
			},
		},
	}
	return
}

// tourRenderStateful draws the page the way the window lays it out, minus
// the window panels: the breadcrumb and the plan line, the page in a scroll
// area under its own id scope, and the page's action at the foot.
func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	st, ok := state.(*tourState)
	if !ok || st == nil {
		return
	}
	inst := st.app
	inst.ids = ids
	inst.renderBreadcrumb()
	inst.renderPlanRow()
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		for range c.IdScope(inst.ids.PrepareStr("page-" + inst.step.String())) {
			for range c.Frame(inst.ids.PrepareStr("pad")).InnerMarginSides(12, 12, 8, 8).KeepIter() {
				inst.renderStructure()
			}
		}
	}
	for range c.HorizontalWrapped().KeepIter() {
		inst.renderApplyControls()
	}
}
