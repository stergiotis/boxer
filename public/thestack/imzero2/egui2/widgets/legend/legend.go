// Package legend is the painter-lane legend shared by canvas widgets: a
// boxed list of colour swatch plus label rows, one per item, drawn inside a
// widget's own canvas and — when the widget wants it interactive — stamped
// with one sense region per row so a click toggles the row and a hover
// highlights it. It was factored out of the ImPlot port when graphview grew
// its aura legend (ADR-0224 §SD11); both draw the same rows through it.
//
// The package keeps no state: the caller owns which items are hidden and
// which is hovered, and reads the previous frame's clicks and hovers back
// through [Read] before painting, so a toggle applies in the frame it is
// read. Coordinates are canvas pixels, like every Paint* opcode.
//
//	items := []legend.Item{{Key: "a", Label: "A", Color: colA, Hidden: hiddenA}}
//	clicked, hovered := legend.Read(sm, ids, "legend-", items) // last frame's input
//	legend.Paint(items, x, y, st)
//	legend.EmitSense(ids, "legend-", items, x, y, st)          // after the widget's own regions
//
// Sense-region emission order is hit-test priority on the painter lane:
// emit the legend's rows after the widget's area region so a click on a
// row never falls through to a pan.
package legend

import (
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Item is one legend row. Key names the row's sense region under the
// caller's prefix and defaults to Label; two rows with one key share a
// region, which is how same-label series share an entry. Hidden dims the
// swatch and the text.
type Item struct {
	Key    string
	Label  string
	Color  color.Color
	Hidden bool
}

func (inst Item) key() string {
	if inst.Key != "" {
		return inst.Key
	}
	return inst.Label
}

// Style holds the legend's metrics and colours. A zero Style is
// DefaultStyle(); a caller sets the fields it cares about.
type Style struct {
	FontSize   float32 // points, default 11
	RowHeight  float32 // pixels, default 16
	Padding    float32 // pixels around the rows and between swatch and text, default 6
	Swatch     float32 // swatch side in pixels, default 10
	Rounding   float32 // box and swatch corner radius, default 3
	Background color.Color
	Border     color.Color
	Text       color.Color
	TextHidden color.Color // label of a hidden row
	Monospace  bool
	// TextWidth estimates a label's width at a font size, for sizing the
	// box. nil takes a rune-count estimate at the house glyph ratio; a
	// caller with the ImPlot estimator passes it for one shared answer.
	TextWidth func(s string, fontSize float32) float32
	// Horizontal lays the items out left to right, each as wide as its own
	// label, wrapping onto a further line where the next one would pass
	// MaxWidth — the legend for under a plot. Unset, one item per line.
	Horizontal bool
	// MaxWidth bounds a horizontal legend's box; 0 keeps every item on one
	// line. An item wider than the bound gets a line of its own.
	MaxWidth float32
}

// DefaultStyle is the design-token appearance.
func DefaultStyle() Style {
	hex := func(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }
	return Style{
		FontSize:   11,
		RowHeight:  16,
		Padding:    6,
		Swatch:     10,
		Rounding:   3,
		Background: color.Hex(styletokens.NeutralBgPanel.AsHex()&^0xff | 0xee),
		Border:     hex(styletokens.NeutralBorderFaint),
		Text:       hex(styletokens.NeutralTextSecondary),
		TextHidden: hex(styletokens.NeutralTextDisabled),
	}
}

func (inst Style) withDefaults() Style {
	d := DefaultStyle()
	num := func(v *float32, dv float32) {
		if *v <= 0 {
			*v = dv
		}
	}
	col := func(v *color.Color, dv color.Color) {
		if v.Kind() == color.ColorKindNone {
			*v = dv
		}
	}
	num(&inst.FontSize, d.FontSize)
	num(&inst.RowHeight, d.RowHeight)
	num(&inst.Padding, d.Padding)
	num(&inst.Swatch, d.Swatch)
	num(&inst.Rounding, d.Rounding)
	col(&inst.Background, d.Background)
	col(&inst.Border, d.Border)
	col(&inst.Text, d.Text)
	col(&inst.TextHidden, d.TextHidden)
	if inst.TextWidth == nil {
		inst.TextWidth = estimateTextWidth
	}
	return inst
}

// glyphWidthRatio mirrors the ImPlot port's house estimate of a glyph's
// advance as a fraction of the font size (implot.GlyphWidthRatio); the two
// must move together.
const glyphWidthRatio = 0.62

func estimateTextWidth(s string, fontSize float32) float32 {
	return float32(utf8.RuneCountInString(s)) * fontSize * glyphWidthRatio
}

// itemGap is the space between two items on one line of a horizontal
// legend, in units of Padding.
const itemGap = 2

// layout calls fn with each item's cell — the swatch, the label and the
// padding before them, relative to the box's top-left — in item order, and
// returns the box size. st must have its defaults filled in.
func layout(items []Item, st Style, fn func(i int, x, y, w float32)) (w, h float32) {
	if len(items) == 0 {
		return
	}
	if !st.Horizontal {
		widest := float32(0)
		for i := range items {
			widest = max(widest, st.TextWidth(items[i].Label, st.FontSize))
		}
		w = st.Padding*3 + st.Swatch + widest
		h = st.Padding*2 + float32(len(items))*st.RowHeight
		if fn != nil {
			for i := range items {
				fn(i, 0, st.Padding+float32(i)*st.RowHeight, w)
			}
		}
		return
	}
	x, line := float32(0), 0
	for i := range items {
		cw := st.Padding*2 + st.Swatch + st.TextWidth(items[i].Label, st.FontSize)
		if x > 0 && st.MaxWidth > 0 && x+st.Padding*itemGap+cw+st.Padding > st.MaxWidth {
			x, line = 0, line+1
		}
		if x > 0 {
			x += st.Padding * itemGap
		}
		if fn != nil {
			fn(i, x, st.Padding+float32(line)*st.RowHeight, cw)
		}
		x += cw
		w = max(w, x+st.Padding)
	}
	h = st.Padding*2 + float32(line+1)*st.RowHeight
	return
}

// Measure returns the box the items need. Zero for no items.
func Measure(items []Item, st Style) (w, h float32) {
	return layout(items, st.withDefaults(), nil)
}

// Paint emits the box, one swatch and one label per item, with the box's
// top-left at (x, y). It returns the box size so a caller can place what
// follows. Nothing is emitted for no items.
func Paint(items []Item, x, y float32, st Style) (w, h float32) {
	st = st.withDefaults()
	w, h = layout(items, st, nil)
	if len(items) == 0 {
		return
	}
	c.PaintRectFilled(x, y, x+w, y+h, st.Rounding, st.Background).Send()
	c.PaintRectStroke(x, y, x+w, y+h, st.Rounding, st.Border, styletokens.StrokeHair).Send()
	layout(items, st, func(i int, cx, cy, _ float32) {
		it := &items[i]
		rx, ry := x+cx, y+cy
		sw, txt := it.Color, st.Text
		if it.Hidden {
			sw, txt = dimmed(it.Color), st.TextHidden
		}
		c.PaintRectFilled(rx+st.Padding, ry+(st.RowHeight-st.Swatch)/2, rx+st.Padding+st.Swatch, ry+(st.RowHeight+st.Swatch)/2, min(st.Rounding, st.Swatch/2), sw).Send()
		t := c.PaintText(rx+st.Padding*2+st.Swatch, ry+st.RowHeight/2, 0, 1, it.Label, st.FontSize, txt)
		if st.Monospace {
			t = t.Monospace()
		}
		t.Send()
	})
	return
}

// EmitSense stamps one sense region per item, keyed prefix+Key under ids,
// over the cells Paint drew at (x, y) — a whole row of a vertical legend,
// the item's own extent in a horizontal one. Read reads them back next frame.
//
// Each row also gets a hit region labelled with the row's Label, which gives
// it an accessibility node of that name and a tooltip saying what a click
// does. A sense region carries no name, so without it a scene or an agent
// could not find a row; the hit region only names and hovers, the sense
// region under it keeps the click. An agent driving the host clicks a row by
// name with a pointer press (imzero2 drive's "pointer": true), since an
// AccessKit click reaches the hit region and not the sense region.
func EmitSense(ids *c.WidgetIdStack, prefix string, items []Item, x, y float32, st Style) {
	if len(items) == 0 {
		return
	}
	st = st.withDefaults()
	layout(items, st, func(i int, cx, cy, cw float32) {
		it := &items[i]
		rx, ry := x+cx, y+cy
		c.PaintSenseRegion(ids.PrepareStr(prefix+it.key()), rx, ry, cw, st.RowHeight).Send()
		if it.Label != "" {
			c.PaintHitRect(ids.PrepareStr(prefix+it.key()+nameSuffix), rx, ry, rx+cw, ry+st.RowHeight).
				Label(it.Label).Tooltip(rowTip(it.Hidden)).Send()
		}
	})
}

// nameSuffix keys a row's naming hit region apart from its sense region. The
// unit separator does not occur in a label a person would write.
const nameSuffix = "\x1fname"

// rowTip is the tooltip under a row's name: what a click on it does.
func rowTip(hidden bool) string {
	if hidden {
		return "Hidden. Click to show."
	}
	return "Click to hide."
}

// Read returns the index of the row the previous frame's input clicked and
// the one it hovers, or -1 for either, from the regions EmitSense stamped
// under the same ids and prefix. Rows sharing a key report together; the
// first wins.
func Read(sm *c.StateManager, ids *c.WidgetIdStack, prefix string, items []Item) (clicked, hovered int) {
	clicked, hovered = -1, -1
	for i := range items {
		h := widgethandle.Make(ids.PrepareStr(prefix + items[i].key()).Derive())
		f := sm.GetResponse(h)
		if clicked < 0 && f.HasPrimaryClicked() {
			clicked = i
		}
		if hovered < 0 && f.HasHovered() {
			hovered = i
		}
	}
	return
}

// dimmed is the swatch colour of a hidden row: the item's colour at a
// quarter of full alpha. A colour without a literal value is returned as is.
func dimmed(col color.Color) color.Color {
	if col.Kind() != color.ColorKindLiteral {
		return col
	}
	return color.Hex(col.Literal()&^0xff | 0x40)
}
