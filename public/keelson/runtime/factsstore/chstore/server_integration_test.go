//go:build integration

package chstore

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowalloc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema/dml"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/schemaplan"
)

// The tests in this file run the facts table's design (ADR-0397) on a
// ClickHouse server, which clickhouse-local does not stand in for: parts
// and partitions as a server keeps them, EXCHANGE TABLES in an Atomic
// database, the deduplication window across requests, and leases raced by
// separate connections and separate processes.
//
// Each test owns a scratch database it drops when it ends and names no
// other; none of them names boxer.facts. Setup installs leeway's SQL read
// surface, which is server-wide, as any host's boot does.

// idColumn is the physical name of the facts envelope's row id.
const idColumn = "id:id:u64:47::0:"

// serverScratch is a config over the server the CLICKHOUSE_* variables
// name, bound to a scratch database that is dropped before the test and
// after it. The test skips when no server answers.
func serverScratch(t *testing.T, name string) (cfg Config) {
	t.Helper()
	cfg = ConfigWithEnvDefaults(Config{Database: "boxer_it_" + name, Table: "facts", RunId: "run-" + name})
	probe, err := New(cfg)
	require.NoError(t, err)
	if err := probe.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	drop := func() {
		_ = probe.cli.Exec(context.Background(), "DROP DATABASE IF EXISTS "+cfg.Database+" SYNC")
	}
	drop()
	t.Cleanup(drop)
	return
}

// serverStore is a store over cfg, as one process would build it.
func serverStore(t *testing.T, cfg Config) (s *Store) {
	t.Helper()
	s, err := New(cfg)
	require.NoError(t, err)
	return
}

// scalar runs sql on the server and returns its tab-separated answer,
// trimmed.
func scalar(t *testing.T, s *Store, sql string) string {
	t.Helper()
	body, err := s.cli.Query(context.Background(), sql+" FORMAT TabSeparated")
	require.NoError(t, err, sql)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	return strings.TrimSpace(string(raw))
}

// storedIds reads every row id of table.
func storedIds(t *testing.T, s *Store, table string) (ids []uint64) {
	t.Helper()
	for rec, err := range s.exec.QueryArrow(context.Background(), "SELECT `"+idColumn+"` AS id FROM "+table+" ORDER BY id") {
		require.NoError(t, err)
		ids = append(ids, rec.Column(0).(*array.Uint64).Uint64Values()...)
		rec.Release()
	}
	return
}

func tagOf(id uint64) identifier.TagValue {
	return identifier.TaggedId(id).GetTag().GetValue()
}

