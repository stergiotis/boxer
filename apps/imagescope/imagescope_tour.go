package imagescope

// imagescope_tour.go enrols the window into the imzero2 demo registry
// (ADR-0057) in a seeded state, without window chrome: the synthetic field
// with a region marked on its bright blob, so the screenshot driver and the
// design-system review see the controls, the image with its colour bar and
// histogram, and the statistics table together; the sample stack at its
// second time as a three-band composite, with the stack's controls and the
// placement line (ADR-0398 §M4); and the stack with regions and an
// operation run on it (ADR-0399 §M2, ADR-0400 §M2). The stage has no runtime:
// the jobs run without task rows, and Open… and Send to play say that there
// is no bus.

import (
	"context"
	"image"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
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

func init() {
	registry.Register(registry.Demo{
		Name:     "imagescope-stack",
		Category: "Science",
		Title:    icons.IconFileImage + " imagescope — a selection of a stack",
		Stage:    [2]float32{1040, 840},
		Flags:    registry.DemoFlagNeedsLargeArea | registry.DemoFlagNonDeterministic,
		Kind:     registry.DemoKindUX,
		Description: "The image scope window on the synthetic stack of three times and four bands, placed in a " +
			"projected reference system: a stepper and a slider for the time, the band choice showing three " +
			"bands as red, green and blue, the line naming the frame and the pixel size, and the statistics " +
			"of a marked region of the selection shown.",
		Init:           tourStackInit,
		RenderStateful: tourRender,
		SourceFunc:     (*App).renderStackControls,
	})
}

func init() {
	registry.Register(registry.Demo{
		Name:     "imagescope-regions",
		Category: "Science",
		Title:    icons.IconFileImage + " imagescope — regions and an operation",
		Stage:    [2]float32{1040, 1000},
		Flags:    registry.DemoFlagNeedsLargeArea | registry.DemoFlagNonDeterministic,
		Kind:     registry.DemoKindUX,
		Description: "The image scope window on the synthetic stack with regions drawn over it — a rectangle and a " +
			"line made by the person, an ellipse made by an agent, drawn dashed — their table with the figures " +
			"of each, measured off the frame, and the operations section with an expression over two bands " +
			"planned and run, its result in the image list with its record.",
		Init:           tourRegionsInit,
		RenderStateful: tourRegionsRender,
		SourceFunc:     (*App).renderRegionsPane,
	})
}

// tourRegionsInit opens the stack with three regions, measures them and
// runs an expression over two bands; the work lands off the frame.
func tourRegionsInit(ids *c.WidgetIdStack) (state any) {
	inst := tourStackInit(ids).(*App)
	if inst.pic == nil {
		return inst
	}
	inst.viewer.SetRegion(image.Rectangle{})
	for _, r := range []struct {
		spec   RegionSpec
		origin string
	}{
		{RegionSpec{Label: "water", Shape: ShapeArgs{Kind: "rectangle", Centre: [2]float64{35, 60}, Size: [2]float64{40, 50}}}, originPerson},
		{RegionSpec{Label: "field", Shape: ShapeArgs{Kind: "ellipse", Centre: [2]float64{120, 60}, Radii: [2]float64{22, 14}, Angle: 25}}, "task:tour"},
		{RegionSpec{Label: "transect", Shape: ShapeArgs{Kind: "line", From: [2]float64{10, 100}, To: [2]float64{150, 20}}}, originPerson},
	} {
		_, _ = inst.addRegion(r.spec, r.origin)
	}
	_ = inst.startRegionMeasure(nil, true)
	inst.chooseOperator("expr")
	inst.panel.setParam("expression", "(B8 - B4) / (B8 + B4)")
	inst.panel.inputs = []RunInputArgs{{Image: openedId, Band: "B8"}, {Image: openedId, Band: "B4"}}
	_ = inst.startRun(inst.panelArgs())
	return inst
}

func tourRegionsRender(ids *c.WidgetIdStack, state any) {
	tourRender(ids, state)
	inst, ok := state.(*App)
	if !ok || inst == nil || inst.pic == nil {
		return
	}
	inst.renderRegionsPane()
	inst.renderOperationsPane()
}

// tourStackRegion is the region the stack tour marks: across the boundary
// of the two covers.
var tourStackRegion = image.Rect(50, 30, 110, 90)

func tourStackInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.bindIds(ids)
	st, err := samples.StackByName(samples.StackBands)
	if err != nil {
		return inst
	}
	sel := selection{at: []int{1, 0}, bands: []int{3, 2, 1}}
	if pic, err := storeOf(context.Background(), st, sel, func(uint64, uint64, string) {}); err == nil {
		inst.setShown(pic, newStackView(st, sel))
		inst.viewer.SetRegion(tourStackRegion)
	}
	return inst
}

func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.bindIds(ids)
	if img, err := samples.ByName(samples.NameField); err == nil {
		if pic, err := tiles.FromImage(context.Background(), img, tiles.Options{}); err == nil {
			inst.setPic(pic)
		}
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
	if inst.pic != nil {
		inst.renderViewer(func() imageview.Events { return inst.viewer.Render(1000, 480) })
		inst.renderMeasurePane()
	}
}
