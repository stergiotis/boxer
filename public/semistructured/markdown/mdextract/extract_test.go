package mdextract_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdextract"
)

// goldenRegenEnvVar rewrites the golden instead of comparing against it. A
// test-only variable, outside the ADR-0009 registry — the vocabulary
// goldens' precedent.
const goldenRegenEnvVar = "BOXER_MDEXTRACT_GOLDEN_REGEN"

const (
	vaultDir = "testdata/vault"
	golden   = "testdata/vault-extraction.golden.json"
)

func readVault(t *testing.T) (docs map[string]*mdextract.Document) {
	t.Helper()
	docs = map[string]*mdextract.Document{}
	err := filepath.WalkDir(vaultDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		src, rerr := os.ReadFile(path)
		require.NoError(t, rerr)
		rel, rerr := filepath.Rel(vaultDir, path)
		require.NoError(t, rerr)
		docs[filepath.ToSlash(rel)] = mdextract.Extract(src)
		return nil
	})
	require.NoError(t, err)
	return
}

// TestVaultExtractionMatchesTheGolden pins the whole reading of the fixture
// vault. It is a golden because the thing worth catching is a change in what
// gets extracted — a parser priority moving, a feature flag flipping — and a
// property test cannot see that.
func TestVaultExtractionMatchesTheGolden(t *testing.T) {
	docs := readVault(t)
	names := make([]string, 0, len(docs))
	for n := range docs {
		names = append(names, n)
	}
	sort.Strings(names)
	ordered := make([]struct {
		File string
		Doc  *mdextract.Document
	}, 0, len(names))
	for _, n := range names {
		ordered = append(ordered, struct {
			File string
			Doc  *mdextract.Document
		}{n, docs[n]})
	}
	got, err := json.MarshalIndent(ordered, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')

	if os.Getenv(goldenRegenEnvVar) != "" {
		require.NoError(t, os.WriteFile(golden, got, 0o644))
		t.Skip("golden rewritten; unset " + goldenRegenEnvVar + " to compare against it")
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err, "read the golden; regenerate with "+goldenRegenEnvVar+"=1")
	assert.Equal(t, string(want), string(got))
}

func find(t *testing.T, docs map[string]*mdextract.Document, name string) *mdextract.Document {
	t.Helper()
	d, ok := docs[name]
	require.True(t, ok, "fixture %s", name)
	return d
}

func TestHeadingTree(t *testing.T) {
	d := find(t, readVault(t), "alpha.md")
	require.Len(t, d.Headings, 4)
	assert.Equal(t, "Alpha", d.Title)

	setup := d.Headings[1]
	assert.Equal(t, "Setup steps", setup.Text, "the anchor is stripped from the text")
	assert.Equal(t, "setup", setup.Anchor)
	assert.Equal(t, "setup", setup.Slug, "an explicit anchor is the slug")
	assert.Equal(t, 0, setup.Parent)
	assert.Equal(t, []string{"Alpha"}, setup.Path)

	notes := d.Headings[2]
	assert.Equal(t, uint8(3), notes.Level)
	assert.Equal(t, 1, notes.Parent)
	assert.Equal(t, []string{"Alpha", "Setup steps"}, notes.Path)

	status := d.Headings[3]
	assert.Equal(t, 0, status.Parent, "a level-2 after a level-3 climbs back to the level-1")
	assert.Equal(t, uint64(25), status.Line)
}

func TestSetextHeadingAndSlug(t *testing.T) {
	d := find(t, readVault(t), "projects/beta.md")
	require.Len(t, d.Headings, 3)
	assert.Equal(t, "Heading without content", d.Headings[1].Text)
	assert.Equal(t, "heading-without-content", d.Headings[1].Slug)
	assert.Equal(t, uint8(2), d.Headings[1].Level)
	assert.Equal(t, uint64(5), d.Headings[1].Line)
}

func TestLinksInEverySpelling(t *testing.T) {
	d := find(t, readVault(t), "index.md")
	byKind := map[mdextract.LinkKindE][]mdextract.Link{}
	for _, l := range d.Links {
		byKind[l.Kind] = append(byKind[l.Kind], l)
	}
	wl := byKind[mdextract.LinkKindWikilink]
	require.Len(t, wl, 3)
	assert.Equal(t, "Alpha", wl[0].Target)
	assert.Equal(t, "projects/beta", wl[1].Target)
	assert.Equal(t, "the beta project", wl[1].Text)
	assert.Equal(t, "Alpha", wl[2].Target)
	assert.Equal(t, "Setup steps", wl[2].Fragment)
	assert.Equal(t, uint64(21), wl[0].Line)
	assert.Equal(t, 0, wl[0].Section, "under the level-1 heading")

	ext := byKind[mdextract.LinkKindInline]
	require.Len(t, ext, 1)
	assert.Equal(t, "https://clickhouse.com/docs", ext[0].Target)
	assert.Equal(t, "arrays", ext[0].Fragment)
	assert.Equal(t, "ClickHouse docs", ext[0].Text)
	assert.True(t, ext[0].External)
	assert.Equal(t, 1, ext[0].Section, "under the level-2 heading")

	auto := byKind[mdextract.LinkKindAutolink]
	require.Len(t, auto, 1)
	assert.Equal(t, "https://example.org/page", auto[0].Target)
	assert.True(t, auto[0].External)

	img := byKind[mdextract.LinkKindImage]
	require.Len(t, img, 1)
	assert.Equal(t, "assets/my diagram.png", img[0].Target, "non-URL targets are percent-decoded")
	assert.Equal(t, "diagram", img[0].Text)
	assert.False(t, img[0].External)

	emb := byKind[mdextract.LinkKindEmbed]
	require.Len(t, emb, 1)
	assert.Equal(t, "Alpha", emb[0].Target)
	assert.Equal(t, "Setup steps", emb[0].Fragment)

	for _, l := range d.Links {
		assert.NotEqual(t, "Ghost", l.Target, "a commented-out link is not extracted")
	}
}

func TestMarkdownLinkToNoteWithFragment(t *testing.T) {
	d := find(t, readVault(t), "alpha.md")
	var inline []mdextract.Link
	for _, l := range d.Links {
		if l.Kind == mdextract.LinkKindInline {
			inline = append(inline, l)
		}
	}
	require.Len(t, inline, 1)
	assert.Equal(t, "projects/beta.md", inline[0].Target)
	assert.Equal(t, "goals", inline[0].Fragment)
	assert.False(t, inline[0].External)
	assert.Equal(t, 2, inline[0].Section)
}

func TestCodeBlocks(t *testing.T) {
	d := find(t, readVault(t), "alpha.md")
	require.Len(t, d.CodeBlocks, 1)
	cb := d.CodeBlocks[0]
	assert.Equal(t, "go", cb.Language)
	assert.Equal(t, `go title="main.go"`, cb.Info)
	assert.Equal(t, "package main\n\nfunc main() {}\n", cb.Content)
	assert.Equal(t, uint64(3), cb.Lines)
	assert.Equal(t, uint64(15), cb.Line, "the opening fence's line")
	assert.Equal(t, 1, cb.Section)

	idx := find(t, readVault(t), "index.md")
	require.Len(t, idx.CodeBlocks, 1)
	assert.Equal(t, "sql", idx.CodeBlocks[0].Language)
	assert.Equal(t, uint64(35), idx.CodeBlocks[0].Line)
}

func TestEmphasis(t *testing.T) {
	d := find(t, readVault(t), "index.md")
	styles := map[mdextract.EmphasisStyleE][]string{}
	for _, e := range d.Emphases {
		styles[e.Style] = append(styles[e.Style], e.Text)
	}
	assert.Equal(t, []string{"vault"}, styles[mdextract.EmphasisStyleBold])
	assert.Equal(t, []string{"setup"}, styles[mdextract.EmphasisStyleItalic])
	assert.Equal(t, []string{"highlighted"}, styles[mdextract.EmphasisStyleHighlight])
	assert.Equal(t, []string{"struck"}, styles[mdextract.EmphasisStyleStrikethrough])

	a := find(t, readVault(t), "alpha.md")
	var texts []string
	for _, e := range a.Emphases {
		texts = append(texts, e.Style.String()+":"+e.Text)
	}
	assert.Equal(t, []string{"italic:key", "bold:key"}, texts, "***x*** yields one entry per style")

	b := find(t, readVault(t), "projects/beta.md")
	texts = texts[:0]
	for _, e := range b.Emphases {
		texts = append(texts, e.Style.String()+":"+e.Text)
	}
	assert.Equal(t, []string{"italic:Italic", "bold:bold"}, texts, "underscore spellings")
}

func TestTagsFromBodyAndFrontmatter(t *testing.T) {
	d := find(t, readVault(t), "index.md")
	var body, fm []string
	for _, tg := range d.Tags {
		switch tg.Source {
		case mdextract.TagSourceBody:
			body = append(body, tg.Tag)
			assert.NotZero(t, tg.Line)
		case mdextract.TagSourceFrontmatter:
			fm = append(fm, tg.Tag)
			assert.Zero(t, tg.Line)
			assert.Equal(t, -1, tg.Section)
		}
	}
	assert.Equal(t, []string{"hub", "meta/structure"}, body, "#4 is a number, #ghost is commented out")
	assert.Equal(t, []string{"hub", "meta/structure"}, fm)
	for i, tg := range d.Tags {
		assert.Equal(t, uint64(i), tg.Ordinal, "ordinals run across both sources")
	}

	a := find(t, readVault(t), "alpha.md")
	fm = fm[:0]
	for _, tg := range a.Tags {
		if tg.Source == mdextract.TagSourceFrontmatter {
			fm = append(fm, tg.Tag)
		}
	}
	assert.Equal(t, []string{"project", "alpha"}, fm, "the comma-separated spelling")
}

func TestNormalizeTag(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"#tag", "tag", true},
		{"nested/tag", "nested/tag", true},
		{" my-tag ", "my-tag", true},
		{"trailing/", "trailing", true},
		{"_under", "_under", true},
		{"2024", "", false},
		{"#", "", false},
		{"-dash", "", false},
		{"has space", "", false},
		{"ünïcode", "", false},
	}
	for _, c := range cases {
		got, ok := mdextract.NormalizeTag(c.in)
		assert.Equal(t, c.ok, ok, c.in)
		assert.Equal(t, c.want, got, c.in)
	}
}

