package jackstay

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateFilter(t *testing.T) {
	cols := []string{"k", "s", "arr", "tup", "ts"}
	for _, f := range []string{
		"k > 5",
		"k > 5 AND s != ''",
		"s LIKE 'a%' OR k IN (1, 2, 3)",
		"arrayExists(x -> x > 1, arr)",
		"tup.1 = 'a'",
		"toYYYYMM(ts) = 202609",
		"`k` % 4 = 0",
	} {
		_, err := ValidateFilter(f, cols)
		assert.NoError(t, err, f)
	}
	for f, why := range map[string]string{
		"":                  "empty",
		"other > 1":         "column that is not copied",
		"t.k > 1":           "unqualified",
		"ts > now() - 3600": "depends on when or where",
		"rand() % 2 = 0":    "depends on when or where",
		"k IN (SELECT 1)":   "without a subquery",
		"k > (":             "does not parse",
		"1) UNION ALL SELECT 1 FROM other WHERE (1": "subquery",
		"dictGet('d', 'a', k) = 1":                  "depends on when or where",
	} {
		_, err := ValidateFilter(f, cols)
		if assert.Error(t, err, f) {
			assert.Contains(t, err.Error(), why, f)
		}
	}
	notes, err := ValidateFilter("ts >= '2026-01-01 00:00:00'", cols)
	require.NoError(t, err)
	assert.Len(t, notes, 1)
	notes, err = ValidateFilter("toDate(ts) >= '2026-01-01'", cols)
	require.NoError(t, err)
	assert.Empty(t, notes, "a Date literal is read the same in any zone")
}

func TestBuildPlan_Filter(t *testing.T) {
	ops, err := newTableOps()
	require.NoError(t, err)
	src := &Inventory{Databases: []DatabaseInfo{{Name: "s"}}, Tables: []TableInfo{plainTable("s", "t")}}
	dst := &Inventory{}
	sel := Selection{Databases: []string{"s"}, Filters: map[string]string{"s.t": " k > 1 "}}
	plan, err := BuildPlan(ops, Endpoint{URL: "http://a/"}, Endpoint{URL: "http://b/"}, src, dst, sel, time.Now())
	require.NoError(t, err)
	require.Len(t, plan.Tables, 1)
	assert.Equal(t, "k > 1", plan.Tables[0].Filter)
	require.NoError(t, plan.Validate())

	s, d, _ := plan.Tables[0].DigestSpecs(false)
	assert.Equal(t, "k > 1", s.Filter)
	assert.Equal(t, "k > 1", d.Filter, "the filter applies to both sides")
	cs := s.ForChunk("", "")
	assert.Contains(t, cs.LeafDigestQuery(), " WHERE k > 1)")

	sel.Filters = map[string]string{"s.t": "nope > 1"}
	_, err = BuildPlan(ops, Endpoint{URL: "http://a/"}, Endpoint{URL: "http://b/"}, src, dst, sel, time.Now())
	assert.ErrorContains(t, err, "invalid filter")

	sel.Filters = map[string]string{"s.missing": "k > 1"}
	_, err = BuildPlan(ops, Endpoint{URL: "http://a/"}, Endpoint{URL: "http://b/"}, src, dst, sel, time.Now())
	assert.ErrorContains(t, err, "does not hold")

	// A pack's table carries the filter it was exported under.
	packed := plainTable("s", "t")
	packed.Filter = "k > 1"
	src.Tables = []TableInfo{packed}
	sel.Filters = nil
	plan, err = BuildPlan(ops, Endpoint{Pack: "/p"}, Endpoint{URL: "http://b/"}, src, dst, sel, time.Now())
	require.NoError(t, err)
	assert.Equal(t, "k > 1", plan.Tables[0].Filter)
	sel.Filters = map[string]string{"s.t": "k > 2"}
	_, err = BuildPlan(ops, Endpoint{Pack: "/p"}, Endpoint{URL: "http://b/"}, src, dst, sel, time.Now())
	assert.ErrorContains(t, err, "exported under")
}

