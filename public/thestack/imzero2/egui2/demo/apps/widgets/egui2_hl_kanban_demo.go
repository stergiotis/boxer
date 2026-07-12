package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/kanban"
)

func init() {
	registry.Register(registry.Demo{
		Name: "kanban", Category: "Layout & widgets", Title: icons.IconTable + " kanban board",
		Stage:       [2]float32{1040, 560},
		Kind:        registry.DemoKindMixed,
		Description: "A board of columns and the cards in them. Move a card between columns with its ◀ ▶ controls, reorder it within a column with ▲ ▼; click a card to select it (accent-stroked). Cards carry an optional accent bullet and a one-level parent link (a \"sub-item of …\" trailer; parents show a \"◱ N sub\" chip) — sub-items are scheduled independently of their parent. Drag-and-drop is a deferred slice.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newKanbanState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoKanban(ids, state.(*kanbanDemoState))
		},
		SourceFunc: demoKanban,
	})
}

// kanbanDemoState holds the board model across frames.
type kanbanDemoState struct {
	model *kanban.Model
}

// newKanbanState seeds a four-lane board with a few accented cards and one card
// ("Design board API") whose three sub-items are scheduled across three
// different columns — the "scheduled separately" shape.
func newKanbanState() *kanbanDemoState {
	acc := func(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }

	cols := []kanban.Column{
		{ID: 1, Title: "Backlog"},
		{ID: 2, Title: "Todo"},
		{ID: 3, Title: "Doing"},
		{ID: 4, Title: "Done"},
	}
	cards := []kanban.Card{
		{ID: 10, ColumnID: 3, Title: "Design board API", Subtitle: "concise imzero2 shape", Accent: acc(styletokens.AccentDefault)},
		{ID: 11, ColumnID: 2, Title: "Card frame + selection", Accent: acc(styletokens.InfoDefault)},
		{ID: 12, ColumnID: 1, Title: "Drag-and-drop", Subtitle: "deferred slice", Accent: acc(styletokens.WarningDefault)},
		{ID: 13, ColumnID: 4, Title: "Reconnaissance", Accent: acc(styletokens.SuccessDefault)},
		{ID: 14, ColumnID: 1, Title: "Independent column scroll", Subtitle: "deferred"},

		// Sub-items of #10, each scheduled in its own column.
		{ID: 20, ColumnID: 4, ParentID: 10, Title: "Data model"},
		{ID: 21, ColumnID: 3, ParentID: 10, Title: "Move helpers"},
		{ID: 22, ColumnID: 2, ParentID: 10, Title: "Column layout"},
	}
	return &kanbanDemoState{model: kanban.NewModel(cols, cards)}
}

func demoKanban(ids *c.WidgetIdStack, st *kanbanDemoState) {
	kanban.Render(kanban.Input{Ids: ids, ScopeKey: "kanban", Model: st.model})
	// The demo doesn't persist; drain so the queue doesn't grow unbounded.
	st.model.DrainMoves()
}
