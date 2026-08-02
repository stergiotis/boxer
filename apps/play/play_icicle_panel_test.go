package play

import (
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_icicle_panel_test.go covers the two column contracts the Icicle pane
// accepts and the two builds behind them. The widget's own layout is tested in
// its package; what is tested here is the mapping from a result set onto
// icicle.Tree — which is where a panel loses value silently if it is wrong.

// icicleTestCol is one column of a fixture record: a string column when str is
// non-nil, a list-of-string column when paths is, a float64 one otherwise. A nil
// entry in paths is a NULL cell.
type icicleTestCol struct {
	name  string
	str   []string
	num   []float64
	paths [][]string
}

func icicleTestRec(t *testing.T, cols ...icicleTestCol) arrow.RecordBatch {
	t.Helper()
	mem := memory.NewGoAllocator()
	fields := make([]arrow.Field, 0, len(cols))
	arrs := make([]arrow.Array, 0, len(cols))
	rows := 0
	for _, col := range cols {
		var arr arrow.Array
		switch {
		case col.paths != nil:
			// The non-nullable element type pprofarrow emits, so the fixture is
			// the shape a real capture arrives in.
			b := array.NewListBuilderWithField(mem,
				arrow.Field{Name: "item", Type: arrow.BinaryTypes.String})
			vb := b.ValueBuilder().(*array.StringBuilder)
			for _, p := range col.paths {
				if p == nil {
					b.AppendNull()
					continue
				}
				b.Append(true)
				vb.AppendValues(p, nil)
			}
			arr = b.NewArray()
			b.Release()
			rows = len(col.paths)
		case col.str != nil:
			b := array.NewStringBuilder(mem)
			b.AppendValues(col.str, nil)
			arr = b.NewArray()
			b.Release()
			rows = len(col.str)
		default:
			b := array.NewFloat64Builder(mem)
			b.AppendValues(col.num, nil)
			arr = b.NewArray()
			b.Release()
			rows = len(col.num)
		}
		// Take the field type from the array rather than restating it: a
		// mismatch is a panic inside NewRecordBatch, not a readable failure.
		fields = append(fields, arrow.Field{Name: col.name, Type: arr.DataType()})
		arrs = append(arrs, arr)
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, int64(rows))
	for _, a := range arrs {
		a.Release()
	}
	return rec
}

// icicleTotalOf sums a tree's self values — the quantity every build must
// conserve against the rows it accepted, whatever it did to the shape.
func icicleTotalOf(tr icicle.Tree) (sum float64) {
	for _, v := range tr.Self {
		sum += v
	}
	return
}

// icicleNodeByPath resolves a root-first path to its tree index, or -1. The
// builds intern by (parent, label), so this is how a test names a node without
// depending on the order the trie happened to allocate.
func icicleNodeByPath(tr icicle.Tree, path ...string) int {
	cur := int32(-1)
	for _, want := range path {
		found := int32(-1)
		for i := range tr.Labels {
			if tr.Parents[i] == cur && tr.Labels[i] == want {
				found = int32(i)
				break
			}
		}
		if found < 0 {
			return -1
		}
		cur = found
	}
	return int(cur)
}

// Both contracts resolve by name, and the folded one additionally by type.
func TestResolveIcicleColumnsFolded(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}}},
		icicleTestCol{name: "value", num: []float64{1}},
	)
	defer rec.Release()

	cl, reason := resolveIcicleColumns(rec.Schema())
	require.Empty(t, reason)
	assert.Equal(t, icicleModeFolded, cl.mode)
	assert.Equal(t, 0, cl.stackCol)
	assert.Equal(t, 1, cl.valueCol)
	assert.Equal(t, -1, cl.unitCol, "an absent optional column resolves to -1")
}

