// Package selector renders an "exactly one of N" choice bound directly to a
// Go enum value, filling the gap left by the egui2 bindings: egui's own
// enum-native helpers — `Ui::radio_value(&mut current, value, text)` and
// `Ui::selectable_value(...)`, whose whole point is to compare-and-assign an
// enum in one call — are not exposed in the IDL. Only the raw *bool-shaped
// primitives are (`RadioButton::new(checked)`, `Button::selectable(checked)`,
// `Button.Selected(bool)`), so every mutually-exclusive-choice site otherwise
// hand-rolls the same derive-checked / catch-click / assign loop.
//
// This package is that loop, written once. Like badge it is pure Go
// composition over existing FFFI2 primitives — no IDL or Rust changes — so the
// wire format is identical to the hand-written button/radio it replaces.
//
// Entry points:
//
//   - [RadioValue]   — one control bound to one value; call it standalone or in
//     your own loop. The direct analogue of egui's `radio_value`.
//   - [Segmented]    — a whole option bar over one *T, addressed through a
//     [c.WidgetIdStack] + scope key (the fsmview / kanban convention).
//   - [SegmentedAbs] — the same bar for widgets that address children by
//     absolute id (a `scope string`), with no id stack to thread.
//
// All replicate egui's exact change-rule: a primary click assigns
// `*current = value` and reports changed only when the value actually moved
// (re-clicking the already-selected option is not a change). The bridge to
// [c.ResponseFlagsE.HasPrimaryClicked] rather than a change flag is deliberate:
// egui's RadioButton / selectable never call `mark_changed`, so `HasChanged`
// would never fire — this is the apply-side gate of ADR-0013.
//
// The [Style] knob picks the visual skin; all three are the same databinding
// underneath, so a radio group and a segmented bar differ only in looks:
//
//	selector.Segmented(ids, "granularity", &opts.granularity).
//	    Option(rowPerDBRow, "per DB row").
//	    Option(rowPerAttr, "per attribute").
//	    SendResp()
//
//	selector.RadioValue(ids.PrepareStr("theme"), &cfg.theme, themeDark).
//	    Icon(icons.IconColorMode).Text("Dark").SendResp()
package selector

