package promptbook

import (
	"math"
	"testing"
	"testing/fstest"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// The table lists every document of every registered book, a failed one
// as a row with its error, and spells the purpose the way a call does.
func TestLlmPromptsTable(t *testing.T) {
	require.NoError(t, Register("test-table", fstest.MapFS{
		"good.md": {Data: []byte("---\ntitle: T\nsummary: S\nscope: buffer\ntemperature: 0.5\n---\nbody\n")},
		"bad.md":  {Data: []byte("---\nsummary: S\n---\nbody\n")},
	}))
	reg := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(reg))
	p, ok := reg.Lookup(TableName)
	require.True(t, ok)
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()

	col := func(name string) int {
		for i, f := range rec.Schema().Fields() {
			if f.Name == name {
				return i
			}
		}
		t.Fatalf("no column %q", name)
		return -1
	}
	books := rec.Column(col("book")).(*array.String)
	purposes := rec.Column(col("purpose")).(*array.String)
	scopes := rec.Column(col("scope")).(*array.String)
	temps := rec.Column(col("temperature")).(*array.Float64)
	perrs := rec.Column(col("parse_error")).(*array.String)
	var good, bad bool
	for i := 0; i < int(rec.NumRows()); i++ {
		if books.Value(i) != "test-table" {
			continue
		}
		switch purposes.Value(i) {
		case "test-table/good":
			good = true
			assert.Equal(t, "buffer", scopes.Value(i))
			assert.InDelta(t, 0.5, temps.Value(i), 1e-6)
			assert.Empty(t, perrs.Value(i))
		case "":
			bad = true
			assert.Contains(t, perrs.Value(i), "title")
			assert.True(t, math.IsNaN(temps.Value(i)))
		}
	}
	assert.True(t, good, "the good document is a row")
	assert.True(t, bad, "the failed document is a row with its error")
}
