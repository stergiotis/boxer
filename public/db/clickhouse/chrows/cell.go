// Package chrows reads a ClickHouse Arrow result into Go values (ADR-0257):
// per-cell readers that absorb the several Arrow spellings one ClickHouse
// type arrives in, and [Decode] / [DecodeStream], which lay a whole result
// into a struct of column slices tagged `ch:"<column>"`. [EncodeStream] is
// the inverse over the same struct, for fixtures and for publishers.
//
// The readers own the mechanics — which Arrow arrays hold an integer, a text,
// a moment — and nothing else: a caller's policy (a NaN counted as missing, a
// text made UTF-8-safe for a wire) stays with the caller.
package chrows

import (
	"math"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// IsInteger reports the signed and unsigned integer types.
func IsInteger(dt arrow.DataType) (ok bool) {
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64:
		ok = true
	}
	return
}

// IsNumeric reports the integer and floating-point types. Decimals are
// not included; [IsDecimal] asks for them.
func IsNumeric(dt arrow.DataType) (ok bool) {
	switch dt.ID() {
	case arrow.FLOAT16, arrow.FLOAT32, arrow.FLOAT64:
		ok = true
	default:
		ok = IsInteger(dt)
	}
	return
}

// IsDecimal reports the decimal types ClickHouse's Decimal(P, S) arrives as.
func IsDecimal(dt arrow.DataType) (ok bool) {
	switch dt.ID() {
	case arrow.DECIMAL32, arrow.DECIMAL64, arrow.DECIMAL128, arrow.DECIMAL256:
		ok = true
	}
	return
}

// IsStringLike reports the types [String] reads: UTF-8 and binary in their
// plain, large and view layouts. ClickHouse writes a String as binary unless
// output_format_arrow_string_as_string is set, so both spellings count.
// Dictionary-encoded values are not included; [ValueType] unwraps them.
func IsStringLike(dt arrow.DataType) (ok bool) {
	switch dt.ID() {
	case arrow.STRING, arrow.LARGE_STRING, arrow.STRING_VIEW,
		arrow.BINARY, arrow.LARGE_BINARY, arrow.BINARY_VIEW:
		ok = true
	}
	return
}

// ValueType is dt with a dictionary encoding removed — the type a
// LowCardinality column reads as.
func ValueType(dt arrow.DataType) (vt arrow.DataType) {
	vt = dt
	if d, ok := dt.(*arrow.DictionaryType); ok {
		vt = d.ValueType
	}
	return
}

// deref resolves a dictionary cell to its value array and index. ok is
// false for a NULL, a row out of range, or a NULL dictionary entry.
func deref(arr arrow.Array, row int) (a arrow.Array, i int, ok bool) {
	if arr == nil || row < 0 || row >= arr.Len() || arr.IsNull(row) {
		return
	}
	a, i = arr, row
	if d, isDict := arr.(*array.Dictionary); isDict {
		a, i = d.Dictionary(), d.GetValueIndex(row)
		if a.IsNull(i) {
			return
		}
	}
	ok = true
	return
}

// Int64 reads an integer cell. ok is false for a NULL, a non-integer
// array, and a UInt64 above math.MaxInt64.
func Int64(arr arrow.Array, row int) (v int64, ok bool) {
	a, i, ok := deref(arr, row)
	if !ok {
		return
	}
	switch c := a.(type) {
	case *array.Int8:
		v = int64(c.Value(i))
	case *array.Int16:
		v = int64(c.Value(i))
	case *array.Int32:
		v = int64(c.Value(i))
	case *array.Int64:
		v = c.Value(i)
	case *array.Uint8:
		v = int64(c.Value(i))
	case *array.Uint16:
		v = int64(c.Value(i))
	case *array.Uint32:
		v = int64(c.Value(i))
	case *array.Uint64:
		u := c.Value(i)
		if u > math.MaxInt64 {
			return 0, false
		}
		v = int64(u)
	default:
		ok = false
	}
	return
}

// Uint64 reads an integer cell. ok is false for a NULL, a non-integer
// array, and a negative value.
func Uint64(arr arrow.Array, row int) (v uint64, ok bool) {
	if u, isU := arr.(*array.Uint64); isU {
		if row < 0 || row >= u.Len() || u.IsNull(row) {
			return
		}
		return u.Value(row), true
	}
	s, ok := Int64(arr, row)
	if !ok || s < 0 {
		return 0, false
	}
	v = uint64(s)
	return
}

// Float64 reads a numeric or decimal cell. ok is false for a NULL and a
// non-numeric array; a NaN is a value, ok true.
func Float64(arr arrow.Array, row int) (v float64, ok bool) {
	a, i, ok := deref(arr, row)
	if !ok {
		return
	}
	switch c := a.(type) {
	case *array.Float64:
		v = c.Value(i)
	case *array.Float32:
		v = float64(c.Value(i))
	case *array.Float16:
		v = float64(c.Value(i).Float32())
	case *array.Uint64:
		v = float64(c.Value(i))
	case *array.Decimal32:
		v = c.Value(i).ToFloat64(scaleOf(c.DataType()))
	case *array.Decimal64:
		v = c.Value(i).ToFloat64(scaleOf(c.DataType()))
	case *array.Decimal128:
		v = c.Value(i).ToFloat64(scaleOf(c.DataType()))
	case *array.Decimal256:
		v = c.Value(i).ToFloat64(scaleOf(c.DataType()))
	default:
		var s int64
		s, ok = Int64(a, i)
		v = float64(s)
	}
	return
}

