package play

import (
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_hierarchy_test.go covers what the shared contract gained when it was
// hoisted out of the Icicle pane (ADR-0166 §SD1): the optional `color` column,
// and the reject messages speaking as the pane that asked. The two contracts
// themselves are exercised by play_icicle_panel_test.go, whose assertions did
// not change when the code moved — which is the evidence the move was one.
//
// It reuses that file's icicleTestRec fixture: same package, and a second
// record builder would be a second thing to keep true.

// treemapTestForm is a second vocabulary, so the tests can tell that the
// messages are parameterised rather than merely reworded.
var treemapTestForm = hierForm{noun: "treemap", elem: "cell"}

func TestHierColorKindOf(t *testing.T) {
	cases := []struct {
		name string
		dt   arrow.DataType
		want hierColorKindE
	}{
		{"float64", arrow.PrimitiveTypes.Float64, hierColorNumeric},
		{"int32", arrow.PrimitiveTypes.Int32, hierColorNumeric},
		{"uint64", arrow.PrimitiveTypes.Uint64, hierColorNumeric},
		{"decimal128", &arrow.Decimal128Type{Precision: 18, Scale: 4}, hierColorNumeric},
		{"string", arrow.BinaryTypes.String, hierColorCategorical},
		{"large string", arrow.BinaryTypes.LargeString, hierColorCategorical},
		// ClickHouse LowCardinality(String) arrives dictionary-encoded, and is
		// the most natural spelling of a category column there is.
		{"dictionary of string", &arrow.DictionaryType{
			IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String,
		}, hierColorCategorical},
		{"dictionary of float", &arrow.DictionaryType{
			IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.PrimitiveTypes.Float64,
		}, hierColorNumeric},
		{"boolean is neither", arrow.FixedWidthTypes.Boolean, hierColorNone},
		{"a list is neither", arrow.ListOf(arrow.BinaryTypes.String), hierColorNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hierColorKindOf(tc.dt))
		})
	}
}

func TestResolveHierarchyColorColumn(t *testing.T) {
	numeric := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}}},
		icicleTestCol{name: "value", num: []float64{1}},
		icicleTestCol{name: "color", num: []float64{7}},
	)
	defer numeric.Release()
	cl, reason := resolveHierarchy(numeric.Schema(), treemapTestForm)
	require.Empty(t, reason)
	assert.Equal(t, 2, cl.colorCol)
	assert.Equal(t, hierColorNumeric, cl.colorKind)

	categorical := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}}},
		icicleTestCol{name: "value", num: []float64{1}},
		icicleTestCol{name: "color", str: []string{"net"}},
	)
	defer categorical.Release()
	cl, reason = resolveHierarchy(categorical.Schema(), treemapTestForm)
	require.Empty(t, reason)
	assert.Equal(t, hierColorCategorical, cl.colorKind)
}

// An unusable `color` is IGNORED rather than rejected: colour is an enrichment,
// and refusing to draw the hierarchy over it would trade a picture for a
// pedantry.
func TestResolveHierarchyUnusableColorIsIgnored(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}}},
		icicleTestCol{name: "value", num: []float64{1}},
		icicleTestCol{name: "color", paths: [][]string{{"nope"}}},
	)
	defer rec.Release()

	cl, reason := resolveHierarchy(rec.Schema(), treemapTestForm)
	assert.Empty(t, reason, "an unusable colour must not reject the result")
	assert.Equal(t, hierModeFolded, cl.mode)
	assert.Equal(t, -1, cl.colorCol)
	assert.Equal(t, hierColorNone, cl.colorKind)

	tr, _ := buildHierarchy(rec, cl)
	assert.Empty(t, tr.ColorNum)
	assert.Empty(t, tr.ColorKey)
}

// The reject messages name the pane that asked, so one resolver can speak as
// whichever tab the reader is looking at.
func TestResolveHierarchyMessagesTakeTheFormsNoun(t *testing.T) {
	neither := icicleTestRec(t, icicleTestCol{name: "x", num: []float64{1}})
	defer neither.Release()
	_, reason := resolveHierarchy(neither.Schema(), treemapTestForm)
	assert.Contains(t, reason, "treemap")
	assert.NotContains(t, reason, "flame view")

	noValue := icicleTestRec(t, icicleTestCol{name: "stack", paths: [][]string{{"a"}}})
	defer noValue.Release()
	_, reason = resolveHierarchy(noValue.Schema(), treemapTestForm)
	assert.Contains(t, reason, "treemap")
	assert.Contains(t, reason, "cell's own quantity", "the element noun comes from the form too")

	// The same schema, asked by the other pane, says frame.
	_, reason = resolveHierarchy(noValue.Schema(), icicleForm)
	assert.Contains(t, reason, "flame view")
	assert.Contains(t, reason, "frame's own quantity")
}

// Folded mode: a row's colour lands on the node its VALUE lands on — the
// deepest element of its path. Interior nodes a row merely passed through were
// synthesised, and nothing in the result described them.
func TestBuildHierarchyFoldedColorLandsOnTheTerminalNode(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{
			{"root", "net"},
			{"root", "fs"},
			{"root"}, // root's own value: a path that is a PREFIX of the others
		}},
		icicleTestCol{name: "value", num: []float64{3, 2, 5}},
		icicleTestCol{name: "color", str: []string{"network", "storage", "core"}},
	)
	defer rec.Release()

	cl, reason := resolveHierarchy(rec.Schema(), treemapTestForm)
	require.Empty(t, reason)
	tr, st := buildHierarchy(rec, cl)
	require.Equal(t, hierColorCategorical, tr.ColorKind)
	require.Len(t, tr.ColorKey, tr.Len())
	assert.Zero(t, st.colorConflicts)

	at := func(path ...string) int {
		i := hierNodeByPath(tr, path...)
		require.GreaterOrEqual(t, i, 0, "node %v should exist", path)
		return i
	}
	assert.Equal(t, "network", tr.ColorKey[at("root", "net")])
	assert.Equal(t, "storage", tr.ColorKey[at("root", "fs")])
	// `root` terminates its own row, so it IS coloured — the interior-value case.
	assert.Equal(t, "core", tr.ColorKey[at("root")])
}

