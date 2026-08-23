package fieldview

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
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
//	base.Render(&headerState, headerFields)
//	base.ShowKind(false).Render(&footerState, footerFields)
//
// View state is not config and does not travel in the copies: each
// call takes the [State] belonging to the place it draws.
type Renderer struct {
	ids         *c.WidgetIdStack
	idPrefix    string
	showKind    bool
	indent      float32
	bytesMax    int
	defaultOpen bool
	nameWidth   float32
	valueWidth  float32
	maxHeight   float32
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
		nameWidth:   defaultNameWidth,
		valueWidth:  defaultValueWidth,
		density:     styletokens.ActiveDensity(),
	}
	return
}

// Column widths, both resizable so these are starting points rather
// than rules. The name column fits a typical log field's key plus its
// kind tag; the value column takes a short scalar without truncating.
const (
	defaultNameWidth  float32 = 220
	defaultValueWidth float32 = 320
)

// NameWidth and ValueWidth set the two columns' starting widths in
// points. Both are resizable at runtime, so these matter mainly for
// the first frame and for a host that knows its own proportions.
func (inst Renderer) NameWidth(v float32) (out Renderer) {
	inst.nameWidth = v
	out = inst
	return
}

func (inst Renderer) ValueWidth(v float32) (out Renderer) {
	inst.valueWidth = v
	out = inst
	return
}

// MaxHeight caps the vertical extent the field list claims. Leave it
// 0 in a host that already bounds it; set it in a tall or unbounded
// one, where the underlying table otherwise auto-fits to a 400 pt cap
// that a long field list overruns.
func (inst Renderer) MaxHeight(v float32) (out Renderer) {
	inst.maxHeight = v
	out = inst
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

// Indent sets the horizontal step per nesting level, in points.
// Default 12. Zero is allowed and takes the outline's own default,
// since a tree with no indent has no visible hierarchy.
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

// DefaultOpen sets the collapsed/expanded state a container (Object
// / Array) takes until the reader opens or closes it. Default true so
// a freshly-rendered tree shows everything; set false for deep trees
// where the initial summary should be terse. It is a default rather
// than a seed — changing it still moves every container the reader
// has not touched.
func (inst Renderer) DefaultOpen(v bool) (out Renderer) {
	inst.defaultOpen = v
	out = inst
	return
}

// Render draws the field list at the current ui scope, into the
// caller-owned state. Iteration order is the slice order; container
// fields hold their children beneath them. No outer wrapper is added
// — the caller owns whatever surrounding scope (CollapsingHeader,
// Frame, panel) frames the viewer.
//
// A nil state renders a fully collapsed list, which is only useful
// for a one-shot draw nobody interacts with; pass a retained *State
// for anything the reader can open.
func (inst Renderer) Render(state *State, fields []Field) {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	if state == nil {
		state = &State{}
	}
	inst.build(state, fields)
	// DefaultOpen is a default and not a seed, so it is pushed every frame
	// rather than at construction: changing it still moves every container the
	// reader has not touched, which is what it promises.
	state.st.SetDefaultExpanded(inst.defaultOpen)
	tree.Render(tree.Input{
		Ids:      inst.ids,
		ScopeKey: inst.idPrefix,
		Tree:     state.tree(),
		State:    &state.st,
		Indent:   inst.indent,
		Outline: tree.Column{
			Width:     inst.nameWidth,
			Resizable: true,
			Cell:      func(r tree.Row) { inst.nameCell(state, r.Node) },
		},
		Columns: []tree.Column{{
			Width: inst.valueWidth,
			Cell:  func(r tree.Row) { inst.valueCell(state, r.Node) },
		}},
		MaxHeight: inst.maxHeight,
	})
}

// nameCell draws the field's name and, when ShowKind is on, its typed-slot
// tag. Both are Selectable(false): a selectable label senses click-and-drag
// and is registered after the row's own sense region, so it would sit over it
// and swallow clicks on its rect (ADR-0176 SD7).
func (inst Renderer) nameCell(state *State, node int32) {
	c.LabelAtoms(c.Atoms().BeginRichText(state.labels[node]).Strong().End().Keep()).
		Selectable(false).Truncate().Send()
	kind := state.nodes[node].kind
	if kind == "" {
		return
	}
	c.AddSpace(styletokens.GapInline(inst.density))
	c.LabelAtoms(c.Atoms().
		BeginRichTextColored(detailMutedFg, transparentBgFv, "["+kind+"]").Small().End().
		Keep()).Selectable(false).Truncate().Send()
}

// valueCell draws the formatted value, monospace so digits and hex line up
// down the column. It truncates rather than wrapping — the row is one line
// high — and carries the full text as a tooltip, which is where a long JSON
// string or a hex dump is now read.
func (inst Renderer) valueCell(state *State, node int32) {
	val := state.nodes[node].value
	if val == "" {
		return
	}
	atoms := c.Atoms().BeginRichText(val).Monospace().End().Keep()
	for range c.HoverText(val).KeepIter() {
		c.LabelAtoms(atoms).Selectable(false).Truncate().Send()
	}
}
