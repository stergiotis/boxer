package codeview

import (
	"strings"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
)

// MarkdownFontScale is the size factor a heading of each level renders at in a
// formatted source pane, indexed by level-1 (ADR-0334 §SD6). Factors of the
// editor's base font size, so the pane keeps one knob — the base — and the
// hierarchy rides on it. Levels 5 and 6 stay at body size: by then the
// distinction is the marker's, as it is in every rendered view.
var MarkdownFontScale = [6]float32{1.7, 1.45, 1.25, 1.1, 1.0, 1.0}

// mdMonospace marks the categories whose bytes must line up by column:
// code, fences, frontmatter, raw HTML and tables. Everything else is prose
// and takes the body family.
var mdMonospace = func() (m [markdownhighlight.CategoryCount]bool) {
	for _, cat := range []markdownhighlight.CategoryE{
		markdownhighlight.CategoryInlineCodeDelim,
		markdownhighlight.CategoryInlineCodeText,
		markdownhighlight.CategoryFenceDelim,
		markdownhighlight.CategoryFenceLang,
		markdownhighlight.CategoryCodeBlockBody,
		markdownhighlight.CategoryFrontmatterDelim,
		markdownhighlight.CategoryFrontmatterKey,
		markdownhighlight.CategoryFrontmatterValue,
		markdownhighlight.CategoryRawHtml,
		markdownhighlight.CategoryTablePipe,
		markdownhighlight.CategoryTableAlign,
		markdownhighlight.CategoryTableHeaderText,
		markdownhighlight.CategoryTableCellText,
	} {
		m[cat] = true
	}
	return
}()

// MarkdownFontSections derives the font overlay of a formatted markdown source
// pane from the lex-tier spans somebody already computed for its colours
// (ADR-0334 §SD6): the whole buffer in the proportional body family, each ATX
// heading line scaled by [MarkdownFontScale], and every column-aligned span —
// code, fences, frontmatter, tables, raw HTML — in monospace, at the size of
// the line it sits on. The result is appended to dst and is meant for
// [BuildStyledSections], beside whatever decorations the caller adds.
//
// Whitespace between two monospace spans is monospace too, so the indentation
// inside a fence and the padding inside a table row keep their columns; a run
// of adjacent monospace spans becomes one section. Heading level is the
// marker's length, which is what the lexer emits for an ATX marker — setext
// headings read as prose in the source tier (ADR-0178) and stay body-sized.
//
// `src` must be the exact text the spans describe.
func MarkdownFontSections(src string, spans []markdownhighlight.Span, dst []StyledSection) []StyledSection {
	if len(src) == 0 {
		return dst
	}
	dst = append(dst, StyledSection{Start: 0, Stop: uint32(len(src)), Flags: StyleFont, Family: FontProportional, Size: 1.0})

	// Heading lines first, so a monospace span can look its line's scale up.
	type headingLine struct {
		start, stop int
		scale       float32
	}
	var headings []headingLine
	for _, sp := range spans {
		if sp.Category != markdownhighlight.CategoryHeadingMarker {
			continue
		}
		level := int(sp.Stop - sp.Start)
		if level < 1 {
			continue
		}
		if level > len(MarkdownFontScale) {
			level = len(MarkdownFontScale)
		}
		start := int(sp.Start)
		stop := len(src)
		if nl := strings.IndexByte(src[start:], '\n'); nl >= 0 {
			stop = start + nl
		}
		if stop <= start {
			continue
		}
		scale := MarkdownFontScale[level-1]
		headings = append(headings, headingLine{start, stop, scale})
		dst = append(dst, StyledSection{Start: uint32(start), Stop: uint32(stop), Flags: StyleFont, Family: FontProportional, Size: scale})
	}
	scaleAt := func(pos int) float32 {
		for _, h := range headings {
			if pos >= h.start && pos < h.stop {
				return h.scale
			}
		}
		return 1.0
	}

	// Monospace runs: a span is mono by category, or whitespace fenced by
	// mono on both sides; adjacent mono spans coalesce.
	isMono := func(i int) bool {
		sp := spans[i]
		if mdMonospace[sp.Category] {
			return true
		}
		if sp.Category != markdownhighlight.CategoryWhitespace || i == 0 || i+1 >= len(spans) {
			return false
		}
		return mdMonospace[spans[i-1].Category] && mdMonospace[spans[i+1].Category]
	}
	runStart, runStop := -1, -1
	flush := func() {
		if runStart >= 0 && runStop > runStart {
			dst = append(dst, StyledSection{Start: uint32(runStart), Stop: uint32(runStop), Flags: StyleFont, Family: FontMonospace, Size: scaleAt(runStart)})
		}
		runStart, runStop = -1, -1
	}
	for i, sp := range spans {
		if !isMono(i) {
			flush()
			continue
		}
		// A run never crosses a line: the scale is per line, and so is a
		// table row or a fence line. The lexer's spans do carry newlines (a
		// fence line is one span, newline included), so a span is walked
		// line by line and the newline byte itself belongs to no run.
		start, stop := int(sp.Start), int(sp.Stop)
		for start < stop {
			pieceStop := stop
			if nl := strings.IndexByte(src[start:stop], '\n'); nl >= 0 {
				pieceStop = start + nl
			}
			if pieceStop > start {
				if runStart >= 0 && start != runStop {
					flush()
				}
				if runStart < 0 {
					runStart = start
				}
				runStop = pieceStop
			}
			if pieceStop == stop {
				break
			}
			start = pieceStop + 1
		}
	}
	flush()
	return dst
}
