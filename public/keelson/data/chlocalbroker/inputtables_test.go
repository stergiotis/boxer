package chlocalbroker

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// arrowFileI32 builds a single-column Int32 Arrow IPC file (footer
// form — the 'Arrow' format, not ArrowStream) for use as an InputTable.
func arrowFileI32(t *testing.T, col string, vals []int32) (b []byte) {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: col, Type: arrow.PrimitiveTypes.Int32}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	for _, v := range vals {
		rb.Field(0).(*array.Int32Builder).Append(v)
	}
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w, err := ipc.NewFileWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(memory.DefaultAllocator))
	require.NoError(t, err)
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	b = buf.Bytes()
	return
}

func TestValidInputTableName(t *testing.T) {
	for _, n := range []string{"t", "env", "_x", "Table1", "a_b_c", "T123"} {
		assert.True(t, validInputTableName(n), "want valid: %q", n)
	}
	for _, n := range []string{"", "1t", "a-b", "a b", "a;b", "a.b", "drop table", "a/b", "ä"} {
		assert.False(t, validInputTableName(n), "want invalid: %q", n)
	}
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'a'
	}
	assert.False(t, validInputTableName(string(long)), "65 bytes must be rejected")
	assert.True(t, validInputTableName(string(long[:64])), "64 bytes is allowed")
}

func TestSqlQuoteString(t *testing.T) {
	assert.Equal(t, `'/tmp/x.arrow'`, sqlQuoteString("/tmp/x.arrow"))
	assert.Equal(t, `'a\'b'`, sqlQuoteString("a'b"))
	assert.Equal(t, `'a\\b'`, sqlQuoteString(`a\b`))
}

func TestFoldInputTables(t *testing.T) {
	base := computeCacheKey("SELECT 1", "TabSeparated", nil)
	assert.Equal(t, base, foldInputTables(base, nil), "no input tables → base unchanged")

	a := foldInputTables(base, map[string][]byte{"t": []byte("aaa")})
	b := foldInputTables(base, map[string][]byte{"t": []byte("bbb")})
	assert.NotEqual(t, a, b, "different content must change the key")
	assert.NotEqual(t, base, a, "presence of an input table must change the key")

	k1 := foldInputTables(base, map[string][]byte{"a": []byte("1"), "b": []byte("2")})
	k2 := foldInputTables(base, map[string][]byte{"b": []byte("2"), "a": []byte("1")})
	assert.Equal(t, k1, k2, "map iteration order must not affect the key")
}

func TestExecOnPool_InputTablesRoundTrip(t *testing.T) {
	_, caller := newTestBroker(t)
	data := arrowFileI32(t, "n", []int32{10, 20, 30})
	rep, err := ExecOnPool(context.Background(), caller, "introspect", ExecRequest{
		SQL:         "SELECT count() AS c, sum(n) AS s FROM mytab",
		Format:      "TabSeparated",
		InputTables: map[string][]byte{"mytab": data},
	})
	require.NoError(t, err)
	require.NoError(t, rep.Err())
	body, err := io.ReadAll(rep)
	require.NoError(t, err)
	assert.Equal(t, "3\t60\n", string(body))
	require.NoError(t, rep.Close())
}

func TestExecOnPool_InputTablesJoin(t *testing.T) {
	_, caller := newTestBroker(t)
	rep, err := ExecOnPool(context.Background(), caller, "introspect", ExecRequest{
		SQL:    "SELECT count() AS c FROM a JOIN b ON a.n = b.n",
		Format: "TabSeparated",
		InputTables: map[string][]byte{
			"a": arrowFileI32(t, "n", []int32{1, 2, 3}),
			"b": arrowFileI32(t, "n", []int32{2, 3, 4}),
		},
	})
	require.NoError(t, err)
	require.NoError(t, rep.Err())
	body, _ := io.ReadAll(rep)
	assert.Equal(t, "2\n", string(body), "inner join {1,2,3}∩{2,3,4} = {2,3}")
	require.NoError(t, rep.Close())
}

func TestExecOnPool_InputTablesCacheRespectsContent(t *testing.T) {
	_, caller := newTestBroker(t)
	const sql = "SELECT sum(n) AS s FROM t"
	v1 := arrowFileI32(t, "n", []int32{1, 2})   // sum 3
	v2 := arrowFileI32(t, "n", []int32{10, 20}) // sum 30

	r1, err := ExecOnPool(context.Background(), caller, "in_cache", ExecRequest{
		SQL: sql, Format: "TabSeparated", Cacheable: true,
		InputTables: map[string][]byte{"t": v1},
	})
	require.NoError(t, err)
	require.NoError(t, r1.Err())
	b1, _ := io.ReadAll(r1)
	assert.Equal(t, "3\n", string(b1))
	assert.False(t, r1.CacheHit, "first call is a miss")
	require.NoError(t, r1.Close())

	// Same SQL, different content → must reflect the new data, not the
	// stale hit (the content folds into the cache key — ADR-0094 §SD5).
	r2, err := ExecOnPool(context.Background(), caller, "in_cache", ExecRequest{
		SQL: sql, Format: "TabSeparated", Cacheable: true,
		InputTables: map[string][]byte{"t": v2},
	})
	require.NoError(t, err)
	require.NoError(t, r2.Err())
	b2, _ := io.ReadAll(r2)
	assert.Equal(t, "30\n", string(b2), "changed input must be reflected")
	assert.False(t, r2.CacheHit, "changed content must miss")
	require.NoError(t, r2.Close())

	// Identical SQL + identical bytes → hit.
	r3, err := ExecOnPool(context.Background(), caller, "in_cache", ExecRequest{
		SQL: sql, Format: "TabSeparated", Cacheable: true,
		InputTables: map[string][]byte{"t": v2},
	})
	require.NoError(t, err)
	require.NoError(t, r3.Err())
	_, _ = io.ReadAll(r3)
	assert.True(t, r3.CacheHit, "identical input + SQL must hit")
	require.NoError(t, r3.Close())
}

func TestExecOnPool_InvalidInputTableNameRejected(t *testing.T) {
	_, caller := newTestBroker(t)
	rep, err := ExecOnPool(context.Background(), caller, "bad_in", ExecRequest{
		SQL:         "SELECT 1",
		Format:      "TabSeparated",
		InputTables: map[string][]byte{"bad; DROP": []byte("x")},
	})
	require.NoError(t, err, "bus request itself succeeds; the rejection surfaces via rep.Err()")
	require.NotNil(t, rep)
	require.Error(t, rep.Err())
	assert.Contains(t, rep.Err().Error(), "invalid input table name")
}
