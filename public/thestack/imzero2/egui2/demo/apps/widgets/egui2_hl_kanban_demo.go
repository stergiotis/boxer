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
		Description: "A board of columns and the cards in them. Move a card by dragging it (a ghost follows the pointer, an insertion line shows where it lands) or with its ◀ ▶ / ▲ ▼ controls; click a card to select it (accent-stroked). Cards carry an optional accent bullet, a packed row of small legend-backed tally dots along the bottom (up to 3 kinds, each repeated by its count with no gap between kinds), and a one-level parent link (a \"sub-item of …\" trailer; parents show a \"◱ N sub\" chip) — sub-items are scheduled independently of their parent. The legend above the board names each dot kind and shows its detail on hover.",
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
	model  *kanban.Model
	group  kanban.GroupModeE
	owners map[uint64]string // card id → owner, demoing GroupByField
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
		{ID: 4, Title: "Done", IsDone: true},
	}
	cards := []kanban.Card{
		{ID: 10, ColumnID: 3, Title: "Design board API", Subtitle: "concise imzero2 shape", Accent: acc(styletokens.AccentDefault), Dots: []kanban.DotTally{{ID: dotNeedsReview, Count: 2}}},
		{ID: 11, ColumnID: 2, Title: "Card frame + selection", Accent: acc(styletokens.InfoDefault)},
		{ID: 12, ColumnID: 1, Title: "Drag-and-drop", Subtitle: "deferred slice", Accent: acc(styletokens.WarningDefault), Dots: []kanban.DotTally{{ID: dotBlocked, Count: 1}, {ID: dotNeedsReview, Count: 3}}},
		{ID: 13, ColumnID: 4, Title: "Reconnaissance", Accent: acc(styletokens.SuccessDefault)},
		{ID: 14, ColumnID: 1, Title: "Independent column scroll", Subtitle: "deferred", Dots: []kanban.DotTally{{ID: dotBlocked, Count: 6}, {ID: dotNeedsReview, Count: 2}, {ID: dotSecurity, Count: 3}}},

		// Sub-items of #10, each scheduled in its own column.
		{ID: 20, ColumnID: 4, ParentID: 10, Title: "Data model"},
		{ID: 21, ColumnID: 3, ParentID: 10, Title: "Move helpers"},
		{ID: 22, ColumnID: 2, ParentID: 10, Title: "Column layout"},
	}
	m := kanban.NewModel(cols, cards)
	// DotLegend: cards above span 0 to 3 dot-kinds, each a tally rather than a
	// single flag (#11/#13/the sub-items carry none, #10 carries one kind
	// x2, #12 two kinds, #14 all three kinds — 6+2+3 — so the packed-tally
	// look is visible at a glance).
	m.DotLegend = []kanban.DotKind{
		{ID: dotBlocked, Color: acc(styletokens.ErrorDefault), Label: "Blocked", Tooltip: "Waiting on an external dependency or decision"},
		{ID: dotNeedsReview, Color: acc(styletokens.WarningDefault), Label: "Needs review", Tooltip: "Ready for a second pair of eyes before it moves on"},
		{ID: dotSecurity, Color: acc(styletokens.InfoDefault), Label: "Security", Tooltip: "Touches auth, data handling, or another security-sensitive area"},
	}
	return &kanbanDemoState{
		model: m,
		// Owners cut across the parent/child structure — the by-owner view
		// shows a lane per person plus an Unassigned lane (#14 has no owner).
		owners: map[uint64]string{
			10: "Alice", 12: "Alice", 20: "Alice", 22: "Alice",
			11: "Bob", 13: "Bob", 21: "Bob",
		},
	}
}

// Sample DotKind ids for newKanbanState's DotLegend, in legend display order.
const (
	dotBlocked uint64 = iota + 1
	dotNeedsReview
	dotSecurity
)

func demoKanban(ids *c.WidgetIdStack, st *kanbanDemoState) {
	// Group-mode toggle: flat (drag to move), a swimlane per parent, or a
	// swimlane per owner (GroupByField over caller-side owner data).
	for range c.Horizontal().KeepIter() {
		if c.SelectableLabel(ids.PrepareStr("g-flat"), st.group == kanban.GroupNone, "Flat").
			SendResp().HasPrimaryClicked() {
			st.group = kanban.GroupNone
		}
		if c.SelectableLabel(ids.PrepareStr("g-parent"), st.group == kanban.GroupByParent, "By parent").
			SendResp().HasPrimaryClicked() {
			st.group = kanban.GroupByParent
		}
		if c.SelectableLabel(ids.PrepareStr("g-owner"), st.group == kanban.GroupByField, "By owner").
			SendResp().HasPrimaryClicked() {
			st.group = kanban.GroupByField
		}
	}
	c.AddSpace(styletokens.GapInline(styletokens.DensityFromEnv()))
	kanban.RenderLegend(st.model.DotLegend)
	c.AddSpace(styletokens.GapInline(styletokens.DensityFromEnv()))
	kanban.Render(kanban.Input{
		Ids: ids, ScopeKey: "kanban", Model: st.model, Group: st.group,
		GroupField: func(cd *kanban.Card) (key, label string) {
			o := st.owners[cd.ID]
			return o, o
		},
	})
	// The demo doesn't persist; drain so the queue doesn't grow unbounded.
	st.model.DrainMoves()
}
