package markdown

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
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

// An explicit `{#anchor}` names the section instead of its title: it is
// stripped from the heading text and becomes the Slug, which is what lets a
// heading be retitled without invalidating the fragments pointing at it.
func TestParse_HeadingAnchor_NamesTheSection(t *testing.T) {
	src := "## Creating a table {#creating-a-table}\n\nbody\n"
	doc := Parse([]byte(src))
	if len(doc.headings) != 1 {
		t.Fatalf("headings: got %d want 1", len(doc.headings))
	}
	h := doc.headings[0]
	if h.Text != "Creating a table" {
		t.Errorf("Text: got %q want %q — the anchor belongs to the slug, not the title", h.Text, "Creating a table")
	}
	if h.Slug != "creating-a-table" {
		t.Errorf("Slug: got %q want %q", h.Slug, "creating-a-table")
	}
	if h.Level != 2 {
		t.Errorf("Level: got %d want 2", h.Level)
	}
	// ByteOffset still points at the heading text, not at the `#` marker:
	// trimming the anchor moves the line's end, never its start.
	if h.ByteOffset < 0 || !strings.HasPrefix(src[h.ByteOffset:], "Creating a table") {
		t.Errorf("ByteOffset %d does not land on the heading text in %q", h.ByteOffset, src)
	}
}

// The anchor is sanitised like a derived slug, so a Slug is in one form
// whatever its origin — an anchor in another case would otherwise never match
// the fragment a wikilink or docref resolves to.
func TestParse_HeadingAnchor_SanitisedLikeADerivedSlug(t *testing.T) {
	doc := Parse([]byte("## Mixed {#Creating-A-Table}\n"))
	if len(doc.headings) != 1 || doc.headings[0].Slug != "creating-a-table" {
		t.Fatalf("Slug: got %+v want slug %q", doc.headings, "creating-a-table")
	}
}

// The anchor must terminate the line; a trailing full stop leaves the braces
// as the literal text they are. Asserted so the strictness is a decision on
// record rather than an accident of the upstream parser.
func TestParse_HeadingAnchor_MustTerminateTheLine(t *testing.T) {
	doc := Parse([]byte("## Creating a table {#creating-a-table}.\n"))
	if len(doc.headings) != 1 {
		t.Fatalf("headings: got %d want 1", len(doc.headings))
	}
	if doc.headings[0].Text != "Creating a table {#creating-a-table}." {
		t.Errorf("Text: got %q, want the braces kept literal", doc.headings[0].Text)
	}
	if doc.headings[0].Slug == "creating-a-table" {
		t.Error("Slug: a non-terminating anchor must not name the section")
	}
}

func TestParse_HeadingAnchor_DisabledByFeatures(t *testing.T) {
	cfg := defaultConfig()
	doc := Parse([]byte("## Creating a table {#creating-a-table}\n"),
		WithFeatures(cfg.features&^obsidian.FeatureHeadingAnchor))
	if len(doc.headings) != 1 {
		t.Fatalf("headings: got %d want 1", len(doc.headings))
	}
	if doc.headings[0].Text != "Creating a table {#creating-a-table}" {
		t.Errorf("Text: got %q, want the anchor left in place with the feature off", doc.headings[0].Text)
	}
}

