package sqlapplet

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// chsysDefsBySlug parses the embedded ClickHouse-introspection book and
// indexes it by slug.
func chsysDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("chsys", help.MustSub(bookchsysFS, "bookchsys"))
	require.Empty(t, errs)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, 6)
	return bySlug
}

// TestChsysBookCorpus is the ADR-0132 §SD6 gate over the book, plus the
// panel contracts each page is written against: a page whose result stopped
// matching its panel's column names would mount and draw nothing.
func TestChsysBookCorpus(t *testing.T) {
	bySlug := chsysDefsBySlug(t)
	for slug, d := range bySlug {
		assert.Equal(t, EndpointDefault, d.Endpoint, "%s reads the server's system tables", slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: a system-table read auto-runs", slug)
		assert.False(t, d.HasUnboundSlots, "%s: every knob is prelude-bound", slug)
		assert.NotEmpty(t, d.Icon, slug)
	}

	// Folded hierarchy contract (ADR-0160, ADR-0166): a list-typed `stack`
	// and each path's own `value`.
	for _, slug := range []string{"ch-flame", "ch-storage"} {
		assert.Contains(t, bySlug[slug].SQL, "AS stack", slug)
		assert.Contains(t, bySlug[slug].SQL, "AS value", slug)
	}
	// Symbolisation needs the setting per query, in a SETTINGS clause: a
	// top-level SET of it would classify the page mutating and stop auto-run.
	assert.Contains(t, bySlug["ch-flame"].SQL, "SETTINGS allow_introspection_functions = 1")

	// Graph contract (ADR-0129): the convention-named CTEs.
	for _, slug := range []string{"ch-pipeline", "ch-deps"} {
		assert.Contains(t, bySlug[slug].SQL, "edges AS (", slug)
		assert.Contains(t, bySlug[slug].SQL, "vertices AS (", slug)
		assert.Contains(t, bySlug[slug].SQL, "AS source", slug)
		assert.Contains(t, bySlug[slug].SQL, "AS target", slug)
	}
	// Sankey contract (ADR-0159): `flows` with source/target/value.
	assert.Contains(t, bySlug["ch-readflow"].SQL, "flows AS (")
	assert.Contains(t, bySlug["ch-readflow"].SQL, "AS value")
	// Timeline interval contract.
	assert.Contains(t, bySlug["ch-merges"].SQL, "AS _tl_time,")
	assert.Contains(t, bySlug["ch-merges"].SQL, "AS _tl_time_end,")
	assert.Contains(t, bySlug["ch-merges"].SQL, "AS _tl_lane,")

	assert.Equal(t, []TabSel{{ID: "icicle"}, {ID: "treemap"}, {ID: "table"}}, bySlug["ch-flame"].Tabs)
	assert.Equal(t, []TabSel{{ID: "network"}, {ID: "graphview"}, {ID: "table"}}, bySlug["ch-deps"].Tabs)
}

// TestBookChsysRegistered guards the embed + RegisterBook pair: RegisterBook
// only logs on failure, so a dropped init fails silently at runtime.
func TestBookChsysRegistered(t *testing.T) {
	var found bool
	booksMu.Lock()
	for _, b := range books {
		if b.id == "chsys" {
			found = true
			break
		}
	}
	booksMu.Unlock()
	require.True(t, found, "the chsys book must be registered by init")
}

// TestMintChsysBook runs the book through the host's minting, topics
// included: the book defaults to observability, and the two pages about what
// the server holds override it to data.
func TestMintChsysBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "chsys", fsys: help.MustSub(bookchsysFS, "bookchsys"), topics: []app.TopicT{app.TopicObservability}},
	})
	require.Empty(t, errs)
	assert.Equal(t, 6, minted)
	for slug, want := range map[string][]app.TopicT{
		"ch-flame":   {app.TopicObservability},
		"ch-deps":    {app.TopicData},
		"ch-storage": {app.TopicData},
	} {
		m, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + slug))
		require.True(t, ok, slug)
		assert.Equal(t, want, m.Topics, slug)
	}
}
