package markdown

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
)

// ---------------- stringifyFrontmatterValue --------------------------------

func TestStringifyFrontmatterValue_Scalars(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want string
	}{
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"nil", nil, "(nil)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stringifyFrontmatterValue(tc.in)
			if got != tc.want {
				t.Errorf("stringifyFrontmatterValue(%#v): got %q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestStringifyFrontmatterValue_Slice(t *testing.T) {
	in := []interface{}{"a", 2, true}
	got := stringifyFrontmatterValue(in)
	want := "[a, 2, true]"
	if got != want {
		t.Errorf("slice: got %q want %q", got, want)
	}
}

func TestStringifyFrontmatterValue_EmptySlice(t *testing.T) {
	in := []interface{}{}
	got := stringifyFrontmatterValue(in)
	if got != "[]" {
		t.Errorf("empty slice: got %q want %q", got, "[]")
	}
}

func TestStringifyFrontmatterValue_NestedKV(t *testing.T) {
	kv := containers.NewBinarySearchGrowingKVFromAnyMap(map[string]interface{}{
		"k1": "v1",
		"k2": 7,
	})
	got := stringifyFrontmatterValue(kv)
	// IteratePairs is key-sorted, so order is stable.
	want := "{k1: v1, k2: 7}"
	if got != want {
		t.Errorf("nested KV: got %q want %q", got, want)
	}
}

func TestStringifyFrontmatterValue_NestedEmptyKV(t *testing.T) {
	// A nested empty YAML map (`key: {}`) converts to a typed-nil
	// *BinarySearchGrowingKV inside the interface value, which still
	// matches the nested-KV type-switch case. Reads on the nil receiver
	// are the empty container (containers review 2026-07-05, D3) — this
	// path used to panic.
	kv := containers.NewBinarySearchGrowingKVFromAnyMap(map[string]interface{}{
		"meta": map[string]interface{}{},
	})
	got := stringifyFrontmatterValue(kv)
	want := "{meta: {}}"
	if got != want {
		t.Errorf("nested empty KV: got %q want %q", got, want)
	}
}

func TestParse_FrontmatterNestedEmptyMap_StringifiesSafely(t *testing.T) {
	// Full pipeline variant of TestStringifyFrontmatterValue_NestedEmptyKV:
	// YAML `meta: {}` through goldmark-meta → FromAnyMap → typed-nil
	// nested KV → stringify. Used to panic in RenderFrontmatter.
	src := "---\ntitle: hello\nmeta: {}\n---\n\nbody\n"
	doc := Parse([]byte(src))
	fm := doc.Frontmatter()
	if fm == nil {
		t.Fatal("Frontmatter() is nil, want parsed frontmatter")
	}
	metaRaw, has := fm.Get("meta")
	if !has {
		t.Fatal("frontmatter key \"meta\" missing")
	}
	if got := stringifyFrontmatterValue(metaRaw); got != "{}" {
		t.Errorf("empty nested map: got %q want %q", got, "{}")
	}
}

func TestStringifyFrontmatterValue_RecursesIntoSliceOfSlices(t *testing.T) {
	in := []interface{}{
		[]interface{}{"a", "b"},
		[]interface{}{1, 2},
	}
	got := stringifyFrontmatterValue(in)
	want := "[[a, b], [1, 2]]"
	if got != want {
		t.Errorf("nested slice: got %q want %q", got, want)
	}
}

// ---------------- Parse: structural shape ---------------------------------

func TestParse_EmptyInput_EmptyDoc(t *testing.T) {
	doc := Parse(nil)
	if doc == nil {
		t.Fatal("Parse(nil) returned nil")
	}
	if len(doc.segments) != 0 {
		t.Errorf("empty input: got %d segments want 0", len(doc.segments))
	}
}

func TestParse_SingleParagraph_OneParagraphSegment(t *testing.T) {
	doc := Parse([]byte("just one paragraph here\n"))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1", len(doc.segments))
	}
	if doc.segments[0].kind != segKindParagraph {
		t.Errorf("kind: got %d want segKindParagraph", doc.segments[0].kind)
	}
}

func TestParse_Heading_AllLevels(t *testing.T) {
	src := strings.Join([]string{
		"# h1",
		"## h2",
		"### h3",
		"#### h4",
		"##### h5",
		"###### h6",
	}, "\n\n") + "\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 6 {
		t.Fatalf("segments: got %d want 6", len(doc.segments))
	}
	for i, seg := range doc.segments {
		if seg.kind != segKindHeading {
			t.Errorf("segment[%d].kind: got %d want segKindHeading", i, seg.kind)
		}
	}
}

