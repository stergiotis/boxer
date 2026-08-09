package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "focus",
		Category: "Layout & widgets",
		Title:    icons.PhKeyboard + " focus",
		Stage:    [2]float32{900, 500},
		Kind:     registry.DemoKindDX,
		Description: "ADR-0177 M0+M1: a Frame can be made focusable, focus can be moved from code, and " +
			"a focused Frame can capture the keys it declares. Click either panel to focus it, Tab " +
			"between them, or use the buttons. Only panel B declares a capture mask: with it focused " +
			"the arrow keys are consumed and logged, and the surrounding gallery does not scroll on " +
			"them. Focus panel A instead and the same keys scroll the gallery as usual — the " +
			"difference is the mask, not the focus.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &focusDemoState{}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoFocus(ids, state.(*focusDemoState))
		},
		SourceFunc: demoFocus,
	})
}

// =============================================================================
// focus — ADR-0177 M0 and M1.
//
// What this exists to show, and to prove:
//
//   - `.CaptureKeys(mask)` on a focused Frame consumes exactly the keys the
//     mask names (SD1–SD3), and they arrive in Go via GetCapturedKeys. Only
//     panel B declares a mask, so the two panels differ in one thing and the
//     effect of that one thing is watchable: arrows pressed over B are logged
//     and the gallery stays put; the same arrows over A scroll the gallery.
//     That contrast is the fencing observable, not a claim about it.
//
//   - `.Focusable()` on a Frame registers a post-body interact rect (SD8), so
//     the Frame becomes something egui can focus: click it, or Tab to it.
//   - `RequestFocus(id)` / `SurrenderFocus(id)` move focus from Go — and only
//     against an id egui knows. That is SD7's trap: an imzero2 widget id is
//     only the r7 read-back key, so requesting focus for a widget that did NOT
//     call `.Focusable()` silently does nothing. The third button below does
//     exactly that, on purpose, so the failure mode is visible rather than
//     described.
//   - The focus flags were already plumbed (HAS_FOCUS / GAINED_FOCUS /
//     LOST_FOCUS reach Go as ResponseFlagsE); M0 only added something to hang
//     them on.
//
// Read-back is one frame late, like every response here, so the readout
// describes the PREVIOUS frame.
// =============================================================================

type focusDemoState struct {
	// events is a short log of focus transitions, newest last. A log rather
	// than a flag because Gained/Lost are one-frame edges — a reader watching
	// only the current frame would miss every one of them.
	events []string
	// wantFocus is a one-shot request: set by a button, consumed by the render.
	// Requesting every frame would PIN focus and make the panels impossible to
	// Tab out of, which is the mistake this field exists to avoid.
	wantFocus  int
	wantSurren int
	// lastFocused is which panel held focus as of the last GAINED edge — the
	// state the one-frame Gained/Lost edges add up to.
	lastFocused int
	// handles are the ids the panels' Frames ACTUALLY sent, captured during
	// render. Not the ids handed to c.Frame: the generated factory calls
	// i.DeriveStacked(), so the value on the wire is re-derived against the id
	// stack and differs from what the caller passed. FrameFluid.Id() is the
	// only correct source, and reading r7 or calling RequestFocus with
	// anything else fails SILENTLY — which is SD7's trap, one level deeper
	// than the ADR states it.
	handles map[int]uint64
}

const (
	focusNone = 0
	focusA    = 1
	focusB    = 2
	// focusGhost asks for focus on a widget that never called .Focusable().
	// Nothing happens, and that is the lesson.
	focusGhost = 3
)

func (inst *focusDemoState) log(s string) {
	inst.events = append(inst.events, s)
	if len(inst.events) > 6 {
		inst.events = inst.events[len(inst.events)-6:]
	}
}

func demoFocus(ids *c.WidgetIdStack, st *focusDemoState) {
	stdSection("two focusable panels",
		"click one, or Tab between them; only B captures the arrow keys")

	for range c.Horizontal().KeepIter() {
		st.panel(ids, "A", focusA, false)
		c.AddSpace(gapSections())
		st.panel(ids, "B", focusB, true)
	}

	c.AddSpace(padInner())
	stdSection("moving focus from Go",
		"RequestFocus works only against an id egui registered — the third button proves it")

	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("fk-a"), c.Atoms().Text("focus A").Keep()).
			SendResp().HasPrimaryClicked() {
			st.wantFocus = focusA
		}
		c.AddSpace(gapInline())
		if c.Button(ids.PrepareStr("fk-b"), c.Atoms().Text("focus B").Keep()).
			SendResp().HasPrimaryClicked() {
			st.wantFocus = focusB
		}
		c.AddSpace(gapInline())
		if c.Button(ids.PrepareStr("fk-ghost"), c.Atoms().Text("focus a non-focusable id").Keep()).
			SendResp().HasPrimaryClicked() {
			st.wantFocus = focusGhost
			st.log("requested focus on an id egui never registered — expect nothing")
		}
		c.AddSpace(gapInline())
		if c.Button(ids.PrepareStr("fk-drop"), c.Atoms().Text("surrender").Keep()).
			SendResp().HasPrimaryClicked() {
			st.wantSurren = st.focused()
		}
	}

	// Consume the one-shot requests AFTER the panels rendered, so the id they
	// registered exists this frame.
	switch st.wantFocus {
	case focusA, focusB:
		if h := st.handle(st.wantFocus); h != 0 {
			c.RequestFocus(h)
		} else {
			st.log("emit skipped: handle is 0")
		}
	case focusGhost:
		// A real id, on a widget that never called .Focusable(). egui has
		// never heard of it, so this does nothing at all — the silence SD7
		// describes, made visible by putting it next to two buttons that work.
		c.RequestFocus(ids.PrepareStr("fk-ghost-never-focusable").Derive())
	}
	st.wantFocus = focusNone
	if h := st.handle(st.wantSurren); h != 0 {
		c.SurrenderFocus(h)
	}
	st.wantSurren = focusNone

	c.AddSpace(padInner())
	stdSection("focus transitions and captured keys",
		"focus panel B and press the arrow keys — captured, consumed, and logged here")
	if len(st.events) == 0 {
		for rt := range c.RichTextLabel("(nothing yet — click a panel)") {
			rt.Weak().Italics()
		}
	}
	for _, e := range st.events {
		for rt := range c.RichTextLabel(e) {
			rt.Monospace().Small()
		}
	}
}

