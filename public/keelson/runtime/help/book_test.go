package help

import (
	"embed"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

//go:embed testdata/help
var testdataFS embed.FS

// helpFS returns the embedded test corpus rooted at the `help` directory,
// matching what apps will pass into Manifest.Help (apps typically do
// `//go:embed help`).
func helpFS(t *testing.T) (fsys fs.FS) {
	t.Helper()
	sub, err := fs.Sub(testdataFS, "testdata/help")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	fsys = sub
	return
}

func TestNewBook_NilFS(t *testing.T) {
	_, err := NewBook("github.com/test/app", nil)
	if err == nil {
		t.Fatalf("expected error on nil fs.FS")
	}
}

func TestBook_DocsIndex(t *testing.T) {
	b, err := NewBook("github.com/test/app", helpFS(t))
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	docs := b.Docs()
	if len(docs) != 3 {
		t.Fatalf("Docs(): got %d, want 3 — %+v", len(docs), docs)
	}
	// Path-sorted ordering invariant.
	want := []string{"howto/replay", "no-title", "overview"}
	for i := range docs {
		if docs[i].Path != want[i] {
			t.Errorf("Docs()[%d].Path: got %q, want %q", i, docs[i].Path, want[i])
		}
	}
}

func TestBook_TitleResolution(t *testing.T) {
	b, err := NewBook("github.com/test/app", helpFS(t))
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	cases := []struct {
		path  string
		title string
		typ   string
	}{
		// Frontmatter title wins.
		{"overview", "Overview", "explanation"},
		// No frontmatter title — falls through to first H1.
		{"howto/replay", "Replaying a session", "how-to"},
		// No frontmatter, no headings — falls through to filename leaf.
		{"no-title", "no-title", ""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			_, info, ok := b.Doc(tc.path)
			if !ok {
				t.Fatalf("Doc(%q): not found", tc.path)
			}
			if info.Title != tc.title {
				t.Errorf("Title: got %q, want %q", info.Title, tc.title)
			}
			if info.Type != tc.typ {
				t.Errorf("Type: got %q, want %q", info.Type, tc.typ)
			}
		})
	}
}

func TestBook_Sections(t *testing.T) {
	b, err := NewBook("github.com/test/app", helpFS(t))
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	_, info, ok := b.Doc("overview")
	if !ok {
		t.Fatalf("Doc(overview): not found")
	}
	want := []SectionInfo{
		{Slug: "overview", Text: "Overview", Level: 1},
		{Slug: "what-it-covers", Text: "What it covers", Level: 2},
		{Slug: "sub-section-with-caps", Text: "Sub-section with caps", Level: 2},
	}
	if len(info.Sections) != len(want) {
		t.Fatalf("Sections: got %d, want %d — %+v", len(info.Sections), len(want), info.Sections)
	}
	for i := range want {
		if info.Sections[i] != want[i] {
			t.Errorf("Sections[%d]: got %+v, want %+v", i, info.Sections[i], want[i])
		}
	}
}

func TestBook_HasSection(t *testing.T) {
	b, err := NewBook("github.com/test/app", helpFS(t))
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	if !b.HasSection("overview", "what-it-covers") {
		t.Errorf("HasSection(overview, what-it-covers) = false, want true")
	}
	if b.HasSection("overview", "missing") {
		t.Errorf("HasSection(overview, missing) = true, want false")
	}
	if b.HasSection("nonexistent", "x") {
		t.Errorf("HasSection on missing doc returned true")
	}
}

func TestBook_DocMissing(t *testing.T) {
	b, err := NewBook("github.com/test/app", helpFS(t))
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	_, _, ok := b.Doc("not-there")
	if ok {
		t.Errorf("Doc(not-there) = ok, want not-found")
	}
}

func TestBook_Source(t *testing.T) {
	b, err := NewBook("github.com/test/app", helpFS(t))
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	src, ok := b.Source("overview")
	if !ok {
		t.Fatalf("Source(overview): not found")
	}
	// The fixture starts with YAML frontmatter; verify the bytes are
	// the raw on-disk form (frontmatter intact, not stripped).
	if len(src) == 0 || src[0] != '-' {
		t.Errorf("Source(overview): want raw .md bytes, got %q...", string(src[:min(40, len(src))]))
	}
	// Negative: missing doc → ok=false, nil src.
	if src2, ok2 := b.Source("not-there"); ok2 || src2 != nil {
		t.Errorf("Source(not-there): got src=%v ok=%v, want nil/false", src2, ok2)
	}
}

func TestBook_AppId(t *testing.T) {
	id := app.AppIdT("github.com/test/something")
	b, err := NewBook(id, helpFS(t))
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	if b.AppId() != id {
		t.Errorf("AppId: got %q, want %q", b.AppId(), id)
	}
}

func TestBook_EmptyFS(t *testing.T) {
	b, err := NewBook("github.com/test/empty", fstest.MapFS{})
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	if docs := b.Docs(); len(docs) != 0 {
		t.Errorf("Docs(): got %d, want 0 — %+v", len(docs), docs)
	}
}

// TestBook_IgnoresNonMarkdown asserts the walk skips entries that
// happen to share a directory with help docs (assets, JSON sidecars,
// etc.) instead of treating them as docs.
func TestBook_IgnoresNonMarkdown(t *testing.T) {
	fsys := fstest.MapFS{
		"overview.md":      {Data: []byte("# Overview\n")},
		"assets/diag.png":  {Data: []byte("not really a png")},
		"meta.json":        {Data: []byte(`{"x":1}`)},
	}
	b, err := NewBook("github.com/test/mixed", fsys)
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	docs := b.Docs()
	if len(docs) != 1 || docs[0].Path != "overview" {
		t.Errorf("Docs(): got %+v, want one entry for overview", docs)
	}
}

// Compile-time guards that the fs.FS the tests pass implements the
// interface the library expects — keeps the test signature explicit
// rather than relying on the inferred type at the call site.
var (
	_ fs.FS = (fstest.MapFS)(nil)
	_ fs.FS = (*embed.FS)(nil)
)
