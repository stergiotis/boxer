package sqlapplet

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

func catalogDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("catalog", help.MustSub(bookcatalogFS, "bookcatalog"))
	require.Empty(t, errs)
	require.Len(t, defs, 3)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, 3)
	return bySlug
}

// TestBookCatalogRegistered guards the embed + RegisterBook pair: a book whose
// directory is renamed or whose init is dropped fails silently at runtime
// (RegisterBook only logs), so the assertion belongs in a test.
func TestBookCatalogRegistered(t *testing.T) {
	entries, err := bookcatalogFS.ReadDir("bookcatalog")
	require.NoError(t, err)
	require.NotEmpty(t, entries, "bookcatalog must embed at least one page")

	var found bool
	booksMu.Lock()
	for _, b := range books {
		if b.id == "catalog" {
			found = true
			break
		}
	}
	booksMu.Unlock()
	require.True(t, found, "the catalog book must be registered by init")
}

// TestMintCatalogBook runs the book through the same minting the host does at
// mount, so a page that fails to parse or classify fails here rather than in a
// window.
func TestMintCatalogBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "catalog", fsys: help.MustSub(bookcatalogFS, "bookcatalog"),
			topics: []app.TopicT{app.TopicData}},
	})
	require.Empty(t, errs)
	assert.Equal(t, 3, minted, "overview, shapes and unmatched")
}

// TestCatalogBookCorpus is the ADR-0132 §SD6 gate over the data-catalog book.
func TestCatalogBookCorpus(t *testing.T) {
	bySlug := catalogDefsBySlug(t)
	for slug, d := range bySlug {
		// EndpointDefault, unlike every other gov book: these tables live in
		// a real ClickHouse (`boxer.tables_*`), not on the introspection
		// plane, because they describe a server rather than a process.
		assert.Equal(t, EndpointDefault, d.Endpoint, slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: a catalog chapter only reads", slug)
		assert.NotEmpty(t, d.Icon, slug)
		assert.False(t, d.HasUnboundSlots, "%s: every knob is prelude-bound", slug)
	}

	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["cat-overview"].Tabs)
	assert.Equal(t, []TabSel{{ID: "sankey"}, {ID: "table"}}, bySlug["cat-shapes"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["cat-unmatched"].Tabs)
}

// Every chapter reads the catalog tables through their real qualified names.
// The book is worthless pointed at a database that does not hold them, and a
// bare `FROM tables_catalog` would resolve against whatever the endpoint's
// default database happens to be — the failure the jsonbench book earned the
// hard way.
func TestCatalogBookNamesTheCatalogTables(t *testing.T) {
	bySlug := catalogDefsBySlug(t)
	referenced := make(map[string]struct{}, len(datacatalog.AllTables))
	for slug, d := range bySlug {
		for line := range strings.SplitSeq(d.SQL, "\n") {
			fields := strings.Fields(line)
			for i := 0; i < len(fields)-1; i++ {
				if !strings.EqualFold(fields[i], "FROM") {
					continue
				}
				ref := strings.TrimRight(fields[i+1], ",)")
				if strings.HasPrefix(ref, "(") || strings.Contains(ref, "(") {
					continue // a subquery or a table function
				}
				if strings.Contains(ref, ".") {
					assert.Truef(t, strings.HasPrefix(ref, datacatalog.DatabaseName+"."),
						"%s: `FROM %s` reads outside %s", slug, ref, datacatalog.DatabaseName)
					referenced[strings.TrimPrefix(ref, datacatalog.DatabaseName+".")] = struct{}{}
				}
			}
		}
	}
	// Three of the four tables carry a chapter; the pair matrix, the inventory
	// and the shape matches are each somebody's subject.
	for _, table := range []string{
		datacatalog.TableCatalog, datacatalog.TableCompatibility, datacatalog.TableOpaqueShapes,
	} {
		_, has := referenced[table]
		assert.Truef(t, has, "no chapter reads %s", table)
	}
}

// The Sankey chapter must declare the ADR-0159 contract by name. A missing
// column here does not fail loudly — the panel simply rejects the result and
// shows its hint, which reads like a query bug rather than a book bug.
func TestCatalogBookSankeyContract(t *testing.T) {
	sql := catalogDefsBySlug(t)["cat-shapes"].SQL
	for _, col := range []string{" AS source", " AS target", " AS value"} {
		assert.Containsf(t, sql, col, "cat-shapes: the flows contract needs%s", col)
	}
	assert.Contains(t, sql, "flows AS (", "cat-shapes: the panel demands a `flows` CTE by name")
	assert.Contains(t, sql, "nodes AS (", "cat-shapes: the optional `nodes` CTE decorates the diagram")
	// Disjoint pairs are stored but must never be drawn: a shape with no
	// members is not a shape.
	assert.Contains(t, sql, "relation != 'disjoint'")
}

// Staleness has to be visible: the catalog is a snapshot, replaced whole per
// run, so a reader who cannot see when it was taken cannot tell whether the
// answer is current.
func TestCatalogBookSurfacesTheRunStamp(t *testing.T) {
	bySlug := catalogDefsBySlug(t)
	for _, slug := range []string{"cat-overview", "cat-unmatched"} {
		assert.Containsf(t, bySlug[slug].SQL, "run_id", "%s: no run stamp", slug)
		assert.Containsf(t, bySlug[slug].SQL, "discovered_at", "%s: no discovery time", slug)
	}
}
