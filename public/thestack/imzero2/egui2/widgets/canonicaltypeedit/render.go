package canonicaltypeedit

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypesummary"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

const editorMinWidth = 460

// formToggleTip is the hover tooltip on the inline disclosure caret that
// reveals/hides the structured form, keeping the editor a single bar row when
// collapsed.
const formToggleTip = "structured editor"

type familyOpt struct {
	key, label string
	fam        familyE
}

var familyOrder = []familyOpt{
	{"str", "string", familyString},
	{"num", "numeric", familyNumeric},
	{"tmp", "temporal", familyTemporal},
	{"net", "network", familyNetwork},
}

type baseOpt struct {
	r     byte
	label string
}

var familyBases = map[familyE][]baseOpt{
	familyString: {
		{byte(canonicaltypes.BaseTypeStringUtf8), "utf8"},
		{byte(canonicaltypes.BaseTypeStringBytes), "bytes"},
		{byte(canonicaltypes.BaseTypeStringBool), "bool"},
	},
	familyNumeric: {
		{byte(canonicaltypes.BaseTypeMachineNumericUnsigned), "uint"},
		{byte(canonicaltypes.BaseTypeMachineNumericSigned), "int"},
		{byte(canonicaltypes.BaseTypeMachineNumericFloat), "float"},
	},
	familyTemporal: {
		{byte(canonicaltypes.BaseTypeTemporalUtcDatetime), "utc-datetime"},
		{byte(canonicaltypes.BaseTypeTemporalZonedDatetime), "zoned-datetime"},
		{byte(canonicaltypes.BaseTypeTemporalZonedTime), "zoned-time"},
	},
	familyNetwork: {
		{byte(canonicaltypes.BaseTypeNetworkIPv4), "ipv4"},
		{byte(canonicaltypes.BaseTypeNetworkIPv6), "ipv6"},
	},
}

type byteOrderOpt struct {
	key, label string
	mod        canonicaltypes.ByteOrderModifierE
}

var byteOrderOrder = []byteOrderOpt{
	{"def", "default", canonicaltypes.ByteOrderModifierNone},
	{"le", "LE", canonicaltypes.ByteOrderModifierLittleEndian},
	{"be", "BE", canonicaltypes.ByteOrderModifierBigEndian},
}

type scalarOpt struct {
	key, label string
	mod        canonicaltypes.ScalarModifierE
}

var scalarOrder = []scalarOpt{
	{"scalar", "scalar", canonicaltypes.ScalarModifierNone},
	{"array", "array", canonicaltypes.ScalarModifierHomogenousArray},
	{"set", "set", canonicaltypes.ScalarModifierSet},
}

// Render draws the editor (formula bar + structured form + live status chip)
// and applies the ADR-0067 §SD2 bidirectional sync. Call once per frame;
// scopeKey scopes every widget id (pass a stable short string per call site,
// e.g. "ctedit"). All edits mutate the receiver in place.
func (m *Model) Render(ids *c.WidgetIdStack, scopeKey string) {
	for range c.IdScope(ids.PrepareStr(scopeKey)) {
		for range c.Vertical().KeepIter() {
			c.UiSetMinWidth(editorMinWidth)
			m.renderEditBody(ids)
			c.Separator().Send()
			m.renderStatus(ids)
		}
	}
}

// renderEditBody draws the formula bar (with its inline form-disclosure caret)
// and, when expanded, the structured form below it, applying the ADR-0067 §SD2
// edge-ownership sync and mutating the draft in place. It assumes the caller has
// already opened an IdScope and a layout container. Returns whether the type
// changed this frame — the signature editor uses this to know when to reassemble.
func (m *Model) renderEditBody(ids *c.WidgetIdStack) (changed bool) {
	barChanged := m.renderBar(ids)

	// The grammar-mirroring form is gated behind the bar's inline disclosure
	// caret (drawn by renderBar), so a collapsed editor stays a single bar row:
	// the form is simply not rendered until m.formOpen. That also means a hidden
	// form emits no widgets and cannot report a stray edit, so the edge-ownership
	// rule below reads formChanged=false whenever it is collapsed.
	var formChanged bool
	if m.formOpen {
		c.AddSpace(styletokens.PaddingInner(styletokens.DensityFromEnv()))
		formChanged = m.renderForm(ids)
	}

	// Edge ownership: at most one side was edited this frame.
	switch {
	case barChanged:
		if n, err := parsePrimitive(m.barBuf); err != nil {
			// Keep the draft + buffer so a mid-typing intermediate survives;
			// just surface the headline.
			m.barErr = firstLine(err.Error())
		} else {
			m.barErr = ""
			m.nodeToDraft(n)
			m.rebuildFromDraft()
		}
		changed = true
	case formChanged:
		m.barErr = ""
		m.rebuildFromDraft()
		m.barBuf = m.canonical
		changed = true
	}
	return
}

// renderBar draws the free-text formula bar plus the trailing inline disclosure
// caret (renderFormToggle) on one row, and reports whether the bar text changed
// this frame (the caret toggles form visibility, not the bar, so it is excluded).
func (m *Model) renderBar(ids *c.WidgetIdStack) (changed bool) {
	for range c.Horizontal().KeepIter() {
		rowLabel("type")
		changed = c.TextEdit(ids.PrepareStr("bar"), m.barBuf, false).
			HintText("canonical, e.g. u32l").
			DesiredWidth(240).
			SendRespVal(&m.barBuf).
			HasChanged()
		m.renderFormToggle(ids)
	}
	if m.barErr != "" {
		for rt := range c.RichTextLabelColored(
			color.Hex(styletokens.ErrorDefault.AsHex()).Keep(),
			color.Transparent.Keep(),
			"parse error: "+m.barErr) {
			rt.Small()
		}
	}
	return
}

