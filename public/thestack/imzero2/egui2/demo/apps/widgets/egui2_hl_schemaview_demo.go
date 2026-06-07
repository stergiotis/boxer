package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/schemaview"
)

func init() {
	registry.Register(registry.Demo{
		Name: "schemaview", Category: "Leeway", Title: icons.IconTreeStructure + " schema inspector",
		Stage:       [2]float32{1100, 760},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "Inspect a leeway TableDesc as a master-detail view across two dock panes: a section navigator on the left, a decoded property pane (canonical type, encoding hints, value semantics, membership spec, groups) on the right. The glyph vocabulary is keyed by the \"?\" legend popup in the navigator header. Reads the authored schema directly; IR expansion, the physical/DDL descent and sample data are out of scope.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newSchemaViewState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoSchemaView(ids, state.(*schemaViewDemoState))
		},
		SourceFunc: demoSchemaView,
	})
}

// schemaFixture is one named schema offered in the chooser. The TableDesc is
// stored by value; newSchemaViewState builds the slice once and never
// re-appends, so &fixtures[i].table stays a stable pointer for the widget.
type schemaFixture struct {
	name  string
	table common.TableDesc
}

// schemaViewDemoState holds the inspector model plus the fixture chooser
// across frames.
type schemaViewDemoState struct {
	model    *schemaview.Model
	fixtures []schemaFixture
	selected int
}

// newSchemaViewState seeds the inspector with the leewaywidgets fixture (a
// geo co-section group, all five membership shapes) and the JSON document
// mapping (many type-sections, including value-less membership-only ones).
func newSchemaViewState() *schemaViewDemoState {
	st := &schemaViewDemoState{}
	if td, err := leewaywidgets.BuildFixtureTableDesc(); err == nil {
		st.fixtures = append(st.fixtures, schemaFixture{name: "fixture (geo co-section)", table: td})
	}
	if td, err := mapping.NewJsonMapping(); err == nil {
		st.fixtures = append(st.fixtures, schemaFixture{name: "JSON document mapping", table: td})
	}
	if len(st.fixtures) > 0 {
		st.model = schemaview.NewModel(&st.fixtures[0].table)
	} else {
		st.model = schemaview.NewModel(nil)
	}
	return st
}

// demoSchemaView renders a segmented fixture chooser (when more than one is
// available) above the inspector for the given per-window state.
func demoSchemaView(ids *c.WidgetIdStack, st *schemaViewDemoState) {
	if len(st.fixtures) > 1 {
		for range c.Horizontal().KeepIter() {
			for i := range st.fixtures {
				if c.SelectableLabel(ids.PrepareSeq(uint64(0x100+i)), i == st.selected, st.fixtures[i].name).
					SendResp().HasPrimaryClicked() {
					st.selected = i
					st.model.SetTable(&st.fixtures[i].table)
				}
			}
		}
		c.AddSpace(6)
	}
	schemaview.Render(schemaview.Input{Ids: ids, ScopeKey: "schemaview", Model: st.model})
}