func TestResolveIcicleColumnsNodes(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "id", str: []string{"a"}},
		icicleTestCol{name: "parent", str: []string{""}},
		icicleTestCol{name: "label", str: []string{"A"}},
		icicleTestCol{name: "value", num: []float64{1}},
	)
	defer rec.Release()

	cl, reason := resolveIcicleColumns(rec.Schema())
	require.Empty(t, reason)
	assert.Equal(t, icicleModeNodes, cl.mode)
	assert.Equal(t, 0, cl.idCol)
	assert.Equal(t, 1, cl.parentCol)
	assert.Equal(t, 2, cl.labelCol)
	assert.Equal(t, 3, cl.valueCol)
}

// A schema satisfying both takes the folded reading — the more specific claim —
// and a `stack` that is NOT list-typed falls through to the node contract rather
// than failing a result that carries a usable one.
func TestResolveIcicleColumnsPrecedence(t *testing.T) {
	both := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}}},
		icicleTestCol{name: "id", str: []string{"a"}},
		icicleTestCol{name: "parent", str: []string{""}},
		icicleTestCol{name: "value", num: []float64{1}},
	)
	defer both.Release()
	cl, reason := resolveIcicleColumns(both.Schema())
	require.Empty(t, reason)
	assert.Equal(t, icicleModeFolded, cl.mode)

	scalarStack := icicleTestRec(t,
		icicleTestCol{name: "stack", str: []string{"a"}},
		icicleTestCol{name: "id", str: []string{"a"}},
		icicleTestCol{name: "parent", str: []string{""}},
		icicleTestCol{name: "value", num: []float64{1}},
	)
	defer scalarStack.Release()
	cl, reason = resolveIcicleColumns(scalarStack.Schema())
	require.Empty(t, reason)
	assert.Equal(t, icicleModeNodes, cl.mode, "a scalar `stack` is not a path; the node contract still applies")
}

// The rejections are the pane's whole empty state, so each says what to do.
func TestResolveIcicleColumnsRejections(t *testing.T) {
	scalarOnly := icicleTestRec(t,
		icicleTestCol{name: "stack", str: []string{"a"}},
		icicleTestCol{name: "value", num: []float64{1}},
	)
	defer scalarOnly.Release()
	cl, reason := resolveIcicleColumns(scalarOnly.Schema())
	assert.Equal(t, icicleModeNone, cl.mode)
	assert.Contains(t, reason, "splitByChar", "a mistyped `stack` is told how to become an array")

	noValue := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}}},
	)
	defer noValue.Release()
	cl, reason = resolveIcicleColumns(noValue.Schema())
	assert.Equal(t, icicleModeNone, cl.mode, "a claim missing `value` is not a claim")
	assert.Contains(t, reason, "`value`")

	neither := icicleTestRec(t, icicleTestCol{name: "x", num: []float64{1}})
	defer neither.Release()
	cl, reason = resolveIcicleColumns(neither.Schema())
	assert.Equal(t, icicleModeNone, cl.mode)
	assert.Contains(t, reason, "`stack`")
	assert.Contains(t, reason, "`parent`")
}

// Folded interning: one node per distinct prefix, a repeated path SUMS, and a
// path that is a PREFIX of another lands its value on the interior node — the
// case that separates this widget from a treemap, and the shape a profile with
// both self samples and callees has.
func TestBuildIcicleFolded(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{
			{"main", "run", "parse"},
			{"main", "run", "emit"},
			{"main", "run"},          // run's own samples
			{"main", "run", "parse"}, // a repeat of the first
		}},
		icicleTestCol{name: "value", num: []float64{3, 2, 5, 1}},
	)
	defer rec.Release()
	cl, reason := resolveIcicleColumns(rec.Schema())
	require.Empty(t, reason)

	tr, st := buildIcicleTree(rec, cl)
	require.NoError(t, tr.Validate())
	assert.Equal(t, icicleModeFolded, st.mode)
	assert.Equal(t, 4, tr.Len(), "main, run, parse, emit — one node per distinct prefix")
	assert.Equal(t, 4, st.nodes)
	assert.Zero(t, st.droppedPath)
	assert.Zero(t, st.droppedValue)

	assert.Equal(t, 0.0, tr.Self[icicleNodeByPath(tr, "main")], "an interior frame no row ended on carries nothing")
	assert.Equal(t, 5.0, tr.Self[icicleNodeByPath(tr, "main", "run")], "the prefix row's value lands on the interior node")
	assert.Equal(t, 4.0, tr.Self[icicleNodeByPath(tr, "main", "run", "parse")], "3+1: a repeated path sums")
	assert.Equal(t, 2.0, tr.Self[icicleNodeByPath(tr, "main", "run", "emit")])
	assert.Equal(t, 11.0, icicleTotalOf(tr), "every accepted row reaches the total")
}

