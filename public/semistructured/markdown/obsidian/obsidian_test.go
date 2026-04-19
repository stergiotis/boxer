//go:build llm_generated_opus46

package obsidian

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/parser"
)

var updateGolden = flag.Bool("update-golden", false, "update golden files")

func render(t *testing.T, opts Options, input string) string {
	t.Helper()
	md := New(opts)
	var buf bytes.Buffer
	err := md.Convert([]byte(input), &buf)
	require.NoError(t, err)
	return strings.TrimSpace(buf.String())
}

func allFeatures() Options {
	return Options{
		Features: FeatureAll,
		Resolver: resolver.NoopResolver{},
	}
}

// =============================================================================
// Wikilinks
// =============================================================================

func TestWikilink_Simple(t *testing.T) {
	out := render(t, allFeatures(), "Link to [[MyPage]]")
	require.Contains(t, out, `<a href="/MyPage" class="wikilink">MyPage</a>`)
}

func TestWikilink_Alias(t *testing.T) {
	out := render(t, allFeatures(), "See [[MyPage|click here]]")
	require.Contains(t, out, `<a href="/MyPage" class="wikilink">click here</a>`)
}

func TestWikilink_Heading(t *testing.T) {
	out := render(t, allFeatures(), "Go to [[MyPage#Introduction]]")
	require.Contains(t, out, `href="/MyPage#introduction"`)
	require.Contains(t, out, `>MyPage &gt; Introduction</a>`)
}

func TestWikilink_HeadingAndAlias(t *testing.T) {
	out := render(t, allFeatures(), "[[MyPage#Intro|see intro]]")
	require.Contains(t, out, `href="/MyPage#intro"`)
	require.Contains(t, out, `>see intro</a>`)
}

func TestWikilink_SpacesInPageName(t *testing.T) {
	out := render(t, allFeatures(), "[[My Page]]")
	require.Contains(t, out, `href="/My%20Page"`)
}

func TestWikilink_Empty(t *testing.T) {
	out := render(t, allFeatures(), "[[]]")
	// Empty wikilink should not be parsed
	require.Contains(t, out, "[[]]")
}

func TestWikilink_InParagraph(t *testing.T) {
	out := render(t, allFeatures(), "before [[page]] after")
	require.Contains(t, out, "before ")
	require.Contains(t, out, `<a href="/page" class="wikilink">page</a>`)
	require.Contains(t, out, " after")
}

// =============================================================================
// Embeds
// =============================================================================

func TestEmbed_Image(t *testing.T) {
	out := render(t, allFeatures(), "![[photo.png]]")
	require.Contains(t, out, `<img src="/photo.png"`)
	require.Contains(t, out, `class="embed-image"`)
}

func TestEmbed_Note(t *testing.T) {
	out := render(t, allFeatures(), "![[SomeNote]]")
	require.Contains(t, out, `<div class="embed-note"`)
	require.Contains(t, out, `data-src="/SomeNote"`)
}

func TestEmbed_NoteWithHeading(t *testing.T) {
	out := render(t, allFeatures(), "![[SomeNote#Section]]")
	require.Contains(t, out, `data-src="/SomeNote#section"`)
	require.Contains(t, out, "SomeNote")
}

func TestEmbed_ImageExtensions(t *testing.T) {
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp"} {
		out := render(t, allFeatures(), "![[file"+ext+"]]")
		require.Contains(t, out, "<img", "expected <img> for extension %s", ext)
	}
}

// =============================================================================
// Highlights
// =============================================================================

func TestHighlight_Simple(t *testing.T) {
	out := render(t, allFeatures(), "This is ==highlighted== text")
	require.Contains(t, out, "<mark>highlighted</mark>")
}

func TestHighlight_InParagraph(t *testing.T) {
	out := render(t, allFeatures(), "before ==middle== after")
	require.Contains(t, out, "before <mark>middle</mark> after")
}

