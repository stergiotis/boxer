package imagescope

// A composite of layers (ADR-0401 §M2): the third way of showing an image,
// beside the image as stored and one plane. Each layer is one band of the
// stack shown — or one plane of an image that is no stack's — a recipe of
// its own and a colour of a fixed set, and the colours are added. The
// window keeps the composite in the viewer and states it in get_view, in the
// view resource and on screen; for a stack it takes the selection that
// holds the bands the layers name, at most the planes an image holds.

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
)

const (
	opSetComposite = "set_composite"
	opSetLayer     = "set_layer"

	limitsZScale   = "zscale"
	planeComposite = "composite"

	modeAsStored  = "as stored"
	modeSingle    = "single plane"
	modeComposite = "composite"
)

// LayerArgs is one layer of a composite as set_composite and set_layer take
// it (ADR-0401 §SD1).
type LayerArgs struct {
	Band   int            `json:",omitzero" desc:"for a stack: the band's position on the band axis, as set_bands takes it; not read for an image that is no stack's"`
	Plane  int            `json:",omitzero" desc:"for an image that is no stack's: the plane's index in get_view's Image.Planes; not read for a stack"`
	Colour string         `desc:"red, green, blue, cyan, magenta, yellow or white; the colours of the shown layers are added"`
	Hidden bool           `json:",omitzero" desc:"leave the layer out: its level is not added and its invalid samples do not make a pixel invalid"`
	Recipe *SetRecipeArgs `json:",omitzero" desc:"the layer's own recipe, for an unlinked composite; absent takes the range of the data of its plane, linear. Refused in a linked composite, whose one recipe is CompositeArgs.Recipe"`
}

// CompositeArgs is a composite, whole: set_composite's argument and part of
// the view resource.
type CompositeArgs struct {
	Layers []LayerArgs    `desc:"the layers in order, one to eight; a band or plane may be the plane of several layers. For a stack, the bands of all layers together are at most three different ones when its samples declare an invalid value, four otherwise: they are taken as the planes of one image"`
	Linked bool           `json:",omitzero" desc:"one recipe for every layer, and a recipe set later sets them all; false: each layer has its own, and the window states beside the image that the colours are not comparable with another view's"`
	Recipe *SetRecipeArgs `json:",omitzero" desc:"for a linked composite, its one recipe; absent takes the range of the data of the shown layers' planes together, linear. Data limits of a linked composite are that joint range; percentile and zscale limits are found from the layer in hand's plane"`
	InHand int            `json:",omitzero" desc:"the layer set_recipe and the histogram act on while the composite is unlinked, and whose plane a linked composite finds percentile and zscale limits from; 0 is the first"`
}

// SetLayerArgs changes one layer of the composite shown.
type SetLayerArgs struct {
	Index  int       `json:",omitzero" desc:"the layer, 0 the first, in get_view's View.Composite.Layers order"`
	Layer  LayerArgs `desc:"the layer, whole: read it from get_view's View.Composite.Layers, change what should change, send it back; its Recipe is refused while the composite is linked"`
	InHand bool      `json:",omitzero" desc:"also make it the layer in hand; false leaves the layer in hand where it is"`
}

// LayerInfo is a layer of the composite shown, as get_view reports it.
type LayerInfo struct {
	Index  int           `desc:"the layer's position, 0 the first"`
	Label  string        `desc:"the band's or plane's label, as the legend and the readout name it"`
	Band   int           `json:",omitzero" desc:"for a stack: the band's position on the band axis"`
	Plane  int           `json:",omitzero" desc:"the plane's index in Image.Planes"`
	Colour string        `desc:"the colour the layer is drawn in"`
	Pixel  string        `desc:"that colour at full level, as #rrggbb, the legend's swatch"`
	Hidden bool          `json:",omitzero" desc:"the layer is left out"`
	Recipe SetRecipeArgs `desc:"the layer's recipe with the limits in force"`
}

// CompositeInfo is the composite shown.
type CompositeInfo struct {
	Layers    []LayerInfo `desc:"the layers in order"`
	Linked    bool        `desc:"one recipe for every layer; false: each layer stretched by its own, and two such views are not comparable by colour"`
	InHand    int         `desc:"the layer set_recipe and the histogram act on"`
	Statement string      `json:",omitzero" desc:"the statement the window draws beside the image: unlinked, with two or more layers shown; empty when it draws none"`
	Pending   bool        `json:",omitzero" desc:"a composite was set whose bands the stack's selection shown does not hold yet; set_composite and set_layer settle when it is shown"`
}

// ClippedInfo is the share of a plane's valid samples the limits in force
// clip (ADR-0401 §SD8).
type ClippedInfo struct {
	Plane string  `desc:"the plane: the single plane shown, or a composite's layer in hand's"`
	Below float64 `desc:"share of the valid samples below the lower limit, 0 to 1"`
	Above float64 `desc:"share above the upper limit, 0 to 1; a value equal to a limit is not clipped"`
	Exact bool    `desc:"counted, for a kind with one histogram bin per code; otherwise read off the bins, each share off by at most the share of the bin a limit falls in"`
	Text  string  `desc:"the same as the window writes it beside the histogram"`
}