// A table set up on a server has the layout, the window and the stamp the
// contract names; rows written through the store land one partition per
// month under leased ids, and read back through the store's readers.
func TestServer_FreshTable_LayoutWindowStampAndPartitions(t *testing.T) {
	cfg := serverScratch(t, "fresh")
	s := serverStore(t, cfg)
	ctx := context.Background()
	require.NoError(t, s.SetupTable(ctx, ""))
	require.NoError(t, s.SetupTable(ctx, ""), "setup is idempotent")

	where := " FROM system.tables WHERE database = '" + cfg.Database + "' AND name = 'facts'"
	assert.Equal(t, "MergeTree", scalar(t, s, "SELECT engine"+where))
	assert.Equal(t, "toYYYYMM(`"+factsschema.TsColumn+"`)", scalar(t, s, "SELECT partition_key"+where))
	assert.Equal(t, "`"+factsschema.TsColumn+"`", scalar(t, s, "SELECT sorting_key"+where))
	assert.Equal(t, "1", scalar(t, s, "SELECT create_table_query LIKE '%"+recordstore.DedupWindowSetting+" = "+strconv.Itoa(recordstore.DedupWindowBlocks)+"%'"+where))
	assert.Equal(t, "Atomic", scalar(t, s, "SELECT engine FROM system.databases WHERE name = '"+cfg.Database+"'"),
		"the recipe's EXCHANGE TABLES needs the default database engine")

	c, err := SchemaContract(cfg)
	require.NoError(t, err)
	assert.Equal(t, c.Stamp, scalar(t, s, "SELECT comment"+where), "the stamp is the table's comment")
	v := recordstore.JudgeTable(ctx, s.exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	assert.False(t, v.LayoutDiffers, v.Summary())
	assert.Equal(t, recordstore.StampAgeSame, v.Age)
	assert.True(t, v.DedupWindow)
	assert.Empty(t, schemaplan.PlanTable(ctx, s.exec, c).Statements, "a fresh table needs nothing")

	base := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	rows := []factsstore.LogRow{
		{AppId: "a", Level: "info", Message: "now", Ts: base},
		{AppId: "a", Level: "warn", Message: "last month", Ts: base.AddDate(0, -1, 0)},
		{AppId: "b", Level: "info", Message: "two years ago", Ts: base.AddDate(-2, 0, 0)},
	}
	ids, err := s.WriteLogs(ctx, rows)
	require.NoError(t, err)
	require.Len(t, ids, 3)
	for _, id := range ids {
		assert.Equal(t, vocab.IdStreamFactsRow.Value(), tagOf(id), "leased, not process-local")
	}
	assert.Equal(t, "202411:1,202610:1,202611:1", scalar(t, s,
		"SELECT arrayStringConcat(arraySort(groupArray(concat(partition_id, ':', toString(r)))), ',') FROM (SELECT partition_id, sum(rows) AS r FROM system.parts WHERE active AND database = '"+cfg.Database+"' AND table = 'facts' GROUP BY partition_id)"),
		"one partition per month of ts")
	assert.Equal(t, ids, storedIds(t, s, cfg.Database+".facts"))

	got, err := s.LogRows(ctx, factsstore.LogRowsFilter{})
	require.NoError(t, err)
	require.Len(t, got, 3, "the rows read back through the store's reader and the read surface")

	next, err := strconv.ParseUint(scalar(t, s, "SELECT max(next) FROM "+cfg.Database+"."+rowalloc.TableName+" WHERE tag = "+strconv.FormatUint(uint64(vocab.IdStreamFactsRow.Value()), 10)), 10, 64)
	require.NoError(t, err)
	assert.Equal(t, uint64(1+rowalloc.DefaultBlockSize), next, "one block was leased from the lease table beside the facts table")
}

// A table an older build created keeps working on a server, and the
// migration recipe (doc/migration/2026-11-facts-layout.md) moves it into
// the build's layout with every row, one partition per month.
func TestServer_LegacyTableAndTheRecipe(t *testing.T) {
	cfg := serverScratch(t, "legacy")
	s := serverStore(t, cfg)
	ctx := context.Background()
	table := cfg.Database + ".facts"
	next := cfg.Database + ".facts_next"

	// What an older build ran: the generated script over the legacy clause,
	// without the window and the stamp.
	old, err := tableStatements(cfg, "MergeTree() ORDER BY `"+factsschema.TsColumn+"`")
	require.NoError(t, err)
	create := old[len(old)-1]
	create = create[:strings.Index(create, ", "+recordstore.DedupWindowSetting)]
	require.NoError(t, s.exec.Exec(ctx, old[0]))
	require.NoError(t, s.exec.Exec(ctx, create))
	// The upgraded build starts: setup leaves the table as it is and
	// provisions the lease table beside it.
	require.NoError(t, s.SetupTable(ctx, ""))
	assert.Equal(t, "", scalar(t, s, "SELECT partition_key FROM system.tables WHERE database = '"+cfg.Database+"' AND name = 'facts'"),
		"CREATE TABLE IF NOT EXISTS changes no existing table")
	// 5000 rows over 150 days: counter ids, as an older build minted them.
	require.NoError(t, s.exec.Exec(ctx, "INSERT INTO "+table+" (`"+idColumn+"`, `"+factsschema.TsColumn+"`) SELECT number + 1, toDateTime64('2026-11-20 00:00:00', 9, 'UTC') - toIntervalMinute(number * 43) FROM numbers(5000)"))
	months := scalar(t, s, "SELECT uniqExact(toYYYYMM(`"+factsschema.TsColumn+"`)) FROM "+table)
	require.Equal(t, "6", months, "June to November")

	c, err := SchemaContract(cfg)
	require.NoError(t, err)
	p := schemaplan.PlanTable(ctx, s.exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, p.Verdict.Verdict, p.Verdict.Summary())
	require.True(t, p.Verdict.LayoutDiffers)
	assert.Contains(t, strings.Join(p.Verdict.Notes, "\n"), factsschema.MigrationRecipe)
	require.Equal(t, []string{"ALTER TABLE `" + cfg.Database + "`.`facts` MODIFY SETTING " + recordstore.DedupWindowSetting + " = " + strconv.Itoa(recordstore.DedupWindowBlocks)}, p.Statements)
	applied, err := schemaplan.Apply(ctx, s.exec, p)
	require.NoError(t, err)
	assert.Equal(t, 1, applied)
	after := schemaplan.PlanTable(ctx, s.exec, c)
	assert.Empty(t, after.Statements, "the window is set; the layout note stays, and no stamp is offered while it does")
	assert.True(t, after.Verdict.LayoutDiffers)

	// The store writes to the legacy table as it is.
	leasedId, err := s.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "into the old layout", Ts: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	assert.Equal(t, vocab.IdStreamFactsRow.Value(), tagOf(leasedId), "leased ids beside the old counter ids")
	assert.Greater(t, leasedId, uint64(5000), "a leased id's tag sits in bits no counter reached")

	// The recipe, step by step.
	nextStmts, err := tableStatements(Config{Database: cfg.Database, Table: "facts_next"}, "")
	require.NoError(t, err)
	for _, stmt := range nextStmts {
		require.NoError(t, s.exec.Exec(ctx, stmt))
	}
	require.NoError(t, s.exec.Exec(ctx, "INSERT INTO "+next+" SELECT * FROM "+table+" SETTINGS max_partitions_per_insert_block = 0, insert_deduplicate = 0"))
	digest := func(tbl string) string {
		return scalar(t, s, "SELECT count(), sum(cityHash64(*)) FROM "+tbl)
	}
	require.Equal(t, "5001", scalar(t, s, "SELECT count() FROM "+next))
	require.Equal(t, digest(table), digest(next), "every row, every column")
	require.NoError(t, s.exec.Exec(ctx, "EXCHANGE TABLES "+table+" AND "+next))
	require.NoError(t, s.exec.Exec(ctx, "DROP TABLE "+next+" SYNC"))

	v := recordstore.JudgeTable(ctx, s.exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	assert.False(t, v.LayoutDiffers, v.Summary())
	assert.Equal(t, recordstore.StampAgeSame, v.Age)
	assert.Empty(t, schemaplan.PlanTable(ctx, s.exec, c).Statements)
	assert.Equal(t, months, scalar(t, s, "SELECT uniqExact(partition_id) FROM system.parts WHERE active AND database = '"+cfg.Database+"' AND table = 'facts'"),
		"one partition per month that holds rows")

	// Dropping a month is the retention the layout is for.
	require.NoError(t, s.exec.Exec(ctx, "ALTER TABLE "+table+" DROP PARTITION ID '202611'"))
	assert.Equal(t, "0", scalar(t, s, "SELECT count() FROM "+table+" WHERE toYYYYMM(`"+factsschema.TsColumn+"`) = 202611"))

	// A store that was running across the exchange keeps writing: the name
	// resolves to the new table.
	id, err := s.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "after the exchange", Ts: time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	assert.Equal(t, "1", scalar(t, s, "SELECT count() FROM "+table+" WHERE `"+idColumn+"` = "+strconv.FormatUint(id, 10)))
}

const (
	writerChildKey   = "BOXER_CHSTORE_IT_WRITER_DB"
	writerChildRows  = 64
	writerChildBlock = 8
)

// writeBurst writes n log rows one insert each through a store of its own
// over cfg, leasing ids in small blocks so that the lease row is contended.
func writeBurst(cfg Config, who string, n int) (err error) {
	cfg.RowIds.BlockSize = writerChildBlock
	s, err := New(cfg)
	if err != nil {
		return
	}
	for i := range n {
		if _, err = s.WriteLog(context.Background(), factsstore.LogRow{AppId: "writer", Level: "info", Message: who + "-" + strconv.Itoa(i)}); err != nil {
			return
		}
	}
	return
}

// TestServerWriterChild is the child half of
// TestServer_ConcurrentWriters_LeaseDisjointIds: one more operating-system
// process writing to the parent's scratch table. It is skipped in a normal
// run.
func TestServerWriterChild(t *testing.T) {
	db := os.Getenv(writerChildKey)
	if db == "" {
		t.Skip("child-process helper for the concurrent writers test")
	}
	cfg := ConfigWithEnvDefaults(Config{Database: db, Table: "facts", RunId: "run-child-" + strconv.Itoa(os.Getpid())})
	require.NoError(t, writeBurst(cfg, "pid"+strconv.Itoa(os.Getpid()), writerChildRows))
}

// Several stores in this process and several other processes write to one
// table at once, each leasing small blocks: every row lands, no id repeats,
// and every id carries the leased stream's tag (ADR-0397 §SD2, ADR-0319
// §SD3).
func TestServer_ConcurrentWriters_LeaseDisjointIds(t *testing.T) {
	cfg := serverScratch(t, "writers")
	s := serverStore(t, cfg)
	require.NoError(t, s.SetupTable(context.Background(), ""))

	const inProcess, children = 6, 3
	var wg sync.WaitGroup
	errs := make(chan error, inProcess+children)
	for i := range inProcess {
		wg.Go(func() {
			errs <- writeBurst(cfg, "store"+strconv.Itoa(i), writerChildRows)
		})
	}
	for range children {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestServerWriterChild$", "-test.count=1")
			cmd.Env = append(os.Environ(), writerChildKey+"="+cfg.Database)
			out, err := cmd.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("child writer: %w\n%s", err, out)
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	ids := storedIds(t, s, cfg.Database+".facts")
	require.Len(t, ids, (inProcess+children)*writerChildRows, "every row landed")
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		_, dup := seen[id]
		require.False(t, dup, "id %d was handed out twice", id)
		seen[id] = struct{}{}
		require.Equal(t, vocab.IdStreamFactsRow.Value(), tagOf(id), "leased, not process-local")
	}
	// Every writer leased its own blocks: the bodies span at least what was
	// written, and the lease row moved past all of them.
	next, err := strconv.ParseUint(scalar(t, s, "SELECT argMin(next, (_block_number, _block_offset)) FROM "+cfg.Database+"."+rowalloc.TableName+" WHERE tag = "+strconv.FormatUint(uint64(vocab.IdStreamFactsRow.Value()), 10)), 10, 64)
	require.NoError(t, err)
	var maxBody uint64
	for _, id := range ids {
		maxBody = max(maxBody, uint64(identifier.TaggedId(id).RemoveTag()))
	}
	assert.Less(t, maxBody, next, "no id lies beyond what the lease row gave away")
}

// While the lease table cannot be reached a write still lands, under the
// process-local stream; once it is back and the retry interval has passed
// the store leases again (ADR-0397 §SD2).
func TestServer_LeaseOutageAndRecovery(t *testing.T) {
	cfg := serverScratch(t, "outage")
	s := serverStore(t, cfg)
	ctx := context.Background()
	require.NoError(t, s.SetupTable(ctx, ""))
	lease := cfg.Database + "." + rowalloc.TableName

	first, err := s.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "before"})
	require.NoError(t, err)
	require.Equal(t, vocab.IdStreamFactsRow.Value(), tagOf(first))

	// The outage: the lease table is out of reach, its state kept.
	require.NoError(t, s.exec.Exec(ctx, "RENAME TABLE "+lease+" TO "+lease+"_away"))
	late := serverStore(t, cfg) // a process that starts during the outage holds no block
	during, err := late.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "during"})
	require.NoError(t, err, "the write lands without a lease")
	assert.Equal(t, vocab.IdStreamFactsRowLocal.Value(), tagOf(during), "its id says it is process-local")
	again, err := late.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "during, again"})
	require.NoError(t, err)
	assert.Equal(t, vocab.IdStreamFactsRowLocal.Value(), tagOf(again), "no lease is attempted inside the retry interval")
	held, err := s.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "a block in hand outlasts the outage"})
	require.NoError(t, err)
	assert.Equal(t, vocab.IdStreamFactsRow.Value(), tagOf(held))

	// Recovery.
	require.NoError(t, s.exec.Exec(ctx, "RENAME TABLE "+lease+"_away TO "+lease))
	stillLocal, err := late.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "back, inside the interval"})
	require.NoError(t, err)
	assert.Equal(t, vocab.IdStreamFactsRowLocal.Value(), tagOf(stillLocal))
	late.ids.retryAt.Store(0) // the retry interval passes
	after, err := late.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "after"})
	require.NoError(t, err)
	assert.Equal(t, vocab.IdStreamFactsRow.Value(), tagOf(after), "leased again")

	ids := storedIds(t, s, cfg.Database+".facts")
	require.Len(t, ids, 6)
	seen := map[uint64]struct{}{}
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	assert.Len(t, seen, 6, "no id repeats across the outage")
}