func TestHighlight_Empty(t *testing.T) {
	out := render(t, allFeatures(), "====")
	// Empty highlight should not be parsed
	require.NotContains(t, out, "<mark>")
}

// =============================================================================
// Comments
// =============================================================================

func TestComment_Stripped(t *testing.T) {
	out := render(t, allFeatures(), "visible %%hidden%% text")
	require.NotContains(t, out, "hidden")
	require.Contains(t, out, "visible")
	require.Contains(t, out, "text")
}

func TestComment_Multiple(t *testing.T) {
	out := render(t, allFeatures(), "a %%b%% c %%d%% e")
	require.NotContains(t, out, "b")
	require.NotContains(t, out, "d")
	require.Contains(t, out, "a")
	require.Contains(t, out, "c")
	require.Contains(t, out, "e")
}

// =============================================================================
// Tags
// =============================================================================

func TestTag_Simple(t *testing.T) {
	out := render(t, allFeatures(), "Hello #mytag world")
	require.Contains(t, out, `<span class="tag">#mytag</span>`)
}

func TestTag_Nested(t *testing.T) {
	out := render(t, allFeatures(), "See #project/frontend")
	require.Contains(t, out, `#project/frontend</span>`)
}

func TestTag_WithHyphen(t *testing.T) {
	out := render(t, allFeatures(), "Tag #my-tag here")
	require.Contains(t, out, `#my-tag</span>`)
}

func TestTag_RenderAsLink(t *testing.T) {
	opts := Options{
		Features:  FeatureTag,
		TagRender: TagRenderLink,
	}
	out := render(t, opts, "Hello #mytag")
	require.Contains(t, out, `<a href="#mytag" class="tag">#mytag</a>`)
}

func TestTag_NotHeading(t *testing.T) {
	// # at start of line is a heading, not a tag — but goldmark handles
	// headings at block level before inline parsing, so this should be a heading.
	out := render(t, allFeatures(), "# Heading")
	require.Contains(t, out, "<h1>Heading</h1>")
	require.NotContains(t, out, `class="tag"`)
}

// =============================================================================
// Callouts
// =============================================================================

func TestCallout_Basic(t *testing.T) {
	input := "> [!note]\n> This is a note"
	out := render(t, allFeatures(), input)
	require.Contains(t, out, `class="callout callout-note"`)
	require.Contains(t, out, `class="callout-title">Note</div>`)
	require.Contains(t, out, `class="callout-content"`)
	require.Contains(t, out, "This is a note")
}

func TestCallout_CustomTitle(t *testing.T) {
	input := "> [!warning] Be careful\n> Danger ahead"
	out := render(t, allFeatures(), input)
	require.Contains(t, out, `callout-warning`)
	require.Contains(t, out, `Be careful`)
}

func TestCallout_Foldable(t *testing.T) {
	input := "> [!tip]-\n> Hidden content"
	out := render(t, allFeatures(), input)
	require.Contains(t, out, "<details>")
	require.Contains(t, out, "<summary")
	require.NotContains(t, out, "open")
}

func TestCallout_FoldableOpen(t *testing.T) {
	input := "> [!tip]+\n> Visible content"
	out := render(t, allFeatures(), input)
	require.Contains(t, out, `<details open>`)
}

func TestCallout_CaseInsensitive(t *testing.T) {
	input := "> [!NOTE]\n> Content"
	out := render(t, allFeatures(), input)
	require.Contains(t, out, `callout-note`)
}

// =============================================================================
// GFM
// =============================================================================

func TestGFM_Strikethrough(t *testing.T) {
	out := render(t, allFeatures(), "~~deleted~~")
	require.Contains(t, out, "<del>deleted</del>")
}

func TestGFM_TaskList(t *testing.T) {
	out := render(t, allFeatures(), "- [ ] todo\n- [x] done")
	require.Contains(t, out, `type="checkbox"`)
}