// CompositeOutcome is what set_composite and set_layer settle with.
type CompositeOutcome struct {
	Phase     string         `desc:"the phase the change ended in"`
	Composite *CompositeInfo `json:",omitzero" desc:"the composite shown now; absent when none could be"`
	Note      string         `json:",omitzero" desc:"what the change kept of the view, or had to change"`
	Error     string         `json:",omitzero" desc:"why the composite could not be shown; what was shown stayed"`
}

// compositeAsk is a composite checked and asked for: the band or plane of
// each layer, its colour, and the arguments it came from. note is what the
// window says when it is shown.
type compositeAsk struct {
	args    CompositeArgs
	bands   []int
	colours []display.LayerColorE
	note    string
}

// Hover help of the composite's controls (keelson-app A5).
const (
	tipComposite  = "Several bands — or planes — together, each its own layer: a recipe and a colour of its own, the colours added. Linked, they share one recipe; unlinked, each is stretched by itself, which changes the colour balance, and the window says so beside the image."
	tipLayerHand  = "The layer in hand: set_recipe, the Limits buttons, the histogram and its handles act on it — on every layer while the composite is linked."
	tipLayerBand  = "The band this layer shows. The bands of all layers together are taken as the planes of one image."
	tipLayerColor = "The layer's colour. The set is fixed — red, green, blue, cyan, magenta, yellow, white — so the colours' sums stay readable and a result can be described in words."
	tipLayerShow  = "Show or hide this layer. A hidden layer adds nothing, and its invalid samples do not blank a pixel."
	tipLayerDrop  = "Remove this layer."
	tipLayerAdd   = "Add a layer: the next band, in a colour no layer has."
	tipLinked     = "One recipe for every layer. Off, each layer keeps its own and a change goes to the layer in hand; two unlinked views of the same data are not comparable by colour."
	tipLimitsZ    = "Limits by zscale: a line is fitted to a sample of about a thousand of the plane's values, sorted, outliers rejected, and the limits are where it runs, steepened by a contrast of 0.25, at the ends of the sample. A few bright sources do not pull them wide."
)

// colourOf reads a layer colour's name, refusing one not of the set.
func colourOf(name string) (col display.LayerColorE, err error) {
	if col.UnmarshalText([]byte(name)) != nil {
		names := make([]string, len(display.AllLayerColors))
		for i, k := range display.AllLayerColors {
			names[i] = k.String()
		}
		return 0, app.RefuseOperation("no layer colour named " + strconv.Quote(name) + "; the colours are " + strings.Join(names, ", "))
	}
	return col, nil
}

// pixelHex writes a 0xRRGGBBAA pixel's colour as #rrggbb.
func pixelHex(px uint32) string { return fmt.Sprintf("#%06x", px>>8) }

// compositePlanes is the bands an image taken for a composite holds, in
// order: each band the layers name, once, in the order of the first layer
// that names it, and the first again when there are two — no colour model
// has two planes but grey and alpha, and alpha declares no invalid value.
func compositePlanes(bands []int) []int {
	out := make([]int, 0, 4)
	for _, b := range bands {
		if !slices.Contains(out, b) {
			out = append(out, b)
		}
	}
	if len(out) == 2 {
		out = append(out, out[0])
	}
	return out
}

// stackDeclaresInvalid reports whether a stack's planes declare an invalid
// value.
func stackDeclaresInvalid(st imaging.Stack) bool {
	p, err := st.Plane(make([]int, len(st.Axes())))
	if err != nil {
		return false
	}
	_, ok := p.Invalid()
	return ok
}

// compositeImage is the image of a selection taken for a composite: its
// bands as the planes of a grey image, an RGB one or — four bands of a
// stack that declares no invalid value — an RGBA one, labelled with the
// bands' labels. It shares the stack's planes.
func compositeImage(st imaging.Stack, sel selection) (img imaging.Image, err error) {
	if len(sel.bands) != 4 {
		return st.Image(sel.at, sel.bands)
	}
	band, _ := st.BandAxis()
	index := slices.Clone(sel.at)
	planes := make([]imaging.Plane, 4)
	labels := make([]string, 4)
	for i, b := range sel.bands {
		index[band] = b
		if planes[i], err = st.Plane(index); err != nil {
			return img, err
		}
		labels[i] = st.Label(band, b)
	}
	if img, err = imaging.NewImage(imaging.ColorModelRGBA, planes, st.Provenance()); err != nil {
		return img, err
	}
	if img, err = img.WithPlaneLabels(labels); err != nil {
		return img, err
	}
	return img.WithPlacement(st.Placement()), nil
}

// compositeStack is the stack whose bands a composite's layers name: the
// stack whose selection is shown, when it has a band axis; nil when the
// layers name planes of the image shown.
func (inst *App) compositeStack() *stackView {
	if stk := inst.stackShown(); stk != nil && stk.band >= 0 {
		return stk
	}
	return nil
}

