// Demo-registry enrollment for the sccmap "Repo code exploration" app
// (ADR-0057). sccmap is a stateful AppI, so each scene registers via the
// Init / RenderStateful pair: Init builds an App bound to the host-supplied
// WidgetIdStack — pinned to a size/color metric and to the include-tests /
// show-values toggles the scene means to showcase — runs a synchronous scc
// scan, and builds the treemap; RenderStateful draws the app body into
// the host Ui scope each frame via App.Frame (no c.Window — the driver owns
// the wrapping). The central TestDriver (widgets) captures one PNG per scene.
//
// The scenes exercise the two refinements: the in-cell humanized values
// (size · color, drawn under each tile name) and the test filter — the
// "bytes-tests" scene flips Include tests on and switches the size metric to
// Bytes so the byte humanizer ("1.2 MB") shows in the tiles.

package sccmap

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// sccmapScenes is one entry per registered Demo: a name plus the metric
// selection and toggle state to pin before rendering.
var sccmapScenes = []struct {
	name         string
	title        string
	desc         string
	sizeIdx      int
	colorIdx     int
	includeTests bool
	showValues   bool
}{
	{
		"sccmap-treemap",
		icons.PhGridNine + " Repo code exploration — treemap",
		"The scc treemap over this repo: tile area = code lines, colour = log-scaled complexity, with each tile's humanized size · complexity drawn under its name. Tests and generated files are excluded.",
		defaultSizeMetricIdx, defaultColorMetricIdx, false, true,
	},
	{
		"sccmap-bytes-tests",
		icons.PhGridNine + " Repo code exploration — bytes, tests included",
		"The same treemap sized by bytes (tiles show humanized byte sizes like 1.2 MB) with the Include tests toggle on, so test files re-enter the statistics and the distribution summaries below.",
		2, defaultColorMetricIdx, true, true, // size index 2 == Bytes
	},
}

func init() {
	for _, sc := range sccmapScenes {
		registry.Register(registry.Demo{
			Name:           sc.name,
			Category:       "Tools",
			Title:          sc.title,
			Stage:          [2]float32{1100, 720},
			Flags:          registry.DemoFlagNeedsLargeArea,
			Kind:           registry.DemoKindMixed,
			Description:    sc.desc,
			Init:           makeTourInit(sc.sizeIdx, sc.colorIdx, sc.includeTests, sc.showValues),
			RenderStateful: tourRenderStateful,
			SourceFunc:     (*App).Frame,
		})
	}
}

// makeTourInit returns an Init that builds an App bound to the host-supplied
// id stack, pins the scene's metric/toggle state, runs a synchronous scc scan,
// and builds the treemap + legend against that stack. Where App.Mount kicks
// off a background scan and lets Frame render a placeholder until it lands, the
// tour scans inline so the scene's data is ready before the first capture. ids
// must be set before rebuildTreemap, since the treemap and colorscale derive
// their widget ids from it.
func makeTourInit(sizeIdx, colorIdx int, includeTests, showValues bool) func(ids *c.WidgetIdStack) (state any) {
	return func(ids *c.WidgetIdStack) (state any) {
		inst := newApp()
		inst.ids = ids
		inst.sizeMetricIdx = sizeIdx
		inst.colorMetricIdx = colorIdx
		inst.includeTests = includeTests
		inst.showValues = showValues
		// Screenshot harness: scan synchronously so the scene has data at Init
		// time (the interactive app scans on a background job instead). The
		// header box shows "." — the current repo — rather than a
		// machine-specific absolute path that would leak into a committed PNG.
		inst.repoPath = "."
		if d, scanErr := scanSccSync(inst.repoPath); scanErr == nil {
			inst.data = d
		}
		inst.rebuildTreemap()
		state = inst
		return
	}
}

// tourRenderStateful rebinds the App to the host id stack (it's stable across
// frames, but cheap to reassert) and draws the app body. App.Frame ignores
// its FrameContextI, so nil is safe here.
func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.ids = ids
	_ = inst.Frame(nil)
}
