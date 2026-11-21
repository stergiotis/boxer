package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
)

// The imageview demo (ADR-0373 M2, M3): three synthetic images — a 16-bit
// greyscale field whose interesting range is a small part of what 16 bits
// hold, an 8-bit colour image with an alpha ramp, and a tiny greyscale one to
// zoom into — and the controls a recipe has. The field is larger than the
// view, so fitted it is drawn from a reduced level; its saturated column and
// bright blob are what a percentile stretch is for. Nothing is read from
// disk, so the demo captures the same on every host.
type imageViewDemoState struct {
	v       *imageview.Viewer
	images  []imageViewDemoImage
	current int
	// Bound values keep one address across frames. A checkbox writes its
	// bound value between frames, when the responses arrive, so invert and
	// flag are compared with the recipe before the boxes are drawn and never
	// overwritten from it while a toggle may be pending.
	invert, flag, smooth bool
	// linked is the composite's link as its check box holds it.
	linked  bool
	problem string
}

type imageViewDemoImage struct {
	name string
	img  imaging.Image
}

// imageViewDemoPalettes is the short list the demo offers; the widget has
// more (imageview.PaletteNames).
var imageViewDemoPalettes = []string{imageview.PaletteGray, "batlow", "viridis", "magma", "vik"}

// imageViewDemoPercentiles are the percentile stretches the demo offers.
var imageViewDemoPercentiles = [][2]float64{{1, 99}, {0.1, 99.9}}

func newImageViewDemoState(ids *c.WidgetIdStack) *imageViewDemoState {
	st := &imageViewDemoState{v: imageview.New(ids, "imageview-demo", imageview.Options{})}
	for _, im := range []struct{ label, name string }{
		{"field 16-bit", samples.NameField}, {"colour + alpha", samples.NameColour}, {"small 8-bit", samples.NameSmall},
		{"elevation signed 16-bit", samples.NameElevation},
	} {
		if img, err := samples.ByName(im.name); err == nil {
			st.images = append(st.images, imageViewDemoImage{im.label, img})
		}
	}
	st.v.SetImage(st.images[0].img)
	return st
}

func imageViewDemoButton(ids *c.WidgetIdStack, key string, seq uint64, label string, selected bool) bool {
	for range c.IdScope(ids.PrepareStr(key)) {
		return c.Button(ids.PrepareSeq(seq), c.Atoms().Text(label).Keep()).Selected(selected).SendResp().HasPrimaryClicked()
	}
	return false
}

// set applies a change to the recipe and keeps what the viewer
// said about it.
func (st *imageViewDemoState) set(change func(r *display.Recipe)) {
	r := st.v.Recipe()
	change(&r)
	st.problem = ""
	if err := st.v.SetRecipe(r); err != nil {
		st.problem = err.Error()
	}
}

// syncChecks sets the checkboxes to what the viewer's recipe says, after
// anything that replaced the recipe.
func (st *imageViewDemoState) syncChecks() {
	r := st.v.Recipe()
	st.invert, st.flag = r.Invert, r.FlagClipped
}

