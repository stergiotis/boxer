package imagescope

import (
	"bytes"
	"context"
	"image"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

// The signed 16-bit sample, through the catalog: its kind and its declared
// invalid value are reported, its border is invalid in read_pixels and in
// measure, and its histogram is exact.
func TestASignedSampleWithADeclaredInvalidValue(t *testing.T) {
	inst := testApp(t)
	require.NoError(t, command(t, inst, opOpenSample, OpenSampleArgs{Name: samples.NameElevation}))
	settle(t, inst, func() bool { return inst.pic != nil && !inst.open.Running() })
	v, err := query[appops.None, View](t, inst, opGetView, appops.None{})
	require.NoError(t, err)
	assert.Equal(t, "i16", v.Image.Kind)
	assert.True(t, v.Image.HasInvalid)
	assert.Equal(t, float64(samples.ElevationInvalid), v.Image.InvalidValue)
	require.Len(t, v.Image.Notes, 1)
	assert.Contains(t, v.Image.Notes[0], "-32768 marks no measurement")

	px, err := query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{Col: 6, Row: 100, Width: 4, Height: 1})
	require.NoError(t, err)
	assert.Equal(t, 2, px.Invalid, "columns 6 and 7 are the border")
	assert.Equal(t, 0.0, px.Values[0][0])
	assert.NotEqual(t, 0.0, px.Values[0][2])

	m, err := query[RegionArgs, Measurement](t, inst, opMeasure, RegionArgs{Col: 0, Row: 0, Width: 20, Height: 20})
	require.NoError(t, err)
	assert.EqualValues(t, 400-12*12, m.Planes[0].Invalid)
	assert.EqualValues(t, 12*12, m.Planes[0].Samples)
	assert.Greater(t, m.Planes[0].Min, float64(samples.ElevationInvalid), "the declared value is in no figure")
	assert.NotContains(t, m.Method, "4096", "a signed 16-bit plane has a bin per code")
	assert.Contains(t, m.Method, "declare invalid")

	sql := handoverSql(datasetAlias, inst.pic)
	assert.Contains(t, sql, "WHERE gray != -32768")
	assert.Contains(t, sql, "-- The stored value -32768 marks no measurement")
}

// The 32-bit integer and 64-bit float samples are binned, and every kind
// crosses to play in its own Arrow type.
func TestTheWideKindsAreBinnedAndCrossInTheirOwnType(t *testing.T) {
	inst := withSample(t, samples.NameCounts)
	m, err := query[RegionArgs, Measurement](t, inst, opMeasure, RegionArgs{Col: 0, Row: 0, Width: 32, Height: 32})
	require.NoError(t, err)
	assert.EqualValues(t, 16*16, m.Planes[0].Invalid)
	assert.Contains(t, m.Method, "4096 equal bins", "signed 32-bit is binned")
	assert.Contains(t, m.Method, "signed 32-bit integers")

	for _, tc := range []struct {
		name string
		typ  arrow.DataType
	}{
		{samples.NameElevation, arrow.PrimitiveTypes.Int16},
		{samples.NameCounts, arrow.PrimitiveTypes.Int32},
		{samples.NameDouble, arrow.PrimitiveTypes.Float64},
		{samples.NameFloat, arrow.PrimitiveTypes.Float32},
	} {
		img := sampleImage(t, tc.name)
		rect := image.Rect(10, 20, 13, 22)
		region, err := sampleStore(t, tc.name).Read(context.Background(), rect)
		require.NoError(t, err)
		stream, rows, err := pixelsArrow(region, rect.Min)
		require.NoError(t, err)
		assert.EqualValues(t, 6, rows)
		rd, err := ipc.NewReader(bytes.NewReader(stream))
		require.NoError(t, err)
		require.True(t, rd.Next())
		rec := rd.RecordBatch()
		assert.Equal(t, tc.typ, rec.Schema().Field(2).Type, tc.name)
		for i := range 6 {
			x, y := 10+i%3, 20+i/3
			want, _ := img.Plane(0).StoredAt(x, y)
			var got float64
			switch col := rec.Column(2).(type) {
			case *array.Int16:
				got = float64(col.Value(i))
			case *array.Int32:
				got = float64(col.Value(i))
			case *array.Float64:
				got = col.Value(i)
			case *array.Float32:
				got = float64(col.Value(i))
			}
			assert.Equal(t, want, got, "%s at (%d,%d)", tc.name, x, y)
		}
		rd.Release()
	}
	assert.Contains(t, handoverSql(datasetAlias, sampleStore(t, samples.NameDouble)), "WHERE NOT isNaN(gray)")
	assert.Contains(t, handoverSql(datasetAlias, sampleStore(t, samples.NameCounts)), "WHERE gray != -2147483648")
}

