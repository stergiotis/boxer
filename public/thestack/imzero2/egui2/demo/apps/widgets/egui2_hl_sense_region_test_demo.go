package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// senseRegionTestDemoState carries the per-window click-count array.
// The cell labels / colors / id-base constants stay package-level —
// they're immutable.
type senseRegionTestDemoState struct {
	clickCounts [srTestCols * srTestRows]int
}

func init() {
	registry.Register(registry.Demo{
		Name:        "sense-region-test",
		Category:    "Inspectors & feedback",
		Title:       "sense-region test",
		Stage:       [2]float32{1024, 460},
		Kind:        registry.DemoKindDX,
		Description: "Pointer / click event reporter for SenseRegion — useful when wiring custom hit-testing on PaintCanvas.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &senseRegionTestDemoState{}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoSenseRegionTest(ids, state.(*senseRegionTestDemoState))
		},
		SourceFunc: demoSenseRegionTest,
	})
}

// =============================================================================
// Interactive test: PaintSenseRegion per-region r7 responses.
//
// Each sense region registers a rect+id inside the canvas drain; the Rust
// side calls ui.interact(rect, id, click_and_drag) and pushes r7 flags keyed
// by that id. Hit-testing and click attribution happen entirely Rust-side.
// =============================================================================

const (
	srTestCols    = 3
	srTestRows    = 2
	srTestCellW   = float32(120)
	srTestCellH   = float32(80)
	srTestGap     = float32(8)
	srTestHeaderH = float32(100) // description text above the grid
	srTestCanvasW = float32(srTestCols)*srTestCellW + float32(srTestCols-1)*srTestGap
	srTestCanvasH = srTestHeaderH + float32(srTestRows)*srTestCellH + float32(srTestRows-1)*srTestGap
)

// Base ID for sense regions. Step by 2 between cells so the |1 that Derive
// applies cannot collapse adjacent ids to the same value.
const srTestIdBase uint64 = 0xbb00ff0000

var srTestCellColors = [srTestCols * srTestRows]uint32{
	0x5c6bc0ee, 0x42a5f5ee, 0x26c6daee,
	0x66bb6aee, 0xffa726ee, 0xef5350ee,
}

var srTestCellNames = [srTestCols * srTestRows]string{
	"A", "B", "C",
	"D", "E", "F",
}

func demoSenseRegionTest(ids *c.WidgetIdStack, st *senseRegionTestDemoState) {
	sm := c.CurrentApplicationState.StateManager

	// Description text painted into the canvas.
	descColor := uint32(0xccccddff)
	dimColor := uint32(0x888899ff)
	c.PaintText(4, 4, 0, 0, "PaintSenseRegion hit-test verification", 14.0, color.Hex(0xffffffff)).Send()
	c.PaintText(4, 24, 0, 0, "Expected: hover a cell -> white border + status below", 11.0, color.Hex(descColor)).Send()
	c.PaintText(4, 38, 0, 0, "Expected: left-click a cell -> click counter increments", 11.0, color.Hex(descColor)).Send()
	c.PaintText(4, 52, 0, 0, "Expected: right-click a cell -> SECONDARY_CLICKED in status", 11.0, color.Hex(descColor)).Send()
	c.PaintText(4, 66, 0, 0, "Expected: hover gap between cells -> no cell highlighted", 11.0, color.Hex(descColor)).Send()
	c.PaintText(4, 80, 0, 0, "Expected: click cell A -> only A counter increments, not B", 11.0, color.Hex(dimColor)).Send()

	c.PaintLine(4, srTestHeaderH-6, srTestCanvasW-4, srTestHeaderH-6, color.Hex(0x555566ff), 0.5).Send()

	// Phase 1: emit cells + sense regions. Previous-frame responses drive
	// the hover border so it renders into this frame's canvas.
	for row := 0; row < srTestRows; row++ {
		for col := 0; col < srTestCols; col++ {
			idx := row*srTestCols + col
			x := float32(col) * (srTestCellW + srTestGap)
			y := srTestHeaderH + float32(row)*(srTestCellH+srTestGap)

			c.PaintRectFilled(x, y, x+srTestCellW, y+srTestCellH, 6.0, color.Hex(srTestCellColors[idx])).Send()

			label := fmt.Sprintf("%s (%d)", srTestCellNames[idx], st.clickCounts[idx])
			c.PaintText(x+srTestCellW/2, y+srTestCellH/2, 1, 1, label, 14.0, color.Hex(0xffffffff)).Send()

			senseAbsId := c.MakeAbsoluteIdHighEntropy(srTestIdBase + uint64(idx)*2)
			c.PaintSenseRegion(senseAbsId, x, y, srTestCellW, srTestCellH).Send()

			if sm.GetResponse(widgethandle.Make(senseAbsId.Derive())).HasHovered() {
				c.PaintRectStroke(x-1, y-1, x+srTestCellW+1, y+srTestCellH+1, 6.0, color.Hex(0xffffffcc), 2.5).Send()
			}
		}
	}

	// Phase 2: drain into the canvas. Rust-side ui.interact on each sense
	// region populates r7 keyed by the region's id.
	c.PaintCanvas(ids.PrepareStr("sr-test-canvas"), srTestCanvasW, srTestCanvasH).
		Background(color.Hex(0x121218ff)).
		Send()

	// Phase 3: read previous-frame r7 flags and build status / counters.
	var statusParts []string
	for row := 0; row < srTestRows; row++ {
		for col := 0; col < srTestCols; col++ {
			idx := row*srTestCols + col
			senseAbsId := c.MakeAbsoluteIdHighEntropy(srTestIdBase + uint64(idx)*2)
			resp := sm.GetResponse(widgethandle.Make(senseAbsId.Derive()))

			if resp.HasPrimaryClicked() {
				st.clickCounts[idx]++
				statusParts = append(statusParts, fmt.Sprintf("%s: PRIMARY_CLICKED", srTestCellNames[idx]))
			}
			if resp.HasSecondaryClicked() {
				statusParts = append(statusParts, fmt.Sprintf("%s: SECONDARY_CLICKED", srTestCellNames[idx]))
			}
			if resp.HasHovered() {
				statusParts = append(statusParts, fmt.Sprintf("%s: HOVERED", srTestCellNames[idx]))
			}
		}
	}

	if len(statusParts) > 0 {
		status := ""
		for i, p := range statusParts {
			if i > 0 {
				status += "  |  "
			}
			status += p
		}
		c.Label(status).Send()
	} else {
		c.Label("Hover or click any cell").Send()
	}

	c.Label(fmt.Sprintf("clicks:  A=%d  B=%d  C=%d  D=%d  E=%d  F=%d",
		st.clickCounts[0], st.clickCounts[1], st.clickCounts[2],
		st.clickCounts[3], st.clickCounts[4], st.clickCounts[5])).Send()
}
