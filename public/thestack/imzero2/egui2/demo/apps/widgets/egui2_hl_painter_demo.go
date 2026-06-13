package widgets

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// painterDemoState carries the per-window animation counter for the
// clock sub-demo. Two open gallery windows tick their clocks
// independently from each other.
type painterDemoState struct {
	clockFrame uint64
}

func init() {
	registry.Register(registry.Demo{
		Name:        "painter",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " painter (shapes / clock / bezier)",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindMixed,
		Description: "Direct shape drawing on a Painter: primitives, an animated analog clock and parametric Bézier curves.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &painterDemoState{}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*painterDemoState)
			for range c.CollapsingHeader(ids.PrepareStr("painter-shapes-demo"), c.WidgetText().Text("shapes (circles, rects, lines, text)").Keep()).DefaultOpen(true).KeepIter() {
				demoPainterShapes(ids)
			}
			for range c.CollapsingHeader(ids.PrepareStr("painter-clock-demo"), c.WidgetText().Text("clock (animated)").Keep()).KeepIter() {
				demoPainterClock(ids, st)
			}
			for range c.CollapsingHeader(ids.PrepareStr("painter-bezier-demo"), c.WidgetText().Text("bezier curves (cubic)").Keep()).KeepIter() {
				demoPainterBezier(ids)
			}
		},
	})
}

// =============================================================================
// DEMO: Custom painting — shapes, text, arrows on a canvas
// =============================================================================