// The lease table is state: the next unleased body of each stream lives
// nowhere else. Dropped and provisioned again it starts over, and hands
// out ids rows already carry.
func TestServer_ADroppedLeaseTableRestartsTheStream(t *testing.T) {
	cfg := serverScratch(t, "leaseloss")
	s := serverStore(t, cfg)
	ctx := context.Background()
	require.NoError(t, s.SetupTable(ctx, ""))
	first, err := s.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "before"})
	require.NoError(t, err)

	require.NoError(t, s.exec.Exec(ctx, "DROP TABLE "+cfg.Database+"."+rowalloc.TableName+" SYNC"))
	require.NoError(t, s.SetupTable(ctx, ""))
	fresh := serverStore(t, cfg)
	second, err := fresh.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "after"})
	require.NoError(t, err)
	assert.Equal(t, first, second, "the stream started over: this is why the lease table is backed up and restored with the facts")
}

// logBatch is n log rows as one facts-shaped batch, with ids minted by s.
func logBatch(t *testing.T, s *Store, n int, message string) (records []arrow.RecordBatch) {
	t.Helper()
	ent := dml.NewInEntityFacts(s.allocator, n)
	ts := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for i := range n {
		id, err := s.mintId(context.Background())
		require.NoError(t, err)
		s.encodeLogEntity(ent, id, factsstore.LogRow{AppId: "a", Level: "info", Message: message + strconv.Itoa(i), Ts: ts})
		require.NoError(t, ent.CommitEntity())
	}
	records, err := ent.TransferRecords(nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, r := range records {
			r.Release()
		}
	})
	return
}