func demoImageView(ids *c.WidgetIdStack, st *imageViewDemoState) {
	img := st.v.Image()
	recipe := st.v.Recipe()
	plane, single := st.v.Plane()

	for range c.HorizontalTop().KeepIter() {
		c.Label("Image").Selectable(false).Send()
		for i, im := range st.images {
			if imageViewDemoButton(ids, "image", uint64(i), im.name, st.current == i) {
				st.current = i
				st.v.SetImage(im.img)
				st.problem = ""
				st.syncChecks()
			}
		}
		if imageViewDemoButton(ids, "nav", 0, "fit", false) {
			st.v.Fit()
		}
		if imageViewDemoButton(ids, "nav", 1, "1:1", false) {
			st.v.ActualSize()
		}
		c.Checkbox(ids.PrepareStr("smooth"), st.smooth, "smooth").SendRespVal(&st.smooth)
		st.v.Opts.Smooth = st.smooth
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label("Show").Selectable(false).Send()
		comp := st.v.IsComposite()
		if imageViewDemoButton(ids, "plane", 99, "as stored", !single && !comp) {
			st.problem = ""
			if err := st.v.SetAsStored(); err != nil {
				st.problem = err.Error()
			}
			st.syncChecks()
		}
		for i, name := range img.PlaneLabels() {
			if imageViewDemoButton(ids, "plane", uint64(i), name, single && plane == i) {
				st.problem = ""
				if err := st.v.SetPlane(i); err != nil {
					st.problem = err.Error()
				}
				st.syncChecks()
			}
		}
		// A colour image's red, green and blue as a composite (ADR-0401):
		// linked over the range of their data.
		if img.Model().IsColor() && imageViewDemoButton(ids, "plane", 98, "composite", comp) {
			st.problem = ""
			if err := st.v.SetComposite(imageViewDemoComposite(img), true); err != nil {
				st.problem = err.Error()
			}
			st.linked = true
			st.syncChecks()
		}
		c.Label("Limits").Selectable(false).Send()
		shownIdx := max(plane, 0)
		if layers, ok := st.v.Composite(); ok {
			hand, _ := st.v.LayerInHand()
			shownIdx = layers[hand].Plane
		}
		shown := img.Plane(shownIdx)
		if imageViewDemoButton(ids, "limits", 0, "data", recipe.Limits.Source == display.LimitsSourceData) {
			if l, ok := display.DataLimits(shown); ok {
				st.set(func(r *display.Recipe) { r.Limits = l })
			}
		}
		if imageViewDemoButton(ids, "limits", 1, "nominal", recipe.Limits.Source == display.LimitsSourceNominal) {
			if l, ok := display.NominalLimits(shown.Kind(), shown.Transform()); ok {
				st.set(func(r *display.Recipe) { r.Limits = l })
			}
		}
		measured := single || comp || !img.Model().IsColor()
		for i, pct := range imageViewDemoPercentiles {
			current := recipe.Limits.Source == display.LimitsSourcePercentile &&
				recipe.Limits.PercentLo == pct[0] && recipe.Limits.PercentHi == pct[1]
			label := fmt.Sprintf("%g–%g %%", pct[0], pct[1])
			if measured {
				if imageViewDemoButton(ids, "percentile", uint64(i), label, current) {
					st.problem = ""
					st.set(func(r *display.Recipe) {
						r.Limits = display.Limits{Source: display.LimitsSourcePercentile, PercentLo: pct[0], PercentHi: pct[1]}
					})
				}
			}
		}
		if measured && imageViewDemoButton(ids, "limits", 2, "zscale", recipe.Limits.Source == display.LimitsSourceZScale) {
			st.set(func(r *display.Recipe) { r.Limits = display.Limits{Source: display.LimitsSourceZScale} })
		}
	}
	if layers, ok := st.v.Composite(); ok {
		st.renderCompositeRow(ids, img, layers)
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label("Transfer").Selectable(false).Send()
		for i, tr := range display.AllTransfers {
			if imageViewDemoButton(ids, "transfer", uint64(i), tr.String(), recipe.Transfer == tr) {
				st.set(func(r *display.Recipe) { r.Transfer = tr })
			}
		}
		// A toggle since the last frame shows as a bound value that differs
		// from the recipe; a recipe the viewer refuses puts the box back.
		if current := st.v.Recipe(); st.invert != current.Invert || st.flag != current.FlagClipped {
			st.set(func(r *display.Recipe) { r.Invert, r.FlagClipped = st.invert, st.flag })
			st.syncChecks()
		}
		c.Checkbox(ids.PrepareStr("invert"), st.invert, "invert").SendRespVal(&st.invert)
		c.Checkbox(ids.PrepareStr("flag"), st.flag, "flag clipped").SendRespVal(&st.flag)
		c.Label("Palette").Selectable(false).Send()
		for i, name := range imageViewDemoPalettes {
			current := recipe.Palette == name || (recipe.Palette == "" && name == imageview.PaletteGray)
			if imageViewDemoButton(ids, "palette", uint64(i), name, current) {
				st.set(func(r *display.Recipe) { r.Palette = name })
			}
		}
	}

	ev := st.v.Render(960, 470)

	// The limits may have been dragged in this Render.
	recipe = st.v.Recipe()
	levels := "levels building"
	if st.v.LevelsReady() {
		levels = "levels ready"
	}
	c.Label(fmt.Sprintf("%s · %d × %d · %s %s · limits %.6g … %.6g (%s) · %s · level %d, %s",
		img.Provenance().SourceName, img.Width(), img.Height(), img.Model(), img.Kind(),
		recipe.Limits.Lo, recipe.Limits.Hi, recipe.Limits.Source, recipe.Transfer, st.v.Level(), levels)).Selectable(false).Send()
	if ev.ClickedOk {
		c.Label(fmt.Sprintf("clicked pixel (%d, %d)", ev.Clicked.Col, ev.Clicked.Row)).Selectable(false).Send()
	}
	if st.problem != "" {
		c.Label("refused: " + st.problem).Wrap().Send()
	}
}