// The section machinery keys on Slug, so an anchored heading is addressable
// by its anchor — the payoff for a caller that filters or scrolls to a
// section it linked to earlier.
func TestParse_HeadingAnchor_SectionFilterKeysOnTheAnchor(t *testing.T) {
	src := "## Creating a table {#creating-a-table}\n\nbody\n\n## Other\n\nother body\n"
	doc := Parse([]byte(src))
	got := visibleSegments(doc.segments, doc.headings, func(slug string) bool { return slug == "creating-a-table" })
	want := []bool{true, true, false, false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visibility = %v, want %v", got, want)
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

// ---------------- GFM tables ----------------------------------------------

// tableSeg finds the single segKindTable segment in a doc, failing the
// test when there is not exactly one. Every table assertion below starts
// here, so a lowering that drops the table (the pre-table-support
// behaviour) fails loudly rather than silently passing an empty grid.
func tableSeg(t *testing.T, doc *Doc) (seg *segment) {
	t.Helper()
	for i := range doc.segments {
		if doc.segments[i].kind != segKindTable {
			continue
		}
		if seg != nil {
			t.Fatalf("expected exactly one table segment; got kinds=%v", kindsOf(doc.segments))
		}
		seg = &doc.segments[i]
	}
	if seg == nil {
		t.Fatalf("no segKindTable segment; got kinds=%v", kindsOf(doc.segments))
	}
	return
}

func TestParse_GFMTable_LowersToTableSegment(t *testing.T) {
	src := strings.Join([]string{
		"| Stage | Reads | Writes |",
		"|-------|-------|--------|",
		"| describe | Go types | IR |",
		"| map | IR | mapping plan |",
	}, "\n") + "\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 1 {
		t.Fatalf("segments: got %d want 1 (kinds=%v)", len(doc.segments), kindsOf(doc.segments))
	}
	seg := tableSeg(t, doc)

	if seg.tableCols != 3 {
		t.Errorf("tableCols: got %d want 3", seg.tableCols)
	}
	wantHeader := []string{"Stage", "Reads", "Writes"}
	if !reflect.DeepEqual(seg.tableHeader, wantHeader) {
		t.Errorf("tableHeader: got %#v want %#v", seg.tableHeader, wantHeader)
	}
	wantCells := []string{
		"describe", "Go types", "IR",
		"map", "IR", "mapping plan",
	}
	if !reflect.DeepEqual(seg.tableCells, wantCells) {
		t.Errorf("tableCells: got %#v want %#v", seg.tableCells, wantCells)
	}
	// Row count is derived, never stored — the renderer computes it the
	// same way to size the table op.
	if rows := len(seg.tableCells) / int(seg.tableCols); rows != 2 {
		t.Errorf("derived rows: got %d want 2", rows)
	}
}

func TestParse_GFMTable_HeaderOnly_NoBodyRows(t *testing.T) {
	// A delimiter row with no data rows is legal GFM. The header must
	// still lower, with an empty body — the table op then renders a
	// header and zero rows.
	src := "| a | b |\n|---|---|\n"
	doc := Parse([]byte(src))
	seg := tableSeg(t, doc)
	if !reflect.DeepEqual(seg.tableHeader, []string{"a", "b"}) {
		t.Errorf("tableHeader: got %#v want [a b]", seg.tableHeader)
	}
	if len(seg.tableCells) != 0 {
		t.Errorf("tableCells: got %#v want empty", seg.tableCells)
	}
}

func TestParse_GFMTable_EmptyCells_StayInGrid(t *testing.T) {
	// An empty cell must occupy its slot, not collapse — otherwise every
	// following cell shifts one column left in the row-major body.
	src := strings.Join([]string{
		"| a | b | c |",
		"|---|---|---|",
		"| 1 |  | 3 |",
		"|  |  |  |",
	}, "\n") + "\n"
	doc := Parse([]byte(src))
	seg := tableSeg(t, doc)
	wantCells := []string{
		"1", "", "3",
		"", "", "",
	}
	if !reflect.DeepEqual(seg.tableCells, wantCells) {
		t.Errorf("tableCells: got %#v want %#v", seg.tableCells, wantCells)
	}
}

func TestParse_GFMTable_RaggedRows_PadAndTruncate(t *testing.T) {
	// The delimiter row fixes the column count at 3. A short row is
	// padded with empty cells and an over-long one is truncated, so the
	// body stays exactly rows×cols and cells[row*cols+col] addresses
	// what the reader sees.
	src := strings.Join([]string{
		"| a | b | c |",
		"|---|---|---|",
		"| short | row |",
		"| too | many | cells | here |",
		"| just | right | ok |",
	}, "\n") + "\n"
	doc := Parse([]byte(src))
	seg := tableSeg(t, doc)
	if seg.tableCols != 3 {
		t.Fatalf("tableCols: got %d want 3", seg.tableCols)
	}
	wantCells := []string{
		"short", "row", "",
		"too", "many", "cells",
		"just", "right", "ok",
	}
	if !reflect.DeepEqual(seg.tableCells, wantCells) {
		t.Errorf("tableCells: got %#v want %#v", seg.tableCells, wantCells)
	}
	if len(seg.tableCells)%int(seg.tableCols) != 0 {
		t.Errorf("body is not rectangular: %d cells over %d columns",
			len(seg.tableCells), seg.tableCols)
	}
}

func TestParse_GFMTable_InlineContentFlattensToText(t *testing.T) {
	// Cells take a plain string, so styling, code spans, links,
	// wikilinks and autolinks all reduce to their visible text. The
	// wikilink and autolink cases are the ones that would render blank
	// under a Text-nodes-only flattener.
	src := strings.Join([]string{
		"| kind | cell |",
		"|------|------|",
		"| styled | **bold** and *italic* |",
		"| code | `reflect` walk |",
		"| link | see [the docs](https://example.com/docs) |",
		"| wikilink | [[SomePage]] |",
		"| autolink | <https://example.com> |",
	}, "\n") + "\n"
	doc := Parse([]byte(src))
	seg := tableSeg(t, doc)
	wantCells := []string{
		"styled", "bold and italic",
		"code", "reflect walk",
		"link", "see the docs",
		"wikilink", "SomePage",
		"autolink", "https://example.com",
	}
	if !reflect.DeepEqual(seg.tableCells, wantCells) {
		t.Errorf("tableCells: got %#v want %#v", seg.tableCells, wantCells)
	}
}

func TestParse_GFMTable_AlignmentRow_DoesNotLeakIntoCells(t *testing.T) {
	// Alignment is parsed by goldmark and deliberately not applied by
	// the renderer (no alignment knob on tableColumn / tableCellText).
	// What must hold regardless: the `:---:` delimiter row is consumed
	// as structure and never surfaces as a body row.
	src := strings.Join([]string{
		"| left | centre | right |",
		"|:-----|:------:|------:|",
		"| 1 | 2 | 3 |",
	}, "\n") + "\n"
	doc := Parse([]byte(src))
	seg := tableSeg(t, doc)
	if !reflect.DeepEqual(seg.tableHeader, []string{"left", "centre", "right"}) {
		t.Errorf("tableHeader: got %#v", seg.tableHeader)
	}
	if !reflect.DeepEqual(seg.tableCells, []string{"1", "2", "3"}) {
		t.Errorf("tableCells: got %#v want [1 2 3]", seg.tableCells)
	}
}

func TestParse_GFMTable_WithoutGFMFeature_StaysProse(t *testing.T) {
	// The table nodes only exist because FeatureGFM is in the default
	// set. Dropping it must leave prose, not a half-lowered table.
	src := "| a | b |\n|---|---|\n| 1 | 2 |\n"
	doc := Parse([]byte(src), WithFeatures(obsidian.FeatureFrontmatter))
	for _, seg := range doc.segments {
		if seg.kind == segKindTable {
			t.Fatal("table segment lowered even though FeatureGFM is off")
		}
	}
}

func TestParse_GFMTable_InListItem(t *testing.T) {
	// lowerBlock is shared by the nested-block walkers, so a table
	// indented under a list item lowers the same way.
	src := strings.Join([]string{
		"- item",
		"",
		"  | a | b |",
		"  |---|---|",
		"  | 1 | 2 |",
	}, "\n") + "\n"
	doc := Parse([]byte(src))
	if len(doc.segments) != 1 || doc.segments[0].kind != segKindList {
		t.Fatalf("expected one list segment; got kinds=%v", kindsOf(doc.segments))
	}
	found := false
	for _, item := range doc.segments[0].children {
		for _, ch := range item.children {
			if ch.kind == segKindTable {
				found = true
				if ch.tableCols != 2 {
					t.Errorf("nested tableCols: got %d want 2", ch.tableCols)
				}
				if !reflect.DeepEqual(ch.tableCells, []string{"1", "2"}) {
					t.Errorf("nested tableCells: got %#v want [1 2]", ch.tableCells)
				}
			}
		}
	}
	if !found {
		t.Error("no table segment under the list item")
	}
}

func TestParse_GFMTable_ColumnRunes_WidestCellWins(t *testing.T) {
	// The per-column rune maximum drives the initial column widths. It
	// must span the header and the whole body, not just one of them —
	// column 0's widest cell is its header, column 1's is a body cell.
	src := strings.Join([]string{
		"| a longish header | b |",
		"|------------------|---|",
		"| x | a much longer body cell |",
		"| y | short |",
	}, "\n") + "\n"
	doc := Parse([]byte(src))
	seg := tableSeg(t, doc)
	want := []uint32{
		uint32(len("a longish header")),
		uint32(len("a much longer body cell")),
	}
	if !reflect.DeepEqual(seg.tableColRunes, want) {
		t.Errorf("tableColRunes: got %#v want %#v", seg.tableColRunes, want)
	}
}

func TestTableColumnWidth_ClampsToBounds(t *testing.T) {
	colCap := tableColumnCap(2)
	if got := tableColumnWidth(0, colCap); got != tableColumnMinWidth {
		t.Errorf("tableColumnWidth(0): got %v want the minimum %v", got, tableColumnMinWidth)
	}
	if got := tableColumnWidth(10_000, colCap); got != colCap {
		t.Errorf("tableColumnWidth(10000): got %v want the cap %v", got, colCap)
	}
	// A mid-range column lands strictly inside the bounds and grows with
	// its content.
	narrow, wide := tableColumnWidth(10, colCap), tableColumnWidth(20, colCap)
	if narrow <= tableColumnMinWidth || wide >= colCap {
		t.Fatalf("test inputs no longer straddle the clamp: %v, %v (cap %v)", narrow, wide, colCap)
	}
	if wide <= narrow {
		t.Errorf("width should grow with rune count: 10 runes → %v, 20 runes → %v", narrow, wide)
	}
}

func TestTableColumnCap_TightensAsColumnsMultiply(t *testing.T) {
	// The last column is the remainder and gets whatever the fixed ones
	// leave over, so the fixed columns must share one budget: a wide
	// table has to accept narrower columns or the remainder starves.
	narrow2, narrow5 := tableColumnCap(2), tableColumnCap(5)
	if narrow5 >= narrow2 {
		t.Errorf("cap should tighten with more columns: 2 cols → %v, 5 cols → %v", narrow2, narrow5)
	}
	// Fixed columns claim at most the budget, whatever the column count.
	for _, cols := range []int{1, 2, 3, 5, 9, 40} {
		fixed := cols - 1
		if fixed < 1 {
			continue
		}
		if claimed := tableColumnCap(cols) * float32(fixed); claimed > tableColumnBudget {
			// The floor is allowed to exceed the budget — below
			// tableColumnMinWidth a column is unreadable — but nothing else is.
			if tableColumnCap(cols) != tableColumnMinWidth {
				t.Errorf("cols=%d: fixed columns claim %v > budget %v", cols, claimed, tableColumnBudget)
			}
		}
	}
	if got := tableColumnCap(1); got < tableColumnMinWidth || got > tableColumnMaxWidth {
		t.Errorf("tableColumnCap(1): got %v, want it inside the absolute bounds", got)
	}
}

func TestTableRowHeight_TracksTypeScale(t *testing.T) {
	// A table with a header sizes off the larger heading step, because
	// the interpreter draws header cells with ui.heading() and one row
	// height covers header and body alike.
	d := styletokens.DensityFromEnv()
	wantHeader := styletokens.ScaledPt(styletokens.HeadingPt, d) * tableRowHeightFactor
	wantBody := styletokens.ScaledPt(styletokens.BodyPt, d) * tableRowHeightFactor
	if got := tableRowHeight(true); got != wantHeader {
		t.Errorf("tableRowHeight(true): got %v want %v", got, wantHeader)
	}
	if got := tableRowHeight(false); got != wantBody {
		t.Errorf("tableRowHeight(false): got %v want %v", got, wantBody)
	}
	if wantHeader <= wantBody {
		t.Errorf("header row height %v should exceed body row height %v", wantHeader, wantBody)
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
		"| col | col |",
		"|-----|-----|",
		"| 1 | 2 |",
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
		segKindTable,
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

// WithCodeActionFilter must withhold the BUTTONS, not merely let the host
// ignore the click — an affordance that does nothing is worse than none.
//
// The buttons themselves need a live Ui, so what is asserted here is the
// contract the render path reads: the filter sees every fenced block, with its
// language, in document order.
func TestCodeActionFilterSeesEveryBlock(t *testing.T) {
	doc := Parse([]byte("prose\n\n```sql\nSELECT 1\n```\n\n" +
		"more\n\n```response\n\u250c\u2500a\u2500\u2510\n```\n\n```\nbare\n```\n"))

	var ro renderOptions
	WithCodeActionFilter(func(text, lang string) bool {
		return lang == "sql" || lang == ""
	})(&ro)
	if ro.actionAccept == nil {
		t.Fatal("WithCodeActionFilter must install the predicate")
	}

	var got [][2]string
	var accepted []string
	for i := range doc.segments {
		if doc.segments[i].kind != segKindCodeBlock {
			continue
		}
		text, lang := doc.segments[i].codeText, doc.segments[i].codeLang
		got = append(got, [2]string{text, lang})
		if ro.actionAccept(text, lang) {
			accepted = append(accepted, lang)
		}
	}
	want := [][2]string{
		{"SELECT 1\n", "sql"},
		{"\u250c\u2500a\u2500\u2510\n", "response"},
		{"bare\n", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("blocks offered = %q, want %q", got, want)
	}
	// The response block — ClickHouse query output — must not be actionable.
	if !reflect.DeepEqual(accepted, []string{"sql", ""}) {
		t.Errorf("accepted = %q, want the sql and untyped blocks only", accepted)
	}
}

// The zero option set accepts everything, which is what keeps the behaviour of
// callers that never pass the filter unchanged.
func TestCodeActionFilterDefaultsToEverything(t *testing.T) {
	var ro renderOptions
	if ro.actionAccept != nil {
		t.Error("nil predicate means accept every block")
	}
}

// Sections follow the parse-time heading side table: a top-level
// heading opens a section, the doc-level region before the first
// heading is slug "", and a heading nested inside a container is NOT a
// boundary — it stays with its enclosing section (and, symmetrically,
// is absent from Doc.Headings).
func TestVisibleSegments_SectionMembership(t *testing.T) {
	src := "preamble text\n\n" +
		"## Alpha\n\nalpha body\n\n" +
		"> ## Nested heading stays with alpha\n\n" +
		"## Beta\n\nbeta body\n"
	doc := Parse([]byte(src))
	if len(doc.headings) != 2 || doc.headings[0].Slug != "alpha" || doc.headings[1].Slug != "beta" {
		t.Fatalf("side table should hold the two top-level headings only, got %+v", doc.headings)
	}
	// Expected top-level lowering: para(preamble), heading(Alpha),
	// para(alpha body), blockquote, heading(Beta), para(beta body).
	want := []bool{false, true, true, true, false, false}
	if len(doc.segments) != len(want) {
		kinds := make([]segKindE, len(doc.segments))
		for i := range doc.segments {
			kinds[i] = doc.segments[i].kind
		}
		t.Fatalf("segment layout changed: %d segments, kinds %v — update `want`", len(doc.segments), kinds)
	}
	got := visibleSegments(doc.segments, doc.headings, func(slug string) bool { return slug == "alpha" })
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visibility = %v, want %v", got, want)
	}
	pre := visibleSegments(doc.segments, doc.headings, func(slug string) bool { return slug == "" })
	if !pre[0] || pre[1] || pre[3] {
		t.Errorf("doc-level filter should keep only the preamble, got %v", pre)
	}
}

// A section filter cannot coexist with scroll-to-section: skipped
// headings would desynchronise the dispatch's heading ordinals, so
// renderCollect disarms the scroll when a filter is present. Assert at
// the option level (the render path needs a live FFFI sink).
func TestVisibleSegments_FilterPresenceKnown(t *testing.T) {
	var ro renderOptions
	WithSectionFilter(func(string) bool { return true })(&ro)
	if ro.sectionAccept == nil {
		t.Fatal("WithSectionFilter did not install the predicate")
	}
	if ro.sectionAccept("anything") != true {
		t.Error("predicate not passed through")
	}
}

// ---------------------------------------------------------------------------
// Obsidian tags
// ---------------------------------------------------------------------------

// TestParse_TagSurvivesLowering is the trap this feature had to clear before
// it could be enabled: an inline node the lowering does not enumerate reaches
// emitInline's default branch and is SILENTLY DROPPED, and a tag carries its
// text in a field rather than in child Text nodes — so turning the parser
// feature on without the lowering case would have deleted the tag from the
// document instead of rendering it unstyled.
func TestParse_TagSurvivesLowering(t *testing.T) {
	doc := Parse([]byte("## Release #v2 notes\n\nFiled under #project/frontend today.\n"))

	if len(doc.headings) != 1 {
		t.Fatalf("headings: got %d want 1", len(doc.headings))
	}
	// The heading's text becomes its SLUG, so a dropped tag here is a broken
	// scroll target and a broken fragment link, not only a missing word.
	if doc.headings[0].Text != "Release #v2 notes" {
		t.Errorf("heading text: got %q, want the tag kept", doc.headings[0].Text)
	}
	// Stated as an equality against the feature being OFF rather than as a
	// literal slug: what has to hold is that turning tags on moved no existing
	// section's address, and a hardcoded slug would assert SlugHeading's
	// sanitisation instead — a different contract, owned elsewhere.
	cfg := defaultConfig()
	off := Parse([]byte("## Release #v2 notes\n"), WithFeatures(cfg.features&^obsidian.FeatureTag))
	if len(off.headings) != 1 {
		t.Fatalf("headings with the feature off: got %d want 1", len(off.headings))
	}
	if doc.headings[0].Slug != off.headings[0].Slug {
		t.Errorf("slug moved when tags were enabled: %q with, %q without",
			doc.headings[0].Slug, off.headings[0].Slug)
	}
}

// TestParse_TagInATableCellIsNotBlank covers the other flattener — the one
// whose doc comment already records this exact failure mode for wikilinks and
// embeds: a cell whose entire content is a node carrying its text in a field
// renders empty unless the flattener knows about it.
func TestParse_TagInATableCellIsNotBlank(t *testing.T) {
	doc := Parse([]byte("| topic | tag |\n|---|---|\n| one | #alpha |\n"))
	seg := tableSeg(t, doc)
	found := false
	for _, cell := range seg.tableCells {
		if strings.Contains(cell, "#alpha") {
			found = true
		}
	}
	if !found {
		t.Errorf("a cell holding only a tag rendered blank: %+v", seg.tableCells)
	}
}

// TestParse_TagRulesAreTheParsers pins the two rules that make the feature
// safe to have on by default over technical prose, at the level a reader of
// this package cares about: what survives into the document.
func TestParse_TagRulesAreTheParsers(t *testing.T) {
	doc := Parse([]byte("# C#sharp and open question #4\n"))
	if len(doc.headings) != 1 {
		t.Fatalf("headings: got %d want 1", len(doc.headings))
	}
	if doc.headings[0].Text != "C#sharp and open question #4" {
		t.Errorf("heading text: got %q, want both left as prose", doc.headings[0].Text)
	}
}