func TestParse_FencedCodeBlock_LandsAsCodeBlockSegment(t *testing.T) {
	src := "```go\nprintln(\"hi\")\n```\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1", len(doc.segments))
	}
	if doc.segments[0].kind != segKindCodeBlock {
		t.Errorf("kind: got %d want segKindCodeBlock", doc.segments[0].kind)
	}
}

func TestParse_IndentedCodeBlock_LandsAsCodeBlockSegment(t *testing.T) {
	src := "    x := 1\n    y := 2\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1", len(doc.segments))
	}
	if doc.segments[0].kind != segKindCodeBlock {
		t.Errorf("kind: got %d want segKindCodeBlock", doc.segments[0].kind)
	}
}

func TestParse_HorizontalRule(t *testing.T) {
	// A leading `---` is consumed by the frontmatter parser; put a paragraph
	// in front so we get a paragraph + thematic-break pair.
	doc := Parse([]byte("intro\n\n---\n"))
	if len(doc.segments) < 2 {
		t.Fatalf("segments: got %d want >=2", len(doc.segments))
	}
	if doc.segments[1].kind != segKindHorizontalRule {
		t.Errorf("segment[1].kind: got %d want segKindHorizontalRule", doc.segments[1].kind)
	}
}

func TestParse_HorizontalRule_StarVariant(t *testing.T) {
	// `***` is a CommonMark thematic break that's unambiguous even at doc start.
	doc := Parse([]byte("***\n"))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1", len(doc.segments))
	}
	if doc.segments[0].kind != segKindHorizontalRule {
		t.Errorf("kind: got %d want segKindHorizontalRule", doc.segments[0].kind)
	}
}

func TestParse_UnorderedList(t *testing.T) {
	src := "- one\n- two\n- three\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1", len(doc.segments))
	}
	seg := doc.segments[0]
	if seg.kind != segKindList {
		t.Fatalf("kind: got %d want segKindList", seg.kind)
	}
	if seg.listOrdered {
		t.Error("unordered list flagged as ordered")
	}
	if len(seg.children) != 3 {
		t.Errorf("children: got %d want 3", len(seg.children))
	}
	for i, ch := range seg.children {
		if ch.kind != segKindListItem {
			t.Errorf("children[%d].kind: got %d want segKindListItem", i, ch.kind)
		}
	}
}

func TestParse_OrderedList_DefaultStart(t *testing.T) {
	src := "1. one\n2. two\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1", len(doc.segments))
	}
	seg := doc.segments[0]
	if !seg.listOrdered {
		t.Error("ordered list not flagged as ordered")
	}
	if seg.listStart != 1 {
		t.Errorf("listStart: got %d want 1", seg.listStart)
	}
}

func TestParse_OrderedList_ExplicitStart(t *testing.T) {
	src := "5. five\n6. six\n7. seven\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1", len(doc.segments))
	}
	seg := doc.segments[0]
	if !seg.listOrdered {
		t.Error("ordered list not flagged as ordered")
	}
	if seg.listStart != 5 {
		t.Errorf("listStart: got %d want 5", seg.listStart)
	}
}

