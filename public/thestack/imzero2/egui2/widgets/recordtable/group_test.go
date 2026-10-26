package recordtable

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groupRec is the fixture every layout test reads: an entity id, a nullable
// path ("" is null), a nullable value (NaN is null) — the shape of an
// exploded leeway record, one row per attribute.
func groupRec(t *testing.T, ents []int64, paths []string, vals []float64) arrow.RecordBatch {
	t.Helper()
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "ent", Type: arrow.PrimitiveTypes.Int64},
		{Name: "path", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "val", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	}, nil)
	eb, pb, vb := array.NewInt64Builder(mem), array.NewStringBuilder(mem), array.NewFloat64Builder(mem)
	for i := range ents {
		eb.Append(ents[i])
		if paths[i] == "" {
			pb.AppendNull()
		} else {
			pb.Append(paths[i])
		}
		if vals[i] != vals[i] { // NaN
			vb.AppendNull()
		} else {
			vb.Append(vals[i])
		}
	}
	ea, pa, va := eb.NewArray(), pb.NewArray(), vb.NewArray()
	rec := array.NewRecordBatch(schema, []arrow.Array{ea, pa, va}, int64(len(ents)))
	for _, r := range []interface{ Release() }{eb, pb, vb, ea, pa, va} {
		r.Release()
	}
	return rec
}

const (
	colEnt  = 0
	colPath = 1
	colVal  = 2
)

var null = func() float64 { var z float64; return z / z }()

// segments splits the path column on "/", skipping the leading empty step:
// a stand-in for play's jsonshred segmenter.
func segments(rec arrow.RecordBatch) func(row int64) []Key {
	arr := rec.Column(colPath).(*array.String)
	return func(row int64) (segs []Key) {
		if arr.IsNull(int(row)) {
			return nil
		}
		for _, s := range strings.Split(strings.TrimPrefix(arr.Value(int(row)), "/"), "/") {
			segs = append(segs, Key{Value: s})
		}
		return
	}
}

// aggCols declares val summed and counted, the way a host would.
func aggCols(agg AggE) []Column {
	return []Column{{Arrow: colEnt}, {Arrow: colPath}, {Arrow: colVal, Agg: agg}}
}

// draw renders the lines as text, one per line, for assertions that read
// like the screen: a group line is `>name` or `vname` by its state,
// indented by depth; a leaf is its record row number.
func draw(lay *Layout) string {
	var b strings.Builder
	for i, ln := range lay.Lines {
		if i > 0 {
			b.WriteByte(' ')
		}
		if ln.IsGroup() {
			g := lay.Groups[ln.Group]
			b.WriteString(strings.Repeat(".", int(g.Depth)))
			if g.Open {
				b.WriteByte('v')
			} else {
				b.WriteByte('>')
			}
			if g.Key.Null {
				b.WriteString("~")
			} else {
				b.WriteString(g.Key.Value)
			}
			continue
		}
		b.WriteString(strings.Repeat(".", int(lay.Groups[ln.Group].Depth)+1))
		b.WriteString(itoa(ln.Row))
	}
	return b.String()
}

func itoa(n int64) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+n%10)))
}

// The zero state groups nothing: the lines are the leaves in order under
// the root, and every query is the identity.
func TestGroupZeroStateIsFlat(t *testing.T) {
	rec := groupRec(t, []int64{1, 1, 2}, []string{"/a", "/b", "/a"}, []float64{1, 2, 3})
	defer rec.Release()
	var st GroupState
	assert.False(t, st.Active())
	lay, err := st.LayoutFor(rec, nil, rec.NumRows(), aggCols(AggNone))
	require.NoError(t, err)
	assert.Equal(t, "0 1 2", draw(lay))
	assert.Len(t, lay.Groups, 1, "the root alone")
	assert.Equal(t, int64(3), lay.Groups[0].Leaves)
	assert.Equal(t, int64(1), lay.PosOf(1))
	assert.Equal(t, int32(-1), lay.GroupAt(1))
	assert.Equal(t, int64(-1), lay.PosOf(7), "a row the record lacks")
	_, ok := lay.Total(colVal)
	assert.False(t, ok, "no aggregate declared")
}