func TestCarryOver_FilterChangeDropsDiff(t *testing.T) {
	old := Plan{Tables: []PlanTable{*singleChunkTable("t", "t")}}
	old.Tables[0].Diff = &TableDiff{}
	fresh := Plan{Tables: []PlanTable{*singleChunkTable("t", "t")}}
	fresh.Tables[0].Chunking = nil
	fresh.Tables[0].Filter = "k > 1"
	fresh.CarryOver(&old, true)
	assert.NotNil(t, fresh.Tables[0].Chunking, "the layout survives a filter change")
	assert.Nil(t, fresh.Tables[0].Diff, "digests of other rows are not carried")
}

// A filtered table owns only its slice: the ownership count reads the slice,
// and a clear is a DELETE within the filter, never a TRUNCATE.
func TestSyncTable_FilteredReplaceClearsOnlyTheSlice(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.Filter = "k > 1"
	pt.Sync = &TableSync{Mode: SyncModeFull, Existing: ExistingPolicyReplace}
	src := &fakeClient{answer: func(sql string) (string, error) {
		switch {
		case isChunkList(sql):
			return `{"chunk":"","pid":"","display":""}` + "\n", nil
		case isDigestQuery(sql, "t"):
			return digestRow("", 2, 3, 3), nil
		}
		return "", nil
	}}
	var counts []string
	dstDigests := 0
	dst := &fakeClient{answer: func(sql string) (string, error) {
		switch {
		case strings.HasPrefix(sql, "SELECT count() AS n"):
			counts = append(counts, sql)
			return `{"n":5}` + "\n", nil
		case isDigestQuery(sql, "t"):
			assert.Contains(t, sql, "WHERE k > 1", "the target is read as its slice")
			dstDigests++
			if dstDigests == 1 {
				return digestRow("", 1, 9, 9), nil
			}
			return digestRow("", 2, 3, 3), nil
		}
		return "", nil
	}}
	j := openTestJournal(t, "r")
	rep, err := SyncTable(context.Background(), ServerSource(src), dst, pt, j, DefaultSyncOptions(), time.Now)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Copied, rep.Problems)
	require.Len(t, counts, 1)
	assert.Contains(t, counts[0], "WHERE k > 1")
	require.Len(t, dst.execs, 1)
	assert.True(t, strings.HasPrefix(dst.execs[0], "DELETE FROM"), dst.execs[0])
	assert.Contains(t, dst.execs[0], "k > 1")
	e, started := j.Started("s.t")
	require.True(t, started)
	assert.Equal(t, "k > 1", e.Filter)

	// Resuming the run under another filter is refused.
	pt.Filter = "k > 2"
	_, err = SyncTable(context.Background(), ServerSource(src), dst, pt, j, DefaultSyncOptions(), time.Now)
	assert.ErrorContains(t, err, "another row filter")
}

func TestClearPredicate(t *testing.T) {
	c := Chunking{Kind: ChunkingRange, Exprs: []string{"k"}, BoundType: "UInt64", Bounds: []string{"10"}, Leaves: 4}
	spec := DigestSpec{Ref: ref("d", "t"), KeyExprs: []string{"k"}, Chunking: c, Filter: "s = 'a'"}
	p := clearPredicate(&spec, "1", []uint32{2})
	assert.Equal(t, "(s = 'a') AND (((k) >= CAST('10' AS UInt64)) AND ((cityHash64(tuple(k))) % 4 IN (2)))", p)
	spec.Filter = ""
	assert.Equal(t, "(k) >= CAST('10' AS UInt64)", clearPredicate(&spec, "1", nil))
}

func plainTable(db string, name string) TableInfo {
	return mergeTree(ref(db, name), "k", col("k", "UInt64"), col("s", "String"))
}
