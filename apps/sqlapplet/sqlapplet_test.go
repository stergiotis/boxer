package sqlapplet

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docMD assembles a minimal applet document. The type/status keys keep the
// help-book walker's documentation-standard conformance check quiet.
func docMD(front string, body string) *fstest.MapFile {
	// summary is required (ADR-0214 §SD4), and almost no test here is about
	// it, so the helper supplies one unless the caller's own frontmatter
	// already does. That keeps the summary rule's own cases explicit and
	// every other case unchanged.
	if !strings.Contains(front, "summary:") {
		front = front + "\nsummary: \"Fixture applet for parser tests\""
	}
	return &fstest.MapFile{Data: []byte("---\ntype: reference\nstatus: draft\n" + front + "\n---\n\n# Heading\n\nProse.\n\n" + body + "\n")}
}

const sqlFence = "```sql\nSELECT * FROM keelson('env')\n```"

// TestStarterBookCorpus is the ADR-0132 §SD6 hard gate over the embedded
// starter book: every doc parses, classifies, and mints cleanly.
func TestStarterBookCorpus(t *testing.T) {
	defs, errs := ParseBook("sqlapplet", help.MustSub(bookFS, "book"))
	require.Empty(t, errs)
	require.Len(t, defs, 5)

	docsSearch, recent, apps, env, timeline := defs[0], defs[1], defs[2], defs[3], defs[4]
	assert.Equal(t, "docs-search", docsSearch.Slug)
	assert.Equal(t, EndpointIntrospection, docsSearch.Endpoint)
	assert.Equal(t, []TabSel{{ID: "table"}, {ID: "detail"}}, docsSearch.Tabs)
	assert.Equal(t, analysis.QuerySecurityRead, docsSearch.Class, "docsearch('…') is a read like any SELECT")
	assert.Equal(t, []app.TopicT{app.TopicAbout}, docsSearch.Topics, "frontmatter topics overrides the book default")
	assert.Contains(t, docsSearch.Preamble, "Every pattern must hit")

	assert.Equal(t, "runtime-apps", apps.Slug)
	assert.Equal(t, "Runtime apps", apps.Title)
	assert.NotEmpty(t, apps.Icon)
	assert.Equal(t, EndpointIntrospection, apps.Endpoint)
	assert.Nil(t, apps.Tabs, "absent tabs key ⇒ auto")
	assert.Equal(t, analysis.QuerySecurityRead, apps.Class, "keelson('…') classifies as a local read")
	assert.False(t, apps.HasUnboundSlots)

	assert.Equal(t, "runtime-env", env.Slug)
	assert.Equal(t, EndpointIntrospection, env.Endpoint)
	assert.Equal(t, []TabSel{{ID: "table"}, {ID: "detail"}}, env.Tabs)
	assert.Equal(t, analysis.QuerySecurityRead, env.Class)
	assert.Contains(t, env.Preamble, "declares", "the `md preamble` fence rides the def")
	assert.NotContains(t, env.Preamble, "```", "the fence markers stay out of the text")
	assert.Empty(t, apps.Preamble, "absent fence ⇒ no preamble, not an empty strip")

	assert.Equal(t, "recent-queries", recent.Slug)
	assert.Equal(t, EndpointDefault, recent.Endpoint)
	assert.Equal(t, analysis.QuerySecurityRead, recent.Class)
	assert.False(t, recent.HasUnboundSlots, "the lim slot is prelude-bound — a param, not a signal")

	assert.Equal(t, "runtime-timeline", timeline.Slug)
	assert.Equal(t, EndpointIntrospection, timeline.Endpoint,
		"the trail is read through keelson('runtime_events'), not by decoding memberships in the buffer (ADR-0191 §SD7)")
	assert.Equal(t, []TabSel{{ID: "timeline"}, {ID: "table"}, {ID: "detail"}}, timeline.Tabs)
	assert.Equal(t, analysis.QuerySecurityRead, timeline.Class)
	assert.Equal(t, []app.TopicT{app.TopicRuntime, app.TopicObservability}, timeline.Topics,
		"frontmatter topics overrides the book default")
	assert.False(t, timeline.HasUnboundSlots, "all four slots are prelude-bound params")
	assert.Contains(t, timeline.Preamble, "Lanes are windows")

	// Every def carries the whole document, not just what parsing extracted
	// from it: the Definition drawer shows the source, so a def that dropped
	// it would mint an applet unable to say what it is.
	for _, def := range defs {
		assert.Contains(t, string(def.Source), def.SQL, "%s: Source is the document the buffer came from", def.Slug)
		assert.Contains(t, string(def.Source), "title:", "%s: frontmatter survives into Source", def.Slug)
	}
}

func TestMintStarterBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{{id: "sqlapplet", fsys: help.MustSub(bookFS, "book"), topics: []app.TopicT{app.TopicRuntime}}})
	require.Empty(t, errs)
	assert.Equal(t, 5, minted)

	m, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + "runtime-apps"))
	require.True(t, ok)
	assert.Equal(t, "Runtime apps", m.Display)
	assert.Equal(t, app.KindApplet, m.Kind, "provenance is a column, not a section (ADR-0158 §SD5)")
	assert.Equal(t, app.SurfaceWindowed, m.Surface)
	require.Len(t, m.Caps, 2, "attenuation in manifest form: the two escape hatches only")
	assert.Equal(t, clipboardbroker.SubjectWrite, m.Caps[0].Pattern)
	assert.Equal(t, windowhost.OpenSubject, m.Caps[1].Pattern, "Open in Playground cap (ADR-0135 §SD7)")
	assert.Empty(t, m.PersistedKeys, "committed definition ⇒ nothing to persist")

	// Factory dispatch yields a fresh AppI per open.
	a1, err := reg.Open(m.Id)
	require.NoError(t, err)
	a2, err := reg.Open(m.Id)
	require.NoError(t, err)
	assert.NotSame(t, a1, a2)
	assert.Equal(t, m.Id, a1.Manifest().Id)
}

func TestScanFences(t *testing.T) {
	src := []byte("prose\n```sql\nSELECT 1\n```\ntext\n```sql bands\nSELECT 2\n```\n```bash\nls\n```\n```\nplain\n```\n```sql\nunclosed")
	fences := scanFences(src)
	require.Len(t, fences, 4, "the unclosed trailing fence is dropped")
	assert.Equal(t, fence{Lang: "sql", Text: "SELECT 1"}, fences[0])
	assert.Equal(t, fence{Lang: "sql", Role: "bands", Text: "SELECT 2"}, fences[1])
	assert.Equal(t, fence{Lang: "bash", Text: "ls"}, fences[2])
	assert.Equal(t, fence{Lang: "", Text: "plain"}, fences[3])
}

// parseOne runs ParseBook over a single crafted document.
func parseOne(t *testing.T, name string, file *fstest.MapFile) (def *AppletDef, errs []error) {
	t.Helper()
	defs, errs := ParseBook("t", fstest.MapFS{name: file})
	if len(defs) > 0 {
		require.Len(t, defs, 1)
		def = defs[0]
	}
	return
}

