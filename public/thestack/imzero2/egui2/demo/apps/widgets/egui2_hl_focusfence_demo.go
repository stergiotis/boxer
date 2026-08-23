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
		Name:     "focus fencing",
		Category: "Layout & widgets",
		Title:    icons.PhSelectionSlash + " focus fencing",
		Stage:    [2]float32{900, 560},
		Kind:     registry.DemoKindDX,
		Description: "ADR-0177 M2: a capturing widget inside a ScrollArea, and the same widget " +
			"without a mask, so the fence can be measured rather than asserted. Focus either " +
			"panel and press the arrow keys: the readout reports whether the panel kept focus " +
			"and whether the ScrollArea moved under it.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &fenceDemoState{}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoFocusFence(ids, state.(*fenceDemoState))
		},
		SourceFunc: demoFocusFence,
	})
}

// =============================================================================
// focus fencing — ADR-0177 M2.
//
// M2 exists to verify SD2's fencing claim, and verifying it first meant
// checking what the claim actually describes. SD2 says a capturing widget stops
// "an enclosing ScrollArea also scrolling on ↑/↓/PageUp/PageDown". egui 0.35's
// `scroll_area.rs` contains no key handling at all — a plain ScrollArea never
// scrolls on arrows, so that specific collision cannot occur and could not have
// been observed.
//
// What DOES happen on an unmodified arrow is focus navigation: `Focus::begin_pass`
// latches a direction from the raw input and `end_pass` moves focus to the
// neighbouring widget. So the thing a capturing widget must fence off is the
// framework taking its focus away, not a parent stealing its scroll.
//
// Rather than assume which of the two is visible, this demo measures both:
//
//   - `focus kept` — did the panel still have focus after the key?
//   - `scrollY` — did the ScrollArea's content move under it?
//
// The two panels differ in exactly one thing (the mask), so the difference
// between their readouts is the fence. A reader who distrusts the ADR's wording
// can drive the demo and believe the numbers instead.
// =============================================================================

type fenceDemoState struct {
	// handles are the ids the two panels' Frames put on the wire, captured at
	// render (FrameFluid.Id(), not the id passed in — c.Frame re-derives).
	handles map[int]uint64
	// keyCounts is how many keys each panel has captured, so a reader can tell
	// "captured and ignored" from "never captured".
	keyCounts map[int]int
	// lastKey is the most recent captured event, for the readout.
	lastKey string
	// wantFocus is a one-shot focus request from the buttons, consumed after
	// the panels render. Buttons rather than clicking a panel directly: a
	// pointer click needs the panel's screen position, and inside a bounded
	// ScrollArea nested in the scrollable gallery that position depends on two
	// scroll offsets. A button at the top level is reachable by name, so the
	// test drives focus without depending on where anything landed.
	wantFocus int
	// baselineY is the marker's offset from the outside reference at its
	// unscrolled position. scrollY is measured against it because the bindings
	// expose no scroll-offset readback — the marker's own movement IS the
	// scroll, read through the R21 ui-rect register.
	baselineY    float32
	haveBaseline bool
}

const (
	fenceOpen    = 1 // no capture mask: arrows move focus away
	fenceCapture = 2 // captures the navigation keys
)

// fenceMarkerSeq is the R21 sequence the scroll marker's rect is captured
// under. A constant rather than a per-frame counter: the register is keyed by
// seq, and a moving key would leave the previous frame's row unread forever.
const (
	fenceMarkerSeq = 0xA0177
	// fenceOuterSeq anchors the same measurement outside the ScrollArea.
	fenceOuterSeq = 0xA0178
)