// A batch sent again lands once on a server: under the retry token a
// generated store sends (ADR-0387 §SD6), and, with no token, as the same
// block within the table's window. Another batch under another token
// lands.
func TestServer_AResentBatchLandsOnce(t *testing.T) {
	cfg := serverScratch(t, "resend")
	s := serverStore(t, cfg)
	ctx := context.Background()
	require.NoError(t, s.SetupTable(ctx, ""))
	table := cfg.Database + ".facts"

	withToken := logBatch(t, s, 3, "token-")
	settings := recordstore.InsertSettings{}.WithDeduplicationToken("batch-1")
	for range 3 {
		applied, err := s.exec.InsertArrowWith(ctx, table, withToken, settings)
		require.NoError(t, err)
		require.True(t, applied)
	}
	assert.Equal(t, "3", scalar(t, s, "SELECT count() FROM "+table), "three sends under one token are one batch")

	plain := logBatch(t, s, 2, "plain-")
	for range 2 {
		require.NoError(t, s.exec.InsertArrow(ctx, table, plain))
	}
	assert.Equal(t, "5", scalar(t, s, "SELECT count() FROM "+table), "the same block sent twice without a token is folded by the window")

	other := logBatch(t, s, 2, "other-")
	_, err := s.exec.InsertArrowWith(ctx, table, other, recordstore.InsertSettings{}.WithDeduplicationToken("batch-2"))
	require.NoError(t, err)
	assert.Equal(t, "7", scalar(t, s, "SELECT count() FROM "+table))

	// The window is the table's: a table without it keeps both sends.
	bare := Config{Database: cfg.Database, Table: "facts_bare"}
	stmts, err := tableStatements(bare, "")
	require.NoError(t, err)
	create := stmts[len(stmts)-1]
	i := strings.Index(create, ", "+recordstore.DedupWindowSetting)
	j := strings.Index(create[i:], "COMMENT '")
	require.Greater(t, j, 0)
	require.NoError(t, s.exec.Exec(ctx, create[:i]+" "+create[i+j:]))
	for range 2 {
		_, err = s.exec.InsertArrowWith(ctx, cfg.Database+".facts_bare", withToken, settings)
		require.NoError(t, err)
	}
	assert.Equal(t, "6", scalar(t, s, "SELECT count() FROM "+cfg.Database+".facts_bare"), "without the window a re-sent batch lands twice")
}

