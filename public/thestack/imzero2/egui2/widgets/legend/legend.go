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

// Measure returns the box the items need. Zero for no items.
func Measure(items []Item, st Style) (w, h float32) {
	if len(items) == 0 {
		return
	}
	st = st.withDefaults()
	widest := float32(0)
	for i := range items {
		widest = max(widest, st.TextWidth(items[i].Label, st.FontSize))
	}
	w = st.Padding*3 + st.Swatch + widest
	h = st.Padding*2 + float32(len(items))*st.RowHeight
	return
}

// Paint emits the box, one swatch and one label per item, with the box's
// top-left at (x, y). It returns the box size so a caller can place what
// follows. Nothing is emitted for no items.
func Paint(items []Item, x, y float32, st Style) (w, h float32) {
	w, h = Measure(items, st)
	if len(items) == 0 {
		return
	}
	st = st.withDefaults()
	c.PaintRectFilled(x, y, x+w, y+h, st.Rounding, st.Background).Send()
	c.PaintRectStroke(x, y, x+w, y+h, st.Rounding, st.Border, styletokens.StrokeHair).Send()
	for i := range items {
		it := &items[i]
		ry := y + st.Padding + float32(i)*st.RowHeight
		sw, txt := it.Color, st.Text
		if it.Hidden {
			sw, txt = Dimmed(it.Color), st.TextHidden
		}
		c.PaintRectFilled(x+st.Padding, ry+(st.RowHeight-st.Swatch)/2, x+st.Padding+st.Swatch, ry+(st.RowHeight+st.Swatch)/2, min(st.Rounding, st.Swatch/2), sw).Send()
		t := c.PaintText(x+st.Padding*2+st.Swatch, ry+st.RowHeight/2, 0, 1, it.Label, st.FontSize, txt)
		if st.Monospace {
			t = t.Monospace()
		}
		t.Send()
	}
	return
}

// EmitSense stamps one sense region per row, keyed prefix+Key under ids,
// over the rows Paint drew at (x, y). Read reads them back next frame.
func EmitSense(ids *c.WidgetIdStack, prefix string, items []Item, x, y float32, st Style) {
	if len(items) == 0 {
		return
	}
	w, _ := Measure(items, st)
	st = st.withDefaults()
	for i := range items {
		ry := y + st.Padding + float32(i)*st.RowHeight
		c.PaintSenseRegion(ids.PrepareStr(prefix+items[i].key()), x, ry, w, st.RowHeight).Send()
	}
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

// RowAt is the pure hit-test for a caller that picks Go-side instead of
// stamping regions: the row under canvas point (px, py) for a legend Paint
// drew at (x, y), or -1.
func RowAt(items []Item, x, y, px, py float32, st Style) int {
	w, h := Measure(items, st)
	if len(items) == 0 || px < x || px > x+w || py < y || py > y+h {
		return -1
	}
	st = st.withDefaults()
	rel := py - y - st.Padding
	if rel < 0 {
		return -1
	}
	row := int(rel / st.RowHeight)
	if row >= len(items) {
		return -1
	}
	return row
}

// Dimmed is the swatch colour of a hidden row: the item's colour at a
// quarter of full alpha. A colour without a literal value is returned as is.
func Dimmed(col color.Color) color.Color {
	if col.Kind() != color.ColorKindLiteral {
		return col
	}
	return color.Hex(col.Literal()&^0xff | 0x40)
}