func TestGFM_Table(t *testing.T) {
	input := "| A | B |\n|---|---|\n| 1 | 2 |"
	out := render(t, allFeatures(), input)
	require.Contains(t, out, "<table>")
}

// =============================================================================
// Feature toggle
// =============================================================================

func TestFeature_DisableWikilink(t *testing.T) {
	opts := Options{Features: FeatureHighlight} // no wikilink
	out := render(t, opts, "[[page]]")
	require.NotContains(t, out, `class="wikilink"`)
	require.Contains(t, out, "[[page]]")
}

func TestFeature_DisableHighlight(t *testing.T) {
	opts := Options{Features: FeatureWikilink} // no highlight
	out := render(t, opts, "==text==")
	require.NotContains(t, out, "<mark>")
}

// =============================================================================
// Combined features
// =============================================================================

func TestCombined_WikilinkAndHighlight(t *testing.T) {
	// Wikilink inside highlight: highlight wraps the wikilink text as-is
	// (inline parsers don't recurse into highlight content)
	out := render(t, allFeatures(), "==important== and [[page]]")
	require.Contains(t, out, "<mark>important</mark>")
	require.Contains(t, out, `class="wikilink"`)
}

func TestCombined_TagAndComment(t *testing.T) {
	out := render(t, allFeatures(), "#visible %%hidden%%")
	require.Contains(t, out, `class="tag"`)
	require.NotContains(t, out, "hidden")
}

// =============================================================================
// Custom resolver
// =============================================================================

type testResolver struct{}

func (inst testResolver) ResolveWikilink(page string, heading string) (url string, exists bool) {
	if page == "missing" {
		url = "/missing"
		exists = false
		return
	}
	url = "/wiki/" + page
	if heading != "" {
		url += "#" + heading
	}
	exists = true
	return
}

func (inst testResolver) ResolveEmbed(target string, heading string) (url string, isImage bool, exists bool) {
	url = "/assets/" + target
	isImage = resolver.IsImageFile(target)
	exists = true
	return
}

func TestCustomResolver_Wikilink(t *testing.T) {
	opts := Options{
		Features: FeatureWikilink,
		Resolver: testResolver{},
	}
	out := render(t, opts, "[[MyPage]]")
	require.Contains(t, out, `href="/wiki/MyPage"`)
}

func TestCustomResolver_BrokenLink(t *testing.T) {
	opts := Options{
		Features: FeatureWikilink,
		Resolver: testResolver{},
	}
	out := render(t, opts, "[[missing]]")
	require.Contains(t, out, `wikilink-broken`)
}

func TestCustomResolver_Embed(t *testing.T) {
	opts := Options{
		Features: FeatureEmbed,
		Resolver: testResolver{},
	}
	out := render(t, opts, "![[photo.png]]")
	require.Contains(t, out, `src="/assets/photo.png"`)
}

// =============================================================================
// Frontmatter
// =============================================================================

func renderWithContext(t *testing.T, opts Options, input string) (output string, pc parser.Context) {
	t.Helper()
	md := New(opts)
	pc = NewParserContext()
	var buf bytes.Buffer
	err := md.Convert([]byte(input), &buf, parser.WithContext(pc))
	require.NoError(t, err)
	output = strings.TrimSpace(buf.String())
	return
}

func TestFrontmatter_Parsed(t *testing.T) {
	input := "---\ntitle: My Note\ntags:\n  - foo\n  - bar\n---\n\nHello world"
	out, pc := renderWithContext(t, allFeatures(), input)
	require.Contains(t, out, "Hello world")
	require.NotContains(t, out, "title:")
	require.NotContains(t, out, "---")

	meta := GetFrontmatter(pc)
	require.NotNil(t, meta)
	require.Equal(t, "My Note", meta["title"])
	tags, ok := meta["tags"].([]interface{})
	require.True(t, ok)
	require.Len(t, tags, 2)
	require.Equal(t, "foo", tags[0])
	require.Equal(t, "bar", tags[1])
}

