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
	editorMinWidth = 470
	outputMinWidth = 560
	previewRows    = 30
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
		for range c.Horizontal().KeepIter() {
			// Left: the editor, in an explicit Vertical column. A bare Group
			// would inherit the surrounding Horizontal layout and stream its
			// children rightward; a pinned-width Vertical fixes the column.
			// Edits set m.dirty via the per-control HasChanged.
			for range c.Vertical().KeepIter() {
				c.UiSetMinWidth(editorMinWidth)
				c.UiSetMaxWidth(editorMinWidth)
				renderEditor(in.Ids, m)
			}

			// Recompute between the panes — pure Go, emits no UI — so the right
			// pane reflects this frame's edits rather than last frame's.
			if m.dirty && in.Recompute != nil {
				in.Recompute(m)
				m.dirty = false
			}

			c.AddSpace(8)

			// Right: the generated-code preview + verdict, in its own column.
			for range c.Vertical().KeepIter() {
				c.UiSetMinWidth(outputMinWidth)
				c.UiSetMaxWidth(outputMinWidth)
				renderOutput(in.Ids, m)
			}
		}
	}
}

func renderEditor(ids *c.WidgetIdStack, m *Model) {
	for rt := range c.RichTextLabel("plan") {
		rt.Strong()
	}
	for range c.Horizontal().KeepIter() {
		if c.TextEdit(ids.PrepareStr("kind"), m.Kind, false).DesiredWidth(150).SendRespVal(&m.Kind).HasChanged() {
			m.dirty = true
		}
		if c.TextEdit(ids.PrepareStr("pkg"), m.PackageName, false).DesiredWidth(130).SendRespVal(&m.PackageName).HasChanged() {
			m.dirty = true
		}
		if c.TextEdit(ids.PrepareStr("type"), m.KindType, false).DesiredWidth(150).SendRespVal(&m.KindType).HasChanged() {
			m.dirty = true
		}
	}
	c.Label("kind / package / type").Send()
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
	}
	if hasRemove {
		m.removeByUID(removeUID)
	}

	c.AddSpace(4)
	if c.Button(ids.PrepareStr("add-field"), c.Atoms().Text("+ field").Keep()).SendResp().HasPrimaryClicked() {
		m.AddRow()
	}
}

// renderRow draws one field row and returns true if its remove button fired.
func renderRow(ids *c.WidgetIdStack, m *Model, r *FieldRow) (remove bool) {
	for range c.Frame(ids.PrepareStr("rowframe")).
		Fill(color.Hex(styletokens.NeutralBgSurface.AsHex())).
		InnerMargin(6).
		CornerRadius(4).
		KeepIter() {

		// Go field name, Go type, remove.
		for range c.Horizontal().KeepIter() {
			if c.TextEdit(ids.PrepareStr("gofield"), r.GoField, false).DesiredWidth(120).SendRespVal(&r.GoField).HasChanged() {
				m.dirty = true
			}
			if c.TextEdit(ids.PrepareStr("gotype"), r.GoType, false).DesiredWidth(110).SendRespVal(&r.GoType).HasChanged() {
				m.dirty = true
			}
			if c.Button(ids.PrepareStr("rm"), c.Atoms().Text("remove").Keep()).SendResp().HasPrimaryClicked() {
				remove = true
			}
		}

		// Membership, section, sub-column.
		for range c.Horizontal().KeepIter() {
			if c.TextEdit(ids.PrepareStr("memb"), r.Membership, false).DesiredWidth(120).SendRespVal(&r.Membership).HasChanged() {
				m.dirty = true
			}
			if c.TextEdit(ids.PrepareStr("sec"), r.Section, false).DesiredWidth(110).SendRespVal(&r.Section).HasChanged() {
				m.dirty = true
			}
			if c.TextEdit(ids.PrepareStr("col"), r.Column, false).DesiredWidth(90).SendRespVal(&r.Column).HasChanged() {
				m.dirty = true
			}
		}

		// Channel + flags + shape — only meaningful on tagged value/const
		// fields; a plain column (empty membership) carries none of them.
		if r.Membership != "" {
			for range c.Horizontal().KeepIter() {
				renderChannelCombo(ids, m, r)
				if c.Checkbox(ids.PrepareStr("unit"), r.Unit, "unit").SendRespVal(&r.Unit).HasChanged() {
					m.dirty = true
				}
				if c.Checkbox(ids.PrepareStr("explode"), r.Explode, "explode").SendRespVal(&r.Explode).HasChanged() {
					m.dirty = true
				}
				if c.Checkbox(ids.PrepareStr("const"), r.IsConst, "const").SendRespVal(&r.IsConst).HasChanged() {
					m.dirty = true
				}
				if r.IsConst {
					if c.TextEdit(ids.PrepareStr("constval"), r.ConstValue, false).DesiredWidth(100).SendRespVal(&r.ConstValue).HasChanged() {
						m.dirty = true
					}
				}
			}
			for range c.Horizontal().KeepIter() {
				if c.Checkbox(ids.PrepareStr("opt"), r.IsOption, "option").SendRespVal(&r.IsOption).HasChanged() {
					m.dirty = true
				}
				if c.Checkbox(ids.PrepareStr("slice"), r.IsSlice, "slice").SendRespVal(&r.IsSlice).HasChanged() {
					m.dirty = true
				}
				if c.Checkbox(ids.PrepareStr("roaring"), r.IsRoaring, "roaring").SendRespVal(&r.IsRoaring).HasChanged() {
					m.dirty = true
				}
			}
		}

		// The assembled lw: tag, read-only — what PlanBuilder actually parses.
		for rt := range c.RichTextLabel(`lw:"` + r.LWTag() + `"`) {
			rt.Small()
		}
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

	// Read-only monospace code view: a transient per-frame string (no retained
	// codeview holder to recycle). DesiredRows sets the height; the multiline
	// TextEdit scrolls internally for longer output. viewBuf is a stable
	// backing field so any id-keyed edit state tracks the latest content.
	if m.Valid {
		m.viewBuf = m.GoPreview
	} else {
		m.viewBuf = m.ErrText
	}
	c.TextEdit(ids.PrepareStr("preview"), m.viewBuf, true).
		CodeEditor().
		Interactive(false).
		DesiredRows(previewRows).
		DesiredWidth(outputMinWidth - 16).
		SendRespVal(&m.viewBuf)
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
