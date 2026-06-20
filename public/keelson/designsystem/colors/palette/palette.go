// Package palette parses palette.toml (ADR-0040, consolidating ADR-0033 §SD3) and resolves token
// names to OKLCh + sRGB triples after gamut clipping.
package palette

import (
	"fmt"
	"sort"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/gma"
)

// EmphasisLevel names the three emphasis levels per ADR-0033 §SD3.
type EmphasisLevel string

const (
	EmphasisSubtle  EmphasisLevel = "subtle"
	EmphasisDefault EmphasisLevel = "default"
	EmphasisStrong  EmphasisLevel = "strong"
)

// LC carries the (L, C) target for one emphasis level.
type LC struct {
	L float64 `toml:"L"`
	C float64 `toml:"C"`
}

// SemanticRole holds a hue plus three emphasis LCs.
type SemanticRole struct {
	Hue     float64 `toml:"hue"`
	Subtle  LC      `toml:"subtle"`
	Default LC      `toml:"default"`
	Strong  LC      `toml:"strong"`
}

// NeutralBlock holds the dark-theme neutral spine.
type NeutralBlock struct {
	Hue    float64           `toml:"hue"`
	Chroma float64           `toml:"chroma"`
	Spine  map[string]float64 `toml:"spine"`
}

// File is the full palette.toml structure.
type File struct {
	Meta struct {
		OklabVersion string   `toml:"oklab_version"`
		Gamut        string   `toml:"gamut"`
		Emit         []string `toml:"emit"`
		Theme        string   `toml:"theme"`
	} `toml:"meta"`
	Neutral  NeutralBlock             `toml:"neutral"`
	Semantic map[string]SemanticRole `toml:"semantic"`
}

// Token is one resolved color: OKLCh target, post-clip OKLCh, sRGB hex.
type Token struct {
	Name      string  // e.g., "neutral.spine.bg_panel" or "semantic.info.default"
	TargetL   float64
	TargetC   float64
	Hue       float64
	PostClipC float64 // post-gamut-clip C
	R, G, B   uint8
}

// Hex returns "#rrggbb" lower-case.
func (inst Token) Hex() (s string) {
	s = fmt.Sprintf("#%02x%02x%02x", inst.R, inst.G, inst.B)
	return
}

// Resolve produces the token list in deterministic order: neutral.spine
// keys (sorted), then semantic roles (sorted), each with subtle/default/strong.
func Resolve(file *File) (tokens []Token, err error) {
	// Neutral spine — sorted keys for determinism.
	spineKeys := make([]string, 0, len(file.Neutral.Spine))
	for k := range file.Neutral.Spine {
		spineKeys = append(spineKeys, k)
	}
	sort.Strings(spineKeys)
	for _, k := range spineKeys {
		l := file.Neutral.Spine[k]
		var t Token
		t, err = makeToken("neutral.spine."+k, l, file.Neutral.Chroma, file.Neutral.Hue)
		if err != nil {
			return
		}
		tokens = append(tokens, t)
	}

	// Semantic roles — sorted role names; subtle/default/strong order.
	roleNames := make([]string, 0, len(file.Semantic))
	for name := range file.Semantic {
		roleNames = append(roleNames, name)
	}
	sort.Strings(roleNames)
	for _, role := range roleNames {
		r := file.Semantic[role]
		for _, e := range []struct {
			name string
			lc   LC
		}{
			{"subtle", r.Subtle},
			{"default", r.Default},
			{"strong", r.Strong},
		} {
			var t Token
			t, err = makeToken("semantic."+role+"."+e.name, e.lc.L, e.lc.C, r.Hue)
			if err != nil {
				return
			}
			tokens = append(tokens, t)
		}
	}
	return
}

func makeToken(name string, l, c, h float64) (t Token, err error) {
	if l < 0.0 || l > 1.0 {
		err = fmt.Errorf("token %s: L=%v out of [0, 1]", name, l)
		return
	}
	if c < 0.0 {
		err = fmt.Errorf("token %s: C=%v negative", name, c)
		return
	}
	// CSS Color 4 §13 gamut mapping (was: naive chroma stepping per
	// ADR-0033 §SD5 — replaced after adversarial review).
	r, g, b, postC := gma.MapToSrgbU8(l, c, h)
	t = Token{
		Name:      name,
		TargetL:   l,
		TargetC:   c,
		Hue:       h,
		PostClipC: postC,
		R:         r,
		G:         g,
		B:         b,
	}
	return
}
