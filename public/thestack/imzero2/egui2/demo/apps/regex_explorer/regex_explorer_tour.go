// Demo-registry enrollment for the regex explorer (ADR-0057). This replaces
// the former per-app screenshot tour: instead of a settle/capture/advance state
// machine driven by a screenshot-mode SeededFuncApp, the empty and populated
// scenes register as Demos that render the explorer body into the host Ui scope.
// The central TestDriver (widgets) captures one PNG per scene.
//
// Each scene owns a private [App] built by BusInit, rather than pinning fields
// on a package-level singleton the way this file used to. The registry's
// stateful contract hands BusInit both the host WidgetIdStack and the host
// BusI, which is exactly what an App needs — so the scenes get real
// ClickHouse-backed result tabs instead of the "bus unavailable" error the
// singleton path produced, and two scenes rendered in the same frame cannot
// scribble on each other's state.
//
// Flagged NonDeterministic — the explorer runs live queries whose byte output
// is not stable across runs.

package regex_explorer

import (
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// regexScenes is one entry per registered Demo: a name plus the pattern and
// haystack to seed before rendering.
var regexScenes = []struct {
	name     string
	title    string
	desc     string
	pattern  string
	haystack string
}{
	{"regex-explorer-empty", icons.IconSearch + " Regex explorer — empty",
		"The regex explorer with empty inputs — the pattern/haystack editors, cheatsheet panel, and result tabs in their initial state.", "", ""},
	{"regex-explorer-populated", icons.IconSearch + " Regex explorer — populated",
		"The regex explorer evaluating \\w+ against \"hello world 123\" — highlighted matches with the result tabs populated.", `\w+`, "hello world 123"},
}

func init() {
	for _, sc := range regexScenes {
		registry.Register(registry.Demo{
			Name:           sc.name,
			Category:       "Tools",
			Title:          sc.title,
			Stage:          [2]float32{1100, 720},
			Flags:          registry.DemoFlagNonDeterministic | registry.DemoFlagNeedsLargeArea,
			Kind:           registry.DemoKindMixed,
			Description:    sc.desc,
			BusInit:        makeTourInit(sc.pattern, sc.haystack),
			RenderStateful: renderTourScene,
			SourceFunc:     (*App).RenderWindow,
		})
	}
}

// makeTourInit returns the scene's BusInit: one [App] per demo instance,
// seeded with the scene's inputs and wired to the host's id stack and bus.
// Called once per Mount, so the seeding happens before any frame rather
// than being re-applied on every one.
func makeTourInit(pattern string, haystack string) func(ids *c.WidgetIdStack, bus runtimeapp.BusI) (state any) {
	return func(ids *c.WidgetIdStack, bus runtimeapp.BusI) (state any) {
		inst := newApp()
		inst.ids = ids
		inst.setBus(bus)
		inst.pattern = pattern
		inst.haystack = haystack
		state = inst
		return
	}
}

// renderTourScene draws one scene's App. Rebinds ids every frame because
// the gallery renders demos inside a per-demo id scope and hands the
// current stack in; the seeded inputs persist on the App across frames.
func renderTourScene(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok {
		return
	}
	inst.ids = ids
	inst.RenderWindow()
}
