package mappingplanview

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
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
	// Recompute rebuilds the output panes from the Model. The host supplies it
	// (it owns the mappingplan / marshallgen / dql wiring); Render calls it at
	// most once per frame, only when the Model is dirty, reporting back through
	// Model.SetOutputs / Model.SetInvalid. The host must also call it once at
	// init so the dock's initial split has the output tab ids to place.
	Recompute func(*Model)
}

const (
	// editorTabID is the dock tab id of the editor pane. Reserved high so it
	// never collides with host-supplied Output.TabID values (which start at 1).
	editorTabID uint64 = 1 << 62

	editorFrac    = 0.40 // fraction of the dock width the editor leaf keeps (left); outputs take the rest
	dockMinHeight = 460  // floor so the dock has a bounded rect in the gallery's scroll host
	cardWidth     = 430  // uniform field-card content width
	cardMinHeight = 128  // uniform field-card content min height (shorter cards pad up to it)
	rowBarWidth   = 4
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

// Render draws the whole widget as a single dock area: the editor pane on the
// left and one generated-output pane (Go / SQL / JSON) per Output on the right.
// The split is the initial preset — the user can drag panes around and the
// layout persists (egui_dock). Call once per frame.
func Render(in Input) {
	m := in.Model
	for range c.IdScope(in.Ids.PrepareStr(in.ScopeKey)) {
		c.UiSetMinHeight(dockMinHeight) // bound the dock in a scrolling host (gallery)
		for dock := range c.DockArea(in.Ids.PrepareStr("mpvdock")) {
			// Initial layout (honoured once, on first dock_state construction):
			// editor in the root leaf, the output panes split off to its right.
			// The output ids come from the panes the host produced — populated
			// by an initial Recompute before the first frame (see the demo Init);
			// without that they would be empty here and the split would be lost.
			root := dock.InitRoot(editorTabID)
			if outIDs := paneTabIDs(m); len(outIDs) > 0 {
				dock.Split(root, c.DockRight, editorFrac, outIDs...)
			}

			// Editor pane. SendRespVal bindings inside apply this frame's input
			// during this tab's capture (pure-Go pointer writes), so the model
			// reflects the edits before the recompute below.
			for range dock.Tab(editorTabID, "plan") {
				renderEditor(in.Ids, m)
			}

			// Recompute between the editor tab and the output tabs — pure Go,
			// emits no UI — so the output panes show this frame's edits.
			if m.dirty && in.Recompute != nil {
				in.Recompute(m)
				m.dirty = false
			}

			// One dock tab per output pane. Format-agnostic: a new format is
			// just another pane. The codeview job is rebuilt only on recompute
			// (Model.SetOutputs); here CodeView splices its bytes into the frame.
			for i := range m.panes {
				p := &m.panes[i]
				for range dock.Tab(p.out.TabID, p.out.Title) {
					for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
						c.CodeView(in.Ids.PrepareSeq(p.out.TabID), p.job).Wrap().Send()
					}
				}
			}
		}
		// Per-field inspector windows are spawned AFTER the DockArea block: a
		// floating window cannot be created from inside a dock-tab body. The
		// chips in the editor tab only captured their toggle rects; the tether
		// bridges toggle ↔ window by scope. Mirrors schemaview's glyph-legend.
		renderFieldPopups(in.Ids, m)
	}
}

// renderFieldPopups draws the open per-field inspector windows for the rows on
// the current page — the only rows whose chips (hence toggle rects) were emitted
// this frame. A field whose inspector is left open while you page away is simply
// not drawn until you page back; its open flag is retained on the Widget.
func renderFieldPopups(ids *c.WidgetIdStack, m *Model) {
	start, end := m.pager.Range()
	for i := start; i < end && i < int64(len(m.Fields)); i++ {
		r := m.Fields[i]
		if r.fsmW != nil && r.fsmW.IsOpen() {
			for range c.IdScope(ids.PrepareSeq(r.uid)) {
				r.fsmW.RenderPopup()
			}
		}
	}
}

