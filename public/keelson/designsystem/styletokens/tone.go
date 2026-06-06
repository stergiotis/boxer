package styletokens

// Semantic tone roles (ADR-0031 §SD2 + §Updates 2026-06-06). A Tone names a
// semantic colour family — neutral / accent / success / warning / error /
// info — and resolves it to the IDS semantic-palette tokens. It is the role
// *selector* the semantic palette always implied: a caller says "paint this
// in the error role" and the resolver picks the right token.
//
// Promoted here from the badge widget's private tonePalette so a SECOND
// consumer (the gauge widget, ADR-0068) that *paints* a tone — rather than
// handing it to badge.New — can resolve it directly. Tone→token is IDS
// colour policy, not a widget detail. badge re-exports `type ToneE = Tone`
// plus the constants, so its public surface and every existing call site
// (badge.ToneError, …) are unchanged.
//
// Go-only — no Rust mirror, no drift test (cf. surface.go): the resolver is
// policy over palette tokens that ALREADY have their Rust counterpart
// (palette_generated.rs); nothing new reaches egui::Style. If Rust ever
// needs to resolve a tone, that is a later mirror.
//
// Methods return RGBA8, never a drawable color.Color: styletokens sits on
// the keelson side of the ADR-0035 layering and must not import the thestack
// widget color package. Widgets bridge at their own edge with
// color.Hex(tone.Fill().AsHex()) — the designlint-L2 path (see rgba8.go).
//
// Boxer enum-suffix convention: the type is Tone (the constants carry the
// Tone prefix); badge keeps the ToneE alias for source-compat.
type Tone uint8

const (
	ToneNeutral Tone = iota
	TonePrimary      // accent role — ADR-0031 forbids "primary" as a TOKEN name; the role enumerator keeps the name
	ToneSuccess
	ToneWarning
	ToneError
	ToneInfo
)

// roles returns the three emphasis-varying tokens for a tone — Default,
// Subtle, Strong — in one switch so the role→token mapping has a single
// source of truth. An unknown Tone falls back to the neutral role (never
// panics), matching the badge tonePalette default.
func (t Tone) roles() (deflt, subtle, strong RGBA8) {
	switch t {
	case TonePrimary:
		return AccentDefault, AccentSubtle, AccentStrong
	case ToneSuccess:
		return SuccessDefault, SuccessSubtle, SuccessStrong
	case ToneWarning:
		return WarningDefault, WarningSubtle, WarningStrong
	case ToneError:
		return ErrorDefault, ErrorSubtle, ErrorStrong
	case ToneInfo:
		return InfoDefault, InfoSubtle, InfoStrong
	default: // ToneNeutral
		return NeutralDefault, NeutralSubtle, NeutralStrong
	}
}

// Fill is the solid fill for a tone — the <role>.Default token (L≈0.80).
// Use for a filled chip, a gauge zone band, a status dot.
func (t Tone) Fill() (c RGBA8) {
	c, _, _ = t.roles()
	return
}

// Soft is the quiet fill for a tone — the <role>.Subtle token (L≈0.20). Use
// for a translucent "category tag" background that does not dominate.
func (t Tone) Soft() (c RGBA8) {
	_, c, _ = t.roles()
	return
}

// Strong is the tone-coloured foreground — the <role>.Strong token (L≈0.90).
// Use for text/label drawn in the tone's hue (e.g. over a Soft fill).
func (t Tone) Strong() (c RGBA8) {
	_, _, c = t.roles()
	return
}

// TextOnFill is the high-contrast foreground to place ON a Fill background —
// the neutral bg.extreme token, dark enough to read on every tone's Default
// (Lc ≈ -100 via APCA on the dark spine). Tone-independent by design.
func (Tone) TextOnFill() (c RGBA8) {
	c = NeutralBgExtreme
	return
}
