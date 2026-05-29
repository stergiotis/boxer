//go:build llm_generated_opus47

package inspector

import (
	"math"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// AnchorToggle is the canonical "open this inspector in a pinned window"
// affordance. Renders a compact Phosphor `arrow-square-out` glyph
// (`icons.PhArrowSquareOut`) inside a Frame whose fill switches between
// transparent (idle) and [styletokens.AccentSubtle] (pinned). The icon
// itself stays in [styletokens.AccentDefault] across both states — the
// same hue the bezier-connector overlay uses for its curve — so toggle
// and tether read as one continuous system at the glass layer.
//
// State ownership is on the caller: pass a stable `*bool` and the toggle
// flips it on click. Returns true on the frame the toggle was clicked so
// callers can wire side effects (focus the spawned window, request a
// repaint, log telemetry) without re-reading the bool. nil `pinned` is a
// no-op — the toggle renders nothing and returns false; callers that
// don't want a pinning affordance simply don't call this function.
//
// The popup itself is also caller-owned. When `*pinned` is true the
// caller renders a `c.Window(...)` containing the inspector's level-2
// body somewhere in the frame; the toggle does not spawn the window
// because different inspectors want different popup shapes (fsmview's
// tabbed window, distsummary's letter-value plot, fieldview's tree).
// Forcing one callback shape would either underspecify the popup or
// drag every inspector through one body contract.
//
// Click capture uses the badge pattern (`widgets/badge/badge.go:268-303`):
// `c.Frame(id).SenseClick()` + previous-frame response via
// [c.CurrentApplicationState.StateManager.GetResponseByIdRaw]. One-frame
// lag, same as every interactive widget in the system.
//
// ADR-0046 §Updates 2026-05-25 records the design pivot that retired the
// unicode `AnchorChevron` in favour of this widget.
func AnchorToggle(ids c.WidgetIdCreatorI, pinned *bool) (clicked bool) {
	if pinned == nil {
		return false
	}

	accent := color.Hex(styletokens.AccentDefault.AsHex())
	transparentBg := color.Transparent
	fill := transparentBg
	if *pinned {
		fill = color.Hex(styletokens.AccentSubtle.AsHex())
	}

	atoms := c.Atoms().
		BeginRichTextColored(accent, transparentBg, icons.PhArrowSquareOut).
		End().Keep()

	f := c.Frame(ids).
		Fill(fill).
		CornerRadius(4).
		InnerMarginSides(4, 4, 1, 1).
		SenseClick().
		HoverCursorPointer()
	frameId := f.Id()
	// Tooltip text is state-aware so the affordance reads as a real
	// toggle: hovering tells the user what the click will do given the
	// current state. The HoverText scope wraps the Frame so the popup
	// anchors to the toggle's full bbox, not just the glyph itself.
	var tooltip string
	if *pinned {
		tooltip = "close inspector window"
	} else {
		tooltip = "open inspector window"
	}
	for range c.HoverText(tooltip).KeepIter() {
		for range f.KeepIter() {
			c.LabelAtoms(atoms).Send()
		}
	}

	resp := c.CurrentApplicationState.StateManager.GetResponseByIdRaw(frameId)
	if resp.HasPrimaryClicked() {
		*pinned = !*pinned
		clicked = true
	}
	return
}

// AnchorTether is the shared bezier-connector infrastructure tying an
// [AnchorToggle] to its pinned inspector window. Construct once per
// inspector instance via [NewAnchorTether] with the same scope string
// the inspector uses elsewhere for its absolute widget ids; the tether
// derives independent R21 rect-capture seqs so multiple inspectors on
// the same screen render independent bezier curves without colliding.
//
// Wire-in is three calls per pinned-window cycle:
//
//  1. After emitting [AnchorToggle] (inside the Horizontal that holds
//     the level-1 row), call [AnchorTether.CaptureToggle] so the
//     cumulative ui min_rect pins the bezier "from" endpoint at the
//     toggle's right edge.
//  2. At the TOP of the inspector's pinned-window body, before any
//     content, call [AnchorTether.CaptureWindow] so ui.min_rect()
//     reflects the window's content area (title bar excluded). Capturing
//     after content emission shrinks the rect to the body bbox and the
//     "to" endpoint drifts inward as content changes (PoC lesson —
//     `egui2_hl_bezier_connector_demo.go:134-135`).
//  3. After the window body has emitted, call [AnchorTether.Paint] to
//     read previous-frame rects, compute endpoints, and emit the bezier
//     curve through [c.PaintAbsoluteOverlay]. Caller must gate on
//     *pinned themselves — Paint draws unconditionally when both rects
//     are present.
//
// Endpoints, tangent-length heuristic, accent colour, and overlay
// routing mirror the proof-of-concept in
// `demo/apps/widgets/egui2_hl_bezier_connector_demo.go` so every value
// inspector that opts in joins one shared visual vocabulary.
//
// One-frame lag: the first frame the window opens has no captured
// rects yet; Paint short-circuits and the bezier appears on frame 2.
//
// Limitation: endpoints assume the window sits to the right of the
// toggle. When the user drags the window above / below / left, the
// curve still emerges from the toggle's right and enters the window's
// content-left, which can look awkward; a future refinement will pick
// the side closest to the geometric midpoint instead of hard-coding
// right→left.
type AnchorTether struct {
	toggleSeq uint64
	windowSeq uint64
}

// NewAnchorTether constructs a tether scoped by the given string —
// typically the same idPrefix / scopeKey the inspector uses to derive
// its absolute widget ids. The scope is hashed independently for the
// toggle and window R21 seqs so the same scope across runs deterministic-
// ally addresses the same capture slots; distinct scopes across
// inspectors give independent slots.
func NewAnchorTether(scope string) AnchorTether {
	return AnchorTether{
		toggleSeq: uint64(c.MakeAbsoluteIdStr(scope + "-anchor-tether-toggle-rect")),
		windowSeq: uint64(c.MakeAbsoluteIdStr(scope + "-anchor-tether-window-rect")),
	}
}

// tetherSpringState carries the spring simulation for one tether's
// two interior bezier control points (p1 near the toggle, p2 near
// the window). p0 and p3 stay glued to the toggle / window edges
// each frame — only the rope's belly springs.
//
// Lives in [tetherSpringStates] keyed by the tether's toggleSeq so
// the same scope across frames addresses the same slot — the
// [AnchorTether] value itself stays immutable, mirroring how the R21
// rect captures persist in [c.StateManager].
//
// Position and velocity are in pixel and pixel/sec units; the simulation
// uses unit mass so the spring force is `-k * (pos - target)` and the
// damping force is `-c * vel`. Integrated with semi-implicit Euler each
// frame: `vel += acc * dt; pos += vel * dt`.
type tetherSpringState struct {
	// Spring-tracked positions of the two interior bezier control points.
	c1x, c1y float32
	c2x, c2y float32
	// Velocities for the same.
	v1x, v1y float32
	v2x, v2y float32
	// Wall-clock stamp of the previous Paint, used to derive dt. Zero
	// when uninitialized; the snap path seeds it on the first valid
	// paint.
	lastPaintNanos int64
	initialized    bool
}

var tetherSpringStates sync.Map // map[uint64 toggleSeq]*tetherSpringState

func getTetherSpringState(key uint64) *tetherSpringState {
	actual, _ := tetherSpringStates.LoadOrStore(key, &tetherSpringState{})
	return actual.(*tetherSpringState)
}

const (
	// tetherSpringK is the spring stiffness in 1/sec². Natural angular
	// frequency ω = sqrt(k); with k=200, ω ≈ 14.1 rad/s and the
	// undamped period ≈ 445 ms. Pair with [tetherSpringC] below.
	tetherSpringK float32 = 200.0

	// tetherSpringC is the damping coefficient in 1/sec. The damping
	// ratio is ζ = c / (2 * sqrt(k)) — with k=200, c=11 gives ζ ≈ 0.39,
	// clearly underdamped. First overshoot ≈ 25 % of the displacement,
	// 2–3 visible oscillation cycles, settles in ≈ 1.0 sec. Tunable
	// dial for the "rubbery" feel: lower c → bouncier, higher c → less
	// overshoot. Stability requires `ω * dt < 2`; at 60 Hz dt=16.7 ms
	// our margin is ω·dt ≈ 0.24, with another order of magnitude before
	// integration would explode.
	tetherSpringC float32 = 11.0

	// tetherSpringDtMaxSecs caps the integration step. Continuous
	// rendering keeps dt near 1/60 in normal operation, but a debugger
	// pause or a stalled compositor can produce a larger gap; without
	// this cap the spring would take one giant step and visibly snap
	// or oscillate wildly.
	tetherSpringDtMaxSecs float32 = 0.033

	// tetherSpringPauseGap is the wall-clock gap past which we treat
	// the previous frame as a *session resume* rather than a frame
	// step — typical cause is the inspector being closed for some time
	// while the toggle / window moved, so the stored velocity is
	// meaningless. Triggers the same snap path as a large positional
	// delta.
	tetherSpringPauseGap = 100 * time.Millisecond

	// tetherSnapDistancePx is the positional threshold past which we
	// treat the target jump as a teleport (panel reflow, large window
	// reposition) and snap the control point to its target with
	// zeroed velocity rather than letting the spring catch up. 200 px
	// is wide enough to swallow any reasonable single-frame drag (a
	// fast mouse moves ~100 px/frame at 60 Hz) and narrow enough to
	// catch screen-scale teleports.
	tetherSnapDistancePx float32 = 200.0
)

// integrateSpring advances one axis of one control point by `dt`
// seconds. Mass is unit so force == acceleration. Semi-implicit Euler:
// the new velocity feeds the position update in the same step, which is
// stable for the (ω, dt) regime we operate in (ω·dt < 2).
func integrateSpring(pos, vel *float32, target, dt float32) {
	acc := -tetherSpringK*(*pos-target) - tetherSpringC*(*vel)
	*vel += acc * dt
	*pos += *vel * dt
}

// CaptureToggle stamps the current ui.min_rect() into the R21 toggle
// slot. Call inside the Horizontal that holds the level-1 row,
// immediately after [AnchorToggle] (so the cumulative row bbox extends
// through the toggle's right edge).
func (t AnchorTether) CaptureToggle() {
	c.CaptureUiRect(t.toggleSeq)
}

// CaptureWindow stamps the current ui.min_rect() into the R21 window
// slot. Call at the TOP of the inspector's pinned-window body, before
// any content emits; ui.min_rect() at that point is the window's
// content area (title bar + frame margins excluded).
func (t AnchorTether) CaptureWindow() {
	c.CaptureUiRect(t.windowSeq)
}

// Paint reads both previous-frame R21 rects and, when both are
// available, emits the cubic-bezier curve plus endpoint dots and
// drains through [c.PaintAbsoluteOverlay] so the curve crosses
// egui::Window boundaries. No-op when either capture is missing
// (first-frame open, or window not yet rendered this session). Caller
// is responsible for gating on *pinned — Paint itself paints whenever
// both rects exist.
//
// The two interior bezier control points (p1, p2) are driven by a
// per-tether mass-spring-damper toward the geometric S-curve targets;
// p0 and p3 (and the endpoint dots) stay glued to the toggle and
// window edges. Drag the window and the rope's belly trails, then
// snaps back with a couple of overshoots — see [tetherSpringK] /
// [tetherSpringC] for the underdamped tuning and stability bounds.
// Continuous rendering ([project_imzero2_continuous_rendering])
// guarantees Paint is invoked every frame so the integration
// progresses smoothly.
//
// Snap-path: first frame, session resume (gap > [tetherSpringPauseGap]),
// and teleport-scale target jumps (> [tetherSnapDistancePx]) bypass the
// spring and snap the control points to their targets with zeroed
// velocity. Otherwise the simulation would either sweep visibly from
// (0, 0) on first open, integrate one huge step on resume, or animate
// across the screen on a panel reflow.
func (t AnchorTether) Paint() {
	sm := c.CurrentApplicationState.StateManager
	toggleRect, toggleOk := sm.GetUiRect(t.toggleSeq)
	windowRect, windowOk := sm.GetUiRect(t.windowSeq)
	if !toggleOk || !windowOk {
		return
	}
	fromX := toggleRect.MaxX + 4
	fromY := (toggleRect.MinY + toggleRect.MaxY) / 2
	toX := windowRect.MinX - 4
	toY := (windowRect.MinY + windowRect.MaxY) / 2

	// S-curve tangent scales with the toggle→window gap so short
	// connectors don't kink and long ones don't go flat. Clamped to a
	// sane range; 0.45×gap is the bezier-aesthetics sweet spot most
	// node editors land on.
	dx := toX - fromX
	if dx < 0 {
		dx = -dx
	}
	bezTangent := dx * 0.45
	if bezTangent < 90 {
		bezTangent = 90
	}
	if bezTangent > 260 {
		bezTangent = 260
	}

	// Spring targets — the geometric "ideal" S-curve control points
	// that the spring tries to settle on each frame.
	tgt1x := fromX + bezTangent
	tgt1y := fromY
	tgt2x := toX - bezTangent
	tgt2y := toY

	st := getTetherSpringState(t.toggleSeq)
	now := time.Now().UnixNano()
	sinceLast := time.Duration(now - st.lastPaintNanos)
	st.lastPaintNanos = now

	dist1 := float32(math.Hypot(float64(st.c1x-tgt1x), float64(st.c1y-tgt1y)))
	dist2 := float32(math.Hypot(float64(st.c2x-tgt2x), float64(st.c2y-tgt2y)))
	snap := !st.initialized ||
		sinceLast > tetherSpringPauseGap ||
		dist1 > tetherSnapDistancePx ||
		dist2 > tetherSnapDistancePx
	if snap {
		st.c1x, st.c1y = tgt1x, tgt1y
		st.c2x, st.c2y = tgt2x, tgt2y
		st.v1x, st.v1y = 0, 0
		st.v2x, st.v2y = 0, 0
		st.initialized = true
	} else {
		dt := float32(sinceLast.Seconds())
		if dt > tetherSpringDtMaxSecs {
			dt = tetherSpringDtMaxSecs
		}
		integrateSpring(&st.c1x, &st.v1x, tgt1x, dt)
		integrateSpring(&st.c1y, &st.v1y, tgt1y, dt)
		integrateSpring(&st.c2x, &st.v2x, tgt2x, dt)
		integrateSpring(&st.c2y, &st.v2y, tgt2y, dt)
	}

	// Honour the global reduced-motion preference: when motion is off
	// (tour conformance, OS pref), bypass the spring entirely so the
	// curve renders at its instantaneous geometric form. Doing this
	// *after* the integration above also keeps the simulation state
	// in sync — when motion is re-enabled later the spring resumes
	// from the displayed pose rather than a stale lagged one.
	if !styletokens.MotionEnabled() {
		st.c1x, st.c1y = tgt1x, tgt1y
		st.c2x, st.c2y = tgt2x, tgt2y
		st.v1x, st.v1y = 0, 0
		st.v2x, st.v2y = 0, 0
	}

	accent := color.Hex(styletokens.AccentDefault.AsHex())
	c.PaintCubicBezier(
		fromX, fromY,
		st.c1x, st.c1y,
		st.c2x, st.c2y,
		toX, toY,
		accent, 1.75,
	).Send()
	c.PaintCircleFilled(fromX, fromY, 3.5, accent).Send()
	c.PaintCircleFilled(toX, toY, 3.5, accent).Send()
	c.PaintAbsoluteOverlay()
}
