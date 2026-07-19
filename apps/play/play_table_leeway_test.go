package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// buildListCol builds a List<String> Arrow column with one list per entry in
// lens — lens[i]==0 produces an empty list at row i, the physical shape of
// "this entity lacks the tag" that sectionHasAttrInRange must recognise (see
// streamreadaccess/EXPLANATION.md: every tagged value column is List/LargeList,
// its outer length the entity's attribute count for the section).
func buildListCol(t *testing.T, lens ...int) *array.List {
	t.Helper()
	lb := array.NewListBuilder(memory.NewGoAllocator(), arrow.BinaryTypes.String)
	vb := lb.ValueBuilder().(*array.StringBuilder)
	for _, n := range lens {
		lb.Append(true)
		for range n {
			vb.Append("v")
		}
	}
	arr := lb.NewListArray()
	t.Cleanup(arr.Release)
	return arr
}

func TestSectionHasAttrInRange(t *testing.T) {
	// rows: 0:[], 1:[], 2:[a,b], 3:[], 4:[a]
	col := buildListCol(t, 0, 0, 2, 0, 1)
	cases := []struct {
		name   string
		lo, hi int64
		want   bool
	}{
		{"all-empty prefix", 0, 2, false},
		{"includes non-empty row", 0, 3, true},
		{"single non-empty row", 2, 3, true},
		{"empty range", 3, 3, false},
		{"trailing non-empty", 3, 5, true},
		{"whole column", 0, 5, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sectionHasAttrInRange(col, c.lo, c.hi); got != c.want {
				t.Errorf("sectionHasAttrInRange(lo=%d,hi=%d) = %v, want %v", c.lo, c.hi, got, c.want)
			}
		})
	}
}

// TestSectionHasAttrInRange_nonListDefaultsToPresent covers the defensive
// fallback: leeway's DDL never emits a non-list tagged value column, but if
// classification and physical layout ever disagree, the column must stay
// visible rather than being silently guessed away.
func TestSectionHasAttrInRange_nonListDefaultsToPresent(t *testing.T) {
	b := array.NewStringBuilder(memory.NewGoAllocator())
	b.Append("x")
	arr := b.NewArray()
	defer arr.Release()
	if !sectionHasAttrInRange(arr, 0, 1) {
		t.Error("a non-List column must default to present (never guessed empty)")
	}
}

// TestEmptyTaggedSections checks the section-level aggregation: a backbone
// (plain) column is never a candidate regardless of its values, and a tagged
// section's emptiness is judged only over the requested row range — the same
// section can be empty on one page and not another.
func TestEmptyTaggedSections(t *testing.T) {
	idB := array.NewInt64Builder(memory.NewGoAllocator())
	idB.AppendValues([]int64{1, 2, 3}, nil)
	idArr := idB.NewArray()
	defer idArr.Release()

	emptyTag := buildListCol(t, 0, 0, 0)  // never has an attribute
	sparseTag := buildListCol(t, 0, 0, 1) // only row 2 has one

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "empty_tag", Type: arrow.ListOf(arrow.BinaryTypes.String)},
		{Name: "sparse_tag", Type: arrow.ListOf(arrow.BinaryTypes.String)},
	}, nil)
	rec := array.NewRecordBatch(schema, []arrow.Array{idArr, emptyTag, sparseTag}, 3)
	defer rec.Release()

	classes := []streamreadaccess.ColumnClass{
		{ArrowIdx: 0, Class: streamreadaccess.ColumnRoleClassValue, PlainItemType: common.PlainItemTypeEntityId},
		{ArrowIdx: 1, Class: streamreadaccess.ColumnRoleClassValue, SectionName: naming.StylableName("empty-tag")},
		{ArrowIdx: 2, Class: streamreadaccess.ColumnRoleClassValue, SectionName: naming.StylableName("sparse-tag")},
	}

	got := emptyTaggedSections(classes, rec, 0, 3)
	if !got["empty-tag"] {
		t.Error(`"empty-tag" should be empty over the full page`)
	}
	if got["sparse-tag"] {
		t.Error(`"sparse-tag" should not be empty over the full page (row 2 has a value)`)
	}
	if got["id"] {
		t.Error("a backbone column must never appear in the empty-sections result")
	}

	// Excluding row 2 makes sparse-tag empty too — the set of hidden sections
	// changes with the page, which is the whole point of the toggle.
	got = emptyTaggedSections(classes, rec, 0, 2)
	if !got["sparse-tag"] {
		t.Error(`"sparse-tag" should be empty on a page that excludes row 2`)
	}
}
