package introspect

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sampleRow struct {
	name string
	n    int32
	tags []string
}

func sampleTable(rows []sampleRow) *Table {
	return NewTable().
		String("name", func(i int) string { return rows[i].name }).
		Int32("n", func(i int) int32 { return rows[i].n }).
		StringList("tags", func(i int) []string { return rows[i].tags })
}

func TestTable_Schema(t *testing.T) {
	sc := sampleTable(nil).Schema() // nil data is fine — Schema never calls a getter
	require.Equal(t, 3, sc.NumFields())
	assert.Equal(t, "name", sc.Field(0).Name)
	assert.Equal(t, "n", sc.Field(1).Name)
	assert.Equal(t, "tags", sc.Field(2).Name)
	assert.Equal(t, arrow.BinaryTypes.String, sc.Field(0).Type)
}

func TestTable_BuildAllColumns(t *testing.T) {
	rows := []sampleRow{{"a", 1, []string{"x"}}, {"b", 2, []string{"y", "z"}}}
	rec := sampleTable(rows).Build(AllColumns(), len(rows))
	defer rec.Release()
	assert.EqualValues(t, 3, rec.NumCols())
	assert.EqualValues(t, 2, rec.NumRows())
}

func TestTable_BuildProjected(t *testing.T) {
	rows := []sampleRow{{"a", 1, nil}}
	rec := sampleTable(rows).Build(Columns("name", "n"), len(rows))
	defer rec.Release()
	require.EqualValues(t, 2, rec.NumCols())
	assert.Equal(t, "name", rec.Schema().Field(0).Name)
	assert.Equal(t, "n", rec.Schema().Field(1).Name)
}

func TestTable_BuildEmptyProjectionFallsBackToAll(t *testing.T) {
	rows := []sampleRow{{"a", 1, nil}}
	// A projection that names no column this table has must never yield
	// a zero-column table (e.g. SELECT count(*) FROM t).
	rec := sampleTable(rows).Build(Columns("nonexistent"), len(rows))
	defer rec.Release()
	assert.EqualValues(t, 3, rec.NumCols())
}

func TestEncodeStream_RoundTrip(t *testing.T) {
	rows := []sampleRow{{"alpha", 10, []string{"p", "q"}}, {"beta", 20, nil}}
	rec := sampleTable(rows).Build(AllColumns(), len(rows))
	defer rec.Release()

	b, err := EncodeStream(rec)
	require.NoError(t, err)
	require.NotEmpty(t, b)

	rdr, err := ipc.NewReader(bytes.NewReader(b), ipc.WithAllocator(memory.DefaultAllocator))
	require.NoError(t, err)
	defer rdr.Release()
	require.True(t, rdr.Next())
	got := rdr.RecordBatch()
	require.EqualValues(t, 3, got.NumCols())
	require.EqualValues(t, 2, got.NumRows())

	names := got.Column(0).(*array.String)
	assert.Equal(t, "alpha", names.Value(0))
	assert.Equal(t, "beta", names.Value(1))
	ns := got.Column(1).(*array.Int32)
	assert.EqualValues(t, 10, ns.Value(0))
	assert.EqualValues(t, 20, ns.Value(1))
}

func TestEncodeFile_HasArrowMagic(t *testing.T) {
	rows := []sampleRow{{"a", 1, nil}}
	rec := sampleTable(rows).Build(AllColumns(), len(rows))
	defer rec.Release()
	b, err := EncodeFile(rec)
	require.NoError(t, err)
	// Arrow IPC file format begins with the "ARROW1" magic; ArrowStream
	// does not. This is what distinguishes the file('...','Arrow') feed
	// from the ArrowStream HTTP body.
	require.GreaterOrEqual(t, len(b), 6)
	assert.Equal(t, "ARROW1", string(b[:6]))
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	p := &stubProvider{name: "env"}
	require.NoError(t, r.Register(p))

	// duplicate name rejected
	require.Error(t, r.Register(&stubProvider{name: "env"}))
	// invalid name rejected
	require.Error(t, r.Register(&stubProvider{name: "bad name"}))

	got, ok := r.Lookup("env")
	require.True(t, ok)
	assert.Equal(t, p, got)

	_, ok = r.Lookup("missing")
	assert.False(t, ok)

	require.NoError(t, r.Register(&stubProvider{name: "apps"}))
	assert.Equal(t, []string{"apps", "env"}, r.Names(), "Names must be sorted")
	require.Len(t, r.Providers(), 2)
}

func TestValidTableName(t *testing.T) {
	for _, n := range []string{"env", "apps", "_x", "T1"} {
		assert.True(t, validTableName(n), "want valid: %q", n)
	}
	for _, n := range []string{"", "1x", "a-b", "a b", "a.b", "system "} {
		assert.False(t, validTableName(n), "want invalid: %q", n)
	}
}

// stubProvider is a minimal Provider for registry tests.
type stubProvider struct{ name string }

func (s *stubProvider) Name() string            { return s.name }
func (s *stubProvider) Schema() *arrow.Schema    { return NewTable().String("x", func(int) string { return "" }).Schema() }
func (s *stubProvider) Freshness() FreshnessClass { return FreshnessStatic }
func (s *stubProvider) Snapshot(proj Projection) (arrow.RecordBatch, error) {
	return NewTable().String("x", func(int) string { return "" }).Build(proj, 0), nil
}
