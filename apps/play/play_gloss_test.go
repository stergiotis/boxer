package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// glossRec builds a two-row record: a declared temperature column, a declared
// Luhn column, an undeclared float, a declaration with a typo, and a
// temperature declared over a string column (kind refused).
func glossRec(t *testing.T) arrow.RecordBatch {
	t.Helper()
	mem := memory.NewGoAllocator()
	fb := array.NewFloat64Builder(mem)
	defer fb.Release()
	fb.AppendValues([]float64{21.5, -3}, nil)
	temps := fb.NewArray()
	sb := array.NewStringBuilder(mem)
	defer sb.Release()
	sb.AppendValues([]string{"4111111111111111", "4111111111111112"}, nil)
	pans := sb.NewArray()
	pb := array.NewFloat64Builder(mem)
	defer pb.Release()
	pb.AppendValues([]float64{1, 2}, nil)
	plain := pb.NewArray()
	tb := array.NewFloat64Builder(mem)
	defer tb.Release()
	tb.AppendValues([]float64{7, 8}, nil)
	typo := tb.NewArray()
	wb := array.NewStringBuilder(mem)
	defer wb.Release()
	wb.AppendValues([]string{"hot", "cold"}, nil)
	wrongKind := wb.NewArray()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "t@gloss/temperature;unit=C", Type: arrow.PrimitiveTypes.Float64},
		{Name: "pan@gloss/luhn", Type: arrow.BinaryTypes.String},
		{Name: "plain", Type: arrow.PrimitiveTypes.Float64},
		{Name: "x@gloss/temperatur;unit=C", Type: arrow.PrimitiveTypes.Float64},
		{Name: "w@gloss/temperature;unit=C", Type: arrow.BinaryTypes.String},
	}, nil)
	return array.NewRecordBatch(schema, []arrow.Array{temps, pans, plain, typo, wrongKind}, 2)
}

func TestGlossColumnsResolvePerSchema(t *testing.T) {
	app := &PlayApp{}
	rec := glossRec(t)
	defer rec.Release()
	cols := app.glossColumns(rec.Schema())
	require.Len(t, cols, 5)

	assert.Equal(t, "t", cols[0].label)
	assert.Equal(t, gloss.MediaTypeTemperature, cols[0].mediaType)
	assert.Equal(t, glossSourceAlias, cols[0].source)
	assert.True(t, cols[0].glossed())
	assert.Equal(t, "glossed as gloss/temperature (alias)", glossHover(&cols[0]))

	assert.True(t, cols[1].glossed())
	assert.Empty(t, cols[2].mediaType, "an undeclared column is plain")
	assert.Empty(t, glossHover(&cols[2]))

	assert.Equal(t, "gloss/temperatur", cols[3].mediaType)
	assert.Nil(t, cols[3].inst)
	assert.Contains(t, cols[3].reason, "unknown media type")
	assert.Contains(t, glossHover(&cols[3]), "refused")

	assert.NotNil(t, cols[4].inst, "the declaration is fine — the column kind is not")
	assert.False(t, cols[4].glossed())
	assert.Contains(t, cols[4].rowReason, "expects numeric, got text")
	assert.Contains(t, glossHover(&cols[4]), "not applied")

	// Same schema pointer → same slice, no re-resolution.
	again := app.glossColumns(rec.Schema())
	assert.Equal(t, &cols[0], &again[0])
}

func TestGlossCellRendersFaceOrPlain(t *testing.T) {
	app := &PlayApp{}
	rec := glossRec(t)
	defer rec.Release()
	cols := app.glossColumns(rec.Schema())

	text, tone := app.glossCell(&cols[0], rec.Column(0), 0, false)
	assert.Equal(t, "21.5 °C", text)
	assert.Equal(t, gloss.ToneNeutral, tone)

	text, tone = app.glossCell(&cols[1], rec.Column(1), 1, false)
	assert.Equal(t, "4111 •••• •••• 1112 ✗", text)
	assert.Equal(t, gloss.ToneError, tone)

	text, _ = app.glossCell(&cols[2], rec.Column(2), 0, false)
	assert.Equal(t, "1", text, "plain column, plain text")
	text, _ = app.glossCell(&cols[3], rec.Column(3), 0, false)
	assert.Equal(t, "7", text, "a refused declaration renders plain")
	text, _ = app.glossCell(&cols[4], rec.Column(4), 0, false)
	assert.Equal(t, "hot", text, "a refused kind renders plain")

	// The raw toggle bypasses every gloss.
	app.tableOpts.rawCells = true
	text, tone = app.glossCell(&cols[0], rec.Column(0), 0, false)
	assert.Equal(t, "21.5", text)
	assert.Equal(t, gloss.ToneNeutral, tone)
	assert.True(t, app.anyGlossed(rec.Schema()), "the toggle is offered while a column is glossed")

	// The text-backed variant (per-attribute grid) applies the same face.
	app.tableOpts.rawCells = false
	assert.Equal(t, "-3.0 °C", app.glossText(&cols[0], "-3", gloss.ValueKindNumeric))
	assert.Equal(t, "", app.glossText(&cols[0], "", gloss.ValueKindNumeric), "an absent item stays blank")
}
