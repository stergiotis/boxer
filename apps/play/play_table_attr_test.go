package play

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// These tests drive attrExplodeSink through the exact SinkI callback sequence
// the streamreadaccess.Driver emits (see leeway_onlineapi_driver.go:
// BeginEntity → [plain sections] → BeginTaggedSections → [tagged sections, each
// stacking its attributes] → EndTaggedSections → EndEntity). They cover the
// three things the pooled/positional rewrite has to get right and that the
// package's allocation-agnostic tests would miss: attribute stacking within a
// section, section overlay across the entity's visual rows, and — crucially —
// that reusing the pooled sink across frames never leaks a previous frame's cell
// into a recycled row slot.

type tCol struct {
	idx   int
	items []string
	coll  bool // collection (array/set) — explodes each item to its own row
}
type tAttr struct{ cols []tCol }
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

func assertRows(t *testing.T, got []attrGridRow, want []attrGridRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("row count = %d, want %d\n got=%v", len(got), len(want), got)
	}
	for r := range want {
		g, w := got[r], want[r]
		if g.absRow != w.absRow || g.firstOfEntity != w.firstOfEntity {
			t.Errorf("row %d: absRow/first = (%d,%v), want (%d,%v)", r, g.absRow, g.firstOfEntity, w.absRow, w.firstOfEntity)
		}
		if len(g.cells) != len(w.cells) {
			t.Errorf("row %d: cells len = %d, want %d (%v)", r, len(g.cells), len(w.cells), g.cells)
			continue
		}
		for c := range w.cells {
			if g.cells[c] != w.cells[c] {
				t.Errorf("row %d col %d = %q, want %q", r, c, g.cells[c], w.cells[c])
			}
		}
	}
}

// TestAttrExplodeSink_explodeOverlay checks attribute stacking, collection
// explosion, and section overlay for a two-entity page. visCols=[1,2] ⇒ cell 0
// is column 1, cell 1 is column 2.
func TestAttrExplodeSink_explodeOverlay(t *testing.T) {
	var sink attrExplodeSink
	sink.reset(nil, 10, nil, nil, []int{1, 2}, 3)
	driveShape(&sink, []tEntity{
		{sections: []tSection{
			// section "a": two scalar attributes → stacks 2 rows in col 1.
			{name: "a", attrs: []tAttr{
				{cols: []tCol{{idx: 1, items: []string{"x"}}}},
				{cols: []tCol{{idx: 1, items: []string{"y"}}}},
			}},
			// section "b": one 3-item collection → explodes to 3 rows in col 2,
			// overlaying a's rows 0/1 and extending the entity to 3 visual rows.
			{name: "b", attrs: []tAttr{
				{cols: []tCol{{idx: 2, items: []string{"p", "q", "r"}, coll: true}}},
			}},
		}},
		{sections: []tSection{
			{name: "a", attrs: []tAttr{{cols: []tCol{{idx: 1, items: []string{"z"}}}}}},
			{name: "b", attrs: []tAttr{{cols: []tCol{{idx: 2, items: []string{"m"}}}}}},
		}},
	})
	assertRows(t, sink.rows, []attrGridRow{
		{absRow: 10, firstOfEntity: true, cells: []string{"x", "p"}},
		{absRow: 10, firstOfEntity: false, cells: []string{"y", "q"}},
		{absRow: 10, firstOfEntity: false, cells: []string{"", "r"}}, // a has no 3rd row
		{absRow: 11, firstOfEntity: true, cells: []string{"z", "m"}},
	})
}

