package application

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/thestack/imzero2/fontresolve"
)

// fillFonts sets the main, bold, mono and fallback faces left empty to the
// ones fontconfig finds, and logs what it chose and what it could not find.
func (inst *Config) fillFonts(r fontresolve.Resolver) {
	if inst.MainFontTTF != "" && inst.BoldFontTTF != "" && inst.MonoFontTTF != "" && inst.FallbackFontTTF != "" {
		return
	}
	found, notes := r.Resolve()
	for _, f := range []struct {
		name  string
		dst   *string
		found string
	}{
		{"main", &inst.MainFontTTF, found.Main},
		{"bold", &inst.BoldFontTTF, found.Bold},
		{"mono", &inst.MonoFontTTF, found.Mono},
		{"fallback", &inst.FallbackFontTTF, found.Fallback},
	} {
		if *f.dst == "" && f.found != "" {
			*f.dst = f.found
			log.Debug().Str("face", f.name).Str("path", f.found).Msg("font found through fontconfig")
		}
	}
	for _, n := range notes {
		log.Warn().Msg(n)
	}
}
