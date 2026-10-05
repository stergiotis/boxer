package widgets

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/breadcrumbs"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "breadcrumbs",
		Category: "Layout & widgets",
		Title:    icons.PhCaretRight + " breadcrumbs",
		Stage:    [2]float32{1024, 240},
		Kind:     registry.DemoKindMixed,
		Description: "widgets/breadcrumbs as a path, where a click returns to an ancestor, and as a wizard's steps, " +
			"where done steps are ticked and a locked one says why on hover. The current item is host-owned state.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newBreadcrumbsDemoState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoBreadcrumbs(ids, state.(*breadcrumbsDemoState))
		},
		SourceFunc: demoBreadcrumbs,
	})
}

// breadcrumbsDemoState is one gallery window's two trails.
type breadcrumbsDemoState struct {
	path      breadcrumbs.Model
	pathState breadcrumbs.State
	steps     breadcrumbs.Model
	stepState breadcrumbs.State
}

func newBreadcrumbsDemoState() (st *breadcrumbsDemoState) {
	st = &breadcrumbsDemoState{
		path: breadcrumbs.Model{Labels: []string{"home", "projects", "boxer", "doc", "adr"}},
		steps: breadcrumbs.Model{
			Labels: []string{"Connect", "Databases", "Structure", "Differences", "Sync"},
			Done:   []bool{true, true, false, false, false},
			Locked: []string{"", "", "", "plan the structure first", "plan the structure first"},
		},
	}
	st.stepState.SetCurrent(2)
	return
}

func demoBreadcrumbs(ids *c.WidgetIdStack, st *breadcrumbsDemoState) {
	c.Label("A path: a click on an ancestor drops the segments after it.").Send()
	res := breadcrumbs.Render(breadcrumbs.Input{Ids: ids, ScopeKey: "path", Model: &st.path, State: &st.pathState})
	if res.Clicked >= 0 {
		st.path.Labels = st.path.Labels[:res.Clicked+1]
		st.pathState.SetCurrent(-1)
	}
	c.Label("current: /" + strings.Join(st.path.Labels, "/")).Send()
	c.Separator().Send()
	c.Label("A wizard's steps: done steps are ticked, locked ones say why on hover.").Send()
	breadcrumbs.Render(breadcrumbs.Input{Ids: ids, ScopeKey: "steps", Model: &st.steps, State: &st.stepState})
}
