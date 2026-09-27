//go:build integration

package jackstay

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
)

const (
	itDiffSource = "jackstay_it_diff_src"
	itDiffTarget = "jackstay_it_diff_dst"
)

// TestDiff_LiveServer builds known differences between two databases on one
// server and checks that the diff counts them exactly (ADR-0259 §SD4).
func TestDiff_LiveServer(t *testing.T) {
	liveClient(t)
	client := chclient.New(chclient.ConfigFromEnv(), &http.Client{})
	ctx := context.Background()
	drop := func() {
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itDiffSource+" SYNC")
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itDiffTarget+" SYNC")
	}
	drop()
	t.Cleanup(drop)

	s, d := itDiffSource+".", itDiffTarget+"."
	for _, sql := range []string{
		"CREATE DATABASE " + itDiffSource,
		"CREATE DATABASE " + itDiffTarget,

		// Partitioned: 10 rows missing, 5 extra, 3 changed, and the whole
		// k >= 9000 partition absent on the target.
		"CREATE TABLE " + s + "part (k UInt64, s String, ts DateTime64(3, 'Europe/Zurich')) ENGINE = MergeTree PARTITION BY toUInt8(k >= 9000) ORDER BY k",
		"INSERT INTO " + s + "part SELECT number, toString(number), toDateTime64('2026-01-01 00:00:00', 3, 'UTC') + number FROM numbers(10000) WHERE number NOT BETWEEN 5000 AND 5004",
		"CREATE TABLE " + d + "part AS " + s + "part",
		"INSERT INTO " + d + "part SELECT k, if(k BETWEEN 500 AND 502, 'changed', s), ts FROM " + s + "part WHERE k NOT BETWEEN 100 AND 109 AND k < 9000",
		"INSERT INTO " + d + "part SELECT number, 'extra', now64(3) FROM numbers(5000, 5)",

		// Unpartitioned and large enough (under the test's options) for range
		// chunks: one row changed.
		"CREATE TABLE " + s + "rng (k UInt64, v Float64) ENGINE = MergeTree ORDER BY k",
		"INSERT INTO " + s + "rng SELECT number, number / 7 FROM numbers(5000)",
		"CREATE TABLE " + d + "rng AS " + s + "rng",
		"INSERT INTO " + d + "rng SELECT k, if(k = 4321, -1, v) FROM " + s + "rng",

		// No sorting key: rows are keyed by themselves.
		"CREATE TABLE " + s + "logt (a UInt32, b String) ENGINE = Log",
		"INSERT INTO " + s + "logt SELECT number, toString(number) FROM numbers(100)",
		"CREATE TABLE " + d + "logt AS " + s + "logt",
		"INSERT INTO " + d + "logt SELECT a, b FROM " + s + "logt WHERE a NOT IN (7, 8)",
		"INSERT INTO " + d + "logt VALUES (1000, 'x')",

		// JSON written with different key order on each side: equal.
		"CREATE TABLE " + s + "js (k UInt64, j JSON) ENGINE = MergeTree ORDER BY k",
		"INSERT INTO " + s + "js SELECT number, '{\"a\":1,\"b\":\"x\"}' FROM numbers(50)",
		"CREATE TABLE " + d + "js AS " + s + "js",
		"INSERT INTO " + d + "js SELECT number, '{\"b\":\"x\",\"a\":1}' FROM numbers(50)",

		// Replacing: the source still holds an unmerged duplicate.
		"CREATE TABLE " + s + "repl (k UInt64, v UInt64) ENGINE = ReplacingMergeTree ORDER BY k",
		"SYSTEM STOP MERGES " + s + "repl",
		"INSERT INTO " + s + "repl VALUES (1, 1), (2, 2)",
		"INSERT INTO " + s + "repl VALUES (1, 1)",
		"CREATE TABLE " + d + "repl AS " + s + "repl",
		"INSERT INTO " + d + "repl VALUES (1, 1), (2, 2)",
	} {
		require.NoError(t, client.Exec(ctx, sql), sql)
	}

	ops := newOps(t)
	ep := SourceEndpoint()
	inv, err := Discover(ctx, client)
	require.NoError(t, err)
	plan, err := BuildPlan(ops, ep, ep, &inv, &inv, Selection{Databases: []string{itDiffSource}, DatabaseMap: map[string]string{itDiffSource: itDiffTarget}}, time.Now())
	require.NoError(t, err)
	tables := make(map[string]*PlanTable, len(plan.Tables))
	for i := range plan.Tables {
		tables[plan.Tables[i].Source.Name] = &plan.Tables[i]
	}

	chunkOpts := DefaultChunkingOptions()
	chunkOpts.TargetChunkRows = 1000
	chunkOpts.TargetLeafRows = 256
	opts := DefaultDiffOptions()
	diff := func(name string, final bool) *TableDiff {
		pt := tables[name]
		require.NotNil(t, pt, name)
		require.True(t, pt.IsDiffable(), "%s: %s %v", name, pt.Verdict, pt.Reasons)
		require.NoError(t, DiffPlanTable(ctx, client, client, pt, final, chunkOpts, opts, time.Now()))
		return pt.Diff
	}

	t.Run("partitioned", func(t *testing.T) {
		dd := diff("part", false)
		pt := tables["part"]
		assert.Equal(t, ChunkingPartition, pt.Chunking.Kind)
		assert.Equal(t, leavesFor(pt.Rows/2, chunkOpts), pt.Chunking.Leaves, "leaves are sized per partition, of which there are two")
		assert.Less(t, pt.Chunking.Leaves, leavesFor(pt.Rows, chunkOpts), "fewer than a single-chunk table of the same rows would get")
		assert.Equal(t, uint64(9995), dd.SrcRows)
		assert.Equal(t, uint64(8995-10+5), dd.DstRows)
		assert.Equal(t, uint64(0), dd.UnresolvedLeaves)
		assert.Equal(t, uint64(1010), dd.Missing)
		assert.Equal(t, uint64(5), dd.Extra)
		assert.Equal(t, uint64(3), dd.Changed)
		absent := 0
		for _, cd := range dd.Differing {
			if cd.AbsentOnTarget {
				absent++
				assert.Equal(t, uint64(1000), cd.SrcRows)
				assert.Equal(t, "(1)", cd.Display)
			}
		}
		assert.Equal(t, 1, absent)
		assert.NotEmpty(t, dd.Examples)
	})
	t.Run("range chunks, reused", func(t *testing.T) {
		dd := diff("rng", false)
		c := tables["rng"].Chunking
		require.Equal(t, ChunkingRange, c.Kind)
		assert.GreaterOrEqual(t, len(c.Bounds), 3)
		assert.Equal(t, "UInt64", c.BoundType)
		assert.Equal(t, uint64(1), dd.Changed)
		assert.Zero(t, dd.Missing+dd.Extra)
		require.Len(t, dd.Differing, 1)
		assert.Equal(t, dd.Chunks-1, dd.IdenticalChunks)
		bounds := append([]string(nil), c.Bounds...)
		diff("rng", false)
		assert.Equal(t, bounds, tables["rng"].Chunking.Bounds, "a second diff reuses the layout")

		// The chunk predicates partition the table: their counts sum to its rows.
		type nRow struct {
			N uint64 `json:"n"`
		}
		var total uint64
		for i := 0; i <= len(c.Bounds); i++ {
			rows, qerr := queryRows[nRow](ctx, client, "SELECT count() AS n FROM "+s+"rng WHERE "+c.ChunkPredicate(strconv.Itoa(i), "")+jsonSettings)
			require.NoError(t, qerr)
			require.Len(t, rows, 1)
			total += rows[0].N
		}
		assert.Equal(t, uint64(5000), total)
		rows, qerr := queryRows[nRow](ctx, client, "SELECT count() AS n FROM "+s+"rng WHERE "+c.ChunkPredicate(strconv.Itoa(len(c.Bounds)+1), "")+jsonSettings)
		require.NoError(t, qerr)
		assert.Zero(t, rows[0].N, "past the last chunk selects nothing")
	})
	t.Run("no sorting key", func(t *testing.T) {
		dd := diff("logt", false)
		assert.Equal(t, uint64(2), dd.Missing)
		assert.Equal(t, uint64(1), dd.Extra)
		assert.Zero(t, dd.Changed)
	})
	t.Run("json key order", func(t *testing.T) {
		dd := diff("js", false)
		assert.True(t, dd.IsIdentical(), "%+v", dd.Differing)
	})
	t.Run("replacing without and with FINAL", func(t *testing.T) {
		dd := diff("repl", false)
		assert.False(t, dd.IsIdentical())
		assert.True(t, dd.MaybeSpurious)
		dd = diff("repl", true)
		assert.True(t, dd.IsIdentical(), "%+v", dd.Differing)
		assert.True(t, dd.Final)
	})
}
