//go:build integration

package chstore

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowalloc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema/dml"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stergiotis/boxer/public/storage/recordstore/schemaplan"
)

// localExec is a clickhouse-local executor over a fresh directory; the
// test skips without the binary. Nothing here reaches a server.
func localExec(t *testing.T) *chexec.LocalExecutor {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse local unavailable: %v", err)
	}
	return exec
}

func setupLocal(t *testing.T, exec recordstore.ExecutorI, cfg Config, engineClause string) {
	t.Helper()
	ctx := context.Background()
	stmts, err := tableStatements(cfg, engineClause)
	require.NoError(t, err)
	for _, s := range stmts {
		require.NoError(t, exec.Exec(ctx, s), s)
	}
	require.NoError(t, rowalloc.ProvisionIn(ctx, exec, rowalloc.Layout{Database: cfg.Database}))
}

// A table created with the default clause is partitioned and stamped as
// the contract says, judged ok with no layout note, and takes a log row
// whose id is leased from the facts row stream.
func TestFactsLayout_CreateWriteRead_Local(t *testing.T) {
	exec := localExec(t)
	ctx := context.Background()
	cfg := Config{Database: "boxer", Table: "facts", RunId: "run-1"}
	setupLocal(t, exec, cfg, "")

	c, err := SchemaContract(cfg)
	require.NoError(t, err)
	v := recordstore.JudgeTable(ctx, exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	assert.False(t, v.LayoutDiffers, v.Summary())
	assert.Equal(t, recordstore.StampAgeSame, v.Age)
	assert.True(t, v.DedupWindow)
	assert.Empty(t, schemaplan.PlanTable(ctx, exec, c).Statements, "a fresh table needs nothing")

	s := &Store{cfg: cfg, allocator: memory.NewGoAllocator()}
	require.NoError(t, s.ids.init(exec, s.leaseLayout()))
	ts := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	var ids []uint64
	for _, at := range []time.Time{ts, ts.AddDate(-2, 0, 0)} {
		id, err := s.mintId()
		require.NoError(t, err)
		require.Equal(t, vocab.IdStreamFactsRow.Value(), identifier.TaggedId(id).GetTag().GetValue(), "leased, not local")
		ent := dml.NewInEntityFacts(s.allocator, 1)
		s.encodeLogEntity(ent, id, factsstore.LogRow{AppId: "a", Level: "info", Message: "m", Ts: at})
		require.NoError(t, ent.CommitEntity())
		records, err := ent.TransferRecords(nil)
		require.NoError(t, err)
		require.NoError(t, exec.InsertArrow(ctx, "boxer.facts", records))
		for _, r := range records {
			r.Release()
		}
		ids = append(ids, id)
	}
	assert.Less(t, ids[0], ids[1], "ids from one block increase")

	got := map[uint64]string{}
	for rec, err := range exec.QueryArrow(ctx, "SELECT `id:id:u64:47::0:` AS id, _partition_id AS p FROM boxer.facts ORDER BY id") {
		require.NoError(t, err)
		idCol := rec.Column(0).(*array.Uint64)
		for i := range int(rec.NumRows()) {
			got[idCol.Value(i)] = textAt(rec.Column(1), i)
		}
		rec.Release()
	}
	assert.Equal(t, map[uint64]string{ids[0]: "202611", ids[1]: "202411"}, got, "one partition per month of ts")
}

// A table an older build created keeps working: ok with a layout note,
// the plan proposes the window and leaves the stamp off, and the recipe's
// statements copy it into the build's layout with every row.
func TestFactsLayout_LegacyTableAndTheRecipe_Local(t *testing.T) {
	exec := localExec(t)
	ctx := context.Background()
	cfg := Config{Database: "boxer", Table: "facts"}
	legacy := "MergeTree() ORDER BY `" + factsschema.TsColumn + "`"
	// What an older build ran: the generated script over the legacy clause,
	// without the window and the stamp.
	old, err := tableStatements(cfg, legacy)
	require.NoError(t, err)
	create := old[len(old)-1]
	create = create[:strings.Index(create, ", "+recordstore.DedupWindowSetting)]
	require.NoError(t, exec.Exec(ctx, old[0]))
	require.NoError(t, exec.Exec(ctx, create))
	for i := range 3 {
		require.NoError(t, exec.Exec(ctx, "INSERT INTO boxer.facts (`id:id:u64:47::0:`, `ts:ts:z64:47::0:`) VALUES ("+strconv.Itoa(i+1)+", now64(9) - INTERVAL "+strconv.Itoa(i*40)+" DAY)"))
	}

	c, err := SchemaContract(cfg)
	require.NoError(t, err)
	p := schemaplan.PlanTable(ctx, exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, p.Verdict.Verdict, p.Verdict.Summary())
	require.True(t, p.Verdict.LayoutDiffers)
	assert.Contains(t, p.Verdict.Summary(), "layout differs (informational)")
	assert.Contains(t, strings.Join(p.Verdict.Notes, "\n"), factsschema.MigrationRecipe)
	require.Equal(t, []string{"ALTER TABLE `boxer`.`facts` MODIFY SETTING non_replicated_deduplication_window = 1000"}, p.Statements)

	// The recipe (doc/migration/2026-11-facts-layout.md), step by step.
	next, err := tableStatements(Config{Database: "boxer", Table: "facts_next"}, "")
	require.NoError(t, err)
	for _, s := range next {
		require.NoError(t, exec.Exec(ctx, s))
	}
	require.NoError(t, exec.Exec(ctx, "INSERT INTO boxer.facts_next SELECT * FROM boxer.facts SETTINGS max_partitions_per_insert_block = 0, insert_deduplicate = 0"))
	require.Equal(t, count(t, exec, "boxer.facts"), count(t, exec, "boxer.facts_next"))
	require.NoError(t, exec.Exec(ctx, "EXCHANGE TABLES boxer.facts AND boxer.facts_next"))
	require.NoError(t, exec.Exec(ctx, "DROP TABLE boxer.facts_next"))

	v := recordstore.JudgeTable(ctx, exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	assert.False(t, v.LayoutDiffers, v.Summary())
	assert.Equal(t, recordstore.StampAgeSame, v.Age)
	assert.Equal(t, "3", count(t, exec, "boxer.facts"))
}

func count(t *testing.T, exec recordstore.ExecutorI, table string) (n string) {
	t.Helper()
	for rec, err := range exec.QueryArrow(context.Background(), "SELECT toString(count()) AS n FROM "+table) {
		require.NoError(t, err)
		n = textAt(rec.Column(0), 0)
		rec.Release()
	}
	return
}

func textAt(col interface{ Len() int }, i int) string {
	switch a := col.(type) {
	case *array.String:
		return a.Value(i)
	case *array.LargeString:
		return a.Value(i)
	case *array.Binary:
		return string(a.Value(i))
	}
	return ""
}
