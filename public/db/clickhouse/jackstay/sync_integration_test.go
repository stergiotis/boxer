//go:build integration

package jackstay

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
)

const (
	itSyncSource = "jackstay_it_sync_src"
	itSyncTarget = "jackstay_it_sync_dst"
)

// TestSync_LiveServer copies tables between two databases on one server in
// each mode and checks the result with the diff (ADR-0259 §SD5).
func TestSync_LiveServer(t *testing.T) {
	liveClient(t)
	client := chclient.New(chclient.ConfigFromEnv(), &http.Client{})
	ctx := context.Background()
	drop := func() {
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itSyncSource+" SYNC")
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itSyncTarget+" SYNC")
	}
	drop()
	t.Cleanup(drop)
	exec := func(sqls ...string) {
		for _, sql := range sqls {
			require.NoError(t, client.Exec(ctx, sql), sql)
		}
	}
	s, d := itSyncSource+".", itSyncTarget+"."
	exec(
		"CREATE DATABASE "+itSyncSource,
		"CREATE DATABASE "+itSyncTarget,
		"CREATE TABLE "+s+"part (k UInt64, s String) ENGINE = MergeTree PARTITION BY k % 4 ORDER BY k",
		"INSERT INTO "+s+"part SELECT number, toString(number) FROM numbers(20000)",
		"CREATE TABLE "+d+"part AS "+s+"part",
		"CREATE TABLE "+s+"rng (k UInt64, v Float64) ENGINE = MergeTree ORDER BY k",
		"INSERT INTO "+s+"rng SELECT number, number / 3 FROM numbers(6000)",
		"CREATE TABLE "+d+"rng AS "+s+"rng",
		"CREATE TABLE "+s+"logt (a UInt32) ENGINE = Log",
		"INSERT INTO "+s+"logt SELECT number FROM numbers(300)",
		"CREATE TABLE "+d+"logt AS "+s+"logt",
		"CREATE TABLE "+s+"app (k UInt64) ENGINE = MergeTree ORDER BY k",
		"INSERT INTO "+s+"app SELECT number FROM numbers(1000)",
		"CREATE TABLE "+d+"app AS "+s+"app",
		"INSERT INTO "+d+"app SELECT number + 1000000 FROM numbers(10)",
	)

	ops := newOps(t)
	ep := SourceEndpoint()
	inv, err := Discover(ctx, client)
	require.NoError(t, err)
	plan, err := BuildPlan(ops, ep, ep, &inv, &inv, Selection{Databases: []string{itSyncSource}, DatabaseMap: map[string]string{itSyncSource: itSyncTarget}}, time.Now())
	require.NoError(t, err)
	tables := make(map[string]*PlanTable, len(plan.Tables))
	for i := range plan.Tables {
		tables[plan.Tables[i].Source.Name] = &plan.Tables[i]
	}
	chunkOpts := DefaultChunkingOptions()
	chunkOpts.TargetChunkRows = 1000
	chunkOpts.TargetLeafRows = 256
	diffOpts := DefaultDiffOptions()

	journalPath := filepath.Join(t.TempDir(), "plan.json.journal")
	openJournal := func(run string) *Journal {
		j, jerr := OpenJournal(journalPath, run)
		require.NoError(t, jerr)
		t.Cleanup(func() { _ = j.Close() })
		return j
	}
	prepare := func(name string, ts TableSync) *PlanTable {
		pt := tables[name]
		require.NotNil(t, pt, name)
		require.True(t, pt.IsDiffable(), "%s: %s %v", name, pt.Verdict, pt.Reasons)
		if pt.Chunking == nil {
			c, cerr := DeriveChunking(ctx, client, pt.Source, pt.SortingKey, pt.PartitionKey, pt.Rows, chunkOpts)
			require.NoError(t, cerr)
			pt.Chunking = &c
		}
		pt.Sync = &ts
		return pt
	}
	diffOf := func(pt *PlanTable) *TableDiff {
		require.NoError(t, DiffPlanTable(ctx, client, client, pt, false, chunkOpts, diffOpts, time.Now()))
		return pt.Diff
	}
	count := func(table string) uint64 {
		rows, cerr := queryRows[countRow](ctx, client, "SELECT count() AS n FROM "+table+jsonSettings)
		require.NoError(t, cerr)
		return rows[0].N
	}

	t.Run("full, interrupted and resumed", func(t *testing.T) {
		pt := prepare("part", TableSync{Mode: SyncModeFull})
		j := openJournal("run-full")
		cctx, cancel := context.WithCancel(ctx)
		opts := DefaultSyncOptions()
		copied := 0
		opts.Progress = func(r ChunkResult) {
			copied++
			if copied == 2 {
				cancel()
			}
		}
		_, err := SyncTable(cctx, client, client, pt, j, opts, time.Now)
		assert.ErrorIs(t, err, context.Canceled)
		cancel()
		require.NoError(t, j.Close())

		j = openJournal("run-full")
		rep, err := SyncTable(ctx, client, client, pt, j, DefaultSyncOptions(), time.Now)
		require.NoError(t, err)
		assert.Equal(t, 2, rep.Done, "two chunks were verified before the interruption")
		assert.Equal(t, 2, rep.Copied)
		assert.Zero(t, rep.Failed, "%v", rep.Problems)
		assert.True(t, diffOf(pt).IsIdentical())
	})
	t.Run("non-empty target: refuse, then replace", func(t *testing.T) {
		pt := prepare("part", TableSync{Mode: SyncModeFull})
		_, err := SyncTable(ctx, client, client, pt, openJournal("run-refuse"), DefaultSyncOptions(), time.Now)
		assert.Error(t, err)
		exec("ALTER TABLE " + d + "part UPDATE s = 'drifted' WHERE k % 100 = 7 SETTINGS mutations_sync = 2")
		pt.Sync = &TableSync{Mode: SyncModeFull, Existing: ExistingPolicyReplace}
		rep, err := SyncTable(ctx, client, client, pt, openJournal("run-replace"), DefaultSyncOptions(), time.Now)
		require.NoError(t, err)
		assert.Equal(t, 4, rep.Copied)
		assert.Equal(t, uint64(20000), rep.Cleared)
		assert.True(t, diffOf(pt).IsIdentical())
	})
	t.Run("repair, and its stale guard", func(t *testing.T) {
		pt := prepare("part", TableSync{Mode: SyncModeRepair})
		exec(
			"DELETE FROM "+d+"part WHERE k BETWEEN 100 AND 120",
			"ALTER TABLE "+d+"part UPDATE s = 'changed' WHERE k IN (5001, 5002) SETTINGS mutations_sync = 2",
			"INSERT INTO "+d+"part VALUES (900001, 'extra')",
		)
		dd := diffOf(pt)
		require.False(t, dd.IsIdentical())
		// A change the diff did not see, in a chunk it listed.
		exec("INSERT INTO " + d + "part VALUES (900005, 'unseen')")
		rep, err := SyncTable(ctx, client, client, pt, openJournal("run-repair-stale"), DefaultSyncOptions(), time.Now)
		require.NoError(t, err)
		assert.Positive(t, rep.Stale, "%+v", rep)
		// 900001 and 900005 both land in partition k % 4 = 1: that chunk is
		// stale and untouched (5 rows still missing, both extras kept); the
		// other three partitions were repaired.
		assert.Equal(t, uint64(5000-5+2), count(d+"part WHERE k % 4 = 1"), "a stale chunk is left alone")
		assert.Equal(t, uint64(15000), count(d+"part WHERE k % 4 != 1"))

		dd = diffOf(pt)
		rep, err = SyncTable(ctx, client, client, pt, openJournal("run-repair"), DefaultSyncOptions(), time.Now)
		require.NoError(t, err)
		assert.Zero(t, rep.Stale+rep.Failed, "%v", rep.Problems)
		assert.True(t, diffOf(pt).IsIdentical())
		assert.Equal(t, uint64(20000), count(d+"part"))
	})
	t.Run("range chunks, sample", func(t *testing.T) {
		pt := prepare("rng", TableSync{Mode: SyncModeSample, SampleNum: 1, SampleDen: 10})
		require.Equal(t, ChunkingRange, pt.Chunking.Kind)
		rep, err := SyncTable(ctx, client, client, pt, openJournal("run-sample"), DefaultSyncOptions(), time.Now)
		require.NoError(t, err)
		assert.Zero(t, rep.Failed, "%v", rep.Problems)
		spec, _, _ := pt.DigestSpecs(false)
		want := count(s + "rng WHERE " + spec.SamplePredicate(1, 10))
		assert.Equal(t, want, count(d+"rng"))
		assert.InDelta(t, 600, float64(want), 150)
	})
	t.Run("log engine, leftover rows cleared", func(t *testing.T) {
		pt := prepare("logt", TableSync{Mode: SyncModeFull})
		j := openJournal("run-log")
		require.NoError(t, j.RecordStart(pt.Source.String(), true, *pt.Sync, time.Now()))
		exec("INSERT INTO " + d + "logt VALUES (1), (2)") // a half-finished earlier attempt
		rep, err := SyncTable(ctx, client, client, pt, j, DefaultSyncOptions(), time.Now)
		require.NoError(t, err)
		assert.Equal(t, 1, rep.Copied)
		assert.Equal(t, uint64(2), rep.Cleared)
		assert.Equal(t, uint64(300), count(d+"logt"))
	})
	t.Run("append beside foreign rows", func(t *testing.T) {
		pt := prepare("app", TableSync{Mode: SyncModeFull, Existing: ExistingPolicyAppend})
		rep, err := SyncTable(ctx, client, client, pt, openJournal("run-append"), DefaultSyncOptions(), time.Now)
		require.NoError(t, err)
		assert.Equal(t, 1, rep.Copied)
		assert.Equal(t, uint64(1010), count(d+"app"))
		dd := diffOf(pt)
		assert.Equal(t, uint64(10), dd.Extra)
		assert.Zero(t, dd.Missing+dd.Changed)
	})
}