// A table created with another layout than the build's is judged by its
// columns: ok, with a note, and nothing in the plan rewrites it (ADR-0397:
// a differing layout is a note, not a failure). With the engine setting
// naming that layout, the note is gone.
func TestServer_AForeignLayoutIsANote(t *testing.T) {
	cfg := serverScratch(t, "foreign")
	s := serverStore(t, cfg)
	ctx := context.Background()
	foreign := "MergeTree() PARTITION BY toYYYYMMDD(`" + factsschema.TsColumn + "`) ORDER BY (`" + idColumn + "`, `" + factsschema.TsColumn + "`)"
	require.NoError(t, s.SetupTable(ctx, foreign))

	c, err := SchemaContract(cfg)
	require.NoError(t, err)
	p := schemaplan.PlanTable(ctx, s.exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, p.Verdict.Verdict, p.Verdict.Summary())
	assert.True(t, p.Verdict.LayoutDiffers, p.Verdict.Summary())
	assert.Empty(t, p.Statements, "the window is there, and the plan does not stamp or rewrite a table in another layout")
	assert.False(t, p.IsRefused())

	_, err = s.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "into a foreign layout", Ts: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	assert.Equal(t, "20261120", scalar(t, s, "SELECT any(partition_id) FROM system.parts WHERE active AND database = '"+cfg.Database+"' AND table = 'facts'"))

	// Under BOXER_FACTS_ENGINE naming the table's own layout the contract
	// expects it.
	own, err := composeFactsTable(cfg, foreign)
	require.NoError(t, err)
	c.Layout, c.Stamp = own.layout, own.stamp
	v := recordstore.JudgeTable(ctx, s.exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	assert.False(t, v.LayoutDiffers, v.Summary())
	assert.Equal(t, recordstore.StampAgeSame, v.Age)
}

// A binary without a transform finds a view under the name it writes to:
// setup fails and says what the binary lacks, instead of succeeding over a
// CREATE TABLE IF NOT EXISTS that changed nothing (ADR-0402 §SD2).
func TestServer_AViewUnderTheWrittenNameFailsSetup(t *testing.T) {
	cfg := serverScratch(t, "viewname")
	s := serverStore(t, cfg)
	ctx := context.Background()
	rows := Config{Database: cfg.Database, Table: "rows"}
	stmts, err := tableStatements(rows, "")
	require.NoError(t, err)
	for _, stmt := range stmts {
		require.NoError(t, s.exec.Exec(ctx, stmt))
	}
	require.NoError(t, s.exec.Exec(ctx, "CREATE VIEW "+cfg.Database+".facts AS SELECT * FROM "+cfg.Database+".rows"))

	err = s.SetupTable(ctx, "")
	require.ErrorIs(t, err, ErrFactsTableTakesNoInsert)
	assert.Equal(t, "View", scalar(t, s, "SELECT engine FROM system.tables WHERE database = '"+cfg.Database+"' AND name = 'facts'"))

	c, err := SchemaContract(cfg)
	require.NoError(t, err)
	p := schemaplan.PlanTable(ctx, s.exec, c)
	assert.Equal(t, recordstore.SchemaVerdictIncompatible, p.Verdict.Verdict, p.Verdict.Summary())
	assert.Empty(t, p.Statements)

	// The same server through the deployment's binary: setup passes and
	// creates nothing under the name.
	unregister := recordstore.RegisterInsertTransform(cfg.Database+".facts", recordstore.InsertTransformFunc(
		func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			for _, r := range records {
				r.Retain()
			}
			return cfg.Database + ".rows", records, nil
		}))
	defer unregister()
	require.NoError(t, s.SetupTable(ctx, ""))
	id, err := s.WriteLog(ctx, factsstore.LogRow{AppId: "a", Message: "through the transform"})
	require.NoError(t, err)
	assert.Equal(t, "1", scalar(t, s, "SELECT count() FROM "+cfg.Database+".facts WHERE `"+idColumn+"` = "+strconv.FormatUint(id, 10)))
}
