package mappingplanview

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Input is the per-frame render state for the mappingplan playground.
type Input struct {
	// Ids is the widget ID stack. Render opens its own IdScope via
	// Ids.PrepareStr(ScopeKey) and derives a stable per-row scope from each
	// row's uid.
	Ids *c.WidgetIdStack
	// ScopeKey scopes every id Render emits. Pass a stable short string per
	// call site (e.g. "mpv").
	ScopeKey string
	// Model is the editable state, mutated in place by the editor controls.
	Model *Model
	// Recompute rebuilds the preview from the Model. The host supplies it (it
	// owns the mappingplan / marshallgen wiring); Render calls it at most once
	// per frame, only when the Model is dirty, and the implementation reports
	// back through Model.SetValid / Model.SetInvalid. May be nil (no preview).
	Recompute func(*Model)
}

const (
	editorPanelWidth = 540 // left side-panel default width (resizable)
	dockMinHeight    = 360 // floor for the output dock so it has a bounded rect in a scrolling host
	previewRows      = 30  // error-text TextEdit height, in rows
	rowBarWidth      = 4
)

// channelChoices is the v1 channel picker set — the four Cut-1 channels.
// Carrier channels (mixed* / *parametrized) need a paired carrier field the
// editor does not model yet (see doc.go).
var channelChoices = []mappingplan.MembershipChannel{
	mappingplan.MembershipChannelLowCardRef,
	mappingplan.MembershipChannelLowCardVerbatim,
	mappingplan.MembershipChannelHighCardRef,
	mappingplan.MembershipChannelHighCardVerbatim,
}

// channelLabel is the picker label for a channel — its lw: flag spelling, with
// the empty default spelled out.
func channelLabel(ch mappingplan.MembershipChannel) string {
	if s := ch.String(); s != "" {
		return s
	}
	return "lowCardRef"
}

// Render draws the playground and runs the host's Recompute when the Model has
// changed since the last frame. Stateless apart from the Model it is handed;
// call once per frame.
func Render(in Input) {
	m := in.Model
	for range c.IdScope(in.Ids.PrepareStr(in.ScopeKey)) {
		// Left: the editor in a resizable side panel (its controls are
		// fixed-width). PanelLeftInside + PanelCentralInside fill the host area
		// responsively — fixed-width columns stranded the output at a fixed
		// width in the wider interactive gallery window and left the dock
		// without a bounded height.
		for range c.PanelLeftInside(in.Ids.PrepareStr("editorpanel")).DefaultSize(editorPanelWidth).Resizable(true).KeepIter() {
			renderEditor(in.Ids, m)
		}

		// Recompute between the panels — pure Go, emits no UI — so the output
		// reflects this frame's edits rather than last frame's.
		if m.dirty && in.Recompute != nil {
			in.Recompute(m)
			m.dirty = false
		}

		// Central: the output dock fills the remaining width and height.
		for range c.PanelCentralInside().KeepIter() {
			renderOutput(in.Ids, m)
		}
	}
}

func renderEditor(ids *c.WidgetIdStack, m *Model) {
	for rt := range c.RichTextLabel("plan") {
		rt.Strong()
	}
	for range c.Horizontal().KeepIter() {
		if editField(ids, "kind", "kind", &m.Kind, 150, true) {
			m.dirty = true
		}
		if editField(ids, "pkg", "package", &m.PackageName, 130, true) {
			m.dirty = true
		}
		if editField(ids, "type", "Go type", &m.KindType, 150, true) {
			m.dirty = true
		}
	}
	c.Separator().Send()

	// Field rows, stacked directly in the column. (A bounded scroll area for
	// long field lists is a v2 refinement.)
	var removeUID uint64
	hasRemove := false
	for _, r := range m.Fields {
		for range c.IdScope(ids.PrepareSeq(r.uid)) {
			if renderRow(ids, m, r) {
				removeUID = r.uid
				hasRemove = true
			}
		}
		c.AddSpace(4)
	}
	if hasRemove {
		m.removeByUID(removeUID)
	}

	if c.Button(ids.PrepareStr("add-field"), c.Atoms().Text("+ field").Keep()).SendResp().HasPrimaryClicked() {
		m.AddRow()
	}
}