func TestParse_NestedList(t *testing.T) {
	src := "- outer\n  - inner1\n  - inner2\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 1 || doc.segments[0].kind != segKindList {
		t.Fatalf("expected one list segment; got %+v", doc.segments)
	}
	outer := doc.segments[0]
	if len(outer.children) != 1 {
		t.Fatalf("outer items: got %d want 1", len(outer.children))
	}
	// The single outer item contains a nested list as one of its children.
	item := outer.children[0]
	if item.kind != segKindListItem {
		t.Fatalf("outer child kind: got %d want segKindListItem", item.kind)
	}
	hasNestedList := false
	for _, ch := range item.children {
		if ch.kind == segKindList {
			hasNestedList = true
			if len(ch.children) != 2 {
				t.Errorf("nested list items: got %d want 2", len(ch.children))
			}
		}
	}
	if !hasNestedList {
		t.Error("nested list segment not found under outer item")
	}
}

func TestParse_Blockquote(t *testing.T) {
	src := "> a quoted line\n> a second quoted line\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1", len(doc.segments))
	}
	if doc.segments[0].kind != segKindBlockquote {
		t.Errorf("kind: got %d want segKindBlockquote", doc.segments[0].kind)
	}
	if len(doc.segments[0].children) == 0 {
		t.Error("blockquote should have children")
	}
}

func TestParse_Callout_BasicShape(t *testing.T) {
	// Obsidian-flavored callout: > [!info] Title
	src := "> [!info] My Title\n> body line\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1", len(doc.segments))
	}
	seg := doc.segments[0]
	if seg.kind != segKindCallout {
		t.Fatalf("kind: got %d want segKindCallout", seg.kind)
	}
	if seg.calloutType != "info" {
		t.Errorf("calloutType: got %q want %q", seg.calloutType, "info")
	}
	if seg.calloutTitle != "My Title" {
		t.Errorf("calloutTitle: got %q want %q", seg.calloutTitle, "My Title")
	}
}

func TestParse_Callout_FoldableMarker(t *testing.T) {
	// `> [!warning]-` marks foldable, collapsed by default; `+` is foldable, open.
	src := "> [!warning]- collapsed\n> body\n"
	doc := Parse([]byte(src))
	if len(doc.segments) == 0 {
		t.Fatal("no segments parsed")
	}
	seg := doc.segments[0]
	if seg.kind != segKindCallout {
		t.Fatalf("kind: got %d want segKindCallout", seg.kind)
	}
	if !seg.calloutFoldable {
		t.Error("calloutFoldable: got false want true (for `-` marker)")
	}
}

// ---------------- Hyperlinks (markdown, wikilink, autolink) ---------------

func TestParse_MarkdownLink_BecomesLinkRun(t *testing.T) {
	src := "see [docs](https://example.com/docs) for details\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1", len(doc.segments))
	}
	var found *paragraphRun
	for i, r := range doc.segments[0].runs {
		if r.kind == runKindLink {
			found = &doc.segments[0].runs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected a runKindLink in the paragraph")
	}
	if found.label != "docs" {
		t.Errorf("link label: got %q want %q", found.label, "docs")
	}
	if found.url != "https://example.com/docs" {
		t.Errorf("link url: got %q want %q", found.url, "https://example.com/docs")
	}
}

