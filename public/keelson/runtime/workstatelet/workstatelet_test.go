package workstatelet

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

const sampleDoc = `---
type: reference
audience: end-user
status: draft
title: Trail review
summary: "Open the trail beside a query window"
topics: [observability]
keywords: [audit, trail]
desktop:
  layout: main-stack
  mainRatio: 0.6
---

Prose a reader sees, with a fence that is not a window:

` + "```sql\nSELECT 1\n```" + `

## Trail

` + "```yaml window\napp: trailviewer\n```" + `

## Query

` + "```yaml window\nkind: playLaunch\nfloating: true\nconfig: |\n  AQID\n  BAUG\n```" + `
`

func TestParse(t *testing.T) {
	doc, err := Parse("trail-review", []byte(sampleDoc), nil)
	require.NoError(t, err)
	assert.Equal(t, "Trail review", doc.Title)
	assert.Equal(t, "Open the trail beside a query window", doc.Summary)
	assert.Equal(t, []app.TopicT{app.TopicObservability}, doc.Topics)
	assert.Equal(t, []string{"audit", "trail"}, doc.Keywords)
	assert.Equal(t, Desktop{Layout: "main-stack", MainRatio: 0.6}, doc.Desktop)
	require.Len(t, doc.Windows, 2, "the sql fence is commentary, not a window")
	assert.Equal(t, Window{App: "trailviewer"}, doc.Windows[0])
	assert.Equal(t, Window{Kind: "playLaunch", Floating: true, Config: []byte{1, 2, 3, 4, 5, 6}}, doc.Windows[1],
		"a wrapped config reads as one base64 text")
}

func TestParseDefaultTopics(t *testing.T) {
	src := strings.Replace(sampleDoc, "topics: [observability]\n", "", 1)
	doc, err := Parse("trail-review", []byte(src), []app.TopicT{app.TopicRuntime})
	require.NoError(t, err)
	assert.Equal(t, []app.TopicT{app.TopicRuntime}, doc.Topics)

	_, err = Parse("trail-review", []byte(src), nil)
	assert.Error(t, err, "a document with no topics and no defaults cannot be filed")
}

func TestParseRefuses(t *testing.T) {
	edit := func(old, new string) string {
		require.Contains(t, sampleDoc, old)
		return strings.Replace(sampleDoc, old, new, 1)
	}
	cases := map[string]string{
		"no frontmatter":         "# Trail review\n",
		"unknown frontmatter":    edit("title: Trail review", "title: Trail review\nlaunch: play"),
		"no title":               edit("title: Trail review\n", ""),
		"no summary":             edit("summary: \"Open the trail beside a query window\"\n", ""),
		"unregistered topic":     edit("topics: [observability]", "topics: [gardening]"),
		"layout not an ident":    edit("layout: main-stack", "layout: \"Main Stack\""),
		"ratio out of range":     edit("mainRatio: 0.6", "mainRatio: 1.5"),
		"parameters, no layout":  edit("  layout: main-stack\n", ""),
		"unknown window key":     edit("app: trailviewer", "app: trailviewer\nrect: [0, 0, 1, 1]"),
		"neither app nor kind":   edit("app: trailviewer", "floating: true"),
		"config without a kind":  edit("kind: playLaunch\n", "app: play\n"),
		"config not base64":      edit("  AQID\n  BAUG", "  not base64!"),
		"app with yaml syntax":   edit("app: trailviewer", "app: \"trail viewer\""),
		"unclosed fence":         sampleDoc + "\n```yaml window\napp: play\n",
		"no window":              edit("```yaml window\napp: trailviewer\n```", "") + "",
		"frontmatter not closed": "---\ntitle: x\n",
	}
	// "no window" above still has the second window; remove both for it.
	cases["no window"] = strings.Replace(cases["no window"], "```yaml window\nkind: playLaunch", "```yaml\nkind: playLaunch", 1)
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("trail-review", []byte(src), nil)
			assert.Error(t, err)
		})
	}
	_, err := Parse("Trail Review", []byte(sampleDoc), nil)
	assert.Error(t, err, "a slug is lower-case alphanumerics and dashes")
}

