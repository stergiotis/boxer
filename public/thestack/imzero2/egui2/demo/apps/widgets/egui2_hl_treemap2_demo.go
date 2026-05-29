//go:build llm_generated_opus47

package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// =============================================================================
// Treemap demo — Frame-based treemap widget with zoom-from-rect transitions.
// All behavior lives in the treemap package; this file just wires one
// instance into the demo window. (Window label still reads "Treemap2 …" as
// the original name users know; the filename keeps the "2" for stable git
// history alongside the historical scc demo.)
// =============================================================================

// treemap2DemoState carries the per-app-instance *treemap.Treemap so
// every open gallery window has its own zoom / animation state and
// derives widget ids from the host-supplied WidgetIdStack.
type treemap2DemoState struct {
	tm *treemap.Treemap
}

func init() {
	registry.Register(registry.Demo{
		Name:        "treemap2",
		Category:    "Charts & plots",
		Title:       icons.PhGridNine + " treemap",
		Stage:       [2]float32{1024, 600},
		Kind:        registry.DemoKindUX,
		Description: "Frame-based treemap with click-to-zoom drill-down on a synthetic hierarchical dataset.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			// Stretched depth palette into Batlow's bright half. The default
			// DepthColoring(Batlow8) puts a shallow tree (2-3 rendered
			// depths) into Batlow's dark navy/teal third (L≈0.15-0.20),
			// which sits at the same luminance as bg.panel (L=0.16) and
			// reads as uniform on the IDS dark canvas — the encoding is
			// technically correct but visually invisible. Picking from
			// the bright half (olive → orange → peach) puts every depth
			// well above panel luminance and makes the depth distinction
			// obvious. The treemap demo data renders at depths 0-1 (outer
			// cells and their leaves); the trailing entry covers any
			// deeper drill-down via DepthColoring's depth%len fallback.
			stretchedDepthPalette := []uint32{
				treemap.Batlow8[3], // olive  #627941 — outer cells (depth 0)
				treemap.Batlow8[5], // orange #E69858 — inner cells (depth 1)
				treemap.Batlow8[6], // peach  #FDB0A9 — leaves     (depth 2+)
			}
			state = &treemap2DemoState{
				tm: treemap.New(ids, "tm2-demo", makeSampleTree(),
					treemap.WithContainerSize(700, 450),
					treemap.WithAnimationDuration(0.28),
					treemap.WithColoring(treemap.DepthColoring(stretchedDepthPalette)),
				),
			}
			return
		},
		RenderStateful: func(_ *c.WidgetIdStack, state any) {
			state.(*treemap2DemoState).tm.Render()
		},
	})
}

// makeSampleTree returns a synthetic project-tree used by the treemap demo.
// Also consumed by integration tests that want a fixed, non-trivial shape.
func makeSampleTree() *layout.Node {
	return &layout.Node{Name: "project", Children: []*layout.Node{
		{Name: "src", Children: []*layout.Node{
			{Name: "frontend", Children: []*layout.Node{
				{Name: "components", Children: []*layout.Node{
					{Name: "Button.tsx", Size: 120},
					{Name: "Modal.tsx", Size: 200},
					{Name: "Table.tsx", Size: 350},
					{Name: "Form.tsx", Size: 180},
					{Name: "Nav.tsx", Size: 90},
				}},
				{Name: "pages", Children: []*layout.Node{
					{Name: "Dashboard.tsx", Size: 400},
					{Name: "Settings.tsx", Size: 250},
					{Name: "Profile.tsx", Size: 150},
				}},
				{Name: "hooks", Children: []*layout.Node{
					{Name: "useAuth.ts", Size: 80},
					{Name: "useQuery.ts", Size: 120},
					{Name: "useTheme.ts", Size: 60},
				}},
			}},
			{Name: "backend", Children: []*layout.Node{
				{Name: "api", Children: []*layout.Node{
					{Name: "routes.go", Size: 300},
					{Name: "middleware.go", Size: 180},
					{Name: "auth.go", Size: 220},
				}},
				{Name: "db", Children: []*layout.Node{
					{Name: "migrations.go", Size: 400},
					{Name: "queries.go", Size: 500},
					{Name: "pool.go", Size: 100},
				}},
				{Name: "worker", Children: []*layout.Node{
					{Name: "scheduler.go", Size: 250},
					{Name: "email.go", Size: 150},
					{Name: "export.go", Size: 200},
				}},
			}},
		}},
		{Name: "tests", Children: []*layout.Node{
			{Name: "unit", Size: 600},
			{Name: "integration", Size: 400},
			{Name: "e2e", Size: 300},
		}},
		{Name: "docs", Children: []*layout.Node{
			{Name: "README.md", Size: 100},
			{Name: "ARCHITECTURE.md", Size: 250},
			{Name: "API.md", Size: 350},
		}},
		{Name: "config", Children: []*layout.Node{
			{Name: "docker-compose.yml", Size: 80},
			{Name: "Makefile", Size: 60},
			{Name: ".github", Size: 120},
		}},
	}}
}