// A column level makes one group per distinct key, ordered by the column's
// type, the null group last; leaves keep the incoming permutation within
// their group, so a header sort refines rather than reorders.
func TestGroupColumnLevel(t *testing.T) {
	rec := groupRec(t, []int64{10, 2, 10, 2, 10}, []string{"/x", "/y", "", "/z", "/w"}, []float64{1, 2, 3, 4, 5})
	defer rec.Release()
	var st GroupState
	st.SetLevels([]GroupLevel{{Arrow: colEnt}})
	assert.True(t, st.Active())
	lay, err := st.LayoutFor(rec, nil, rec.NumRows(), aggCols(AggNone))
	require.NoError(t, err)
	// Typed: 2 before 10, which a string compare would reverse.
	assert.Equal(t, "v2 .1 .3 v10 .0 .2 .4", draw(lay))
	assert.Equal(t, int64(3), lay.Groups[lay.GroupAt(3)].Leaves)
	assert.Equal(t, int64(4), lay.PosOf(0))
	assert.Equal(t, int32(0), lay.Groups[1].Depth)
	assert.Equal(t, int32(0), lay.Groups[1].Parent, "a root-level group's parent is the root")

	// A permutation reorders leaves within a group only.
	order := []int64{4, 3, 2, 1, 0}
	lay, err = st.LayoutFor(rec, order, 5, aggCols(AggNone))
	require.NoError(t, err)
	assert.Equal(t, "v2 .3 .1 v10 .4 .2 .0", draw(lay))

	// The null group sits at the edge, last by default and first on ask.
	nulls := groupRec(t, []int64{1, 1, 2}, []string{"", "/a", "/b"}, []float64{1, 2, 3})
	defer nulls.Release()
	st.SetLevels([]GroupLevel{{Arrow: colPath}})
	lay, err = st.LayoutFor(nulls, nil, 3, aggCols(AggNone))
	require.NoError(t, err)
	assert.Equal(t, "v/a .1 v/b .2 v~ .0", draw(lay))
	st.SetOrder(0, GroupOrder{NullsFirst: true, Desc: true})
	lay, err = st.LayoutFor(nulls, nil, 3, aggCols(AggNone))
	require.NoError(t, err)
	assert.Equal(t, "v~ .0 v/b .2 v/a .1", draw(lay), "Desc reverses the keys, not the null edge")
}

// Two column levels nest; a path differs by level, so the same key under two
// parents is two groups.
func TestGroupTwoLevelsNest(t *testing.T) {
	rec := groupRec(t, []int64{1, 2, 1, 2}, []string{"/a", "/a", "/b", "/a"}, []float64{1, 2, 3, 4})
	defer rec.Release()
	var st GroupState
	st.SetLevels([]GroupLevel{{Arrow: colEnt}, {Arrow: colPath}})
	lay, err := st.LayoutFor(rec, nil, 4, aggCols(AggNone))
	require.NoError(t, err)
	assert.Equal(t, "v1 .v/a ..0 .v/b ..2 v2 .v/a ..1 ..3", draw(lay))
	a1, a2 := lay.Groups[lay.GroupAt(1)], lay.Groups[lay.GroupAt(6)]
	assert.Equal(t, a1.Key, a2.Key)
	assert.NotEqual(t, a1.Path, a2.Path)
	assert.Equal(t, int32(1), a1.Depth)
	assert.Equal(t, lay.GroupAt(0), a1.Parent)
}

// Orders: by first appearance, and by an aggregate, in both directions.
func TestGroupOrders(t *testing.T) {
	rec := groupRec(t, []int64{3, 1, 2, 1}, []string{"/a", "/a", "/a", "/a"}, []float64{5, 1, 10, 2})
	defer rec.Release()
	var st GroupState
	st.SetLevels([]GroupLevel{{Arrow: colEnt, Order: GroupOrder{Appearance: true}}})
	lay, err := st.LayoutFor(rec, nil, 4, aggCols(AggSum))
	require.NoError(t, err)
	assert.Equal(t, "v3 .0 v1 .1 .3 v2 .2", draw(lay))

	st.SetOrder(0, GroupOrder{ByAgg: true, AggColumn: colVal})
	lay, err = st.LayoutFor(rec, nil, 4, aggCols(AggSum))
	require.NoError(t, err)
	assert.Equal(t, "v1 .1 .3 v3 .0 v2 .2", draw(lay), "sums 3, 5, 10")

	st.SetOrder(0, GroupOrder{ByAgg: true, AggColumn: colVal, Desc: true})
	lay, err = st.LayoutFor(rec, nil, 4, aggCols(AggSum))
	require.NoError(t, err)
	assert.Equal(t, "v2 .2 v3 .0 v1 .1 .3", draw(lay))

	st.ToggleDesc(0)
	lay, err = st.LayoutFor(rec, nil, 4, aggCols(AggSum))
	require.NoError(t, err)
	assert.Equal(t, "v1 .1 .3 v3 .0 v2 .2", draw(lay))

	// Ordering by an aggregate the column does not declare is an error the
	// grid draws, not a silent fallback.
	_, err = st.LayoutFor(rec, nil, 4, aggCols(AggNone))
	assert.Error(t, err)
	st.SetOrder(0, GroupOrder{ByAgg: true, AggColumn: 9})
	_, err = st.LayoutFor(rec, nil, 4, aggCols(AggSum))
	assert.Error(t, err)
	st.SetLevels([]GroupLevel{{Arrow: 9}})
	_, err = st.LayoutFor(rec, nil, 4, aggCols(AggNone))
	assert.Error(t, err, "a level on a column the record lacks")
}

