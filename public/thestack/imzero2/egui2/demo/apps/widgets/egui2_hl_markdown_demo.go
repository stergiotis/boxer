//go:build llm_generated_opus47

package widgets

import (
	"fmt"
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// demoMarkdownResolver supplies inline image pixels for the markdown
// demo. Embeds [resolver.NoopResolver] so wikilinks and note embeds
// keep the default `/page` URL behaviour; LoadImage returns procedural
// RGBA8 buffers for the demo's known refs and ok=false for everything
// else (which falls back to the 🖼-glyph hyperlink path).
type demoMarkdownResolver struct {
	resolver.NoopResolver
}

func (inst demoMarkdownResolver) LoadImage(ref string) (pixels []uint32, widthPx uint32, heightPx uint32, ok bool) {
	switch ref {
	case "diagram.png":
		pixels, widthPx, heightPx = buildDemoMdImage(0)
		ok = true
	case "architecture.png":
		pixels, widthPx, heightPx = buildDemoMdImage(1)
		ok = true
	case "hero.png":
		pixels, widthPx, heightPx = buildDemoMdImage(2)
		ok = true
	}
	return
}

// buildDemoMdImage returns a 128×80 procedural RGBA8 buffer used by the
// markdown image demo. The variant index picks a colour palette so the
// three demo images are visually distinct from each other (and from
// the image-widget demo's teal/yellow checker).
func buildDemoMdImage(variant uint8) (pixels []uint32, w uint32, h uint32) {
	w, h = 128, 80
	pixels = make([]uint32, int(w*h))
	var primary, secondary uint32
	switch variant {
	case 0:
		primary, secondary = 0x4ea1ffff, 0x21918cff // blue / teal
	case 1:
		primary, secondary = 0xff8c4eff, 0xfde725ff // orange / yellow
	default:
		primary, secondary = 0xa970ffff, 0xff70c1ff // purple / pink
	}
	const bg = uint32(0x141821ff)
	const border = uint32(0x33384aff)
	for y := uint32(0); y < h; y++ {
		for x := uint32(0); x < w; x++ {
			switch {
			case x == 0 || y == 0 || x == w-1 || y == h-1:
				pixels[y*w+x] = border
			case y < h/2:
				pixels[y*w+x] = mixRgb(primary, bg, float32(x)/float32(w))
			default:
				pixels[y*w+x] = mixRgb(bg, secondary, float32(x)/float32(w))
			}
		}
	}
	return
}

// mixRgb linearly interpolates the RGB channels of two RGBA8 colours.
// t is clamped to [0, 1]; alpha is fixed at 0xff (the demo has no
// transparent assets, and any alpha bytes in a or b are ignored).
func mixRgb(a uint32, b uint32, t float32) (out uint32) {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	ar := float32((a >> 24) & 0xff)
	ag := float32((a >> 16) & 0xff)
	ab := float32((a >> 8) & 0xff)
	br := float32((b >> 24) & 0xff)
	bg := float32((b >> 16) & 0xff)
	bb := float32((b >> 8) & 0xff)
	r := uint32(ar*(1-t) + br*t)
	g := uint32(ag*(1-t) + bg*t)
	bl := uint32(ab*(1-t) + bb*t)
	out = (r << 24) | (g << 16) | (bl << 8) | 0xff
	return
}

// markdownDemoState is the per-app-instance state for the Load
// sub-section. The pre-parsed mdHeadings/mdInline/... Docs stay
// package-level — they are read-only text content built once at
// startup and don't depend on a per-window WidgetIdStack.
type markdownDemoState struct {
	loadPath string
	loadDoc  *markdown.Doc
	loadErr  string
	loadInfo string
}

func init() {
	registry.Register(registry.Demo{
		Name:        "markdown",
		Category:    "Text & code",
		Title:       "markdown (Obsidian-flavored)",
		Stage:       [2]float32{1024, 760},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "Obsidian-flavored markdown renderer: headings, inline, lists, blockquote, code, rule, frontmatter, highlight, wikilinks, embeds (Obsidian + CommonMark images), callouts, comments — plus an interactive Load section.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &markdownDemoState{}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoMarkdown(ids, state.(*markdownDemoState))
		},
		SourceFunc: demoMarkdown,
	})
}

