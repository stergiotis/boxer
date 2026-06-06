package styletokens_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// TestToneResolvesToSemanticPaletteTokens locks the role→token mapping the
// gauge widget and badge both depend on (ADR-0031 §Updates 2026-06-06). It is
// the exact table badge.tonePalette used to own, now sourced from styletokens.
func TestToneResolvesToSemanticPaletteTokens(t *testing.T) {
	type want struct{ fill, soft, strong, textOnFill styletokens.RGBA8 }
	cases := []struct {
		name string
		tone styletokens.Tone
		want want
	}{
		{"neutral", styletokens.ToneNeutral, want{styletokens.NeutralDefault, styletokens.NeutralSubtle, styletokens.NeutralStrong, styletokens.NeutralBgExtreme}},
		{"primary(accent)", styletokens.TonePrimary, want{styletokens.AccentDefault, styletokens.AccentSubtle, styletokens.AccentStrong, styletokens.NeutralBgExtreme}},
		{"success", styletokens.ToneSuccess, want{styletokens.SuccessDefault, styletokens.SuccessSubtle, styletokens.SuccessStrong, styletokens.NeutralBgExtreme}},
		{"warning", styletokens.ToneWarning, want{styletokens.WarningDefault, styletokens.WarningSubtle, styletokens.WarningStrong, styletokens.NeutralBgExtreme}},
		{"error", styletokens.ToneError, want{styletokens.ErrorDefault, styletokens.ErrorSubtle, styletokens.ErrorStrong, styletokens.NeutralBgExtreme}},
		{"info", styletokens.ToneInfo, want{styletokens.InfoDefault, styletokens.InfoSubtle, styletokens.InfoStrong, styletokens.NeutralBgExtreme}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.tone.Fill(); got != c.want.fill {
				t.Errorf("Fill() = %+v, want %+v", got, c.want.fill)
			}
			if got := c.tone.Soft(); got != c.want.soft {
				t.Errorf("Soft() = %+v, want %+v", got, c.want.soft)
			}
			if got := c.tone.Strong(); got != c.want.strong {
				t.Errorf("Strong() = %+v, want %+v", got, c.want.strong)
			}
			if got := c.tone.TextOnFill(); got != c.want.textOnFill {
				t.Errorf("TextOnFill() = %+v, want %+v", got, c.want.textOnFill)
			}
		})
	}
}

// TestToneUnknownFallsBackToNeutral documents that an out-of-range Tone
// resolves to the neutral role instead of panicking — the badge.tonePalette
// default contract, preserved by the switch default in Tone.roles.
func TestToneUnknownFallsBackToNeutral(t *testing.T) {
	bogus := styletokens.Tone(250)
	if got := bogus.Fill(); got != styletokens.NeutralDefault {
		t.Errorf("unknown tone Fill() = %+v, want NeutralDefault %+v", got, styletokens.NeutralDefault)
	}
	if got := bogus.TextOnFill(); got != styletokens.NeutralBgExtreme {
		t.Errorf("unknown tone TextOnFill() = %+v, want NeutralBgExtreme %+v", got, styletokens.NeutralBgExtreme)
	}
}