func TestParse_AutoLink_BecomesLinkRun(t *testing.T) {
	src := "visit <https://example.com> today\n"
	doc := Parse([]byte(src))
	var found *paragraphRun
	for i, r := range doc.segments[0].runs {
		if r.kind == runKindLink {
			found = &doc.segments[0].runs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected a runKindLink for autolink")
	}
	if found.url != "https://example.com" {
		t.Errorf("autolink url: got %q want %q", found.url, "https://example.com")
	}
}

func TestParse_EmailAutoLink_GetsMailtoPrefix(t *testing.T) {
	src := "mail <foo@example.com>\n"
	doc := Parse([]byte(src))
	var found *paragraphRun
	for i, r := range doc.segments[0].runs {
		if r.kind == runKindLink {
			found = &doc.segments[0].runs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected a runKindLink for email autolink")
	}
	if found.url != "mailto:foo@example.com" {
		t.Errorf("email autolink url: got %q want %q", found.url, "mailto:foo@example.com")
	}
}

// ---------------- WithFeatures / WithResolver -----------------------------

func TestParse_FrontmatterPresent_PopulatesKV(t *testing.T) {
	src := "---\ntitle: hello\ncount: 3\n---\n\nbody\n"
	doc := Parse([]byte(src))
	fm := doc.Frontmatter()
	if fm == nil {
		t.Fatal("Frontmatter() returned nil even though feature is on by default")
	}
	if fm.IsEmpty() {
		t.Error("frontmatter should not be empty")
	}
}

func TestParse_WithFeaturesNoFrontmatter_DropsFrontmatter(t *testing.T) {
	src := "---\ntitle: hello\n---\n\nbody\n"
	doc := Parse([]byte(src), WithFeatures(obsidian.FeatureGFM))
	if doc.Frontmatter() != nil {
		t.Error("Frontmatter() should be nil when FeatureFrontmatter is excluded")
	}
}

func TestParse_FrontmatterAbsent_FrontmatterNil(t *testing.T) {
	// NewBinarySearchGrowingKVFromAnyMap returns nil for an empty input, so a
	// source with no frontmatter block produces Frontmatter()==nil even with
	// the feature enabled.
	doc := Parse([]byte("no frontmatter here\n"))
	if fm := doc.Frontmatter(); fm != nil {
		t.Errorf("Frontmatter(): got non-nil KV %v want nil", fm)
	}
}

// stubResolver records the inputs handed to ResolveWikilink so we can verify
// the parser routes them through the configured resolver.
//
// imageRefs records each ref handed to LoadImage; imagePayload (when
// non-empty) seeds a 1×1 ok response so image-routing tests can assert
// both call-through and run-kind without needing a real decoder.
type stubResolver struct {
	lastPage     string
	lastHeading  string
	imageRefs    []string
	imagePayload []uint32
	imageW       uint32
	imageH       uint32
}

func (s *stubResolver) ResolveWikilink(page, heading string) (url string, ok bool) {
	s.lastPage = page
	s.lastHeading = heading
	return "STUB://" + page, true
}
func (s *stubResolver) ResolveEmbed(target, heading string) (url string, isImage bool, ok bool) {
	return "STUB-EMBED://" + target, resolver.IsImageFile(target), true
}
func (s *stubResolver) LoadImage(ref string) (pixels []uint32, widthPx uint32, heightPx uint32, ok bool) {
	s.imageRefs = append(s.imageRefs, ref)
	if len(s.imagePayload) == 0 {
		return
	}
	pixels = s.imagePayload
	widthPx = s.imageW
	heightPx = s.imageH
	ok = true
	return
}

func TestParse_WithResolver_WikilinkUsesResolverURL(t *testing.T) {
	r := &stubResolver{}
	doc := Parse([]byte("see [[SomePage]]\n"), WithResolver(r))
	if r.lastPage != "SomePage" {
		t.Errorf("resolver.lastPage: got %q want %q", r.lastPage, "SomePage")
	}
	// Confirm the resolved URL is what the paragraph run carries.
	var url string
	for _, run := range doc.segments[0].runs {
		if run.kind == runKindLink {
			url = run.url
			break
		}
	}
	if url != "STUB://SomePage" {
		t.Errorf("wikilink URL: got %q want %q", url, "STUB://SomePage")
	}
}

func TestParse_WithResolver_NilArgIsIgnored(t *testing.T) {
	// WithResolver(nil) must not blank out the default resolver.
	doc := Parse([]byte("see [[Page]]\n"), WithResolver(nil))
	if doc == nil || len(doc.segments) == 0 {
		t.Fatal("Parse failed under WithResolver(nil)")
	}
	// Default resolver (NoopResolver) yields a non-empty URL like "/Page".
	for _, run := range doc.segments[0].runs {
		if run.kind == runKindLink && run.url == "" {
			t.Error("URL should be non-empty under the default NoopResolver")
		}
	}
}

func TestParse_DefaultResolverIsNoop(t *testing.T) {
	// Smoke check: the documented default is resolver.NoopResolver.
	cfg := defaultConfig()
	if cfg.resolver == nil {
		t.Fatal("defaultConfig.resolver is nil")
	}
	if _, ok := cfg.resolver.(resolver.NoopResolver); !ok {
		t.Errorf("default resolver type: got %T want resolver.NoopResolver", cfg.resolver)
	}
}

// ---------------- Images -------------------------------------------------

// stubLoaderPixels returns a tiny 2x2 buffer the stubResolver can seed when
// LoadImage is supposed to succeed. The exact contents don't matter for the
// parse-shape assertions below.
func stubLoaderPixels() (pixels []uint32, w, h uint32) {
	w, h = 2, 2
	pixels = []uint32{0xff0000ff, 0x00ff00ff, 0x0000ffff, 0xffffffff}
	return
}

func TestParse_CommonMarkImage_WithLoader_ProducesImageRun(t *testing.T) {
	r := &stubResolver{}
	r.imagePayload, r.imageW, r.imageH = stubLoaderPixels()

	doc := Parse([]byte("see ![my alt](pic.png) now\n"), WithResolver(r))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1", len(doc.segments))
	}
	var img *paragraphRun
	for i, run := range doc.segments[0].runs {
		if run.kind == runKindImage {
			img = &doc.segments[0].runs[i]
			break
		}
	}
	if img == nil {
		t.Fatalf("expected runKindImage; got runs=%v", kindsOfRuns(doc.segments[0].runs))
	}
	if img.imgWidthPx != 2 || img.imgHeightPx != 2 {
		t.Errorf("image dims: got (%d,%d) want (2,2)", img.imgWidthPx, img.imgHeightPx)
	}
	if len(img.imgPixels) != 4 {
		t.Errorf("image pixels: got len=%d want 4", len(img.imgPixels))
	}
	if len(r.imageRefs) != 1 || r.imageRefs[0] != "pic.png" {
		t.Errorf("LoadImage refs: got %v want [pic.png]", r.imageRefs)
	}
}

func TestParse_CommonMarkImage_WithoutLoader_FallsBackToHyperlink(t *testing.T) {
	// stubResolver with no imagePayload returns ok=false; image must fall
	// back to a glyph-prefixed link so the reference stays discoverable.
	r := &stubResolver{}
	doc := Parse([]byte("see ![cap](pic.png) now\n"), WithResolver(r))
	for _, run := range doc.segments[0].runs {
		if run.kind == runKindImage {
			t.Fatal("expected fallback runKindLink, not runKindImage")
		}
	}
	var found *paragraphRun
	for i, run := range doc.segments[0].runs {
		if run.kind == runKindLink {
			found = &doc.segments[0].runs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected runKindLink fallback")
	}
	if !strings.HasPrefix(found.label, "🖼 ") {
		t.Errorf("fallback label glyph: got %q want 🖼 prefix", found.label)
	}
	if found.url != "pic.png" {
		t.Errorf("fallback url: got %q want %q", found.url, "pic.png")
	}
}

func TestParse_ObsidianImageEmbed_WithLoader_ProducesImageRun(t *testing.T) {
	r := &stubResolver{}
	r.imagePayload, r.imageW, r.imageH = stubLoaderPixels()

	doc := Parse([]byte("see ![[diagram.png]] now\n"), WithResolver(r))
	var img *paragraphRun
	for i, run := range doc.segments[0].runs {
		if run.kind == runKindImage {
			img = &doc.segments[0].runs[i]
			break
		}
	}
	if img == nil {
		t.Fatalf("expected runKindImage; got runs=%v", kindsOfRuns(doc.segments[0].runs))
	}
	if len(r.imageRefs) != 1 || r.imageRefs[0] != "diagram.png" {
		t.Errorf("LoadImage refs: got %v want [diagram.png]", r.imageRefs)
	}
}

func TestParse_ObsidianNoteEmbed_StaysAsHyperlink(t *testing.T) {
	// Note transclusion (`![[Note]]`) is not an image even when the loader
	// is present; LoadImage must not be called and the rendering must stay
	// as the existing 📄-prefixed glyph hyperlink.
	r := &stubResolver{}
	r.imagePayload, r.imageW, r.imageH = stubLoaderPixels()

	doc := Parse([]byte("see ![[SomeNote]] now\n"), WithResolver(r))
	for _, run := range doc.segments[0].runs {
		if run.kind == runKindImage {
			t.Fatal("note embed must not produce runKindImage")
		}
	}
	if len(r.imageRefs) != 0 {
		t.Errorf("LoadImage refs: got %v want [] (note embed must not call LoadImage)", r.imageRefs)
	}
}

func TestParse_Image_LoaderDimMismatch_FallsBackToHyperlink(t *testing.T) {
	// Loader returns inconsistent (w*h != len(pixels)) — the visitor must
	// reject and fall back, not splice a malformed buffer into a segment.
	r := &stubResolver{}
	r.imagePayload = []uint32{0xff0000ff, 0x00ff00ff} // 2 pixels
	r.imageW, r.imageH = 4, 4                         // claims 16

	doc := Parse([]byte("![bad](bad.png)\n"), WithResolver(r))
	for _, run := range doc.segments[0].runs {
		if run.kind == runKindImage {
			t.Fatal("malformed loader response must not produce runKindImage")
		}
	}
}

func TestWithImageMaxSize_FlowsToDoc(t *testing.T) {
	doc := Parse([]byte("hi\n"), WithImageMaxSize(123, 456))
	if doc.imageMaxW != 123 || doc.imageMaxH != 456 {
		t.Errorf("imageMax: got (%d,%d) want (123,456)", doc.imageMaxW, doc.imageMaxH)
	}
}

func TestParse_DefaultImageMaxSize(t *testing.T) {
	doc := Parse([]byte("hi\n"))
	if doc.imageMaxW != imageMaxDefaultW || doc.imageMaxH != imageMaxDefaultH {
		t.Errorf("default imageMax: got (%d,%d) want (%d,%d)",
			doc.imageMaxW, doc.imageMaxH, imageMaxDefaultW, imageMaxDefaultH)
	}
}

// TestParse_OversizedImage_RejectedAtVisitor pairs with the
// imageMaxPixelCount cap in visitor.go: a resolver claiming a
// pathologically large image must not produce a runKindImage even
// when len(pixels) happens to match w*h on a smaller actual buffer
// (the cap is the first line of defence, before any allocation
// downstream of the visitor).
func TestParse_OversizedImage_RejectedAtVisitor(t *testing.T) {
	r := &stubResolver{}
	// Width × height > imageMaxPixelCount (64 Mpx). 16384×16384 = 256 Mpx.
	r.imageW, r.imageH = 16384, 16384
	r.imagePayload = make([]uint32, 16384*16384)
	doc := Parse([]byte("![oversized](huge.png)\n"), WithResolver(r))
	for _, run := range doc.segments[0].runs {
		if run.kind == runKindImage {
			t.Fatal("oversized image must be rejected")
		}
	}
}

// TestParse_CommonMarkImage_EmptyAlt_FallsBackToURLLabel covers the
// `![](pic.png)` corner case where alt is empty — the fallback
// hyperlink label substitutes the URL so the user sees *something*
// pointing at the asset rather than a bare 🖼 glyph.
func TestParse_CommonMarkImage_EmptyAlt_FallsBackToURLLabel(t *testing.T) {
	r := &stubResolver{} // imagePayload empty → LoadImage returns ok=false
	doc := Parse([]byte("![](pic.png)\n"), WithResolver(r))
	var found *paragraphRun
	for i, run := range doc.segments[0].runs {
		if run.kind == runKindLink {
			found = &doc.segments[0].runs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected runKindLink fallback for empty-alt CommonMark image")
	}
	if found.label != "🖼 pic.png" {
		t.Errorf("fallback label: got %q want %q", found.label, "🖼 pic.png")
	}
}

// TestParse_ObsidianImageEmbed_WithHeading_PassesHeadingInRef confirms
// the visitor synthesises "target#heading" as the LoadImage ref for
// embeds carrying a section anchor. The resolver receives the joined
// string verbatim and is responsible for splitting it if it cares.
func TestParse_ObsidianImageEmbed_WithHeading_PassesHeadingInRef(t *testing.T) {
	r := &stubResolver{} // ok=false; we just verify the ref shape via imageRefs
	doc := Parse([]byte("see ![[diagram.png#section A]] now\n"), WithResolver(r))
	_ = doc
	if len(r.imageRefs) != 1 || r.imageRefs[0] != "diagram.png#section A" {
		t.Errorf("LoadImage refs: got %v want [diagram.png#section A]", r.imageRefs)
	}
}

// TestParse_NoTrackerOnDoc guards the post-cleanup invariant that
// nothing inside Doc tracks "have I sent pixels for this image
// before". Per the bindings doc at [c.ImageVersionTracker], keying a
// tracker by seq instead of widget id silently drops pixels on the
// second scope; the package therefore re-sends pixels every frame,
// and a future contributor reintroducing a tracker field would have
// to defeat this assertion deliberately.
func TestParse_NoTrackerOnDoc(t *testing.T) {
	doc := Parse([]byte("hi\n"))
	v := reflect.ValueOf(*doc)
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if strings.Contains(strings.ToLower(f.Name), "version") ||
			strings.Contains(strings.ToLower(f.Name), "tracker") {
			t.Errorf("Doc.%s exists; the package contract is to skip per-Doc image trackers", f.Name)
		}
	}
}

func kindsOfRuns(runs []paragraphRun) []runKindE {
	out := make([]runKindE, len(runs))
	for i, r := range runs {
		out[i] = r.kind
	}
	return out
}

func TestParse_HeadingFontSize_TableCovers1Through6AndFallback(t *testing.T) {
	cases := []struct {
		level uint8
		want  float32
	}{
		{1, 26},
		{2, 22},
		{3, 18},
		{4, 16},
		{5, 14},
		{6, 12.5},
		{7, 14}, // out-of-range falls back to 14
		{0, 14}, // out-of-range falls back to 14
	}
	for _, tc := range cases {
		if got := headingFontSize(tc.level); got != tc.want {
			t.Errorf("headingFontSize(%d): got %v want %v", tc.level, got, tc.want)
		}
	}
}

// ---------------- Cumulative shape: multi-block document ------------------

func TestParse_DocumentWithMixedBlocks(t *testing.T) {
	src := strings.Join([]string{
		"# Title",
		"",
		"intro paragraph.",
		"",
		"- a",
		"- b",
		"",
		"> a quote",
		"",
		"---",
		"",
		"```",
		"code",
		"```",
	}, "\n") + "\n"
	doc := Parse([]byte(src))
	wantKinds := []segKindE{
		segKindHeading,
		segKindParagraph,
		segKindList,
		segKindBlockquote,
		segKindHorizontalRule,
		segKindCodeBlock,
	}
	if len(doc.segments) != len(wantKinds) {
		t.Fatalf("segments: got %d want %d (got kinds=%v)",
			len(doc.segments), len(wantKinds), kindsOf(doc.segments))
	}
	for i, k := range wantKinds {
		if doc.segments[i].kind != k {
			t.Errorf("segment[%d].kind: got %d want %d", i, doc.segments[i].kind, k)
		}
	}
}

func kindsOf(segs []segment) []segKindE {
	out := make([]segKindE, len(segs))
	for i, s := range segs {
		out[i] = s.kind
	}
	return out
}