// Pre-parsed Obsidian-flavored markdown documents — one per construct
// group. Hoisted to package scope so each frame's Render is a tree
// walk over already-built retained holders, mirroring sqlSimple etc.
// in egui2_hl_sql_demo.go.
var (
	mdHeadings = markdown.Parse([]byte(`# Heading 1

## Heading 2

### Heading 3

#### Heading 4

##### Heading 5

###### Heading 6

Body text below the headings.`))

	mdInline = markdown.Parse([]byte(`Plain text, **strong text**, *italic text*, ` + "`inline code`" + `, ~~strikethrough~~, and a [hyperlink](https://example.com) inline.

Combined: ***bold italic***, **bold with ` + "`code`" + ` inside**, and an autolink: <https://docs.rs>.`))

	mdLists = markdown.Parse([]byte(`Bulleted:

- first item
- second item with *italic*
- third item with a [link](https://example.com)
  - nested bullet
  - another nested

Numbered:

1. step one
2. step two
3. step three

Mixed start:

5. starts at five
6. continues at six`))

	mdBlockquote = markdown.Parse([]byte(`Standalone paragraph above the quote.

> This is a blockquote.
> It can span multiple lines and may contain **bold** or *italic* text.
>
> A second paragraph inside the quote.

And a paragraph after.`))

	mdCode = markdown.Parse([]byte("Fenced **Go** (highlighted via `codeview.PrepareGo`):\n\n" +
		"```go\n" +
		"package main\n\n" +
		"import \"fmt\"\n\n" +
		"// greet prints a friendly greeting.\n" +
		"func greet(name string) {\n" +
		"    fmt.Printf(\"hello, %s\\n\", name)\n" +
		"}\n" +
		"```\n\n" +
		"Fenced **SQL** (highlighted via `codeview.PrepareSql`):\n\n" +
		"```sql\n" +
		"SELECT id, name, created_at\n" +
		"FROM users\n" +
		"WHERE active = true\n" +
		"  AND created_at >= now() - INTERVAL 7 DAY\n" +
		"ORDER BY created_at DESC;\n" +
		"```\n\n" +
		"Fenced **JSON** (highlighted via `codeview.PrepareJson`):\n\n" +
		"```json\n" +
		"{\n" +
		"  \"id\": 42,\n" +
		"  \"name\": \"sample\",\n" +
		"  \"tags\": [\"demo\", \"markdown\"],\n" +
		"  \"active\": true,\n" +
		"  \"meta\": null\n" +
		"}\n" +
		"```\n\n" +
		"Untagged fence (plain `CodeViewJob` fallback):\n\n" +
		"```\n" +
		"fn main() {\n" +
		"    println!(\"hello, world\");\n" +
		"}\n" +
		"```\n\n" +
		"Indented block — CommonMark strips the first 4 leading spaces " +
		"(the indent marker), so the 4/8/12-space rows below render as 0/4/8:\n\n" +
		"    SELECT id, name\n" +
		"        FROM users\n" +
		"            WHERE id = 42\n"))

	mdRule = markdown.Parse([]byte(`Above the rule.

---

Below the rule.`))

	mdFrontmatter = markdown.Parse([]byte(`---
title: Sample Note
tags: [demo, markdown]
kind: example
---

Body of the note follows the frontmatter. The frontmatter map is
exposed via [Doc.Frontmatter] and is not rendered into the visible
flow in phase 1.`))

	mdHighlight = markdown.Parse([]byte(`Plain text with ==highlighted text== inline.

Combine with other inline styles: ==**bold highlight**== and
==*italic highlight*== and ==` + "`code highlight`" + `==.`))

	mdWikilinks = markdown.Parse([]byte(`Plain wikilink: [[MyPage]].

Aliased wikilink: [[OtherPage|alias text]].

Wikilink with heading: [[Page#Section]].

Wikilink with heading and alias: [[Doc#Intro|see intro]].

Surrounding prose so wikilinks sit inline with regular text.`))

	mdEmbeds = markdown.Parse([]byte(`Note embed: ![[Some Note]].

Image embed: ![[diagram.png]].

Embed with heading: ![[Reference#Section A]].

Embed inline within prose: see ![[architecture.png]] for the layout.`),
		markdown.WithResolver(demoMarkdownResolver{}),
		markdown.WithImageMaxSize(200, 140))

	// mdImages exercises the CommonMark inline-image path (the Obsidian
	// embed path is already shown by mdEmbeds above). Two refs match
	// demoMarkdownResolver's procedural assets and render as real
	// images; one unknown ref falls back to the 🖼-glyph hyperlink so
	// both paths are visible side-by-side in the demo.
	mdImages = markdown.Parse([]byte(`CommonMark inline image with alt text:

![A schematic](diagram.png)

Image inline within prose: the system overview ![arch](architecture.png) sits above the deployment diagram.

Hero image on its own line:

![Hero banner](hero.png)

Unknown ref falls back to a 🖼-prefixed hyperlink:

![missing asset](this-image-does-not-exist.png)`),
		markdown.WithResolver(demoMarkdownResolver{}),
		markdown.WithImageMaxSize(200, 140))

	mdCallouts = markdown.Parse([]byte(`> [!note] A note callout
> Standard note styling. Body can contain **inline** *formatting*
> and even a [link](https://example.com).

> [!info]
> An info callout with no explicit title; the type itself becomes the title.

> [!tip] Pro tip
> Tips render with the green family — same shape, different palette.

> [!warning] Heads up
> Warnings carry an amber border so they stand out on a scan.

> [!danger] Critical
> Danger callouts use the red palette for high-severity notices.

> [!quote] Source
> Quotes get a neutral gray look — closer to a blockquote than a notice.

> [!example]- Foldable example
> Foldable callouts collapse on click. Defaults to closed unless a
> trailing ` + "`+`" + ` is present (here it isn't, so this one starts closed).

> [!summary]+ Foldable summary, default open
> The trailing ` + "`+`" + ` after ]+ flips the default to open.`))

	mdComment = markdown.Parse([]byte(`Visible text. %%this comment is dropped%% More visible text.

Comments are stripped silently — they do not appear in the rendered
flow at all.`))

	// mdSelfHl shows the AST-driven markdown highlighter (codeview.PrepareMarkdown)
	// applied to a fenced ```markdown block. The text rendered inside is a
	// canonical form: gofmt-style normalization (single-* emphasis vs **strong**,
	// list bullets unified to `-`, frontmatter keys sorted, indented blocks
	// promoted to fenced) — not a 1:1 echo of the source bytes.
	mdSelfHl = markdown.Parse([]byte("Markdown source highlighted through " +
		"`codeview.PrepareMarkdown` (AST → canonical → spans):\n\n" +
		"```markdown\n" +
		"---\n" +
		"title: Sample note\n" +
		"tags: [demo, markdown]\n" +
		"---\n\n" +
		"# Heading 1\n\n" +
		"## Heading 2\n\n" +
		"Paragraph with **strong**, *emphasis*, ~~strike~~, " +
		"`inline code` and a [link](https://example.com).\n\n" +
		"- bullet one\n" +
		"- bullet two with [[Wikilink|alias]] and ![[image.png]]\n" +
		"- bullet three with ==highlighted== text\n\n" +
		"Task list:\n\n" +
		"- [x] done item\n" +
		"- [ ] todo item with `code`\n" +
		"- [x] another done\n\n" +
		"| Feature | Status   | Notes        |\n" +
		"|:--------|:--------:|-------------:|\n" +
		"| Tables  | done     | M2           |\n" +
		"| Tasks   | done     | M2           |\n" +
		"| Math    | planned  | future       |\n\n" +
		"> [!note] Callout title\n" +
		"> Body line with **strong** and *emphasis*.\n\n" +
		"End of sample.\n" +
		"```\n"))
)

