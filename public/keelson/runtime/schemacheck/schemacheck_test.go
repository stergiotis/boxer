package schemacheck

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

func TestBoardKeepsTheLatestVerdictPerTableAndKind(t *testing.T) {
	b := NewBoard()
	b.Record(recordstore.SchemaVerdict{Kind: "k", Table: "db.t", Verdict: recordstore.SchemaVerdictMigratable})
	b.Record(recordstore.SchemaVerdict{Kind: "other", Table: "db.t", Verdict: recordstore.SchemaVerdictOk})
	b.Record(recordstore.SchemaVerdict{Kind: "k", Table: "db.t", Verdict: recordstore.SchemaVerdictOk})
	vs := b.Verdicts()
	require.Len(t, vs, 2)
	assert.Equal(t, recordstore.SchemaVerdictOk, vs[0].Verdict)
	var nilBoard *Board
	nilBoard.Record(vs...)
	assert.Empty(t, nilBoard.Verdicts())
}

func TestReportLogsOneLinePerTableNotOkAndFailsOnlyWhenStrict(t *testing.T) {
	vs := []recordstore.SchemaVerdict{
		{Table: "db.a", Verdict: recordstore.SchemaVerdictOk},
		{Table: "db.b", Verdict: recordstore.SchemaVerdictMigratable, Missing: []string{"x"}},
		{Table: "db.c", Verdict: recordstore.SchemaVerdictIncompatible, Reasons: []string{"column y is UInt16 on the table, UInt8 generated"}},
	}
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	require.NoError(t, Report(vs, log, false))
	assert.Equal(t, 2, bytes.Count(buf.Bytes(), []byte("\n")))
	assert.Contains(t, buf.String(), `"level":"warn"`)
	assert.Contains(t, buf.String(), `"level":"error"`)

	err := Report(vs, zerolog.Nop(), true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 tables are not ok")
	assert.Contains(t, err.Error(), "db.b (migratable)")
	require.NoError(t, Report(vs[:1], zerolog.Nop(), true))
}

func TestCheckOverClickhouseLocal(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	require.NoError(t, exec.Exec(ctx, `CREATE TABLE t ("a" String, "b" UInt8) ENGINE = MergeTree() ORDER BY "a"`))
	ok := recordstore.TableContract{Kind: "t", Table: "t", Columns: []recordstore.ColumnSpec{{Name: "a", Type: "String"}}}
	newer := recordstore.TableContract{Kind: "t2", Table: "t", Columns: []recordstore.ColumnSpec{{Name: "a", Type: "String"}, {Name: "c", Type: "UInt8"}}}
	absent := recordstore.TableContract{Kind: "u", Table: "u", Columns: ok.Columns}
	b := NewBoard()
	vs, err := Check(ctx, exec, b, zerolog.Nop(), false, ok, newer, absent)
	require.NoError(t, err)
	require.Len(t, vs, 3)
	assert.Equal(t, recordstore.SchemaVerdictOk, vs[0].Verdict)
	assert.Equal(t, []string{"b"}, vs[0].Extra)
	assert.Equal(t, recordstore.SchemaVerdictMigratable, vs[1].Verdict)
	assert.Equal(t, recordstore.SchemaVerdictAbsent, vs[2].Verdict)
	assert.Len(t, b.Verdicts(), 3)

	_, err = Check(ctx, exec, nil, zerolog.Nop(), true, ok, newer)
	require.Error(t, err)

	reg := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(reg, b))
	p, found := reg.Lookup(TableName)
	require.True(t, found)
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.EqualValues(t, 3, rec.NumRows())
}
