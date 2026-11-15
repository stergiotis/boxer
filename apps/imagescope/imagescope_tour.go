package imagescope

// imagescope_tour.go enrols the window into the imzero2 demo registry
// (ADR-0057) in a seeded state, without window chrome: the synthetic field
// with a region marked on its bright blob, so the screenshot driver and the
// design-system review see the controls, the image with its colour bar and
// histogram, and the statistics table together. The stage has no runtime:
// the jobs run without task rows, and Open… and Send to play say that there
// is no bus.

import (
	"image"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// tourRegion is the region the tour marks: the field's bright blob and some
// of the wave around it.
var tourRegion = image.Rect(900, 250, 1060, 410)

func init() {
	registry.Register(registry.Demo{
		Name:     "imagescope",
		Category: "Science",
		Title:    icons.IconFileImage + " imagescope — a region measured",
		Stage:    [2]float32{1040, 840},
		Flags:    registry.DemoFlagNeedsLargeArea | registry.DemoFlagNonDeterministic,
		Kind:     registry.DemoKindUX,
		Description: "The image scope window on a synthetic 16-bit field with a region marked: the controls, " +
			"the image through its recipe with the colour bar and the plane's histogram, and under it the " +
			"region's statistics, one row per plane, computed from the samples at full resolution. Tiles and " +
			"the statistics arrive from background work, so a capture taken at once can show them pending.",
		Init:           tourInit,
		RenderStateful: tourRender,
		SourceFunc:     (*App).renderMeasurePane,
	})
}

func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.bindIds(ids)
	if img, err := samples.ByName(samples.NameField); err == nil {
		inst.setImage(img)
		inst.viewer.SetRegion(tourRegion)
	}
	return inst
}

func tourRender(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.bindIds(ids)
	inst.takeResults()
	inst.mirrorPhase()
	inst.renderControls()
	if !inst.img.IsZero() {
		inst.viewer.Render(1000, 480)
		inst.renderMeasurePane()
	}
}
