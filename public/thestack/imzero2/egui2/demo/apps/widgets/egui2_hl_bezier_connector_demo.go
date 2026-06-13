// Bezier-connector PoC for inspector ↔ source-value tethering (v2).
//
// v0 painted everything as primitives in a single canvas. v1 lifted the
// chips to real SelectableLabels and the inspector to a real egui::Window
// but the bezier didn't follow the window because of a coord-system
// mismatch: PaintCanvas paints in parent-Ui-relative coords while
// egui::Window lives at viewport-absolute coords, and the host carousel
// wraps the demo in its own c.Window so the relative origin isn't (0,0).
//
// v2 closes that gap with two new FFFI2 primitives (this is the first
// site that uses them):
//
//   - [c.CaptureUiRect](seq) — stamps the current ui.min_rect into the
//     interpreter's r21 vectors. Called once for the host Ui (seq=
//     [seqHost]) at the top of Render, and once for the window content
//     Ui (seq=[seqWindow]) inside the c.Window body. One-frame lag.
//
//   - [c.PaintAbsoluteOverlay]() — drains paint_cmds into an
//     Order::Foreground viewport-absolute painter whose clip is the full
//     screen_rect, so the curve renders ABOVE every window and is not
//     clipped by any host's content area.
//
// With those two, the bezier endpoint tracks the inspector window as the
// user drags it anywhere on screen (one-frame lag from the capture).
// Real chips, real window, no coord-system gymnastics required at the
// call site.

package widgets

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

type bezierConnectorSource struct {
	Subject string
	Label   string
}

var bezierConnectorSources = [3]bezierConnectorSource{
	{Subject: "app.play.event.fsm.machineFoo", Label: "fsm: foo"},
	{Subject: "app.spinnaker.event.dist.latencies", Label: "dist: latencies"},
	{Subject: "app.imztop.event.cpu.aggregate", Label: "cpu: agg"},
}

type bezierConnectorState struct {
	selected int32
}

const (
	bezierConnectorSeqSelChip uint64 = 0xBC0001
	bezierConnectorSeqWindow  uint64 = 0xBC0002
)

func init() {
	registry.Register(registry.Demo{
		Name:        "bezier-connector",
		Category:    "Graphics & canvas",
		Title:       "bezier connector (inspector ← source PoC)",
		Stage:       [2]float32{1024, 500},
		Kind:        registry.DemoKindMixed,
		Description: "PoC for inspector ↔ source tethering. Real SelectableLabel chips drive selection; the inspector is a real draggable egui::Window; the connector is a cubic bezier painted into an Order::Foreground absolute overlay so it tracks the window anywhere on screen.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &bezierConnectorState{selected: 1}
			return
		},
		RenderStateful: demoBezierConnector,
	})
}

func demoBezierConnector(ids *c.WidgetIdStack, anyState any) {
	st := anyState.(*bezierConnectorState)

	const (
		wndDefaultX float32 = 600
		wndDefaultY float32 = 170
		wndDefaultW float32 = 360
		wndDefaultH float32 = 240
	)

	selIdx := st.selected
	if selIdx < 0 || int(selIdx) >= len(bezierConnectorSources) {
		selIdx = 0
	}

	sm := c.CurrentApplicationState.StateManager
	selChipRect, selChipOk := sm.GetUiRect(bezierConnectorSeqSelChip)
	windowRect, windowOk := sm.GetUiRect(bezierConnectorSeqWindow)

	// Chip row in a Horizontal layout — natural per-label sizing means the
	// captured min_rect's MaxX is the actual right edge of the selected
	// chip. The row flows naturally in the parent Ui (below the host
	// carousel's RenderDemoIntro) instead of being pinned to absolute
	// parent-relative coords — pinning would overlap the intro text.
	// We capture AFTER the selected chip renders so the Horizontal Ui's
	// cumulative min_rect ends exactly at that chip; MinY/MaxY then
	// bracket the chip row's visual vertical bounds tightly.
	for range c.Horizontal().KeepIter() {
		for i, src := range bezierConnectorSources {
			if c.SelectableLabel(
				ids.PrepareStr(fmt.Sprintf("chip-%d", i)),
				st.selected == int32(i),
				src.Label,
			).SendResp().HasPrimaryClicked() {
				st.selected = int32(i)
			}
			if i == int(selIdx) {
				c.CaptureUiRect(bezierConnectorSeqSelChip)
			}
		}
	}

	for range c.Window(
		ids.PrepareStr("inspector-window"),
		c.WidgetText().Text("inspector").Keep(),
	).
		DefaultOpen(true).
		DefaultPos(wndDefaultX, wndDefaultY).
		DefaultSize(wndDefaultW, wndDefaultH).
		Resizable(true).
		Collapsible(false).
		KeepIter() {
		// Capture FIRST inside the body, before adding labels: ui.min_rect()
		// at this point is the window's content rect (title bar excluded);
		// capturing after labels would shrink min_rect to the labels' bbox
		// and the bezier endpoint would drift inward as content changed.
		c.CaptureUiRect(bezierConnectorSeqWindow)
		active := bezierConnectorSources[selIdx]
		// Standard inspector-header chip — the canonical "what am I
		// looking at" affordance every value inspector renders. The
		// chip pairs with the bezier overlay above: the curve shows
		// where, the chip says what. SampledAt = time.Now() is the
		// demo cheat (no real bus subscription); real inspectors will
		// thread the actual sample time through.
		inspector.ProvenanceChip(inspector.Provenance{
			Subject:   active.Subject,
			SampledAt: time.Now(),
		})
		c.Separator().Send()
		c.Label("(placeholder inspector body)").Send()
		c.Label("value: …").Send()
		c.Label("schema: …").Send()
	}

	if !selChipOk || !windowOk {
		return
	}

	fromX := selChipRect.MaxX + 6
	fromY := (selChipRect.MinY + selChipRect.MaxY) / 2
	toX := windowRect.MinX - 6
	toY := (windowRect.MinY + windowRect.MaxY) / 2

	// S-curve tangent length scales with the chip→window gap so short
	// connectors don't kink and long connectors don't go flat. Clamped
	// to a sane range; 0.45×gap is the Bezier-aesthetics sweet spot most
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

	// IDS tokens (ADR-0031): AccentDefault is the canonical accent role
	// for affordances drawn over the neutral surface; NeutralTextSecondary
	// is the muted-text role (same token the inspector chip itself uses
	// for its trailers, so the curve + caption + chip read as one
	// visual system).
	accent := color.Hex(styletokens.AccentDefault.AsHex())
	mutedTxt := color.Hex(styletokens.NeutralTextSecondary.AsHex())

	c.PaintCubicBezier(
		fromX, fromY,
		fromX+bezTangent, fromY,
		toX-bezTangent, toY,
		toX, toY,
		accent, 1.75,
	).Send()
	c.PaintCircleFilled(fromX, fromY, 3.5, accent).Send()
	c.PaintCircleFilled(toX, toY, 3.5, accent).Send()

	midX := (fromX + toX) / 2
	midY := (fromY+toY)/2 - 12
	c.PaintText(midX, midY, 1, 1,
		"inspector ← source", 10.0, mutedTxt).Monospace().Send()

	c.PaintAbsoluteOverlay()
}