// Aggregates fold the leaves and merge upward: a parent's mean is the mean
// of all its leaves, never an average of its children's averages; nulls
// count as leaves and nothing else.
func TestGroupAggregatesMergeUpward(t *testing.T) {
	// ent 1: values 1, 2, 3 (mean 2); ent 2: value 10 and a null (mean 10).
	// The root mean is 16/4 = 4, not (2+10)/2 = 6.
	rec := groupRec(t, []int64{1, 1, 1, 2, 2}, []string{"/a", "/a", "/b", "/a", "/b"}, []float64{1, 2, 3, 10, null})
	defer rec.Release()
	var st GroupState
	st.SetLevels([]GroupLevel{{Arrow: colEnt}, {Arrow: colPath}})
	lay, err := st.LayoutFor(rec, nil, 5, aggCols(AggMean))
	require.NoError(t, err)

	total, ok := lay.Total(colVal)
	require.True(t, ok)
	assert.Equal(t, int64(5), total.Count)
	assert.Equal(t, int64(4), total.Num)
	mean, ok := total.Mean()
	require.True(t, ok)
	assert.Equal(t, 4.0, mean)
	assert.Equal(t, 1.0, total.Min)
	assert.Equal(t, 10.0, total.Max)
	assert.Equal(t, 16.0, total.Sum)

	ent2 := lay.Groups[lay.GroupAt(lay.PosOf(3)-2)] // the group line two above leaf 3: "v2 .v/a ..3"
	assert.Equal(t, "2", ent2.Key.Value)
	v, ok := lay.Aggregate(lay.GroupAt(lay.PosOf(3)-2), colVal)
	require.True(t, ok)
	assert.Equal(t, int64(2), v.Count)
	assert.Equal(t, int64(1), v.Num)
	m, _ := v.Mean()
	assert.Equal(t, 10.0, m)

	// A group of nulls only: a count, no number.
	nb := lay.GroupAt(lay.PosOf(4) - 1)
	v, ok = lay.Aggregate(nb, colVal)
	require.True(t, ok)
	assert.Equal(t, int64(1), v.Count)
	_, ok = v.Mean()
	assert.False(t, ok)
	_, ok = v.Of(AggSum)
	assert.False(t, ok)
	c, ok := v.Of(AggCount)
	assert.True(t, ok)
	assert.Equal(t, 1.0, c)

	_, ok = lay.Aggregate(0, colEnt)
	assert.False(t, ok, "a column without an Agg has no accumulator")
}