// paneTabIDs collects the current output pane tab ids for the initial split.
func paneTabIDs(m *Model) []uint64 {
	out := make([]uint64, 0, len(m.panes))
	for i := range m.panes {
		out = append(out, m.panes[i].out.TabID)
	}
	return out
}

func renderEditor(ids *c.WidgetIdStack, m *Model) {
	renderVerdict(ids, m)
	c.Separator().Send()

	// Plan identity — the `_`-field's kind / package / Go type. Styled like the
	// schemaview header (Strong + Size(15) title, weak caption) with each input
	// labelled and tooltip-described so the trio reads unambiguously.
	for rt := range c.RichTextLabel("plan") {
		rt.Strong().Size(15)
	}
	for rt := range c.RichTextLabel("the _-field identity the generated codec & SQL are built for") {
		rt.Weak().Small()
	}
	c.AddSpace(2)
	for range c.Horizontal().KeepIter() {
		if labeledField(ids, "kind", "entity kind",
			"entity kind — the kind: declared on the _ field; names the entity this plan maps",
			"kind: tag", &m.Kind, 150) {
			m.dirty = true
		}
		if labeledField(ids, "pkg", "Go package",
			"package of the generated DTO; cosmetic — it only sets the package header in the Go preview",
			"DTO package", &m.PackageName, 130) {
			m.dirty = true
		}
		if labeledField(ids, "type", "DTO type",
			"Go struct type the codec marshals — the DTO whose fields the rows below describe",
			"struct name", &m.KindType, 150) {
			m.dirty = true
		}
	}
	c.Separator().Send()

	// Pagination via the shared pager widget (extracted from apps/play):
	// Configure with the field total, draw the bar, then render the current
	// page's cards from the pager's Range.
	m.pager.Configure(int64(len(m.Fields)))
	m.pager.Render()
	start, end := m.pager.Range()
	var removeUID uint64
	hasRemove := false
	for i := start; i < end; i++ {
		r := m.Fields[i]
		for range c.IdScope(ids.PrepareSeq(r.uid)) {
			if renderRow(ids, m, r) {
				removeUID = r.uid
				hasRemove = true
			}
		}
		c.AddSpace(6)
	}
	if hasRemove {
		m.removeByUID(removeUID)
	}

	if c.Button(ids.PrepareStr("add-field"), c.Atoms().Text("+ field").Keep()).SendResp().HasPrimaryClicked() {
		m.AddRow()
		m.pager.GoToLast() // jump to the new field's page
	}
}

// renderVerdict draws the PlanBuilder verdict at the top of the editor pane —
// green "valid" or red "invalid" plus the full error text (read-only).
func renderVerdict(ids *c.WidgetIdStack, m *Model) {
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
		m.viewBuf = m.ErrText
		c.TextEdit(ids.PrepareStr("err"), m.viewBuf, true).
			Interactive(false).
			DesiredRows(4).
			SendRespVal(&m.viewBuf)
	}
	// Per-field roll-up — how many fields sit in each validity state — so the
	// editor reads at a glance before scanning individual chips. (A plan-level
	// Finish conflict shows in the invalid headline above; per-field Conflicting
	// counts here come from AddField-time collisions.)
	if summary := m.stateRollup(); summary != "" {
		for rt := range c.RichTextLabel(summary) {
			rt.Weak().Small()
		}
	}
}

