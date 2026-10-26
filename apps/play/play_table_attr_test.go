package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// These tests drive attrExplodeSink through the SinkI callback sequence the
// streamreadaccess.Driver emits (see leeway_onlineapi_driver.go: BeginEntity →
// [plain sections] → BeginTaggedSections → [tagged sections, each stacking its
// attributes, each with a tag frame] → EndTaggedSections → EndEntity) and read
// the exploded record it builds: attribute stacking within a section, a
// collection's items on rows of their own, sections stacked rather than
// overlaid, the membership path and parameters, and the two levels the grid
// groups by (ADR-0336 §SD12).

type tCol struct {
	idx   int
	items []string
	coll  bool // collection (array/set) — explodes each item to its own row
}
type tAttr struct {
	cols   []tCol
	path   string // a low-cardinality verbatim membership, "" for none
	params string // a high-cardinality parameter membership, "" for none
}
type tSection struct {
	name  string // "" ⇒ plain (backbone) section, else a tagged section
	attrs []tAttr
}
type tEntity struct{ sections []tSection }

func driveCols(sink streamreadaccess.SinkI, a tAttr) {
	for _, col := range a.cols {
		sink.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: col.idx}, "", nil, valueaspects.AspectSet(""))
		if col.coll {
			sink.BeginHomogenousArrayValue(len(col.items))
			for i, it := range col.items {
				sink.BeginValueItem(i)
				_, _ = sink.WriteString(it)
				sink.EndValueItem()
			}
			sink.EndHomogenousArrayValue()
		} else {
			sink.BeginScalarValue()
			_, _ = sink.WriteString(col.items[0])
			_ = sink.EndScalarValue()
		}
		sink.EndColumn()
	}
}

// driveShape replays the Driver's sink protocol for a described page.
func driveShape(sink streamreadaccess.SinkI, entities []tEntity) {
	ms, _ := sink.(streamreadaccess.MembershipSinkI)
	sink.BeginBatch()
	for _, e := range entities {
		sink.BeginEntity()
		for _, s := range e.sections {
			if s.name != "" {
				continue // plain sections drive before the tagged block
			}
			sink.BeginPlainSection(0, nil, nil, 1)
			for _, a := range s.attrs {
				sink.BeginPlainValue()
				driveCols(sink, a)
				_ = sink.EndPlainValue()
			}
			_ = sink.EndPlainSection()
		}
		sink.BeginTaggedSections()
		for _, s := range e.sections {
			if s.name == "" {
				continue
			}
			sink.BeginSection(naming.StylableName(s.name), nil, nil, useaspects.AspectSet(""), len(s.attrs))
			for _, a := range s.attrs {
				sink.BeginTaggedValue()
				driveCols(sink, a)
				sink.BeginTags(0)
				if ms != nil {
					switch {
					case a.path != "" && a.params != "":
						ms.AddMembershipMixedLowCardVerbatimHighCardParam(a.path, a.params)
					case a.path != "":
						ms.AddMembershipVerbatim(true, a.path)
					case a.params != "":
						ms.AddMembershipVerbatim(false, a.params)
					}
				}
				sink.EndTags()
				_ = sink.EndTaggedValue()
			}
			_ = sink.EndSection()
		}
		_ = sink.EndTaggedSections()
		_ = sink.EndEntity()
	}
	_ = sink.EndBatch()
}

// explode drives a page and returns the exploded record.
func explode(t *testing.T, pageStart int64, visCols []int, nFields int, entities []tEntity) arrow.RecordBatch {
	t.Helper()
	var sink attrExplodeSink
	sink.reset(nil, pageStart, nil, nil, visCols, nFields)
	driveShape(&sink, entities)
	rec := sink.record(attrExplodedSchema(nil, visCols))
	require.NotNil(t, rec)
	t.Cleanup(rec.Release)
	return rec
}

// xrow reads one exploded row: the source row, the section, the path and
// the cells ("∅" for a null).
func xrow(rec arrow.RecordBatch, row int) (src int64, section, path string, cells []string) {
	src = rec.Column(attrXSrc).(*array.Int64).Value(row)
	section = rec.Column(attrXSection).(*array.String).Value(row)
	if p := rec.Column(attrXPath).(*array.String); !p.IsNull(row) {
		path = p.Value(row)
	} else {
		path = "∅"
	}
	for c := attrXCells; c < int(rec.NumCols()); c++ {
		col := rec.Column(c).(*array.String)
		if col.IsNull(row) {
			cells = append(cells, "∅")
		} else {
			cells = append(cells, col.Value(row))
		}
	}
	return
}

