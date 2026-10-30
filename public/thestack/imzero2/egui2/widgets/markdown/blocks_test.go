package markdown

import (
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
	"strconv"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

const blocksSample = "---\ntitle: T\n---\n\n# Title\n\nFirst paragraph\nsecond line.\n\n- one\n- two\n  nested\n\n```go\nfunc f() {}\n```\n\n> quoted\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\n---\n\n> [!note] Hi\n> body\n\nLast."

func TestBlocksCoverTheSourceWithWhitespaceBetween(t *testing.T) {
	doc := Parse([]byte(blocksSample))
	blocks := doc.Blocks()
	src := blocksSample
	want := []struct {
		kind BlockKindE
		text string
	}{
		{BlockHeading, "# Title"},
		{BlockParagraph, "First paragraph\nsecond line."},
		{BlockList, "- one\n- two\n  nested"},
		{BlockCodeBlock, "```go\nfunc f() {}\n```"},
		{BlockBlockquote, "> quoted"},
		{BlockTable, "| a | b |\n| - | - |\n| 1 | 2 |"},
		{BlockHorizontalRule, "---"},
		{BlockCallout, "> [!note] Hi\n> body"},
		{BlockParagraph, "Last."},
	}
	if len(blocks) != len(want) {
		t.Fatalf("want %d blocks, got %d: %+v", len(want), len(blocks), blocks)
	}
	prev := 0
	if s, e, ok := doc.FrontmatterSpan(); !ok || src[s:e] != "---\ntitle: T\n---" {
		t.Fatalf("frontmatter span: %d %d %v", s, e, ok)
	} else {
		prev = e
	}
	for i, w := range want {
		b := blocks[i]
		if b.Synthetic {
			t.Fatalf("block %d synthetic: %+v", i, b)
		}
		if b.Kind != w.kind {
			t.Errorf("block %d kind %v, want %v", i, b.Kind, w.kind)
		}
		if got := src[b.Start:b.Stop]; got != w.text {
			t.Errorf("block %d text %q, want %q", i, got, w.text)
		}
		if gap := src[prev:b.Start]; strings.TrimSpace(gap) != "" {
			t.Errorf("block %d gap %q is not whitespace", i, gap)
		}
		prev = b.Stop
	}
	if strings.TrimSpace(src[prev:]) != "" {
		t.Errorf("trailing %q is not whitespace", src[prev:])
	}
	if blocks[0].HeadingLevel != 1 || blocks[3].CodeLang != "go" {
		t.Errorf("heading level %d, code lang %q", blocks[0].HeadingLevel, blocks[3].CodeLang)
	}
}

func TestBlocksWithoutFrontmatterAndWithWindowsNewlines(t *testing.T) {
	doc := Parse([]byte("para\r\n\r\n## H\r\n"))
	if _, _, ok := doc.FrontmatterSpan(); ok {
		t.Fatal("no frontmatter expected")
	}
	blocks := doc.Blocks()
	if len(blocks) != 2 {
		t.Fatalf("got %+v", blocks)
	}
	src := "para\r\n\r\n## H\r\n"
	if src[blocks[0].Start:blocks[0].Stop] != "para\r" || src[blocks[1].Start:blocks[1].Stop] != "## H\r" {
		t.Fatalf("ranges keep the carriage return as part of the line: %+v", blocks)
	}
}

func TestFootnoteDefinitionsAtTheEndAreABlock(t *testing.T) {
	src := "text[^1]\n\n[^1]: the note\n"
	blocks := Parse([]byte(src)).Blocks()
	if len(blocks) != 2 || blocks[1].Kind != BlockFootnotes || blocks[1].Synthetic {
		t.Fatalf("got %+v", blocks)
	}
	if got := src[blocks[1].Start:blocks[1].Stop]; got != "[^1]: the note" {
		t.Fatalf("definitions text %q", got)
	}
}

func TestScatteredFootnoteDefinitionsAreSynthetic(t *testing.T) {
	src := "a[^1]\n\n[^1]: one\n\nmore prose\n"
	blocks := Parse([]byte(src)).Blocks()
	var fn *BlockInfo
	for i := range blocks {
		if blocks[i].Kind == BlockFootnotes {
			fn = &blocks[i]
		}
	}
	if fn == nil || !fn.Synthetic {
		t.Fatalf("a definition before later prose must not claim a range: %+v", blocks)
	}
}

func TestUnclosedFenceRunsToTheEnd(t *testing.T) {
	src := "```\nbody\nmore"
	blocks := Parse([]byte(src)).Blocks()
	if len(blocks) != 1 || src[blocks[0].Start:blocks[0].Stop] != src {
		t.Fatalf("got %+v", blocks)
	}
	src = "~~~~\nx\n~~~~\n\nafter"
	blocks = Parse([]byte(src)).Blocks()
	if len(blocks) != 2 || src[blocks[0].Start:blocks[0].Stop] != "~~~~\nx\n~~~~" {
		t.Fatalf("tilde fence: %+v", blocks)
	}
}

func TestFrontmatterSpanEdgeCases(t *testing.T) {
	if _, _, ok := frontmatterSpan([]byte("---\nno close")); ok {
		t.Fatal("an unclosed block is not frontmatter")
	}
	if _, _, ok := frontmatterSpan([]byte("--- not\n---\n")); ok {
		t.Fatal("a decorated opening line is not a delimiter")
	}
	if s, e, ok := frontmatterSpan([]byte("---\n---")); !ok || s != 0 || e != 7 {
		t.Fatalf("empty block: %d %d %v", s, e, ok)
	}
}

func TestRenderBlockHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	doc := Parse([]byte(blocksSample))
	ids := c.NewWidgetIdStack()
	for i := range doc.Blocks() {
		_ = RenderBlock(Input{Ids: ids, ScopeKey: "b" + strconv.Itoa(i), Doc: doc}, i)
	}
	// out of range draws nothing and does not panic
	_ = RenderBlock(Input{Ids: ids, Doc: doc}, len(doc.Blocks()))
	_ = RenderBlock(Input{Ids: ids, Doc: doc}, -1)
}