// planeOfBand is the plane of the image of a selection that holds a band:
// the band itself for the image as stored, its first place among the bands
// otherwise; ok is false when the selection does not hold it.
func planeOfBand(stk *stackView, sel selection, band int) (plane int, ok bool) {
	if sel.bands == nil {
		return band, band >= 0 && band < stk.axes[stk.band].Size
	}
	plane = slices.Index(sel.bands, band)
	return plane, plane >= 0
}

// bandOfPlane is the band a plane of the image of a selection holds.
func bandOfPlane(stk *stackView, sel selection, plane int) int {
	if sel.bands == nil || plane < 0 || plane >= len(sel.bands) {
		return plane
	}
	return sel.bands[plane]
}

// holdsBands reports whether a selection's image holds every band.
func holdsBands(stk *stackView, sel selection, bands []int) bool {
	for _, b := range bands {
		if _, ok := planeOfBand(stk, sel, b); !ok {
			return false
		}
	}
	return true
}

// checkComposite reads a composite's arguments into an ask, refusing what
// cannot be shown with the remedy.
func (inst *App) checkComposite(in CompositeArgs) (ask compositeAsk, err error) {
	n := len(in.Layers)
	switch {
	case n == 0:
		return ask, app.RefuseOperation("a composite needs at least one layer; set_plane shows the image as stored or one plane instead")
	case n > display.MaxLayers:
		return ask, app.RefuseOperation(fmt.Sprintf("%d layers: a composite holds at most %d", n, display.MaxLayers))
	case in.InHand < 0 || in.InHand >= n:
		return ask, app.RefuseOperation(fmt.Sprintf("InHand %d is not a layer; the layers are 0 to %d", in.InHand, n-1))
	case !in.Linked && in.Recipe != nil:
		return ask, app.RefuseOperation("Recipe is the one recipe of a linked composite; an unlinked one takes a Recipe per layer, or set Linked")
	}
	stk := inst.compositeStack()
	if inst.stackShown() != nil && stk == nil {
		return ask, app.RefuseOperation("this stack has no band axis, so its images have one plane: there is nothing to composite; set_position chooses the plane shown")
	}
	ask = compositeAsk{args: in, bands: make([]int, n), colours: make([]display.LayerColorE, n)}
	ask.args.Layers = slices.Clone(in.Layers)
	for i, l := range in.Layers {
		if ask.colours[i], err = colourOf(l.Colour); err != nil {
			return ask, err
		}
		if in.Linked && l.Recipe != nil {
			return ask, app.RefuseOperation("the composite is linked: its one recipe is Recipe, and a layer's own recipe is refused; set Linked false to give each layer its own")
		}
		if stk != nil {
			a := stk.axes[stk.band]
			if l.Band < 0 || l.Band >= a.Size {
				return ask, app.RefuseOperation(fmt.Sprintf("layer %d: band %d is outside %q, whose positions are 0 to %d", i, l.Band, a.Name, a.Size-1))
			}
			ask.bands[i] = l.Band
			continue
		}
		if planes := inst.pic.Model().PlaneCount(); l.Plane < 0 || l.Plane >= planes {
			return ask, app.RefuseOperation(fmt.Sprintf("layer %d: plane %d is outside this image, whose planes are 0 to %d: %s", i, l.Plane, planes-1, strings.Join(inst.pic.PlaneLabels(), ", ")))
		}
		ask.bands[i] = l.Plane
	}
	if stk != nil {
		planes := compositePlanes(ask.bands)
		switch {
		case len(planes) > 4:
			return ask, app.RefuseOperation(fmt.Sprintf("the layers name %d different bands; an image holds at most four planes, so a composite of a stack shows at most four different bands", len(planes)))
		case len(planes) == 4 && stackDeclaresInvalid(stk.stack):
			return ask, app.RefuseOperation("the layers name four different bands, and this stack's samples declare an invalid value: the fourth plane of an image is alpha, which declares none; name at most three different bands")
		}
	}
	return ask, nil
}

// setComposite shows a composite (ADR-0401 §SD1, §SD2). It sets, never
// toggles. For a stack whose selection shown does not hold the layers'
// bands, the selection that does is asked for and the composite is shown
// when it lands; set_composite settles then.
func (inst *App) setComposite(in CompositeArgs) error {
	if inst.pic == nil {
		return errNoImage
	}
	if inst.entryPending() {
		return errEntryPending
	}
	ask, err := inst.checkComposite(in)
	if err != nil {
		return err
	}
	return inst.askComposite(ask)
}

