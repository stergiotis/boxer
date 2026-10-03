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
		"k > (":             "one expression",
		"k > 1 AND":         "does not parse",
		"1) UNION ALL SELECT 1 FROM other WHERE (1": "one expression",
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
	notes, err = ValidateFilter("toDate(ts, 'UTC') >= '2026-01-01'", cols)
	require.NoError(t, err)
	assert.Empty(t, notes, "a Date literal is read the same in any zone, and the zone is named")
}

// A filter is joined to other predicates as text, so a fragment that closes
// the probe's parentheses and opens new ones must not pass: joined to a chunk
// predicate it would select every row, and reach the target's DELETE.
func TestValidateFilter_OneExpression(t *testing.T) {
	cols := []string{"a", "s"}
	for _, f := range []string{
		"1) OR (1",
		"a = 1) OR (a > 0",
		"a = 1)) OR ((a > 0",
		"(a = 1",
		"a = 1)",
		"a = 1 -- )",
		"a = 1 /* ) */",
		"a = 1 # )",
		"s = ')",
	} {
		_, err := ValidateFilter(f, cols)
		if assert.Error(t, err, f) {
			assert.Contains(t, err.Error(), "one expression", f)
		}
	}
	for _, f := range []string{
		"s = ')'",
		"s = '('",
		"s = ') OR (1'",
		"s IN ('(', ')') AND (a > 1 OR a < 0)",
		"`a` > 0",
		`s = 'it\'s )'`,
		"s = 'it''s )'",
		"a - 1 > 0",
	} {
		_, err := ValidateFilter(f, cols)
		assert.NoError(t, err, f)
	}
	assert.Equal(t, "(s = ')') AND (k > 1)", andPredicates("s = ')'", "k > 1"))
}

// A lambda's parameter is a name in its body only.
func TestValidateFilter_LambdaScope(t *testing.T) {
	cols := []string{"a", "b"}
	_, err := ValidateFilter("arrayExists(x -> x > 0, b)", cols)
	require.NoError(t, err)
	_, err = ValidateFilter("arrayExists((x, y) -> x > y, b, b)", cols)
	require.NoError(t, err)
	_, err = ValidateFilter("x > 0 AND arrayExists(x -> x > 0, b)", cols)
	assert.ErrorContains(t, err, "not copied")
	_, err = ValidateFilter("arrayExists(x -> arrayExists(y -> y > x, b), b)", cols)
	assert.NoError(t, err, "an outer lambda's parameter is in scope in an inner body")
	_, err = ValidateFilter("arrayExists(x -> arrayExists(y -> y > 0, b), b) AND y > 0", cols)
	assert.ErrorContains(t, err, "not copied")
}

// Functions that read the server, the clock or the block, or multiply rows,
// select different rows on each side.
func TestValidateFilter_VolatileFunctions(t *testing.T) {
	cols := []string{"k", "arr"}
	for _, f := range []string{
		"arrayJoin(arr) > 1",
		"k = getServerPort('tcp_port')",
		"k = tcpPort()",
		"buildId() = ''",
		"displayName() = 'x'",
		"hostName() = 'x'",
		"fqdn() = 'x'",
		"k = shardNum()",
		"joinGetOrNull('j', 'v', k) = 1",
		"k < filesystemAvailable()",
		"k > toUnixTimestamp(current_timestamp())",
	} {
		_, err := ValidateFilter(f, cols)
		if assert.Error(t, err, f) {
			assert.Contains(t, err.Error(), "depends on when or where", f)
		}
	}
}

// A date function that names no timezone reads a DateTime in the server's.
func TestValidateFilter_ZoneNotes(t *testing.T) {
	cols := []string{"ts"}
	for f, noted := range map[string]bool{
		"toDate(ts) >= '2026-01-01'":                        true,
		"toDate(ts, 'UTC') >= '2026-01-01'":                 false,
		"toStartOfDay(ts) = toStartOfDay(ts)":               true,
		"toStartOfDay(ts, 'UTC') > '2026-01-01'":            false,
		"toStartOfInterval(ts, INTERVAL 1 HOUR) > 0":        true,
		"toStartOfInterval(ts, INTERVAL 1 HOUR, 'UTC') > 0": false,
		"formatDateTime(ts, '%Y') = '2026'":                 true,
		"toYYYYMM(ts) = 202609":                             true,
		"toUInt64(ts) > 0":                                  false,
	} {
		notes, err := ValidateFilter(f, cols)
		require.NoError(t, err, f)
		if noted {
			assert.Len(t, notes, 1, f)
		} else {
			assert.Empty(t, notes, f)
		}
	}
	notes, err := ValidateFilter("toDate(ts) = toDate(ts)", cols)
	require.NoError(t, err)
	assert.Len(t, notes, 1, "one note per function")
}

// A dotted name is a subcolumn or tuple element of a copied column, or a
// column whose own name holds the dot (Nested); a qualifier that is no
// copied column, or two qualifiers, are refused.
func TestValidateFilter_DottedNames(t *testing.T) {
	cols := []string{"tup", "n.x"}
	for _, f := range []string{"tup.a = 1", "n.x > 0", "`n.x` > 0", "tup.1 = 1"} {
		_, err := ValidateFilter(f, cols)
		assert.NoError(t, err, f)
	}
	for f, why := range map[string]string{
		"n.y > 0":                          "a dotted name must start with a copied column",
		"jackstay_filter_probe.tup = 1":    "a dotted name must start with a copied column",
		"db.jackstay_filter_probe.tup = 1": "unqualified",
	} {
		_, err := ValidateFilter(f, cols)
		if assert.Error(t, err, f) {
			assert.Contains(t, err.Error(), why, f)
		}
	}
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
