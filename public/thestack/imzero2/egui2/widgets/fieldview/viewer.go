//go:build llm_generated_opus47

package fieldview

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// detailMutedFg is the IDS NeutralTextSecondary token (ADR-0031 §SD2);
// shares the same token as the logviewer detail pane (b648b57f) and the
// errorview renderer so the three surfaces read as one visual system.
var (
	detailMutedFg   = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	transparentBgFv = color.Transparent
)

// Renderer is the configured field viewer. Construct via New, tune
// with the fluent setters (each returns a modified copy), then call
// Render any number of times. Holds a pointer to the caller's
// WidgetIdStack so widget IDs derive deterministically from the
// caller's id scope plus the per-Renderer idPrefix — two viewers on
// the same id stack don't collide as long as their prefixes differ.
//
// The Renderer is intentionally a value (not a pointer): config
// changes don't mutate the caller's instance, which makes it safe to
// build a "base" config once and customise per-call:
//
//	base := fieldview.New(ids, "card").BytesMax(64)
//	base.Render(headerFields)
//	base.ShowKind(false).Render(footerFields)   // local override
type Renderer struct {
	ids         *c.WidgetIdStack
	idPrefix    string
	showKind    bool
	indent      float32
	bytesMax    int
	defaultOpen bool
	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at construction.
	density styletokens.DensityE
}

// New constructs a Renderer with sensible defaults: ShowKind on,
// Indent 12 px, BytesMax 64, DefaultOpen for container fields. The
// idPrefix scopes every widget ID this Renderer emits so multiple
// viewers can share an ids stack without collisions; pass a stable
// short string ("card-fld" / "log-fld" / "settings").
func New(ids *c.WidgetIdStack, idPrefix string) (inst Renderer) {
	inst = Renderer{
		ids:         ids,
		idPrefix:    idPrefix,
		showKind:    true,
		indent:      12,
		bytesMax:    64,
		defaultOpen: true,
		density:     styletokens.DensityFromEnv(),
	}
	return
}

// ShowKind toggles the "[str]" / "[uint]" tag rendered next to each
// leaf field's name. Useful to disable in compact contexts where the
// kind is obvious from the value or unimportant.
func (inst Renderer) ShowKind(v bool) (out Renderer) {
	inst.showKind = v
	out = inst
	return
}

// Indent sets the per-leaf left padding before the value line, in
// pixels. Default 12. Zero is allowed for a flat (no-indent) layout.
func (inst Renderer) Indent(v float32) (out Renderer) {
	inst.indent = v
	out = inst
	return
}

// BytesMax bounds the hex dump of Bytes values. 0 disables
// truncation (full dump). Default 64 — past this, the value renders
// as "<hex>… (N bytes)".
func (inst Renderer) BytesMax(v int) (out Renderer) {
	inst.bytesMax = v
	out = inst
	return
}

// DefaultOpen sets the initial collapsed/expanded state of container
// CollapsingHeaders (Object / Array). Default true so a freshly-
// rendered tree shows everything; set false for deep trees where
// the initial summary should be terse.
func (inst Renderer) DefaultOpen(v bool) (out Renderer) {
	inst.defaultOpen = v
	out = inst
	return
}

// Render draws the field list at the current ui scope. Iteration
// order is the slice order; container fields recurse via the
// CollapsingHeader path. No outer wrapper is added — the caller
// owns whatever surrounding scope (CollapsingHeader, Frame, panel)
// frames the viewer.
func (inst Renderer) Render(fields []Field) {
	for fi, f := range fields {
		inst.renderField(fi, f, 0)
	}
}

// renderField dispatches one Field to the leaf or container path.
// depth is folded into the widget id so a deeply nested field can't
// collide with a sibling at a different level (the path "0/1/2" and
// "1/2" both produce idx 2 at their last step, but at different
// depths — keying on (depth, idx) makes the id unique).
func (inst Renderer) renderField(idx int, f Field, depth int) {
	if f.IsContainer() {
		inst.renderContainer(idx, f, depth)
		return
	}
	inst.renderLeaf(idx, f, depth)
}

// renderContainer emits a CollapsingHeader for an Object or Array
// Field; child rendering happens inside the header body. Header
// title shows "name [kind, N]" so the operator can tell containers
// apart at a glance and knows how many children sit below.
func (inst Renderer) renderContainer(idx int, f Field, depth int) {
	title := f.Name
	if inst.showKind {
		title = fmt.Sprintf("%s  [%s, %d]", f.Name, kindName(f.Kind), len(f.Children))
	}
	hdrId := inst.ids.PrepareStr(fmt.Sprintf("%s-h-%d-%d", inst.idPrefix, depth, idx))
	for range c.CollapsingHeader(hdrId, c.WidgetText().Text(title).Keep()).
		DefaultOpen(inst.defaultOpen).
		KeepIter() {
		for ci, child := range f.Children {
			inst.renderField(ci, child, depth+1)
		}
	}
}

// renderLeaf emits the two-line layout: header row "name [kind]"
// (compact, never grows wide because both atoms are short) and a
// value row indented by inst.indent with LabelAtoms.Wrap so long
// values word-wrap to the parent width instead of expanding it.
func (inst Renderer) renderLeaf(idx int, f Field, depth int) {
	for range c.Horizontal().KeepIter() {
		nameAtoms := c.Atoms().BeginRichText(f.Name).Strong().End().Keep()
		c.LabelAtoms(nameAtoms).Send()
		if inst.showKind {
			c.AddSpace(styletokens.GapInline(inst.density))
			kindAtoms := c.Atoms().BeginRichTextColored(detailMutedFg, transparentBgFv,
				"["+kindName(f.Kind)+"]").Small().End().Keep()
			c.LabelAtoms(kindAtoms).Send()
		}
	}
	for range c.Horizontal().KeepIter() {
		if inst.indent > 0 {
			c.AddSpace(inst.indent)
		}
		valAtoms := c.Atoms().BeginRichText(formatField(f, inst.bytesMax)).
			Monospace().End().Keep()
		c.LabelAtoms(valAtoms).Wrap().Send()
	}
	c.AddSpace(styletokens.PaddingHair(inst.density))
	_ = depth
}
