package gloss

import (
	"fmt"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// CellI is a gloss's view of one value. Two implementations ship: ArrowCell
// over an Arrow array and row (the grids), and TextCell over an already
// formatted string (the leeway card, tests). A gloss asks for the access it
// needs and falls back to Text.
type CellI interface {
	IsNull() bool
	Kind() ValueKindE
	// Text is the plain rendering — what the cell shows without a gloss.
	// Empty for a null.
	Text() string
	// Raw is the undecorated content of a string or binary value, as-is. For
	// an Arrow-backed cell it may alias the array's memory and must not be
	// retained past the frame. ok is false for every other kind, and the
	// caller falls back to Text.
	Raw() (raw string, ok bool)
	Float64() (v float64, ok bool)
	Int64() (v int64, ok bool)
	// Uint64 reads an integer cell as an unsigned 64-bit word. It exists
	// because the top of the UInt64 range is neither an Int64 (which refuses
	// it) nor a Float64 (which rounds it): a fibonacci-tagged id carries its
	// tag in the high bits, so most of them are above 2^63 and every bit of
	// them is meaningful. A negative signed value is not a uint64 and reads
	// as not-ok rather than wrapping.
	Uint64() (v uint64, ok bool)
}

// ArrowCell reads (Arr, Row). A row out of range reads as null.
type ArrowCell struct {
	Arr arrow.Array
	Row int
}

var _ CellI = ArrowCell{}

func (inst ArrowCell) IsNull() bool {
	return inst.Arr == nil || inst.Row < 0 || inst.Row >= inst.Arr.Len() || inst.Arr.IsNull(inst.Row)
}

func (inst ArrowCell) Kind() ValueKindE {
	if inst.Arr == nil {
		return ValueKindOther
	}
	return KindOfArrow(inst.Arr.DataType())
}

func (inst ArrowCell) Text() string {
	if inst.Arr == nil {
		return ""
	}
	return FormatArrowElem(inst.Arr, int64(inst.Row))
}

// Raw returns the bytes of a string or binary value without validation or
// hex-encoding — the content a decoder needs. String.Value returns a
// substring of the array's backing string and Binary.ValueString is
// documented zero-copy, so this is a length read, not a copy.
func (inst ArrowCell) Raw() (raw string, ok bool) {
	if inst.IsNull() {
		return "", false
	}
	switch a := inst.Arr.(type) {
	case *array.String:
		return a.Value(inst.Row), true
	case *array.LargeString:
		return a.Value(inst.Row), true
	case *array.Binary:
		return a.ValueString(inst.Row), true
	case *array.LargeBinary:
		return a.ValueString(inst.Row), true
	case *array.FixedSizeBinary:
		return string(a.Value(inst.Row)), true
	case *array.Dictionary:
		if dv, isStr := a.Dictionary().(*array.String); isStr {
			return dv.Value(a.GetValueIndex(inst.Row)), true
		}
	}
	return "", false
}

func (inst ArrowCell) Float64() (v float64, ok bool) {
	if inst.IsNull() {
		return 0, false
	}
	i := inst.Row
	switch a := inst.Arr.(type) {
	case *array.Float64:
		return a.Value(i), true
	case *array.Float32:
		return float64(a.Value(i)), true
	case *array.Float16:
		return float64(a.Value(i).Float32()), true
	case *array.Int8:
		return float64(a.Value(i)), true
	case *array.Int16:
		return float64(a.Value(i)), true
	case *array.Int32:
		return float64(a.Value(i)), true
	case *array.Int64:
		return float64(a.Value(i)), true
	case *array.Uint8:
		return float64(a.Value(i)), true
	case *array.Uint16:
		return float64(a.Value(i)), true
	case *array.Uint32:
		return float64(a.Value(i)), true
	case *array.Uint64:
		return float64(a.Value(i)), true
	case *array.Decimal128:
		if dt, isDec := a.DataType().(*arrow.Decimal128Type); isDec {
			return a.Value(i).ToFloat64(dt.Scale), true
		}
	case *array.Decimal256:
		if dt, isDec := a.DataType().(*arrow.Decimal256Type); isDec {
			return a.Value(i).ToFloat64(dt.Scale), true
		}
	}
	return 0, false
}

func (inst ArrowCell) Int64() (v int64, ok bool) {
	if inst.IsNull() {
		return 0, false
	}
	i := inst.Row
	switch a := inst.Arr.(type) {
	case *array.Int8:
		return int64(a.Value(i)), true
	case *array.Int16:
		return int64(a.Value(i)), true
	case *array.Int32:
		return int64(a.Value(i)), true
	case *array.Int64:
		return a.Value(i), true
	case *array.Uint8:
		return int64(a.Value(i)), true
	case *array.Uint16:
		return int64(a.Value(i)), true
	case *array.Uint32:
		return int64(a.Value(i)), true
	case *array.Uint64:
		u := a.Value(i)
		if u > uint64(1<<63-1) {
			return 0, false
		}
		return int64(u), true
	}
	return 0, false
}

func (inst ArrowCell) Uint64() (v uint64, ok bool) {
	if inst.IsNull() {
		return 0, false
	}
	i := inst.Row
	switch a := inst.Arr.(type) {
	case *array.Uint8:
		return uint64(a.Value(i)), true
	case *array.Uint16:
		return uint64(a.Value(i)), true
	case *array.Uint32:
		return uint64(a.Value(i)), true
	case *array.Uint64:
		return a.Value(i), true
	case *array.Int8:
		return nonNegative(int64(a.Value(i)))
	case *array.Int16:
		return nonNegative(int64(a.Value(i)))
	case *array.Int32:
		return nonNegative(int64(a.Value(i)))
	case *array.Int64:
		return nonNegative(a.Value(i))
	}
	return 0, false
}

// nonNegative is the signed→unsigned read: a negative value is not a uint64
// and is refused rather than wrapped to the top of the range.
func nonNegative(v int64) (u uint64, ok bool) {
	if v < 0 {
		return 0, false
	}
	return uint64(v), true
}

// TextCell wraps an already formatted value: the leeway card's cell text, or
// a test's literal. Numeric access parses the text.
type TextCell struct {
	S string
	K ValueKindE
}

var _ CellI = TextCell{}

func (inst TextCell) IsNull() bool     { return false }
func (inst TextCell) Kind() ValueKindE { return inst.K }
func (inst TextCell) Text() string     { return inst.S }
func (inst TextCell) Raw() (raw string, ok bool) {
	return inst.S, true
}
func (inst TextCell) Float64() (v float64, ok bool) {
	v, err := strconv.ParseFloat(inst.S, 64)
	return v, err == nil
}
func (inst TextCell) Int64() (v int64, ok bool) {
	v, err := strconv.ParseInt(inst.S, 10, 64)
	return v, err == nil
}

// Uint64 parses the text in base 10, matching Int64 and Float64 — they read
// the marshalled value, not a literal, so a prefixed base would be a
// different value than the cell shows. A gloss that wants to accept `0x…`
// typed by a user parses the text itself.
func (inst TextCell) Uint64() (v uint64, ok bool) {
	v, err := strconv.ParseUint(inst.S, 10, 64)
	return v, err == nil
}

// FormatArrowElem formats the row-th element of an arbitrary Arrow array as
// its plain display string, empty for NULL or out of range. It is the
// un-glossed rendering every grid falls back to, and the one `gloss/raw`
// returns. Binary values hex-encode — a gloss that wants the bytes reads
// CellI.Raw instead.
func FormatArrowElem(arr arrow.Array, row int64) string {
	if row < 0 || int(row) >= arr.Len() {
		return ""
	}
	if arr.IsNull(int(row)) {
		return ""
	}
	switch a := arr.(type) {
	case *array.Boolean:
		if a.Value(int(row)) {
			return "true"
		}
		return "false"
	case *array.Int8:
		return strconv.FormatInt(int64(a.Value(int(row))), 10)
	case *array.Int16:
		return strconv.FormatInt(int64(a.Value(int(row))), 10)
	case *array.Int32:
		return strconv.FormatInt(int64(a.Value(int(row))), 10)
	case *array.Int64:
		return strconv.FormatInt(a.Value(int(row)), 10)
	case *array.Uint8:
		return strconv.FormatUint(uint64(a.Value(int(row))), 10)
	case *array.Uint16:
		return strconv.FormatUint(uint64(a.Value(int(row))), 10)
	case *array.Uint32:
		return strconv.FormatUint(uint64(a.Value(int(row))), 10)
	case *array.Uint64:
		return strconv.FormatUint(a.Value(int(row)), 10)
	case *array.Float32:
		return strconv.FormatFloat(float64(a.Value(int(row))), 'g', -1, 32)
	case *array.Float64:
		return strconv.FormatFloat(a.Value(int(row)), 'g', -1, 64)
	case *array.String:
		return utfsafe.EnsureUTF8(a.Value(int(row)))
	case *array.LargeString:
		return utfsafe.EnsureUTF8(a.Value(int(row)))
	case *array.Binary:
		return fmt.Sprintf("%x", a.Value(int(row)))
	case *array.LargeBinary:
		// LargeBinary.ValueStr() returns string(rawBytes) without UTF-8
		// validation — feeding that through a label ships non-UTF-8 to the
		// renderer and breaks the FFFI protocol mid-frame. Hex-encode like
		// *array.Binary.
		return fmt.Sprintf("%x", a.Value(int(row)))
	case *array.FixedSizeBinary:
		return fmt.Sprintf("%x", a.Value(int(row)))
	case *array.Timestamp:
		ts := a.Value(int(row))
		unit := arrow.Second
		if tt, ok := arr.DataType().(*arrow.TimestampType); ok {
			unit = tt.Unit
		}
		return ts.ToTime(unit).UTC().Format(time.RFC3339Nano)
	case *array.Date32:
		return a.Value(int(row)).FormattedString()
	case *array.Date64:
		return a.Value(int(row)).FormattedString()
	case *array.Duration:
		return strconv.FormatInt(int64(a.Value(int(row))), 10)
	case *array.List:
		beg, end := a.ValueOffsets(int(row))
		return fmt.Sprintf("[len=%d]", end-beg)
	case *array.LargeList:
		beg, end := a.ValueOffsets(int(row))
		return fmt.Sprintf("[len=%d]", end-beg)
	case *array.FixedSizeList:
		return fmt.Sprintf("[len=%d]", a.DataType().(*arrow.FixedSizeListType).Len())
	case *array.Struct:
		return fmt.Sprintf("{struct fields=%d}", a.NumField())
	case *array.Map:
		beg, end := a.ValueOffsets(int(row))
		return fmt.Sprintf("{map len=%d}", end-beg)
	case *array.Dictionary:
		return formatDictValue(a, int(row))
	default:
		// Safe fallback — every arrow.Array implements ValueStr since
		// 14.x. Some implementations (e.g. LargeBinary) return raw bytes
		// stringified, which can contain invalid UTF-8 and break the
		// downstream FFFI wire (read_plain_s does String::from_utf8).
		// Validate and hex-fallback so the protocol stays intact for
		// any Arrow type not explicitly cased above.
		return utfsafe.EnsureUTF8(a.ValueStr(int(row)))
	}
}

func formatDictValue(d *array.Dictionary, row int) string {
	if d.IsNull(row) {
		return ""
	}
	idx := d.GetValueIndex(row)
	dict := d.Dictionary()
	switch dv := dict.(type) {
	case *array.String:
		// EnsureUTF8 to match the direct *array.String case — CH
		// LowCardinality(String) can carry non-UTF-8 bytes that would break
		// the FFI wire downstream of a label.
		return utfsafe.EnsureUTF8(dv.Value(idx))
	case *array.Int64:
		return strconv.FormatInt(dv.Value(idx), 10)
	case *array.Uint64:
		return strconv.FormatUint(dv.Value(idx), 10)
	default:
		return fmt.Sprintf("<dict %T[%d]>", dict, idx)
	}
}