func demoPainterShapes(ids *c.WidgetIdStack) {
	// SVG export trigger — exercises the new exportSvg FFFI opcode end-to-
	// end: button click queues the path into ImZeroFffi::export_state, then
	// SvgExportPlugin::on_end_pass walks ctx.graphics() and writes the file
	// before tessellation. Open /tmp/painter_demo.svg in a browser.
	r := c.Button(ids.PrepareStr("export-svg"),
		c.Atoms().Text("Export SVG").Keep()).SendResp()
	if r.HasPrimaryClicked() {
		// Light-weight by default; flip embedFonts to true if pixel-faithful
		// font rendering is required (adds ~30-80KB per used face).
		// bgRgba=0x1e1e1eff keeps the dark VIEWPORT_BG baseline rect
		// behind the painted shapes; pass 0 to omit the baseline and
		// let the host (browser, viewer) show through.
		c.ExportSvg("/tmp/painter_demo.svg", false, 0x1e1e1eff)
	}

	// Background grid lines
	for ix := 0; ix < 10; ix++ {
		x := float32(ix) * 40.0
		c.PaintLine(x, 0.0, x, 300.0, color.Hex(0x33333344), 0.5).Send()
	}
	for iy := 0; iy < 8; iy++ {
		y := float32(iy) * 40.0
		c.PaintLine(0.0, y, 400.0, y, color.Hex(0x33333344), 0.5).Send()
	}

	// Filled circles
	c.PaintCircleFilled(80.0, 80.0, 30.0, color.Hex(0xff4444cc)).Send()
	c.PaintCircleFilled(160.0, 80.0, 20.0, color.Hex(0x44ff44cc)).Send()
	c.PaintCircleFilled(220.0, 80.0, 25.0, color.Hex(0x4444ffcc)).Send()

	// Stroked circle
	c.PaintCircleStroke(320.0, 80.0, 35.0, color.Hex(0xffaa00ff), 2.0).Send()

	// Filled rects
	c.PaintRectFilled(40.0, 150.0, 140.0, 200.0, 5.0, color.Hex(0xff6600cc)).Send()
	c.PaintRectFilled(160.0, 150.0, 260.0, 200.0, 0.0, color.Hex(0x0066ffcc)).Send()
	// Note: PaintRectStroke, PaintLine args use minX/minY/maxX/maxY and fromX/fromY/toX/toY names
	// but positionally they're the same as before

	// Stroked rect
	c.PaintRectStroke(280.0, 150.0, 380.0, 210.0, 8.0, color.Hex(0x00ff88ff), 2.0).Send()

	// Lines
	c.PaintLine(40.0, 240.0, 200.0, 280.0, color.Hex(0xffff00ff), 2.0).Send()
	c.PaintLine(200.0, 280.0, 360.0, 240.0, color.Hex(0xff00ffff), 2.0).Send()

	// Polyline (zigzag)
	zigzagXs := make([]float32, 10)
	zigzagYs := make([]float32, 10)
	for ip := 0; ip < 10; ip++ {
		zigzagXs[ip] = 40.0 + float32(ip)*35.0
		if ip%2 == 0 {
			zigzagYs[ip] = 220.0
		} else {
			zigzagYs[ip] = 240.0
		}
	}
	c.PaintPolyline(zigzagXs, zigzagYs, color.Hex(0x44ddffff), 1.5).Send()

	// Arrows
	c.PaintArrow(80.0, 260.0, 60.0, -30.0, color.Hex(0xffffffff), 1.5).Send()
	c.PaintArrow(300.0, 260.0, -40.0, 20.0, color.Hex(0xaaffaaff), 1.5).Send()

	// Dashed lines — same FFFI2 family as PaintLine but with explicit dash
	// and gap lengths. epaint::Shape::dashed_line decomposes Rust-side, so
	// the wire cost is one opcode per call regardless of dash count. Three
	// variants showing the dash/gap parameter space:
	//   short:    2/2  — fine perforation
	//   standard: 6/4  — readable mid-density (timeline annotation default)
	//   long:    12/6  — sparse, for emphasis lines
	c.PaintDashedLine(40.0, 310.0, 360.0, 310.0, 2.0, 2.0, color.Hex(0xffff00ff), 1.5).Send()
	c.PaintDashedLine(40.0, 330.0, 360.0, 330.0, 6.0, 4.0, color.Hex(0xff00ffff), 1.5).Send()
	c.PaintDashedLine(40.0, 350.0, 360.0, 350.0, 12.0, 6.0, color.Hex(0x44ddffff), 1.5).Send()

	// Text
	c.PaintText(200.0, 20.0, 1, 0, "Canvas Demo", 18.0, color.Hex(0xffffffff)).Send()
	c.PaintText(80.0, 120.0, 1, 0, "R", 12.0, color.Hex(0xffccccff)).Send()
	c.PaintText(160.0, 120.0, 1, 0, "G", 12.0, color.Hex(0xccffccff)).Send()
	c.PaintText(220.0, 120.0, 1, 0, "B", 12.0, color.Hex(0xccccffff)).Send()
	c.PaintText(40.0, 297.0, 0, 0, "dashed lines (dashLen / gapLen):", 10.0, color.Hex(0xaaaaaaff)).Monospace().Send()
	c.PaintText(370.0, 310.0, 0, 1, "2/2", 10.0, color.Hex(0xffff00cc)).Monospace().Send()
	c.PaintText(370.0, 330.0, 0, 1, "6/4", 10.0, color.Hex(0xff00ffcc)).Monospace().Send()
	c.PaintText(370.0, 350.0, 0, 1, "12/6", 10.0, color.Hex(0x44ddffcc)).Monospace().Send()
	c.PaintText(40.0, 372.0, 0, 0, "coordinates: 0.0, 0.0 = top-left", 11.0, color.Hex(0xaaaaaaff)).Monospace().Send()

	c.PaintCanvas(ids.PrepareStr("painter-shapes"), 400.0, 385.0).
		Background(color.Hex(0x1a1a1aff)).
		Send()
}

// =============================================================================
// DEMO: Animated clock face using painter
// =============================================================================

