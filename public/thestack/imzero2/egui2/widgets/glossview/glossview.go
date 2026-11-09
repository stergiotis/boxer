// Package glossview draws gloss faces: a value's inline face in its tone, a
// gloss/url value as a link, and a column's resolution as hover text. It is
// the imzero2 half of the gloss catalog, which stays free of any UI; a host
// resolves a column with gloss.Resolve and draws its values here.
package glossview

import (
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// ToneColor maps a face's tone onto the design system's semantic palette.
// Neutral is no colour: the text keeps the style it would have had, rather
// than a neutral grey that would read as disabled.
func ToneColor(t gloss.ToneE) (col color.Color, ok bool) {
	var def styletokens.RGBA8
	switch t {
	case gloss.ToneInfo:
		def = styletokens.InfoDefault
	case gloss.ToneSuccess:
		def = styletokens.SuccessDefault
	case gloss.ToneWarning:
		def = styletokens.WarningDefault
	case gloss.ToneError:
		def = styletokens.ErrorDefault
	case gloss.ToneAccent:
		def = styletokens.AccentDefault
	default:
		return
	}
	return color.Hex(def.AsHex()), true
}

// Style is how a face's text is set.
type Style struct {
	Monospace bool
	Small     bool
	Weak      bool
}

func (inst Style) apply(rt c.RichTextScope) {
	if inst.Monospace {
		rt.Monospace()
	}
	if inst.Small {
		rt.Small()
	}
	if inst.Weak {
		rt.Weak()
	}
}

// Face draws one face: its text in its tone, or as a hyperlink to link when
// link is not empty.
func Face(face gloss.Inline, link string, s Style) {
	if link != "" {
		c.HyperlinkTo(face.Text, link).OpenInNewTab(true).Send()
		return
	}
	if col, toned := ToneColor(face.Tone); toned {
		for rt := range c.RichTextLabelColored(col, color.Transparent, face.Text) {
			s.apply(rt)
		}
		return
	}
	for rt := range c.RichTextLabel(face.Text) {
		s.apply(rt)
	}
}

// Value draws one value of a resolved column: the gloss's face when the
// column is glossed, the plain text otherwise. A non-empty hover becomes the
// value's tooltip. glossed reports which face was drawn.
func Value(res *gloss.Resolution, cell gloss.CellI, s Style, hover string) (glossed bool) {
	face, glossed := res.Face(cell)
	link := res.Link(cell)
	if hover == "" {
		Face(face, link, s)
		return
	}
	for range c.HoverText(hover).KeepIter() {
		Face(face, link, s)
	}
	return
}
