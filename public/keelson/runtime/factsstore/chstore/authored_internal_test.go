package chstore

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/factsread"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// authoredReads is every read this store composes, with every optional
// filter set, so a clause that only appears under a filter is expanded too.
func authoredReads() (reads map[string]string) {
	return authoredReadsAt(time.Unix(1700000000, 0), "r")
}

func authoredReadsAt(since time.Time, run string) (reads map[string]string) {
	const table = "boxer.facts"
	b := binds{}
	trail := factsstore.AppTrailFilter{Since: since, Limit: 10}
	return map[string]string{
		"run events":   composeRunEventsSql(table, factsstore.RunEventFilter{RunId: run, Since: since}, 10, b),
		"launch stats": composeAppLaunchStatsSql(table, time.Hour, 10),
		"app runs":     composeAppRunsSql(table, trail, b),
		"app log tail": composeAppLogTailSql(table, trail, 3, b),
		"app audit":    composeAppAuditSql(table, trail, b),
		"log rows": composeLogRowsSql(table, factsstore.LogRowsFilter{
			Since: since, Until: since.Add(time.Hour), Levels: []string{"error"}, AppContains: "a",
			MessageContains: "m", RunId: run, InstanceKey: 7, Limit: 10,
		}, b),
	}
}

// TestAuthoredReads_ExpandWithNoServer: each read names sections,
// memberships and handles the generated schema knows, and names no physical
// lane itself (ADR-0367 §SD5, update of 2026-11-11).
func TestAuthoredReads_ExpandWithNoServer(t *testing.T) {
	for name, authored := range authoredReads() {
		require.NotContains(t, authored, "`tv:", "%s spells a physical column", name)
		require.NotContains(t, authored, "arrayCumSum", "%s computes a lane position", name)
		out, err := factsread.Prepare(authored, "boxer.facts")
		require.NoError(t, err, name)
		require.NotContains(t, out, "LW_GET", "%s: an extraction call was left unexpanded", name)
		require.NotContains(t, out, "LW_SEL", "%s: a selector was left unexpanded", name)
		require.Equal(t, 0, strings.Count(out, "FORMAT"), name)
	}
}

// TestAuthoredReads_TextDoesNotMoveWithTheFilterValues: a polled read with
// a moving look-back is one statement, so its expansion is kept, and a value
// a person typed is bound rather than written into the SQL.
func TestAuthoredReads_TextDoesNotMoveWithTheFilterValues(t *testing.T) {
	a := authoredReadsAt(time.Unix(1700000000, 0), "r")
	b := authoredReadsAt(time.Unix(1800000000, 5), "it's \\ another")
	require.Equal(t, a, b)
	for name, authored := range a {
		out, err := factsread.Prepare(authored, "boxer.facts")
		require.NoError(t, err, name)
		if strings.Contains(authored, "{since:Int64}") {
			require.Contains(t, out, "{since:Int64}", "%s: the placeholder survives the expansion", name)
		}
	}
}

func TestBinds_EscapeWhatTheServerUnescapes(t *testing.T) {
	b := binds{}
	require.Equal(t, "{q:String}", b.str("q", "a\tb\\c\nd'e"))
	require.Equal(t, `a\tb\\c\nd'e`, b["q"])
	require.Equal(t, "{n:Int64}", b.i64("n", -3))
	require.Equal(t, "-3", b["n"])
}

// TestPrepare_CarriesNoFormat: the executor asks for Arrow, so a statement
// that names a format of its own would be sent with two.
func TestPrepare_CarriesNoFormat(t *testing.T) {
	authored := composeAppLaunchStatsSql("boxer.facts", time.Hour, 10)
	a, err := prepare(authored, "boxer.facts")
	require.NoError(t, err)
	b, err := prepare(authored, "boxer.facts")
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.NotContains(t, a, "FORMAT")
}

// TestSetupStatements_CarryTheTableAndTheReadSurface: a caller that runs
// them through its own client ends with a server the reads work on.
func TestSetupStatements_CarryTheTableAndTheReadSurface(t *testing.T) {
	stmts, err := SetupStatements(Config{Database: "boxer", Table: "facts"}, "")
	require.NoError(t, err)
	table, err := tableStatements(Config{Database: "boxer", Table: "facts"}, "")
	require.NoError(t, err)
	require.Equal(t, table, stmts[:len(table)], "the table comes first")
	surface := lwsqlsurface.AllStatements("")
	require.Equal(t, surface, stmts[len(table):])
	require.Contains(t, stmts, lwsqlsurface.MarkerStatement())
	all := strings.Join(stmts, "\n")
	for name := range authoredReads() {
		out, err := factsread.Prepare(authoredReads()[name], "boxer.facts")
		require.NoError(t, err)
		for _, fn := range []string{"LW_VALUE_BY_TAG_EQUAL", "LW_LIST_BY_TAG_EQUAL", "LW_RAGGED_PARENT_IDS", "LW_CO_GATHER", "LW_RAGGED_ELEM"} {
			if strings.Contains(out, fn+"(") {
				require.Contains(t, all, fn, "%s calls %s, which setup does not create", name, fn)
			}
		}
	}
}

// BenchmarkPrepareLogRows is the expansion of the largest read, uncached:
// what a read pays when its authored text has not been seen.
func BenchmarkPrepareLogRows(b *testing.B) {
	authored := authoredReads()["log rows"]
	for b.Loop() {
		if _, err := factsread.Prepare(authored, "boxer.facts"); err != nil {
			b.Fatal(err)
		}
	}
}