func demoPainterClock(ids *c.WidgetIdStack, st *painterDemoState) {
	st.clockFrame++
	cx := float32(100.0)
	cy := float32(100.0)
	radius := float32(80.0)

	// Clock face
	c.PaintCircleFilled(cx, cy, radius, color.Hex(0x222233ff)).Send()
	c.PaintCircleStroke(cx, cy, radius, color.Hex(0x8888aaff), 2.0).Send()

	// Hour marks
	for ih := 0; ih < 12; ih++ {
		angle := float64(ih) * math.Pi / 6.0
		innerR := float64(radius) * 0.85
		outerR := float64(radius) * 0.95
		x0 := float32(float64(cx) + math.Sin(angle)*innerR)
		y0 := float32(float64(cy) - math.Cos(angle)*innerR)
		x1 := float32(float64(cx) + math.Sin(angle)*outerR)
		y1 := float32(float64(cy) - math.Cos(angle)*outerR)
		c.PaintLine(x0, y0, x1, y1, color.Hex(0xaaaaccff), 1.5).Send()
	}

	// "Second hand" (rotates with frame count)
	secAngle := float64(st.clockFrame%360) * math.Pi / 180.0
	handLen := float64(radius) * 0.75
	hx := float32(float64(cx) + math.Sin(secAngle)*handLen)
	hy := float32(float64(cy) - math.Cos(secAngle)*handLen)
	c.PaintLine(cx, cy, hx, hy, color.Hex(0xff4444ff), 1.5).Send()

	// Center dot
	c.PaintCircleFilled(cx, cy, 3.0, color.Hex(0xffffffff)).Send()

	// Label
	c.PaintText(cx, cy+radius+12.0, 1, 0, "clock", 11.0, color.Hex(0xaaaaaaff)).Send()

	c.PaintCanvas(ids.PrepareStr("painter-clock"), 200.0, 210.0).
		Background(color.Hex(0x111118ff)).
		Send()
}

// =============================================================================
// DEMO: Cubic Bezier curves
// =============================================================================

func demoPainterBezier(ids *c.WidgetIdStack) {
	w := float32(400.0)
	h := float32(200.0)

	// S-curve
	c.PaintCubicBezier(30, 150, 130, 20, 270, 180, 370, 50, color.Hex(0x44ddffff), 2.0).Send()
	// control point markers + guide lines for the S-curve
	c.PaintCircleFilled(30, 150, 4, color.Hex(0x44ddffaa)).Send()
	c.PaintCircleFilled(370, 50, 4, color.Hex(0x44ddffaa)).Send()
	c.PaintCircleStroke(130, 20, 3, color.Hex(0x44ddff66), 1.0).Send()
	c.PaintCircleStroke(270, 180, 3, color.Hex(0x44ddff66), 1.0).Send()
	c.PaintLine(30, 150, 130, 20, color.Hex(0x44ddff33), 0.5).Send()
	c.PaintLine(270, 180, 370, 50, color.Hex(0x44ddff33), 0.5).Send()

	// Arch
	c.PaintCubicBezier(40, 180, 100, 30, 300, 30, 360, 180, color.Hex(0xff8844ff), 2.0).Send()

	// Swoop
	c.PaintCubicBezier(200, 20, 50, 190, 350, 190, 200, 20, color.Hex(0xaa44ffff), 1.5).Send()

	// Labels
	c.PaintText(w/2, 10, 1, 0, "Cubic Bezier Curves", 14.0, color.Hex(0xffffffff)).Send()
	c.PaintText(90, 95, 0, 1, "S-curve", 10.0, color.Hex(0x44ddffcc)).Send()
	c.PaintText(200, 55, 1, 0, "arch", 10.0, color.Hex(0xff8844cc)).Send()
	c.PaintText(200, 150, 1, 1, "swoop", 10.0, color.Hex(0xaa44ffcc)).Send()

	c.PaintCanvas(ids.PrepareStr("painter-bezier"), w, h).
		Background(color.Hex(0x1a1a1aff)).
		Send()
}
