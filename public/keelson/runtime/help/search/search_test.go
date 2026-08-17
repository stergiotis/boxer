package search

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

const testAppId app.AppIdT = "test/searchfixture"

// fixtureFS is a two-doc corpus exercising every slicing case: YAML
// frontmatter, a preamble before the first heading, nested heading
// levels, a fenced code block, and a doc with neither frontmatter nor
// preamble.
var fixtureFS = fstest.MapFS{
	"alpha.md": {Data: []byte(`---
title: Alpha Guide
type: how-to
status: draft
---

Preamble mentions penguins before any heading.

# Alpha Guide

Intro paragraph under the H1.

## Deduplicate rows

Use argMax over the sort key; the merge keeps the newest value.

## Sliding windows

Tumbling and hopping windows differ in overlap.

### Edge cases

Empty buckets at the range boundary need explicit fill.
`)},
	"beta.md": {Data: []byte(`# Beta Notes

Calling quantile( with no closing paren is a syntax error.

` + "```sql\nSELECT hasAny(tags, ['a']) FROM t\n```\n")},
}

func fixtureIndex(t *testing.T) *Index {
	t.Helper()
	b, err := help.NewBook(testAppId, fixtureFS)
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	return NewIndexBooks(b)
}

func TestCompileDegradesToLiteral(t *testing.T) {
	b := Compile([]string{"argMax", "quantile(", ""}, true)
	if len(b.Patterns) != 2 {
		t.Fatalf("want 2 patterns (empty dropped), got %d", len(b.Patterns))
	}
	if b.Patterns[0].Literal {
		t.Errorf("valid regex flagged literal")
	}
	if !b.Patterns[1].Literal {
		t.Errorf("unbalanced-paren token should degrade to literal")
	}
	if !b.Patterns[1].Matches("x quantile( y") {
		t.Errorf("degraded literal does not match its own text")
	}
	if b.Patterns[0].Matches("nothing here") {
		t.Errorf("pattern matches unrelated text")
	}
}

func TestParseQueryFieldsAndZero(t *testing.T) {
	b := ParseQuery("  argMax   windows ")
	if len(b.Patterns) != 2 || !b.RequireAll {
		t.Fatalf("want 2 required patterns, got %+v", b)
	}
	if z := ParseQuery("   "); !z.IsZero() {
		t.Errorf("blank query should compile to the zero battery")
	}
}

func TestBodyHitMapsToOwningSection(t *testing.T) {
	idx := fixtureIndex(t)
	hits := idx.Search(ParseQuery("argMax"), 0)
	if len(hits) != 1 {
		t.Fatalf("want exactly 1 hit, got %d: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.Ref.Doc != "alpha" || h.Ref.Section != "deduplicate-rows" {
		t.Errorf("hit landed on %s#%s, want alpha#deduplicate-rows", h.Ref.Doc, h.Ref.Section)
	}
	if !strings.Contains(h.Context, "argMax") {
		t.Errorf("context line %q misses the match", h.Context)
	}
	if h.Score != weightBody {
		t.Errorf("body-only hit scored %d, want %d", h.Score, weightBody)
	}
}

func TestFrontmatterExcludedFromBody(t *testing.T) {
	idx := fixtureIndex(t)
	for _, h := range idx.Search(ParseQuery("draft"), 0) {
		t.Errorf("frontmatter value leaked into body scan: %+v", h)
	}
}

func TestPreambleIsDocLevelSection(t *testing.T) {
	idx := fixtureIndex(t)
	hits := idx.Search(ParseQuery("penguins"), 0)
	if len(hits) != 1 || hits[0].Ref.Section != "" {
		t.Fatalf("preamble hit should land on the doc-level section, got %+v", hits)
	}
}

func TestTitleTierOutranksHeadingAndBody(t *testing.T) {
	idx := fixtureIndex(t)
	hits := idx.Search(ParseQuery("alpha"), 0)
	if len(hits) < 2 {
		t.Fatalf("want doc-level and H1 hits, got %+v", hits)
	}
	if hits[0].Ref.Section != "" || hits[0].Score != weightTitle {
		t.Errorf("top hit should be the doc-level title tier (score %d), got %+v", weightTitle, hits[0])
	}
	if hits[1].Ref.Section != "alpha-guide" || hits[1].Score != weightHeading {
		t.Errorf("second hit should be the H1 heading tier (score %d), got %+v", weightHeading, hits[1])
	}
}

func TestCodeBlockTextIsSearchable(t *testing.T) {
	idx := fixtureIndex(t)
	hits := idx.Search(ParseQuery("hasAny"), 0)
	if len(hits) != 1 || hits[0].Ref.Doc != "beta" {
		t.Fatalf("fenced-block text should match, got %+v", hits)
	}
}

func TestRequireAllSemantics(t *testing.T) {
	idx := fixtureIndex(t)
	if hits := idx.Search(ParseQuery("argMax windows"), 0); len(hits) != 0 {
		t.Errorf("no section holds both tokens; RequireAll should drop all, got %+v", hits)
	}
	hits := idx.Search(Compile([]string{"argMax", "hopping"}, false), 0)
	if len(hits) != 2 {
		t.Fatalf("any-mode should hit both sections, got %+v", hits)
	}
}

func TestRegexTokensWork(t *testing.T) {
	idx := fixtureIndex(t)
	hits := idx.Search(ParseQuery(`(tumbling|bogus)\s+and`), 0)
	if len(hits) != 1 || hits[0].Ref.Section != "sliding-windows" {
		t.Fatalf("regex token should match sliding-windows, got %+v", hits)
	}
}

func TestSearchLimitAndZeroBattery(t *testing.T) {
	idx := fixtureIndex(t)
	if hits := idx.Search(Battery{}, 0); hits != nil {
		t.Errorf("zero battery must return nil, got %+v", hits)
	}
	hits := idx.Search(Compile([]string{"the"}, false), 1)
	if len(hits) != 1 {
		t.Errorf("limit not applied: %d hits", len(hits))
	}
}

func TestContextLineTrimmed(t *testing.T) {
	long := strings.Repeat("x", 300) + " needle " + strings.Repeat("y", 300)
	got := contextLine(long, 301, 307)
	if len(got) > 120 {
		t.Errorf("context length %d exceeds cap", len(got))
	}
	if !strings.Contains(got, "needle") {
		t.Errorf("trimmed context %q lost the match", got)
	}
}

func TestFrontmatterEnd(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{"no frontmatter", 0},
		{"---\na: b\n---\nbody", 13},
		{"---\na: b\n--- not closed", 0},
		{"--- \na: b\n---\n", 0}, // opener must be exactly ---
	}
	for _, tc := range cases {
		if got := frontmatterEnd(tc.src); got != tc.want {
			t.Errorf("frontmatterEnd(%q) = %d, want %d", tc.src, got, tc.want)
		}
	}
}

