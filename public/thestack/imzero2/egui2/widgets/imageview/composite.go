package imageview

import (
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// CompositeLayer is one layer of a composite view (ADR-0401 §SD1): a plane
// of the source shown, by its index in the model's plane order, and how it
// is drawn — its recipe, its colour and whether it is shown. The layers of a
// composite are planes of one source (§SD6), and several layers may show one
// plane.
type CompositeLayer struct {
	Plane int
	display.Layer
}

// CompositeUnlinkedText is the statement a viewer draws beside a composite
// whose layers are stretched separately (ADR-0401 §SD2), and what
// [Viewer.CompositeStatement] reports.
const CompositeUnlinkedText = "unlinked: each layer has its own stretch, so these colours are not comparable with another view's"

// compositeView is a composite as the viewer shows it: its layers, whether
// one recipe is kept for all of them, and the layer in hand — the one the
// histogram strip, its handles and [Viewer.SetRecipe] act on.
type compositeView struct {
	layers []CompositeLayer
	linked bool
	inHand int
}

// legendHeight is the row under a composite that holds its legend and the
// statement of an unlinked one.
const (
	legendHeight = 24
	swatchSize   = 12
)

// IsComposite reports whether a composite of layers is shown.
func (inst *Viewer) IsComposite() bool { return inst.plane == asComposite }

// Composite is the composite shown, its layers in order; ok is false when
// none is.
func (inst *Viewer) Composite() (layers []CompositeLayer, ok bool) {
	if inst.plane != asComposite {
		return nil, false
	}
	return slices.Clone(inst.comp.layers), true
}

// CompositeLinked reports whether the composite shown is linked: one recipe
// for every layer, so that a recipe set on one is set on all (ADR-0401
// §SD2). It is false when no composite is shown.
func (inst *Viewer) CompositeLinked() bool { return inst.plane == asComposite && inst.comp.linked }

// CompositeStatement is the statement drawn beside the composite shown when
// it is unlinked and shows more than one layer — [CompositeUnlinkedText] —
// and ok false otherwise: a person reading colours off the view needs to
// know that the layers' stretches differ (ADR-0401 §SD2).
func (inst *Viewer) CompositeStatement() (text string, ok bool) {
	if inst.plane != asComposite || inst.comp.linked {
		return "", false
	}
	shown := 0
	for _, l := range inst.comp.layers {
		if !l.Hidden {
			shown++
		}
	}
	if shown < 2 {
		return "", false
	}
	return CompositeUnlinkedText, true
}

// LayerInHand is the composite's layer the histogram strip, its handles and
// [Viewer.SetRecipe] act on; ok is false when no composite is shown.
func (inst *Viewer) LayerInHand() (index int, ok bool) {
	if inst.plane != asComposite {
		return 0, false
	}
	return inst.comp.inHand, true
}

// SetComposite shows the source as a composite of layers (ADR-0401): each a
// plane of it by index, a recipe, a colour from the fixed set, and whether
// it is shown; the colours of the shown layers are added. Linked, every
// layer must have one recipe, and a recipe set later is set on all of them;
// unlinked, the view states beside the image that the layers are stretched
// separately. The recipes are taken as given — limits found from the data
// are found by [Viewer.SetRecipe] and [Viewer.SetLayerRecipe] — and a
// composite that cannot be compiled is refused, the view staying as it was.
// The layer in hand is kept where it can be, and is the first otherwise.
// Every layer is mapped from the samples the source's tiles already carry,
// so a change of layers asks the source nothing that is cached (ADR-0374
// §SD3). A composite has no alpha (§SD5).
func (inst *Viewer) SetComposite(layers []CompositeLayer, linked bool) error {
	if inst.info.IsZero() {
		return eb.Build().Errorf("no image to show a composite of")
	}
	if linked && !sameRecipe(layers) {
		return eb.Build().Errorf("a linked composite has one recipe for every layer; give each the same, or unlink it")
	}
	if inst.plane == asComposite && inst.comp.linked == linked && slices.Equal(inst.comp.layers, layers) {
		return nil
	}
	inHand := 0
	if inst.plane == asComposite && inst.comp.inHand < len(layers) {
		inHand = inst.comp.inHand
	}
	if err := inst.publishComposite(layers, linked, inHand); err != nil {
		return err
	}
	inst.pending, inst.drag = nil, limitDrag{}
	inst.m.InvalidateTiles()
	return nil
}

// SetLayer replaces one layer of the composite shown: its plane, colour,
// visibility or recipe. In a linked composite its recipe must be the one
// the others have; [Viewer.SetLayerRecipe] sets a recipe instead.
func (inst *Viewer) SetLayer(index int, layer CompositeLayer) error {
	if err := inst.checkLayerIndex(index); err != nil {
		return err
	}
	old := inst.comp.layers[index]
	if old == layer {
		return nil
	}
	layers := slices.Clone(inst.comp.layers)
	layers[index] = layer
	if inst.comp.linked && !sameRecipe(layers) {
		return eb.Build().Int("layer", index).Errorf("the composite is linked: a layer's recipe is every layer's; set it as the composite's recipe, or unlink the composite")
	}
	if err := inst.publishComposite(layers, inst.comp.linked, inst.comp.inHand); err != nil {
		return err
	}
	if old.Plane != layer.Plane || old.Recipe != layer.Recipe {
		inst.dropPending(index)
	}
	inst.m.InvalidateTiles()
	return nil
}

// SetLayerRecipe sets one layer's recipe, as [Viewer.SetRecipe] sets the
// single plane's: limits from percentiles or zscale are found from that
// layer's plane, off the frame where they must be. In a linked composite the
// recipe is set on every layer, its limits found from this layer's plane.
func (inst *Viewer) SetLayerRecipe(index int, recipe display.Recipe) error {
	if err := inst.checkLayerIndex(index); err != nil {
		return err
	}
	target := index
	if inst.comp.linked {
		target = allLayers
	}
	return inst.setRecipeFor(target, inst.comp.layers[index].Plane, recipe)
}

// SetCompositeLinked links or unlinks the composite shown (ADR-0401 §SD2).
// Linking gives every layer the recipe of the layer in hand; unlinking
// changes no layer, and recipes set afterwards go to the layer in hand
// alone. Limits pending for the layer in hand stay pending, now for all
// layers or for it alone; any others are dropped.
func (inst *Viewer) SetCompositeLinked(on bool) error {
	if inst.plane != asComposite {
		return eb.Build().Errorf("no composite is shown")
	}
	if on == inst.comp.linked {
		return nil
	}
	hand := inst.comp.inHand
	keep := inst.pending[:0]
	for _, w := range inst.pending {
		switch {
		case on && w.target == hand:
			w.target = allLayers
			keep = append(keep, w)
		case !on && w.target == allLayers:
			w.target = hand
			keep = append(keep, w)
		}
	}
	inst.pending = keep
	if !on {
		inst.comp.linked = false
		return nil
	}
	layers := slices.Clone(inst.comp.layers)
	for i := range layers {
		layers[i].Recipe = layers[hand].Recipe
	}
	if err := inst.publishComposite(layers, true, hand); err != nil {
		return err
	}
	inst.m.InvalidateTiles()
	return nil
}

// SetLayerInHand makes a layer the one the histogram strip, its handles and
// [Viewer.SetRecipe] act on. Nothing is mapped again.
func (inst *Viewer) SetLayerInHand(index int) error {
	if err := inst.checkLayerIndex(index); err != nil {
		return err
	}
	if index != inst.comp.inHand {
		inst.comp.inHand, inst.drag = index, limitDrag{}
		inst.recipe = inst.comp.layers[index].Recipe
	}
	return nil
}

// checkLayerIndex refuses a layer the composite shown does not have.
func (inst *Viewer) checkLayerIndex(index int) error {
	if inst.plane != asComposite {
		return eb.Build().Errorf("no composite is shown")
	}
	if index < 0 || index >= len(inst.comp.layers) {
		return eb.Build().Int("layer", index).Int("layers", len(inst.comp.layers)).Errorf("the composite has no such layer")
	}
	return nil
}

// sameRecipe reports whether every layer, shown or hidden, has one recipe.
func sameRecipe(layers []CompositeLayer) bool {
	for _, l := range layers {
		if l.Recipe != layers[0].Recipe {
			return false
		}
	}
	return true
}

// publishComposite compiles a composite for the source shown and, when that
// works, makes it what the workers map with. The viewer's own fields change
// only on success.
func (inst *Viewer) publishComposite(layers []CompositeLayer, linked bool, inHand int) error {
	n := inst.info.Model.PlaneCount()
	dl := make([]display.Layer, len(layers))
	specs := make([]display.PlaneSpec, len(layers))
	planes := make([]int, len(layers))
	for i, l := range layers {
		if l.Plane < 0 || l.Plane >= n {
			return eb.Build().Int("layer", i).Int("plane", l.Plane).Int("planes", n).Errorf("a layer names a plane the image does not have")
		}
		dl[i], specs[i], planes[i] = l.Layer, inst.info.spec(l.Plane), l.Plane
	}
	mapper, err := display.CompileComposite(dl, specs)
	if err != nil {
		return err
	}
	inHand = min(max(inHand, 0), len(layers)-1)
	sh := inst.newShown(asComposite)
	sh.composite, sh.layerPlanes = mapper, planes
	gray, _ := paletteFor("")
	inst.plane, inst.recipe, inst.palette = asComposite, layers[inHand].Recipe, gray
	inst.comp = compositeView{layers: slices.Clone(layers), linked: linked, inHand: inHand}
	inst.src.cur.Store(sh)
	return nil
}

// legendRows is the height the rows under a composite take: the legend, and
// the statement of an unlinked one on a row of its own above it.
func (inst *Viewer) legendRows() float32 {
	if _, ok := inst.CompositeStatement(); ok {
		return 2*legendHeight + layoutGap
	}
	return legendHeight
}

// renderLegend draws the rows under a composite: the statement of an
// unlinked one, then each shown layer's colour, its plane's label and its
// limits, the layer in hand's swatch outlined in the accent.
func (inst *Viewer) renderLegend() {
	if text, ok := inst.CompositeStatement(); ok {
		badge.New(inst.ids.PrepareStr("unlinked"), text).Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
	}
	for range c.Horizontal().KeepIter() {
		for range c.IdScope(inst.ids.PrepareStr("legend")) {
			for i, l := range inst.comp.layers {
				if l.Hidden {
					continue
				}
				outline, width := color.Hex(styletokens.NeutralBorderDefault.AsHex()), styletokens.StrokeHair
				if i == inst.comp.inHand {
					outline, width = color.Hex(styletokens.AccentDefault.AsHex()), styletokens.StrokeStrong
				}
				c.PaintRectFilled(1, 1, swatchSize-1, swatchSize-1, 2, color.Hex(l.Color.Pixel())).Send()
				c.PaintRectStroke(1, 1, swatchSize-1, swatchSize-1, 2, outline, width).Send()
				c.PaintCanvas(inst.ids.PrepareSeq(uint64(i)), swatchSize, swatchSize).Send()
				c.Label(inst.legendText(l)).Selectable(false).Send()
			}
		}
	}
}

// legendText is a layer as the legend names it: its plane's label and its
// limits, and its transfer function when it is not linear.
func (inst *Viewer) legendText(l CompositeLayer) string {
	var b strings.Builder
	b.WriteString(inst.info.PlaneLabel(l.Plane))
	b.WriteByte(' ')
	b.WriteString(formatValue(l.Recipe.Limits.Lo))
	b.WriteString("–")
	b.WriteString(formatValue(l.Recipe.Limits.Hi))
	if l.Recipe.Transfer != display.TransferLinear {
		b.WriteByte(' ')
		b.WriteString(l.Recipe.Transfer.String())
	}
	if l.Recipe.Invert {
		b.WriteString(" inverted")
	}
	return b.String()
}
