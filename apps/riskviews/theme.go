package riskviews

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Every colour the views paint comes from the IDS token layer. The mapping
// from the prototype's CSS custom properties to IDS facilities:
//
//	--surface, --page        → NeutralBgFaint / NeutralBgPanel (the implot IDS chrome's canvas)
//	--ink, --ink-2, --muted  → NeutralTextPrimary / NeutralTextSecondary / NeutralTextDisabled
//	--grid, --axis           → NeutralBorderFaint at reduced alpha / NeutralBorderDefault
//	--s1, --s2, --s3         → QualitativeCycle slots (Okabe–Ito)
//	--seq1..--seq5           → Sequential(Oslo) samples — the IDS single-blue ramp
//	--div-lo, --div-hi       → Diverging(Vik) at ∓0.75
//	--good, --warn, --crit   → Tone Success / Warning / Error .Fill(); "none" → Tone Neutral
//	--band1..--band3         → Sequential(GrayC) samples (no neutral band ramp in IDS)
//
// Two gaps show here. IDS has no neutral "band" ramp for qualitative zones
// (appetite / tolerance / beyond), so GrayC — a data-encoding sequential — is
// borrowed; and the status Tones are the badge's semantic colours, which is
// what ADR-0029 L7's deferred `statussemantics` enum would own.

// qualitative palette slots the prototype's --s1/--s2/--s3 map onto.
const (
	slotInherent = 1 // blue           (prototype --s1, blue)
	slotResidual = 0 // reddish purple (prototype --s2 was orange, which sat too close to the warning status colour)
	slotThird    = 3 // bluish green   (prototype --s3, green)
)

func tok(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }

func tokA(t styletokens.RGBA8, a uint8) color.Color {
	return color.Hex(t.AsHex()&^0xff | uint32(a))
}

func hexA(t styletokens.RGBA8, a uint8) uint32 { return t.AsHex()&^0xff | uint32(a) }

type palette struct {
	bg, area, grid, axis, border color.Color
	ink, ink2, muted             color.Color
	s                            [3]color.Color
	sHex                         [3]uint32
	seq                          [5]color.Color
	divLo, divHi                 color.Color
	band                         [3]color.Color
}

func slotColor(slot int) styletokens.RGBA8 { return styletokens.QualitativeCycle(slot) }

// pal resolves the palette. Called once per frame: tokens are package vars
// that the theme init can rewrite, so nothing is cached across frames.
func pal() (p palette) {
	p.bg = tok(styletokens.NeutralBgFaint)
	p.area = tok(styletokens.NeutralBgSurface)
	p.grid = tokA(styletokens.NeutralBorderFaint, 0x50)
	p.axis = tok(styletokens.NeutralBorderFaint)
	p.border = tokA(styletokens.NeutralBorderDefault, 0x60)
	p.ink = tok(styletokens.NeutralTextPrimary)
	p.ink2 = tok(styletokens.NeutralTextSecondary)
	p.muted = tok(styletokens.NeutralTextDisabled)
	for i, slot := range []int{slotInherent, slotResidual, slotThird} {
		p.s[i] = tok(slotColor(slot))
		p.sHex[i] = slotColor(slot).AsHex()
	}
	for i := range p.seq {
		// Oslo runs black → blue → white; the low end is the dark surface,
		// so the ramp starts above it.
		p.seq[i] = tok(styletokens.Sequential(styletokens.SequentialOslo, 0.18+0.13*float32(i)))
	}
	p.divLo = tok(styletokens.Diverging(styletokens.DivergingVik, -0.7))
	p.divHi = tok(styletokens.Diverging(styletokens.DivergingVik, 0.7))
	// GrayC runs white → black; on the dark spine "within appetite" is the
	// quietest band, so it takes the darkest sample.
	p.band[0] = tok(styletokens.Sequential(styletokens.SequentialGrayC, 0.90))
	p.band[1] = tok(styletokens.Sequential(styletokens.SequentialGrayC, 0.76))
	p.band[2] = tok(styletokens.Sequential(styletokens.SequentialGrayC, 0.60))
	return
}

func statusTone(s StatusE) styletokens.Tone {
	switch s {
	case StatusGood:
		return styletokens.ToneSuccess
	case StatusWarn:
		return styletokens.ToneWarning
	case StatusCrit:
		return styletokens.ToneError
	default:
		return styletokens.ToneNeutral
	}
}

func statusFill(s StatusE) color.Color { return tok(statusTone(s).Fill()) }

// statusGlyph is a Phosphor glyph, never a fallback-font codepoint (imzero2
// skill §12 "Oversized, Off-Centre Glyph").
func statusGlyph(s StatusE) string {
	switch s {
	case StatusGood:
		return icons.PhCheck
	case StatusWarn:
		return icons.PhExclamationMark
	case StatusCrit:
		return icons.PhX
	default:
		// Phosphor's "question" is a circled mark that reads as a ring on a
		// filled disc, and PhQuestionMark carries the same codepoint in the
		// generated table; a plain "?" is in the main text font, so it does
		// not escape to the fallback chain.
		return "?"
	}
}

// type sizes from the IDS typography scale.
const (
	fsAx    = styletokens.CaptionPt // axis ticks
	fsLbl   = styletokens.CaptionPt // the prototype's 12px labels; IDS has no step between Caption (11) and Body (13)
	fsBody  = styletokens.BodyPt
	fsTitle = styletokens.HeadingPt
)

var transparent = color.Transparent