func TestExpandDescendants(t *testing.T) {
	sections := []help.SectionInfo{
		{Slug: "h1", Level: 1},
		{Slug: "a", Level: 2},
		{Slug: "a-child", Level: 3},
		{Slug: "b", Level: 2},
		{Slug: "b-child", Level: 3},
	}
	out := ExpandDescendants(sections, map[string]bool{"a": true})
	if !out["a"] || !out["a-child"] {
		t.Errorf("descendant expansion missed a's subtree: %+v", out)
	}
	if out["b"] || out["b-child"] || out["h1"] {
		t.Errorf("expansion leaked outside a's subtree: %+v", out)
	}
	leaf := ExpandDescendants(sections, map[string]bool{"b-child": true})
	if !leaf["b-child"] || len(leaf) != 1 {
		t.Errorf("leaf-only acceptance widened unexpectedly: %+v", leaf)
	}
}

func TestCoverageOfHits(t *testing.T) {
	idx := fixtureIndex(t)
	// A match-everything pattern selects every region: non-empty bodies
	// match `.`, and the empty beta doc-level region rides its title
	// tier with zero SpanBytes — so bytes sum to the whole corpus.
	all := idx.Search(Compile([]string{"."}, false), 0)
	cov := idx.Coverage(all)
	if cov.SelBytes != cov.TotalBytes || cov.SelSections != cov.TotalSections {
		t.Errorf("match-everything should cover the corpus: %+v", cov)
	}
	if cov.Frac() != 1 {
		t.Errorf("Frac = %v, want 1", cov.Frac())
	}
	hits := idx.Search(ParseQuery("argMax"), 0)
	cov = idx.Coverage(hits)
	if cov.SelSections != 1 || cov.SelBytes <= 0 || cov.SelBytes >= cov.TotalBytes {
		t.Errorf("single-section hit coverage out of bounds: %+v", cov)
	}
	if f := cov.Frac(); f <= 0 || f >= 1 {
		t.Errorf("Frac = %v, want strictly between 0 and 1", f)
	}
	if z := (Coverage{}); z.Frac() != 0 {
		t.Errorf("zero coverage Frac = %v, want 0", z.Frac())
	}
}