// renderRow draws one field row as a fixed-size bordered card (uniform width +
// min height so the cards line up) and returns true if its remove button fired.
func renderRow(ids *c.WidgetIdStack, m *Model, r *FieldRow) (remove bool) {
	// A const is a fixed literal declared on a `_` field: no Go field, no
	// Option, no explode. Normalise those off so the row stays valid and the
	// type editor + flags below render disabled.
	if r.IsConst && (r.IsOption || r.Explode) {
		r.IsOption, r.Explode = false, false
		m.dirty = true
	}

	glyph, word, catCol := rowCategory(r)
	tagged := r.Membership != ""

	for range c.Horizontal().KeepIter() {
		// Category colour bar — plain / value / const at a glance.
		for range c.Frame(ids.PrepareStr("bar")).Fill(color.Hex(catCol.AsHex())).CornerRadius(3).KeepIter() {
			c.AddSpace(rowBarWidth)
		}
		c.AddSpace(6)

		// Framed body — a bordered, fixed-size card. UiSetMin/MaxWidth pins the
		// width and UiSetMinHeight pads shorter cards so every field is the same
		// size. The Vertical pins line stacking (a Frame inherits the
		// surrounding Horizontal otherwise).
		for range c.Frame(ids.PrepareStr("body")).
			Fill(color.Hex(styletokens.NeutralBgSurface.AsHex())).
			Stroke(1, color.Hex(styletokens.NeutralBorderDefault.AsHex())).
			InnerMargin(8).
			CornerRadius(4).
			KeepIter() {
			for range c.Vertical().KeepIter() {
				c.UiSetMinWidth(cardWidth)
				c.UiSetMaxWidth(cardWidth)
				c.UiSetMinHeight(cardMinHeight)
				renderRowHeader(ids, r, glyph, word, catCol)
				renderRowReason(r)

				// Go field + remove (the value type is its own editor below).
				for range c.Horizontal().KeepIter() {
					if editField(ids, "gofield", "Go field", &r.GoField, 120, !r.IsConst) {
						m.dirty = true
					}
					// Icon-only remove (× via IconClose), tooltip-described for
					// discoverability — matches the inspector's icon-affordance style.
					for range c.HoverText("remove field").KeepIter() {
						if c.Button(ids.PrepareStr("rm"), c.Atoms().Text(icons.IconClose).Keep()).Small().SendResp().HasPrimaryClicked() {
							remove = true
						}
					}
				}

				// Value type — authored as a leeway canonical (ADR-0008) via the
				// canonicaltypeedit bar/form, in place of a Go-type text box. Greyed
				// for a const (a const carries a literal, not a value-type field).
				renderTypeEditor(ids, m, r)

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

// renderRowHeader draws the category glyph + word (coloured, echoing the
// schemaview navigator), the field name, and the live assembled lw: tag
// (monospace, since it is a code string).
func renderRowHeader(ids *c.WidgetIdStack, r *FieldRow, glyph, word string, catCol styletokens.RGBA8) {
	name := rowDisplayName(r)

	// Per-field validity chip on its own row (validity-first; a chip is itself a
	// nested Horizontal, so keeping it off the identity row avoids the baseline
	// staircase nested layout containers cause — see editField). The
	// fsmview.Widget is lazily built (it needs the frame's id stack); its title
	// tracks the field name; the machine is mirrored to this row's derived state
	// each frame, carrying the reason so the inspector History shows *why* it
	// moved. Same-state mirrors are no-ops, so a steady field records nothing.
	if r.fsm != nil {
		if r.fsmW == nil {
			r.fsmW = fsmview.New(ids, fieldFSMScope(r.uid), r.fsm).Tethered().BadgeTone(FieldState.tone)
			if r.seedOpen {
				r.fsmW.Open()
			}
		}
		r.fsmW.Title("field: " + name)
		var md map[string]string
		if r.reason != "" {
			md = map[string]string{"reason": r.reason}
		}
		r.fsm.MirrorWithMetadata(r.state, md)
		r.fsmW.RenderChip()
	}

	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabelColored(color.Hex(catCol.AsHex()).Keep(), color.Transparent.Keep(), glyph+" "+word) {
			rt.Small()
		}
		c.AddSpace(6)
		for rt := range c.RichTextLabel(name) {
			rt.Strong()
		}
		c.AddSpace(8)
		for rt := range c.RichTextLabel(`lw:"` + r.LWTag() + `"`) {
			rt.Weak().Small().Monospace()
		}
	}
}

// rowDisplayName is the field's human label: the Go field name, else the section
// (for plain columns / value-less rows), else a placeholder.
func rowDisplayName(r *FieldRow) string {
	if r.GoField != "" {
		return r.GoField
	}
	if r.Section != "" {
		return r.Section
	}
	return "field"
}

// fieldFSMScope is the per-row fsmview scope key — stable (keyed by the row's
// uid) and unique, so each field's chip / inspector / tether stay independent.
func fieldFSMScope(uid uint64) string {
	return fmt.Sprintf("mpv-fld-%d", uid)
}

// renderRowReason shows a terse "<state>: <why>" line under the header for any
// field that isn't cleanly Valid or Empty — the at-a-glance "why" without
// opening the inspector. Coloured by state severity; the full reason + the
// transition history live in the tethered inspector.
func renderRowReason(r *FieldRow) {
	if r.reason == "" || r.state == StateValid || r.state == StateEmpty {
		return
	}
	col := stateColor(r.state, true)
	for rt := range c.RichTextLabelColored(color.Hex(col.AsHex()).Keep(), color.Transparent.Keep(), r.state.label()+": "+r.reason) {
		rt.Small()
	}
}

// renderTypeEditor renders the per-row canonical type editor (the
// canonicaltypeedit bar/form), greyed for a const row, and marks the model
// dirty when the canonical changes (the editor exposes no change signal, so we
// compare it frame-to-frame).
func renderTypeEditor(ids *c.WidgetIdStack, m *Model, r *FieldRow) {
	for range c.Scope().KeepIter() {
		if r.IsConst {
			c.UiDisable()
		}
		r.typeModel.Render(ids, "ctype")
	}
	if cur := r.typeModel.Canonical(); cur != r.lastCanonical {
		r.lastCanonical = cur
		m.dirty = true
	}
}

// renderRowFlags draws the channel picker, the Option[T] presence flag, and the
// unit/explode/const flags, disabling any control whose toggle would compose an
// invalid field. Multiplicity ([]T / roaring) now lives in the canonical type
// (HomogenousArray / Set modifier), not separate toggles; it is read back here
// to gate explode/unit/Option. A control stays interactive while on, so a state
// that became invalid (e.g. by editing the type) can always be backed out.
func renderRowFlags(ids *c.WidgetIdStack, m *Model, r *FieldRow) {
	mod, _ := canonicaltypes.GetScalarModifier(r.typeModel.Node())
	isMulti := mod == canonicaltypes.ScalarModifierHomogenousArray || mod == canonicaltypes.ScalarModifierSet
	optEnabled := !r.IsConst && (r.IsOption || !isMulti)   // Option only over a scalar value type
	explodeEnabled := !r.IsConst && (r.Explode || isMulti) // explode requires a multi shape
	unitEnabled := r.Unit || !(isMulti && !r.Explode)      // unit on a multi shape requires explode

	for range c.Horizontal().KeepIter() {
		renderChannelCombo(ids, m, r)
		if toggle(ids, "opt", "Option[T]", &r.IsOption, optEnabled) {
			m.dirty = true
		}
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
}

// rowCategory classifies a row in leeway's own vocabulary: plain (◆) and
// tagged (◇) are leeway's two membership categories — matching the schemaview
// navigator's glyphs (◆ plain item-types, ◇ tagged sections) — while const (▪)
// is a mappingplan refinement (a constant declared on a tagged `_` field). The
// colour tints the header bar and category word.
func rowCategory(r *FieldRow) (glyph, word string, col styletokens.RGBA8) {
	switch {
	case r.IsConst:
		return "▪", "const", styletokens.WarningDefault
	case r.Membership == "":
		return "◆", "plain", styletokens.InfoDefault
	default:
		return "◇", "tagged", styletokens.AccentDefault
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

// labeledField stacks a weak caption above a text field — schemaview's
// label-over-value rhythm — and wraps the input in a HoverText scope so the
// fuller tip describes its role on hover. The plan-identity inputs are always
// editable, so there is no disable path.
func labeledField(ids *c.WidgetIdStack, key, label, tip, hint string, val *string, width float32) (changed bool) {
	for range c.Vertical().KeepIter() {
		for rt := range c.RichTextLabel(label) {
			rt.Weak().Small()
		}
		for range c.HoverText(tip).KeepIter() {
			changed = editField(ids, key, hint, val, width, true)
		}
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