func demoMarkdownHeadings(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-headings")) {
		mdHeadings.Render(ids)
	}
}

func demoMarkdownInline(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-inline")) {
		mdInline.Render(ids)
	}
}

func demoMarkdownLists(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-lists")) {
		mdLists.Render(ids)
	}
}

func demoMarkdownBlockquote(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-blockquote")) {
		mdBlockquote.Render(ids)
	}
}

func demoMarkdownCode(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-code")) {
		mdCode.Render(ids)
	}
}

func demoMarkdownRule(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-rule")) {
		mdRule.Render(ids)
	}
}

func demoMarkdownFrontmatter(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-frontmatter")) {
		mdFrontmatter.Render(ids)
		mdFrontmatter.RenderFrontmatter()
	}
}

func demoMarkdownHighlight(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-highlight")) {
		mdHighlight.Render(ids)
	}
}

func demoMarkdownWikilinks(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-wikilinks")) {
		mdWikilinks.Render(ids)
	}
}

func demoMarkdownEmbeds(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-embeds")) {
		mdEmbeds.Render(ids)
	}
}

func demoMarkdownImages(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-images")) {
		mdImages.Render(ids)
	}
}

func demoMarkdownCallouts(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-callouts")) {
		mdCallouts.Render(ids)
	}
}

func demoMarkdownComment(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-comment")) {
		mdComment.Render(ids)
	}
}

func demoMarkdownSelfHl(ids *c.WidgetIdStack) {
	for range c.IdScope(ids.PrepareStr("md-self-hl")) {
		mdSelfHl.Render(ids)
	}
}

