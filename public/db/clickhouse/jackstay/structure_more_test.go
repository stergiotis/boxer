package jackstay

import (
	"context"
	"encoding/json/v2"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
)

// Every branch of Restate's stale list, and the progress that is not stale.
func TestRestate_Branches(t *testing.T) {
	ops := newOps(t)
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	ep := Endpoint{URL: "http://a:8123/", User: "u"}
	src := inventory("26.8.1.1", []string{"app"},
		mergeTree(ref("app", "t1"), "k", col("k", "UInt64")),
		mergeTree(ref("app", "t2"), "k", col("k", "UInt64"), col("s", "String")))
	dst := inventory("26.8.1.1", []string{"app"}, mergeTree(ref("app", "t2"), "k", col("k", "UInt64")))
	plan, err := BuildPlan(ops, ep, Endpoint{URL: "http://b:8123/"}, src, dst, Selection{}, now)
	require.NoError(t, err)
	require.Equal(t, VerdictCreate, plan.Tables[0].Verdict)
	require.Equal(t, VerdictExtend, plan.Tables[1].Verdict)

	t.Run("new and vanished on the source", func(t *testing.T) {
		moved := inventory("26.8.1.1", []string{"app"},
			mergeTree(ref("app", "t2"), "k", col("k", "UInt64"), col("s", "String")),
			mergeTree(ref("app", "t3"), "k", col("k", "UInt64")))
		_, stale, err := Restate(ops, &plan, moved, dst, now)
		require.NoError(t, err)
		assert.Equal(t, []string{"app.t1: no longer on the source", "app.t3: new on the source"}, stale)
	})
	t.Run("the plan's own DDL ran", func(t *testing.T) {
		done := inventory("26.8.1.1", []string{"app"},
			mergeTree(ref("app", "t1"), "k", col("k", "UInt64")),
			mergeTree(ref("app", "t2"), "k", col("k", "UInt64"), col("s", "String"), col("extra", "UInt8")))
		fresh, stale, err := Restate(ops, &plan, src, done, now)
		require.NoError(t, err)
		assert.Empty(t, stale, "create → identical and extend → narrower are progress")
		assert.Equal(t, VerdictIdentical, fresh.Tables[0].Verdict)
		assert.Equal(t, VerdictNarrower, fresh.Tables[1].Verdict)
	})
	t.Run("a database the plan did not create", func(t *testing.T) {
		mapped := plan
		mapped.Selection = Selection{Databases: []string{"app"}, DatabaseMap: map[string]string{"app": "elsewhere"}}
		mapped.DatabaseDDL = nil
		_, stale, err := Restate(ops, &mapped, src, dst, now)
		require.NoError(t, err)
		assert.Contains(t, stale, "database DDL not in the plan: "+CreateDatabaseDDL("elsewhere"))
	})
	t.Run("DDL that grew", func(t *testing.T) {
		grown := inventory("26.8.1.1", []string{"app"},
			mergeTree(ref("app", "t1"), "k", col("k", "UInt64")),
			mergeTree(ref("app", "t2"), "k", col("k", "UInt64"), col("s", "String"), col("more", "String")))
		_, stale, err := Restate(ops, &plan, grown, dst, now)
		require.NoError(t, err)
		assert.Equal(t, []string{"app.t2: DDL differs"}, stale)
	})
}

// Two source tables mapped onto one target collide only when both stay in
// the plan; a table the leeway filter drops takes no target.
func TestBuildPlan_LeewayOnlyCollision(t *testing.T) {
	ops := newOps(t)
	now := time.Now()
	src := inventory("26.8.1.1", []string{"a", "b"},
		mergeTree(ref("a", "t"), "k", col("k", "UInt64")),
		mergeTree(ref("b", "t"), "k", leewayColumns(":")...))
	dst := inventory("26.8.1.1", nil)
	sel := Selection{Databases: []string{"a", "b"}, DatabaseMap: map[string]string{"a": "z", "b": "z"}}
	_, err := BuildPlan(ops, Endpoint{URL: "http://a/"}, Endpoint{URL: "http://b/"}, src, dst, sel, now)
	require.Error(t, err, "both tables land on z.t")
	sel.LeewayOnly = true
	plan, err := BuildPlan(ops, Endpoint{URL: "http://a/"}, Endpoint{URL: "http://b/"}, src, dst, sel, now)
	require.NoError(t, err)
	require.Len(t, plan.Tables, 1)
	assert.Equal(t, ref("b", "t"), plan.Tables[0].Source)
}