// Expansion: all open by default; a collapse default closes groups at a
// depth; an override survives a new record with the same keys; an override
// that agrees with the default is dropped; Reveal opens a row's ancestors.
func TestGroupExpansion(t *testing.T) {
	rec := groupRec(t, []int64{1, 1, 2}, []string{"/a", "/b", "/a"}, []float64{1, 2, 3})
	defer rec.Release()
	var st GroupState
	st.SetLevels([]GroupLevel{{Arrow: colEnt}, {Arrow: colPath}})
	cols := aggCols(AggNone)
	lay, err := st.LayoutFor(rec, nil, 3, cols)
	require.NoError(t, err)
	assert.Equal(t, "v1 .v/a ..0 .v/b ..1 v2 .v/a ..2", draw(lay))
	assert.Equal(t, int64(-1), lay.Revealed)

	g1 := lay.Groups[lay.GroupAt(0)]
	gen := st.Gen()
	assert.False(t, st.Toggle(g1))
	assert.NotEqual(t, gen, st.Gen())
	lay, err = st.LayoutFor(rec, nil, 3, cols)
	require.NoError(t, err)
	assert.Equal(t, ">1 v2 .v/a ..2", draw(lay))
	assert.Equal(t, int64(-1), lay.PosOf(0), "folded away")
	assert.Equal(t, int64(-1), lay.Groups[lay.GroupAt(0)+1].Line, "a hidden group has no line")
	assert.Equal(t, int64(3), lay.PosOf(2))

	// The same keys in a new record keep the closed group closed.
	rec2 := groupRec(t, []int64{2, 1}, []string{"/q", "/q"}, []float64{1, 2})
	defer rec2.Release()
	lay, err = st.LayoutFor(rec2, nil, 2, cols)
	require.NoError(t, err)
	assert.Equal(t, ">1 v2 .v/q ..0", draw(lay))

	// Opening it again agrees with the default and drops the entry.
	st.SetOpen(lay.Groups[lay.GroupAt(0)], true)
	assert.Empty(t, st.expanded)

	// A collapse default closes depth 1 and deeper; an override opens one.
	st.CollapseBelow(0)
	lay, err = st.LayoutFor(rec, nil, 3, cols)
	require.NoError(t, err)
	assert.Equal(t, "v1 .>/a .>/b v2 .>/a", draw(lay))
	st.SetOpen(lay.Groups[lay.GroupAt(1)], true)
	lay, err = st.LayoutFor(rec, nil, 3, cols)
	require.NoError(t, err)
	assert.Equal(t, "v1 .v/a ..0 .>/b v2 .>/a", draw(lay))
	assert.Len(t, st.expanded, 1)

	st.CollapseAll()
	lay, err = st.LayoutFor(rec, nil, 3, cols)
	require.NoError(t, err)
	assert.Equal(t, ">1 >2", draw(lay))
	assert.Empty(t, st.expanded, "CollapseAll forgets the overrides")

	// Reveal opens the ancestors of a row and names its line, once.
	st.Reveal(1)
	lay, err = st.LayoutFor(rec, nil, 3, cols)
	require.NoError(t, err)
	assert.Equal(t, "v1 .>/a .v/b ..1 >2", draw(lay))
	assert.Equal(t, int64(3), lay.Revealed)
	assert.Equal(t, lay.Revealed, lay.PosOf(1))
	lay, err = st.LayoutFor(rec, nil, 3, cols)
	require.NoError(t, err)
	assert.Equal(t, int64(-1), lay.Revealed, "served once")

	st.ExpandAll()
	lay, err = st.LayoutFor(rec, nil, 3, cols)
	require.NoError(t, err)
	assert.Equal(t, "v1 .v/a ..0 .v/b ..1 v2 .v/a ..2", draw(lay))
	assert.Empty(t, st.expanded)
}

// A path level opens one group per proper prefix: a prefix no row ends at
// is a group all the same, a leaf sits beside a subgroup ordered by its
// name, a one-segment row is a leaf under the parent, and empty segments
// are the null group.
func TestGroupPathLevel(t *testing.T) {
	rec := groupRec(t,
		[]int64{1, 1, 1, 1, 1, 1},
		[]string{"/metrics/mem", "/metrics/cpu", "/tags/_", "/hostname", "", "/metrics/disk/used"},
		[]float64{80, 45.5, 1, null, null, 7})
	defer rec.Release()
	var st GroupState
	st.SetLevels([]GroupLevel{{Segments: segments(rec)}})
	lay, err := st.LayoutFor(rec, nil, 6, aggCols(AggCount))
	require.NoError(t, err)
	// Root: the leaf `hostname` and the groups `metrics`, `tags` interleave
	// by name; under `metrics`, the leaves cpu, mem and the subgroup disk
	// interleave by name too; the null group trails.
	assert.Equal(t, "3 vmetrics .1 .vdisk ..5 .0 vtags .2 v~ .4", draw(lay))
	metrics := lay.Groups[lay.GroupAt(1)]
	assert.Equal(t, int64(3), metrics.Leaves, "leaves beneath, the subgroup's included")
	disk := lay.Groups[lay.GroupAt(3)]
	assert.Equal(t, int32(1), disk.Depth)
	assert.Equal(t, lay.GroupAt(1), disk.Parent)
	assert.Equal(t, int32(0), disk.Level, "every prefix of a path level is at that level")
	v, ok := lay.Aggregate(lay.GroupAt(1), colVal)
	require.True(t, ok)
	assert.Equal(t, int64(3), v.Count)

	// Appearance keeps document order for leaves and groups alike.
	st.SetOrder(0, GroupOrder{Appearance: true})
	lay, err = st.LayoutFor(rec, nil, 6, aggCols(AggCount))
	require.NoError(t, err)
	assert.Equal(t, "vmetrics .0 .1 .vdisk ..5 vtags .2 3 v~ .4", draw(lay))
}