// Empty path elements are skipped rather than drawn as unlabelled frames — the
// leading ” of splitByChar('/', '/usr/bin') — and skipping conserves the total.
func TestBuildIcicleFoldedSkipsEmptyElements(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"", "usr", "bin"}, {"usr", "lib"}}},
		icicleTestCol{name: "value", num: []float64{4, 6}},
	)
	defer rec.Release()
	cl, _ := resolveIcicleColumns(rec.Schema())

	tr, st := buildIcicleTree(rec, cl)
	require.NoError(t, tr.Validate())
	assert.Equal(t, 3, tr.Len(), "usr, bin, lib — the empty element is not a frame")
	assert.Equal(t, int32(-1), tr.Parents[icicleNodeByPath(tr, "usr")], "usr is the root, not a child of ''")
	assert.Equal(t, 4.0, tr.Self[icicleNodeByPath(tr, "usr", "bin")])
	assert.Equal(t, 10.0, icicleTotalOf(tr))
	assert.Zero(t, st.droppedPath)
}

// Rows the form cannot draw are dropped and COUNTED, rather than failing the
// whole picture the way Validate would.
func TestBuildIcicleFoldedDropsAndCounts(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{
			{"a"},
			nil,   // NULL cell
			{},    // an empty array
			{""},  // nothing but empty elements
			{"b"}, // negative value
			{"c"}, // not finite
		}},
		icicleTestCol{name: "value", num: []float64{7, 1, 1, 1, -2, math.Inf(1)}},
	)
	defer rec.Release()
	cl, _ := resolveIcicleColumns(rec.Schema())

	tr, st := buildIcicleTree(rec, cl)
	require.NoError(t, tr.Validate())
	assert.Equal(t, 1, tr.Len(), "only the first row is drawable")
	assert.Equal(t, 7.0, icicleTotalOf(tr))
	assert.Equal(t, 3, st.droppedPath, "null, empty and all-empty-elements")
	assert.Equal(t, 2, st.droppedValue, "negative and infinite")
}

// A path deeper than the cap is cut at it and counted; the value still lands, on
// the deepest frame that survived, so the total is conserved.
func TestBuildIcicleFoldedTruncatesDepth(t *testing.T) {
	deep := make([]string, icicleMaxDepth+5)
	for i := range deep {
		deep[i] = "f" + string(rune('a'+i%26)) + string(rune('0'+i/26%10))
	}
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{deep}},
		icicleTestCol{name: "value", num: []float64{9}},
	)
	defer rec.Release()
	cl, _ := resolveIcicleColumns(rec.Schema())

	tr, st := buildIcicleTree(rec, cl)
	require.NoError(t, tr.Validate())
	assert.Equal(t, icicleMaxDepth, tr.Len())
	assert.Equal(t, 1, st.truncated)
	assert.Equal(t, 9.0, icicleTotalOf(tr), "truncation costs depth, not quantity")
}

