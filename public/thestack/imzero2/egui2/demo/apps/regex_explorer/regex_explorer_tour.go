// Demo-registry enrollment for the regex explorer (ADR-0057). This replaces
// the former per-app screenshot tour: instead of a settle/capture/advance state
// machine driven by a screenshot-mode SeededFuncApp, the empty and populated
// scenes register as stateless Demos that render the explorer body into the
// host Ui scope. The central TestDriver (widgets) captures one PNG per scene.
//
// regex_explorer keeps its package-level `app` singleton: tour mode has always
// read/written it directly (see the AppInstance.Frame swap and the note above
// RenderWindow), so each Demo pins the scene's pattern/haystack on `app` and
// calls RenderWindow. The drivers render demos in isolation (the TestDriver one
// per frame, the gallery in a per-demo id scope), so the shared singleton does
// not collide across scenes. Flagged NonDeterministic — the explorer scans a
// synthetic corpus whose byte output is not stable across runs.

package regex_explorer

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// regexScenes is one entry per registered Demo: a name plus the pattern and
// haystack to pin before rendering.
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
			Name:        sc.name,
			Category:    "Tools",
			Title:       sc.title,
			Stage:       [2]float32{1100, 720},
			Flags:       registry.DemoFlagNonDeterministic | registry.DemoFlagNeedsLargeArea,
			Kind:        registry.DemoKindMixed,
			Description: sc.desc,
			Render:      makeTourRender(sc.pattern, sc.haystack),
			SourceFunc:  RenderWindow,
		})
	}
}

// makeTourRender returns a stateless Render that pins the scene's inputs on the
// package-level `app` (under app.mu, since a background scan goroutine reads
// them) and binds app.ids to the host-supplied stack so widget ids derive from
// the host scope, then draws the explorer body via RenderWindow.
func makeTourRender(pattern, haystack string) func(ids *c.WidgetIdStack) {
	return func(ids *c.WidgetIdStack) {
		app.mu.Lock()
		app.pattern = pattern
		app.haystack = haystack
		app.patternList = ""
		app.replacement = ""
		app.mu.Unlock()
		app.ids = ids
		RenderWindow()
	}
}
