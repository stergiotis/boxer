package sqlapplet

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

const ladingBookChapters = 9

func ladingDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("lading", help.MustSub(bookladingFS, "booklading"))
	require.Empty(t, errs)
	require.Len(t, defs, ladingBookChapters)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, ladingBookChapters)
	return bySlug
}

func TestBookLadingRegistered(t *testing.T) {
	entries, err := bookladingFS.ReadDir("booklading")
	require.NoError(t, err)
	require.NotEmpty(t, entries, "booklading must embed at least one page")

	var found bool
	booksMu.Lock()
	for _, b := range books {
		if b.id == "lading" {
			found = true
			break
		}
	}
	booksMu.Unlock()
	require.True(t, found, "the lading book must be registered by init")
}

func TestMintLadingBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "lading", fsys: help.MustSub(bookladingFS, "booklading"),
			topics: []app.TopicT{app.TopicData}},
	})
	require.Empty(t, errs)
	assert.Equal(t, ladingBookChapters, minted)
}

// TestLadingBookCorpus — every chapter reads, every knob is prelude-bound, the
// tab selections are as declared, and every chapter names a lading macro with
// the mount as the `m` knob (ADR-0200 §SD8).
func TestLadingBookCorpus(t *testing.T) {
	bySlug := ladingDefsBySlug(t)
	for slug, d := range bySlug {
		assert.Equal(t, EndpointDefault, d.Endpoint, slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: a lading chapter only reads", slug)
		assert.NotEmpty(t, d.Icon, slug)
		assert.False(t, d.HasUnboundSlots, "%s: every knob is prelude-bound", slug)
		assert.Contains(t, d.SQL, "SET param_m = '*';", "%s: the mount knob defaults to every visible mount", slug)
		assert.Contains(t, d.SQL, "{m:String}", "%s: the mount knob feeds the macro", slug)
	}

	assert.Equal(t, []TabSel{{ID: "table"}, {ID: "timeline"}}, bySlug["lad-ledger"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}, {ID: "detail", Zone: "side"}}, bySlug["lad-browse"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}, {ID: "detail", Zone: "side"}}, bySlug["lad-find"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["lad-grep"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}, {ID: "timeline"}}, bySlug["lad-history"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["lad-diff"].Tabs)
	assert.Equal(t, []TabSel{{ID: "treemap", Node: "files"}, {ID: "table"}}, bySlug["lad-du"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["lad-problems"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["lad-audit"].Tabs)

	// The contracts the panels resolve against.
	for _, slug := range []string{"lad-ledger", "lad-history"} {
		assert.Contains(t, bySlug[slug].SQL, " AS _tl_time", "%s: the Timeline tab needs a time slot", slug)
	}
	du := bySlug["lad-du"].SQL
	assert.Contains(t, du, " AS stack", "lad-du: the folded hierarchy arm")
	assert.Contains(t, du, " AS value", "lad-du: the folded hierarchy arm")
	assert.Contains(t, du, "files AS (", "lad-du: the treemap binds to the files CTE")
}

// TestLadingBookExpands — every chapter's body, with its own prelude, passes
// through the macro expansion under an open visibility: the knobs resolve, the
// macros rewrite, and what comes out is a statement. The integration lane runs
// them against a server; this is the half that needs none.
func TestLadingBookExpands(t *testing.T) {
	bySlug := ladingDefsBySlug(t)
	cfg := ladingsql.Config{Visibility: ladingsql.VisibleAll{}}
	for slug, d := range bySlug {
		out, err := ladingsql.Expand(cfg, d.SQL)
		require.NoErrorf(t, err, "%s must expand under an open visibility", slug)
		assert.NotContains(t, out, "{m:String}", "%s: the mount knob must be resolved at expansion", slug)
		assert.Contains(t, out, "boxer.fs", "%s: the expansion reads the store's tables", slug)
		refs := ladingsql.References(d.SQL)
		require.NotEmptyf(t, refs, "%s must reference the store", slug)
		for _, r := range refs {
			assert.Truef(t, r.All, "%s: with m = '*' every reference is a wildcard, got %s", slug, r)
		}
		_ = strings.TrimSpace(out)
	}
}