// An image taken from a stack names its planes by its bands everywhere the
// window names a plane: get_view, set_plane, read_pixels, measure and the
// columns sent to play.
func TestPlanesAreNamedByTheirLabels(t *testing.T) {
	st, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	img, err := st.Image([]int{0, 0}, []int{3, 2, 2})
	require.NoError(t, err)
	pic, err := tiles.FromImage(context.Background(), img, tiles.Options{})
	require.NoError(t, err)
	inst := testApp(t)
	inst.setPic(pic)

	v, err := query[appops.None, View](t, inst, opGetView, appops.None{})
	require.NoError(t, err)
	assert.Equal(t, []string{"B8", "B4", "B4"}, v.Image.Planes)
	require.NoError(t, command(t, inst, opSetPlane, SetPlaneArgs{Plane: "B8"}))
	v, _ = query[appops.None, View](t, inst, opGetView, appops.None{})
	assert.Equal(t, "B8", v.View.Plane)
	assert.Contains(t, refusal(t, command(t, inst, opSetPlane, SetPlaneArgs{Plane: "red"})), "this image has B8, B4, B4")

	px, err := query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{Col: 100, Row: 50})
	require.NoError(t, err)
	assert.Equal(t, []string{"B8", "B4", "B4"}, px.Planes)
	m, err := query[RegionArgs, Measurement](t, inst, opMeasure, RegionArgs{Col: 100, Row: 50, Width: 4, Height: 4})
	require.NoError(t, err)
	assert.Equal(t, "B4", m.Planes[1].Plane)

	assert.Equal(t, []string{"B8", "B4", "B4_2"}, columnNames([]string{"B8", "B4", "B4"}))
	assert.Equal(t, []string{"col_2", "row_2", "x"}, columnNames([]string{"col", "row", "x"}))
	sql := handoverSql(datasetAlias, pic)
	assert.Contains(t, sql, "SELECT 'B4' AS plane")
	assert.Contains(t, sql, "avg(B4_2)")
	assert.Contains(t, sql, "WHERE B8 != 0", "the bands declare 0 invalid")
	assert.Equal(t, "`865 nm`", quoteIdent("865 nm"))
	assert.Equal(t, "`a\\`b`", quoteIdent("a`b"))
	assert.Equal(t, "`1st`", quoteIdent("1st"))
	assert.Equal(t, "band_1", quoteIdent("band_1"))

	r, err := pic.Read(context.Background(), image.Rect(0, 0, 2, 2))
	require.NoError(t, err)
	stream, _, err := pixelsArrow(r, image.Point{})
	require.NoError(t, err)
	rd, err := ipc.NewReader(bytes.NewReader(stream))
	require.NoError(t, err)
	defer rd.Release()
	require.True(t, rd.Next())
	rec := rd.RecordBatch()
	assert.Equal(t, []string{"col", "row", "B8", "B4", "B4_2"}, []string{rec.ColumnName(0), rec.ColumnName(1), rec.ColumnName(2), rec.ColumnName(3), rec.ColumnName(4)})
}

func TestTheNewDerivationsAndAReadersNotesAreShown(t *testing.T) {
	assert.Contains(t, derivationNote(imaging.DerivationRowsFlipped), "bottom row first")
	assert.Contains(t, derivationNote(imaging.DerivationAxesReordered), "another order")
	notes := sourceNotes(imaging.Provenance{Derivations: imaging.DerivationRowsFlipped, Notes: []string{"BSCALE and BZERO are the value transform"}})
	require.Len(t, notes, 2)
	assert.Equal(t, "BSCALE and BZERO are the value transform", notes[1])
	for _, d := range imaging.AllDerivations {
		assert.NotEqual(t, "the reader changed the stored samples", derivationNote(d), "derivation %d has its own text", d)
	}
}
