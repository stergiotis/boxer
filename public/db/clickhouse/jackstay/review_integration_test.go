//go:build integration

package jackstay

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
)

// liveCount runs a `SELECT count() AS cnt` query and returns its value.
func liveCount(t *testing.T, q QueryI, sql string) (n uint64) {
	t.Helper()
	type row struct {
		N uint64 `json:"cnt"`
	}
	rows, err := queryRows[row](context.Background(), q, sql)
	require.NoError(t, err, sql)
	require.Len(t, rows, 1, sql)
	return rows[0].N
}

// A keyless table's leaf predicate hashes the whole row as RowBinary, whose
// bytes for a JSON column depend on output_format_binary_write_json_as_string.
// A DELETE's mutation evaluates its WHERE under the server's defaults, not the
// settings the query carries, so the hash reads JSON as text, and the repair's
// DELETE removes exactly the rows the diff's predicate selected.
func TestRepairDelete_KeylessJSON_LiveServer(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()
	drop := func() { _ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itTarget+" SYNC") }
	drop()
	t.Cleanup(drop)
	require.NoError(t, client.Exec(ctx, "CREATE DATABASE "+itTarget))
	ref := datacatalog.TableRef{Database: itTarget, Name: "keyless"}
	if err := client.Exec(ctx, "CREATE TABLE "+QuoteRef(ref)+" (n UInt64, j JSON) ENGINE = MergeTree ORDER BY tuple()"); err != nil {
		t.Skipf("the server has no JSON type: %v", err)
	}
	require.NoError(t, client.Exec(ctx, "INSERT INTO "+QuoteRef(ref)+
		" SELECT number, concat('{\"a\":', toString(number), ',\"b\":\"', toString(number % 7), '\"}') FROM numbers(2000)"))

	inv, err := Discover(ctx, client)
	require.NoError(t, err)
	ti, has := inv.Table(ref)
	require.True(t, has)
	hashAsText := jsonColumns(ti, []string{"n", "j"})
	require.Equal(t, []string{"j"}, hashAsText, "the plan records the JSON column")
	spec := DigestSpec{Ref: ref, CopyColumns: []string{"n", "j"}, HashAsText: hashAsText, Chunking: Chunking{Kind: ChunkingSingle, Leaves: 8}}
	pred := spec.LeafPredicate([]uint32{0, 2, 5})
	selected := liveCount(t, client, "SELECT count() AS cnt FROM "+QuoteRef(ref)+" WHERE "+pred+digestSettings)
	unpinned := liveCount(t, client, "SELECT count() AS cnt FROM "+QuoteRef(ref)+" WHERE "+pred+
		" SETTINGS output_format_binary_write_json_as_string = 0 FORMAT JSONEachRow")
	require.NotZero(t, selected)
	assert.Equal(t, selected, unpinned, "the predicate selects the same rows whatever the session's settings")

	require.NoError(t, client.Exec(ctx, deleteQuery(ref, pred)))
	left := liveCount(t, client, "SELECT count() AS cnt FROM "+QuoteRef(ref)+" FORMAT JSONEachRow")
	assert.Equal(t, uint64(2000)-selected, left, "the DELETE removes the rows the diff's predicate selected")
	assert.Zero(t, liveCount(t, client, "SELECT count() AS cnt FROM "+QuoteRef(ref)+" WHERE "+pred+digestSettings),
		"no row the predicate selects is left")
}

// A materialized view on the target that reads from a synced table is named
// in the table's verdict: a sync's inserts reach it, a clear does not.
func TestMaterializedViewOnTarget_LiveServer(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()
	drop := func() {
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itSource+" SYNC")
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itTarget+" SYNC")
	}
	drop()
	t.Cleanup(drop)
	for _, sql := range []string{
		"CREATE DATABASE " + itSource,
		"CREATE DATABASE " + itTarget,
		"CREATE TABLE " + itSource + ".t (k UInt64, v String) ENGINE = MergeTree ORDER BY k",
		"CREATE TABLE " + itTarget + ".t (k UInt64, v String) ENGINE = MergeTree ORDER BY k",
		"CREATE TABLE " + itTarget + ".plain (k UInt64, v String) ENGINE = MergeTree ORDER BY k",
		"CREATE MATERIALIZED VIEW " + itTarget + ".mv ENGINE = SummingMergeTree ORDER BY k AS SELECT k, count() AS c FROM " + itTarget + ".t GROUP BY k",
		"CREATE TABLE " + itSource + ".plain (k UInt64, v String) ENGINE = MergeTree ORDER BY k",
	} {
		require.NoError(t, client.Exec(ctx, sql), sql)
	}
	inv, err := Discover(ctx, client)
	require.NoError(t, err)
	ep := SourceEndpoint()
	sel := Selection{Databases: []string{itSource}, DatabaseMap: map[string]string{itSource: itTarget}}
	plan, err := BuildPlan(newOps(t), ep, ep, &inv, &inv, sel, time.Now())
	require.NoError(t, err)

	pt := findTable(&plan, datacatalog.TableRef{Database: itSource, Name: "t"})
	require.NotNil(t, pt)
	assert.True(t, containsSubstring(pt.Notes, itTarget+".mv"), "notes: %v", pt.Notes)
	plain := findTable(&plan, datacatalog.TableRef{Database: itSource, Name: "plain"})
	require.NotNil(t, plain)
	assert.False(t, containsSubstring(plain.Notes, "read from this table"), "notes: %v", plain.Notes)
}