func TestParseRefusesAnOversizedConfigAndTooManyWindows(t *testing.T) {
	doc := Doc{Slug: "big", Title: "Big", Summary: "Too big to open", Topics: []app.TopicT{app.TopicRuntime},
		Windows: []Window{{Kind: "k", Config: make([]byte, MaxConfigBytes+1)}}}
	assert.Error(t, doc.Validate())

	doc.Windows = nil
	for range MaxWindows + 1 {
		doc.Windows = append(doc.Windows, Window{App: "play"})
	}
	assert.Error(t, doc.Validate())
	doc.Windows = doc.Windows[:MaxWindows]
	assert.NoError(t, doc.Validate())
}

func TestComposeReadsBack(t *testing.T) {
	cfg := make([]byte, 200)
	for i := range cfg {
		cfg[i] = byte(i * 7)
	}
	in := Doc{
		Slug:     "my-desk",
		Title:    "My desk: trail & query",
		Summary:  "Two windows, \"quoted\" and side by side",
		Icon:     icons.PhBrowsers,
		Topics:   []app.TopicT{app.TopicObservability, app.TopicSql},
		Keywords: []string{"desk", "two words"},
		Desktop:  Desktop{Layout: "main-stack", MainRatio: 0.55, MainCount: 2},
		Windows: []Window{
			{App: "github.com/example/apps/trail"},
			{App: "play", Kind: "playLaunch", Config: cfg, Floating: true},
			{Kind: "playLaunch"},
		},
		Note: "Captured from the desktop.",
	}
	composed, err := Compose(in)
	require.NoError(t, err)
	assert.Contains(t, string(composed.Source), "Captured from the desktop.")
	for _, line := range strings.Split(string(composed.Source), "\n") {
		assert.LessOrEqual(t, len(line), 100, "a config is wrapped")
	}

	out, err := Parse(in.Slug, composed.Source, nil)
	require.NoError(t, err)
	assert.Equal(t, in.Title, out.Title)
	assert.Equal(t, in.Summary, out.Summary)
	assert.Equal(t, in.Icon, out.Icon)
	assert.Equal(t, in.Topics, out.Topics)
	assert.Equal(t, in.Keywords, out.Keywords)
	assert.Equal(t, in.Desktop, out.Desktop)
	assert.Equal(t, in.Windows, out.Windows)
}

func TestComposeWithoutADesktopWritesNone(t *testing.T) {
	composed, err := Compose(Doc{Slug: "one", Title: "One", Summary: "One window", Topics: []app.TopicT{app.TopicRuntime},
		Windows: []Window{{App: "play"}}})
	require.NoError(t, err)
	assert.NotContains(t, string(composed.Source), "desktop")
	out, err := Parse("one", composed.Source, nil)
	require.NoError(t, err)
	assert.Equal(t, Desktop{}, out.Desktop)
}

func TestSlugOf(t *testing.T) {
	for title, want := range map[string]string{
		"Trail review":         "trail-review",
		"  My desk: 2 windows": "my-desk-2-windows",
		"Ünïcode desk":         "n-code-desk",
		"play":                 "play",
	} {
		got, ok := SlugOf(title)
		assert.True(t, ok, title)
		assert.Equal(t, want, got, title)
	}
	_, ok := SlugOf("—  —")
	assert.False(t, ok)
}