func TestFrontmatterLeaves(t *testing.T) {
	d := find(t, readVault(t), "index.md")
	require.NotNil(t, d.Frontmatter)
	fm := d.Frontmatter
	assert.Empty(t, fm.Err)
	assert.Zero(t, fm.Dropped)
	assert.Equal(t, []string{"Home", "Start Here"}, fm.Aliases)

	byPath := map[string][]mdextract.Leaf{}
	for _, l := range fm.Leaves {
		byPath[l.Path] = append(byPath[l.Path], l)
	}
	title := byPath["/title"]
	require.Len(t, title, 1)
	assert.Equal(t, mdextract.LeafKindString, title[0].Kind)
	assert.Equal(t, "Vault Index", title[0].S)
	assert.Nil(t, title[0].Params)

	tags := byPath["/tags/_"]
	require.Len(t, tags, 2)
	assert.Equal(t, []uint64{0}, tags[0].Params)
	assert.Equal(t, "hub", tags[0].S)
	assert.Equal(t, []uint64{1}, tags[1].Params)

	roles := byPath["/reviewers/_/roles/_"]
	require.Len(t, roles, 2, "two nested list positions under reviewer 0")
	assert.Equal(t, []uint64{0, 0}, roles[0].Params)
	assert.Equal(t, "lead", roles[0].S)
	assert.Equal(t, []uint64{0, 1}, roles[1].Params)

	emptyRoles := byPath["/reviewers/_/roles"]
	require.Len(t, emptyRoles, 1, "reviewer 1's empty list is a value-less leaf")
	assert.Equal(t, mdextract.LeafKindEmptyArray, emptyRoles[0].Kind)
	assert.Equal(t, []uint64{1}, emptyRoles[0].Params)

	require.Len(t, byPath["/extra"], 1)
	assert.Equal(t, mdextract.LeafKindEmptyObject, byPath["/extra"][0].Kind)
	require.Len(t, byPath["/notes"], 1)
	assert.Equal(t, mdextract.LeafKindNull, byPath["/notes"][0].Kind)

	require.Len(t, byPath["/rating"], 1)
	assert.Equal(t, mdextract.LeafKindFloat, byPath["/rating"][0].Kind)
	assert.Equal(t, 4.5, byPath["/rating"][0].F)
	require.Len(t, byPath["/draft"], 1)
	assert.Equal(t, mdextract.LeafKindBool, byPath["/draft"][0].Kind)
	assert.False(t, byPath["/draft"][0].B)
	require.Len(t, byPath["/created"], 1)
	assert.Equal(t, mdextract.LeafKindTime, byPath["/created"][0].Kind, "a YAML date is recognised on its text")
	assert.Equal(t, "2024-03-01", byPath["/created"][0].S)
	assert.Equal(t, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), byPath["/created"][0].T)

	// Sorted-key order within one level, document order preserved inside lists.
	var paths []string
	for _, l := range fm.Leaves {
		paths = append(paths, l.Path)
	}
	assert.True(t, sort.StringsAreSorted(paths[:2]), "aliases before created")
}