// imageViewDemoComposite is a colour image's red, green and blue planes as
// a linked composite over the range of their data together.
func imageViewDemoComposite(img imaging.Image) []imageview.CompositeLayer {
	recipe := display.DefaultPlaneRecipe(img.Plane(0))
	for i := 1; i < 3; i++ {
		if l, ok := display.DataLimits(img.Plane(i)); ok {
			recipe.Limits.Lo, recipe.Limits.Hi = min(recipe.Limits.Lo, l.Lo), max(recipe.Limits.Hi, l.Hi)
		}
	}
	colours := display.DefaultLayerColors(3)
	layers := make([]imageview.CompositeLayer, 3)
	for i := range layers {
		layers[i] = imageview.CompositeLayer{Plane: i, Layer: display.Layer{Recipe: recipe, Color: colours[i]}}
	}
	return layers
}

// renderCompositeRow is the composite's controls: the layer in hand, each
// layer's visibility, and the link.
func (st *imageViewDemoState) renderCompositeRow(ids *c.WidgetIdStack, img imaging.Image, layers []imageview.CompositeLayer) {
	hand, _ := st.v.LayerInHand()
	for range c.HorizontalTop().KeepIter() {
		c.Label("Layers").Selectable(false).Send()
		for i, l := range layers {
			label := img.PlaneLabels()[l.Plane] + " in " + l.Color.String()
			if l.Hidden {
				label += " (hidden)"
			}
			if imageViewDemoButton(ids, "layer", uint64(i), label, i == hand) {
				st.problem = ""
				if i == hand {
					// A second click on the layer in hand shows or hides it.
					l.Hidden = !l.Hidden
					if err := st.v.SetLayer(i, l); err != nil {
						st.problem = err.Error()
					}
				} else if err := st.v.SetLayerInHand(i); err != nil {
					st.problem = err.Error()
				}
				st.syncChecks()
			}
		}
		if st.linked != st.v.CompositeLinked() {
			if err := st.v.SetCompositeLinked(st.linked); err != nil {
				st.problem = err.Error()
			}
		}
		c.Checkbox(ids.PrepareStr("linked"), st.linked, "linked").SendRespVal(&st.linked)
	}
}

func init() {
	registry.Register(registry.Demo{
		Name: "imageview", Category: "Graphics & canvas", Title: icons.IconFileImage + " imageview (samples and a recipe)",
		Stage:       [2]float32{1024, 800},
		Flags:       registry.DemoFlagNeedsLargeArea | registry.DemoFlagNonDeterministic,
		Kind:        registry.DemoKindUX,
		Description: "The image viewer of ADR-0373: an image is planes of samples kept at their own bit depth, and what is shown is those planes through a recipe — limits, a transfer function, and for a single plane a colour map — mapped in Go for the tiles in view. A colour image opens as stored; any one plane can be shown by itself instead. The readout under the image is read from the samples, not from what was drawn, so it does not change with the recipe. Under a single plane is its histogram, counted over the whole plane, with the two limits as handles to drag; a percentile stretch sets them where a few extreme samples cannot pull them wide, and zscale finds them around the bulk of the values from a sample of a thousand. A colour image can also be shown as a composite of its planes (ADR-0401): a layer per plane in red, green and blue, linked under one recipe or, unlinked, each under its own — which the viewer then states beside the image — with the histogram and the limits of the layer in hand. Shown smaller than it is, the image is drawn from means of blocks of samples, and the readout says so. Pan by dragging, zoom with the wheel. Tiles arrive from worker goroutines, so a capture taken at once can show the image partly drawn.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			return newImageViewDemoState(ids)
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoImageView(ids, state.(*imageViewDemoState))
		},
	})
}
