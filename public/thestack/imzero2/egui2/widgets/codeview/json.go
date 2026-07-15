package codeview

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jsonhighlight"
)

// jsonColors is the per-category palette for the JSON highlighter,
// matching the SQL and Go palettes so the three highlighters share a
// visual family. Retained holders are interned at init() and reused
// across frames.
var jsonColors [jsonhighlight.CategoryWhitespace + 1]color.Color

var jsonSpec highlighterSpec

func init() {
	defaultColor := internRgb(212, 212, 212) // light gray
	lightBlue := internRgb(156, 220, 254)    // object keys (matches Go field-name)
	orange := internRgb(206, 145, 120)       // string values (matches Go string-lit)
	number := internRgb(181, 206, 168)       // number literals (matches Go number-lit)
	purple := internRgb(197, 134, 192)       // bool / null (matches Go bool/nil)

	jsonColors[jsonhighlight.CategoryPlain] = defaultColor
	jsonColors[jsonhighlight.CategoryPunctuation] = defaultColor
	jsonColors[jsonhighlight.CategoryKey] = lightBlue
	jsonColors[jsonhighlight.CategoryStringLit] = orange
	jsonColors[jsonhighlight.CategoryNumberLit] = number
	jsonColors[jsonhighlight.CategoryBoolLit] = purple
	jsonColors[jsonhighlight.CategoryNullLit] = purple
	jsonColors[jsonhighlight.CategoryWhitespace] = defaultColor

	jsonSpec = highlighterSpec{
		highlight:   jsonHighlight,
		gutterColor: defaultColor,
		plainColor:  defaultColor,
	}
}

func jsonHighlight(src string) (out []section) {
	spans := jsonhighlight.Highlight(src)
	out = make([]section, len(spans))
	for i, s := range spans {
		out[i] = section{
			start: uint32(s.Start),
			stop:  uint32(s.Stop),
			col:   jsonColors[s.Category],
		}
	}
	return
}

// BuildJson highlights JSON and returns a retained CodeViewJob. Every call
// re-tokenises — use it for one-shot work, or when you already hold a cheaper
// key than the source text. Use [PrepareJson] otherwise.
func BuildJson(src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return build(jsonSpec, src)
}

// PrepareJson highlights JSON through the package memo: the same source
// prepared again returns the same retained holder without re-tokenising
// (ADR-0125).
func PrepareJson(src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return memo.prepare(memoKey{lang: langJSON, src: src}, func() job {
		return build(jsonSpec, src)
	})
}
