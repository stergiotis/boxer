// Package fontresolve finds the font files the imzero2 render head draws with:
// Noto Sans for text and its bold face, a fixed-width face that covers the box-drawing block, and
// Noto Sans CJK as the fallback for the glyphs the others lack. It asks
// fontconfig first, guarded by the family it answers with, and falls back to
// fixed paths, then to candidates the caller names (such as a bundle's own
// copy).
package fontresolve

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// ErrUnavailable reports that fontconfig, or the part of it a question needs,
// is not installed.
var ErrUnavailable = errors.New("fontconfig is not available")

// FontconfigI is the part of fontconfig the resolver asks.
type FontconfigI interface {
	// Match returns the file and family fontconfig answers pattern with. It
	// always answers, with an unrelated family when nothing matches.
	Match(pattern string) (file string, family string, err error)
	// Charset returns the charset of file as fontconfig prints it: hex code
	// point ranges separated by spaces. ErrUnavailable when it cannot tell.
	Charset(file string) (charset string, err error)
}

// Fonts are the files handed to the render head; an empty one is left to the
// head's own default.
type Fonts struct {
	// Main is the proportional text face (--mainFontTTF).
	Main string
	// Bold is the bold face of Main's family (--boldFontTTF); without it
	// headings and strong runs draw at regular weight.
	Bold string
	// Mono is the fixed-width face (--monoFontTTF).
	Mono string
	// Fallback is the face for glyphs the others lack, CJK in particular
	// (--fallbackFontTTF).
	Fallback string
}

// Resolver resolves Fonts.
type Resolver struct {
	// Fontconfig answers the family queries; nil uses the fixed paths only.
	Fontconfig FontconfigI
	// Exists reports whether a font file is there; nil uses os.Stat.
	Exists func(path string) bool
	// ExtraFallback are CJK files tried after fontconfig and the fixed paths.
	ExtraFallback []string
}

// MainFamily, MonoFamilies and FallbackFamily are the families asked for, in
// order of preference.
const (
	MainFamily     = "Noto Sans"
	BoldPattern    = "Noto Sans:bold"
	FallbackFamily = "Noto Sans Mono CJK JP"
)

// MonoFamilies are the fixed-width families tried in order: DejaVu Sans Mono is
// the most widely installed face covering the box-drawing block.
var MonoFamilies = []string{"DejaVu Sans Mono", "Liberation Mono", "Adwaita Mono"}

// The fixed paths, tried when fontconfig has no genuine answer: Fedora's,
// Debian's and Arch's layouts.
var (
	mainPaths = []string{
		"/usr/share/fonts/google-noto-vf/NotoSans[wght].ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
		"/usr/share/fonts/noto/NotoSans-Regular.ttf",
	}
	boldPaths = []string{
		"/usr/share/fonts/truetype/noto/NotoSans-Bold.ttf",
		"/usr/share/fonts/noto/NotoSans-Bold.ttf",
	}
	fallbackPaths = []string{
		"/usr/share/fonts/google-noto-sans-mono-cjk-vf-fonts/NotoSansMonoCJK-VF.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc",
	}
)

// Resolve finds each face, leaving empty the ones it cannot find. notes say
// which it could not find and what the head does without them.
func (inst Resolver) Resolve() (out Fonts, notes []string) {
	out.Main = inst.Family(MainFamily, MainFamily)
	if out.Main == "" {
		out.Main = inst.first(mainPaths)
	}
	if out.Main == "" {
		notes = append(notes, "no Noto Sans on this host: the GUI draws text with egui's built-in font (install fonts-noto-core, google-noto-sans-vf-fonts or noto-fonts)")
	}
	out.Bold = inst.Family(BoldPattern, MainFamily)
	if out.Bold == "" {
		out.Bold = inst.first(boldPaths)
	}
	for _, fam := range MonoFamilies {
		if out.Mono = inst.mono(fam); out.Mono != "" {
			break
		}
	}
	if out.Mono == "" {
		notes = append(notes, "no fixed-width face covering the box-drawing block ("+strings.Join(MonoFamilies, ", ")+"): the head uses the main font, and query results and tables do not line up")
	}
	out.Fallback = inst.Family(FallbackFamily, "CJK")
	if out.Fallback == "" {
		out.Fallback = inst.first(fallbackPaths)
	}
	if out.Fallback == "" {
		out.Fallback = inst.first(inst.ExtraFallback)
	}
	if out.Fallback == "" {
		notes = append(notes, "no Noto Sans CJK: CJK text draws as empty boxes (install fonts-noto-cjk)")
	}
	return
}

// Family asks fontconfig for pattern and takes the answer only when its
// family contains want and the file is there; fontconfig answers every query,
// with an unrelated family when nothing matches.
func (inst Resolver) Family(pattern string, want string) (file string) {
	if inst.Fontconfig == nil {
		return
	}
	f, fam, err := inst.Fontconfig.Match(pattern)
	if err != nil || !strings.Contains(fam, want) || !inst.exists(f) {
		return
	}
	file = f
	return
}

// mono is family with one more requirement: the face covers the box-drawing
// block (U+2500…U+257F). Without it those characters come from a fallback
// with another advance, and a table's frame does not close. A face whose
// charset fontconfig cannot tell is taken.
func (inst Resolver) mono(fam string) (file string) {
	f := inst.Family(fam, fam)
	if f == "" {
		return
	}
	charset, err := inst.Fontconfig.Charset(f)
	switch {
	case errors.Is(err, ErrUnavailable):
		file = f
	case err != nil:
	case CoversBoxDrawing(charset):
		file = f
	}
	return
}

// The box-drawing block.
const (
	boxDrawingFirst = 0x2500
	boxDrawingLast  = 0x257F
)

// CoversBoxDrawing reports whether a fontconfig charset (hex code points and
// ranges, "2500-262f", separated by spaces) covers the whole box-drawing
// block.
func CoversBoxDrawing(charset string) (ok bool) {
	next := rune(boxDrawingFirst)
	for _, field := range strings.Fields(charset) {
		lo, hi, isRange := strings.Cut(field, "-")
		if !isRange {
			hi = lo
		}
		a, errA := strconv.ParseUint(lo, 16, 32)
		b, errB := strconv.ParseUint(hi, 16, 32)
		if errA != nil || errB != nil || rune(a) > next {
			continue
		}
		if rune(b) >= next {
			next = rune(b) + 1
		}
		if next > boxDrawingLast {
			return true
		}
	}
	return
}

func (inst Resolver) first(paths []string) (file string) {
	for _, p := range paths {
		if inst.exists(p) {
			return p
		}
	}
	return
}

func (inst Resolver) exists(p string) (ok bool) {
	if p == "" {
		return
	}
	if inst.Exists != nil {
		return inst.Exists(p)
	}
	st, err := os.Stat(p)
	ok = err == nil && st.Mode().IsRegular()
	return
}
