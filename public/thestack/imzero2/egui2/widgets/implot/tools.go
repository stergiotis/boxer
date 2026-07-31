package implot

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// toolKind discriminates the M5 tool declarations.
type toolKind uint8

const (
	toolDragLineX toolKind = iota
	toolDragLineY
	toolDragPoint
	toolAnnotation
	toolTagX
	toolTagY
)

// toolFrame records one tool declaration for End's render pass, with the
// interaction state already resolved at declaration time (the ImPlot
// contract: Drag* return this frame's result and mutate the caller-held
// value in place).
type toolFrame struct {
	kind       toolKind
	key        string
	x, y       float64
	colHex     uint32
	text       string
	dxPx, dyPx float32
	clamp      bool
	active     bool // hovered or dragging — rendered emphasized
}

// grabPx is the half-width of a drag tool's invisible grab strip.
const grabPx = 5.0

func (p *Plot) toolHandle(key string) widgethandle.WidgetHandle {
	return widgethandle.Make(p.ids.PrepareStr("tool-" + key).Derive())
}

// DragLineX declares a draggable vertical line at *x. While the user drags
// its grab strip the value updates through the pointer (one-frame lag,
// like every gesture); the return reports that a drag moved it this frame.
// The pointer must be stable across frames (the usual heap-pointer rule).
// The tool's sense region sits above the plot-area region, so dragging a
// line never pans the plot.
func (p *Plot) DragLineX(key string, x *float64, colHex uint32) (dragged bool) {
	p.setupLocked = true
	fl := c.CurrentApplicationState.StateManager.GetResponse(p.toolHandle(key))
	if fl.HasDragged() && p.st.prevOk && p.toolPosOk {
		*x = p.st.prev.plotX(p.toolPos[0])
		dragged = true
	}
	p.tools = append(p.tools, toolFrame{kind: toolDragLineX, key: key, x: *x,
		colHex: colHex, active: fl.HasHovered() || dragged})
	return
}

// DragLineY declares a draggable horizontal line at *y.
func (p *Plot) DragLineY(key string, y *float64, colHex uint32) (dragged bool) {
	p.setupLocked = true
	fl := c.CurrentApplicationState.StateManager.GetResponse(p.toolHandle(key))
	if fl.HasDragged() && p.st.prevOk && p.toolPosOk {
		*y = p.st.prev.plotY(p.toolPos[1])
		dragged = true
	}
	p.tools = append(p.tools, toolFrame{kind: toolDragLineY, key: key, y: *y,
		colHex: colHex, active: fl.HasHovered() || dragged})
	return
}

// DragPoint declares a draggable point at (*x, *y).
func (p *Plot) DragPoint(key string, x *float64, y *float64, colHex uint32) (dragged bool) {
	p.setupLocked = true
	fl := c.CurrentApplicationState.StateManager.GetResponse(p.toolHandle(key))
	if fl.HasDragged() && p.st.prevOk && p.toolPosOk {
		*x = p.st.prev.plotX(p.toolPos[0])
		*y = p.st.prev.plotY(p.toolPos[1])
		dragged = true
	}
	p.tools = append(p.tools, toolFrame{kind: toolDragPoint, key: key, x: *x, y: *y,
		colHex: colHex, active: fl.HasHovered() || dragged})
	return
}

// Annotation declares a text callout at the plot point, offset by pixels;
// with clamp the box stays inside the plot area even when the point pans
// out (ImPlot's annotation contract).
func (p *Plot) Annotation(x float64, y float64, dxPx float32, dyPx float32, colHex uint32, clamp bool, text string) *Plot {
	p.setupLocked = true
	p.tools = append(p.tools, toolFrame{kind: toolAnnotation, x: x, y: y,
		dxPx: dxPx, dyPx: dyPx, colHex: colHex, clamp: clamp, text: text})
	return p
}

// TagX declares a colored value tag on the x axis at x; TagY on the y axis.
func (p *Plot) TagX(x float64, colHex uint32) *Plot {
	p.setupLocked = true
	p.tools = append(p.tools, toolFrame{kind: toolTagX, x: x, colHex: colHex})
	return p
}

func (p *Plot) TagY(y float64, colHex uint32) *Plot {
	p.setupLocked = true
	p.tools = append(p.tools, toolFrame{kind: toolTagY, y: y, colHex: colHex})
	return p
}