// askComposite shows a checked composite now, or asks for the selection
// that holds its bands.
func (inst *App) askComposite(ask compositeAsk) error {
	stk := inst.compositeStack()
	if stk == nil {
		inst.compAsk = nil
		return inst.showComposite(ask, ask.bands)
	}
	if !inst.selectionPending() && holdsBands(stk, stk.shown, ask.bands) {
		inst.compAsk = nil
		planes := make([]int, len(ask.bands))
		for i, b := range ask.bands {
			planes[i], _ = planeOfBand(stk, stk.shown, b)
		}
		return inst.showComposite(ask, planes)
	}
	want := inst.want.clone()
	if !holdsBands(stk, want, ask.bands) {
		want.bands, want.composite = compositePlanes(ask.bands), true
		inst.setWant(want)
	}
	inst.compAsk = &ask
	return nil
}

// takeCompositeAsk shows the composite asked for once the selection shown
// holds its bands; it reports whether it did.
func (inst *App) takeCompositeAsk() bool {
	ask, stk := inst.compAsk, inst.compositeStack()
	if ask == nil || stk == nil || !holdsBands(stk, stk.shown, ask.bands) {
		return false
	}
	inst.compAsk = nil
	planes := make([]int, len(ask.bands))
	for i, b := range ask.bands {
		planes[i], _ = planeOfBand(stk, stk.shown, b)
	}
	if err := inst.showComposite(*ask, planes); err != nil {
		inst.selectErr = "the composite could not be shown: " + refusalText(err)
		return true
	}
	inst.selectNote = ask.note
	return true
}

// showComposite shows a checked composite on the planes of the image shown.
// A layer's recipe absent takes the range of the data of its plane — of the
// shown layers' planes together, for a linked composite — and limits found
// from the data off the frame are asked of the viewer after the layers are
// shown, under the data range meanwhile.
func (inst *App) showComposite(ask compositeAsk, planes []int) error {
	n := len(planes)
	layers := make([]imageview.CompositeLayer, n)
	finds := make([]*display.Recipe, n)
	if ask.args.Linked {
		shown := make([]int, 0, n)
		for i, l := range ask.args.Layers {
			if !l.Hidden {
				shown = append(shown, planes[i])
			}
		}
		if len(shown) == 0 {
			shown = planes
		}
		start, find, err := inst.layerRecipe(ask.args.Recipe, shown)
		if err != nil {
			return err
		}
		for i := range layers {
			layers[i] = imageview.CompositeLayer{Plane: planes[i], Layer: display.Layer{Recipe: start, Color: ask.colours[i], Hidden: ask.args.Layers[i].Hidden}}
		}
		finds[ask.args.InHand] = find
	} else {
		for i, l := range ask.args.Layers {
			start, find, err := inst.layerRecipe(l.Recipe, planes[i:i+1])
			if err != nil {
				return layerRefusal(i, err)
			}
			layers[i] = imageview.CompositeLayer{Plane: planes[i], Layer: display.Layer{Recipe: start, Color: ask.colours[i], Hidden: l.Hidden}}
			finds[i] = find
		}
	}
	if err := inst.viewer.SetComposite(layers, ask.args.Linked); err != nil {
		return refuse(err)
	}
	if err := inst.viewer.SetLayerInHand(ask.args.InHand); err != nil {
		return refuse(err)
	}
	for i, f := range finds {
		if f != nil {
			if err := inst.viewer.SetLayerRecipe(i, *f); err != nil {
				return refuse(err)
			}
		}
	}
	inst.syncChecks()
	return nil
}

// layerRefusal names the layer a refusal is about.
func layerRefusal(layer int, err error) error {
	return app.RefuseOperation("layer " + strconv.Itoa(layer) + ": " + refusalText(err))
}

// layerRecipe is the recipe a layer — or every layer of a linked composite —
// starts under, from its arguments over the planes its data limits are
// taken from, and, when its limits are found from the data off the frame,
// the recipe to find them with. Absent arguments take the range of the
// data, linear. A layer has no colour map and flags nothing.
func (inst *App) layerRecipe(in *SetRecipeArgs, planes []int) (start display.Recipe, find *display.Recipe, err error) {
	data, ok := dataLimitsOver(inst.pic, planes)
	if !ok {
		if data, ok = display.NominalLimits(inst.pic.Kind(), inst.pic.Transform(planes[0])); !ok {
			data = display.Limits{Lo: 0, Hi: 1, Source: display.LimitsSourceManual}
		}
	}
	start = display.Recipe{Limits: data}
	if in == nil {
		return start, nil, nil
	}
	r, err := inst.parseRecipe(*in, display.Recipe{}, planes, false, true)
	if err != nil {
		return start, nil, err
	}
	if r.Limits.Source == display.LimitsSourcePercentile || r.Limits.Source == display.LimitsSourceZScale {
		start.Transfer, start.Param, start.Invert = r.Transfer, r.Param, r.Invert
		return start, &r, nil
	}
	return r, nil, nil
}

