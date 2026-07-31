package codeview

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexhighlight"
)

// regexColors is the per-category palette for the RE2 highlighter
// (ADR-0015), in the same VS Code dark+ family as the SQL / JSON / Go
// palettes. Retained holders are interned at init() and reused across
// frames.
//
// CategoryGroup is the exception: group parens resolve their colour from
// the span's nesting depth through regexDepthColors, so the entry here is
// only the fallback.
var regexColors [regexhighlight.CategoryError + 1]color.Color

// regexDepthColors is the bracket-pair cycle: group parens take their
// colour from the span's Depth, so a nested pattern's parens pair up by
// eye (ADR-0015 §SD2).
//
// Taken from styletokens.QualitativeCycle, the same source the Preview
// tab's per-capture-group cells use, so the two cycle in step by ordinal
// rather than merely resembling each other.
//
// This diverged into a hand-picked set until ADR-0156. The reason given
// was sound at the time — the then-current palette (batlowS) was chosen
// for fills and opened on a near-black navy, invisible as foreground on
// the dark editor background — and it no longer holds now that every
// cycle entry clears 3:1 as a foreground. Rejoining also fixes something
// the hand-picked set got wrong: its gold and orchid were ΔE 1.2 apart
// under deuteranopia, so consecutive bracket depths were nearly
// indistinguishable to a deuteranope. The cycle's first four measure 14.6.
//
// Four entries: what matters is that *consecutive* depths differ
// strongly, which they do; a pattern nested more than four deep repeats
// the cycle, exactly as bracket-pair colourisation does elsewhere.
var regexDepthColors [4]color.Color

var regexSpec highlighterSpec

var regexListSpec highlighterSpec

func init() {
	defaultColor := internRgb(212, 212, 212) // light gray — plain literals
	blue := internRgb(86, 156, 214)          // anchors: zero-width assertions
	purple := internRgb(197, 134, 192)       // . and | — the control operators
	yellow := internRgb(220, 220, 170)       // quantifiers and inline flags
	orange := internRgb(206, 145, 120)       // escapes (matches the string-lit family)
	teal := internRgb(78, 201, 176)          // named classes and group names
	green := internRgb(181, 206, 168)        // class brackets and the range dash
	lightBlue := internRgb(156, 220, 254)    // class members
	red := internRgb(244, 71, 71)            // the two byte-level certainties

	regexColors[regexhighlight.CategoryLiteral] = defaultColor
	regexColors[regexhighlight.CategoryMeta] = purple
	regexColors[regexhighlight.CategoryQuantifier] = yellow
	regexColors[regexhighlight.CategoryAnchor] = blue
	regexColors[regexhighlight.CategoryEscape] = orange
	regexColors[regexhighlight.CategoryClassName] = teal
	regexColors[regexhighlight.CategoryClassDelim] = green
	regexColors[regexhighlight.CategoryClassLiteral] = lightBlue
	regexColors[regexhighlight.CategoryGroup] = defaultColor
	regexColors[regexhighlight.CategoryGroupName] = teal
	regexColors[regexhighlight.CategoryFlags] = yellow
	regexColors[regexhighlight.CategoryError] = red

	for i := range regexDepthColors {
		q := styletokens.QualitativeCycle(i)
		regexDepthColors[i] = internRgb(q.R, q.G, q.B)
	}

	regexSpec = highlighterSpec{
		highlight:   regexHighlight,
		gutterColor: internRgb(96, 96, 96), // dim gray — visually below source text
		plainColor:  defaultColor,
	}
	regexListSpec = highlighterSpec{
		highlight:   regexListHighlight,
		gutterColor: internRgb(96, 96, 96),
		plainColor:  defaultColor,
	}
}

func regexHighlight(src string) (out []section) {
	return regexSpansToSections(regexhighlight.Highlight(src))
}

// regexListHighlight lexes one independent pattern per line — the shape
// the multi-pattern editor holds. An unclosed `(` on one line must not
// colour the next (ADR-0015 §SD3).
func regexListHighlight(src string) (out []section) {
	return regexSpansToSections(regexhighlight.HighlightLines(src))
}

func regexSpansToSections(spans []regexhighlight.Span) (out []section) {
	out = make([]section, len(spans))
	for i, s := range spans {
		col := regexColors[s.Category]
		if s.Category == regexhighlight.CategoryGroup {
			col = regexDepthColors[int(s.Depth)%len(regexDepthColors)]
		}
		out[i] = section{
			start: uint32(s.Start),
			stop:  uint32(s.Stop),
			col:   col,
		}
	}
	return
}

// BuildRegex highlights one RE2 pattern and returns a retained
// CodeViewJob. Every call re-lexes.
//
// This is the per-keystroke span source for the ADR-0130 editor path
// (TextEdit.HighlightJob), and it is deliberately uncached for the same
// reason as [BuildSqlLex]: editor content is new on every keystroke, so
// the ADR-0125 memo would only churn. Unlike the read-only builders, no
// tab expansion is applied — the job's text must equal the editor buffer
// byte-for-byte for the Rust-side reconcile to line up.
//
// There is no semantic/async second tier here (ADR-0015 §SD4): the lexer
// is O(n) over a pattern of tens of bytes, orders of magnitude below the
// SQL parse that forced ADR-0130's split.
func BuildRegex(src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return build(regexSpec, src)
}

// PrepareRegex highlights one RE2 pattern through the package memo: the
// same pattern prepared again returns the same retained holder without
// re-lexing (ADR-0125). Prefer this for read-only surfaces that show the
// same pattern across frames; use [BuildRegex] for an editor buffer.
func PrepareRegex(src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return memo.prepare(memoKey{lang: langRegex, src: src}, func() typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
		return build(regexSpec, src)
	})
}

// BuildRegexList highlights a newline-separated *list* of RE2 patterns —
// one independent pattern per line, with group depth reset at each
// newline. Every call re-lexes; this is the editor path, see [BuildRegex].
//
// Named List rather than Lines on purpose: in this package the `*Lines`
// suffix means a line-numbered gutter window over one document
// ([BuildGoLines]), which is a different operation.
func BuildRegexList(src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return build(regexListSpec, src)
}

// PrepareRegexList highlights a pattern list through the package memo
// (ADR-0125). Keyed distinctly from [PrepareRegex], so the same source
// prepared both ways does not serve one lexing for the other.
func PrepareRegexList(src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return memo.prepare(memoKey{lang: langRegexList, src: src}, func() typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
		return build(regexListSpec, src)
	})
}