func TestMintBooks(t *testing.T) {
	reg, cat := app.NewRegistry(), NewCatalog()
	fsys := fstest.MapFS{
		"trail-review.md": {Data: []byte(sampleDoc)},
		"broken.md":       {Data: []byte("no frontmatter\n")},
		"notes.txt":       {Data: []byte("not a document")},
	}
	docs, errs := ParseBook("test", fsys, []app.TopicT{app.TopicRuntime})
	require.Len(t, docs, 1)
	require.Len(t, errs, 1, "a document that does not parse is reported and its sibling still reads")

	id, err := cat.Mint(reg, docs[0], OriginBook, "test")
	require.NoError(t, err)
	assert.Equal(t, AppIdOf("trail-review"), id)
	assert.Equal(t, "ws-trail-review", id.SubjectAlias())

	m, ok := reg.LookupManifest(id)
	require.True(t, ok)
	assert.Equal(t, app.KindWorkstatelet, m.Kind)
	assert.Equal(t, "Trail review", m.Display)
	assert.Empty(t, m.LaunchKind)
	assert.Empty(t, m.Caps)

	doc, ok := cat.Lookup(id)
	require.True(t, ok)
	assert.Len(t, doc.Windows, 2)
	e, _ := cat.Entry(id)
	assert.Equal(t, OriginBook, e.Origin)
	assert.Equal(t, "test", e.Book)

	a, err := reg.Open(id)
	require.NoError(t, err, "the registry's constructor yields the card")
	assert.Equal(t, id, a.Manifest().Id)

	_, err = cat.Mint(reg, docs[0], OriginBook, "test")
	assert.Error(t, err, "a second mint of one slug is the registry's duplicate")
	assert.Len(t, cat.Entries(), 1)
}

func deskDoc(slug string, title string, apps ...string) (doc Doc) {
	doc = Doc{Slug: slug, Title: title, Summary: "A captured desktop", Topics: []app.TopicT{app.TopicRuntime}}
	for _, a := range apps {
		doc.Windows = append(doc.Windows, Window{App: a})
	}
	return
}

func TestStoreSavesMintsAndLoads(t *testing.T) {
	storage := NewMemoryStorage()
	reg, cat := app.NewRegistry(), NewCatalog()
	store := NewStore(reg, cat, storage, zerolog.Nop())
	store.Load()

	id, replaced, err := store.Save(deskDoc("my-desk", "My desk", "play", "trailviewer"))
	require.NoError(t, err)
	assert.False(t, replaced)
	_, ok := reg.LookupManifest(id)
	assert.True(t, ok, "a saved document is minted at once")
	e, _ := cat.Entry(id)
	assert.Equal(t, OriginStored, e.Origin)

	_, replaced, err = store.Save(deskDoc("my-desk", "My desk", "play"))
	require.NoError(t, err)
	assert.True(t, replaced, "saving a stored slug again replaces its document")
	doc, _ := cat.Lookup(id)
	assert.Len(t, doc.Windows, 1)

	_, _, err = store.Save(deskDoc("other", "Other", "play"))
	require.NoError(t, err)

	// The next boot: a fresh registry and catalog over the same storage.
	reg2, cat2 := app.NewRegistry(), NewCatalog()
	NewStore(reg2, cat2, storage, zerolog.Nop()).Load()
	require.Len(t, cat2.Entries(), 2)
	doc, ok = cat2.Lookup(AppIdOf("my-desk"))
	require.True(t, ok)
	assert.Len(t, doc.Windows, 1, "the replaced document is the one stored")
}

func TestStoreCommittedSlugWins(t *testing.T) {
	storage := NewMemoryStorage()
	reg, cat := app.NewRegistry(), NewCatalog()
	_, err := cat.Mint(reg, deskDoc("desk", "Committed desk", "play"), OriginBook, "test")
	require.NoError(t, err)

	store := NewStore(reg, cat, storage, zerolog.Nop())
	_, _, err = store.Save(deskDoc("desk", "Captured desk", "trailviewer"))
	assert.Error(t, err, "a capture cannot take a committed document's slug")
	assert.Empty(t, storage.m, "a refused save stores nothing")

	// A stored document whose slug a book took since: the book wins at load.
	other := NewMemoryStorage()
	_, _, err = NewStore(app.NewRegistry(), NewCatalog(), other, zerolog.Nop()).Save(deskDoc("desk", "Captured desk", "trailviewer"))
	require.NoError(t, err)
	NewStore(reg, cat, other, zerolog.Nop()).Load()
	doc, _ := cat.Lookup(AppIdOf("desk"))
	assert.Equal(t, "Committed desk", doc.Title)
}