// dataLimitsOver is the range of the valid values of several planes of a
// store together, through each plane's transform; ok is false when there
// is none, or one value.
func dataLimitsOver(pic *tiles.Store, planes []int) (limits display.Limits, ok bool) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, i := range planes {
		a, b, have := pic.StoredRange(i)
		if !have {
			continue
		}
		t := pic.Transform(i)
		x, y := t.Apply(a), t.Apply(b)
		lo, hi = min(lo, x, y), max(hi, x, y)
	}
	if !(hi > lo) {
		return limits, false
	}
	return display.Limits{Lo: lo, Hi: hi, Source: display.LimitsSourceData}, true
}

// compositeArgsNow is the composite shown as set_composite takes it: what
// the view resource holds and the controls change one field of.
func (inst *App) compositeArgsNow() (out CompositeArgs, ok bool) {
	layers, ok := inst.viewer.Composite()
	if !ok {
		return out, false
	}
	stk := inst.compositeStack()
	out.Linked = inst.viewer.CompositeLinked()
	out.InHand, _ = inst.viewer.LayerInHand()
	if out.Linked {
		r := recipeArgs(layers[0].Recipe)
		r.Palette = ""
		out.Recipe = &r
	}
	out.Layers = make([]LayerArgs, len(layers))
	for i, l := range layers {
		a := LayerArgs{Colour: l.Color.String(), Hidden: l.Hidden}
		if stk != nil {
			a.Band = bandOfPlane(stk, stk.shown, l.Plane)
		} else {
			a.Plane = l.Plane
		}
		if !out.Linked {
			r := recipeArgs(l.Recipe)
			r.Palette = ""
			a.Recipe = &r
		}
		out.Layers[i] = a
	}
	return out, true
}

// compositeInfo is the composite shown as get_view reports it; nil when
// none is.
func (inst *App) compositeInfo() *CompositeInfo {
	layers, ok := inst.viewer.Composite()
	if !ok {
		return nil
	}
	stk := inst.compositeStack()
	out := &CompositeInfo{Linked: inst.viewer.CompositeLinked(), Pending: inst.compAsk != nil, Layers: make([]LayerInfo, len(layers))}
	out.InHand, _ = inst.viewer.LayerInHand()
	out.Statement, _ = inst.viewer.CompositeStatement()
	labels := inst.pic.PlaneLabels()
	for i, l := range layers {
		r := recipeArgs(l.Recipe)
		r.Palette = ""
		info := LayerInfo{Index: i, Label: labels[l.Plane], Plane: l.Plane, Colour: l.Color.String(), Pixel: pixelHex(l.Color.Pixel()),
			Hidden: l.Hidden, Recipe: r}
		if stk != nil {
			info.Band = bandOfPlane(stk, stk.shown, l.Plane)
		}
		out.Layers[i] = info
	}
	return out
}

// clippedInfo is what the limits in force clip of the plane the histogram
// is of; nil until the histogram is counted, and for a colour image as
// stored.
func (inst *App) clippedInfo() *ClippedInfo {
	share, ok := inst.viewer.ClippedShares()
	if !ok || inst.pic == nil {
		return nil
	}
	plane, _ := inst.viewer.Plane()
	if layers, ok := inst.viewer.Composite(); ok {
		hand, _ := inst.viewer.LayerInHand()
		plane = layers[hand].Plane
	}
	plane = max(plane, 0)
	return &ClippedInfo{Plane: inst.pic.PlaneLabels()[plane], Below: share.Below, Above: share.Above, Exact: share.Exact,
		Text: imageview.ClippedText(share)}
}

// viewMode names the way the image is shown.
func (inst *App) viewMode() string {
	switch {
	case inst.viewer.IsComposite():
		return modeComposite
	case inst.viewer.IsSinglePlane():
		return modeSingle
	}
	return modeAsStored
}

// setLayer changes one layer of the composite shown, through
// set_composite's path: the composite whole, with that layer replaced.
func (inst *App) setLayer(in SetLayerArgs) error {
	if inst.pic == nil {
		return errNoImage
	}
	args, ok := inst.compositeArgsNow()
	if !ok {
		return app.RefuseOperation("no composite is shown; set_composite makes one")
	}
	if in.Index < 0 || in.Index >= len(args.Layers) {
		return app.RefuseOperation(fmt.Sprintf("layer %d is not one of the composite's, which are 0 to %d", in.Index, len(args.Layers)-1))
	}
	if args.Linked && in.Layer.Recipe != nil {
		return app.RefuseOperation("the composite is linked: set_recipe sets every layer's recipe; set_composite with Linked false gives each layer its own")
	}
	layer := in.Layer
	if !args.Linked && layer.Recipe == nil {
		layer.Recipe = args.Layers[in.Index].Recipe
	}
	args.Layers[in.Index] = layer
	if in.InHand {
		args.InHand = in.Index
	}
	return inst.setComposite(args)
}

