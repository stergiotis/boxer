package jackstay

import (
	"context"
	"io"
	"strings"
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
	p := Chunking{Kind: ChunkingPartition, Exprs: []string{"toYYYYMM(ts)"}}
	assert.Equal(t, "hex(formatRowNoNewline('RowBinary', toYYYYMM(ts)))", p.ChunkExpr())
	assert.Equal(t, "''", (&Chunking{}).ChunkExpr())
}

func TestCompareChunkIds(t *testing.T) {
	assert.Negative(t, compareChunkIds("9", "10"))
	assert.Positive(t, compareChunkIds("b", "a"))
	assert.Zero(t, compareChunkIds("10", "10"))
}

func TestUnmatched(t *testing.T) {
	ua, ub := unmatched([]uint64{1, 2, 2, 3}, []uint64{2, 3, 4})
	assert.Equal(t, uint64(2), ua, "one 1 and one 2")
	assert.Equal(t, uint64(1), ub, "the 4")
}

// fakeQuery answers the digest and pair queries with canned JSONEachRow bodies.
type fakeQuery struct {
	leaves string
	pairs  string
	seen   []string
}

func (inst *fakeQuery) Query(_ context.Context, sql string) (io.ReadCloser, error) {
	inst.seen = append(inst.seen, sql)
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
	d, err := DiffTable(context.Background(), src, dst, &spec, &spec, DefaultDiffOptions(), time.Unix(0, 0))
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
	d, err = DiffTable(context.Background(), src, dst, &spec, &spec, opts, time.Unix(0, 0))
	require.NoError(t, err)
	assert.Equal(t, uint64(1), d.UnresolvedLeaves)
	assert.Zero(t, d.Changed)
	assert.Len(t, src.seen, 1, "no pair query")
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
	fresh.CarryOver(&old)
	require.NotNil(t, fresh.Tables[0].Chunking)
	assert.Equal(t, *c, *fresh.Tables[0].Chunking)
	assert.Nil(t, fresh.Tables[0].Diff, "diffs are not carried")
	assert.Nil(t, fresh.Tables[1].Chunking, "a changed key drops the layout")
}