func TestFrontmatterAbsentAndBroken(t *testing.T) {
	docs := readVault(t)
	assert.Nil(t, find(t, docs, "plain.md").Frontmatter)
	assert.Nil(t, find(t, docs, "projects/beta.md").Frontmatter)

	broken := find(t, docs, "broken-frontmatter.md")
	require.NotNil(t, broken.Frontmatter)
	assert.NotEmpty(t, broken.Frontmatter.Err)
	assert.Empty(t, broken.Frontmatter.Leaves)
	assert.Equal(t, "Still Parsed", broken.Title, "the body is extracted regardless")
	require.Len(t, broken.Links, 1)
}

func TestWordsAndTitle(t *testing.T) {
	docs := readVault(t)
	plain := find(t, docs, "plain.md")
	assert.Equal(t, uint64(11), plain.Words)
	assert.Equal(t, "", plain.Title)
	assert.Empty(t, plain.Headings)
}

func TestLeafKindStringsAreTheStoredMarkers(t *testing.T) {
	assert.Equal(t, "null", mdextract.LeafKindNull.String())
	assert.Equal(t, "[]", mdextract.LeafKindEmptyArray.String())
	assert.Equal(t, "{}", mdextract.LeafKindEmptyObject.String())
}

func TestEscapedKeys(t *testing.T) {
	d := mdextract.Extract([]byte("---\n\"a/b\": 1\n\"c~d\": 2\n---\nbody\n"))
	require.NotNil(t, d.Frontmatter)
	var paths []string
	for _, l := range d.Frontmatter.Leaves {
		paths = append(paths, l.Path)
	}
	assert.Equal(t, []string{"/a~1b", "/c~0d"}, paths)
}