// A Replicated create whose Keeper path is the source's is refused, and one
// built on {uuid} is proposed. It needs a server with Keeper.
func TestReplicatedCreate_LiveServer(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()
	drop := func() {
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itSource+" SYNC")
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itTarget+" SYNC")
	}
	drop()
	t.Cleanup(drop)
	require.NoError(t, client.Exec(ctx, "CREATE DATABASE "+itSource))
	if err := client.Exec(ctx, "CREATE TABLE "+itSource+".fixed (k UInt64) ENGINE = ReplicatedMergeTree('/clickhouse/jackstay_it/{database}/{table}', 'r1') ORDER BY k"); err != nil {
		t.Skipf("the server cannot create a replicated table (no Keeper?): %v", err)
	}
	require.NoError(t, client.Exec(ctx, "CREATE TABLE "+itSource+".own (k UInt64) ENGINE = ReplicatedMergeTree('/clickhouse/jackstay_it/{uuid}', 'r1') ORDER BY k"))
	inv, err := Discover(ctx, client)
	require.NoError(t, err)
	ep := SourceEndpoint()
	sel := Selection{Databases: []string{itSource}, DatabaseMap: map[string]string{itSource: itTarget}}
	plan, err := BuildPlan(newOps(t), ep, ep, &inv, &inv, sel, time.Now())
	require.NoError(t, err)

	fixed := findTable(&plan, datacatalog.TableRef{Database: itSource, Name: "fixed"})
	require.NotNil(t, fixed)
	assert.Equal(t, VerdictIncompatible, fixed.Verdict, "reasons: %v", fixed.Reasons)
	assert.True(t, containsSubstring(fixed.Reasons, "Keeper path"), "reasons: %v", fixed.Reasons)
	own := findTable(&plan, datacatalog.TableRef{Database: itSource, Name: "own"})
	require.NotNil(t, own)
	assert.Equal(t, VerdictCreate, own.Verdict, "reasons: %v", own.Reasons)
}

// The digest queries alias their outputs chunk, pid, display, kh, rh and
// keytext in the SELECT that reads the table. A table with columns of those
// names must still be hashed, chunked and filtered by its columns: a row that
// differs only in its kh column is a difference.
func TestDiff_ColumnsNamedLikeAliases_LiveServer(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()
	drop := func() {
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itSource+" SYNC")
		_ = client.Exec(context.Background(), "DROP DATABASE IF EXISTS "+itTarget+" SYNC")
	}
	drop()
	t.Cleanup(drop)
	s, d := itSource+".", itTarget+"."
	for _, sql := range []string{
		"CREATE DATABASE " + itSource,
		"CREATE DATABASE " + itTarget,
		"CREATE TABLE " + s + "named (k UInt64, kh UInt64, rh String, chunk String, pid UInt8, display String, keytext String) ENGINE = MergeTree ORDER BY k",
		"INSERT INTO " + s + "named SELECT number, number * 3, toString(number), toString(number % 10), number % 4, 'd', 'x' FROM numbers(3000)",
		"CREATE TABLE " + d + "named AS " + s + "named",
		"INSERT INTO " + d + "named SELECT k, if(k = 1234, 0, kh), rh, if(k = 2345, 'other', chunk), pid, display, keytext FROM " + s + "named",
		// The range key itself is named chunk.
		"CREATE TABLE " + s + "keyed (chunk UInt64, v String) ENGINE = MergeTree ORDER BY chunk",
		"INSERT INTO " + s + "keyed SELECT number, toString(number) FROM numbers(3000)",
		"CREATE TABLE " + d + "keyed AS " + s + "keyed",
		"INSERT INTO " + d + "keyed SELECT chunk, if(chunk = 777, 'changed', v) FROM " + s + "keyed",
	} {
		require.NoError(t, client.Exec(ctx, sql), sql)
	}
	inv, err := Discover(ctx, client)
	require.NoError(t, err)
	ep := SourceEndpoint()
	sel := Selection{Databases: []string{itSource}, DatabaseMap: map[string]string{itSource: itTarget}}
	plan, err := BuildPlan(newOps(t), ep, ep, &inv, &inv, sel, time.Now())
	require.NoError(t, err)
	chunkOpts := DefaultChunkingOptions()
	chunkOpts.TargetChunkRows = 1000
	chunkOpts.TargetLeafRows = 256
	diff := func(pt *PlanTable) *TableDiff {
		require.NotNil(t, pt)
		require.NoError(t, DiffPlanTable(ctx, ServerSource(client), client, pt, false, chunkOpts, DefaultDiffOptions(), time.Now()))
		return pt.Diff
	}

	dd := diff(findTable(&plan, datacatalog.TableRef{Database: itSource, Name: "named"}))
	assert.Equal(t, uint64(2), dd.Changed, "the rows that differ only in kh and in chunk")
	assert.Zero(t, dd.Missing+dd.Extra)

	keyed := findTable(&plan, datacatalog.TableRef{Database: itSource, Name: "keyed"})
	dd = diff(keyed)
	assert.Equal(t, ChunkingRange, keyed.Chunking.Kind)
	assert.Equal(t, uint64(1), dd.Changed)
	assert.Equal(t, uint64(3000), dd.SrcRows)

	// A filter on the column named chunk selects by the column.
	sel.Filters = map[string]string{itSource + ".named": "chunk = '5'"}
	plan, err = BuildPlan(newOps(t), ep, ep, &inv, &inv, sel, time.Now())
	require.NoError(t, err)
	dd = diff(findTable(&plan, datacatalog.TableRef{Database: itSource, Name: "named"}))
	assert.Equal(t, uint64(300), dd.SrcRows)
	assert.Equal(t, uint64(299), dd.DstRows, "the target row whose chunk column changed is outside the slice")
	assert.Equal(t, uint64(1), dd.Missing)
	assert.Zero(t, dd.Changed+dd.Extra)
}

func containsSubstring(ss []string, sub string) (ok bool) {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
