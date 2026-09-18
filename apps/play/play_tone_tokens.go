package play

import (
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// toneTokens is the tone-token colour vocabulary (ADR-0122 §SD2): the
// foreground semantic tones only. Kanban's `@token` dots and the Cards
// pane's `card_tone` (ADR-0245) share it.
//
// The *Subtle tones are deliberately absent. They are background fills
// (L≈0.2 — NeutralSubtle is 27/27/27) and land within a few points of the
// card's own NeutralBgSurface, so a dot or an accent edge painted in one is
// invisible. The vocabulary excludes them by construction rather than
// warning about them.
var toneTokens = map[string]styletokens.RGBA8{
	"success":  styletokens.SuccessDefault,
	"warning":  styletokens.WarningDefault,
	"error":    styletokens.ErrorDefault,
	"info":     styletokens.InfoDefault,
	"accent":   styletokens.AccentDefault,
	"neutral":  styletokens.NeutralDefault,
	"disabled": styletokens.NeutralTextDisabled,
}

func toneTokenColor(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }

// toneTokenNames lists the vocabulary for a reject message, sorted so the
// text is stable across runs (map order is not).
func toneTokenNames() string {
	names := make([]string, 0, len(toneTokens))
	for k := range toneTokens {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
