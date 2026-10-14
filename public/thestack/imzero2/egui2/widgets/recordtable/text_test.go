package recordtable

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stretchr/testify/assert"
)

func densityOf(d uint8) styletokens.DensityE { return styletokens.DensityE(d) }

// FitRunes bounds what a fit-frame cell hands egui_table to measure.
func TestFitRunes(t *testing.T) {
	assert.Equal(t, "abc", FitRunes("abc", 3))
	assert.Equal(t, "ab…", FitRunes("abcd", 2))
	assert.Equal(t, "••…", FitRunes("••••", 2), "runes, not bytes")
	assert.Equal(t, "", FitRunes("", 5))
}

// StringLike left-aligns columns that render as free text: plain, large and
// dictionary-encoded strings, and nothing else.
func TestStringLike(t *testing.T) {
	dictStr := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int8, ValueType: arrow.BinaryTypes.String}
	dictLarge := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int8, ValueType: arrow.BinaryTypes.LargeString}
	dictInt := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int8, ValueType: arrow.PrimitiveTypes.Int64}
	cases := []struct {
		name string
		dt   arrow.DataType
		want bool
	}{
		{"string", arrow.BinaryTypes.String, true},
		{"large_string", arrow.BinaryTypes.LargeString, true},
		{"dict_string", dictStr, true}, // ClickHouse LowCardinality(String)
		{"dict_large_string", dictLarge, true},
		{"dict_int", dictInt, false},
		{"int64", arrow.PrimitiveTypes.Int64, false},
		{"float64", arrow.PrimitiveTypes.Float64, false},
		{"binary", arrow.BinaryTypes.Binary, false}, // rendered as hex, reads like a number
		// A List column shows a packed "[len=N]" marker in a per-row grid,
		// not the strings themselves, so it is not left-aligned there.
		{"list_string", arrow.ListOf(arrow.BinaryTypes.String), false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, StringLike(tc.dt), tc.name)
	}
}

// StringRecord pads a short row and keeps the rows in order.
func TestStringRecord(t *testing.T) {
	rec := StringRecord([]string{"path", "size"}, [][]string{{"/a", "1"}, {"/b"}})
	defer rec.Release()
	assert.Equal(t, int64(2), rec.NumRows())
	assert.Equal(t, "size", rec.Schema().Field(1).Name)
	assert.Equal(t, "", rec.Column(1).ValueStr(1), "a short row is padded")
	assert.Equal(t, "/b", rec.Column(0).ValueStr(1))
}
