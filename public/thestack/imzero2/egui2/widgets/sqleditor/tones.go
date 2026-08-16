package sqleditor

// Overlay tones, all from the design system (ADR-0037 palette) so an editor
// agrees with the banners and pane chrome that report the same conditions.
//
// They are exported because the embedder composes overlays in the same
// vocabulary, and because the gutter derives its marks by recognising a
// section's tone (see buildGutterModel): a section the embedder tinted with
// [ToneError] earns an error mark on its line, so the two can never disagree
// about what the buffer is saying.

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

var (
	// ToneError underlines a token a failed parse pointed at, and — since
	// ADR-0190 §SD9 — a closed-domain literal that does not resolve. Never
	// while the token is being typed: the caret's own token gets an error tone
	// only from the parse, because a name half-written is not a wrong one.
	ToneError = color.Hex(styletokens.ErrorDefault.AsHex())
	// ToneResolved underlines a literal that resolved against its argument's
	// domain — a registered component kind, a field the kind projects
	// (ADR-0190 §SD9).
	//
	// It is the quiet half of the same report ToneError makes, and the same
	// tone the completion pane outlines its exact row with, so the editor and
	// the pane cannot say "this resolves" in two different colours.
	ToneResolved = color.Hex(styletokens.SuccessDefault.AsHex())
	// ToneWarning underlines something the run gate is still waiting for.
	ToneWarning = color.Hex(styletokens.WarningDefault.AsHex())
	// ToneStatementTint backs the statement under the caret. One faint step
	// above the code editor's extreme background rather than a colour of its
	// own — it marks a region, it does not carry a severity.
	ToneStatementTint = color.Hex(styletokens.NeutralBgFaint.AsHex())
	// ToneCarried underlines the environment a narrowed run takes with it:
	// the WITH items in scope and the SET prelude. Info rather than accent so
	// it does not read as "part of the query", which the tint above means.
	ToneCarried = color.Hex(styletokens.InfoDefault.AsHex())
	// ToneSubqueryTint marks a nested query a narrowed run would ship.
	//
	// The accent at a quarter opacity, not a palette background token. The
	// palette's background family tops out at NeutralBgSurface (29,32,33) and
	// every `*Subtle` sits at OKLCh L=0.200 — all within a few levels of the
	// code editor's near-black, which is why the first cut of this (plain
	// AccentSubtle) needed a 4× magnification to see at all. Alpha is the
	// honest instrument here: same accent hue the design system already owns,
	// blended by egui over whatever is behind it (the FFI carries straight
	// alpha, `color32_from_rgba_u32`), landing near rgb(42,47,61) — legible
	// under the bright syntax colours without competing with them.
	// The annotation must sit on the line immediately above the call it covers
	// (ignoreann suppresses line N and N+1), so the reason is the paragraph
	// above rather than the parenthetical here.
	//
	// designlint:ignore=L2 (no mid-tone background token exists; see above)
	ToneSubqueryTint = color.RGBA(styletokens.AccentDefault.R,
		styletokens.AccentDefault.G, styletokens.AccentDefault.B, 0x40)
)
