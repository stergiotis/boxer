package codeview

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/gohighlight"
)

// goTabExpansion is what we substitute for each '\t' byte before
// highlighting. egui's LayoutJob renders '\t' with inconsistent width
// for our font setup, so gofmt-tabbed source loses its indentation.
// Expanding to spaces *before* the lex pass keeps the section byte
// offsets aligned with the text the renderer actually receives.
const goTabExpansion = "    "

// goColors is the per-category palette for the Go highlighter,
// VS Code dark+ inspired and visually matched to the SQL / JSON palettes.
// Retained holders are interned at init() and reused across frames.
var goColors [gohighlight.CategoryWhitespace + 1]color.Color

var goSpec highlighterSpec

func init() {
	defaultColor := internRgb(212, 212, 212) // light gray
	blue := internRgb(86, 156, 214)          // keywords, control flow
	teal := internRgb(78, 201, 176)          // type names
	lightBlue := internRgb(156, 220, 254)    // identifiers / fields
	yellow := internRgb(220, 220, 170)       // function names
	purple := internRgb(197, 134, 192)       // numeric / control literals (true/false/nil)
	orange := internRgb(206, 145, 120)       // string literals
	rune_ := internRgb(206, 145, 120)        // rune literals (same family as strings)
	number := internRgb(181, 206, 168)       // number literals
	dimGreen := internRgb(106, 153, 85)      // comments
	docGreen := internRgb(96, 139, 78)       // doc comments — slightly darker
	buildTag := internRgb(155, 155, 155)     // //go: directives — neutral
	gold := internRgb(220, 220, 170)         // labels — share function color

	goColors[gohighlight.CategoryPlain] = defaultColor
	goColors[gohighlight.CategoryKeyword] = blue
	goColors[gohighlight.CategoryOperator] = defaultColor
	goColors[gohighlight.CategoryPunctuation] = defaultColor
	goColors[gohighlight.CategoryIdentifier] = lightBlue
	goColors[gohighlight.CategoryPackageName] = teal
	goColors[gohighlight.CategoryTypeName] = teal
	goColors[gohighlight.CategoryFuncDecl] = yellow
	goColors[gohighlight.CategoryFuncCall] = yellow
	goColors[gohighlight.CategoryFieldName] = lightBlue
	goColors[gohighlight.CategoryBuiltin] = blue
	goColors[gohighlight.CategoryConstName] = purple
	goColors[gohighlight.CategoryLabel] = gold
	goColors[gohighlight.CategoryStringLit] = orange
	goColors[gohighlight.CategoryNumberLit] = number
	goColors[gohighlight.CategoryRuneLit] = rune_
	goColors[gohighlight.CategoryBoolLit] = purple
	goColors[gohighlight.CategoryNilLit] = purple
	goColors[gohighlight.CategoryComment] = dimGreen
	goColors[gohighlight.CategoryDocComment] = docGreen
	goColors[gohighlight.CategoryImportPath] = orange
	goColors[gohighlight.CategoryBuildTag] = buildTag
	goColors[gohighlight.CategoryWhitespace] = defaultColor

	goGutterColor := internRgb(96, 96, 96) // dim gray — visually below source text

	goSpec = highlighterSpec{
		highlight:   goHighlight,
		gutterColor: goGutterColor,
		plainColor:  defaultColor,
		tabReplace:  goTabExpansion,
	}
}

func goHighlight(src string) (out []section) {
	spans := gohighlight.Highlight(src)
	out = make([]section, len(spans))
	for i, s := range spans {
		out[i] = section{
			start: uint32(s.Start),
			stop:  uint32(s.Stop),
			col:   goColors[s.Category],
		}
	}
	return
}

// BuildGo highlights Go source and returns a retained CodeViewJob. Every call
// re-highlights — use it for one-shot work, or when you already hold a cheaper
// key than the source text. Use [PrepareGo] otherwise.
func BuildGo(src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return build(goSpec, src)
}

// PrepareGo highlights Go source through the package memo: the same source
// prepared again returns the same retained holder without re-highlighting
// (ADR-0125).
func PrepareGo(src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return memo.prepare(memoKey{lang: langGo, src: src}, func() typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
		return build(goSpec, src)
	})
}

// BuildGoLines highlights src and renders the byte slice covering 1-based
// lines [firstLine, lastLine] (inclusive) with a right-aligned line-number
// gutter prefixed to each line. The full source is parsed so AST
// refinement applies — spans that cross the window boundary are clipped at
// the edges.
//
// firstLine/lastLine are clamped to the source's line range. An
// out-of-bounds window returns an empty retained holder.
func BuildGoLines(src string, firstLine int32, lastLine int32) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return buildLines(goSpec, src, firstLine, lastLine)
}

// PrepareGoLines renders a line window through the package memo: the same
// (source, window) prepared again returns the same retained holder without
// re-highlighting (ADR-0125). The window is part of the key, so two windows over
// one source are two entries — and neither collides with [PrepareGo] over that
// source, which would otherwise serve the whole file for PrepareGoLines(src, 0, 0).
func PrepareGoLines(src string, firstLine int32, lastLine int32) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return memo.prepare(
		memoKey{lang: langGoLines, firstLine: firstLine, lastLine: lastLine, src: src},
		func() typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
			return buildLines(goSpec, src, firstLine, lastLine)
		})
}
