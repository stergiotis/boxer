package queryrunfacts_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// Over clickhouse-local: a run an agent task caused and a run the person
// started are written by the capture's encoder and read back by the history
// query, which tells them apart by the Delegation slots (ADR-0277 §SD7).
func TestHistoryReadsBackDelegationOverLocal(t *testing.T) {
	dir := t.TempDir()
	exec, err := chexec.NewLocalExecutor(dir, nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	setup, err := chstore.ComposeSetupSQL(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName}, "")
	require.NoError(t, err)
	stmts := append([]string{}, lwsqlsurface.Statements()...)
	for stmt := range strings.SplitSeq(setup, ";") {
		stmts = append(stmts, stmt)
	}
	for _, stmt := range stmts {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			require.NoError(t, exec.Exec(ctx, stmt))
		}
	}

	at := time.Unix(1700000000, 0).UTC()
	agent := `{"run_id":"run-7","app":"apps/play","instance":2,"lane":"main","authored_fp":"afp","task":"task-ab","task_epoch":1,"task_call":"task-ab-3"}`
	person := `{"run_id":"run-7","app":"apps/play","instance":2,"lane":"main"}`
	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), 2)
	require.NoError(t, queryrunfacts.BuildEntities(ent, []queryrunfacts.Row{
		{Type: "QueryFinish", EventUs: at.UnixMicro(), QueryId: "q-agent", Query: "SELECT 1", QueryKind: "Select", ResultRows: 1, LogComment: agent},
		{Type: "QueryFinish", EventUs: at.Add(time.Second).UnixMicro(), QueryId: "q-person", Query: "SELECT 2", QueryKind: "Select", ResultRows: 1, LogComment: person},
	}))
	records, err := ent.TransferRecords(nil)
	require.NoError(t, err)
	require.NoError(t, exec.InsertArrow(ctx, factsschema.DatabaseName+"."+factsschema.TableName, records))
	for _, r := range records {
		r.Release()
	}

	query := func(filter queryrunfacts.HistoryFilter) []queryrunfacts.HistoryRow {
		sql, cerr := queryrunfacts.ComposeHistorySqlFiltered(factsschema.DatabaseName+"."+factsschema.TableName, 10, filter)
		require.NoError(t, cerr)
		cmd, cerr := extbin.ClickHouseLocal.Command(ctx, extbin.Opts{}, "--path", dir, "--query", sql)
		require.NoError(t, cerr)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		require.NoError(t, cmd.Run(), stderr.String())
		rows, perr := queryrunfacts.ParseHistoryRows(out.Bytes())
		require.NoError(t, perr)
		return rows
	}

	rows := query(queryrunfacts.HistoryFilter{})
	require.Len(t, rows, 2)
	byId := map[string]queryrunfacts.HistoryRow{}
	for _, r := range rows {
		byId[r.QueryText] = r
	}
	a, p := byId["SELECT 1"], byId["SELECT 2"]
	require.True(t, a.Delegated())
	require.Equal(t, "task-ab", a.Task)
	require.Equal(t, "task-ab-3", a.TaskCall)
	require.Equal(t, uint64(1), a.TaskEpoch)
	require.Equal(t, uint64(2), a.Instance)
	require.Equal(t, "afp", a.AuthoredFp)
	require.Equal(t, "run-7", a.RunId)
	require.False(t, p.Delegated(), "the person's run carries no task")
	require.Equal(t, uint64(2), p.Instance)

	only := query(queryrunfacts.HistoryFilter{Task: "task-ab"})
	require.Len(t, only, 1)
	require.Equal(t, "SELECT 1", only[0].QueryText)
	require.Len(t, query(queryrunfacts.HistoryFilter{RunId: "run-7"}), 2)
	require.Empty(t, query(queryrunfacts.HistoryFilter{RunId: "other'run"}), "a quote in a filter value stays a literal")
}
