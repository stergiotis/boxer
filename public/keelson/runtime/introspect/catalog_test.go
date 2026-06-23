package introspect

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stringCol(t *testing.T, rec arrow.RecordBatch, col string) (out []string) {
	t.Helper()
	idx := rec.Schema().FieldIndices(col)
	require.NotEmpty(t, idx, "column %q not found", col)
	a := rec.Column(idx[0]).(*array.String)
	for i := range a.Len() {
		out = append(out, a.Value(i))
	}
	return
}

func TestCatalog_TablesListsEveryProviderIncludingItself(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&stubProvider{name: "env"}))
	require.NoError(t, RegisterCatalog(r))

	tp, ok := r.Lookup("tables")
	require.True(t, ok)
	rec, err := tp.Snapshot(AllColumns())
	require.NoError(t, err)
	defer rec.Release()

	assert.ElementsMatch(t, []string{"columns", "env", "tables"}, stringCol(t, rec, "name"))
	assert.EqualValues(t, 3, rec.NumCols()) // name, freshness, column_count
}

func TestCatalog_ColumnsFlattensSchemas(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&stubProvider{name: "env"})) // 1 column ("x")
	require.NoError(t, RegisterCatalog(r))

	cp, ok := r.Lookup("columns")
	require.True(t, ok)
	rec, err := cp.Snapshot(AllColumns())
	require.NoError(t, err)
	defer rec.Release()

	// env(1) + tables(3) + columns(4) = 8 (table,column) pairs.
	assert.EqualValues(t, 8, rec.NumRows())
	assert.Contains(t, stringCol(t, rec, "table"), "env")
	assert.Contains(t, stringCol(t, rec, "column"), "name") // a column of `tables`
}

func TestCatalog_Projection(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, RegisterCatalog(r))
	tp, _ := r.Lookup("tables")
	rec, err := tp.Snapshot(Columns("name"))
	require.NoError(t, err)
	defer rec.Release()
	require.EqualValues(t, 1, rec.NumCols())
	assert.Equal(t, "name", rec.Schema().Field(0).Name)
}
