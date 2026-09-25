package gloss

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/db/clickhouse/chrows"
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

// Raw returns the bytes of a string or binary value — a FixedString
// included — without validation or hex-encoding: the content a decoder
// needs. Text and binary alias the array's buffer; a FixedString is copied.
func (inst ArrowCell) Raw() (raw string, ok bool) {
	if raw, ok = chrows.String(inst.Arr, inst.Row); ok {
		return
	}
	b, ok := chrows.Bytes(inst.Arr, inst.Row)
	raw = string(b)
	return
}

func (inst ArrowCell) Float64() (v float64, ok bool) { return chrows.Float64(inst.Arr, inst.Row) }

func (inst ArrowCell) Int64() (v int64, ok bool) { return chrows.Int64(inst.Arr, inst.Row) }

// Uint64 refuses a negative signed value rather than wrapping it to the
// top of the range.
func (inst ArrowCell) Uint64() (v uint64, ok bool) { return chrows.Uint64(inst.Arr, inst.Row) }

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

// FormatBinaryMaxBytes is how much of a binary value the un-glossed rendering
// spells out. Identifiers, hashes and packed addresses fit under it whole; a
// stored document or image does not, and its cell names the size instead.
const FormatBinaryMaxBytes = 256

// formatBinary is the un-glossed rendering of bytes: lowercase hex, whole up
// to [FormatBinaryMaxBytes] and the head plus the size past it.
//
// The bound is there because this runs per visible cell per frame with no
// cache behind it, at two bytes of text per byte of value: a grid over a
// column of recordings built megabytes of hex every frame, all of it to be
// truncated to a cell's width (ADR-0245, found while implementing). Nothing
// reads the text back as data — a gloss takes the bytes from the cell
// accessor, never from here.
func formatBinary(b []byte) string {
	if len(b) <= FormatBinaryMaxBytes {
		return hex.EncodeToString(b)
	}
	return hex.EncodeToString(b[:FormatBinaryMaxBytes]) + "… (" + humanize.IBytes(uint64(len(b))) + ")"
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
		return formatBinary(a.Value(int(row)))
	case *array.LargeBinary:
		// LargeBinary.ValueStr() returns string(rawBytes) without UTF-8
		// validation — feeding that through a label ships non-UTF-8 to the
		// renderer and breaks the FFFI protocol mid-frame. Hex-encode like
		// *array.Binary.
		return formatBinary(a.Value(int(row)))
	case *array.FixedSizeBinary:
		return formatBinary(a.Value(int(row)))
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
