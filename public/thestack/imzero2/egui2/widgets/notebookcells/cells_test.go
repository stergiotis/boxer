package notebookcells_test

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookcells"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

func frame(v *notebookview.View) notebookview.Events {
	defer c.CurrentApplicationState.StateManager.ScriptReset()
	return v.Render()
}

// dates is (kind, begins, ends): two instants of one kind and an interval.
func dates(t *testing.T) arrow.RecordBatch {
	t.Helper()
	ts := &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "kind", Type: arrow.BinaryTypes.String},
		{Name: "begins", Type: ts},
		{Name: "ends", Type: ts, Nullable: true},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	at := func(day int) arrow.Timestamp {
		return arrow.Timestamp(time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC).UnixMilli())
	}
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"seen", "seen", "valid"}, nil)
	b.Field(1).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{at(2), at(9), at(1)}, nil)
	b.Field(2).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{0, 0, at(20)}, []bool{false, false, true})
	return b.NewRecordBatch()
}

// places is (name, latitude, longitude).
func places(t *testing.T) arrow.RecordBatch {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "latitude", Type: arrow.PrimitiveTypes.Float32},
		{Name: "longitude", Type: arrow.PrimitiveTypes.Float32},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"Bern", "Lisbon", "nowhere"}, nil)
	b.Field(1).(*array.Float32Builder).AppendValues([]float32{46.948, 38.722, 200}, nil)
	b.Field(2).(*array.Float32Builder).AppendValues([]float32{7.447, -9.139, 0}, nil)
	return b.NewRecordBatch()
}

// Both renderers draw a relation fitted to them by a cell's aliases, and
// say what they read when the columns are not there.
func TestMarksAndPointsDraw(t *testing.T) {
	t.Cleanup(egui2test.Install())
	d, p := dates(t), places(t)
	defer d.Release()
	defer p.Release()
	records := map[string]arrow.RecordBatch{"dates": d, "places": p}

	var w mdnotebook.Writer
	w.Heading(2, "When")
	w.Cell(notebookcells.RendererMarks, "SELECT begins AS "+notebookcells.ColTime+", ends AS "+notebookcells.ColTimeEnd+", kind AS "+notebookcells.ColLabel+" FROM dates")
	w.Heading(2, "Where")
	w.Cell(notebookcells.RendererPoints, "WITH cell_opts AS (SELECT 180 AS height)\nSELECT latitude AS lat, longitude AS lon, name AS label FROM places")
	w.Heading(2, "Not fitted")
	w.Cell(notebookcells.RendererMarks, "SELECT * FROM dates")
	w.Cell(notebookcells.RendererPoints, "SELECT * FROM places")
	require.NoError(t, w.Err())
	nb, err := mdnotebook.Read(w.Bytes())
	require.NoError(t, err)

	assert.Subset(t, notebookview.RegisteredRenderers(), []string{notebookcells.RendererMarks, notebookcells.RendererPoints})
	prepared := notebookview.PrepareFrom(nb, notebookview.Source{Records: records})
	assert.True(t, prepared.Check().Clean(), "%+v", prepared.Check())
	v := notebookview.New(c.NewWidgetIdStack(), "t", notebookview.Options{Plain: true, Statements: notebookview.StatementsHidden})
	v.Set(prepared)
	for range 4 {
		frame(v)
	}
	// The same cells over other rows: the renderers are kept and read again.
	fewer := map[string]arrow.RecordBatch{"dates": d.NewSlice(0, 1), "places": p.NewSlice(1, 2)}
	v.Set(notebookview.PrepareFrom(nb, notebookview.Source{Records: fewer}))
	for range 3 {
		frame(v)
	}
	for _, r := range fewer {
		r.Release()
	}
	// Closing the view stops the map's workers.
	v.Close()
}
