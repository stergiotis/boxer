package sqlapplet

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.Equal(t, 3, minted, "overview, latency, tax")
}

// TestJsonbenchPagesQualifyTheirTable is the regression this book earned the
// hard way. The pages were first written with a bare `FROM facts`, which
// resolves against whatever database the applet's endpoint defaults to — not
// the benchmark-local results database. Hand-testing them with
// `clickhouse-client --database=jsonbench_results` hid that completely: the SQL
// was right and the deployment was wrong, and every page failed with
// UNKNOWN_TABLE the moment a real applet ran it.
//
// So: no page may name a table without qualifying it.
func TestJsonbenchPagesQualifyTheirTable(t *testing.T) {
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

		// Names bound by the page's own WITH clauses are not tables.
		ctes := make(map[string]struct{}, 8)
		for _, line := range strings.Split(sql, "\n") {
			f := strings.Fields(line)
			if len(f) >= 3 && strings.EqualFold(f[1], "AS") && strings.HasPrefix(f[2], "(") {
				ctes[strings.TrimSuffix(f[0], ",")] = struct{}{}
			}
		}

		for _, line := range strings.Split(sql, "\n") {
			f := strings.Fields(line)
			for i := 0; i < len(f)-1; i++ {
				if !strings.EqualFold(f[i], "FROM") {
					continue
				}
				ref := strings.TrimRight(f[i+1], ",)")
				// A subquery, a qualified reference, or one of this page's own
				// CTEs is fine; a bare identifier that is none of those is the
				// bug this test exists for.
				if strings.HasPrefix(ref, "(") || strings.Contains(ref, ".") {
					continue
				}
				if _, ok := ctes[ref]; ok {
					continue
				}
				assert.Failf(t, "unqualified table reference",
					"%s: `FROM %s` does not name a database; use {db:Identifier}.<table>",
					e.Name(), ref)
			}
		}

		// The database is a literal, not a parameter, because grammar1 cannot
		// parse an identifier parameter in FROM position — `{db:Identifier}`
		// fails to classify, so an applet carrying it never mounts. Value
		// parameters (`{tier:String}`) do parse and are used freely.
		assert.Contains(t, sql, "FROM jsonbench_results.facts",
			"%s: the results table must be qualified with its database", e.Name())
	}
}
