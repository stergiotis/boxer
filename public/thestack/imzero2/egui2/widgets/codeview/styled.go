package codeview

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// StyleFlagE is the style vocabulary a styled section can express — exactly
// what egui's TextFormat carries natively (ADR-0130 L3). Flags compose: an
// error underline inside a tinted statement is two sections over the same
// bytes, and both reach the color sections they overlap.
//
// A wavy squiggle is deliberately absent: TextFormat cannot express one, and
// painting over galley rows is deferred until the straight underline proves
// insufficient in use.
type StyleFlagE uint32

const (
	StyleUnderline StyleFlagE = 1 << iota
	StyleBackground
	StyleStrikethrough
	StyleItalics
	// StyleFont says the section's Family and Size are meaningful
	// (ADR-0334 §SD6). It travels on its own builder method, since the
	// decorations overlap-merge into the colour sections they touch while a
	// font has to split them — one shaping run cannot change font halfway.
	StyleFont
)

// FontFamilyE names the family a [StyleFont] section asks for.
type FontFamilyE uint32

const (
	// FontInherit keeps the editor's base family (monospace) and only scales it.
	FontInherit FontFamilyE = iota
	FontProportional
	FontMonospace
)

// StyledSection is one sparse overlay over a byte range of the buffer the
// consuming widget holds. Offsets are byte offsets into that buffer; Color is
// the stroke color for underline/strikethrough and the fill for background
// (italics ignores it). Family and Size are read only under [StyleFont]: Size
// is a factor of the editor's base font size, and a Size of zero keeps it.
//
// Unlike the colour tier this channel does not have to cover the buffer —
// uncovered bytes simply carry no styling, and Rust-side normalization drops
// inverted, empty, and flagless sections rather than gap-filling around them.
// Where font sections nest, the narrowest one covering a byte wins, so a
// whole-buffer body font and a heading line over part of it compose as a
// reader expects regardless of the order they are listed in.
type StyledSection struct {
	Start  uint32
	Stop   uint32
	Flags  StyleFlagE
	Color  color.Color
	Family FontFamilyE
	Size   float32
}

// BuildStyledSections serializes overlays into a retained StyledSections
// holder for TextEdit.SectionStyled.
//
// Deliberately uncached, like [BuildSqlLex]: producers rebuild this list per
// frame from a handful of spans (an error token, the statement under the
// caret, unfilled placeholders), which is cheap enough that a memo would only
// churn — and unlike the colour job, the inputs change on caret movement, not
// only on edits. An empty list returns ok=false so callers can skip the
// builder method entirely.
func BuildStyledSections(secs []StyledSection) (job typed.RetainedFffiHolderTyped[c.StyledSectionsS], ok bool) {
	// Screen before opening the builder: an all-degenerate list would
	// otherwise cost an opcode and a retained holder to say nothing.
	for _, s := range secs {
		if s.Stop > s.Start && s.Flags != 0 {
			ok = true
			break
		}
	}
	if !ok {
		return job, false
	}
	b := c.StyledSections()
	for _, s := range secs {
		if s.Stop <= s.Start || s.Flags == 0 {
			continue
		}
		// A font and a decoration over the same bytes are two wire entries:
		// the register keeps them apart because the layouter treats them
		// differently (split versus overlap-merge).
		if s.Flags&StyleFont != 0 {
			b = b.SectionFont(s.Start, s.Stop, uint32(s.Family), s.Size)
		}
		if rest := s.Flags &^ StyleFont; rest != 0 {
			b = b.Section(s.Start, s.Stop, uint32(rest), s.Color)
		}
	}
	return b.Keep(), true
}
