// Package sectionhead draws the heading that opens a section of a pane, an
// inspector or a settings page: space above, the heading, and a rule under
// it. It is a fluid widget (ADR-0267) without an id: it draws two labels and
// a separator, and the host reads nothing back from them.
//
// One package so that every app groups its content the same way (keelson-ux
// C3: a separator and a caption per group), and so that how loud a heading
// is stays one decision across themes. A pane's title is not a section head;
// it stays a Heading-sized label of the app's own.
//
// The typography follows from that rank. The heading is set between the
// body and the pane title on the type scale (ADR-0030 §SD3), so it stands
// out from the text it opens without matching the title above it; the
// strong text colour adds what a bold face would, which the bundled fonts
// lack. More space goes above the heading than below it, so it reads as
// part of the section it opens rather than the one before. Headings are in
// sentence case.
package sectionhead

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Fluid is one section heading, built by New and drawn by Send.
type Fluid struct {
	text string
	tip  string
}

// New starts the heading of a section named text.
func New(text string) Fluid {
	return Fluid{text: text}
}

// Tip puts text on the heading's hover: what the section holds or how to
// read it, where the heading alone does not say.
func (inst Fluid) Tip(text string) Fluid { inst.tip = text; return inst }

// Send draws the heading in the current ui.
func (inst Fluid) Send() {
	c.AddSpace(styletokens.PaddingOuter(styletokens.ActiveDensity()))
	if inst.tip == "" {
		inst.label()
	} else {
		for range c.HoverText(inst.tip).KeepIter() {
			inst.label()
		}
	}
	c.Separator().Horizontal().Send()
}

func (inst Fluid) label() {
	for rt := range c.RichTextLabel(inst.text) {
		rt.Strong().Size(sizePt(styletokens.ActiveDensity()))
	}
}

// sizePt is the heading's size: halfway between the body and the Heading
// step of the type scale, adjusted for density as the scale's own steps are.
func sizePt(d styletokens.DensityE) float32 {
	return styletokens.ScaledPt((styletokens.BodyPt+styletokens.HeadingPt)/2, d)
}