// Attributes stack within a section, a collection's items take rows of
// their own, and sections stack after one another rather than overlaying:
// an entity is as tall as the sum of its sections' rows.
func TestAttrExplode_sectionsStack(t *testing.T) {
	rec := explode(t, 10, []int{1, 2}, 3, []tEntity{
		{sections: []tSection{
			{name: "a", attrs: []tAttr{
				{cols: []tCol{{idx: 1, items: []string{"x"}}}, path: "/k/x"},
				{cols: []tCol{{idx: 1, items: []string{"y"}}}, path: "/k/y"},
			}},
			{name: "b", attrs: []tAttr{
				{cols: []tCol{{idx: 2, items: []string{"p", "q", "r"}, coll: true}}, path: "/tags/_", params: "0"},
			}},
		}},
		{sections: []tSection{
			{name: "a", attrs: []tAttr{{cols: []tCol{{idx: 1, items: []string{"z"}}}}}},
		}},
	})
	require.EqualValues(t, 6, rec.NumRows())
	want := []struct {
		src     int64
		section string
		path    string
		cells   []string
	}{
		{10, "a", "/k/x", []string{"x", "∅"}},
		{10, "a", "/k/y", []string{"y", "∅"}},
		{10, "b", "/tags/_", []string{"∅", "p"}},
		{10, "b", "/tags/_", []string{"∅", "q"}},
		{10, "b", "/tags/_", []string{"∅", "r"}},
		{11, "a", "∅", []string{"z", "∅"}},
	}
	for i, w := range want {
		src, section, path, cells := xrow(rec, i)
		assert.Equal(t, w.src, src, "row %d", i)
		assert.Equal(t, w.section, section, "row %d", i)
		assert.Equal(t, w.path, path, "row %d", i)
		assert.Equal(t, w.cells, cells, "row %d", i)
	}
	params := rec.Column(attrXParams).(*array.String)
	assert.Equal(t, "0", params.Value(2))
	assert.True(t, params.IsNull(0))
}

// A driven value column not in visCols is dropped, never read.
func TestAttrExplode_hiddenColumnDropped(t *testing.T) {
	rec := explode(t, 0, []int{1}, 6, []tEntity{{sections: []tSection{
		{name: "a", attrs: []tAttr{{cols: []tCol{
			{idx: 1, items: []string{"keep"}},
			{idx: 5, items: []string{"drop"}},
		}}}},
	}}})
	require.EqualValues(t, 1, rec.NumRows())
	_, _, _, cells := xrow(rec, 0)
	assert.Equal(t, []string{"keep"}, cells)
}

// The segmenter reads the jsonshred grammar: steps, escapes, the number
// marker, and "_" replaced by the next parameter.
func TestAttrPathSegments(t *testing.T) {
	keys := func(segs []recordtable.Key) (out []string) {
		for _, k := range segs {
			s := k.Value
			if k.Label != "" {
				s += "=" + k.Label
			}
			out = append(out, s)
		}
		return
	}
	assert.Nil(t, attrPathSegments("", ""))
	assert.Equal(t, []string{"metrics", "cpu"}, keys(attrPathSegments("/metrics/cpu", "")))
	assert.Equal(t, []string{"tags", "1=[1]"}, keys(attrPathSegments("/tags/_", "1")))
	assert.Equal(t, []string{"a", "0=[0]", "b", "2=[2]"}, keys(attrPathSegments("/a/_/b/_", "[0, 2]")))
	assert.Equal(t, []string{"a", "_=[_]"}, keys(attrPathSegments("/a/_", "")), "a missing parameter keeps the placeholder")
	assert.Equal(t, []string{"a/b", "~", "_"}, keys(attrPathSegments("/a~1b/~0/~2", "")))
	assert.Equal(t, []string{"big"}, keys(attrPathSegments("/big~n", "")))
}

// The grid's two levels over an exploded record: a band per entity, keyed
// by the source row, then the path's prefixes; the leaves under the last
// step.
func TestAttrExplode_groupsByEntityThenPath(t *testing.T) {
	rec := explode(t, 10, []int{1}, 2, []tEntity{
		{sections: []tSection{{name: "a", attrs: []tAttr{
			{cols: []tCol{{idx: 1, items: []string{"1"}}}, path: "/metrics/cpu"},
			{cols: []tCol{{idx: 1, items: []string{"2"}}}, path: "/metrics/mem"},
			{cols: []tCol{{idx: 1, items: []string{"p", "q"}, coll: true}}, path: "/tags/_", params: "0"},
			{cols: []tCol{{idx: 1, items: []string{"t"}}}},
		}}}},
		{sections: []tSection{{name: "a", attrs: []tAttr{
			{cols: []tCol{{idx: 1, items: []string{"3"}}}, path: "/metrics/cpu"},
		}}}},
	})
	var st recordtable.GroupState
	st.SetLevels([]recordtable.GroupLevel{
		{Arrow: attrXSrc, Key: attrEntityKeyOf(rec), KeyIsRow: true, Order: recordtable.GroupOrder{Appearance: true}},
		{Segments: attrSegmentsOf(rec)},
	})
	lay, err := st.LayoutFor(rec, nil, rec.NumRows(), nil)
	require.NoError(t, err)
	var got []string
	for _, ln := range lay.Lines {
		if ln.IsGroup() {
			got = append(got, tableGroupPath(lay, ln.Group))
		} else {
			got = append(got, "leaf")
		}
	}
	assert.Equal(t, []string{
		"row 11", "row 11 / metrics", "leaf", "leaf", "row 11 / tags", "leaf", "leaf", "row 11 / (null)", "leaf",
		"row 12", "row 12 / metrics", "leaf",
	}, got)
	row, ok := lay.Groups[1].KeyRow()
	require.True(t, ok)
	assert.Equal(t, int64(10), row, "the entity band stands for the source row")
}