// Node mode: parents are resolved in a second pass, so a child may precede its
// parent — which is the order a recursive CTE most naturally emits.
func TestBuildIcicleNodes(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "id", str: []string{"leaf", "root", "mid"}},
		icicleTestCol{name: "parent", str: []string{"mid", "", "root"}},
		icicleTestCol{name: "label", str: []string{"Leaf", "", "Mid"}},
		icicleTestCol{name: "value", num: []float64{1, 2, 3}},
	)
	defer rec.Release()
	cl, reason := resolveIcicleColumns(rec.Schema())
	require.Empty(t, reason)

	tr, st := buildIcicleTree(rec, cl)
	require.NoError(t, tr.Validate())
	assert.Equal(t, icicleModeNodes, st.mode)
	require.Equal(t, 3, tr.Len())
	assert.Equal(t, []string{"Leaf", "root", "Mid"}, tr.Labels,
		"an empty `label` falls back to the id; the rest override it")
	assert.Equal(t, []int32{2, -1, 1}, tr.Parents, "a child ahead of its parent still resolves")
	assert.Equal(t, 6.0, icicleTotalOf(tr))
	assert.Equal(t, 2.0, tr.Self[1], "an interior node keeps its OWN value")
}

// A parent naming nothing in the result — or naming itself — is demoted to a
// root and counted, rather than dropped (which would lose its value) or left to
// fail Validate (which would lose the whole picture).
func TestBuildIcicleNodesReparents(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "id", str: []string{"a", "b", "c"}},
		icicleTestCol{name: "parent", str: []string{"", "gone", "c"}},
		icicleTestCol{name: "value", num: []float64{1, 2, 3}},
	)
	defer rec.Release()
	cl, _ := resolveIcicleColumns(rec.Schema())

	tr, st := buildIcicleTree(rec, cl)
	require.NoError(t, tr.Validate(), "a self-parent must not survive into the tree")
	assert.Equal(t, []int32{-1, -1, -1}, tr.Parents, "a forest of three roots")
	assert.Equal(t, 2, st.reparented)
	assert.Equal(t, 6.0, icicleTotalOf(tr))
}

// A duplicate id is ambiguous — two nodes claiming one identity — so the first
// wins and the loss is reported, unlike two folded rows sharing a path.
func TestBuildIcicleNodesDropsDuplicateIDs(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "id", str: []string{"a", "a", ""}},
		icicleTestCol{name: "parent", str: []string{"", "", ""}},
		icicleTestCol{name: "value", num: []float64{1, 99, 5}},
	)
	defer rec.Release()
	cl, _ := resolveIcicleColumns(rec.Schema())

	tr, st := buildIcicleTree(rec, cl)
	require.Equal(t, 1, tr.Len())
	assert.Equal(t, 1.0, tr.Self[0], "the first row wins")
	assert.Equal(t, 1, st.droppedDup)
	assert.Equal(t, 1, st.droppedPath, "an empty id is not an id")
}

// A cycle the query produced reaches Compute, which rejects it by name. The
// panel reports that rather than hanging or drawing a lie.
func TestBuildIcicleNodesCycleIsReported(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "id", str: []string{"a", "b"}},
		icicleTestCol{name: "parent", str: []string{"b", "a"}},
		icicleTestCol{name: "value", num: []float64{1, 2}},
	)
	defer rec.Release()
	cl, _ := resolveIcicleColumns(rec.Schema())

	tr, _ := buildIcicleTree(rec, cl)
	_, err := icicle.Compute(tr, icicle.Options{})
	require.Error(t, err)
	assert.Contains(t, icicleReason(err), "cycle")
	assert.NotContains(t, icicleReason(err), "icicle:", "the package prefix is trimmed for the status line")
}

// The layout's reported total is the sum of what the build accepted — the
// invariant a panel that quietly drops rows would break.
func TestIcicleLayoutConservesTotal(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a", "b"}, {"a", "c"}, {"a"}, {"d"}}},
		icicleTestCol{name: "value", num: []float64{2, 3, 1, 4}},
	)
	defer rec.Release()
	cl, _ := resolveIcicleColumns(rec.Schema())
	tr, st := buildIcicleTree(rec, cl)

	lay, err := icicle.Compute(tr, icicleTreeOpts(icicle.OrientFlame, icicle.OrderValueDesc, iciclePruneOff, st.unit))
	require.NoError(t, err)
	assert.Equal(t, 10.0, lay.Report.Total, "two roots, summed")
	assert.Equal(t, 2, lay.Report.Rows, "a and d at depth 0; b and c at depth 1")
}