func TestJudge_TargetOnlyComputedColumn(t *testing.T) {
	ops := newOps(t)
	target := ref("d", "t")
	src := mergeTree(ref("s", "t"), "k", col("k", "UInt64"))
	dst := mergeTree(target, "k", col("k", "UInt64"), ColumnInfo{Name: "m", Type: "UInt64", DefaultKind: "MATERIALIZED", DefaultExpression: "k * 2"})
	v, err := Judge(ops, &src, &dst, target)
	require.NoError(t, err)
	assert.Equal(t, VerdictNarrower, v.Verdict, "a target-only column of any kind is an extra")
	require.Len(t, v.Notes, 1)
	assert.Contains(t, v.Notes[0], "m (MATERIALIZED)")
}

func TestJudge_NestedLowCardinality(t *testing.T) {
	ops := newOps(t)
	target := ref("d", "t")
	src := mergeTree(ref("s", "t"), "k", col("k", "UInt64"), col("tags", "Array(LowCardinality(String))"))
	dst := mergeTree(target, "k", col("k", "UInt64"), col("tags", "Array(String)"))
	v, err := Judge(ops, &src, &dst, target)
	require.NoError(t, err)
	assert.Equal(t, VerdictIdentical, v.Verdict)
	assert.Equal(t, []string{"k", "tags"}, v.CopyColumns)
	require.Len(t, v.Notes, 1)
	assert.Contains(t, v.Notes[0], "differs only in LowCardinality")

	// Nullable is a value difference, not a storage one.
	dst2 := mergeTree(target, "k", col("k", "UInt64"), col("tags", "Array(Nullable(String))"))
	v, err = Judge(ops, &src, &dst2, target)
	require.NoError(t, err)
	assert.Equal(t, VerdictIncompatible, v.Verdict)
}

func TestStripLowCardinalityDeep(t *testing.T) {
	assert.Equal(t, "String", stripLowCardinalityDeep("LowCardinality(String)"))
	assert.Equal(t, "Array(String)", stripLowCardinalityDeep("Array(LowCardinality(String))"))
	assert.Equal(t, "Map(String, Nullable(String))", stripLowCardinalityDeep("Map(LowCardinality(String), LowCardinality(Nullable(String)))"))
	assert.Equal(t, "UInt64", stripLowCardinalityDeep("UInt64"))
	assert.Equal(t, "LowCardinality(", stripLowCardinalityDeep("LowCardinality("), "unterminated input is left alone")
}

func TestJudge_CreateNotesBodyReferences(t *testing.T) {
	ops := newOps(t)
	src := mergeTree(ref("app", "t"), "k", col("k", "UInt64"))
	src.CreateQuery = "CREATE TABLE app.t (`k` UInt64, `n` String DEFAULT dictGet('app.names', 'n', k)) ENGINE = MergeTree ORDER BY k"
	v, err := Judge(ops, &src, nil, ref("copy", "t"))
	require.NoError(t, err)
	require.Equal(t, VerdictCreate, v.Verdict)
	require.Len(t, v.Notes, 1)
	assert.Contains(t, v.Notes[0], "still names the source database app")
	v, err = Judge(ops, &src, nil, ref("app", "t2"))
	require.NoError(t, err)
	assert.Empty(t, v.Notes, "no note when the database is not renamed")
}