// describeKey renders one captured event the way a keymap would write it, so
// the modifier byte is legible rather than a number. Modifiers ride the event
// instead of being part of the match (SD5): Shift+Down arrives as Down with
// Shift set, which is what lets a tree bind "extend selection" without needing
// a second mask entry.
func describeKey(k c.CapturedKey) string {
	s := ""
	if k.Ctrl() {
		s += "Ctrl+"
	}
	if k.Alt() {
		s += "Alt+"
	}
	if k.Shift() {
		s += "Shift+"
	}
	return s + k.Code.String()
}

// handle is the id a panel's Frame put on the wire last frame, or 0 before it
// has rendered once.
func (inst *focusDemoState) handle(which int) uint64 {
	return inst.handles[which]
}

// focused reports which panel held focus as of last frame, or focusNone.
func (inst *focusDemoState) focused() int {
	return inst.lastFocused
}

func (inst *focusDemoState) panel(ids *c.WidgetIdStack, name string, which int, captures bool) {
	// Build the Frame FIRST and take the id it will send. c.Frame stamps its id
	// at construction via DeriveStacked(), so this is the only value that
	// matches what r7 is keyed by; the decoration is chained on afterwards.
	f := c.Frame(ids.PrepareStr("fk-panel-" + name))
	id := f.Id()
	if inst.handles == nil {
		inst.handles = make(map[int]uint64, 2)
	}
	inst.handles[which] = id

	// The Frame is created unconditionally and only its decoration varies —
	// a Frame that appeared and disappeared would XOR every inner widget's id
	// in and out of the stack (see the row-highlight reference).
	flags := c.CurrentApplicationState.StateManager.GetResponseByIdRaw(id)
	stroke, strokeW := color.Transparent, float32(0)
	if flags.HasFocus() {
		stroke, strokeW = color.Hex(styletokens.AccentDefault.AsHex()), 2.0
	}
	// The transitions are derived from the LEVEL (HasFocus) rather than read
	// from the GAINED_FOCUS / LOST_FOCUS edge flags, because those edges are
	// not reliable when focus moves programmatically. `RequestFocus` is applied
	// at the end of a pass, after the widget already ran; egui snapshots
	// `id_previous_frame` from the focus state at the end of that same pass, so
	// by the time the widget next runs it "always had" focus and
	// `gained_focus()` is false. Focus moved — the edge just never fired.
	//
	// Clicking DOES produce the edge, because that request happens during the
	// widget's own interaction. Depending on the edge flags therefore gives a
	// widget that works when a user clicks and silently misses every
	// code-driven focus change, which is the worse half to get wrong.
	if flags.HasFocus() && inst.lastFocused != which {
		inst.log("panel " + name + ": gained focus")
		inst.lastFocused = which
	}
	if !flags.HasFocus() && inst.lastFocused == which {
		inst.log("panel " + name + ": lost focus")
		inst.lastFocused = focusNone
	}
	// Every key this panel captures is one it CONSUMES: while a panel has
	// focus, ↑/↓ move nothing in the surrounding gallery scroll area, because
	// the event never reaches it (SD2). Click away and scrolling returns.
	for _, k := range c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(id)) {
		inst.log("panel " + name + ": " + describeKey(k))
	}

	f = f.Fill(color.Hex(styletokens.NeutralBgFaint.AsHex())).
		Stroke(strokeW, stroke).
		InnerMargin(styletokens.PaddingOuter(styletokens.DensityFromEnv())).
		HoverCursorPointer()
	if captures {
		// CaptureKeys implies Focusable — capture is gated on has_focus, so a
		// mask on a widget egui cannot focus would never fire.
		f = f.CaptureKeys(uint64(keycodes.Navigation))
	} else {
		f = f.Focusable()
	}
	for range f.KeepIter() {
		c.UiSetMinWidth(260)
		for rt := range c.RichTextLabel("panel " + name) {
			rt.Strong()
		}
		if captures {
			c.Label("captures " + icons.PhArrowsOutCardinal + " nav keys").Send()
		} else {
			c.Label("no capture mask").Send()
		}
		c.Label(fmt.Sprintf("hasFocus=%v", flags.HasFocus())).Send()
		c.Label(fmt.Sprintf("gained=%v lost=%v",
			flags.HasGainedFocus(), flags.HasLostFocus())).Send()
	}
}