func scaleOf(dt arrow.DataType) (scale int32) {
	if d, ok := dt.(arrow.DecimalType); ok {
		scale = d.GetScale()
	}
	return
}

// Bool reads a flag cell: a Boolean, or an integer read as non-zero —
// ClickHouse's `1 AS flag` arrives as UInt8. ok is false for a NULL and
// any other array.
func Bool(arr arrow.Array, row int) (v bool, ok bool) {
	a, i, ok := deref(arr, row)
	if !ok {
		return
	}
	if b, isBool := a.(*array.Boolean); isBool {
		return b.Value(i), true
	}
	if u, isU := a.(*array.Uint64); isU {
		return u.Value(i) != 0, true
	}
	s, ok := Int64(a, i)
	v = s != 0
	return
}

// String reads a text or binary cell. The result aliases the array's
// buffer: it is valid while the array is retained, and a caller that keeps
// it longer clones it (strings.Clone). The bytes are not checked for UTF-8.
// ok is false for a NULL and any other array.
func String(arr arrow.Array, row int) (s string, ok bool) {
	a, i, ok := deref(arr, row)
	if !ok {
		return
	}
	switch c := a.(type) {
	case *array.String:
		s = c.Value(i)
	case *array.LargeString:
		s = c.Value(i)
	case *array.StringView:
		s = c.Value(i)
	case *array.Binary:
		s = c.ValueString(i)
	case *array.LargeBinary:
		s = c.ValueString(i)
	case *array.BinaryView:
		s = c.ValueString(i)
	default:
		ok = false
	}
	return
}

// Bytes reads a binary or text cell, and a FixedString (fixed-size
// binary). The slice aliases the array's buffer, as [String]'s result does.
func Bytes(arr arrow.Array, row int) (b []byte, ok bool) {
	a, i, ok := deref(arr, row)
	if !ok {
		return
	}
	switch c := a.(type) {
	case *array.Binary:
		b = c.Value(i)
	case *array.LargeBinary:
		b = c.Value(i)
	case *array.BinaryView:
		b = c.Value(i)
	case *array.FixedSizeBinary:
		b = c.Value(i)
	default:
		var s string
		s, ok = String(a, i)
		b = []byte(s)
	}
	return
}

// TimestampToEpochMillis converts a timestamp value in unit to epoch
// milliseconds, truncating toward zero below a millisecond. An unknown unit
// is returned unchanged.
func TimestampToEpochMillis(v int64, unit arrow.TimeUnit) (ms int64) {
	switch unit {
	case arrow.Second:
		ms = v * 1000
	case arrow.Millisecond:
		ms = v
	case arrow.Microsecond:
		ms = v / 1000
	case arrow.Nanosecond:
		ms = v / 1_000_000
	default:
		ms = v
	}
	return
}

const msPerDay = 24 * 60 * 60 * 1000

// EpochMillis reads a temporal cell — a Timestamp in any unit, a Date32 or
// a Date64 — as epoch milliseconds. An integer is not read as a moment
// here, because only the column's origin says it is one: [Time] documents
// the one integer spelling ClickHouse uses.
func EpochMillis(arr arrow.Array, row int) (ms int64, ok bool) {
	a, i, ok := deref(arr, row)
	if !ok {
		return
	}
	switch c := a.(type) {
	case *array.Timestamp:
		unit := arrow.Second
		if tt, isTs := c.DataType().(*arrow.TimestampType); isTs {
			unit = tt.Unit
		}
		ms = TimestampToEpochMillis(int64(c.Value(i)), unit)
	case *array.Date32:
		ms = int64(c.Value(i)) * msPerDay
	case *array.Date64:
		ms = int64(c.Value(i))
	default:
		ok = false
	}
	return
}

// Time reads a temporal cell as a UTC time.Time: a Timestamp (at its full
// resolution), a Date32 or a Date64, and a UInt32 read as epoch seconds —
// the spelling ClickHouse's ArrowStream gives a DateTime, whose Arrow type
// does not otherwise say it is a moment.
func Time(arr arrow.Array, row int) (t time.Time, ok bool) {
	a, i, ok := deref(arr, row)
	if !ok {
		return
	}
	switch c := a.(type) {
	case *array.Timestamp:
		unit := arrow.Second
		if tt, isTs := c.DataType().(*arrow.TimestampType); isTs {
			unit = tt.Unit
		}
		t = c.Value(i).ToTime(unit)
	case *array.Date32:
		t = c.Value(i).ToTime()
	case *array.Date64:
		t = c.Value(i).ToTime()
	case *array.Uint32:
		t = time.Unix(int64(c.Value(i)), 0)
	default:
		return time.Time{}, false
	}
	t = t.UTC()
	return
}
