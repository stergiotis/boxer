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

func withCreate(t TableInfo, engine string, create string) TableInfo {
	t.Engine = engine
	t.CreateQuery = create
	return t
}

// A pack's filter comes from its manifest, which is as open to editing as a
// plan, so it is validated like the operator's.
func TestBuildPlan_PackFilterValidated(t *testing.T) {
	ops := newOps(t)
	dst := &Inventory{}
	for f, why := range map[string]string{
		"nope > 1": "not copied",
		"1) OR (1": "one expression",
	} {
		packed := plainTable("s", "t")
		packed.Filter = f
		src := &Inventory{Databases: []DatabaseInfo{{Name: "s"}}, Tables: []TableInfo{packed}}
		_, err := BuildPlan(ops, Endpoint{Pack: "/p"}, Endpoint{URL: "http://b/"}, src, dst, Selection{Databases: []string{"s"}}, time.Now())
		if assert.Error(t, err, f) {
			assert.Contains(t, err.Error(), "invalid filter", f)
			assert.Contains(t, err.Error(), why, f)
		}
	}
}

// A loaded plan's filters are checked again: a hand edit of both the
// selection and the table must not reach a DELETE.
func TestPlan_ValidateFilter(t *testing.T) {
	pt := *singleChunkTable("t", "t")
	pt.Filter = "1) OR (1"
	p := Plan{FormatVersion: PlanFormatVersion, Source: Endpoint{URL: "http://a/"}, Target: Endpoint{URL: "http://b/"},
		Selection: Selection{Filters: map[string]string{"s.t": pt.Filter}}, Tables: []PlanTable{pt}}
	assert.ErrorContains(t, p.Validate(), "invalid filter")
	p.Tables[0].Filter = "k > 1"
	p.Selection.Filters["s.t"] = "k > 1"
	assert.NoError(t, p.Validate())
}

// On one server, a table both written and read by the plan is refused.
func TestBuildPlan_SameServerChain(t *testing.T) {
	ops := newOps(t)
	ep := Endpoint{URL: "http://a/"}
	inv := inventory("26.8.1.1", []string{"a", "b"},
		mergeTree(ref("a", "t"), "k", col("k", "UInt64")),
		mergeTree(ref("b", "t"), "k", col("k", "UInt64")))
	sel := Selection{Databases: []string{"a", "b"}, DatabaseMap: map[string]string{"a": "b", "b": "c"}}
	_, err := BuildPlan(ops, ep, ep, inv, inv, sel, time.Now())
	assert.ErrorContains(t, err, "also a source")

	sel.DatabaseMap = map[string]string{"a": "b", "b": "a"}
	_, err = BuildPlan(ops, ep, ep, inv, inv, sel, time.Now())
	assert.ErrorContains(t, err, "also a source", "a swap is a chain too")

	sel.DatabaseMap = map[string]string{"a": "x", "b": "y"}
	_, err = BuildPlan(ops, ep, ep, inv, inv, sel, time.Now())
	assert.NoError(t, err)

	// Two servers: b.t on the target is another table than b.t on the source.
	sel.DatabaseMap = map[string]string{"a": "b", "b": "c"}
	_, err = BuildPlan(ops, ep, Endpoint{URL: "http://other/"}, inv, inventory("26.8.1.1", nil), sel, time.Now())
	assert.NoError(t, err)
}

