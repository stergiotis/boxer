package sqlapplet

import (
	"regexp"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cteBinding matches a WITH-clause binding: an identifier introducing a
// parenthesised subquery.
var cteBinding = regexp.MustCompile(`(?i)(\w+)\s+AS\s*\(`)

// TestBookJsonbenchRegistered guards the embed + RegisterBook pair: a book
// whose directory is renamed or whose init is dropped fails silently at
// runtime (RegisterBook only logs), so the assertion belongs in a test.
func TestBookJsonbenchRegistered(t *testing.T) {
	entries, err := bookjsonbenchFS.ReadDir("bookjsonbench")
	require.NoError(t, err)
	require.NotEmpty(t, entries, "bookjsonbench must embed at least one page")

	var found bool
	booksMu.Lock()
	for _, b := range books {
		if b.id == "jsonbench" {
			found = true
			break
		}
	}
	booksMu.Unlock()
	require.True(t, found, "the jsonbench book must be registered by init")
}

// TestMintJsonbenchBook runs the book through the same minting the host does at
// mount, so a page that fails to parse or classify fails here rather than in a
// window.
func TestMintJsonbenchBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "jsonbench", fsys: help.MustSub(bookjsonbenchFS, "bookjsonbench"),
			topics: []app.TopicT{app.TopicObservability}},
	})
	require.Empty(t, errs)
	assert.Equal(t, 2, minted, "latency and sizes")
}

// TestJsonbenchPagesAreSelfContained is the regression this book earned the
// hard way, twice.
//
// The pages first read a benchmark-local results table through a bare
// `FROM facts`, which resolves against whatever database the applet's endpoint
// defaults to. Hand-testing them with
// `clickhouse-client --database=jsonbench_results` hid it completely: the SQL
// was right and the deployment was wrong, and every page failed UNKNOWN_TABLE
// the moment a real applet ran it. Qualifying the reference fixed that but
// left a worse property — the book only worked where someone had run a load
// step by hand.
//
// So the pages now carry their numbers, and may not reference a stored table
// at all: every `FROM` must name a CTE, a subquery, or the `values` table
// function.
func TestJsonbenchPagesAreSelfContained(t *testing.T) {
	entries, err := bookjsonbenchFS.ReadDir("bookjsonbench")
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, e := range entries {
		body, err := bookjsonbenchFS.ReadFile("bookjsonbench/" + e.Name())
		require.NoError(t, err)
		src := string(body)

		// Only the fenced SQL is SQL; the prose around it says "from the"
		// often enough to matter.
		_, after, ok := strings.Cut(src, "```sql\n")
		require.Truef(t, ok, "%s: no ```sql block", e.Name())
		sql, ok := strings.CutSuffix(strings.TrimRight(after, "\n"), "```")
		if !ok {
			sql = after
		}

		// Names bound by the page's own WITH clauses are not tables. The
		// binding occurs as `<name> AS (`, whether it opens the WITH list
		// ("WITH s AS (") or continues it ("), r AS (").
		ctes := make(map[string]struct{}, 8)
		for _, m := range cteBinding.FindAllStringSubmatch(sql, -1) {
			ctes[m[1]] = struct{}{}
		}

		for _, line := range strings.Split(sql, "\n") {
			f := strings.Fields(line)
			for i := 0; i < len(f)-1; i++ {
				if !strings.EqualFold(f[i], "FROM") {
					continue
				}
				ref := strings.TrimRight(f[i+1], ",)")
				// A subquery, one of this page's own CTEs, or the values table
				// function are all self-contained; anything else is a stored
				// table this book must not depend on.
				if strings.HasPrefix(ref, "(") || strings.HasPrefix(ref, "values(") {
					continue
				}
				if _, ok := ctes[ref]; ok {
					continue
				}
				assert.Failf(t, "page depends on a stored table",
					"%s: `FROM %s` reads something that must be loaded first; "+
						"this book's pages carry their numbers", e.Name(), ref)
			}
		}

		assert.Contains(t, sql, "FROM values(",
			"%s: the page's numbers must ride in the page", e.Name())
	}
}