// emitToolsClipped renders the in-area tool visuals (drag lines, drag
// points, annotations); runs inside the plot-area clip.
func (p *Plot) emitToolsClipped(tr transform, areaX, areaY, areaW, areaH float32) {
	for ti := range p.tools {
		t := &p.tools[ti]
		w := float32(1.5)
		if t.active {
			w = 2.5
		}
		switch t.kind {
		case toolDragLineX:
			px := tr.pxX(t.x)
			c.PaintLine(px, areaY, px, areaY+areaH, color.Hex(t.colHex), w).Send()
		case toolDragLineY:
			py := tr.pxY(t.y)
			c.PaintLine(areaX, py, areaX+areaW, py, color.Hex(t.colHex), w).Send()
		case toolDragPoint:
			r := float32(4.5)
			if t.active {
				r = 6.0
			}
			c.PaintCircleFilled(tr.pxX(t.x), tr.pxY(t.y), r, color.Hex(t.colHex)).Send()
		case toolAnnotation:
			px, py := tr.pxX(t.x), tr.pxY(t.y)
			bx, by := px+t.dxPx, py+t.dyPx
			bw := float32(len(t.text))*charW + 8
			bh := float32(16.0)
			if t.clamp {
				bx = clamp32(bx, areaX+2, areaX+areaW-bw-2)
				by = clamp32(by, areaY+2, areaY+areaH-bh-2)
			}
			c.PaintLine(px, py, bx+bw/2, by+bh/2, color.Hex((t.colHex&^0xff)|0x66), 1.0).Send()
			c.PaintRectFilled(bx, by, bx+bw, by+bh, 2.0, color.Hex((t.colHex&^0xff)|0xd8)).Send()
			c.PaintText(bx+4, by+bh/2, 0, 1, t.text, tickFontSize, color.Hex(colContrastDark)).Monospace().Send()
		}
	}
}

// emitToolChrome renders the axis tags (outside the clip) and — unless
// the plot is NoInputs — stamps the drag tools' sense regions after the
// plot-area region, so they sit on top in egui's hit-test and win the
// gesture.
func (p *Plot) emitToolChrome(tr transform, areaX, areaY, areaW, areaH float32, regions bool) {
	for ti := range p.tools {
		t := &p.tools[ti]
		switch t.kind {
		case toolDragLineX:
			if !regions {
				continue
			}
			px := tr.pxX(t.x)
			c.PaintSenseRegion(p.ids.PrepareStr("tool-"+t.key), px-grabPx, areaY, 2*grabPx, areaH).Send()
		case toolDragLineY:
			if !regions {
				continue
			}
			py := tr.pxY(t.y)
			c.PaintSenseRegion(p.ids.PrepareStr("tool-"+t.key), areaX, py-grabPx, areaW, 2*grabPx).Send()
		case toolDragPoint:
			if !regions {
				continue
			}
			px, py := tr.pxX(t.x), tr.pxY(t.y)
			c.PaintSenseRegion(p.ids.PrepareStr("tool-"+t.key), px-7, py-7, 14, 14).Send()
		case toolTagX:
			px := tr.pxX(t.x)
			lbl := formatTick(t.x, 2)
			bw := float32(len(lbl))*charW + 6
			c.PaintRectFilled(px-bw/2, areaY+areaH+1, px+bw/2, areaY+areaH+tickLen+13, 2.0, color.Hex(t.colHex)).Send()
			c.PaintText(px, areaY+areaH+tickLen+3, 1, 0, lbl, tickFontSize, color.Hex(colContrastDark)).Monospace().Send()
		case toolTagY:
			py := tr.pxY(t.y)
			lbl := formatTick(t.y, 2)
			bw := float32(len(lbl))*charW + 6
			c.PaintRectFilled(areaX-tickLen-bw-1, py-7, areaX-1, py+7, 2.0, color.Hex(t.colHex)).Send()
			c.PaintText(areaX-tickLen-4, py, 2, 1, lbl, tickFontSize, color.Hex(colContrastDark)).Monospace().Send()
		}
	}
}

// emitContextMenu renders the right-click menu window when open: the fit
// actions of ImPlot's plot context menu, re-expressed with native egui2
// widgets per SD2 (chrome is re-expressed, not ported).
func (p *Plot) emitContextMenu() {
	st := p.st
	if !st.ctxOpen {
		return
	}
	// Relative ids from the plot's scope (still pushed here), salted by the
	// open-counter so each opening is a fresh window that re-anchors at the
	// pointer via DefaultPos.
	for range c.Window(p.ids.PrepareStr(fmt.Sprintf("implot-ctx-%d", st.ctxSeq)), c.WidgetText().Text("plot").Keep()).
		TitleBar(false).
		Resizable(false).
		DefaultPos(st.ctxScreen[0], st.ctxScreen[1]).
		KeepIter() {
		if c.Button(p.ids.PrepareStr("ctx-fit-x"), c.Atoms().Text("Fit X axis").Keep()).SendResp().HasPrimaryClicked() {
			st.x.fitNext = true
			st.ctxOpen = false
		}
		if c.Button(p.ids.PrepareStr("ctx-fit-y"), c.Atoms().Text("Fit Y axis").Keep()).SendResp().HasPrimaryClicked() {
			st.y.fitNext = true
			st.ctxOpen = false
		}
		if c.Button(p.ids.PrepareStr("ctx-fit-both"), c.Atoms().Text("Fit both").Keep()).SendResp().HasPrimaryClicked() {
			st.x.fitNext = true
			st.y.fitNext = true
			st.ctxOpen = false
		}
		if c.Button(p.ids.PrepareStr("ctx-close"), c.Atoms().Text("Close").Keep()).SendResp().HasPrimaryClicked() {
			st.ctxOpen = false
		}
	}
}

func clamp32(v, lo, hi float32) float32 {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
