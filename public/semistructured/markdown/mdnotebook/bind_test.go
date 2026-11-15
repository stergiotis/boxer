package mdnotebook_test

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

// oneRow is a record of one Float64 column and one row.
func oneRow(t *testing.T, column string, v float64) arrow.RecordBatch {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: column, Type: arrow.PrimitiveTypes.Float64}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	b.Field(0).(*array.Float64Builder).Append(v)
	return b.NewRecordBatch()
}

func recordsOf(name string, rec arrow.RecordBatch) map[string]arrow.RecordBatch {
	return map[string]arrow.RecordBatch{name: rec}
}

// boundText is a notebook as a programme writes one to show what it holds:
// no frontmatter, no record cell, a figure with a placeholder for a label.
func boundText(t *testing.T) []byte {
	t.Helper()
	var w mdnotebook.Writer
	w.Heading(1, "One reading")
	w.Markdown("Lag was " + mdnotebook.Figure("stats", "p99", "gloss/duration;unit=ms") + ".")
	w.Cell("table", "SELECT * FROM stats")
	require.NoError(t, w.Err())
	return append([]byte(nil), w.Bytes()...)
}

// A bound notebook lists no record and owes none: what the frontmatter does
// not list is a problem only when the host did not bind it either.
func TestBindAnswersFromTheRecordsHandedOver(t *testing.T) {
	nb, err := mdnotebook.Read(boundText(t))
	require.NoError(t, err)
	require.Len(t, nb.Problems, 1, "read alone, the figure names a record nothing lists")

	rec := oneRow(t, "p99", 412.3)
	ld := mdnotebook.Bind(nb, recordsOf("stats", rec), nil)
	rec.Release() // the binding holds its own reference
	defer ld.Release()
	assert.True(t, ld.Bound)
	assert.Empty(t, mdnotebook.Problems(nb, ld), "a placeholder label is not held to the value")
	assert.Empty(t, mdnotebook.StaleRecords(nb, ld))

	res, err := mdnotebook.Evaluate(nb.Cells[1].Body, ld.Records)
	require.NoError(t, err)
	defer res.Release()
	assert.EqualValues(t, 1, res.Main.NumRows())

	// Bound to nothing, the record is missing and said so in the host's terms.
	none := mdnotebook.Bind(nb, nil, nil)
	defer none.Release()
	problems := mdnotebook.Problems(nb, none)
	require.Len(t, problems, 1)
	assert.Equal(t, "the record stats is not bound", problems[0].Text)
}

// A reader of a bound notebook draws a figure's value where the text holds
// a placeholder; a figure that has no value to draw keeps what was written.
func TestRelabelDrawsTheBoundValue(t *testing.T) {
	nb, err := mdnotebook.Read(boundText(t))
	require.NoError(t, err)
	rec := oneRow(t, "p99", 412.3)
	defer rec.Release()

	text, changed := mdnotebook.Relabel(nb, recordsOf("stats", rec), nil)
	require.True(t, changed)
	assert.Contains(t, string(text), `Lag was [412.3 ms](val:stats.p99 "gloss/duration;unit=ms").`)

	again, err := mdnotebook.Read(text)
	require.NoError(t, err)
	_, changed = mdnotebook.Relabel(again, recordsOf("stats", rec), nil)
	assert.False(t, changed, "a label that is its value's rendering is left alone")

	other := oneRow(t, "p50", 1)
	defer other.Release()
	text, changed = mdnotebook.Relabel(nb, recordsOf("stats", other), nil)
	assert.False(t, changed)
	assert.Equal(t, string(nb.Src), string(text))
}

// Sealing a bound notebook stores what was bound: the text gains the
// frontmatter and the labels, and reads back sealed from the blobs.
func TestSealStoresABoundNotebook(t *testing.T) {
	nb, err := mdnotebook.Read(boundText(t))
	require.NoError(t, err)
	// Schema metadata is the writer's and must not name the blob.
	plain := oneRow(t, "p99", 412.3)
	defer plain.Release()
	meta := arrow.MetadataFrom(map[string]string{"writer": "someone"})
	tagged := array.NewRecordBatch(arrow.NewSchema(plain.Schema().Fields(), &meta), plain.Columns(), plain.NumRows())
	defer tagged.Release()

	out, err := mdnotebook.Seal(nb, recordsOf("stats", tagged), "r.blobs", nil)
	require.NoError(t, err)
	same, err := mdnotebook.Seal(nb, recordsOf("stats", plain), "r.blobs", nil)
	require.NoError(t, err)
	assert.Equal(t, out.Records, same.Records, "the same rows are the same blob")
	require.Len(t, out.Records, 1)
	assert.Empty(t, out.Records[0].Of, "a record with no cell has no statement to hash")

	sealed, err := mdnotebook.Read(out.Text)
	require.NoError(t, err)
	assert.Empty(t, sealed.Problems)
	ld := mdnotebook.Load(sealed, blobFS(out))
	defer ld.Release()
	assert.False(t, ld.Bound)
	assert.Empty(t, mdnotebook.Problems(sealed, ld), string(out.Text))
	assert.Contains(t, string(out.Text), "[412.3 ms](val:stats.p99")

	_, err = mdnotebook.Seal(nb, recordsOf("not a name", plain), "r.blobs", nil)
	assert.ErrorContains(t, err, "identifier")
}
