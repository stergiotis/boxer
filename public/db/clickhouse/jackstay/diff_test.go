package jackstay

import (
	"context"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitKeyExprs(t *testing.T) {
	assert.Nil(t, SplitKeyExprs(""))
	assert.Nil(t, SplitKeyExprs("tuple()"))
	assert.Equal(t, []string{"k"}, SplitKeyExprs("k"))
	assert.Equal(t, []string{"`id:id:u64:47::0:`", "s"}, SplitKeyExprs("`id:id:u64:47::0:`, s"))
	assert.Equal(t, []string{"toStartOfDay(ts, 'UTC')", "`a,b`", "arrayJoin([1, 2])", "'x,y'"},
		SplitKeyExprs("toStartOfDay(ts, 'UTC'), `a,b`, arrayJoin([1, 2]), 'x,y'"))
}

func TestLeavesFor(t *testing.T) {
	opts := DefaultChunkingOptions()
	assert.Equal(t, uint32(1), leavesFor(0, opts))
	assert.Equal(t, uint32(1), leavesFor(1024, opts))
	assert.Equal(t, uint32(2), leavesFor(1025, opts))
	assert.Equal(t, uint32(1024), leavesFor(1_000_000, opts))
	assert.Equal(t, opts.MaxLeaves, leavesFor(1<<40, opts))

	// Leaves are sized for one chunk's share of the rows, not the whole table.
	assert.Equal(t, uint32(1024), leavesPerChunk(1_000_000, 1, opts))
	assert.Equal(t, uint32(128), leavesPerChunk(1_000_000, 8, opts))
	assert.Equal(t, uint32(1024), leavesPerChunk(1_000_000, 0, opts), "no active parts counts as one chunk")
	assert.Less(t, leavesPerChunk(9995, 2, opts), leavesFor(9995, opts))
}

func TestChunkingValidate(t *testing.T) {
	assert.NoError(t, (&Chunking{Kind: ChunkingSingle, Leaves: 1}).validate())
	assert.NoError(t, (&Chunking{Kind: ChunkingPartition, Exprs: []string{"p"}, Leaves: 64}).validate())
	assert.NoError(t, (&Chunking{Kind: ChunkingRange, Exprs: []string{"k"}, BoundType: "UInt64", Bounds: []string{"9", "10"}, Leaves: 2}).validate())
	for name, c := range map[string]Chunking{
		"zero leaves":     {Kind: ChunkingSingle},
		"odd leaves":      {Kind: ChunkingSingle, Leaves: 3},
		"bad kind":        {Kind: ChunkingKindE(9), Leaves: 1},
		"no partition":    {Kind: ChunkingPartition, Leaves: 1},
		"two range exprs": {Kind: ChunkingRange, Exprs: []string{"a", "b"}, BoundType: "UInt64", Bounds: []string{"1"}, Leaves: 1},
		"no bound type":   {Kind: ChunkingRange, Exprs: []string{"k"}, Bounds: []string{"1"}, Leaves: 1},
		"no bounds":       {Kind: ChunkingRange, Exprs: []string{"k"}, BoundType: "UInt64", Leaves: 1},
		"repeated bound":  {Kind: ChunkingRange, Exprs: []string{"k"}, BoundType: "UInt64", Bounds: []string{"1", "1"}, Leaves: 1},
	} {
		assert.Error(t, c.validate(), name)
	}
}

func TestRangeIndex(t *testing.T) {
	for id, want := range map[string]int{"0": 0, "1": 1, "10": 10, "2147483647": math.MaxInt32} {
		i, ok := rangeIndex(id)
		assert.True(t, ok, id)
		assert.Equal(t, want, i, id)
	}
	for _, id := range []string{"", "-1", "+1", "01", "1x", " 1", "1.0", "99999999999999999999", "FFFF"} {
		_, ok := rangeIndex(id)
		assert.False(t, ok, id)
	}
}

func TestBoundType(t *testing.T) {
	assert.Equal(t, "UInt64", boundType("UInt64"))
	assert.Equal(t, "String", boundType("LowCardinality(String)"))
	assert.Equal(t, "DateTime('UTC')", boundType("DateTime('Europe/Zurich')"))
	assert.Equal(t, "DateTime('UTC')", boundType("DateTime"))
	assert.Equal(t, "DateTime64(9, 'UTC')", boundType("DateTime64(9, 'Europe/Zurich')"))
	assert.Equal(t, "DateTime64(3, 'UTC')", boundType("DateTime64(3)"))
	assert.True(t, orderableType("LowCardinality(String)"))
	assert.True(t, orderableType("DateTime64(3)"))
	assert.False(t, orderableType("Nullable(UInt64)"))
	assert.False(t, orderableType("Array(UInt64)"))
}

func TestPickBounds(t *testing.T) {
	sample := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	assert.Equal(t, []string{"c", "e", "g"}, pickBounds(sample, 4))
	assert.Nil(t, pickBounds(sample, 1))
	assert.Equal(t, []string{"x"}, pickBounds([]string{"x", "x", "x", "x"}, 4), "repeats collapse")
}

func TestChunkingExprs(t *testing.T) {
	c := Chunking{Kind: ChunkingRange, Exprs: []string{"k"}, BoundType: "String", Bounds: []string{"b", "it's"}, Leaves: 8}
	assert.Equal(t, `toString(arrayCount(b -> b <= (k), [CAST('b' AS String), CAST('it\'s' AS String)]))`, c.ChunkExpr())
	assert.Equal(t, "[-∞, b)", c.RangeDisplay("0"))
	assert.Equal(t, "[b, it's)", c.RangeDisplay("1"))
	assert.Equal(t, "[it's, +∞)", c.RangeDisplay("2"))
	assert.Equal(t, "", c.RangeDisplay("x"))
	assert.Equal(t, "", c.RangeDisplay("01"))

	// Each chunk's predicate pairs the lower bound (>=) with the next one (<);
	// the first and last are open-ended.
	assert.Equal(t, `(k) < CAST('b' AS String)`, c.ChunkPredicate("0", ""))
	assert.Equal(t, `(k) >= CAST('b' AS String) AND (k) < CAST('it\'s' AS String)`, c.ChunkPredicate("1", ""))
	assert.Equal(t, `(k) >= CAST('it\'s' AS String)`, c.ChunkPredicate("2", ""))
	assert.Equal(t, "0", c.ChunkPredicate("3", ""), "past the last chunk selects nothing")
	assert.Equal(t, "0", c.ChunkPredicate("x", ""), "a non-numeric id selects nothing")
	assert.Equal(t, "0", c.ChunkPredicate("-1", ""))

	// A Float key: NaN lands in chunk 0 by ChunkExpr and is selected by chunk 0
	// alone.
	f := Chunking{Kind: ChunkingRange, Exprs: []string{"v"}, BoundType: "Float64", Bounds: []string{"1.5", "3"}, Leaves: 2}
	assert.Equal(t, `((v) < CAST('1.5' AS Float64) OR isNaN(v))`, f.ChunkPredicate("0", ""))
	assert.Equal(t, `(v) >= CAST('1.5' AS Float64) AND (v) < CAST('3' AS Float64)`, f.ChunkPredicate("1", ""))
	assert.Equal(t, `(v) >= CAST('3' AS Float64)`, f.ChunkPredicate("2", ""))

	p := Chunking{Kind: ChunkingPartition, Exprs: []string{"toYYYYMM(ts)"}}
	assert.Equal(t, "hex(formatRowNoNewline('RowBinary', toYYYYMM(ts)))", p.ChunkExpr())
	assert.Equal(t, "''", (&Chunking{}).ChunkExpr())
}

func TestCompareChunkIds(t *testing.T) {
	assert.Negative(t, compareChunkIds("9", "10"))
	assert.Positive(t, compareChunkIds("b", "a"))
	assert.Zero(t, compareChunkIds("10", "10"))
	assert.Negative(t, compareChunkIds("10", "9x"), "a non-index falls back to string order")
}

func TestChunkListQuery_PinsRowBinary(t *testing.T) {
	spec := DigestSpec{Ref: ref("d", "t"), KeyExprs: []string{"k"}, CopyColumns: []string{"k", "j"}, Chunking: Chunking{Kind: ChunkingPartition, Exprs: []string{"j"}, Leaves: 1}}
	for _, sql := range []string{spec.ChunkListQuery(), spec.LeafDigestQuery(), spec.PairQuery([]ChunkLeaf{{Chunk: "x"}})} {
		assert.True(t, strings.HasSuffix(sql, digestSettings), sql)
	}
}

func TestUnmatched(t *testing.T) {
	ua, ub := unmatched([]uint64{1, 2, 2, 3}, []uint64{2, 3, 4})
	assert.Equal(t, uint64(2), ua, "one 1 and one 2")
	assert.Equal(t, uint64(1), ub, "the 4")
}

// fakeQuery answers the digest and pair queries with canned JSONEachRow bodies.
// DiffTable queries both sides concurrently, and a test may pass one fake for
// both, so the log of seen queries is locked.
type fakeQuery struct {
	leaves string
	pairs  string
	mu     sync.Mutex
	seen   []string
}

func (inst *fakeQuery) Query(_ context.Context, sql string) (io.ReadCloser, error) {
	inst.mu.Lock()
	inst.seen = append(inst.seen, sql)
	inst.mu.Unlock()
	if strings.Contains(sql, "sumWithOverflow(kh)") {
		return io.NopCloser(strings.NewReader(inst.leaves)), nil
	}
	return io.NopCloser(strings.NewReader(inst.pairs)), nil
}

func TestDiffTable_Canned(t *testing.T) {
	src := &fakeQuery{
		leaves: `{"chunk":"a","display":"(1)","leaf":0,"n":2,"kd":10,"rd":20}
{"chunk":"a","display":"(1)","leaf":1,"n":1,"kd":5,"rd":6}
{"chunk":"b","display":"(2)","leaf":0,"n":7,"kd":1,"rd":1}
`,
		pairs: `{"chunk":"a","leaf":0,"kh":4,"rh":100,"keytext":"(4)"}
{"chunk":"a","leaf":0,"kh":6,"rh":101,"keytext":"(6)"}
`,
	}
	dst := &fakeQuery{
		leaves: `{"chunk":"a","display":"(1)","leaf":0,"n":2,"kd":10,"rd":21}
{"chunk":"a","display":"(1)","leaf":1,"n":1,"kd":5,"rd":6}
{"chunk":"c","display":"(3)","leaf":0,"n":3,"kd":1,"rd":1}
`,
		pairs: `{"chunk":"a","leaf":0,"kh":4,"rh":100,"keytext":"(4)"}
{"chunk":"a","leaf":0,"kh":6,"rh":999,"keytext":"(6)"}
`,
	}
	spec := DigestSpec{CopyColumns: []string{"k", "v"}, KeyExprs: []string{"k"}, Chunking: Chunking{Kind: ChunkingPartition, Exprs: []string{"p"}, Leaves: 2}}
	d, err := DiffTable(context.Background(), ServerSource(src), dst, &spec, &spec, DefaultDiffOptions(), time.Unix(0, 0))
	require.NoError(t, err)
	assert.Equal(t, uint64(3), d.Chunks)
	assert.Equal(t, uint64(0), d.IdenticalChunks)
	assert.Equal(t, uint64(10), d.SrcRows)
	assert.Equal(t, uint64(6), d.DstRows)
	assert.Equal(t, uint64(7), d.Missing, "chunk b absent on the target")
	assert.Equal(t, uint64(3), d.Extra, "chunk c absent on the source")
	assert.Equal(t, uint64(1), d.Changed, "key 6 changed in chunk a, leaf 0")
	assert.Zero(t, d.UnresolvedLeaves)
	require.Len(t, d.Differing, 3)
	assert.Equal(t, []LeafDiff{{Leaf: 0, SrcRows: 2, DstRows: 2, KeysDiffer: false, Resolved: true, Changed: 1}}, d.Differing[0].Leaves)
	assert.Equal(t, []RowExample{{Kind: "changed", Chunk: "a", Key: "(6)"}}, d.Examples)
	require.Len(t, src.seen, 2)
	assert.Contains(t, src.seen[1], "IN (('a', 0))")

	// Over the threshold: the leaf is reported but not compared row by row.
	src.seen, dst.seen = nil, nil
	opts := DefaultDiffOptions()
	opts.PairThreshold = 1
	d, err = DiffTable(context.Background(), ServerSource(src), dst, &spec, &spec, opts, time.Unix(0, 0))
	require.NoError(t, err)
	assert.Equal(t, uint64(1), d.UnresolvedLeaves)
	assert.Zero(t, d.Changed)
	assert.Len(t, src.seen, 1, "no pair query")

	// A leaf sent for pairing that neither side returns rows for (the table
	// moved between the scans) stays unresolved rather than reading as equal.
	src = &fakeQuery{leaves: `{"chunk":"a","display":"(1)","leaf":0,"n":2,"kd":10,"rd":20}` + "\n"}
	dst = &fakeQuery{leaves: `{"chunk":"a","display":"(1)","leaf":0,"n":2,"kd":10,"rd":21}` + "\n"}
	d, err = DiffTable(context.Background(), ServerSource(src), dst, &spec, &spec, DefaultDiffOptions(), time.Unix(0, 0))
	require.NoError(t, err)
	assert.Len(t, src.seen, 2, "the pair query ran")
	assert.Equal(t, uint64(1), d.UnresolvedLeaves)
	assert.Zero(t, d.Missing+d.Extra+d.Changed)
	require.Len(t, d.Differing, 1)
	require.Len(t, d.Differing[0].Leaves, 1)
	assert.False(t, d.Differing[0].Leaves[0].Resolved)
}

func TestDigestSpecs_Final(t *testing.T) {
	pt := PlanTable{Engine: "ReplacingMergeTree", TargetEngine: "MergeTree", SortingKey: "k"}
	s, d, spurious := pt.DigestSpecs(false)
	assert.False(t, s.Final)
	assert.True(t, spurious)
	s, d, spurious = pt.DigestSpecs(true)
	assert.True(t, s.Final)
	assert.False(t, d.Final, "FINAL only where the engine collapses rows")
	assert.False(t, spurious)
	assert.Contains(t, s.LeafDigestQuery(), " FINAL)")

	// The diff records that FINAL was asked for, even when only one side's
	// engine took it.
	empty := &fakeQuery{}
	td, err := DiffTable(context.Background(), ServerSource(empty), empty, &s, &d, DefaultDiffOptions(), time.Unix(0, 0))
	require.NoError(t, err)
	assert.True(t, td.Final)
}

func TestPlan_CarryOver(t *testing.T) {
	c := &Chunking{Kind: ChunkingRange, Exprs: []string{"k"}, BoundType: "UInt64", Bounds: []string{"5"}, Leaves: 4}
	old := Plan{Tables: []PlanTable{
		{Source: ref("a", "t"), SortingKey: "k", Chunking: c, Diff: &TableDiff{}},
		{Source: ref("a", "u"), SortingKey: "k", Chunking: c},
	}}
	fresh := Plan{Tables: []PlanTable{
		{Source: ref("a", "t"), SortingKey: "k"},
		{Source: ref("a", "u"), SortingKey: "k, s"},
	}}
	fresh.CarryOver(&old, false)
	require.NotNil(t, fresh.Tables[0].Chunking)
	assert.Equal(t, *c, *fresh.Tables[0].Chunking)
	assert.Nil(t, fresh.Tables[0].Diff, "diffs are not carried")
	assert.Nil(t, fresh.Tables[1].Chunking, "a changed key drops the layout")
}