func TestColumnDefinition_Comment(t *testing.T) {
	c := ColumnInfo{Name: "n", Type: "String", DefaultKind: "DEFAULT", DefaultExpression: "'x'", Comment: "it's a name", CompressionCodec: "CODEC(ZSTD(1))"}
	assert.Equal(t, "`n` String DEFAULT 'x' COMMENT 'it\\'s a name' CODEC(ZSTD(1))", ColumnDefinition(c))
}

func TestPlan_Validate(t *testing.T) {
	good := Plan{FormatVersion: PlanFormatVersion, Source: Endpoint{URL: "http://a/"}, Target: Endpoint{URL: "http://b/"},
		Tables: []PlanTable{{Source: ref("a", "t"), Target: ref("b", "t"),
			Chunking: &Chunking{Kind: ChunkingRange, Exprs: []string{"k"}, BoundType: "UInt64", Bounds: []string{"9", "10"}, Leaves: 4}}}}
	require.NoError(t, good.Validate(), "numeric bounds compare as numbers")

	bad := good
	bad.FormatVersion = 2
	assert.Error(t, bad.Validate())
	bad = good
	bad.Target = Endpoint{}
	assert.Error(t, bad.Validate())
	bad = good
	bad.Tables = []PlanTable{{Source: ref("a", "t")}}
	assert.Error(t, bad.Validate())
	bad = good
	bad.Tables = []PlanTable{{Source: ref("a", "t"), Target: ref("b", "t"),
		Chunking: &Chunking{Kind: ChunkingRange, Exprs: []string{"k"}, BoundType: "UInt64", Bounds: []string{"10", "9"}, Leaves: 4}}}
	assert.Error(t, bad.Validate(), "descending bounds")
	bad = good
	bad.Tables = []PlanTable{{Source: ref("a", "t"), Target: ref("b", "t"), Chunking: &Chunking{Kind: ChunkingSingle, Leaves: 3}}}
	assert.Error(t, bad.Validate(), "leaves not a power of two")
	bad = good
	bad.Tables = []PlanTable{{Source: ref("a", "t"), Target: ref("b", "t"), Sync: &TableSync{Mode: SyncModeSample, SampleNum: 3, SampleDen: 2}}}
	assert.Error(t, bad.Validate())

	// A file that decodes but does not validate is refused by LoadPlan.
	path := filepath.Join(t.TempDir(), "plan.json")
	require.NoError(t, bad.Save(path))
	_, err := LoadPlan(path)
	assert.Error(t, err)
}

// discoveryAnswers scripts the four discovery queries for one server.
func discoveryAnswers(uuid string, tables []TableInfo) (answer func(sql string) (string, error)) {
	return func(sql string) (string, error) {
		var b strings.Builder
		switch {
		case strings.Contains(sql, "serverUUID()"):
			return `{"uuid":"` + uuid + `","version":"26.8.1.1","uptime":1,"timezone":"UTC"}` + "\n", nil
		case strings.Contains(sql, "system.databases"):
			seen := map[string]bool{}
			for _, t := range tables {
				if !seen[t.Ref.Database] {
					seen[t.Ref.Database] = true
					b.WriteString(`{"name":"` + t.Ref.Database + `","engine":"Atomic"}` + "\n")
				}
			}
			return b.String(), nil
		case strings.Contains(sql, "system.tables"):
			for _, t := range tables {
				row := map[string]any{"database": t.Ref.Database, "name": t.Ref.Name, "engine": t.Engine, "sorting_key": t.SortingKey,
					"partition_key": t.PartitionKey, "total_rows": t.TotalRows, "total_bytes": t.TotalBytes, "create_table_query": t.CreateQuery}
				data, _ := json.Marshal(row)
				b.Write(data)
				b.WriteByte('\n')
			}
			return b.String(), nil
		case strings.Contains(sql, "system.columns"):
			for _, t := range tables {
				for _, c := range t.Columns {
					row := map[string]any{"database": t.Ref.Database, "table": t.Ref.Name, "name": c.Name, "type": c.Type, "position": c.Position,
						"default_kind": c.DefaultKind, "default_expression": c.DefaultExpression, "compression_codec": c.CompressionCodec, "comment": c.Comment}
					data, _ := json.Marshal(row)
					b.Write(data)
					b.WriteByte('\n')
				}
			}
			return b.String(), nil
		}
		return "", nil
	}
}

