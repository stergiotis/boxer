package taskread

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/fairlead"
)

func TestPassLetsATasksReadThroughAndRefusesTheRest(t *testing.T) {
	cases := []struct {
		name    string
		sql     string
		task    string
		refused bool
	}{
		{name: "a task's select", sql: "SELECT 1", task: "t1"},
		{name: "a task's select with a format", sql: "SELECT a FROM t WHERE b > 1 FORMAT ArrowStream", task: "t1"},
		{name: "a task's with-select", sql: "WITH x AS (SELECT 1 AS a) SELECT a FROM x", task: "t1"},
		{name: "a task's explain", sql: "EXPLAIN AST SELECT 1", task: "t1"},
		{name: "a task's show", sql: "SHOW TABLES", task: "t1"},
		// The aggregate filter parses, so the read is proven rather than
		// left to a leading keyword that does not prove one.
		{name: "a task's select with an aggregate filter", sql: "SELECT quantile(0.05)(L) FILTER (WHERE L BETWEEN 177 AND 1502) AS p05 FROM t", task: "t1"},
		{name: "a task's filter hiding a nested comment", sql: "SELECT sum(x) FILTER (WHERE /* /* */ y > 0 */ 1) FROM t", task: "t1", refused: true},
		// EXISTS parses, in both spellings, so a SELECT using it is a
		// proven read; nothing but a query stands in its parentheses.
		{name: "a task's select with exists", sql: "SELECT a FROM t WHERE exists(SELECT 1 FROM u WHERE u.k = t.k) AND NOT EXISTS (SELECT 1 FROM v)", task: "t1"},
		{name: "a task's exists hiding a nested comment", sql: "SELECT exists(SELECT /* /* */ 1 FROM u */ 1)", task: "t1", refused: true},
		{name: "a task's exists around an insert", sql: "SELECT exists(INSERT INTO t SELECT 1)", task: "t1", refused: true},
		// A common table expression marked MATERIALIZED parses, where the
		// server reads the marker and nowhere else.
		{name: "a task's materialized cte", sql: "WITH a AS MATERIALIZED (SELECT k FROM t), b AS MATERIALIZED (SELECT k FROM a) SELECT count() FROM b", task: "t1"},
		{name: "a task's not materialized, which the server does not have", sql: "WITH a AS NOT MATERIALIZED (SELECT 1) SELECT * FROM a", task: "t1", refused: true},
		{name: "a task's materialized cte around an insert", sql: "WITH a AS MATERIALIZED (INSERT INTO t SELECT 1) SELECT 1", task: "t1", refused: true},
		// Several ARRAY JOIN clauses on one FROM parse.
		{name: "a task's select with two array joins", sql: "SELECT x, T, tau FROM t ARRAY JOIN [1.0, 1.5, 2.0] AS T LEFT ARRAY JOIN [0.5, 0.7] AS tau", task: "t1"},
		{name: "a task's later array join around an insert", sql: "SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN (INSERT INTO t SELECT 1) AS y", task: "t1", refused: true},
		{name: "a task's insert", sql: "INSERT INTO t VALUES (1)", task: "t1", refused: true},
		{name: "a task's insert wrapper", sql: "INSERT INTO t SELECT 1", task: "t1", refused: true},
		{name: "a task's create", sql: "CREATE TABLE t (a UInt8) ENGINE = Memory", task: "t1", refused: true},
		{name: "a task's drop", sql: "DROP TABLE t", task: "t1", refused: true},
		{name: "a task's alter", sql: "ALTER TABLE t DELETE WHERE 1", task: "t1", refused: true},
		{name: "a task's set", sql: "SET max_threads = 1", task: "t1", refused: true},
		{name: "a task's kill", sql: "KILL QUERY WHERE query_id = 'x'", task: "t1", refused: true},
		{name: "a task's grant, a keyword the lexer does not know", sql: "GRANT SELECT ON *.* TO u", task: "t1", refused: true},
		{name: "a task's text that does not parse", sql: "SELEC 1", task: "t1", refused: true},
		{name: "a task's script", sql: "SELECT 1; INSERT INTO t VALUES (1)", task: "t1", refused: true},
		{name: "an empty task text", sql: "", task: "t1", refused: true},
		// ClickHouse nests block comments and boxer's lexer does not: each of
		// these is a read to the grammar and something else to the server.
		{name: "a task's create hidden by a nested comment", sql: "/* /* */ SELECT 1 AS \"x */ CREATE TEMPORARY TABLE t (a UInt8) ENGINE = Memory --\"", task: "t1", refused: true},
		{name: "a task's expression hidden by a nested comment", sql: "SELECT 1 /* /* */ AS \"x */ , throwIf(1) AS z --\"\nFROM numbers(1)", task: "t1", refused: true},
		{name: "a task's drop behind a show and a nested comment", sql: "/* /* */ SHOW */ DROP TABLE t", task: "t1", refused: true},
		{name: "a task's select with comments both lexers read alike", sql: "SELECT '/*' AS s /* a *//* b */ FROM t -- /*\n", task: "t1"},
		{name: "a task's select with /* in a quoted alias", sql: "SELECT 1 AS \"a /* b\"", task: "t1"},
		{name: "an unmarked insert", sql: "INSERT INTO t VALUES (1)"},
		{name: "an unmarked drop", sql: "DROP TABLE t"},
		{name: "an unmarked text that does not parse", sql: "SELEC 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := Pass{}.Lead(context.Background(), fairlead.Statement{SQL: c.sql, Exit: fairlead.ExitServer, Site: "test", Task: c.task})
			if c.refused {
				require.Error(t, err)
				require.Empty(t, out)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.sql, out)
		})
	}
}

func TestARefusalSaysWhatAndWhy(t *testing.T) {
	_, err := Pass{}.Lead(context.Background(), fairlead.Statement{SQL: "DROP TABLE t", Task: "t1"})
	require.ErrorContains(t, err, "a task supplied this statement and it changes server state")
	_, err = Pass{}.Lead(context.Background(), fairlead.Statement{SQL: "SELEC 1", Task: "t1"})
	require.ErrorContains(t, err, "cannot be shown to be a read")
}

// The pass is judged where it is installed: at the fairlead, on the mark the
// context carries.
func TestInstalledPassJudgesOnlyMarkedStatements(t *testing.T) {
	remove := Install()
	defer remove()

	ctx := context.Background()
	marked := fairlead.WithTask(ctx, "t1")

	out, err := fairlead.Lead(marked, fairlead.ExitServer, "test.taskread", "SELECT 1")
	require.NoError(t, err)
	require.Equal(t, "SELECT 1", out)

	_, err = fairlead.Lead(marked, fairlead.ExitServer, "test.taskread", "TRUNCATE TABLE t")
	require.ErrorContains(t, err, "statement refused at the fairlead")

	out, err = fairlead.Lead(ctx, fairlead.ExitServer, "test.taskread", "TRUNCATE TABLE t")
	require.NoError(t, err)
	require.Equal(t, "TRUNCATE TABLE t", out)

	_, err = fairlead.Lead(fairlead.WithTask(marked, ""), fairlead.ExitLocal, "test.taskread", "TRUNCATE TABLE t")
	require.NoError(t, err, "a cleared mark is the process's own statement")
}
