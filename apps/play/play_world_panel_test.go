package play

import (
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// worldRec builds a two-column (string, float64) record for detection and
// extraction tests. valueNaN rows carry a NULL value cell.
func worldRec(t *testing.T, countryField string, countries []string, valueField string, values []float64) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	sb := array.NewStringBuilder(alloc)
	defer sb.Release()
	sb.AppendValues(countries, nil)
	sa := sb.NewStringArray()
	fb := array.NewFloat64Builder(alloc)
	defer fb.Release()
	for _, v := range values {
		if math.IsNaN(v) {
			fb.AppendNull()
		} else {
			fb.Append(v)
		}
	}
	fa := fb.NewFloat64Array()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: countryField, Type: arrow.BinaryTypes.String},
		{Name: valueField, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	return array.NewRecordBatch(schema, []arrow.Array{sa, fa}, int64(len(countries)))
}

func testWorldDriver(t *testing.T) *WorldDriver {
	t.Helper()
	d := NewWorldDriver(c.NewWidgetIdStack())
	if d.widget.Atlas() == nil {
		t.Fatal("world atlas failed to load")
	}
	return d
}

func TestWorldPanelAccept(t *testing.T) {
	p := worldPanel{driver: testWorldDriver(t)}
	if _, reason := p.AcceptForChannel(chMain, nil, sigNone()); reason == "" {
		t.Fatal("nil schema must reject")
	}
	noStr := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil)
	if _, reason := p.AcceptForChannel(chMain, noStr, sigNone()); reason == "" {
		t.Fatal("schema without a string column must reject")
	}
	withStr := arrow.NewSchema([]arrow.Field{
		{Name: "country", Type: arrow.BinaryTypes.String},
		{Name: "v", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	if _, reason := p.AcceptForChannel(chMain, withStr, sigNone()); reason != "" {
		t.Fatalf("schema with a string column rejected: %s", reason)
	}
}

func TestWorldDetectCountryColumn(t *testing.T) {
	d := testWorldDriver(t)
	atlas := d.widget.Atlas()

	rec := worldRec(t, "country", []string{"Germany", "France", "Brazil"}, "v", []float64{1, 2, 3})
	defer rec.Release()
	if got := d.detectCountryColumn(rec, rec.Schema(), atlas); got != 0 {
		t.Fatalf("detect = %d, want 0", got)
	}

	// Under the threshold: 1 of 3 distinct resolves.
	junk := worldRec(t, "s", []string{"foo", "bar", "Germany"}, "v", []float64{1, 2, 3})
	defer junk.Release()
	if got := d.detectCountryColumn(junk, junk.Schema(), atlas); got != -1 {
		t.Fatalf("junk column detected as %d, want -1", got)
	}
}

func TestWorldDetectPrefersHintedColumn(t *testing.T) {
	d := testWorldDriver(t)
	atlas := d.widget.Atlas()
	alloc := memory.NewGoAllocator()
	mk := func(vals []string) arrow.Array {
		b := array.NewStringBuilder(alloc)
		defer b.Release()
		b.AppendValues(vals, nil)
		return b.NewStringArray()
	}
	// Column 0 resolves too (language codes overlapping ISO country codes) but
	// carries no hint; column 1 is name-hinted — the hint must win.
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "lang", Type: arrow.BinaryTypes.String},
		{Name: "country_name", Type: arrow.BinaryTypes.String},
	}, nil)
	rec := array.NewRecordBatch(schema, []arrow.Array{
		mk([]string{"DE", "FR", "IT"}),
		mk([]string{"Japan", "Kenya", "Peru"}),
	}, 3)
	defer rec.Release()
	if got := d.detectCountryColumn(rec, rec.Schema(), atlas); got != 1 {
		t.Fatalf("detect = %d, want hinted column 1", got)
	}
}

func TestWorldExtractValuesAndDuplicates(t *testing.T) {
	d := testWorldDriver(t)
	atlas := d.widget.Atlas()
	rec := worldRec(t, "country",
		[]string{"Germany", "atlantis", "France", "germany", "Brazil"},
		"v", []float64{1, 9, 2, 3, math.NaN()})
	defer rec.Release()

	d.noteExecuted(time.Unix(100, 0))
	d.extract(rec, rec.Schema(), 0, 1, atlas)

	if d.matched != 3 {
		t.Errorf("matched = %d, want 3 (Germany, France, Brazil)", d.matched)
	}
	if d.unmatched != 1 {
		t.Errorf("unmatched = %d, want 1 (atlantis)", d.unmatched)
	}
	if d.dupes != 1 {
		t.Errorf("dupes = %d, want 1 (germany row)", d.dupes)
	}
	deu, _ := atlas.Resolve("DEU")
	if row := d.rowOf[deu]; row != 3 {
		t.Errorf("rowOf[Germany] = %d, want 3 (last wins)", row)
	}
	bra, _ := atlas.Resolve("BRA")
	if row := d.rowOf[bra]; row != 4 {
		t.Errorf("rowOf[Brazil] = %d, want 4", row)
	}
	// Cache: same executed + columns → no rebuild (mutate rowOf and re-extract).
	d.rowOf[deu] = -7
	d.extract(rec, rec.Schema(), 0, 1, atlas)
	if d.rowOf[deu] != -7 {
		t.Error("extract rebuilt despite unchanged cache key")
	}
	// New executed → rebuild.
	d.noteExecuted(time.Unix(200, 0))
	d.extract(rec, rec.Schema(), 0, 1, atlas)
	if d.rowOf[deu] != 3 {
		t.Error("extract did not rebuild on a new executed timestamp")
	}
}

func TestWorldColumnClassifiers(t *testing.T) {
	if !isWorldStringType(arrow.BinaryTypes.String) || !isWorldStringType(arrow.BinaryTypes.LargeString) {
		t.Error("plain string types must classify as string")
	}
	dict := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}
	if !isWorldStringType(dict) {
		t.Error("dictionary-of-string must classify as string (LowCardinality)")
	}
	if isWorldStringType(arrow.PrimitiveTypes.Float64) {
		t.Error("float64 must not classify as string")
	}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "s", Type: arrow.BinaryTypes.String},
		{Name: "a", Type: arrow.PrimitiveTypes.Int32},
		{Name: "b", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	nc := numericColumns(schema)
	if len(nc) != 2 || nc[0] != 1 || nc[1] != 2 {
		t.Errorf("numericColumns = %v, want [1 2]", nc)
	}
}

func TestWorldEffectiveValueCol(t *testing.T) {
	d := testWorldDriver(t)
	if got := d.effectiveValueCol(nil); got != -1 {
		t.Errorf("no numeric columns: got %d, want -1 (presence mode)", got)
	}
	if got := d.effectiveValueCol([]int{2, 5}); got != 2 {
		t.Errorf("auto: got %d, want first numeric (2)", got)
	}
	d.valueCol = 5
	if got := d.effectiveValueCol([]int{2, 5}); got != 5 {
		t.Errorf("explicit pick: got %d, want 5", got)
	}
	// Stale pick (column no longer numeric/present) falls back to auto.
	d.valueCol = 9
	if got := d.effectiveValueCol([]int{2, 5}); got != 2 {
		t.Errorf("stale pick: got %d, want auto (2)", got)
	}
	if d.valueCol != worldValueAuto {
		t.Error("stale pick must reset the persisted choice to auto")
	}
}
