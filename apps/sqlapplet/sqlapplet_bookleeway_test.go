package sqlapplet

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chviews"
)

const leewayBookChapters = 5

func leewayDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("leeway", help.MustSub(bookleewayFS, "bookleeway"))
	require.Empty(t, errs)
	require.Len(t, defs, leewayBookChapters)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, leewayBookChapters)
	return bySlug
}

// TestBookLeewayRegistered guards the embed + RegisterBook pair: a book whose
// directory is renamed or whose init is dropped fails silently at runtime,
// because RegisterBook only logs.
func TestBookLeewayRegistered(t *testing.T) {
	entries, err := bookleewayFS.ReadDir("bookleeway")
	require.NoError(t, err)
	require.NotEmpty(t, entries, "bookleeway must embed at least one page")

	var found bool
	booksMu.Lock()
	for _, b := range books {
		if b.id == "leeway" {
			found = true
			break
		}
	}
	booksMu.Unlock()
	require.True(t, found, "the leeway-schema book must be registered by init")
}

// TestMintLeewayBook runs the book through the same minting the host does at
// mount, so a page that fails to parse or classify fails here rather than in a
// window.
func TestMintLeewayBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "leeway", fsys: help.MustSub(bookleewayFS, "bookleeway"),
			topics: []app.TopicT{app.TopicData}},
	})
	require.Empty(t, errs)
	assert.Equal(t, leewayBookChapters, minted)
}

// TestLeewayBookCorpus is the ADR-0132 §SD6 gate over the leeway-schema book.
func TestLeewayBookCorpus(t *testing.T) {
	bySlug := leewayDefsBySlug(t)
	for slug, d := range bySlug {
		// EndpointDefault, like the catalog book: the views describe a
		// ClickHouse server and are installed on one, so there is nothing for
		// the introspection plane to answer.
		assert.Equal(t, EndpointDefault, d.Endpoint, slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: a schema chapter only reads", slug)
		assert.NotEmpty(t, d.Icon, slug)
		assert.False(t, d.HasUnboundSlots, "%s: every knob is prelude-bound", slug)
	}

	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["lw-tables"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["lw-anatomy"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["lw-aspects"].Tabs)
	assert.Equal(t, []TabSel{
		{ID: "icicle", Node: "nodes"}, {ID: "treemap", Node: "nodes"}, {ID: "table"},
	}, bySlug["lw-bytes"].Tabs)
	assert.Equal(t, []TabSel{
		{ID: "graphview"}, {ID: "network"}, {ID: "table"},
	}, bySlug["lw-affinity"].Tabs)
}

// qualifiedSources returns every `FROM <db>.<table>` a chapter names, with the
// backticks stripped. A CTE reference carries no dot and is skipped, as is a
// subquery or a table function.
func qualifiedSources(sql string) (refs []string) {
	for line := range strings.SplitSeq(sql, "\n") {
		fields := strings.Fields(line)
		for i := 0; i < len(fields)-1; i++ {
			if !strings.EqualFold(fields[i], "FROM") {
				continue
			}
			ref := strings.TrimRight(fields[i+1], ",)")
			if strings.Contains(ref, "(") || !strings.Contains(ref, ".") {
				continue
			}
			refs = append(refs, strings.ReplaceAll(ref, "`", ""))
		}
	}
	return
}

// Every chapter reads the views, not the system tables they are built from.
// A chapter that re-split a physical name out of system.columns by hand would
// be the second implementation ADR-0170 §Q1 rejected and ADR-0226 §SD1 is
// written to avoid — and it would be invisible, because it would work.
func TestLeewayBookReadsTheViews(t *testing.T) {
	target := chviews.TargetDatabase("")
	seen := make(map[string]struct{}, len(chviews.AllViewNames()))
	for slug, d := range leewayDefsBySlug(t) {
		for _, ref := range qualifiedSources(d.SQL) {
			assert.Truef(t, strings.HasPrefix(ref, target.Name()+"."),
				"%s: `FROM %s` reads outside %s", slug, ref, target.Name())
			seen[strings.TrimPrefix(ref, target.Name()+".")] = struct{}{}
		}
	}
	for _, name := range chviews.AllViewNames() {
		_, has := seen[name]
		assert.Truef(t, has, "no chapter reads %s", target.Qualified(name))
	}
}

// `columns` has to be backticked. The client-side parser reads a bare one as
// the start of a COLUMNS('…') matcher — as a table name and as a result alias
// alike — and a statement it cannot parse ships verbatim with every
// pre-execute pass skipped. That failure is silent: the chapter still returns
// rows, having quietly lost handle resolution and the rest (ADR-0226).
func TestLeewayBookBackticksTheColumnsView(t *testing.T) {
	bare := chviews.TargetDatabase("").Name() + "." + chviews.ViewColumns
	quoted := chviews.TargetDatabase("").Name() + ".`" + chviews.ViewColumns + "`"
	for slug, d := range leewayDefsBySlug(t) {
		for line := range strings.SplitSeq(d.SQL, "\n") {
			if !strings.Contains(line, bare) {
				continue
			}
			assert.Containsf(t, line, quoted,
				"%s: `%s` must be backticked or the pre-execute passes are skipped:\n%s", slug, bare, line)
		}
	}
}

// The book must not restate the verdict it is not entitled to (ADR-0226 §SD2).
// name_shape counts column layouts; whether a table IS leeway is the restoring
// classifier's answer, and bookcatalog is where that is read.
func TestLeewayBookClaimsNoVerdict(t *testing.T) {
	for slug, d := range leewayDefsBySlug(t) {
		assert.NotContainsf(t, d.SQL, "is_leeway", "%s: the views report no such column", slug)
	}
}

// The hierarchy contract has to be declared by name, and the panels bound to
// the CTE carrying it: a missing column does not fail loudly, the panel simply
// rejects the result and shows a hint that reads like a query bug.
func TestLeewayBookHierarchyContract(t *testing.T) {
	sql := leewayDefsBySlug(t)["lw-bytes"].SQL
	assert.Contains(t, sql, "nodes AS (", "lw-bytes: the tabs bind a `nodes` CTE by name")
	for _, col := range []string{" AS stack", " AS value", " AS unit", " AS color"} {
		assert.Containsf(t, sql, col, "lw-bytes: the folded hierarchy contract needs%s", col)
	}
	// A ratio's range is a property of the measure, not of this result, so the
	// scale is declared rather than surveyed — otherwise the worst column
	// present paints as the top of the ramp whatever it actually compresses to.
	for _, col := range []string{"color_min", "color_max", "color_unit"} {
		assert.Containsf(t, sql, col, "lw-bytes: the colour scale must be declared (%s)", col)
	}
}

// The graph contract, likewise by name. Both graph tabs read the same two
// CTEs (ADR-0227 §SD1), so one declaration serves them.
func TestLeewayBookGraphContract(t *testing.T) {
	sql := leewayDefsBySlug(t)["lw-affinity"].SQL
	assert.Contains(t, sql, "edges AS (", "lw-affinity: the panels demand an `edges` CTE by name")
	assert.Contains(t, sql, "vertices AS (", "lw-affinity: the optional `vertices` CTE decorates the graph")
	for _, col := range []string{" AS source", " AS target"} {
		assert.Containsf(t, sql, col, "lw-affinity: the edge contract needs%s", col)
	}
	// Each unordered pair once: a cross join offers both orders and the
	// self-pair, and an undirected graph wants neither.
	assert.Contains(t, sql, "(a.database, a.table) < (b.database, b.table)")
}