// renderRow draws one field row — a category colour-bar + a framed body whose
// header names the field and shows its assembled lw: tag — and returns true if
// its remove button fired.
func renderRow(ids *c.WidgetIdStack, m *Model, r *FieldRow) (remove bool) {
	// A const is a fixed scalar string declared on a `_` field: no Go field,
	// no element shape, no explode. Normalise those off so the row stays valid
	// and the shape toggles below render disabled.
	if r.IsConst && (r.IsOption || r.IsSlice || r.IsRoaring || r.Explode) {
		r.IsOption, r.IsSlice, r.IsRoaring, r.Explode = false, false, false, false
		m.dirty = true
	}

	word, catCol := rowCategory(r)
	tagged := r.Membership != ""

	for range c.Horizontal().KeepIter() {
		// Category colour bar — plain / value / const at a glance.
		for range c.Frame(ids.PrepareStr("bar")).Fill(color.Hex(catCol.AsHex())).CornerRadius(3).KeepIter() {
			c.AddSpace(rowBarWidth)
		}
		c.AddSpace(6)

		// Framed body. The Vertical pins line stacking (a Frame inherits the
		// surrounding Horizontal otherwise).
		for range c.Frame(ids.PrepareStr("body")).
			Fill(color.Hex(styletokens.NeutralBgSurface.AsHex())).
			InnerMargin(8).
			CornerRadius(4).
			KeepIter() {
			for range c.Vertical().KeepIter() {
				renderRowHeader(r, word, catCol)

				// Go field + type (both meaningless for a const), and remove.
				for range c.Horizontal().KeepIter() {
					if editField(ids, "gofield", "Go field", &r.GoField, 120, !r.IsConst) {
						m.dirty = true
					}
					if editField(ids, "gotype", "type", &r.GoType, 110, !r.IsConst) {
						m.dirty = true
					}
					if c.Button(ids.PrepareStr("rm"), c.Atoms().Text("remove").Keep()).SendResp().HasPrimaryClicked() {
						remove = true
					}
				}

				// Binding: membership, section, sub-column. A sub-column only
				// applies to a tagged value field (not plain, not const).
				for range c.Horizontal().KeepIter() {
					if editField(ids, "memb", "membership", &r.Membership, 120, true) {
						m.dirty = true
					}
					if editField(ids, "sec", "section", &r.Section, 110, true) {
						m.dirty = true
					}
					if editField(ids, "col", "sub-col", &r.Column, 90, tagged && !r.IsConst) {
						m.dirty = true
					}
				}

				if tagged {
					renderRowFlags(ids, m, r)
				}
			}
		}
	}
	return
}

// renderRowHeader draws the category word (coloured), the field name, and the
// live assembled lw: tag.
func renderRowHeader(r *FieldRow, word string, catCol styletokens.RGBA8) {
	name := r.GoField
	if name == "" {
		name = r.Section
	}
	if name == "" {
		name = "field"
	}
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabelColored(color.Hex(catCol.AsHex()).Keep(), color.Transparent.Keep(), word) {
			rt.Small()
		}
		c.AddSpace(6)
		for rt := range c.RichTextLabel(name) {
			rt.Strong()
		}
		c.AddSpace(8)
		for rt := range c.RichTextLabel(`lw:"` + r.LWTag() + `"`) {
			rt.Small()
		}
	}
}

// renderRowFlags draws the channel picker, the unit/explode/const flags, and
// the element-shape toggles, disabling every control whose toggle would compose
// an invalid field. A control stays interactive while it is on, so a state that
// became invalid (e.g. by changing the shape) can always be backed out.
func renderRowFlags(ids *c.WidgetIdStack, m *Model, r *FieldRow) {
	isMulti := r.IsSlice || r.IsRoaring
	// Shape is mutually exclusive (scalar / Option[T] / []T / roaring) and a
	// const carries none of it.
	optEnabled := !r.IsConst && (r.IsOption || (!r.IsSlice && !r.IsRoaring))
	sliceEnabled := !r.IsConst && (r.IsSlice || (!r.IsOption && !r.IsRoaring))
	roarEnabled := !r.IsConst && (r.IsRoaring || (!r.IsOption && !r.IsSlice))
	explodeEnabled := !r.IsConst && (r.Explode || isMulti) // explode requires a multi shape
	unitEnabled := r.Unit || !(isMulti && !r.Explode)      // unit on a multi shape requires explode

	for range c.Horizontal().KeepIter() {
		renderChannelCombo(ids, m, r)
		if toggle(ids, "unit", "unit", &r.Unit, unitEnabled) {
			m.dirty = true
		}
		if toggle(ids, "explode", "explode", &r.Explode, explodeEnabled) {
			m.dirty = true
		}
		if toggle(ids, "const", "const", &r.IsConst, true) {
			m.dirty = true
		}
		if r.IsConst {
			if editField(ids, "constval", "const value", &r.ConstValue, 120, true) {
				m.dirty = true
			}
		}
	}

	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel("shape") {
			rt.Small()
		}
		c.AddSpace(4)
		if toggle(ids, "opt", "Option[T]", &r.IsOption, optEnabled) {
			m.dirty = true
		}
		if toggle(ids, "slice", "[]T", &r.IsSlice, sliceEnabled) {
			m.dirty = true
		}
		if toggle(ids, "roaring", "roaring", &r.IsRoaring, roarEnabled) {
			m.dirty = true
		}
	}
}