func TestBuildHierarchyFoldedSynthesisedInteriorHasNoColor(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a", "b", "c"}}},
		icicleTestCol{name: "value", num: []float64{1}},
		icicleTestCol{name: "color", str: []string{"leafish"}},
	)
	defer rec.Release()

	cl, _ := resolveHierarchy(rec.Schema(), treemapTestForm)
	tr, _ := buildHierarchy(rec, cl)
	assert.Equal(t, "leafish", tr.ColorKey[hierNodeByPath(tr, "a", "b", "c")])
	for _, path := range [][]string{{"a"}, {"a", "b"}} {
		i := hierNodeByPath(tr, path...)
		require.GreaterOrEqual(t, i, 0)
		assert.Empty(t, tr.ColorKey[i], "%v was synthesised; no row described it", path)
	}
}

// Two rows summing into one node is the defined reading; two rows giving that
// node two colours is not something the query said how to resolve. The first
// answer wins and the conflict is counted, so the status line can say so.
func TestBuildHierarchyFoldedColorConflictKeepsTheFirst(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}, {"a"}, {"a"}}},
		icicleTestCol{name: "value", num: []float64{1, 2, 4}},
		icicleTestCol{name: "color", str: []string{"first", "second", "first"}},
	)
	defer rec.Release()

	cl, _ := resolveHierarchy(rec.Schema(), treemapTestForm)
	tr, st := buildHierarchy(rec, cl)
	require.Equal(t, 1, tr.Len(), "three rows on one path are one node")
	assert.Equal(t, 7.0, tr.Self[0], "the values still sum")
	assert.Equal(t, "first", tr.ColorKey[0])
	assert.Equal(t, 1, st.colorConflicts, "only the disagreeing row counts")
}

func TestBuildHierarchyNodesColorIsPerRow(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "id", str: []string{"a", "b", "c"}},
		icicleTestCol{name: "parent", str: []string{"", "a", "a"}},
		icicleTestCol{name: "value", num: []float64{1, 2, 3}},
		icicleTestCol{name: "color", num: []float64{10, 20, 30}},
	)
	defer rec.Release()

	cl, reason := resolveHierarchy(rec.Schema(), treemapTestForm)
	require.Empty(t, reason)
	require.Equal(t, hierModeNodes, cl.mode)
	tr, st := buildHierarchy(rec, cl)
	require.Equal(t, hierColorNumeric, tr.ColorKind)
	require.Len(t, tr.ColorNum, 3)
	assert.Zero(t, st.colorConflicts)
	for i, want := range []float64{10, 20, 30} {
		assert.Equal(t, want, tr.ColorNum[i])
	}
}

// A dropped row must not shift the colours of the rows that survived it: the
// i-th ACCEPTED row is the i-th node, not the i-th row.
func TestBuildHierarchyNodesColorSurvivesDroppedRows(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "id", str: []string{"a", "", "b"}}, // the middle row has no id
		icicleTestCol{name: "parent", str: []string{"", "", "a"}},
		icicleTestCol{name: "value", num: []float64{1, 9, 2}},
		icicleTestCol{name: "color", num: []float64{10, 99, 20}},
	)
	defer rec.Release()

	cl, _ := resolveHierarchy(rec.Schema(), treemapTestForm)
	tr, st := buildHierarchy(rec, cl)
	require.Equal(t, 2, tr.Len())
	assert.Equal(t, 1, st.droppedPath)
	assert.Equal(t, []float64{10, 20}, tr.ColorNum, "the dropped row's colour must not land on a survivor")
}

func TestBuildHierarchyNumericColorRejectsNonFinite(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "id", str: []string{"a", "b"}},
		icicleTestCol{name: "parent", str: []string{"", ""}},
		icicleTestCol{name: "value", num: []float64{1, 1}},
		icicleTestCol{name: "color", num: []float64{math.Inf(1), 5}},
	)
	defer rec.Release()

	cl, _ := resolveHierarchy(rec.Schema(), treemapTestForm)
	tr, _ := buildHierarchy(rec, cl)
	require.Len(t, tr.ColorNum, 2)
	assert.True(t, math.IsNaN(tr.ColorNum[0]), "a non-finite colour reads as no colour, not as a value")
	assert.Equal(t, 5.0, tr.ColorNum[1])
}

// Without the column there are no colour slices at all, so a panel can test
// ColorKind alone rather than guarding every index.
func TestBuildHierarchyWithoutColorCarriesNoSlices(t *testing.T) {
	rec := icicleTestRec(t,
		icicleTestCol{name: "stack", paths: [][]string{{"a"}}},
		icicleTestCol{name: "value", num: []float64{1}},
	)
	defer rec.Release()

	cl, _ := resolveHierarchy(rec.Schema(), treemapTestForm)
	tr, _ := buildHierarchy(rec, cl)
	assert.Equal(t, hierColorNone, tr.ColorKind)
	assert.Nil(t, tr.ColorNum)
	assert.Nil(t, tr.ColorKey)
}

// hierNodeByPath resolves a root-first path to its tree index, or -1 — the
// hierTree counterpart of icicleNodeByPath, which reads an icicle.Tree.
func hierNodeByPath(tr hierTree, path ...string) int {
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