func demoFocusFence(ids *c.WidgetIdStack, st *fenceDemoState) {
	if st.handles == nil {
		st.handles = make(map[int]uint64, 2)
		st.keyCounts = make(map[int]int, 2)
	}

	stdSection("a capturing widget inside a ScrollArea",
		"focus a panel, press ↑/↓ — the readout below reports focus and scroll separately")

	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("fence-focus-cap"),
			c.Atoms().Text("focus capturing panel").Keep()).SendResp().HasPrimaryClicked() {
			st.wantFocus = fenceCapture
		}
		c.AddSpace(gapInline())
		if c.Button(ids.PrepareStr("fence-focus-open"),
			c.Atoms().Text("focus open panel").Keep()).SendResp().HasPrimaryClicked() {
			st.wantFocus = fenceOpen
		}
	}
	c.AddSpace(padInner())

	// Reference point OUTSIDE the ScrollArea. The scroll readout is the marker
	// measured RELATIVE to this, not in screen coordinates: this demo sits in
	// the gallery, which is itself scrollable, so an absolute reading would
	// move when the gallery scrolled and report the outer scroll as if it were
	// the inner one. Both points move together under an outer scroll, so the
	// difference cancels it and isolates the ScrollArea under test.
	c.CaptureUiAvailableRect(fenceOuterSeq)

	// A bounded ScrollArea with more content than fits, so it CAN scroll. If it
	// could not, "the parent did not scroll" would be true for uninteresting
	// reasons and the test would prove nothing.
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		c.UiSetMaxHeight(240)

		// The scroll probe. Its rect is captured every frame; when the area
		// scrolls, this moves with the content and the delta is the offset.
		c.CaptureUiAvailableRect(fenceMarkerSeq)
		for rt := range c.RichTextLabel("— top of scrolled content —") {
			rt.Weak().Small()
		}

		// Capturing first, open second: both must sit ABOVE the fold of the
		// 240pt viewport, because a trace that has to scroll a panel into view
		// before clicking it has already moved the very thing being measured.
		st.fencePanel(ids, "capturing", fenceCapture, true)
		st.fencePanel(ids, "open", fenceOpen, false)

		// Focusable siblings below, so arrow-key focus navigation has
		// somewhere to go. Without them egui would have no neighbour to move
		// to and the open panel would appear to fence as well as the
		// capturing one — a false pass.
		for i := range 12 {
			c.Button(ids.PrepareStr(fmt.Sprintf("fence-filler-%d", i)),
				c.Atoms().Text(fmt.Sprintf("focusable sibling %d", i)).Keep()).Send()
		}
	}

	// Consumed after the panels rendered, so the ids they registered exist this
	// frame. One-shot: re-requesting every frame would pin focus and the arrow
	// keys could never demonstrate taking it away.
	if h := st.handles[st.wantFocus]; h != 0 {
		c.RequestFocus(h)
	}
	st.wantFocus = 0

	c.AddSpace(padInner())
	stdSection("what actually happened", "focus and scroll are reported separately, because they are separate")

	mark, haveMark := c.CurrentApplicationState.StateManager.GetUiRect(fenceMarkerSeq)
	outer, haveOuter := c.CurrentApplicationState.StateManager.GetUiRect(fenceOuterSeq)
	if haveMark && haveOuter {
		rel := mark.MinY - outer.MinY
		// The baseline is the HIGHEST value ever seen, not the first one.
		// Scrolling down only moves the marker up, so the maximum IS the
		// unscrolled position by construction — whereas taking the first sample
		// made the readout start at -2, because the layout settles by a couple
		// of points after the first frame and the probe caught it mid-settle.
		if !st.haveBaseline || rel > st.baselineY {
			st.baselineY = rel
			st.haveBaseline = true
		}
		// Positive = content moved up, i.e. scrolled down.
		c.Label(fmt.Sprintf("scrollY=%.0f", st.baselineY-rel)).Send()
	} else {
		c.Label("scrollY=?").Send() // designlint:ignore=L1 (no-baseline-yet state of the scrollY=%.0f readout above, same key=value diagnostic style)
	}
	c.Label(fmt.Sprintf("openKeys=%d capturingKeys=%d",
		st.keyCounts[fenceOpen], st.keyCounts[fenceCapture])).Send()
	if st.lastKey != "" {
		c.Label("lastKey=" + st.lastKey).Send()
	}
}

func (inst *fenceDemoState) fencePanel(ids *c.WidgetIdStack, name string, which int, captures bool) {
	f := c.Frame(ids.PrepareStr("fence-" + name))
	id := f.Id()
	inst.handles[which] = id

	flags := c.CurrentApplicationState.StateManager.GetResponseByIdRaw(id)
	for _, k := range c.CurrentApplicationState.StateManager.GetCapturedKeys(widgethandle.Make(id)) {
		inst.keyCounts[which]++
		inst.lastKey = name + ":" + describeKey(k)
	}

	stroke, strokeW := color.Transparent, float32(0)
	if flags.HasFocus() {
		stroke, strokeW = color.Hex(styletokens.AccentDefault.AsHex()), 2.0
	}
	f = f.Fill(color.Hex(styletokens.NeutralBgFaint.AsHex())).
		Stroke(strokeW, stroke).
		InnerMargin(styletokens.PaddingOuter(styletokens.ActiveDensity())).
		HoverCursorPointer()
	if captures {
		f = f.CaptureKeys(uint64(keycodes.Navigation))
	} else {
		f = f.Focusable()
	}
	for range f.KeepIter() {
		c.UiSetMinWidth(320)
		for rt := range c.RichTextLabel("panel " + name) {
			rt.Strong()
		}
		// The readout a trace asserts on. "kept" is the level, not an edge:
		// after an arrow key, a fenced panel still has focus and an open one
		// has handed it to a sibling.
		c.Label(fmt.Sprintf("%s focus=%v", name, flags.HasFocus())).Send()
	}
}