// rowCategory classifies a row for the header colour-bar / word.
func rowCategory(r *FieldRow) (word string, col styletokens.RGBA8) {
	switch {
	case r.IsConst:
		return "const", styletokens.WarningDefault
	case r.Membership == "":
		return "plain", styletokens.InfoDefault
	default:
		return "value", styletokens.AccentDefault
	}
}

// editField renders a single-line text edit with hint text, optionally greyed
// out. The disable is localised with c.Scope() — egui's layout-transparent
// localization wrapper — not c.Horizontal(): a nested horizontal is a layout
// *container*, so each one is allocated as its own sub-region that does not
// share the parent row's vertical baseline, which drifts successive controls
// downward into a staircase. A Scope shares the parent cursor/layout, so the
// control sits on the row baseline exactly as a bare widget would, while
// c.UiDisable() inside it still only affects that one control. Returns true if
// the value changed this frame.
func editField(ids *c.WidgetIdStack, key, hint string, val *string, width float32, enabled bool) (changed bool) {
	for range c.Scope().KeepIter() {
		if !enabled {
			c.UiDisable()
		}
		changed = c.TextEdit(ids.PrepareStr(key), *val, false).HintText(hint).DesiredWidth(width).SendRespVal(val).HasChanged()
	}
	return
}

// toggle renders a checkbox, optionally greyed out + non-interactive when the
// combination it represents would be invalid. Scoped with c.Scope() for the
// same baseline reason as editField.
func toggle(ids *c.WidgetIdStack, key, label string, val *bool, enabled bool) (changed bool) {
	for range c.Scope().KeepIter() {
		if !enabled {
			c.UiDisable()
		}
		changed = c.Checkbox(ids.PrepareStr(key), *val, label).SendRespVal(val).HasChanged()
	}
	return
}

func renderChannelCombo(ids *c.WidgetIdStack, m *Model, r *FieldRow) {
	for range c.ComboBox(ids.PrepareStr("chan"),
		c.WidgetText().Text("channel").Keep(),
		c.WidgetText().Text(channelLabel(r.Channel)).Keep()).KeepIter() {
		for i, ch := range channelChoices {
			selected := ch == r.Channel
			if c.Button(ids.PrepareSeq(uint64(0x6368616e<<8)+uint64(i)),
				c.Atoms().Text(channelLabel(ch)).Keep()).
				Selected(selected).
				SendResp().HasPrimaryClicked() {
				if r.Channel != ch {
					r.Channel = ch
					m.dirty = true
				}
			}
		}
	}
}

func renderOutput(ids *c.WidgetIdStack, m *Model) {
	if m.Valid {
		for rt := range c.RichTextLabelColored(
			color.Hex(styletokens.SuccessDefault.AsHex()).Keep(),
			color.Transparent.Keep(),
			fmt.Sprintf("valid plan — %d field(s)", len(m.Fields))) {
			rt.Strong()
		}
	} else {
		for rt := range c.RichTextLabelColored(
			color.Hex(styletokens.ErrorDefault.AsHex()).Keep(),
			color.Transparent.Keep(),
			"invalid: "+firstLine(m.ErrText)) {
			rt.Strong()
		}
	}
	c.Separator().Send()

	if !m.Valid || len(m.panes) == 0 {
		// Invalid: show the PlanBuilder / emit error as plain read-only text.
		// viewBuf is a stable backing field for the TextEdit's id-keyed state.
		m.viewBuf = m.ErrText
		c.TextEdit(ids.PrepareStr("err"), m.viewBuf, true).
			Interactive(false).
			DesiredRows(previewRows).
			SendRespVal(&m.viewBuf)
		return
	}

	// Each generated output is a dock tab — draggable / splittable, layout
	// persisted by egui_dock across frames. The loop is format-agnostic, so a
	// new format (e.g. the dql SQL artefacts) is just another pane. Each job is
	// rebuilt only on recompute (Model.SetOutputs); here CodeView splices its
	// bytes into the frame. UiSetMinHeight gives the dock a bounded rect inside
	// a scrolling host; AutoShrink(false,false) makes each tab's ScrollArea fill
	// the pane (codeview uses the full width, scrollbar at the pane edge) and
	// Wrap keeps long lines inside that width.
	c.UiSetMinHeight(dockMinHeight)
	for dock := range c.DockArea(ids.PrepareStr("outdock")) {
		for i := range m.panes {
			p := &m.panes[i]
			for range dock.Tab(p.out.TabID, p.out.Title) {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					c.CodeView(ids.PrepareSeq(p.out.TabID), p.job).Wrap().Send()
				}
			}
		}
	}
}

// firstLine returns the first line of s (the headline of a multi-line
// PlanBuilder error), or a fallback when empty.
func firstLine(s string) string {
	if s == "" {
		return "invalid plan"
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