// TestAttrExplodeSink_poolNoStale re-drives the same pooled sink with a much
// smaller page. The rewrite reuses the previous frame's row slots and their
// backing []string; if clearAndSize failed to zero a reused slot, column 2 of
// the surviving row would still read the previous frame's "p".
func TestAttrExplodeSink_poolNoStale(t *testing.T) {
	var sink attrExplodeSink

	sink.reset(nil, 10, nil, nil, []int{1, 2}, 3)
	driveShape(&sink, []tEntity{{sections: []tSection{
		{name: "a", attrs: []tAttr{{cols: []tCol{{idx: 1, items: []string{"x"}}}}}},
		{name: "b", attrs: []tAttr{{cols: []tCol{{idx: 2, items: []string{"p", "q", "r"}, coll: true}}}}},
	}}})
	if len(sink.rows) != 3 {
		t.Fatalf("frame 1: got %d rows, want 3", len(sink.rows))
	}

	// Frame 2: a single row that writes only column 1. Column 2 must be empty.
	sink.reset(nil, 10, nil, nil, []int{1, 2}, 3)
	driveShape(&sink, []tEntity{{sections: []tSection{
		{name: "a", attrs: []tAttr{{cols: []tCol{{idx: 1, items: []string{"ONLY"}}}}}},
	}}})
	assertRows(t, sink.rows, []attrGridRow{
		{absRow: 10, firstOfEntity: true, cells: []string{"ONLY", ""}},
	})
}

// benchShape mirrors a sailing.facts-style page: a backbone id plus a tagged
// "tv" section with several scalar attributes and two 3-item collection
// attributes (which explode to their own rows), across many entities.
func benchShape(entities int) []tEntity {
	out := make([]tEntity, entities)
	for e := range out {
		out[e] = tEntity{sections: []tSection{
			{name: "", attrs: []tAttr{{cols: []tCol{{idx: 0, items: []string{"id"}}}}}},
			{name: "tv", attrs: []tAttr{
				{cols: []tCol{{idx: 1, items: []string{"sym"}}, {idx: 2, items: []string{"1"}}}},
				{cols: []tCol{{idx: 3, items: []string{"3.14"}}, {idx: 4, items: []string{"1"}}}},
				{cols: []tCol{{idx: 5, items: []string{"true"}}, {idx: 6, items: []string{"1"}}}},
				{cols: []tCol{{idx: 7, items: []string{"a", "b", "c"}, coll: true}, {idx: 8, items: []string{"3"}}}},
				{cols: []tCol{{idx: 9, items: []string{"x", "y", "z"}, coll: true}, {idx: 10, items: []string{"3"}}}},
			}},
		}}
	}
	return out
}

// BenchmarkAttrExplodeSink measures the steady-state allocation of a pooled sink
// re-driven every frame (as the live per-attribute Table view drives it). After
// the first iteration grows the reusable buffers, subsequent iterations reuse
// them, so the only allocations left are the per-cell string materialisations
// (the retained cell text) — every map/slice scaffold the rewrite targeted is
// gone. BenchmarkAttrExplodeSinkFresh is the same work with a brand-new sink each
// iteration (no pooling): the delta is what win (4) saves per frame.
// Run: go test -tags=... -bench AttrExplode -benchmem ./apps/play/
func BenchmarkAttrExplodeSink(b *testing.B) {
	entities := benchShape(50)
	visCols := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	var sink attrExplodeSink
	b.ReportAllocs()
	for b.Loop() {
		sink.reset(nil, 0, nil, nil, visCols, 11)
		driveShape(&sink, entities)
	}
}

func BenchmarkAttrExplodeSinkFresh(b *testing.B) {
	entities := benchShape(50)
	visCols := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	b.ReportAllocs()
	for b.Loop() {
		var sink attrExplodeSink // fresh every frame — no cross-frame pooling
		sink.reset(nil, 0, nil, nil, visCols, 11)
		driveShape(&sink, entities)
	}
}

// TestAttrExplodeSink_hiddenColumnDropped checks that a driven value column not
// present in visCols is silently dropped (never read) rather than panicking or
// widening a row past nCols.
func TestAttrExplodeSink_hiddenColumnDropped(t *testing.T) {
	var sink attrExplodeSink
	// Only column 1 is visible; the driver still emits column 5.
	sink.reset(nil, 0, nil, nil, []int{1}, 6)
	driveShape(&sink, []tEntity{{sections: []tSection{
		{name: "a", attrs: []tAttr{{cols: []tCol{
			{idx: 1, items: []string{"keep"}},
			{idx: 5, items: []string{"drop"}},
		}}}},
	}}})
	assertRows(t, sink.rows, []attrGridRow{
		{absRow: 0, firstOfEntity: true, cells: []string{"keep"}},
	})
}