// A filter key joins database and name with a dot; it is matched against the
// source's tables, so dotted names resolve and a collision is refused.
func TestBuildPlan_DottedFilterKeys(t *testing.T) {
	ops := newOps(t)
	ep, dstEp := Endpoint{URL: "http://a/"}, Endpoint{URL: "http://b/"}
	dotted := inventory("26.8.1.1", []string{"a.b"}, plainTable("a.b", "c"))
	plan, err := BuildPlan(ops, ep, dstEp, dotted, inventory("26.8.1.1", nil),
		Selection{Databases: []string{"a.b"}, Filters: map[string]string{"a.b.c": "k > 1"}}, time.Now())
	require.NoError(t, err, "the key is not cut at its first dot")
	require.Len(t, plan.Tables, 1)
	assert.Equal(t, "k > 1", plan.Tables[0].Filter)
	require.NoError(t, plan.Validate())

	both := inventory("26.8.1.1", []string{"a", "a.b"}, plainTable("a.b", "c"), plainTable("a", "b.c"))
	sel := Selection{Databases: []string{"a", "a.b"}, Filters: map[string]string{"a.b.c": "k > 1"}}
	_, err = BuildPlan(ops, ep, dstEp, both, inventory("26.8.1.1", nil), sel, time.Now())
	assert.ErrorContains(t, err, "names two selected tables")

	sel.Databases = []string{"a"}
	plan, err = BuildPlan(ops, ep, dstEp, both, inventory("26.8.1.1", nil), sel, time.Now())
	require.NoError(t, err, "only one of them is selected")
	require.Len(t, plan.Tables, 1)
	assert.Equal(t, ref("a", "b.c"), plan.Tables[0].Source)
	assert.Equal(t, "k > 1", plan.Tables[0].Filter)

	sel = Selection{Databases: []string{"a"}, Filters: map[string]string{"a.b.c": "k > 1"}}
	only := inventory("26.8.1.1", []string{"a", "a.b"}, plainTable("a.b", "c"))
	_, err = BuildPlan(ops, ep, dstEp, only, inventory("26.8.1.1", nil), sel, time.Now())
	assert.ErrorContains(t, err, "outside the selected databases")
}

// A Replicated table is created on the target only with a Keeper path of
// its own; the server stores the source's path with {database} and {table}
// expanded.
func TestJudge_ReplicatedCreate(t *testing.T) {
	ops := newOps(t)
	base := mergeTree(ref("app", "t"), "k", col("k", "UInt64"))
	for create, ok := range map[string]bool{
		"CREATE TABLE app.t (`k` UInt64) ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/app/t', '{replica}') ORDER BY k":  false,
		"CREATE TABLE app.t (`k` UInt64) ENGINE = ReplicatedMergeTree('/clickhouse/tables/{uuid}/{shard}', '{replica}') ORDER BY k": true,
		"CREATE TABLE app.t (`k` UInt64) ENGINE = ReplicatedMergeTree ORDER BY k":                                                   true,
		"CREATE TABLE app.t (`k` UInt64) ENGINE = ReplicatedMergeTree() ORDER BY k":                                                 true,
		"CREATE TABLE app.t (`k` UInt64) ENGINE = ReplicatedReplacingMergeTree('/clickhouse/tables/app/t', 'r1', ver) ORDER BY k":   false,
	} {
		engine := strings.Fields(create[strings.Index(create, "ENGINE = ")+len("ENGINE = "):])[0]
		engine, _, _ = strings.Cut(engine, "(")
		src := withCreate(base, engine, create)
		v, err := Judge(ops, &src, nil, ref("copy", "t"))
		require.NoError(t, err, create)
		if ok {
			assert.Equal(t, VerdictCreate, v.Verdict, create)
		} else {
			assert.Equal(t, VerdictIncompatible, v.Verdict, create)
			if assert.Len(t, v.Reasons, 1, create) {
				assert.Contains(t, v.Reasons[0], "replication group", create)
			}
			assert.Empty(t, v.DDL, create)
		}
	}
}

// Materialized views reading the target table get a note: inserts fire
// them and a clear does not reverse them.
func TestJudge_TargetDependents(t *testing.T) {
	ops := newOps(t)
	src := mergeTree(ref("app", "t"), "k", col("k", "UInt64"))
	dst := mergeTree(ref("copy", "t"), "k", col("k", "UInt64"))
	v, err := Judge(ops, &src, &dst, dst.Ref)
	require.NoError(t, err)
	assert.Empty(t, v.Notes)
	dst.Dependents = []datacatalog.TableRef{ref("copy", "mv")}
	v, err = Judge(ops, &src, &dst, dst.Ref)
	require.NoError(t, err)
	assert.Equal(t, VerdictIdentical, v.Verdict, "a note, not a refusal")
	require.Len(t, v.Notes, 1)
	assert.Contains(t, v.Notes[0], "copy.mv")
}