func TestSetextHeadingIncludesItsUnderline(t *testing.T) {
	src := "Title\n=====\n\npara\n\nSub\n---\n\n# ATX\n---\n"
	blocks := Parse([]byte(src)).Blocks()
	want := []string{"Title\n=====", "para", "Sub\n---", "# ATX", "---"}
	if len(blocks) != len(want) {
		t.Fatalf("want %d blocks, got %+v", len(want), blocks)
	}
	for i, w := range want {
		if got := src[blocks[i].Start:blocks[i].Stop]; got != w {
			t.Errorf("block %d: %q want %q", i, got, w)
		}
	}
	if blocks[0].Kind != BlockHeading || blocks[2].Kind != BlockHeading || blocks[4].Kind != BlockHorizontalRule {
		t.Errorf("kinds: %+v", blocks)
	}
}

func TestRenderBlockCountsNestedCodeBlocks(t *testing.T) {
	doc := Parse([]byte("- item\n\n  ```go\n  x\n  ```\n\n> ```\n> y\n> ```\n\n```\nz\n```\n"))
	n := 0
	for i := range doc.segments {
		n += countCodeBlocks(&doc.segments[i])
	}
	if n != 3 {
		t.Fatalf("counted %d code blocks, want 3: %+v", n, doc.Blocks())
	}
}

func TestFrontmatterSpanFollowsTheFeature(t *testing.T) {
	src := []byte("---\na: 1\n---\n\npara\n")
	if _, _, ok := Parse(src).FrontmatterSpan(); !ok {
		t.Fatal("default features: frontmatter expected")
	}
	doc := ParseWith(src, ParseOptions{Features: obsidian.FeatureGFM})
	if _, _, ok := doc.FrontmatterSpan(); ok {
		t.Fatal("frontmatter reported with the feature off")
	}
	blocks := doc.Blocks()
	if len(blocks) == 0 || blocks[0].Start != 0 {
		t.Fatalf("the delimiter lines are ordinary blocks without the feature: %+v", blocks)
	}
}

// A block id names its block (ADR-0347 §SD2): a trailing marker on a
// paragraph, a lone marker under a list. The marker stays inside the block's
// source range, so the blocks still cover the source.
func TestBlocksCarryTheirIds(t *testing.T) {
	src := "para ^p1\n\n- a\n- b\n\n^l1\n\n## H ^not-an-id\n"
	blocks := Parse([]byte(src)).Blocks()
	if len(blocks) != 3 {
		t.Fatalf("got %+v", blocks)
	}
	if blocks[0].Id != "p1" || src[blocks[0].Start:blocks[0].Stop] != "para ^p1" {
		t.Errorf("paragraph: %+v", blocks[0])
	}
	if blocks[1].Id != "l1" || src[blocks[1].Start:blocks[1].Stop] != "- a\n- b" {
		t.Errorf("list: %+v", blocks[1])
	}
	if blocks[2].Id != "" {
		t.Errorf("a heading's id attribute is its anchor, not a block id: %+v", blocks[2])
	}
}

// The offset target resolves to the block holding the offset, the block
// after a gap, and the last block past the end; a synthetic block is never
// the target.
func TestOffsetBlockIndex(t *testing.T) {
	src := "one\n\ntwo\n\nthree\n"
	doc := Parse([]byte(src))
	cases := map[int]int{0: 0, 2: 0, 3: 0, 4: 1, 5: 1, 8: 1, 10: 2, 15: 2, 40: 2}
	for off, want := range cases {
		if got := offsetBlockIndex(doc.segments, off); got != want {
			t.Errorf("offset %d: got block %d want %d", off, got, want)
		}
	}
	if got := offsetBlockIndex(nil, 3); got != -1 {
		t.Errorf("no blocks: got %d", got)
	}
}

// A `^id` through the section slot arms the block carrying it and consumes
// the slug, so a second block with the same id does not fire; an offset
// target arms its block; a heading slug leaves blocks alone.
func TestArmBlockScroll(t *testing.T) {
	rc := renderCtx{scrollToSlug: "^one"}
	other := segment{blockId: "two"}
	rc.armBlockScroll(&other, false)
	if rc.scrollNow || rc.scrollToSlug != "^one" {
		t.Fatalf("another id armed: %+v", rc)
	}
	one := segment{blockId: "one"}
	rc.armBlockScroll(&one, false)
	if !rc.scrollNow || rc.scrollToSlug != "" {
		t.Fatalf("the id did not arm and consume: %+v", rc)
	}
	rc.scrollNow = false
	rc.armBlockScroll(&one, false)
	if rc.scrollNow {
		t.Fatal("a consumed slug armed a second block")
	}
	rc = renderCtx{scrollToSlug: "one"}
	rc.armBlockScroll(&one, false)
	if rc.scrollNow {
		t.Fatal("a heading slug is not a block id")
	}
	rc.armBlockScroll(&one, true)
	if !rc.scrollNow {
		t.Fatal("the offset target did not arm")
	}
}
