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