func TestDocCoverage(t *testing.T) {
	idx := fixtureIndex(t)
	cov := idx.DocCoverage(testAppId, "alpha", map[string]bool{
		"sliding-windows": true,
		"edge-cases":      true,
	})
	if cov.SelSections != 2 || cov.SelBytes <= 0 || cov.SelBytes >= cov.TotalBytes {
		t.Errorf("accepted-subtree coverage out of bounds: %+v", cov)
	}
	if z := idx.DocCoverage(testAppId, "missing", nil); z != (Coverage{}) {
		t.Errorf("unknown doc should yield the zero Coverage, got %+v", z)
	}
	// Accepting "" selects the doc-level region only — never a
	// degenerate empty-slug heading.
	if c2 := idx.DocCoverage(testAppId, "alpha", map[string]bool{"": true}); c2.SelSections != 1 {
		t.Errorf(`accepted{""} should count exactly the doc-level region, got %+v`, c2)
	}
}

func TestThesaurusAlternation(t *testing.T) {
	th := Thesaurus{"lcase": {"lower"}}
	b := CompileWith([]string{"lcase"}, true, th)
	if len(b.Patterns) != 1 {
		t.Fatalf("want one pattern, got %+v", b)
	}
	p := &b.Patterns[0]
	if !p.Matches("SELECT LOWER(x)") || !p.Matches("lcase") {
		t.Errorf("alternation should match either spelling")
	}
	if got := p.EffectiveSource(); !strings.Contains(got, "(?:lcase|lower)") {
		t.Errorf("EffectiveSource = %q, want the alternation group", got)
	}
	if len(p.Alternates) != 1 || p.Alternates[0] != "lower" {
		t.Errorf("Alternates = %+v", p.Alternates)
	}
	// RequireAll semantics survive: one typed token, one battery entry.
	if !b.RequireAll || len(b.Patterns) != 1 {
		t.Errorf("thesaurus must not grow the battery")
	}
	// A token with top-level alternation keeps its own semantics inside
	// the group.
	b2 := CompileWith([]string{"a|b"}, true, Thesaurus{"a|b": {"c"}})
	p2 := &b2.Patterns[0]
	for _, text := range []string{"xax", "xbx", "xcx"} {
		if !p2.Matches(text) {
			t.Errorf("alternated regex token should match %q", text)
		}
	}
}

func TestThesaurusMultiWordAlternate(t *testing.T) {
	th := Thesaurus{"htop": {"Process monitor"}}
	b := CompileWith([]string{"htop"}, true, th)
	if !b.Patterns[0].Matches("the process   MONITOR pane") {
		t.Errorf("multi-word alternate should match across whitespace runs")
	}
	if b.Patterns[0].Matches("process") {
		t.Errorf("half a phrase must not match")
	}
}

func TestThesaurusFromManifestsCore(t *testing.T) {
	ms := []app.Manifest{
		{Display: "Process monitor", Keywords: []string{"htop", "cpu", "load average", ""}},
		{Display: "Process monitor", Keywords: []string{"htop"}}, // duplicate → dedup
		{Display: "", Keywords: []string{"ghost"}},               // no display → nothing to offer
	}
	th := thesaurusFromManifests(ms)
	if alts := th.alternates("HTOP"); len(alts) != 1 || alts[0] != "Process monitor" {
		t.Errorf("htop alternates = %+v", alts)
	}
	if th.alternates("load average") != nil {
		t.Errorf("multi-word keywords cannot be typed as one token and must be skipped")
	}
	if th.alternates("ghost") != nil {
		t.Errorf("display-less manifest contributed an alternate")
	}
}

func TestThesaurusCHFunctionsGenerated(t *testing.T) {
	th := ThesaurusCHFunctions()
	if len(th) == 0 {
		t.Fatal("generated alias table is empty — regenerate chaliases.gen.go")
	}
	if alts := th.alternates("lcase"); len(alts) != 1 || alts[0] != "lower" {
		t.Errorf("lcase alternates = %+v", alts)
	}
	for k, alts := range th {
		for _, a := range alts {
			if strings.EqualFold(k, a) {
				t.Errorf("self-referential entry %q -> %q survived generation", k, a)
			}
		}
	}
}

func TestDefaultThesaurusComposesAndSearchUsesIt(t *testing.T) {
	if DefaultThesaurus().alternates("lcase") == nil {
		t.Errorf("DefaultThesaurus should carry the CH alias table")
	}
	// End to end over the fixture corpus: a made-up alias reaches the
	// section that only spells the canonical name.
	idx := fixtureIndex(t)
	th := Thesaurus{"maxarg": {"argMax"}}
	hits := idx.Search(ParseQueryWith("maxarg", th), 0)
	if len(hits) != 1 || hits[0].Ref.Section != "deduplicate-rows" {
		t.Fatalf("thesaurus-enriched query should land on the canonical section, got %+v", hits)
	}
}
