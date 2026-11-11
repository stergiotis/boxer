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
	const table = "boxer.facts"
	since := time.Unix(1700000000, 0)
	trail := factsstore.AppTrailFilter{Since: since, Limit: 10}
	return map[string]string{
		"run events":   composeRunEventsSql(table, factsstore.RunEventFilter{RunId: "r", Since: since}, 10),
		"launch stats": composeAppLaunchStatsSql(table, time.Hour, 10),
		"app runs":     composeAppRunsSql(table, trail),
		"app log tail": composeAppLogTailSql(table, trail, 3),
		"app audit":    composeAppAuditSql(table, trail),
		"log rows": composeLogRowsSql(table, factsstore.LogRowsFilter{
			Since: since, Until: since.Add(time.Hour), Levels: []string{"error"}, AppContains: "a",
			MessageContains: "m", RunId: "r", InstanceKey: 7, Limit: 10,
		}),
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

func TestPrepare_KeepsByAuthoredTextAndAppendsTheFormat(t *testing.T) {
	authored := composeAppLaunchStatsSql("boxer.facts", time.Hour, 10)
	a, err := prepare(authored, "boxer.facts", "TabSeparated")
	require.NoError(t, err)
	b, err := prepare(authored, "boxer.facts", "JSONEachRow")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(a, "\nFORMAT TabSeparated"))
	require.True(t, strings.HasSuffix(b, "\nFORMAT JSONEachRow"))
	require.Equal(t, strings.TrimSuffix(a, "TabSeparated"), strings.TrimSuffix(b, "JSONEachRow"))
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