func TestParseTimestamp(t *testing.T) {
	utc := func(y int, mo time.Month, d, h, mi, sec, ns int) time.Time {
		return time.Date(y, mo, d, h, mi, sec, ns, time.UTC)
	}
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"2024-03-01", utc(2024, 3, 1, 0, 0, 0, 0), true},
		{" 2024-03-01 ", utc(2024, 3, 1, 0, 0, 0, 0), true},
		{"2024-03-01T10:20:30Z", utc(2024, 3, 1, 10, 20, 30, 0), true},
		{"2024-03-01t10:20:30z", utc(2024, 3, 1, 10, 20, 30, 0), true},
		{"2024-03-01 10:20:30", utc(2024, 3, 1, 10, 20, 30, 0), true},
		{"2024-03-01T10:20", utc(2024, 3, 1, 10, 20, 0, 0), true},
		{"2024-03-01T10:20:30.5+02:00", utc(2024, 3, 1, 8, 20, 30, 500_000_000), true},
		{"2024-03-01 10:20:30.25 -01:00", utc(2024, 3, 1, 11, 20, 30, 250_000_000), true},
		{"2024-03-01T10:20:30+0200", utc(2024, 3, 1, 8, 20, 30, 0), true},
		{"2024-03-01T10:20:30+02", utc(2024, 3, 1, 8, 20, 30, 0), true},
		{"2024-13-01", time.Time{}, false},
		{"2024-03-01T25:00:00Z", time.Time{}, false},
		{"20240301", time.Time{}, false},
		{"March 1, 2024", time.Time{}, false},
		{"2024-03-01 is the date", time.Time{}, false},
		{"", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := mdextract.ParseTimestamp(c.in)
		assert.Equal(t, c.ok, ok, c.in)
		assert.True(t, c.want.Equal(got), "%s: got %v", c.in, got)
	}
}

func TestTimeLeafFromQuotedAndUnquotedStrings(t *testing.T) {
	d := mdextract.Extract([]byte("---\nwhen: 2024-03-01T10:20:30Z\nquoted: \"2024-03-01\"\nnot: 2024-03\n---\n"))
	require.NotNil(t, d.Frontmatter)
	kinds := map[string]mdextract.LeafKindE{}
	for _, l := range d.Frontmatter.Leaves {
		kinds[l.Path] = l.Kind
	}
	assert.Equal(t, mdextract.LeafKindTime, kinds["/when"])
	assert.Equal(t, mdextract.LeafKindTime, kinds["/quoted"], "the decoder does not distinguish a quoted date")
	assert.Equal(t, mdextract.LeafKindString, kinds["/not"])
}
