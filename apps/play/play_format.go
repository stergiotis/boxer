//go:build llm_generated_opus47

package play

import (
	"fmt"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// formatCell turns (col, row) of a RecordBatch into a display string.
// Returns the empty string for NULL or out-of-range.
func formatCell(rec arrow.RecordBatch, col int, row int64) string {
	arr := rec.Column(col)
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
		// validation — feeding that through c.Label() ships non-UTF-8 to
		// Rust and breaks the FFFI protocol mid-frame. Hex-encode like
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
		// stringified, which can contain invalid UTF-8 and breaks the
		// downstream FFFI wire (read_plain_s does String::from_utf8).
		// Validate and hex-fallback so the protocol stays intact for
		// any Arrow type we haven't explicitly cased above.
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
		return dv.Value(idx)
	case *array.Int64:
		return strconv.FormatInt(dv.Value(idx), 10)
	case *array.Uint64:
		return strconv.FormatUint(dv.Value(idx), 10)
	default:
		return fmt.Sprintf("<dict %T[%d]>", dict, idx)
	}
}
