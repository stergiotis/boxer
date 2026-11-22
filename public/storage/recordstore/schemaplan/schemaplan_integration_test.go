//go:build integration

package schemaplan

import (
	"context"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// localExec is a clickhouse-local directory of the test's own.
func localExec(t *testing.T) recordstore.ExecutorI {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	return exec
}

// serverExec is the server the CLICKHOUSE_* variables name, with a scratch
// database of the test's own, dropped at its end.
func serverExec(t *testing.T, db string) recordstore.ExecutorI {
	t.Helper()
	ctx := context.Background()
	cfg := chclient.ConfigFromEnv()
	cli := chclient.New(cfg, nil)
	if cli.Ping(ctx) != nil {
		t.Skipf("no live ClickHouse at %s", env.RedactURL(cfg.URL))
	}
	require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+db+" SYNC"))
	t.Cleanup(func() { _ = cli.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" SYNC") })
	exec, err := storeexec.New(cli, memory.NewGoAllocator())
	require.NoError(t, err)
	return exec
}

// scalar runs sql, whose one column is a String, and returns its first row.
func scalar(t *testing.T, exec recordstore.ExecutorI, sql string) (out string) {
	t.Helper()
	for rec, err := range exec.QueryArrow(context.Background(), sql+" SETTINGS output_format_arrow_string_as_string=1") {
		require.NoError(t, err)
		if rec.NumRows() > 0 && out == "" {
			out = rec.Column(0).(*array.String).Value(0)
		}
		rec.Release()
	}
	return
}

// A table an older binary created is brought to the current columns by
// the derived statements, on clickhouse-local: the verdict turns ok, the
// window is set and the stamp is the build's.
func TestApplyBringsAnOlderTableInLine(t *testing.T) {
	applyBringsAnOlderTableInLine(t, localExec(t), "boxer.persiststate")
}

// The same on a server (ADR-0387 verification plan), with a row written
// before the apply that is still there after it.
func TestApplyBringsAnOlderTableInLine_Server(t *testing.T) {
	const db = "schemaplan_it_older"
	applyBringsAnOlderTableInLine(t, serverExec(t, db), db+".persiststate")
}

func applyBringsAnOlderTableInLine(t *testing.T, exec recordstore.ExecutorI, table string) {
	ctx := context.Background()
	beforeDDL, before := fixture(t, "persiststate_before.sql")
	stmts, err := recordstore.ProvisioningStatements(beforeDDL, "boxer.persiststate", override(table))
	require.NoError(t, err)
	for _, s := range stmts {
		require.NoError(t, exec.Exec(ctx, s))
	}
	require.NoError(t, exec.Exec(ctx, "INSERT INTO "+table+" (`"+before[0].Name+"`) VALUES ('kept')"))
	_, after := fixture(t, "persiststate_after.sql")
	c := contract(table, after)

	p := PlanTable(ctx, exec, c)
	require.False(t, p.IsRefused(), p.Refused)
	require.NotEmpty(t, p.Statements)
	n, err := Apply(ctx, exec, p)
	require.NoError(t, err)
	require.Equal(t, len(p.Statements), n)

	v := recordstore.JudgeTable(ctx, exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	require.Equal(t, recordstore.StampAgeSame, v.Age)
	require.True(t, v.DedupWindow)
	require.Empty(t, PlanTable(ctx, exec, c).Statements, "a second plan has nothing to do")
	require.Equal(t, "kept", scalar(t, exec, "SELECT `"+before[0].Name+"` FROM "+table), "the row written before the apply is still there")
}

// override is table as ProvisioningStatements takes it: empty for the name
// the fixture bakes in.
func override(table string) string {
	if table == "boxer.persiststate" {
		return ""
	}
	return table
}

// A column left under an older aspect spelling is renamed and recodec'd.
func TestApplyRenamesAnAspectOnlyVariant(t *testing.T) {
	applyRenamesAnAspectOnlyVariant(t, localExec(t), "")
}

// The same on a server, where the rename is a data-modifying alter: the
// value written under the old name is read under the new one, and the
// column carries the generated codec.
func TestApplyRenamesAnAspectOnlyVariant_Server(t *testing.T) {
	applyRenamesAnAspectOnlyVariant(t, serverExec(t, "schemaplan_it_rename"), "schemaplan_it_rename")
}

func applyRenamesAnAspectOnlyVariant(t *testing.T, exec recordstore.ExecutorI, db string) {
	ctx := context.Background()
	v2DDL, v2 := fixture(t, "device_aspects_v2.sql")
	_, v1 := fixture(t, "device_aspects_v1.sql")
	const at = 4
	oldDDL := strings.Replace(v2DDL, `"`+v2[at].Name+`" Array(UInt64) CODEC(Delta,ZSTD(3))`, `"`+v1[at].Name+`" Array(UInt64) CODEC(ZSTD(3))`, 1)
	require.NotEqual(t, v2DDL, oldDDL)
	table := "device"
	if db != "" {
		table = db + ".device"
		require.NoError(t, exec.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+db))
		oldDDL = strings.Replace(oldDDL, "CREATE TABLE IF NOT EXISTS device (", "CREATE TABLE IF NOT EXISTS "+table+" (", 1)
	}
	require.NoError(t, exec.Exec(ctx, oldDDL))
	require.NoError(t, exec.Exec(ctx, "INSERT INTO "+table+" (`"+v2[0].Name+"`, `"+v1[at].Name+"`) VALUES (1, [1,2,3])"))
	c := contract(table, v2)
	p := PlanTable(ctx, exec, c)
	require.False(t, p.IsRefused(), p.Refused)
	_, err := Apply(ctx, exec, p)
	require.NoError(t, err)
	v := recordstore.JudgeTable(ctx, exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	require.Empty(t, v.Extra)
	require.Equal(t, "[1,2,3]", scalar(t, exec, "SELECT toString(`"+v2[at].Name+"`) FROM "+table), "the value is read under the new name")
	dbExpr := "currentDatabase()"
	if db != "" {
		dbExpr = "'" + db + "'"
	}
	require.Contains(t, scalar(t, exec,
		"SELECT compression_codec FROM system.columns WHERE database = "+dbExpr+" AND table = 'device' AND name = '"+v2[at].Name+"'"),
		"Delta", "the column carries the generated codec, Delta in front of ZSTD")
}
