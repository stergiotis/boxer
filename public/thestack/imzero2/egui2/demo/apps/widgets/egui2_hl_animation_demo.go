package widgets

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// =============================================================================
// Animation primitives demo
// Showcases AnimateBoolWithTime, AnimateBoolResponsive, AnimateValueWithTime
// and RequestRepaintAfter (the FFFI2 bindings around egui::Context::animate_*).
// =============================================================================

// Per-tween stable IDs. egui's AnimationManager keys from-values by these,
// so they must stay stable across frames for the same logical tween.
const (
	animIdBoolWithTime   uint64 = 0xa01a05001
	animIdBoolResponsive uint64 = 0xa01a05003
	animIdValueWithTime  uint64 = 0xa01a05005
)

// animationDemoState carries the per-window expanded flag, three
// tween values the FFFI2 binders write back into, and the scheduled
// blink target. Heap-allocated once at Init so the &st.X pointers
// handed to AnimateXxxBind stay stable across frames.
type animationDemoState struct {
	expanded bool
	t1       float64 // tween from AnimateBoolWithTime
	t2       float64 // tween from AnimateBoolResponsive
	t3       float64 // tween from AnimateValueWithTime
	blinkAt  time.Time
}

func init() {
	registry.Register(registry.Demo{
		Name:        "animation",
		Category:    "Layout & widgets",
		Title:       "animation primitives",
		Stage:       [2]float32{1024, 460},
		Kind:        registry.DemoKindDX,
		Description: "Animation primitives: ease-in/out curves and the in-frame animation-freeze knob used by the screenshot tour.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &animationDemoState{}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoAnimation(ids, state.(*animationDemoState))
		},
		SourceFunc: demoAnimation,
	})
}

func demoAnimation(ids *c.WidgetIdStack, st *animationDemoState) {
	// --- Toggle that drives all tweens ---
	toggleLabel := "Expand"
	if st.expanded {
		toggleLabel = "Collapse"
	}
	if c.Button(ids.PrepareSeq(0xa01a05100), c.Atoms().Text(toggleLabel).Keep()).
		SendResp().HasPrimaryClicked() {
		st.expanded = !st.expanded
	}

	// --- Drive the three primitives every frame.
	// The result is one frame behind (Sync writes back next frame), which is
	// imperceptible at 60fps but worth knowing for tight feedback loops.
	var targetVal float32
	if st.expanded {
		targetVal = 1.0
	}
	c.AnimateBoolWithTimeBind(animIdBoolWithTime, st.expanded, styletokens.MotionStandardSecs(), &st.t1)
	c.AnimateBoolResponsiveBind(animIdBoolResponsive, st.expanded, &st.t2)
	c.AnimateValueWithTimeBind(animIdValueWithTime, targetVal, styletokens.MotionSlowSecs(), &st.t3)

	c.AddSpace(padInner())
	c.Label(fmt.Sprintf("AnimateBoolWithTime  (MotionStandard, %dms):  t = %.3f", styletokens.MotionStandardMs, st.t1)).Send()
	c.Label(fmt.Sprintf("AnimateBoolResponsive                       :  t = %.3f", st.t2)).Send()
	c.Label(fmt.Sprintf("AnimateValueWithTime (MotionSlow, %dms):  t = %.3f", styletokens.MotionSlowMs, st.t3)).Send()

	// --- Visual: three boxes whose X position is driven by each tween value.
	// This is the use case relevant for treemap zoom: interpolate rects.
	c.AddSpace(gapInline())
	canvasW := float32(420.0)
	canvasH := float32(140.0)
	boxW := float32(60.0)
	boxH := float32(34.0)
	rowH := float32(40.0)
	margin := float32(8.0)
	leftX := margin
	rightX := canvasW - boxW - margin

	drawRow := func(y float32, t float64, label string, col color.Color) {
		// Track line
		c.PaintLine(leftX+boxW/2, y+boxH/2, rightX+boxW/2, y+boxH/2, color.Hex(0x33333388), 1.0).Send()
		// Box
		x := leftX + (rightX-leftX)*float32(t)
		c.PaintRectFilled(x, y, x+boxW, y+boxH, 5.0, col).Send()
		c.PaintText(canvasW-4, y+boxH/2, 2, 1, label, 10.0, color.Hex(0xccccccff)).Send()
	}
	// Migrated to IDS semantic palette — the three tracks map naturally
	// to info / success / warning tones (blue / green / yellow). Was
	// hardcoded 0x4488dd / 0x44dd88 / 0xdd8844.
	drawRow(8.0, st.t1, "WithTime", color.Hex(styletokens.InfoDefault.AsHex()))
	drawRow(8.0+rowH, st.t2, "Responsive", color.Hex(styletokens.SuccessDefault.AsHex()))
	drawRow(8.0+2*rowH, st.t3, "Value", color.Hex(styletokens.WarningDefault.AsHex()))
	c.PaintCanvas(ids.PrepareStr("anim-canvas"), canvasW, canvasH).
		Background(color.Hex(0x1a1a22ff)).
		Send()

	// --- RequestRepaintAfter: schedule a one-shot repaint without polling.
	c.AddSpace(gapItems())
	c.Separator().Send()
	c.Label("RequestRepaintAfter — schedule a future frame, no polling required").Send()
	if c.Button(ids.PrepareSeq(0xa01a05200), c.Atoms().Text("Blink in 1.0s").Keep()).
		SendResp().HasPrimaryClicked() {
		st.blinkAt = time.Now().Add(1 * time.Second)
		c.RequestRepaintAfter(1.0)
	}
	if !st.blinkAt.IsZero() {
		remaining := time.Until(st.blinkAt).Seconds()
		if remaining > 0 {
			c.Label(fmt.Sprintf("waiting %.2fs ...", remaining)).Send()
		} else {
			c.Label("BLINK").Send()
		}
	}
}