func TestFrontmatter_Aliases(t *testing.T) {
	input := "---\naliases:\n  - alias1\n  - alias2\n---\n\nContent"
	_, pc := renderWithContext(t, allFeatures(), input)

	meta := GetFrontmatter(pc)
	require.NotNil(t, meta)
	aliases, ok := meta["aliases"].([]interface{})
	require.True(t, ok)
	require.Len(t, aliases, 2)
}

func TestFrontmatter_CssClasses(t *testing.T) {
	input := "---\ncssclasses:\n  - wide\n  - no-title\n---\n\nContent"
	_, pc := renderWithContext(t, allFeatures(), input)

	meta := GetFrontmatter(pc)
	require.NotNil(t, meta)
	require.NotNil(t, meta["cssclasses"])
}

func TestFrontmatter_Stripped(t *testing.T) {
	input := "---\nkey: value\n---\n\nVisible"
	out, _ := renderWithContext(t, allFeatures(), input)
	require.Contains(t, out, "Visible")
	require.NotContains(t, out, "key")
	require.NotContains(t, out, "value")
}

func TestFrontmatter_NoFrontmatter(t *testing.T) {
	input := "Just a paragraph"
	_, pc := renderWithContext(t, allFeatures(), input)

	meta := GetFrontmatter(pc)
	require.Nil(t, meta)
}

func TestFrontmatter_Disabled(t *testing.T) {
	opts := Options{Features: FeatureHighlight} // no frontmatter
	input := "---\ntitle: test\n---\n\nContent"
	out, pc := renderWithContext(t, opts, input)
	// Without frontmatter feature, --- is rendered as an <hr> or left as-is
	meta := GetFrontmatter(pc)
	require.Nil(t, meta)
	_ = out
}

func TestFrontmatter_TryGet_Malformed(t *testing.T) {
	input := "---\n: invalid yaml [[\n---\n\nContent"
	_, pc := renderWithContext(t, allFeatures(), input)

	_, err := TryGetFrontmatter(pc)
	// goldmark-meta may produce an error or silently handle malformed YAML
	// depending on the yaml parser — we just verify it doesn't panic
	_ = err
}

// =============================================================================
// Frontmatter HTML rendering
// =============================================================================

func renderFM(t *testing.T, metadata map[string]interface{}, open bool) string {
	t.Helper()
	var buf bytes.Buffer
	err := RenderFrontmatterHTML(&buf, metadata, open)
	require.NoError(t, err)
	return buf.String()
}

func TestRenderFrontmatter_Simple(t *testing.T) {
	m := map[string]interface{}{
		"title":  "My Note",
		"author": "Alice",
	}
	out := renderFM(t, m, true)
	require.Contains(t, out, `<details class="frontmatter" open>`)
	require.Contains(t, out, `<summary>Properties</summary>`)
	require.Contains(t, out, `<dt>title</dt><dd>My Note</dd>`)
	require.Contains(t, out, `<dt>author</dt><dd>Alice</dd>`)
	require.Contains(t, out, `</details>`)
}

func TestRenderFrontmatter_Closed(t *testing.T) {
	m := map[string]interface{}{"key": "val"}
	out := renderFM(t, m, false)
	require.Contains(t, out, `<details class="frontmatter">`)
	require.NotContains(t, out, "open")
}

func TestRenderFrontmatter_Array(t *testing.T) {
	m := map[string]interface{}{
		"tags": []interface{}{"foo", "bar", "baz"},
	}
	out := renderFM(t, m, true)
	require.Contains(t, out, "<ul>")
	require.Contains(t, out, "<li>foo</li>")
	require.Contains(t, out, "<li>bar</li>")
	require.Contains(t, out, "<li>baz</li>")
	require.Contains(t, out, "</ul>")
}

