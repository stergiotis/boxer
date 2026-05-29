//go:build llm_generated_opus47

package codeview

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
)

// mdColors is the per-category palette for the markdown highlighter,
// VS Code dark+ inspired and visually matched to the SQL / JSON / Go
// palettes. Retained holders are interned at init() and reused across
// frames.
var mdColors [markdownhighlight.CategoryCount]color.Color

func init() {
	defaultColor := internRgb(212, 212, 212) // light gray
	blue := internRgb(86, 156, 214)          // headings, blockquote bars
	teal := internRgb(78, 201, 176)          // heading text, callout type
	lightBlue := internRgb(156, 220, 254)    // link labels, wikilink targets
	yellow := internRgb(220, 220, 170)       // emphasis, list markers
	orange := internRgb(206, 145, 120)       // strong, URLs, frontmatter values
	green := internRgb(181, 206, 168)        // code block bodies
	dimGreen := internRgb(106, 153, 85)      // inline code, fence delim, comments
	purple := internRgb(197, 134, 192)       // ==highlight==
	gray := internRgb(128, 128, 128)         // strikethrough, frontmatter scaffolding
	bang := internRgb(231, 144, 78)          // ! embed marker — warm accent

	mdColors[markdownhighlight.CategoryPlain] = defaultColor
	mdColors[markdownhighlight.CategoryWhitespace] = defaultColor
	mdColors[markdownhighlight.CategoryHeadingMarker] = blue
	mdColors[markdownhighlight.CategoryHeadingText] = teal
	mdColors[markdownhighlight.CategoryStrongDelim] = orange
	mdColors[markdownhighlight.CategoryStrongText] = orange
	mdColors[markdownhighlight.CategoryEmphasisDelim] = yellow
	mdColors[markdownhighlight.CategoryEmphasisText] = yellow
	mdColors[markdownhighlight.CategoryStrikeDelim] = gray
	mdColors[markdownhighlight.CategoryStrikeText] = gray
	mdColors[markdownhighlight.CategoryHighlightDelim] = purple
	mdColors[markdownhighlight.CategoryHighlightText] = purple
	mdColors[markdownhighlight.CategoryInlineCodeDelim] = dimGreen
	mdColors[markdownhighlight.CategoryInlineCodeText] = green
	mdColors[markdownhighlight.CategoryFenceDelim] = dimGreen
	mdColors[markdownhighlight.CategoryFenceLang] = teal
	mdColors[markdownhighlight.CategoryCodeBlockBody] = green
	mdColors[markdownhighlight.CategoryBlockquoteMarker] = blue
	mdColors[markdownhighlight.CategoryListMarker] = yellow
	mdColors[markdownhighlight.CategoryLinkPunct] = defaultColor
	mdColors[markdownhighlight.CategoryLinkLabel] = lightBlue
	mdColors[markdownhighlight.CategoryLinkUrl] = orange
	mdColors[markdownhighlight.CategoryThematicBreak] = blue
	mdColors[markdownhighlight.CategoryFrontmatterDelim] = gray
	mdColors[markdownhighlight.CategoryFrontmatterKey] = lightBlue
	mdColors[markdownhighlight.CategoryFrontmatterValue] = orange
	mdColors[markdownhighlight.CategoryWikilinkPunct] = defaultColor
	mdColors[markdownhighlight.CategoryWikilinkTarget] = lightBlue
	mdColors[markdownhighlight.CategoryEmbedMarker] = bang
	mdColors[markdownhighlight.CategoryCalloutMarker] = blue
	mdColors[markdownhighlight.CategoryCalloutType] = teal
	mdColors[markdownhighlight.CategoryCommentDelim] = dimGreen
	mdColors[markdownhighlight.CategoryCommentText] = dimGreen
	mdColors[markdownhighlight.CategoryRawHtml] = gray
	mdColors[markdownhighlight.CategoryTablePipe] = dimGreen
	mdColors[markdownhighlight.CategoryTableAlign] = blue
	mdColors[markdownhighlight.CategoryTableHeaderText] = teal
	mdColors[markdownhighlight.CategoryTableCellText] = defaultColor
	mdColors[markdownhighlight.CategoryTaskMark] = yellow
}

// BuildMarkdown canonicalises and highlights markdown source, returning a
// retained CodeViewJob. Each call re-renders; use PrepareMarkdown for
// static documents.
//
// IMPORTANT: the rendered text is a *canonical* form of the input, not
// the source verbatim. gofmt-style: lists become `-`, emphasis becomes
// `*` / `**`, frontmatter keys sort alphabetically, indented code
// blocks lose their 4-space marker. The text displayed in the CodeView
// is what this function returns, not what was passed in. See package
// [markdownhighlight] for the full canonicalisation rules.
func BuildMarkdown(src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	canonical, spans := markdownhighlight.Highlight([]byte(src))
	job := c.CodeViewJob(canonical)
	for _, s := range spans {
		job = job.Section(uint32(s.Start), uint32(s.Stop), mdColors[s.Category])
	}
	return job.Keep()
}

// PrepareMarkdown is identical to BuildMarkdown — use this name for
// static / global markdown where the retained holder is built once and
// reused across frames.
func PrepareMarkdown(src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return BuildMarkdown(src)
}