func demoMarkdownLoad(ids *c.WidgetIdStack, st *markdownDemoState) {
	for range c.IdScope(ids.PrepareStr("md-load")) {
		for range c.Horizontal().KeepIter() {
			c.Label("Path:").Send()
			c.TextEdit(ids.PrepareStr("path"), st.loadPath, false).
				DesiredWidth(520).
				HintText("/absolute/path/to/note.md").
				SendRespVal(&st.loadPath)
			if c.Button(ids.PrepareStr("load"), c.Atoms().Text("Load").Keep()).
				SendResp().HasPrimaryClicked() {
				st.loadMarkdownFromPath(st.loadPath)
			}
		}
		if st.loadErr != "" {
			red := color.Hex(styletokens.ErrorDefault.AsHex()).Keep()
			bg := color.Transparent.Keep()
			for rt := range c.RichTextLabelColored(red, bg, st.loadErr) {
				rt.Strong()
			}
		} else if st.loadInfo != "" {
			c.Label(st.loadInfo).Send()
		}
		c.Separator().Send()
		if st.loadDoc != nil {
			for range c.IdScope(ids.PrepareStr("md-load-rendered")) {
				st.loadDoc.Render(ids)
				st.loadDoc.RenderFrontmatter()
			}
		} else {
			c.Label("(no document loaded — enter a path and press Load)").Send()
		}
	}
}

// loadMarkdownFromPath reads the file at path, parses it, and swaps
// st.loadDoc. Errors update st.loadErr; on success st.loadInfo
// records the source size so the user can sanity-check the load
// happened.
func (st *markdownDemoState) loadMarkdownFromPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		st.loadErr = "(empty path)"
		st.loadInfo = ""
		st.loadDoc = nil
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		st.loadErr = "read error: " + err.Error()
		st.loadInfo = ""
		st.loadDoc = nil
		return
	}
	st.loadErr = ""
	st.loadInfo = fmt.Sprintf("loaded %s (%d bytes)", path, len(data))
	st.loadDoc = markdown.Parse(data)
}

func demoMarkdown(ids *c.WidgetIdStack, st *markdownDemoState) {
	for range c.CollapsingHeader(ids.PrepareStr("md-load-h"), c.WidgetText().Text("load file (interactive)").Keep()).DefaultOpen(true).KeepIter() {
		demoMarkdownLoad(ids, st)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-images-h"), c.WidgetText().Text("images (CommonMark ![alt](url) — resolver.LoadImage, fallback to hyperlink)").Keep()).DefaultOpen(true).KeepIter() {
		demoMarkdownImages(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-embeds-h"), c.WidgetText().Text("embeds (![[file]] — image embeds render via resolver.LoadImage)").Keep()).DefaultOpen(true).KeepIter() {
		demoMarkdownEmbeds(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-code-h"), c.WidgetText().Text("code blocks (fenced go/sql/json highlighted; indented + untagged plain)").Keep()).KeepIter() {
		demoMarkdownCode(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-self-hl-h"), c.WidgetText().Text("markdown source highlighting (AST → canonical, prototype)").Keep()).KeepIter() {
		demoMarkdownSelfHl(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-headings-h"), c.WidgetText().Text("headings (H1–H6)").Keep()).KeepIter() {
		demoMarkdownHeadings(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-inline-h"), c.WidgetText().Text("inline (strong / italic / code / strike / links)").Keep()).KeepIter() {
		demoMarkdownInline(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-lists-h"), c.WidgetText().Text("lists (bullet / numbered / nested)").Keep()).KeepIter() {
		demoMarkdownLists(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-blockquote-h"), c.WidgetText().Text("blockquote (Frame.PresetGroup)").Keep()).KeepIter() {
		demoMarkdownBlockquote(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-rule-h"), c.WidgetText().Text("thematic break (---)").Keep()).KeepIter() {
		demoMarkdownRule(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-frontmatter-h"), c.WidgetText().Text("frontmatter (YAML)").Keep()).KeepIter() {
		demoMarkdownFrontmatter(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-highlight-h"), c.WidgetText().Text("highlight (==text==)").Keep()).KeepIter() {
		demoMarkdownHighlight(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-wikilinks-h"), c.WidgetText().Text("wikilinks ([[Page]] / [[Page|alias]] / [[Page#Heading]])").Keep()).KeepIter() {
		demoMarkdownWikilinks(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-callouts-h"), c.WidgetText().Text("callouts (> [!type] Title)").Keep()).KeepIter() {
		demoMarkdownCallouts(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("md-comment-h"), c.WidgetText().Text("comments (%%text%% — silently dropped)").Keep()).KeepIter() {
		demoMarkdownComment(ids)
	}
}