// withLink is the composite shown, linked under its layer in hand's recipe
// or unlinked keeping every layer's — what the Linked check box sets.
func (inst *App) withLink(args CompositeArgs, on bool) CompositeArgs {
	if args.Linked == on {
		return args
	}
	layers, _ := inst.viewer.Composite()
	args.Layers = slices.Clone(args.Layers)
	args.Linked = on
	if on {
		r := recipeArgs(layers[args.InHand].Recipe)
		r.Palette = ""
		args.Recipe = &r
		for i := range args.Layers {
			args.Layers[i].Recipe = nil
		}
	} else {
		args.Recipe = nil
		for i := range args.Layers {
			r := recipeArgs(layers[i].Recipe)
			r.Palette = ""
			args.Layers[i].Recipe = &r
		}
	}
	return args
}

// defaultComposite is the composite the composite control opens: for a
// stack, the three bands shown as red, green and blue, the colour bands of
// the image as stored, or the band shown and the next as green and magenta;
// for an image that is no stack's, its planes but alpha. It is linked over
// the range of the data of its planes together.
func (inst *App) defaultComposite() (out CompositeArgs) {
	out.Linked = true
	var bands []int
	if stk := inst.compositeStack(); stk != nil {
		n := stk.axes[stk.band].Size
		sel := stk.shown
		switch {
		case sel.composite || len(sel.bands) == 3:
			bands = slices.Clone(sel.bands)
		case sel.bands == nil:
			for b := range min(n, 3) {
				bands = append(bands, b)
			}
		default:
			bands = []int{sel.bands[0], (sel.bands[0] + 1) % n}
		}
		bands = compositePlanes(bands)
		if len(bands) == 3 && bands[2] == bands[0] {
			bands = bands[:2]
		}
	} else {
		k := inst.pic.Model().PlaneCount()
		if inst.pic.Model().HasAlpha() {
			k--
		}
		for p := range min(k, display.MaxLayers) {
			bands = append(bands, p)
		}
	}
	colours := display.DefaultLayerColors(len(bands))
	stk := inst.compositeStack()
	for i, b := range bands {
		l := LayerArgs{Colour: colours[i].String()}
		if stk != nil {
			l.Band = b
		} else {
			l.Plane = b
		}
		out.Layers = append(out.Layers, l)
	}
	return out
}

// colourCompositeOf is the composite a colour image of a binned kind opens
// as (ADR-0401 §SD3): its red, green and blue planes, linked, over the range
// of their data together. Such an image has no colour mapping.
func colourCompositeOf(pic *tiles.Store) (ask compositeAsk, ok bool) {
	if pic == nil || !pic.Model().IsColor() {
		return ask, false
	}
	if _, _, codes := pic.Kind().Codes(); codes {
		return ask, false
	}
	ask = compositeAsk{args: CompositeArgs{Linked: true}, bands: []int{0, 1, 2}, colours: display.DefaultLayerColors(3)}
	for i, col := range ask.colours {
		ask.args.Layers = append(ask.args.Layers, LayerArgs{Plane: i, Band: i, Colour: col.String()})
	}
	return ask, true
}

// showColourComposite shows a colour image of a binned kind as the
// composite of its colour planes, which it has no other colour view than.
func (inst *App) showColourComposite() {
	if ask, ok := colourCompositeOf(inst.pic); ok {
		_ = inst.showComposite(ask, []int{0, 1, 2})
	}
}

// carryComposite shows again, on a new selection of the stack, the
// composite that was shown: the same layers, with limits found from the
// data found again from the new samples.
func (inst *App) carryComposite(args CompositeArgs) (note string) {
	found := func(r *SetRecipeArgs) bool {
		return r != nil && (r.Limits == limitsData || r.Limits == limitsPercentile || r.Limits == limitsZScale)
	}
	again := found(args.Recipe)
	for _, l := range args.Layers {
		again = again || found(l.Recipe)
	}
	if err := inst.setComposite(args); err != nil {
		return "the composite could not be kept (" + refusalText(err) + "); the view's default is shown"
	}
	if again {
		return "limits taken again from the data of the new selection, layer by layer as they were found"
	}
	return ""
}

// hasCodes reports whether a sample kind has a code per value, which the
// colour mapping takes (u8, u16, i16).
func hasCodes(kind imaging.SampleKindE) bool {
	_, _, ok := kind.Codes()
	return ok
}

// layerBands is the band (or plane) of each layer of a composite.
func layerBands(args CompositeArgs) []int {
	out := make([]int, len(args.Layers))
	for i, l := range args.Layers {
		out[i] = l.Band
	}
	return out
}

// binnedTriple shows three bands of a binned kind — signed 32-bit, float —
// which the colour mapping does not take, as the composite that shows them
// as red, green and blue: linked, over the range of their data together
// (ADR-0401 §SD3).
func (inst *App) binnedTriple(bands []int) error {
	args := CompositeArgs{Linked: true}
	for i, col := range display.DefaultLayerColors(3) {
		args.Layers = append(args.Layers, LayerArgs{Band: bands[i], Colour: col.String()})
	}
	ask, err := inst.checkComposite(args)
	if err != nil {
		return err
	}
	ask.note = "three bands of " + inst.stk.stack.Kind().String() + " samples are shown as a linked composite in red, green and blue over the range of their data: the colour mapping takes samples with a code per value"
	if err = inst.askComposite(ask); err != nil {
		return err
	}
	if inst.compAsk == nil {
		inst.selectNote = ask.note
	}
	return nil
}