// A column level over a path level: the same prefixes under two entities
// are two groups, and a path level's groups nest under the column group.
func TestGroupEntityThenPath(t *testing.T) {
	rec := groupRec(t, []int64{2, 1, 2, 1}, []string{"/m/a", "/m/a", "/m/b", "/x"}, []float64{1, 2, 3, 4})
	defer rec.Release()
	var st GroupState
	st.SetLevels([]GroupLevel{{Arrow: colEnt, KeyIsRow: true}, {Segments: segments(rec)}})
	lay, err := st.LayoutFor(rec, nil, 4, aggCols(AggNone))
	require.NoError(t, err)
	assert.Equal(t, "v1 .vm ..1 .3 v2 .vm ..0 ..2", draw(lay))
	row, ok := lay.Groups[lay.GroupAt(0)].KeyRow()
	assert.True(t, ok)
	assert.Equal(t, int64(1), row)
	m1, m2 := lay.Groups[lay.GroupAt(1)], lay.Groups[lay.GroupAt(5)]
	assert.NotEqual(t, m1.Path, m2.Path)
	assert.Equal(t, int32(1), m1.Level)
	assert.Equal(t, int32(1), m1.Depth)
}

// The partition is skipped while its inputs hold and redone when the
// record, the permutation's identity or the spec moves; a change of
// expansion alone keeps the groups where they are.
func TestGroupLayoutCaching(t *testing.T) {
	rec := groupRec(t, []int64{1, 2, 1}, []string{"/a", "/a", "/b"}, []float64{1, 2, 3})
	defer rec.Release()
	var st GroupState
	st.SetLevels([]GroupLevel{{Arrow: colEnt}})
	cols := aggCols(AggSum)
	order := []int64{2, 1, 0}
	lay, err := st.LayoutFor(rec, order, 3, cols)
	require.NoError(t, err)
	first := draw(lay)
	assert.Equal(t, "v1 .2 .0 v2 .1", first)
	groupsPtr := &lay.Groups[0]
	linesPtr := &lay.Lines[0]

	lay, err = st.LayoutFor(rec, order, 3, cols)
	require.NoError(t, err)
	assert.Same(t, groupsPtr, &lay.Groups[0], "same inputs: no partition")
	assert.Same(t, linesPtr, &lay.Lines[0], "same expansion: no flatten")

	st.Toggle(lay.Groups[lay.GroupAt(0)])
	lay, err = st.LayoutFor(rec, order, 3, cols)
	require.NoError(t, err)
	assert.Same(t, groupsPtr, &lay.Groups[0], "expansion alone re-flattens")
	assert.Equal(t, ">1 v2 .1", draw(lay))

	// A changed Order must be a different slice: the same slice mutated in
	// place is not seen, by contract.
	order2 := []int64{0, 1, 2}
	st.ExpandAll()
	lay, err = st.LayoutFor(rec, order2, 3, cols)
	require.NoError(t, err)
	assert.Equal(t, "v1 .0 .2 v2 .1", draw(lay))

	// A different aggregate declaration re-partitions.
	lay, err = st.LayoutFor(rec, order2, 3, aggCols(AggNone))
	require.NoError(t, err)
	_, ok := lay.Total(colVal)
	assert.False(t, ok)

	// No record: the root and no lines, without error.
	lay, err = st.LayoutFor(nil, nil, 0, cols)
	require.NoError(t, err)
	assert.Equal(t, int64(0), lay.Len())
	assert.Len(t, lay.Groups, 1)
}

// A column level with its own Key buckets the value and labels the group;
// its groups compare as strings.
func TestGroupKeyFunc(t *testing.T) {
	rec := groupRec(t, []int64{1, 2, 3, 4}, []string{"/a", "/a", "/a", "/a"}, []float64{0.5, 1.5, 2.5, 0.75})
	defer rec.Release()
	vals := rec.Column(colVal).(*array.Float64)
	var st GroupState
	st.SetLevels([]GroupLevel{{Arrow: -1, Key: func(row int64) Key {
		if vals.Value(int(row)) < 1 {
			return Key{Value: "0", Label: "below one"}
		}
		return Key{Value: "1", Label: "one and up"}
	}}})
	lay, err := st.LayoutFor(rec, nil, 4, aggCols(AggNone))
	require.NoError(t, err)
	assert.Equal(t, "v0 .0 .3 v1 .1 .2", draw(lay))
	assert.Equal(t, "below one", lay.Groups[lay.GroupAt(0)].Key.Label)
}