import (
	"strconv"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Style selects the visual skin. The three are interchangeable at the call
// site because they share one *bool databinding — the choice is purely visual.
type Style uint8

const (
	// StyleRadio renders egui's classic ○/● RadioButton dot. Default for
	// [RadioValue]; the natural fit for a vertical settings-style group.
	StyleRadio Style = iota
	// StyleSegmented renders a framed Button whose Selected state fills it —
	// the compact "segmented control" look. Default for [Segmented]; fits a
	// horizontal options bar better than a row of radio dots.
	StyleSegmented
	// StyleSelectable renders a frameless highlighted label
	// (egui `Button::selectable`) — the filter-chip / tag-picker look.
	StyleSelectable
)

// commit is the shared change-rule for both entry points, mirroring egui's
// radio_value: assign only on a click that actually moves the value, and report
// that edge. Keeping it in one place stops the single and group paths drifting.
func commit[T comparable](current *T, value T, clicked bool) (changed bool) {
	if clicked && *current != value {
		*current = value
		return true
	}
	return false
}

// renderControl draws one selectable control in the given style and returns
// whether it was primary-clicked this frame. It never mutates state — the
// caller owns the compare/assign via [commit] so both entry points share one
// rule. A non-empty tooltip wraps the single control in a HoverText scope;
// that is safe here precisely because a control is one widget (HoverText
// silently drops all but the first child of a multi-widget body).
func renderControl(id c.WidgetIdCreatorI, style Style, frameless, checked bool, label, icon, tooltip string) (clicked bool) {
	text := label
	if icon != "" {
		// U+00A0 NBSP keeps the glyph and label on one line, as badge does.
		text = icon + " " + label
	}
	render := func() {
		switch style {
		case StyleSegmented:
			b := c.Button(id, c.Atoms().Text(text).Keep()).Selected(checked)
			if frameless {
				// Frame(false) drops the button chrome — the borderless
				// segment used in dense toolbars and ComboBox popups.
				b = b.Frame(false)
			}
			clicked = b.SendResp().HasPrimaryClicked()
		case StyleSelectable:
			clicked = c.SelectableLabel(id, checked, text).
				SendResp().HasPrimaryClicked()
		default: // StyleRadio
			// egui's RadioButton has no plain SendResp; SendRespVal wants a
			// *bool sink. We recompute `checked` from the source of truth every
			// frame, so the written-back value is unread — a throwaway per the
			// standard-widgets demo pattern.
			var sink bool
			clicked = c.RadioButton(id, c.Atoms().Text(text).Keep(), checked).
				SendRespVal(&sink).HasPrimaryClicked()
		}
	}
	if tooltip != "" {
		for range c.HoverText(tooltip).KeepIter() {
			render()
		}
	} else {
		render()
	}
	return
}

// -----------------------------------------------------------------------------
// RadioValue — a single control bound to one value (egui radio_value analogue)
// -----------------------------------------------------------------------------

// ValueFluid is the chained builder for one selectable control. Zero value is
// not valid; always start from [RadioValue].
type ValueFluid[T comparable] struct {
	id      c.WidgetIdCreatorI
	current *T
	value   T
	label   string
	icon    string
	tooltip string
	style   Style
}

// RadioValue binds one control to one enum value. It takes any
// [c.WidgetIdCreatorI] — like badge — because it is a single widget:
// `ids.PrepareStr("x")`, `ids.PrepareSeq(i)` inside a loop, or an absolute id.
// The control shows selected when `*current == value` and, on a primary click,
// assigns `*current = value`. Defaults to [StyleRadio].
func RadioValue[T comparable](id c.WidgetIdCreatorI, current *T, value T) ValueFluid[T] {
	return ValueFluid[T]{id: id, current: current, value: value, style: StyleRadio}
}

// Text sets the label shown beside the control.
func (inst ValueFluid[T]) Text(label string) ValueFluid[T] { inst.label = label; return inst }

// Icon prefixes the label with a glyph (typically an `icons.IconXxx` rune),
// joined by a non-breaking space so it never wraps off the control.
func (inst ValueFluid[T]) Icon(glyph string) ValueFluid[T] { inst.icon = glyph; return inst }

// Tooltip shows the given string on hover. Empty is a no-op.
func (inst ValueFluid[T]) Tooltip(text string) ValueFluid[T] { inst.tooltip = text; return inst }

// Style overrides the visual skin (default [StyleRadio]).
func (inst ValueFluid[T]) Style(s Style) ValueFluid[T] { inst.style = s; return inst }

// SendResp renders the control and returns true only on the frame a click moves
// the selection to this value (re-clicking the current value is not a change),
// so callers can gate a requery or other side effect on the edge.
func (inst ValueFluid[T]) SendResp() (changed bool) {
	clicked := renderControl(inst.id, inst.style, false, *inst.current == inst.value,
		inst.label, inst.icon, inst.tooltip)
	return commit(inst.current, inst.value, clicked)
}

// Send renders the control and discards the change edge — for callers that read
// `*current` next frame rather than reacting to the edge.
func (inst ValueFluid[T]) Send() { _ = inst.SendResp() }

// -----------------------------------------------------------------------------
// Segmented — an option bar over one *T (loops RadioValue, scopes child ids)
// -----------------------------------------------------------------------------

type option[T comparable] struct {
	value   T
	label   string
	icon    string
	tooltip string
}

// GroupFluid is the chained builder for an option bar. Zero value is not valid;
// always start from [Segmented] or [SegmentedAbs].
type GroupFluid[T comparable] struct {
	ids       *c.WidgetIdStack // nil for the SegmentedAbs (absolute-id) form
	scopeKey  string           // id scope name; used only when ids != nil
	absScope  string           // absolute-id prefix; used only when ids == nil
	current   *T
	opts      []option[T]
	style     Style
	vertical  bool
	inline    bool
	frameless bool
	gap       float32
}

// Segmented builds an exclusive option bar bound to `current`. Unlike
// [RadioValue] it takes the concrete `*c.WidgetIdStack` plus a `scopeKey`
// (the fsmview / kanban multi-child convention): the bar renders N children,
// so it needs to prepare the stack once per option, and the scope namespaces
// those ids so two bars on the same stack cannot collide. Defaults to
// [StyleSegmented]; add [GroupFluid.Vertical] + [GroupFluid.Style] with
// [StyleRadio] for a settings-style radio list.
func Segmented[T comparable](ids *c.WidgetIdStack, scopeKey string, current *T) GroupFluid[T] {
	return GroupFluid[T]{ids: ids, scopeKey: scopeKey, current: current, style: StyleSegmented}
}

// SegmentedAbs is [Segmented] for widgets that address their children by
// absolute id (a `scope string` fed to [c.MakeAbsoluteIdStr]) instead of
// threading a [c.WidgetIdStack] — the distsummary / canonicaltypesummary
// "widget-in-a-box" convention. Each option's id is derived as
// MakeAbsoluteIdStr(scope + "#" + i), so pass a `scope` unique to this bar
// (e.g. the widget scope + "-tab"). No id scope is opened — absolute ids are
// already globally unique.
func SegmentedAbs[T comparable](scope string, current *T) GroupFluid[T] {
	return GroupFluid[T]{absScope: scope, current: current, style: StyleSegmented}
}

// Option appends a choice. Order is render order.
func (inst GroupFluid[T]) Option(value T, label string) GroupFluid[T] {
	inst.opts = append(inst.opts, option[T]{value: value, label: label})
	return inst
}

// OptionIcon appends a choice with a leading glyph and an optional hover
// tooltip (pass "" for none).
func (inst GroupFluid[T]) OptionIcon(value T, icon, label, tooltip string) GroupFluid[T] {
	inst.opts = append(inst.opts, option[T]{value: value, label: label, icon: icon, tooltip: tooltip})
	return inst
}

// Style overrides the visual skin (default [StyleSegmented]).
func (inst GroupFluid[T]) Style(s Style) GroupFluid[T] { inst.style = s; return inst }

// Vertical stacks the options top-to-bottom instead of left-to-right — the
// settings-style layout, usually paired with [StyleRadio].
func (inst GroupFluid[T]) Vertical() GroupFluid[T] { inst.vertical = true; return inst }

// Inline emits the options straight into the caller's existing layout instead
// of opening the bar's own HorizontalTop. Use it when the bar shares a row with
// other widgets (a label, a gap, sibling checkboxes) and the nested-horizontal
// vertical offset would misalign them. The id scope is still opened, so two
// inline bars on one stack stay collision-free. When set, [GroupFluid.Vertical]
// is ignored — orientation is then the caller's layout.
func (inst GroupFluid[T]) Inline() GroupFluid[T] { inst.inline = true; return inst }

// Gap inserts px logical points of space between adjacent options (never before
// the first or after the last) — matches the AddSpace(GapInline(density)) that
// hand-rolled tab bars put between segments. Zero (the default) packs them flush.
func (inst GroupFluid[T]) Gap(px float32) GroupFluid[T] { inst.gap = px; return inst }

// Frameless drops the button chrome on [StyleSegmented] (renders each segment
// with Frame(false)); it is a no-op for [StyleSelectable] (already frameless)
// and [StyleRadio].
func (inst GroupFluid[T]) Frameless() GroupFluid[T] { inst.frameless = true; return inst }

// SendResp lays out the bar and returns true on the frame a click moves the
// selection. Stack-form bars render under one IdScope keyed by scopeKey;
// absolute-id bars need no scope.
func (inst GroupFluid[T]) SendResp() (changed bool) {
	withLayout := func() {
		switch {
		case inst.inline:
			// Caller owns the surrounding layout row; emit options into it.
			changed = inst.renderOptions()
		case inst.vertical:
			for range c.Vertical().KeepIter() {
				changed = inst.renderOptions()
			}
		default:
			for range c.HorizontalTop().KeepIter() {
				changed = inst.renderOptions()
			}
		}
	}
	if inst.ids != nil {
		for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
			withLayout()
		}
	} else {
		withLayout()
	}
	return
}

// renderOptions draws every option once, gap-separated, and reports whether any
// click moved the selection. At most one option can be clicked per frame, so
// the running OR yields that option's change edge.
func (inst GroupFluid[T]) renderOptions() (changed bool) {
	for i := range inst.opts {
		if i > 0 && inst.gap > 0 {
			c.AddSpace(inst.gap)
		}
		o := inst.opts[i]
		clicked := renderControl(inst.idFor(i), inst.style, inst.frameless,
			*inst.current == o.value, o.label, o.icon, o.tooltip)
		if commit(inst.current, o.value, clicked) {
			changed = true
		}
	}
	return
}

// idFor derives option i's id from whichever source the constructor set:
// PrepareSeq under the pushed scope (stack form) or a per-index absolute id.
func (inst GroupFluid[T]) idFor(i int) c.WidgetIdCreatorI {
	if inst.ids != nil {
		return inst.ids.PrepareSeq(uint64(i))
	}
	return c.MakeAbsoluteIdStr(inst.absScope + "#" + strconv.Itoa(i))
}

// Send lays out the bar and discards the change edge.
func (inst GroupFluid[T]) Send() { _ = inst.SendResp() }
