package widgets

import (
	"fmt"
	"math"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
)

// The imageview demo (ADR-0373 M2): three synthetic images — a 16-bit
// greyscale field whose interesting range is a small part of what 16 bits
// hold, an 8-bit colour image with an alpha ramp, and a tiny greyscale one to
// zoom into — and the controls a recipe has. Nothing is read from disk, so
// the demo captures the same on every host.
type imageViewDemoState struct {
	v       *imageview.Viewer
	images  []imageViewDemoImage
	current int
	// Bound values keep one address across frames.
	invert, flag, smooth bool
	problem              string
}

type imageViewDemoImage struct {
	name string
	img  imaging.Image
}

// imageViewDemoPalettes is the short list the demo offers; the widget has
// more (imageview.PaletteNames).
var imageViewDemoPalettes = []string{imageview.PaletteGray, "batlow", "viridis", "magma", "vik"}

func newImageViewDemoState(ids *c.WidgetIdStack) *imageViewDemoState {
	st := &imageViewDemoState{v: imageview.New(ids, "imageview-demo", imageview.Options{})}
	st.images = []imageViewDemoImage{
		{"field 16-bit", imageViewDemoField()},
		{"colour + alpha", imageViewDemoColour()},
		{"small 8-bit", imageViewDemoSmall()},
	}
	st.v.SetImage(st.images[0].img)
	return st
}

// imageViewDemoField is 1400 × 1000 of 16-bit samples: a slow wave between
// about 1500 and 4500, one bright blob reaching 40000, and a column of
// saturated pixels — the shape of a detector frame, where the nominal range
// shows almost nothing and a stretch shows the rest.
func imageViewDemoField() imaging.Image {
	const w, h = 1400, 1000
	s := make([]uint16, w*h)
	for y := range h {
		for x := range w {
			wave := (0.5 + 0.5*math.Sin(float64(x)/40)) * (0.5 + 0.5*math.Cos(float64(y)/55))
			dx, dy := float64(x-980), float64(y-330)
			blob := 36000 * math.Exp(-(dx*dx+dy*dy)/(2*45*45))
			v := 1500 + 3000*wave + blob
			if x == 200 && y%7 == 0 {
				v = 65535
			}
			s[y*w+x] = uint16(min(v, 65535))
		}
	}
	p, _ := imaging.NewPlaneU16(w, h, s)
	img, _ := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "synthetic field"})
	return img
}

// imageViewDemoColour is 640 × 480 of 8-bit RGBA: red rises with the column,
// green with the row, blue in blocks, and the right third fades to
// transparent.
func imageViewDemoColour() imaging.Image {
	const w, h = 640, 480
	var s [4][]uint8
	for i := range s {
		s[i] = make([]uint8, w*h)
	}
	for y := range h {
		for x := range w {
			i := y*w + x
			s[0][i] = uint8(x * 255 / (w - 1))
			s[1][i] = uint8(y * 255 / (h - 1))
			s[2][i] = uint8(((x/40 + y/40) % 2) * 200)
			s[3][i] = 255
			if x > 2*w/3 {
				s[3][i] = uint8(255 - (x-2*w/3)*255/(w/3))
			}
		}
	}
	planes := make([]imaging.Plane, 4)
	for i := range planes {
		planes[i], _ = imaging.NewPlaneU8(w, h, s[i])
	}
	img, _ := imaging.NewImage(imaging.ColorModelRGBA, planes, imaging.Provenance{SourceName: "synthetic colour"})
	return img
}

// imageViewDemoSmall is 48 × 32 of 8-bit samples: few enough pixels that the
// fitted view shows each as a square.
func imageViewDemoSmall() imaging.Image {
	const w, h = 48, 32
	s := make([]uint8, w*h)
	for y := range h {
		for x := range w {
			s[y*w+x] = uint8((x*5 + y*3) % 256)
		}
	}
	p, _ := imaging.NewPlaneU8(w, h, s)
	img, _ := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "synthetic small"})
	return img
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
				st.invert, st.flag, st.problem = false, false, ""
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
		if imageViewDemoButton(ids, "plane", 99, "as stored", !single) {
			st.problem = ""
			if err := st.v.SetAsStored(); err != nil {
				st.problem = err.Error()
			}
		}
		for i, name := range img.Model().PlaneNames() {
			if imageViewDemoButton(ids, "plane", uint64(i), name, single && plane == i) {
				st.problem = ""
				if err := st.v.SetPlane(i); err != nil {
					st.problem = err.Error()
				}
			}
		}
		c.Label("Limits").Selectable(false).Send()
		shown := img.Plane(max(plane, 0))
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
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label("Transfer").Selectable(false).Send()
		for i, tr := range display.AllTransfers {
			if imageViewDemoButton(ids, "transfer", uint64(i), tr.String(), recipe.Transfer == tr) {
				st.set(func(r *display.Recipe) { r.Transfer = tr })
			}
		}
		st.invert, st.flag = recipe.Invert, recipe.FlagClipped
		if c.Checkbox(ids.PrepareStr("invert"), st.invert, "invert").SendRespVal(&st.invert).HasChanged() {
			st.set(func(r *display.Recipe) { r.Invert = st.invert })
		}
		if c.Checkbox(ids.PrepareStr("flag"), st.flag, "flag clipped").SendRespVal(&st.flag).HasChanged() {
			st.set(func(r *display.Recipe) { r.FlagClipped = st.flag })
		}
		c.Label("Palette").Selectable(false).Send()
		for i, name := range imageViewDemoPalettes {
			current := recipe.Palette == name || (recipe.Palette == "" && name == imageview.PaletteGray)
			if imageViewDemoButton(ids, "palette", uint64(i), name, current) {
				st.set(func(r *display.Recipe) { r.Palette = name })
			}
		}
	}

	ev := st.v.Render(960, 400)

	c.Label(fmt.Sprintf("%s · %d × %d · %s %s · limits %.6g … %.6g (%s) · %s",
		img.Provenance().SourceName, img.Width(), img.Height(), img.Model(), img.Kind(),
		recipe.Limits.Lo, recipe.Limits.Hi, recipe.Limits.Source, recipe.Transfer)).Selectable(false).Send()
	if ev.ClickedOk {
		c.Label(fmt.Sprintf("clicked pixel (%d, %d)", ev.Clicked.Col, ev.Clicked.Row)).Selectable(false).Send()
	}
	if st.problem != "" {
		c.Label("refused: " + st.problem).Wrap().Send()
	}
}

func init() {
	registry.Register(registry.Demo{
		Name: "imageview", Category: "Graphics & canvas", Title: icons.IconFileImage + " imageview (samples and a recipe)",
		Stage:       [2]float32{1024, 760},
		Flags:       registry.DemoFlagNeedsLargeArea | registry.DemoFlagNonDeterministic,
		Kind:        registry.DemoKindUX,
		Description: "The image viewer of ADR-0373: an image is planes of samples kept at their own bit depth, and what is shown is those planes through a recipe — limits, a transfer function, and for a single plane a colour map — mapped in Go for the tiles in view. A colour image opens as stored; any one plane can be shown by itself instead. The readout under the image is read from the samples, not from what was drawn, so it does not change with the recipe. Pan by dragging, zoom with the wheel. Tiles arrive from worker goroutines, so a capture taken at once can show the image partly drawn.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			return newImageViewDemoState(ids)
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoImageView(ids, state.(*imageViewDemoState))
		},
	})
}