// threeBandDefault gives three bands of a stack whose bands are no colour
// model, shown as red, green and blue, the range of their data together,
// linear: that selection is not an image as stored, and the nominal range
// of a 16-bit kind leaves reflectance-like data nearly black. It says so,
// or says nothing for any other view.
func (inst *App) threeBandDefault() string {
	stk := inst.stackShown()
	if stk == nil || stk.stack.Model() != 0 || stk.shown.composite || len(stk.shown.bands) != 3 || inst.viewer.IsComposite() || inst.viewer.IsSinglePlane() {
		return ""
	}
	l, ok := dataLimitsOver(inst.pic, []int{0, 1, 2})
	if !ok || inst.viewer.SetRecipe(display.Recipe{Limits: l}) != nil {
		return ""
	}
	return "three bands of a stack without a colour model open over the range of their data together, " +
		formatFigure(l.Lo) + " to " + formatFigure(l.Hi) + ", linear, one recipe for all three"
}

// describeComposite is the composite shown in one line: each shown layer's
// band and colour, and whether it is linked.
func (inst *App) describeComposite() string {
	layers, ok := inst.viewer.Composite()
	if !ok {
		return ""
	}
	labels := inst.pic.PlaneLabels()
	parts := make([]string, 0, len(layers))
	for _, l := range layers {
		if !l.Hidden {
			parts = append(parts, labels[l.Plane]+" in "+l.Color.String())
		}
	}
	link := "linked"
	if !inst.viewer.CompositeLinked() {
		link = "unlinked"
	}
	return "composite of " + strings.Join(parts, ", ") + ", " + link
}

// compositeGesture is the person's set_composite.
func (inst *App) compositeGesture(args CompositeArgs) {
	inst.viewGesture(opSetComposite, args, func() error { return inst.setComposite(args) })
}

// layerGesture is the person's set_layer.
func (inst *App) layerGesture(args SetLayerArgs) {
	inst.viewGesture(opSetLayer, args, func() error { return inst.setLayer(args) })
}

// renderCompositeRow draws the composite's controls: per layer the layer in
// hand, its band, its colour, its visibility and its removal; a layer to
// add; and the link (ADR-0401 §M2).
func (inst *App) renderCompositeRow() {
	args, ok := inst.compositeArgsNow()
	if !ok {
		return
	}
	stk := inst.compositeStack()
	for range c.Horizontal().KeepIter() {
		c.Label("Layers").Selectable(false).Send()
		for range c.IdScope(inst.ids.PrepareStr("layers")) {
			for i, l := range args.Layers {
				for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
					inst.renderLayerControls(stk, args, i, l)
				}
			}
			if len(args.Layers) < display.MaxLayers && inst.button("add", "+ layer", tipLayerAdd, false) {
				inst.compositeGesture(inst.withLayerAdded(args))
			}
		}
		// A toggle since the last frame shows as a bound value that differs
		// from the composite (the check boxes' pattern).
		if inst.linkedBox != args.Linked {
			inst.compositeGesture(inst.withLink(args, inst.linkedBox))
			inst.syncChecks()
		}
		for range c.HoverText(tipLinked).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("linked"), inst.linkedBox, "Linked").SendRespVal(&inst.linkedBox)
		}
	}
}

// renderLayerControls is one layer's controls.
func (inst *App) renderLayerControls(stk *stackView, args CompositeArgs, i int, l LayerArgs) {
	set := func(change func(l *LayerArgs), hand bool) {
		next := l
		change(&next)
		inst.layerGesture(SetLayerArgs{Index: i, Layer: next, InHand: hand})
	}
	label := strconv.Itoa(i + 1)
	if inst.button("hand", label, tipLayerHand, args.InHand == i) && args.InHand != i {
		set(func(*LayerArgs) {}, true)
	}
	if stk != nil {
		inst.bandCombo(stk, "band", l.Band, func(b int) { set(func(l *LayerArgs) { l.Band = b }, false) })
	} else {
		labels := inst.pic.PlaneLabels()
		for range c.HoverText(tipLayerBand).KeepIter() {
			for range c.ComboBox(inst.ids.PrepareStr("plane"), c.WidgetText().Text("").Keep(), c.WidgetText().Text(labels[l.Plane]).Keep()).KeepIter() {
				for p, name := range labels {
					if c.Button(inst.ids.PrepareSeq(uint64(p)), c.Atoms().Text(name).Keep()).Selected(p == l.Plane).SendResp().HasPrimaryClicked() && p != l.Plane {
						set(func(l *LayerArgs) { l.Plane = p }, false)
					}
				}
			}
		}
	}
	for range c.HoverText(tipLayerColor).KeepIter() {
		for range c.ComboBox(inst.ids.PrepareStr("colour"), c.WidgetText().Text("").Keep(), c.WidgetText().Text(l.Colour).Keep()).KeepIter() {
			for k, col := range display.AllLayerColors {
				name := col.String()
				if c.Button(inst.ids.PrepareSeq(uint64(k)), c.Atoms().Text(name).Keep()).Selected(name == l.Colour).SendResp().HasPrimaryClicked() && name != l.Colour {
					set(func(l *LayerArgs) { l.Colour = name }, false)
				}
			}
		}
	}
	if inst.layerShownBox[i] == l.Hidden {
		shown := inst.layerShownBox[i]
		set(func(l *LayerArgs) { l.Hidden = !shown }, false)
		inst.syncChecks()
	}
	for range c.HoverText(tipLayerShow).KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("shown"), inst.layerShownBox[i], "shown").SendRespVal(&inst.layerShownBox[i])
	}
	if len(args.Layers) > 1 && inst.button("drop", "×", tipLayerDrop, false) {
		next := args
		next.Layers = slices.Delete(slices.Clone(args.Layers), i, i+1)
		if next.InHand >= i && next.InHand > 0 {
			next.InHand--
		}
		inst.compositeGesture(next)
	}
}

