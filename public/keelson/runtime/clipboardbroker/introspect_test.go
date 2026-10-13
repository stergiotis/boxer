package clipboardbroker

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// Each copy reads as its verdict, and no column carries the handle.
func TestCopiesTable(t *testing.T) {
	at := time.UnixMilli(1_000)
	rows := []CopyRecord{
		{Digest: "d1", Plaintext: true, At: at, From: "a"},
		{Digest: "d2", Handle: HandlePrefix + "secret", At: at, From: "a"},
		{Digest: "d3", Refused: true, Reason: "red", At: at, From: "b"},
	}
	rec := copiesTable(rows).Build(introspect.AllColumns(), len(rows))
	defer rec.Release()
	idx := rec.Schema().FieldIndices("verdict")
	require.Len(t, idx, 1)
	col := rec.Column(idx[0]).(*array.String)
	var got []string
	for i := 0; i < col.Len(); i++ {
		got = append(got, col.Value(i))
	}
	assert.Equal(t, []string{"plaintext", "handle", "refused"}, got)
	for _, f := range rec.Schema().Fields() {
		assert.NotEqual(t, "handle", f.Name, "the handle resolves to the text")
	}

	empty, err := copiesProvider{}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer empty.Release()
	assert.EqualValues(t, 0, empty.NumRows())
}