func TestParseDocShapes(t *testing.T) {
	// A prose page (no fences) is not an applet and not an error.
	def, errs := parseOne(t, "overview.md", docMD("title: Overview", "no fences here"))
	assert.Nil(t, def)
	assert.Empty(t, errs)

	// A second role-less sql fence is a prose example, not the buffer.
	def, errs = parseOne(t, "two-fences.md", docMD("title: Two",
		"```sql\nSELECT 1\n```\n\n```sql\nSELECT 2\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Equal(t, "SELECT 1", def.SQL)

	// The bands aux fence lands beside the buffer.
	def, errs = parseOne(t, "banded.md", docMD("title: Banded",
		"```sql\nSELECT 1\n```\n\n```sql bands\nSELECT 2\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Equal(t, "SELECT 2", def.BandsSQL)

	// The preamble aux fence lands beside the buffer, trimmed; a plain `md`
	// fence is a prose example and claims nothing.
	def, errs = parseOne(t, "prefaced.md", docMD("title: Prefaced",
		"```md preamble\nCounts are **per package**.\n```\n\n```sql\nSELECT 1\n```\n\n```md\njust an example\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Equal(t, "Counts are **per package**.", def.Preamble)

	// `markdown` is accepted alongside `md`.
	def, errs = parseOne(t, "prefaced-long.md", docMD("title: Prefaced long",
		"```sql\nSELECT 1\n```\n\n```markdown preamble\nRead me first.\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Equal(t, "Read me first.", def.Preamble)

	// A mutating buffer parses and mints — it just never auto-runs (§SD3).
	def, errs = parseOne(t, "setter.md", docMD("title: Setter",
		"```sql\nSET max_threads = 4; SELECT 1\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Equal(t, analysis.QuerySecurityMutating, def.Class)

	// An unbound slot — a signal — sets HasUnboundSlots (Live preset at
	// mount, §SD3); a prelude-bound one does not.
	def, errs = parseOne(t, "slotted.md", docMD("title: Slotted",
		"```sql\nSELECT * FROM t WHERE id = {selection_id:UInt64}\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.True(t, def.HasUnboundSlots)

	def, errs = parseOne(t, "bound-slot.md", docMD("title: Bound slot",
		"```sql\nSET param_lim = 10; SELECT * FROM t LIMIT {lim:UInt64}\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.False(t, def.HasUnboundSlots)

	// Explicit tabs with a node binding.
	def, errs = parseOne(t, "bound.md", docMD("title: Bound\ntabs: [\"table:recent\", detail]",
		"```sql\nWITH recent AS (SELECT 1) SELECT * FROM recent\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Equal(t, []TabSel{{ID: "table", Node: "recent"}, {ID: "detail"}}, def.Tabs)

	// A zone suffix places the pane; it composes with a node binding, and the
	// zone comes last because the binding syntax predates it.
	def, errs = parseOne(t, "placed.md", docMD("title: Placed\ntabs: [treemap, \"detail@side\", \"table:rows@bottom\"]",
		"```sql\nWITH rows AS (SELECT 1) SELECT * FROM rows\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Equal(t, []TabSel{
		{ID: "treemap"},
		{ID: "detail", Zone: "side"},
		{ID: "table", Node: "rows", Zone: "bottom"},
	}, def.Tabs)

	// A datasets list declares the ad-hoc aliases the buffer references
	// (ADR-0134 §SD4); absent is nil.
	def, errs = parseOne(t, "ds.md", docMD("title: DS\ndatasets: [items, series_a]",
		"```sql\nSELECT * FROM keelson('items')\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Equal(t, []string{"items", "series_a"}, def.Datasets)
	assert.Empty(t, def.DatasetsHint)

	// datasets_hint is the one line the notice strip shows while an alias is
	// still unbound.
	def, errs = parseOne(t, "dshint.md", docMD("title: DS\ndatasets: [items]\ndatasets_hint: \"Publish it from Foo.\"",
		"```sql\nSELECT * FROM keelson('items')\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Equal(t, "Publish it from Foo.", def.DatasetsHint)

	def, errs = parseOne(t, "nods.md", docMD("title: NoDS", "```sql\nSELECT 1\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Nil(t, def.Datasets)
}

func TestParseDocErrors(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		front   string
		body    string
		errPart string
	}{
		{"unknown_role", "a.md", "title: A", "```sql wat\nSELECT 1\n```\n" + sqlFence, "unknown `sql` fence role"},
		{"unknown_md_role", "a.md", "title: A", "```md epilogue\nhi\n```\n" + sqlFence, "unknown `md` fence role"},
		{"bands_without_buffer", "a.md", "title: A", "```sql bands\nSELECT 1\n```", "aux fence without a buffer"},
		{"preamble_without_buffer", "a.md", "title: A", "```md preamble\nhi\n```", "aux fence without a buffer"},
		{"double_bands", "a.md", "title: A", sqlFence + "\n```sql bands\nSELECT 1\n```\n```sql bands\nSELECT 2\n```", "more than one"},
		{"double_preamble", "a.md", "title: A", sqlFence + "\n```md preamble\na\n```\n```md preamble\nb\n```", "more than one"},
		{"unparseable_sql", "a.md", "title: A", "```sql\nINSERT INTO t VALUES (1)\n```", "does not parse"},
		{"empty_buffer", "a.md", "title: A", "```sql\n\n```", "empty sql buffer"},
		{"bad_slug", "bad_slug.md", "title: A", sqlFence, "must match"},
		{"missing_title", "a.md", "status: draft", sqlFence, "`title` is required"},
		{"bad_endpoint", "a.md", "title: A\nendpoint: nowhere", sqlFence, "unknown endpoint"},
		{"tabs_unknown_panel", "a.md", "title: A\ntabs: [editor]", sqlFence, "not a result panel"},
		{"tabs_empty_node", "a.md", "title: A\ntabs: [\"table:\"]", sqlFence, "empty node binding"},
		{"tabs_duplicate", "a.md", "title: A\ntabs: [table, table]", sqlFence, "twice"},
		{"tabs_unknown_zone", "a.md", "title: A\ntabs: [\"table@basement\"]", sqlFence, "body, side and bottom"},
		{"tabs_chrome_zone", "a.md", "title: A\ntabs: [\"table@editor\"]", sqlFence, "body, side and bottom"},
		{"tabs_empty_zone", "a.md", "title: A\ntabs: [\"table@\"]", sqlFence, "body, side and bottom"},
		{"tabs_bad_shape", "a.md", "title: A\ntabs: yes-please", sqlFence, "must be \"auto\" or a list"},
		{"datasets_not_list", "a.md", "title: A\ndatasets: nope", sqlFence, "must be a list"},
		{"datasets_bad_alias", "a.md", "title: A\ndatasets: [bad-alias]", sqlFence, "not a bare identifier"},
		{"datasets_duplicate", "a.md", "title: A\ndatasets: [items, items]", sqlFence, "twice"},
		{"datasets_hint_not_string", "a.md", "title: A\ndatasets: [items]\ndatasets_hint: [a, b]", sqlFence, "must be a string"},
		{"datasets_hint_without_datasets", "a.md", "title: A\ndatasets_hint: \"nowhere to show\"", sqlFence, "never renders"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def, errs := parseOne(t, tc.file, docMD(tc.front, tc.body))
			assert.Nil(t, def)
			require.Len(t, errs, 1)
			assert.Contains(t, errs[0].Error(), tc.errPart)
		})
	}
}

func TestMintDuplicateSlugAcrossBooks(t *testing.T) {
	mk := func() *fstest.MapFile { return docMD("title: Dup", sqlFence) }
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "book-a", fsys: fstest.MapFS{"dup.md": mk()}, topics: []app.TopicT{app.TopicRuntime}},
		{id: "book-b", fsys: fstest.MapFS{"dup.md": mk()}, topics: []app.TopicT{app.TopicRuntime}},
	})
	assert.Equal(t, 1, minted, "first book wins deterministically (sorted by book id)")
	require.Len(t, errs, 1)
	assert.True(t, strings.Contains(errs[0].Error(), "already minted"))
}

func TestResolveClientConfig(t *testing.T) {
	// An explicit endpoint override wins.
	cfg, err := resolveClientConfig(&AppletDef{Slug: "x"}, "http://over/query")
	require.NoError(t, err)
	assert.Equal(t, "http://over/query", cfg.URL)

	// Introspection with no live endpoint (empty in tests) errors clearly.
	_, err = resolveClientConfig(&AppletDef{Slug: "x", Endpoint: EndpointIntrospection}, "")
	require.Error(t, err)
}

func TestNewEmbedded(t *testing.T) {
	def := &AppletDef{
		Slug:     "demo",
		Title:    "Demo",
		SQL:      "SELECT * FROM keelson('items')",
		Endpoint: EndpointIntrospection,
		Class:    analysis.QuerySecurityRead,
		Datasets: []string{"items"},
	}
	pa, err := NewEmbedded(def, EmbedConfig{
		StampAppId:  "embedder#demo",
		RunId:       "run1",
		Log:         zerolog.Nop(),
		EndpointURL: "http://example.invalid/query",
		Bindings:    map[string]string{"items": "adhoc_deadbeef01234567"},
	})
	require.NoError(t, err)
	require.NotNil(t, pa)
	t.Cleanup(func() { pa.Close() })

	// A malformed binding is rejected.
	_, err = NewEmbedded(def, EmbedConfig{
		Log:         zerolog.Nop(),
		EndpointURL: "http://example.invalid/query",
		Bindings:    map[string]string{"items": "bad-handle"},
	})
	require.Error(t, err)
}