func TestRenderFrontmatter_NestedMap(t *testing.T) {
	m := map[string]interface{}{
		"author": map[string]interface{}{
			"name":  "Alice",
			"email": "a@b.c",
		},
	}
	out := renderFM(t, m, true)
	// Outer dl
	require.Contains(t, out, "<dt>author</dt><dd><dl>")
	// Inner dl
	require.Contains(t, out, "<dt>name</dt><dd>Alice</dd>")
	require.Contains(t, out, "<dt>email</dt><dd>a@b.c</dd>")
}

func TestRenderFrontmatter_ArrayOfMaps(t *testing.T) {
	m := map[string]interface{}{
		"people": []interface{}{
			map[string]interface{}{"name": "Alice"},
			map[string]interface{}{"name": "Bob"},
		},
	}
	out := renderFM(t, m, true)
	require.Contains(t, out, "<ul>")
	require.Contains(t, out, "<li><dl>")
	require.Contains(t, out, "<dt>name</dt><dd>Alice</dd>")
	require.Contains(t, out, "<dt>name</dt><dd>Bob</dd>")
}

func TestRenderFrontmatter_NilValue(t *testing.T) {
	m := map[string]interface{}{
		"draft": nil,
	}
	out := renderFM(t, m, true)
	require.Contains(t, out, "<dt>draft</dt><dd></dd>")
}

func TestRenderFrontmatter_Numeric(t *testing.T) {
	m := map[string]interface{}{
		"version": 42,
		"ratio":   3.14,
	}
	out := renderFM(t, m, true)
	require.Contains(t, out, "<dt>version</dt><dd>42</dd>")
	require.Contains(t, out, "<dt>ratio</dt><dd>3.14</dd>")
}

func TestRenderFrontmatter_Bool(t *testing.T) {
	m := map[string]interface{}{
		"publish": true,
	}
	out := renderFM(t, m, true)
	require.Contains(t, out, "<dt>publish</dt><dd>true</dd>")
}

func TestRenderFrontmatter_HTMLEscape(t *testing.T) {
	m := map[string]interface{}{
		"title": `<script>alert("xss")</script>`,
	}
	out := renderFM(t, m, true)
	require.NotContains(t, out, "<script>")
	require.Contains(t, out, "&lt;script&gt;")
}

func TestRenderFrontmatter_Empty(t *testing.T) {
	out := renderFM(t, nil, true)
	require.Empty(t, out)

	out = renderFM(t, map[string]interface{}{}, true)
	require.Empty(t, out)
}

func TestRenderFrontmatter_SortedKeys(t *testing.T) {
	m := map[string]interface{}{
		"zebra": "z",
		"alpha": "a",
		"mid":   "m",
	}
	out := renderFM(t, m, true)
	alphaIdx := strings.Index(out, "<dt>alpha</dt>")
	midIdx := strings.Index(out, "<dt>mid</dt>")
	zebraIdx := strings.Index(out, "<dt>zebra</dt>")
	require.Greater(t, midIdx, alphaIdx)
	require.Greater(t, zebraIdx, midIdx)
}

func TestRenderFrontmatter_Integration(t *testing.T) {
	input := "---\ntitle: Test\ntags:\n  - a\n  - b\npublish: true\n---\n\nBody"
	_, pc := renderWithContext(t, allFeatures(), input)

	fm := GetFrontmatter(pc)
	require.NotNil(t, fm)

	out := renderFM(t, fm, true)
	require.Contains(t, out, "<dt>title</dt><dd>Test</dd>")
	require.Contains(t, out, "<li>a</li>")
	require.Contains(t, out, "<li>b</li>")
	require.Contains(t, out, "<dt>publish</dt><dd>true</dd>")
}

// =============================================================================
// Showcase — renders testdata/showcase.md to testdata/showcase.out.html
// =============================================================================

type showcaseResolver struct{}