// Recheck carries the chunk layout, the diff and the run across an unchanged
// structure, and drops the diff once the copied columns change on both sides.
func TestRecheck_CarriesAndDrops(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	srcT := mergeTree(ref("app", "t"), "k", col("k", "UInt64"))
	dstT := mergeTree(ref("copy", "t"), "k", col("k", "UInt64"))
	src := &fakeClient{answer: discoveryAnswers("s", []TableInfo{srcT})}
	dst := &fakeClient{answer: discoveryAnswers("d", []TableInfo{dstT})}
	sel := Selection{Databases: []string{"app"}, DatabaseMap: map[string]string{"app": "copy"}}
	plan, err := PlanStructure(ctx, src, dst, Endpoint{URL: "http://s/"}, Endpoint{URL: "http://d/"}, sel, nil, now)
	require.NoError(t, err)
	require.Len(t, plan.Tables, 1)
	require.Equal(t, VerdictIdentical, plan.Tables[0].Verdict)
	plan.Tables[0].Chunking = &Chunking{Kind: ChunkingSingle, Leaves: 2}
	plan.Tables[0].Diff = &TableDiff{Chunks: 1, IdenticalChunks: 1}
	plan.SyncRun = &SyncRun{RunId: "run"}

	fresh, stale, err := Recheck(ctx, src, dst, &plan, now)
	require.NoError(t, err)
	assert.Empty(t, stale)
	require.NotNil(t, fresh.Tables[0].Chunking)
	assert.Equal(t, uint32(2), fresh.Tables[0].Chunking.Leaves)
	require.NotNil(t, fresh.Tables[0].Diff, "an unchanged structure keeps its diff")
	require.NotNil(t, fresh.SyncRun)
	assert.Equal(t, "run", fresh.SyncRun.RunId)

	// A column added on both sides is not stale, but the diff hashed fewer
	// columns than a sync would copy now.
	srcT2 := mergeTree(ref("app", "t"), "k", col("k", "UInt64"), col("v", "String"))
	dstT2 := mergeTree(ref("copy", "t"), "k", col("k", "UInt64"), col("v", "String"))
	src.answer = discoveryAnswers("s", []TableInfo{srcT2})
	dst.answer = discoveryAnswers("d", []TableInfo{dstT2})
	fresh, stale, err = Recheck(ctx, src, dst, &plan, now)
	require.NoError(t, err)
	assert.Empty(t, stale)
	require.NotNil(t, fresh.Tables[0].Chunking, "the keys are unchanged, so the layout stays")
	assert.Nil(t, fresh.Tables[0].Diff, "the diff is dropped with the column list it hashed")
}

func TestPrepareSync_SkipWording(t *testing.T) {
	plan := Plan{Tables: []PlanTable{
		{Source: ref("a", "v"), TableVerdict: TableVerdict{Verdict: VerdictUnsupported, Reasons: []string{"engine View holds no rows a sync can copy"}}},
		{Source: ref("a", "pending"), TableVerdict: TableVerdict{Verdict: VerdictCreate}},
	}}
	_, skipped, err := PrepareSync(context.Background(), nil, &plan, TableSync{Mode: SyncModeFull}, []datacatalog.TableRef{plan.Tables[0].Source, plan.Tables[1].Source}, DefaultChunkingOptions())
	require.NoError(t, err)
	assert.Equal(t, []string{
		"a.v: verdict unsupported (engine View holds no rows a sync can copy)",
		"a.pending: verdict create (apply the DDL first)",
	}, skipped)
}