func TestDiscover_DependentsAndVanishedColumns(t *testing.T) {
	tables := []TableInfo{mergeTree(ref("app", "t"), "k", col("k", "UInt64"))}
	base := discoveryAnswers("u", tables)
	q := &fakeClient{answer: func(sql string) (string, error) {
		if strings.Contains(sql, "system.tables") {
			assert.Contains(t, sql, "dependencies_database, dependencies_table")
			return `{"database":"app","name":"t","engine":"MergeTree","sorting_key":"k","partition_key":"","total_rows":0,"total_bytes":0,` +
				`"create_table_query":"CREATE TABLE app.t (k UInt64) ENGINE = MergeTree ORDER BY k","dependencies_database":["app"],"dependencies_table":["mv"]}` + "\n" +
				`{"database":"app","name":"gone","engine":"MergeTree","sorting_key":"k","partition_key":"","total_rows":0,"total_bytes":0,` +
				`"create_table_query":"CREATE TABLE app.gone (k UInt64) ENGINE = MergeTree ORDER BY k","dependencies_database":[],"dependencies_table":[]}` + "\n", nil
		}
		return base(sql)
	}}
	inv, err := Discover(context.Background(), q)
	require.NoError(t, err)
	tbl, has := inv.Table(ref("app", "t"))
	require.True(t, has)
	assert.Equal(t, []datacatalog.TableRef{ref("app", "mv")}, tbl.Dependents)

	gone, has := inv.Table(ref("app", "gone"))
	require.True(t, has)
	assert.Empty(t, gone.Columns)
	ops := newOps(t)
	v, err := Judge(ops, gone, nil, ref("copy", "gone"))
	require.NoError(t, err)
	assert.Equal(t, VerdictIncompatible, v.Verdict, "a table without columns is not created")
	assert.Empty(t, v.CopyColumns)
	v, err = Judge(ops, tbl, gone, ref("app", "gone"))
	require.NoError(t, err)
	assert.Equal(t, VerdictIncompatible, v.Verdict, "nor extended into")
	assert.Empty(t, v.DDL)
}

// The ADD COLUMN note is about the added column's own clauses, not the
// table's TTL.
func TestJudge_AddedColumnClauses(t *testing.T) {
	ops := newOps(t)
	dst := mergeTree(ref("copy", "t"), "d", col("d", "Date"))
	cases := []struct {
		create string
		want   []string
	}{
		{"CREATE TABLE app.t (`d` Date, `v` String) ENGINE = MergeTree ORDER BY d TTL d + toIntervalDay(1) SETTINGS index_granularity = 8192", nil},
		{"CREATE TABLE app.t (`d` Date, `v` String TTL d + toIntervalDay(1)) ENGINE = MergeTree ORDER BY d", []string{"TTL"}},
		{"CREATE TABLE app.t (`d` Date, `v` String STATISTICS(tdigest) SETTINGS (max_compress_block_size = 1)) ENGINE = MergeTree ORDER BY d", []string{"STATISTICS", "SETTINGS"}},
		{"CREATE TABLE app.t (`d` Date, `v` String DEFAULT ' TTL ') ENGINE = MergeTree ORDER BY d", nil},
		{"CREATE TABLE app.t (`d` Date TTL d + toIntervalDay(1), `v` String) ENGINE = MergeTree ORDER BY d", nil},
	}
	for _, c := range cases {
		src := mergeTree(ref("app", "t"), "d", col("d", "Date"), col("v", "String"))
		src.CreateQuery = c.create
		v, err := Judge(ops, &src, &dst, dst.Ref)
		require.NoError(t, err, c.create)
		require.Equal(t, VerdictExtend, v.Verdict, c.create)
		if c.want == nil {
			assert.Empty(t, v.Notes, c.create)
			continue
		}
		require.Len(t, v.Notes, 1, c.create)
		assert.Contains(t, v.Notes[0], "column v declares "+strings.Join(c.want, ", "), c.create)
	}
}