// renderFormToggle draws the small inline disclosure caret that shows/hides the
// structured form. It sits on the bar row, to the right of the formula bar, so a
// collapsed editor is a single line. The caret points right when collapsed and
// down when open (the usual tree-disclosure convention); the hover tooltip names
// what it reveals.
func (m *Model) renderFormToggle(ids *c.WidgetIdStack) {
	caret := icons.PhCaretRight
	if m.formOpen {
		caret = icons.PhCaretDown
	}
	for range c.HoverText(formToggleTip).KeepIter() {
		if c.Button(ids.PrepareStr("form-toggle"), c.Atoms().Text(caret).Keep()).
			Small().SendResp().HasPrimaryClicked() {
			m.formOpen = !m.formOpen
		}
	}
}

// renderForm draws the structured controls. Each control sits on the grammar
// production for the current family, so only applicable modifiers are shown.
// Returns whether any control changed this frame.
func (m *Model) renderForm(ids *c.WidgetIdStack) (changed bool) {
	fam := familyOf(m.base)

	// Family selector.
	for range c.Horizontal().KeepIter() {
		rowLabel("family")
		for _, f := range familyOrder {
			if c.SelectableLabel(ids.PrepareStr("fam-"+f.key), fam == f.fam, f.label).
				SendResp().HasPrimaryClicked() {
				if fam != f.fam {
					m.base = familyDefaultBase(f.fam)
					if (f.fam == familyNumeric || f.fam == familyTemporal) && m.width == 0 {
						m.width = defaultWidth(f.fam)
					}
					changed = true
				}
			}
		}
	}
	fam = familyOf(m.base)

	// Base selector for the current family.
	for range c.Horizontal().KeepIter() {
		rowLabel("base")
		sel := selector.Segmented(ids, "base", &m.base).Style(selector.StyleSelectable).Inline()
		for _, b := range familyBases[fam] {
			sel = sel.Option(b.r, b.label)
		}
		if sel.SendResp() {
			changed = true
		}
	}

	isBool := canonicaltypes.BaseTypeStringE(m.base) == canonicaltypes.BaseTypeStringBool

	// String fixed-width toggle (bool carries no width).
	if fam == familyString && !isBool {
		for range c.Horizontal().KeepIter() {
			rowLabel("")
			if c.Checkbox(ids.PrepareStr("fixedw"), m.fixedWidth, "fixed width").
				SendRespVal(&m.fixedWidth).HasChanged() {
				if m.fixedWidth && m.width == 0 {
					m.width = 32
				}
				changed = true
			}
		}
	}

	// Width drag (numeric, temporal, or fixed-width string).
	showWidth := fam == familyNumeric || fam == familyTemporal ||
		(fam == familyString && m.fixedWidth && !isBool)
	if showWidth {
		for range c.Horizontal().KeepIter() {
			rowLabel("width")
			w := uint64(m.width)
			c.DragValueU64(ids.PrepareStr("width"), w).
				Speed(1).
				Suffix(" bits").
				SendRespVal(&w)
			if nw := clampWidth(w); nw != m.width {
				m.width = nw
				changed = true
			}
		}
	}

	// Byte order (numeric only).
	if fam == familyNumeric {
		for range c.Horizontal().KeepIter() {
			rowLabel("byte order")
			sel := selector.Segmented(ids, "byte-order", &m.byteOrder).Style(selector.StyleSelectable).Inline()
			for _, bo := range byteOrderOrder {
				sel = sel.Option(bo.mod, bo.label)
			}
			if sel.SendResp() {
				changed = true
			}
		}
	}

	// CIDR (network only).
	if fam == familyNetwork {
		for range c.Horizontal().KeepIter() {
			rowLabel("")
			if c.Checkbox(ids.PrepareStr("cidr"), m.cidr, "CIDR (per-value prefix)").
				SendRespVal(&m.cidr).HasChanged() {
				changed = true
			}
		}
	}

	// Scalar shape (all families).
	for range c.Horizontal().KeepIter() {
		rowLabel("shape")
		sel := selector.Segmented(ids, "shape", &m.scalarMod).Style(selector.StyleSelectable).Inline()
		for _, sh := range scalarOrder {
			sel = sel.Option(sh.mod, sh.label)
		}
		if sel.SendResp() {
			changed = true
		}
	}
	return
}

// renderStatus shows the live result: the embedded canonicaltypesummary
// level-1 chip over the current canonical string. Its validity dot and
// footprint trailer are the editor's status line, and its anchor toggle pops
// the full tethered inspector (ADR-0067 §SD4).
func (m *Model) renderStatus(ids *c.WidgetIdStack) {
	smallLabel("live type")
	canonicaltypesummary.New("ctedit-sum").Render(ids.PrepareSeq(0xC7ED17), m.canonical)
}

// rowLabelWidth pins the editor's left label column so the controls that follow
// align vertically across the whole form (a tabular layout).
const rowLabelWidth = 92

// rowLabel emits a small label in the fixed-width left column, so the controls
// after it sit at the same x on every row.
func rowLabel(text string) {
	for range c.Vertical().KeepIter() {
		c.UiSetMinWidth(rowLabelWidth)
		c.UiSetMaxWidth(rowLabelWidth)
		smallLabel(text)
	}
}

// smallLabel emits a small de-emphasised inline label.
func smallLabel(text string) {
	for rt := range c.RichTextLabel(text) {
		rt.Small()
	}
}
