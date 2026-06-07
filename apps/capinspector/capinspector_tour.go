package capinspector

// capinspector_tour.go enrols the reworked capability schematic into the
// imzero2 demo registry (ADR-0057) so the central TestDriver captures it in the
// widgets tour. The inspector is a windowed AppI whose graph only appears once a
// capability is selected, and its body is built from PanelTopInside /
// PanelCentralInside (window panels) — neither of which fits the tour stage,
// which renders each demo into a plain AllocateUiAtRect Ui. So rather than
// drive App.Frame, each scene pins a selected cap, seeds the effective backends
// the carousel would have set at boot, and renders a focused, panel-free view:
// the picker row, the cap heading, then the layered schematic + activity strip
// (the inline renderGraph). This is screenshot scaffolding only — the live
// inspector still opens from the carousel status bar.

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// capTourScenes is one entry per registered Demo: a stable name plus the cap to
// pin selected before rendering. facts (chstore vs InMem) shows the canonical
// available-vs-effective backend pair; task shows a second two-backend cap from
// a different position in the layout.
var capTourScenes = []struct {
	name  string
	capId CapId
	title string
	desc  string
}{
	{"capinspector-facts", CapFacts, icons.PhPlug + " capability inspector — facts",
		"The cap-broker schematic on the layeredgraph widget (ADR-0069): App → capability → " +
			"backend laid out top-down by Graphviz dot in-process. The selected facts cap is green; " +
			"its effective backend (chstore) is amber and the alternative (InMem) dim. The strip below " +
			"is the companion per-cap audit-activity sparkline (idle in the gallery; live in the carousel)."},
	{"capinspector-task", CapTask, icons.PhPlug + " capability inspector — task",
		"The same schematic with the task capability selected (task + supervisor backends), showing how " +
			"the green selection and amber effective backend track the picked cap."},
}

func init() {
	for _, sc := range capTourScenes {
		registry.Register(registry.Demo{
			Name:           sc.name,
			Category:       "Runtime",
			Title:          sc.title,
			Stage:          [2]float32{1040, 700},
			Flags:          registry.DemoFlagNeedsLargeArea,
			Kind:           registry.DemoKindUX,
			Description:    sc.desc,
			Init:           makeCapTourInit(sc.capId),
			RenderStateful: capTourRenderStateful,
			SourceFunc:     (*App).renderGraph,
		})
	}
}

// capTourState holds the inspector App instance bound to this scene plus the
// cap it pins selected each frame (Init runs for every Demo at setup, so the
// last writer would win there — pinning per-frame keeps each scene honest).
type capTourState struct {
	app   *App
	capId CapId
}

// makeCapTourInit seeds the effective backends and builds an inspector App with
// the scene's cap pre-selected. seedTourBackends is idempotent, so running it
// for every Demo's Init is harmless.
func makeCapTourInit(capId CapId) func(ids *c.WidgetIdStack) (state any) {
	return func(_ *c.WidgetIdStack) (state any) {
		seedTourBackends()
		inst := newApp()
		inst.selectedCap = capId
		return &capTourState{app: inst, capId: capId}
	}
}

func capTourRenderStateful(_ *c.WidgetIdStack, state any) {
	st, ok := state.(*capTourState)
	if !ok || st == nil {
		return
	}
	seedTourBackends()
	inst := st.app
	inst.selectedCap = st.capId
	spec, ok := Registry[inst.selectedCap]
	if !ok {
		return
	}
	// The inspector's widgets emit through the package-level id stack (App.Frame
	// resets + scopes it by the per-instance seed). Reproduce that scope here so
	// the picker and schematic get disjoint, stable ids.
	ids.Reset()
	for range c.IdScope(ids.PrepareSeq(inst.seed)) {
		inst.renderPicker()
		c.AddSpace(styletokens.GapItems(inst.density))
		heading(spec.Display)
		inst.renderGraph(spec)
	}
}

// seedTourBackends marks one effective backend per cap, mirroring the carousel's
// boot-time SetActiveBackend calls (active_backends.go) so the schematic shows
// amber effective nodes and a green/idle (not all-degraded-red) cap row.
func seedTourBackends() {
	SetActiveBackend(CapRun, "runinfo")
	SetActiveBackend(CapFacts, "chstore")
	SetActiveBackend(CapBus, "inprocbus")
	SetActiveBackend(CapFs, "fsbroker")
	SetActiveBackend(CapPersist, "mem")
	SetActiveBackend(CapTask, "task")
}
