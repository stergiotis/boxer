package trivialsql

import (
	"bytes"
	"math"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// The formats Run encodes. A text format writes each value as clickhouse-local
// writes the same Arrow column read back from ClickHouse, and refuses a
// column whose type it does not render that way (textType); ArrowStream
// hands the provider's record on with its own types, where ClickHouse would
// map them (a String comes back LargeBinary), so it equals ClickHouse's in
// values, not in bytes.

// FormatArrowStream is Arrow IPC stream, what play's lanes read.
const FormatArrowStream = "ArrowStream"

// FormatTabSeparated is ClickHouse's default when a statement names none.
const FormatTabSeparated = "TabSeparated"

// FormatTabSeparatedWithNames is TabSeparated after a line of column names.
const FormatTabSeparatedWithNames = "TabSeparatedWithNames"

// FormatCSV is comma-separated, a string always double-quoted.
const FormatCSV = "CSV"

// FormatCSVWithNames is CSV after a line of column names.
const FormatCSVWithNames = "CSVWithNames"

// FormatJSONEachRow is one JSON object per row, its keys in column order.
const FormatJSONEachRow = "JSONEachRow"

// canonicalFormat is the format a FORMAT clause names, matched as ClickHouse
// matches it: without regard to case, and with TSV for TabSeparated.
func canonicalFormat(name string) (canonical string, ok bool) {
	for _, f := range [...]string{FormatArrowStream, FormatTabSeparated, FormatTabSeparatedWithNames, FormatCSV, FormatCSVWithNames, FormatJSONEachRow} {
		if strings.EqualFold(name, f) {
			return f, true
		}
	}
	switch {
	case strings.EqualFold(name, "TSV"):
		return FormatTabSeparated, true
	case strings.EqualFold(name, "TSVWithNames"):
		return FormatTabSeparatedWithNames, true
	}
	return "", false
}

// Encode writes batch in format, one of the Format constants.
func Encode(batch arrow.RecordBatch, format string) (body []byte, err error) {
	if format == FormatArrowStream {
		return introspect.EncodeStream(batch)
	}
	var st styleE
	switch format {
	case FormatTabSeparated, FormatTabSeparatedWithNames:
		st = styleTSV
	case FormatCSV, FormatCSVWithNames:
		st = styleCSV
	case FormatJSONEachRow:
		st = styleJSON
	default:
		return nil, refuse("FORMAT " + format)
	}
	schema := batch.Schema()
	for _, f := range schema.Fields() {
		if !textType(f.Type) {
			return nil, refuse("a column of type " + f.Type.String() + " (" + f.Name + ") in FORMAT " + format + ", which only ArrowStream carries here")
		}
	}
	var buf bytes.Buffer
	cols := batch.Columns()
	if format == FormatTabSeparatedWithNames || format == FormatCSVWithNames {
		for c, f := range schema.Fields() {
			if c > 0 {
				buf.WriteByte(st.sep())
			}
			writeBytes(&buf, []byte(f.Name), st)
		}
		buf.WriteByte('\n')
	}
	for r := 0; r < int(batch.NumRows()); r++ {
		if st == styleJSON {
			buf.WriteByte('{')
		}
		for c, col := range cols {
			if c > 0 {
				buf.WriteByte(st.sep())
			}
			if st == styleJSON {
				writeBytes(&buf, []byte(schema.Field(c).Name), styleJSON)
				buf.WriteByte(':')
			}
			writeValue(&buf, col, r, st)
		}
		if st == styleJSON {
			buf.WriteByte('}')
		}
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// styleE is how a value is written: at the top level of a TabSeparated or CSV
// row, quoted inside an array, or as JSON.
type styleE uint8

const (
	styleTSV styleE = iota
	styleCSV
	styleQuoted
	styleJSON
)

func (inst styleE) sep() byte {
	if inst == styleTSV {
		return '\t'
	}
	return ','
}

// textType reports whether the text formats render dt as ClickHouse does:
// integers, floats, Bool, strings and binaries, and arrays and dictionaries
// of those. Dates, timestamps, decimals, tuples and maps depend on settings
// and types this encoder does not model.
func textType(dt arrow.DataType) bool {
	switch t := dt.(type) {
	case *arrow.Int8Type, *arrow.Int16Type, *arrow.Int32Type, *arrow.Int64Type,
		*arrow.Uint8Type, *arrow.Uint16Type, *arrow.Uint32Type, *arrow.Uint64Type,
		*arrow.Float32Type, *arrow.Float64Type, *arrow.BooleanType,
		*arrow.StringType, *arrow.LargeStringType, *arrow.BinaryType, *arrow.LargeBinaryType:
		return true
	case *arrow.ListType:
		return textType(t.Elem())
	case *arrow.LargeListType:
		return textType(t.Elem())
	case *arrow.FixedSizeListType:
		return textType(t.Elem())
	case *arrow.DictionaryType:
		return textType(t.ValueType)
	}
	return false
}

// writeValue writes row i of arr, whose type textType accepted.
func writeValue(buf *bytes.Buffer, arr arrow.Array, i int, st styleE) {
	if arr.IsNull(i) {
		switch st {
		case styleQuoted:
			buf.WriteString("NULL")
		case styleJSON:
			buf.WriteString("null")
		default:
			buf.WriteString(`\N`)
		}
		return
	}
	var tmp [32]byte
	switch a := arr.(type) {
	case *array.Dictionary:
		writeValue(buf, a.Dictionary(), a.GetValueIndex(i), st)
	case *array.Int8:
		buf.Write(strconv.AppendInt(tmp[:0], int64(a.Value(i)), 10))
	case *array.Int16:
		buf.Write(strconv.AppendInt(tmp[:0], int64(a.Value(i)), 10))
	case *array.Int32:
		buf.Write(strconv.AppendInt(tmp[:0], int64(a.Value(i)), 10))
	case *array.Int64:
		buf.Write(strconv.AppendInt(tmp[:0], a.Value(i), 10))
	case *array.Uint8:
		buf.Write(strconv.AppendUint(tmp[:0], uint64(a.Value(i)), 10))
	case *array.Uint16:
		buf.Write(strconv.AppendUint(tmp[:0], uint64(a.Value(i)), 10))
	case *array.Uint32:
		buf.Write(strconv.AppendUint(tmp[:0], uint64(a.Value(i)), 10))
	case *array.Uint64:
		buf.Write(strconv.AppendUint(tmp[:0], a.Value(i), 10))
	case *array.Float32:
		writeFloat(buf, float64(a.Value(i)), 32, st)
	case *array.Float64:
		writeFloat(buf, a.Value(i), 64, st)
	case *array.Boolean:
		buf.WriteString(strconv.FormatBool(a.Value(i)))
	case *array.String:
		writeBytes(buf, []byte(a.Value(i)), st)
	case *array.LargeString:
		writeBytes(buf, []byte(a.Value(i)), st)
	case *array.Binary:
		writeBytes(buf, a.Value(i), st)
	case *array.LargeBinary:
		writeBytes(buf, a.Value(i), st)
	case array.ListLike:
		if st == styleCSV {
			// A CSV field holds the array's text, quoted as a string.
			var inner bytes.Buffer
			writeList(&inner, a, i, styleQuoted)
			writeBytes(buf, inner.Bytes(), styleCSV)
			return
		}
		inner := styleQuoted
		if st == styleJSON {
			inner = styleJSON
		}
		writeList(buf, a, i, inner)
	}
}

func writeList(buf *bytes.Buffer, a array.ListLike, i int, st styleE) {
	start, end := a.ValueOffsets(i)
	vals := a.ListValues()
	buf.WriteByte('[')
	for j := start; j < end; j++ {
		if j > start {
			buf.WriteByte(',')
		}
		writeValue(buf, vals, int(j), st)
	}
	buf.WriteByte(']')
}

// writeFloat writes v as ClickHouse does: the shortest text that reads back
// as v, in positional notation from 1e-6 up to below 1e21 and in exponent
// notation outside it, with no '+' in an exponent; inf, -inf and nan by
// name, and null in JSON.
func writeFloat(buf *bytes.Buffer, v float64, bits int, st styleE) {
	switch {
	case math.IsNaN(v), math.IsInf(v, 0):
		if st == styleJSON {
			buf.WriteString("null")
		} else if math.IsNaN(v) {
			buf.WriteString("nan")
		} else if v > 0 {
			buf.WriteString("inf")
		} else {
			buf.WriteString("-inf")
		}
		return
	case v == 0:
		if math.Signbit(v) {
			buf.WriteString("-0")
		} else {
			buf.WriteByte('0')
		}
		return
	}
	if v < 0 {
		buf.WriteByte('-')
		v = -v
	}
	// d.ddde±x: the shortest digits and the exponent of the first one.
	e := strconv.FormatFloat(v, 'e', -1, bits)
	mant, expText, _ := strings.Cut(e, "e")
	exp, _ := strconv.Atoi(expText)
	digits := strings.Replace(mant, ".", "", 1)
	k := len(digits)
	n := exp + 1 // the decimal point's position after the first digit
	switch {
	case n > 21 || n <= -6:
		buf.WriteByte(digits[0])
		if k > 1 {
			buf.WriteByte('.')
			buf.WriteString(digits[1:])
		}
		buf.WriteByte('e')
		buf.WriteString(strconv.Itoa(n - 1))
	case n >= k:
		buf.WriteString(digits)
		for range n - k {
			buf.WriteByte('0')
		}
	case n > 0:
		buf.WriteString(digits[:n])
		buf.WriteByte('.')
		buf.WriteString(digits[n:])
	default:
		buf.WriteString("0.")
		for range -n {
			buf.WriteByte('0')
		}
		buf.WriteString(digits)
	}
}

// writeBytes writes a string or binary value: backslash-escaped in
// TabSeparated, single-quoted and escaped inside an array, double-quoted
// with doubled quotes in CSV, and as a JSON string — byte for byte, since
// ClickHouse's String is bytes and need not be UTF-8.
func writeBytes(buf *bytes.Buffer, b []byte, st styleE) {
	switch st {
	case styleTSV:
		writeEscaped(buf, b)
	case styleQuoted:
		buf.WriteByte('\'')
		writeEscaped(buf, b)
		buf.WriteByte('\'')
	case styleCSV:
		buf.WriteByte('"')
		for _, c := range b {
			if c == '"' {
				buf.WriteByte('"')
			}
			buf.WriteByte(c)
		}
		buf.WriteByte('"')
	case styleJSON:
		writeJSONString(buf, b)
	}
}

// writeEscaped is ClickHouse's escaped string, the same at the top level of
// TabSeparated and inside quotes: \b \f \n \r \t \0, the backslash and the
// single quote escaped, every other byte as it is.
func writeEscaped(buf *bytes.Buffer, b []byte) {
	for _, c := range b {
		switch c {
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		case 0:
			buf.WriteString(`\0`)
		case '\\':
			buf.WriteString(`\\`)
		case '\'':
			buf.WriteString(`\'`)
		default:
			buf.WriteByte(c)
		}
	}
}

// writeJSONString is ClickHouse's JSON string: the quote, the backslash and
// the slash escaped, \b \f \n \r \t by name, other control bytes as \u00XX,
// and every byte from 0x7f up as it is.
func writeJSONString(buf *bytes.Buffer, b []byte) {
	const hex = "0123456789ABCDEF"
	buf.WriteByte('"')
	for _, c := range b {
		switch c {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '/':
			buf.WriteString(`\/`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if c < 0x20 {
				buf.WriteString(`\u00`)
				buf.WriteByte(hex[c>>4])
				buf.WriteByte(hex[c&0xf])
			} else {
				buf.WriteByte(c)
			}
		}
	}
	buf.WriteByte('"')
}