// Pruning is layout-time and reported, so the status line can say how much of
// the picture is missing.
func TestIciclePruneIsReported(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"big"}, {"big", "tiny"}}},
		icicleTestCol{name: "value", num: []float64{1000, 1}},
	)
	defer rec.Release()
	cl, _ := resolveIcicleColumns(rec.Schema())
	tr, _ := buildIcicleTree(rec, cl)

	kept, err := icicle.Compute(tr, icicleTreeOpts(icicle.OrientIcicle, icicle.OrderValueDesc, iciclePruneOff, ""))
	require.NoError(t, err)
	require.Equal(t, 2, kept.Report.Nodes)

	pruned, err := icicle.Compute(tr, icicleTreeOpts(icicle.OrientIcicle, icicle.OrderValueDesc, iciclePrunePercent, ""))
	require.NoError(t, err)
	assert.Equal(t, 1, pruned.Report.Nodes, "tiny is 0.1% of the total, below the 1% cut")
	assert.Equal(t, 1, pruned.Report.Pruned)
	assert.Equal(t, 1.0, pruned.Report.PrunedValue)
}

// One unit labels the whole axis, so the first non-empty cell answers for the
// column and a result disagreeing with itself is not rejected over it.
func TestIcicleUnitOf(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}, {"b"}}},
		icicleTestCol{name: "value", num: []float64{1, 2}},
		icicleTestCol{name: "unit", str: []string{"", "bytes"}},
	)
	defer rec.Release()
	cl, _ := resolveIcicleColumns(rec.Schema())
	_, st := buildIcicleTree(rec, cl)
	assert.Equal(t, "bytes", st.unit)

	assert.Equal(t, "15.0k bytes", icicleQty(15000, "bytes"))
	assert.Equal(t, "1500", icicleQty(1500, ""), "below the scaling threshold, and no unit to suffix")
	assert.Equal(t, "2.0G", icicleQty(2e9, ""))
}

// The breadcrumb drops ancestors from the left until it fits, and never drops
// the frame the pointer is actually on.
func TestIcicleFramePath(t *testing.T) {
	tr := icicle.Tree{
		Labels:  []string{"root", "middle", "leaf"},
		Parents: []int32{-1, 0, 1},
		Self:    []float64{0, 0, 1},
	}
	lay, err := icicle.Compute(tr, icicle.Options{})
	require.NoError(t, err)
	leaf := icicleNodeInLayout(t, lay, "leaf")
	assert.Equal(t, "root › middle › leaf", icicleFramePath(lay, leaf))

	long := icicle.Tree{
		Labels:  []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "cccccccccccccccccccccccccccccccccccccccc"},
		Parents: []int32{-1, 0, 1},
		Self:    []float64{0, 0, 1},
	}
	lay, err = icicle.Compute(long, icicle.Options{})
	require.NoError(t, err)
	got := icicleFramePath(lay, icicleNodeInLayout(t, lay, long.Labels[2]))
	assert.LessOrEqual(t, len([]rune(got)), icicleFramePathRunes)
	assert.Contains(t, got, long.Labels[2], "the frame under the pointer is always kept")
	assert.Contains(t, got, "…", "and the elision says ancestors were dropped")
}

func icicleNodeInLayout(t *testing.T, lay *icicle.Layout, label string) int {
	t.Helper()
	for i := range lay.Nodes {
		if lay.Nodes[i].Label == label {
			return i
		}
	}
	t.Fatalf("no node labelled %q in the layout", label)
	return -1
}
