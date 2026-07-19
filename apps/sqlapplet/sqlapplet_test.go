package sqlapplet

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docMD assembles a minimal applet document. The type/status keys keep the
// help-book walker's documentation-standard conformance check quiet.
func docMD(front string, body string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("---\ntype: reference\nstatus: draft\n" + front + "\n---\n\n# Heading\n\nProse.\n\n" + body + "\n")}
}

const sqlFence = "```sql\nSELECT * FROM keelson('env')\n```"

// TestStarterBookCorpus is the ADR-0132 §SD6 hard gate over the embedded
// starter book: every doc parses, classifies, and mints cleanly.
func TestStarterBookCorpus(t *testing.T) {
	defs, errs := ParseBook("sqlapplet", help.MustSub(bookFS, "book"))
	require.Empty(t, errs)
	require.Len(t, defs, 2)

	apps, env := defs[0], defs[1]
	assert.Equal(t, "runtime-apps", apps.Slug)
	assert.Equal(t, "Runtime apps", apps.Title)
	assert.NotEmpty(t, apps.Icon)
	assert.Equal(t, EndpointIntrospection, apps.Endpoint)
	assert.Nil(t, apps.Tabs, "absent tabs key ⇒ auto")
	assert.Equal(t, analysis.QuerySecurityRead, apps.Class, "keelson('…') classifies as a local read")
	assert.False(t, apps.HasSlots)

	assert.Equal(t, "runtime-env", env.Slug)
	assert.Equal(t, EndpointIntrospection, env.Endpoint)
	assert.Equal(t, []TabSel{{ID: "table"}, {ID: "detail"}}, env.Tabs)
	assert.Equal(t, analysis.QuerySecurityRead, env.Class)
}

func TestMintStarterBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{{id: "sqlapplet", fsys: help.MustSub(bookFS, "book")}})
	require.Empty(t, errs)
	assert.Equal(t, 2, minted)

	m, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + "runtime-apps"))
	require.True(t, ok)
	assert.Equal(t, "Runtime apps", m.Display)
	assert.Equal(t, "Applets", m.Category)
	assert.Equal(t, app.SurfaceWindowed, m.Surface)
	assert.Empty(t, m.Caps, "attenuation in manifest form: no bus reach")
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

	// A mutating buffer parses and mints — it just never auto-runs (§SD3).
	def, errs = parseOne(t, "setter.md", docMD("title: Setter",
		"```sql\nSET max_threads = 4; SELECT 1\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Equal(t, analysis.QuerySecurityMutating, def.Class)

	// A slotted buffer notes HasSlots (Live preset at mount, §SD3).
	def, errs = parseOne(t, "slotted.md", docMD("title: Slotted",
		"```sql\nSELECT * FROM t WHERE id = {selection_id:UInt64}\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.True(t, def.HasSlots)

	// Explicit tabs with a node binding.
	def, errs = parseOne(t, "bound.md", docMD("title: Bound\ntabs: [\"table:recent\", detail]",
		"```sql\nWITH recent AS (SELECT 1) SELECT * FROM recent\n```"))
	require.Empty(t, errs)
	require.NotNil(t, def)
	assert.Equal(t, []TabSel{{ID: "table", Node: "recent"}, {ID: "detail"}}, def.Tabs)
}

func TestParseDocErrors(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		front   string
		body    string
		errPart string
	}{
		{"unknown_role", "a.md", "title: A", "```sql wat\nSELECT 1\n```\n" + sqlFence, "unknown fence role"},
		{"bands_without_buffer", "a.md", "title: A", "```sql bands\nSELECT 1\n```", "aux fence without a buffer"},
		{"double_bands", "a.md", "title: A", sqlFence + "\n```sql bands\nSELECT 1\n```\n```sql bands\nSELECT 2\n```", "more than one"},
		{"unparseable_sql", "a.md", "title: A", "```sql\nINSERT INTO t VALUES (1)\n```", "does not parse"},
		{"empty_buffer", "a.md", "title: A", "```sql\n\n```", "empty sql buffer"},
		{"bad_slug", "bad_slug.md", "title: A", sqlFence, "must match"},
		{"missing_title", "a.md", "status: draft", sqlFence, "`title` is required"},
		{"bad_endpoint", "a.md", "title: A\nendpoint: nowhere", sqlFence, "unknown endpoint"},
		{"tabs_unknown_panel", "a.md", "title: A\ntabs: [editor]", sqlFence, "not a result panel"},
		{"tabs_empty_node", "a.md", "title: A\ntabs: [\"table:\"]", sqlFence, "empty node binding"},
		{"tabs_duplicate", "a.md", "title: A\ntabs: [table, table]", sqlFence, "twice"},
		{"tabs_bad_shape", "a.md", "title: A\ntabs: yes-please", sqlFence, "must be \"auto\" or a list"},
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
		{id: "book-a", fsys: fstest.MapFS{"dup.md": mk()}},
		{id: "book-b", fsys: fstest.MapFS{"dup.md": mk()}},
	})
	assert.Equal(t, 1, minted, "first book wins deterministically (sorted by book id)")
	require.Len(t, errs, 1)
	assert.True(t, strings.Contains(errs[0].Error(), "already minted"))
}