// Engines that collapse rows on merge are compared with their parameters:
// a version or sign column of another name collapses other rows.
func TestJudge_MergeParameters(t *testing.T) {
	ops := newOps(t)
	cols := func() []ColumnInfo {
		return []ColumnInfo{col("k", "UInt64"), col("ver", "UInt64"), col("other", "UInt64")}
	}
	mk := func(r datacatalog.TableRef, engine string, clause string) TableInfo {
		t := mergeTree(r, "k", cols()...)
		t.Engine = engine
		t.CreateQuery = "CREATE TABLE " + r.Database + "." + r.Name + " (`k` UInt64, `ver` UInt64, `other` UInt64) ENGINE = " + clause + " ORDER BY k"
		return t
	}
	cases := []struct {
		src, dst TableInfo
		noted    bool
	}{
		{mk(ref("a", "t"), "ReplacingMergeTree", "ReplacingMergeTree(ver)"), mk(ref("b", "t"), "ReplacingMergeTree", "ReplacingMergeTree(other)"), true},
		{mk(ref("a", "t"), "ReplacingMergeTree", "ReplacingMergeTree(ver)"), mk(ref("b", "t"), "ReplacingMergeTree", "ReplacingMergeTree(ver)"), false},
		{mk(ref("a", "t"), "ReplacingMergeTree", "ReplacingMergeTree"), mk(ref("b", "t"), "ReplacingMergeTree", "ReplacingMergeTree(ver)"), true},
		{mk(ref("a", "t"), "CollapsingMergeTree", "CollapsingMergeTree(ver)"), mk(ref("b", "t"), "CollapsingMergeTree", "CollapsingMergeTree(other)"), true},
		{mk(ref("a", "t"), "ReplicatedReplacingMergeTree", "ReplicatedReplacingMergeTree('/p/{uuid}', '{replica}', ver)"),
			mk(ref("b", "t"), "ReplacingMergeTree", "ReplacingMergeTree(ver)"), false},
		{mk(ref("a", "t"), "ReplicatedReplacingMergeTree", "ReplicatedReplacingMergeTree('/p/{uuid}', '{replica}', ver)"),
			mk(ref("b", "t"), "ReplacingMergeTree", "ReplacingMergeTree(other)"), true},
		{mk(ref("a", "t"), "MergeTree", "MergeTree"), mk(ref("b", "t"), "MergeTree", "MergeTree"), false},
	}
	for _, c := range cases {
		v, err := Judge(ops, &c.src, &c.dst, c.dst.Ref)
		require.NoError(t, err, c.src.CreateQuery)
		assert.Equal(t, VerdictIdentical, v.Verdict, c.src.CreateQuery)
		has := false
		for _, n := range v.Notes {
			if strings.Contains(n, "merge parameters differ") {
				has = true
			}
		}
		assert.Equal(t, c.noted, has, "%s vs %s: %v", c.src.CreateQuery, c.dst.CreateQuery, v.Notes)
	}
}

func TestEngineArgs(t *testing.T) {
	q := "CREATE TABLE a.t (`k` UInt64, `s` String COMMENT ' ENGINE = X(1)') ENGINE = ReplicatedSummingMergeTree('/p/{uuid}', 'r', (a, b)) ORDER BY k"
	args, ok := engineArgs(q, "ReplicatedSummingMergeTree")
	require.True(t, ok)
	assert.Equal(t, []string{"'/p/{uuid}'", "'r'", "(a, b)"}, args)
	_, ok = engineArgs(q, "SummingMergeTree")
	assert.False(t, ok, "the engine name is matched whole")
	_, ok = engineArgs(q, "X")
	assert.False(t, ok, "the column list is not searched")
	args, ok = engineArgs("CREATE TABLE a.t (`k` UInt64) ENGINE = Log", "Log")
	require.True(t, ok)
	assert.Empty(t, args)
}

