package play

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/hmi/gloss"
)

// formatCell turns (col, row) of a RecordBatch into a display string.
// Returns the empty string for NULL or out-of-range.
func formatCell(rec arrow.RecordBatch, col int, row int64) string {
	return formatArrayElem(rec.Column(col), row)
}

// formatArrayElem formats the row-th element of an arbitrary Arrow array as a
// display string, empty for NULL or out-of-range: gloss.FormatArrowElem, the
// un-glossed rendering every grid falls back to (ADR-0186). formatCell is the
// top-level (rec, col) entry point; the per-attribute leeway view
// (play_table_attr.go) reuses this on the *inner* array of a List value column
// to render a single exploded scalar, so an exploded cell reads exactly as its
// per-DB-row cell.
func formatArrayElem(arr arrow.Array, row int64) string {
	return gloss.FormatArrowElem(arr, row)
}

// stringLikeArrowType reports whether values of this Arrow type render as free
// text: plain and large UTF-8 strings, and dictionary-encoded strings
// (ClickHouse LowCardinality(String) arrives as a dictionary). The Table pane
// left-aligns such columns so their text lines up under the left-aligned header,
// where numeric columns read fine centered. This is deliberately its own
// predicate, not shared with the world panel's country detector
// (isWorldStringType): the two ask independent questions — "does this render as
// text?" vs. "could this hold a country name?" — that only coincide today.
func stringLikeArrowType(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.STRING, arrow.LARGE_STRING:
		return true
	case arrow.DICTIONARY:
		if d, ok := dt.(*arrow.DictionaryType); ok {
			return stringLikeArrowType(d.ValueType)
		}
	}
	return false
}

// shortArrowType renders an Arrow type as a compact tag for a column header:
// `list<item: float32>` becomes `[f32]`, `utf8` becomes `str`.
//
// This is a layout decision as much as a display one. egui_table fits a column
// to the widest thing in it *including the header*, so whatever the header
// renders sets a floor under the column's width — and the full type string is
// routinely three times the width of the values below it, which is how a
// column of `[len=1]` cells ends up 270px wide. The header keeps the full type
// on hover, so nothing is lost, only moved.
//
// An unrecognised type falls through to its own String(): a long tag is better
// than a wrong one, and the width cost lands only on types this does not know.
func shortArrowType(dt arrow.DataType) string {
	switch t := dt.(type) {
	case *arrow.ListType:
		return "[" + shortArrowType(t.Elem()) + "]"
	case *arrow.LargeListType:
		return "[" + shortArrowType(t.Elem()) + "]"
	case *arrow.FixedSizeListType:
		return "[" + shortArrowType(t.Elem()) + "]"
	case *arrow.DictionaryType:
		// The dictionary encoding is a storage detail — CH LowCardinality(String)
		// arrives this way and reads as its value type to anyone writing SQL.
		return shortArrowType(t.ValueType)
	}
	switch dt.ID() {
	case arrow.BOOL:
		return "bool"
	case arrow.INT8:
		return "i8"
	case arrow.INT16:
		return "i16"
	case arrow.INT32:
		return "i32"
	case arrow.INT64:
		return "i64"
	case arrow.UINT8:
		return "u8"
	case arrow.UINT16:
		return "u16"
	case arrow.UINT32:
		return "u32"
	case arrow.UINT64:
		return "u64"
	case arrow.FLOAT16:
		return "f16"
	case arrow.FLOAT32:
		return "f32"
	case arrow.FLOAT64:
		return "f64"
	case arrow.STRING, arrow.LARGE_STRING:
		return "str"
	case arrow.BINARY, arrow.LARGE_BINARY, arrow.FIXED_SIZE_BINARY:
		return "bin"
	case arrow.DATE32, arrow.DATE64:
		return "date"
	case arrow.TIMESTAMP:
		return "ts"
	case arrow.TIME32, arrow.TIME64:
		return "time"
	case arrow.DECIMAL128, arrow.DECIMAL256:
		return "dec"
	case arrow.NULL:
		return "null"
	}
	return dt.String()
}

// listElemType returns a list type's element type, or dt unchanged when it is
// not a list. The per-attribute Table view explodes each list value down its
// own rows, so a value column's element type — not the outer List — is what
// each rendered cell actually shows; classify its alignment against that.
func listElemType(dt arrow.DataType) arrow.DataType {
	switch t := dt.(type) {
	case *arrow.ListType:
		return t.Elem()
	case *arrow.LargeListType:
		return t.Elem()
	case *arrow.FixedSizeListType:
		return t.Elem()
	}
	return dt
}
