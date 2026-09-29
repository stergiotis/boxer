package fieldview

import (
	"errors"

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

// Input is one frame's field list. Every zero value is the documented
// default, so a list needs only Ids, ScopeKey, Fields and State.
type Input struct {
	// Ids is the host's widget id stack. Render opens its own IdScope under
	// it, so two lists in one frame need only differ in ScopeKey.
	Ids *c.WidgetIdStack
	// ScopeKey names this list within the host's id space; empty uses
	// "fieldview".
	ScopeKey string
	// Fields are drawn in slice order; container fields hold their children
	// beneath them.
	Fields []Field
	// State is the host-owned view state — which containers are open, and
	// the rebuild's scratch. Required: a list with nowhere to record what is
	// open cannot be drawn. One State belongs to one place a list is shown.
	State *State

	// HideKind drops the "[str]" / "[uint]" tag next to each leaf's name, for
	// compact contexts where the kind is obvious or unimportant.
	HideKind bool
	// Indent is the horizontal step per nesting level, in points; 0 takes 12.
	Indent float32
	// BytesMax bounds the hex dump of Bytes values: 0 takes 64, a negative
	// value disables truncation. Past the bound a value renders as
	// "<hex>… (N bytes)".
	BytesMax int
	// StartCollapsed is the state a container (Object / Array) takes until
	// the reader opens or closes it. Off, a freshly rendered tree shows
	// everything. It is a default rather than a seed — changing it still moves
	// every container the reader has not touched.
	StartCollapsed bool
	// NameWidth and ValueWidth are the two columns' starting widths in points,
	// both resizable at runtime; 0 takes 220 and 320.
	NameWidth, ValueWidth float32
	// MaxHeight caps the vertical extent the field list claims. Leave it 0 in
	// a host that already bounds it; set it in a tall or unbounded one, where
	// the underlying table otherwise auto-fits to a 400 pt cap that a long
	// field list overruns.
	MaxHeight float32
}

// Result is what one Render reports.
type Result struct {
	// Rows is how many rows the outline drew this frame.
	Rows int
	// Err is set when Input.State is nil; the widget draws the message in
	// place of the list.
	Err error
}

// Column widths, both resizable so these are starting points rather
// than rules. The name column fits a typical log field's key plus its
// kind tag; the value column takes a short scalar without truncating.
const (
	defaultNameWidth  float32 = 220
	defaultValueWidth float32 = 320
	defaultIndent     float32 = 12
	defaultBytesMax           = 64
	defaultScopeKey           = "fieldview"
)

// settings is one frame's Input with every default resolved.
type settings struct {
	showKind    bool
	indent      float32
	bytesMax    int
	defaultOpen bool
	nameWidth   float32
	valueWidth  float32
	maxHeight   float32
	// density resolves IDS spacing tokens at the active preset (ADR-0032
	// §SD2), re-read every frame because the preset is runtime-switchable.
	density styletokens.DensityE
}

func (in Input) resolve() (s settings) {
	s = settings{
		showKind:    !in.HideKind,
		indent:      in.Indent,
		bytesMax:    in.BytesMax,
		defaultOpen: !in.StartCollapsed,
		nameWidth:   in.NameWidth,
		valueWidth:  in.ValueWidth,
		maxHeight:   in.MaxHeight,
		density:     styletokens.ActiveDensity(),
	}
	if s.indent == 0 {
		s.indent = defaultIndent
	}
	switch {
	case s.bytesMax == 0:
		s.bytesMax = defaultBytesMax
	case s.bytesMax < 0:
		s.bytesMax = 0 // formatField's "no truncation"
	}
	if s.nameWidth == 0 {
		s.nameWidth = defaultNameWidth
	}
	if s.valueWidth == 0 {
		s.valueWidth = defaultValueWidth
	}
	return
}

func (in Input) scopeKey() string {
	if in.ScopeKey == "" {
		return defaultScopeKey
	}
	return in.ScopeKey
}

// Render draws the field list at the current ui scope, into the host's
// State, inside one IdScope under Input.Ids. No outer wrapper is added — the
// caller owns whatever surrounding scope (CollapsingHeader, Frame, panel)
// frames the viewer. A nil Ids draws nothing.
func Render(in Input) (res Result) {
	if in.Ids == nil {
		return
	}
	for range c.IdScope(in.Ids.PrepareStr(in.scopeKey())) {
		res = in.render()
	}
	return
}

func (in Input) render() (res Result) {
	if in.State == nil {
		res.Err = errors.New("fieldview: Input.State is nil")
		for rt := range c.RichTextLabel(res.Err.Error()) {
			rt.Small().Weak()
		}
		return
	}
	inst := in.resolve()
	state := in.State
	inst.build(state, in.Fields)
	// The default open state is a default and not a seed, so it is pushed
	// every frame: changing it still moves every container the reader has not
	// touched, which is what it promises.
	state.st.SetDefaultExpanded(inst.defaultOpen)
	tr := tree.Render(tree.Input{
		Ids:      in.Ids,
		ScopeKey: "outline",
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
	res.Rows = len(tr.Rows)
	res.Err = tr.Err
	return
}

// nameCell draws the field's name and, when ShowKind is on, its typed-slot
// tag. Both are Selectable(false): a selectable label senses click-and-drag
// and is registered after the row's own sense region, so it would sit over it
// and swallow clicks on its rect (ADR-0176 SD7).
func (inst settings) nameCell(state *State, node int32) {
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
func (inst settings) valueCell(state *State, node int32) {
	val := state.nodes[node].value
	if val == "" {
		return
	}
	atoms := c.Atoms().BeginRichText(val).Monospace().End().Keep()
	for range c.HoverText(val).KeepIter() {
		c.LabelAtoms(atoms).Selectable(false).Truncate().Send()
	}
}
