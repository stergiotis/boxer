package obsidian

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/markdown/obsidian/resolver"
	"github.com/stretchr/testify/require"
)

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