func (inst showcaseResolver) ResolveWikilink(page string, heading string) (url string, exists bool) {
	if page == "NonExistent" {
		url = "/" + page
		exists = false
		return
	}
	url = "/" + strings.ReplaceAll(page, " ", "%20")
	if heading != "" {
		url += "#" + strings.ToLower(strings.ReplaceAll(heading, " ", "-"))
	}
	exists = true
	return
}

func (inst showcaseResolver) ResolveEmbed(target string, heading string) (url string, isImage bool, exists bool) {
	url = "/assets/" + target
	if heading != "" {
		url += "#" + strings.ToLower(strings.ReplaceAll(heading, " ", "-"))
	}
	isImage = resolver.IsImageFile(target)
	exists = true
	return
}

func TestShowcase(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "showcase.md"))
	require.NoError(t, err)

	opts := Options{
		Features: FeatureAll,
		Resolver: showcaseResolver{},
	}
	md := New(opts)
	pc := NewParserContext()

	var body bytes.Buffer
	err = md.Convert(src, &body, parser.WithContext(pc))
	require.NoError(t, err)

	fm := GetFrontmatter(pc)

	// Assemble full HTML document
	var doc bytes.Buffer
	doc.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	doc.WriteString("<meta charset=\"utf-8\">\n")
	if fm != nil {
		if title, ok := fm["title"].(string); ok {
			doc.WriteString("<title>")
			doc.WriteString(htmlEscape(title))
			doc.WriteString("</title>\n")
		}
	}
	doc.WriteString("<style>\n")
	doc.WriteString(DefaultStylesheet())
	doc.WriteString("\n</style>\n")
	doc.WriteString("<style>\n")
	doc.WriteString("body { font-family: var(--ob-font-body); color: var(--ob-text); max-width: 50em; margin: 2em auto; padding: 0 1em; line-height: 1.6; }\n")
	doc.WriteString("</style>\n")
	doc.WriteString("</head>\n<body>\n")

	if fm != nil {
		err = RenderFrontmatterHTML(&doc, fm, true)
		require.NoError(t, err)
	}

	doc.Write(body.Bytes())
	doc.WriteString("</body>\n</html>\n")

	// Verify determinism: output must match the committed .out.html exactly.
	// To update the golden file after intentional changes, run:
	//   go test -run TestShowcase -update-golden
	outPath := filepath.Join("testdata", "showcase.out.html")
	got := doc.Bytes()

	if *updateGolden {
		err = os.WriteFile(outPath, got, 0o644)
		require.NoError(t, err)
		t.Logf("updated %s (%d bytes)", outPath, len(got))
	} else {
		want, readErr := os.ReadFile(outPath)
		require.NoError(t, readErr, "golden file missing — run with -update-golden to create it")
		require.Equal(t, string(want), string(got), "output differs from golden file — run with -update-golden to accept changes")
	}

	// Verify key features are present in output
	html := doc.String()
	require.Contains(t, html, `class="wikilink"`)
	require.Contains(t, html, `class="wikilink wikilink-broken"`)
	require.Contains(t, html, `class="embed-image"`)
	require.Contains(t, html, `class="embed-note"`)
	require.Contains(t, html, `<mark>`)
	require.Contains(t, html, `class="tag"`)
	require.Contains(t, html, `class="callout callout-note"`)
	require.Contains(t, html, `class="callout callout-warning"`)
	require.Contains(t, html, `class="callout callout-danger"`)
	require.Contains(t, html, `<details>`)
	require.Contains(t, html, `<details open>`)
	require.Contains(t, html, `<table>`)
	require.Contains(t, html, `type="checkbox"`)
	require.Contains(t, html, `<del>`)
	require.Contains(t, html, `class="frontmatter"`)
	require.Contains(t, html, `<dt>title</dt><dd>Feature Showcase</dd>`)
	require.NotContains(t, html, "but this is hidden")
	require.NotContains(t, html, "a secret note")
}
