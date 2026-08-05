package providers

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

func TestHelpSectionRows(t *testing.T) {
	fsys := fstest.MapFS{
		"overview.md": {Data: []byte("---\ntitle: Fixture\ntype: how-to\nstatus: draft\n---\n\npreamble\n\n# Fixture\n\n## Alpha\n\nalpha body\n")},
	}
	lib := help.NewLibrary()
	b, err := help.NewBook("test/app", fsys)
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	if err = lib.Register(b); err != nil {
		t.Fatalf("Register: %v", err)
	}
	rows := helpSectionRows(lib)
	// Doc-level region + H1 + H2.
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(rows), rows)
	}
	if rows[0].section != "" || !strings.Contains(rows[0].body, "preamble") {
		t.Errorf("row 0 should be the doc-level region, got %+v", rows[0])
	}
	if strings.Contains(rows[0].body, "status: draft") {
		t.Errorf("frontmatter leaked into the doc-level body")
	}
	last := rows[2]
	if last.section != "alpha" || last.heading != "Alpha" || last.level != 2 {
		t.Errorf("H2 row mismatch: %+v", last)
	}
	if !strings.Contains(last.body, "alpha body") {
		t.Errorf("H2 body missing text: %q", last.body)
	}
	if last.ref != "help://test/app::overview#alpha" {
		t.Errorf("ref = %q", last.ref)
	}
	if last.title != "Fixture" || last.kind != "how-to" {
		t.Errorf("doc identity mismatch: %+v", last)
	}
}

func TestAdrSectionRowsAndParseCache(t *testing.T) {
	content := "---\ntype: adr\nstatus: proposed\ndate: 2026-01-01\n---\n\n# ADR-0007: Fixture decision\n\n## Context\n\nwhy\n\n## Decision\n\nwhat\n"
	rows := adrSectionRows([]adrcorpus.AdrContent{{Num: 7, Path: "doc/adr/0007-x.md", Content: content}})
	// Doc-level + H1 + two H2s.
	if len(rows) != 4 {
		t.Fatalf("want 4 rows, got %d", len(rows))
	}
	ctx := rows[2]
	if ctx.section != "context" || ctx.ref != "adr://0007#context" {
		t.Errorf("context row mismatch: %+v", ctx)
	}
	if ctx.title != "ADR-0007: Fixture decision" || ctx.kind != "proposed" {
		t.Errorf("ADR identity mismatch: %+v", ctx)
	}
	if !strings.Contains(ctx.body, "why") || strings.Contains(ctx.body, "what") {
		t.Errorf("context body slice wrong: %q", ctx.body)
	}

	// Same content again: the cache must serve the same parse.
	before, _ := adrParseCache.Load(7)
	_ = adrSectionRows([]adrcorpus.AdrContent{{Num: 7, Path: "p", Content: content}})
	after, _ := adrParseCache.Load(7)
	if before != after {
		t.Errorf("unchanged content should reuse the cached parse")
	}
	// Changed content: revalidation must re-parse.
	_ = adrSectionRows([]adrcorpus.AdrContent{{Num: 7, Path: "p", Content: content + "\nmore\n"}})
	again, _ := adrParseCache.Load(7)
	if again == after {
		t.Errorf("changed content should re-parse")
	}
}

func TestDocsectionsSchemasBuild(t *testing.T) {
	if helpsectionsTable(nil).Schema() == nil || adrsectionsTable(nil).Schema() == nil {
		t.Fatal("schemas must build from empty rosters")
	}
}