// withLayerAdded is a composite with one more layer: the band after the
// last layer's, in the first colour no layer has, under the composite's
// recipe when it is linked and its plane's data range otherwise.
func (inst *App) withLayerAdded(args CompositeArgs) CompositeArgs {
	last := args.Layers[len(args.Layers)-1]
	next := LayerArgs{Colour: display.LayerColorWhite.String()}
	for _, col := range display.AllLayerColors {
		if !slices.ContainsFunc(args.Layers, func(l LayerArgs) bool { return l.Colour == col.String() }) {
			next.Colour = col.String()
			break
		}
	}
	if stk := inst.compositeStack(); stk != nil {
		next.Band = (last.Band + 1) % stk.axes[stk.band].Size
	} else {
		next.Plane = (last.Plane + 1) % inst.pic.Model().PlaneCount()
	}
	args.Layers = append(slices.Clone(args.Layers), next)
	return args
}

// compositeOperations registers set_composite and set_layer (ADR-0401 §M2).
func compositeOperations(s *appops.Set[*App, opsSnap]) {
	needsImage := func(sn opsSnap) (ok bool, reason string) {
		if sn.pic == nil {
			return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
		}
		return true, ""
	}
	appops.Command(s, app.OperationSpec{Name: opSetComposite, Version: 1,
		Summary: "show the image as a composite of layers — each a band of the stack, or a plane of the image, with a colour of its own and, unlinked, a recipe of its own; the colours are added; the call settles when it is shown",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true, Async: true,
		Gesture: "the composite choice of the Show or Bands row, and the Layers row: add, remove, Linked", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"a linked composite has one recipe, Recipe, and refuses a layer's own; unlinked, each layer's Recipe is taken, its absence the range of the data of its plane, and the window states beside the image that the colours are not comparable with another view's",
			"for a stack, the selection holding the layers' bands is taken first: at most four different bands, three when the stack declares an invalid value; set_position afterwards keeps the layers, colours and recipes and finds limits taken from the data again",
			"percentile and zscale limits are found off the frame; get_view's LimitsPending is true until they land"}},
		func(inst *App, _ app.OperationCall, in CompositeArgs) (out appops.None, err error) {
			err = inst.setComposite(in)
			return
		})
	appops.Settles(s, opSetComposite, settleComposite)
	s.Available(opSetComposite, needsImage)

	appops.Command(s, app.OperationSpec{Name: opSetLayer, Version: 1,
		Summary: "change one layer of the composite shown — its band or plane, colour, visibility, its own recipe while unlinked — or make it the layer in hand; the call settles when it is shown",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true, Async: true,
		Gesture: "a layer's controls in the Layers row", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the layer is given whole, as get_view lists it; a recipe is refused while the composite is linked — set_recipe sets every layer's then",
			"the layer in hand is what set_recipe, the Limits buttons and the histogram act on while the composite is unlinked"}},
		func(inst *App, _ app.OperationCall, in SetLayerArgs) (out appops.None, err error) {
			err = inst.setLayer(in)
			return
		})
	appops.Settles(s, opSetLayer, settleComposite)
	s.Available(opSetLayer, func(sn opsSnap) (ok bool, reason string) {
		switch {
		case sn.pic == nil:
			return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
		case sn.composite == nil:
			return false, "no composite is shown; set_composite makes one"
		}
		return true, ""
	})
}

// settleComposite settles set_composite and set_layer once the composite
// asked for is shown, or could not be.
func settleComposite(inst *App, _ app.OperationCall) (out CompositeOutcome, settled bool, err error) {
	if inst.compAsk != nil || inst.selectionPending() {
		return out, false, nil
	}
	return CompositeOutcome{Phase: inst.observePhase().String(), Composite: inst.compositeInfo(), Note: inst.selectNote, Error: inst.selectErr}, true, nil
}