// Applying DDL drops the diff and sync state of the tables it altered only;
// the others, and the sync run, keep theirs.
func TestApplyDDLStep_KeepsUnalteredDiffs(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	t1 := mergeTree(ref("app", "t1"), "k", col("k", "UInt64"))
	t2 := mergeTree(ref("app", "t2"), "k", col("k", "UInt64"))
	src := &fakeClient{answer: discoveryAnswers("s", []TableInfo{t1, t2})}
	before := discoveryAnswers("d", []TableInfo{mergeTree(ref("copy", "t1"), "k", col("k", "UInt64"))})
	after := discoveryAnswers("d", []TableInfo{mergeTree(ref("copy", "t1"), "k", col("k", "UInt64")), mergeTree(ref("copy", "t2"), "k", col("k", "UInt64"))})
	dst := &fakeClient{}
	dst.answer = func(sql string) (string, error) {
		if len(dst.execs) > 0 {
			return after(sql)
		}
		return before(sql)
	}
	sel := Selection{Databases: []string{"app"}, DatabaseMap: map[string]string{"app": "copy"}}
	plan, err := PlanStructure(ctx, ServerSource(src), dst, Endpoint{URL: "http://s/"}, Endpoint{URL: "http://d/"}, sel, nil, now)
	require.NoError(t, err)
	require.Len(t, plan.Tables, 2)
	require.Equal(t, VerdictIdentical, plan.Tables[0].Verdict)
	require.Equal(t, VerdictCreate, plan.Tables[1].Verdict)
	for i := range plan.Tables {
		plan.Tables[i].Chunking = &Chunking{Kind: ChunkingSingle, Leaves: 2}
		plan.Tables[i].Diff = &TableDiff{Chunks: 1}
		plan.Tables[i].Sync = &TableSync{Mode: SyncModeFull}
		plan.Tables[i].SyncReport = &TableSyncReport{}
	}
	plan.SyncRun = &SyncRun{RunId: "run"}

	applied, got, stale, err := ApplyDDLStep(ctx, ServerSource(src), dst, dst, &plan, now)
	require.NoError(t, err)
	require.Empty(t, stale)
	require.Len(t, applied, 1)
	require.Len(t, got.Tables, 2)
	assert.Equal(t, VerdictIdentical, got.Tables[1].Verdict)
	assert.NotNil(t, got.Tables[0].Diff, "no DDL touched t1")
	assert.NotNil(t, got.Tables[0].Sync)
	assert.NotNil(t, got.Tables[0].SyncReport)
	assert.Nil(t, got.Tables[1].Diff, "t2 was created")
	assert.Nil(t, got.Tables[1].Sync)
	assert.Nil(t, got.Tables[1].SyncReport)
	assert.NotNil(t, got.Tables[1].Chunking, "the layout depends on the source keys only")
	require.NotNil(t, got.SyncRun)
	assert.Equal(t, "run", got.SyncRun.RunId)
}

// A sampled export's filter hashes the sorting key, whose columns need not
// be copied (a MATERIALIZED key column); the pack's filter may name them, an
// operator's may not.
func TestPackFilterMayNameSortingKeyColumns(t *testing.T) {
	sample := "(a > 1) AND (cityHash64('jackstay-sample', tuple(m, toDate(ts))) % 100 < 3)"
	_, err := ValidateFilter(sample, []string{"a", "ts"})
	require.Error(t, err, "an operator's filter names copied columns only")
	cols := packFilterColumns([]string{"a", "ts"}, "m, toDate(ts)")
	assert.ElementsMatch(t, []string{"a", "ts", "m"}, cols)
	_, err = ValidateFilter(sample, cols)
	require.NoError(t, err)
	_, err = ValidateFilter("1) OR (1", cols)
	require.Error(t, err, "the structural checks still hold")
}

// A plan whose range layout this version cannot order still opens, without
// the layout and the comparison over it.
func TestAPlanWithAnUnorderedRangeLayoutOpens(t *testing.T) {
	p := Plan{FormatVersion: PlanFormatVersion, Source: Endpoint{URL: "http://a.example:8123/"}, Target: Endpoint{URL: "http://b.example:8123/"}, Tables: []PlanTable{{
		Source:   datacatalog.TableRef{Database: "d", Name: "t"},
		Target:   datacatalog.TableRef{Database: "d", Name: "t"},
		Chunking: &Chunking{Kind: ChunkingRange, Exprs: []string{"id"}, BoundType: "UUID", Bounds: []string{"00000000-0000-0000-0000-000000000001"}, Leaves: 4},
		Diff:     &TableDiff{},
	}}}
	data, err := p.Marshal()
	require.NoError(t, err)
	got, err := ParsePlan(data)
	require.NoError(t, err)
	assert.Nil(t, got.Tables[0].Chunking)
	assert.Nil(t, got.Tables[0].Diff)
	require.Len(t, got.Notes, 1)
	assert.Contains(t, got.Notes[0], "d.t")
}
