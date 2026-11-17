package fontresolve

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeFontconfig struct {
	match    map[string][2]string
	charsets map[string]string
	noQuery  bool
}

func (inst fakeFontconfig) Match(pattern string) (file string, family string, err error) {
	if m, ok := inst.match[pattern]; ok {
		return m[0], m[1], nil
	}
	return "/usr/share/fonts/DejaVuSans.ttf", "DejaVu Sans", nil
}

func (inst fakeFontconfig) Charset(file string) (charset string, err error) {
	if inst.noQuery {
		return "", ErrUnavailable
	}
	return inst.charsets[file], nil
}

func existsIn(files ...string) func(string) bool {
	return func(p string) bool {
		for _, f := range files {
			if f == p {
				return true
			}
		}
		return false
	}
}

func TestResolveThroughFontconfig(t *testing.T) {
	fc := fakeFontconfig{
		match: map[string][2]string{
			"Noto Sans":             {"/f/NotoSans.ttf", "Noto Sans"},
			"Noto Sans:bold":        {"/f/NotoSans-Bold.ttf", "Noto Sans"},
			"DejaVu Sans Mono":      {"/f/DejaVuSansMono.ttf", "DejaVu Sans Mono"},
			"Liberation Mono":       {"/f/LiberationMono.ttf", "Liberation Mono"},
			"Noto Sans Mono CJK JP": {"/f/NotoSansCJK.ttc", "Noto Sans Mono CJK JP"},
		},
		charsets: map[string]string{
			"/f/DejaVuSansMono.ttf": "20-7e 2423 24c0-24ff",
			"/f/LiberationMono.ttf": "20-7e 2000-27bf",
		},
	}
	got, notes := Resolver{Fontconfig: fc, Exists: existsIn("/f/NotoSans.ttf", "/f/NotoSans-Bold.ttf", "/f/DejaVuSansMono.ttf", "/f/LiberationMono.ttf", "/f/NotoSansCJK.ttc")}.Resolve()
	assert.Equal(t, Fonts{Main: "/f/NotoSans.ttf", Bold: "/f/NotoSans-Bold.ttf", Mono: "/f/LiberationMono.ttf", Fallback: "/f/NotoSansCJK.ttc"}, got,
		"DejaVu Sans Mono lacks the box-drawing block, so Liberation Mono is next")
	assert.Empty(t, notes)
}

func TestResolveRejectsSubstitutes(t *testing.T) {
	fc := fakeFontconfig{match: map[string][2]string{
		"Noto Sans Mono CJK JP": {"/f/NotoSans.ttf", "Noto Sans"},
	}}
	got, notes := Resolver{Fontconfig: fc, Exists: existsIn("/f/NotoSans.ttf", "/bundle/NotoSansCJK-Regular.ttc"), ExtraFallback: []string{"/bundle/NotoSansCJK-Regular.ttc"}}.Resolve()
	assert.Equal(t, "", got.Main, "fontconfig's DejaVu Sans is not Noto Sans")
	assert.Equal(t, "", got.Mono)
	assert.Equal(t, "/bundle/NotoSansCJK-Regular.ttc", got.Fallback, "plain Noto Sans is not CJK; the bundle's copy is the last resort")
	all := strings.Join(notes, "\n")
	assert.Contains(t, all, "no Noto Sans")
	assert.Contains(t, all, "box-drawing")
	assert.NotContains(t, all, "CJK")
}

func TestResolveWithoutFontconfig(t *testing.T) {
	got, notes := Resolver{Exists: existsIn("/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf")}.Resolve()
	assert.Equal(t, "/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf", got.Main)
	assert.Equal(t, "", got.Fallback)
	assert.Len(t, notes, 2)
}

func TestMonoWithoutFcQuery(t *testing.T) {
	fc := fakeFontconfig{noQuery: true, match: map[string][2]string{"DejaVu Sans Mono": {"/f/m.ttf", "DejaVu Sans Mono"}}}
	got, _ := Resolver{Fontconfig: fc, Exists: existsIn("/f/m.ttf")}.Resolve()
	assert.Equal(t, "/f/m.ttf", got.Mono, "a charset fontconfig cannot tell is taken")
}

func TestCoversBoxDrawing(t *testing.T) {
	assert.True(t, CoversBoxDrawing("20-7e 2423 2500-262f"))
	assert.True(t, CoversBoxDrawing("20-7e 2000-27bf"))
	assert.True(t, CoversBoxDrawing("2500-2540 2541-257f"))
	assert.False(t, CoversBoxDrawing("20-7e 2500-2550"))
	assert.False(t, CoversBoxDrawing("20-7e 2501-25ff"))
	assert.False(t, CoversBoxDrawing(""))
}
