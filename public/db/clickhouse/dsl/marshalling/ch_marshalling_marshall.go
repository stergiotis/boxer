//go:build llm_generated_opus46

package marshalling

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
)

// --- String escape/unescape ---

// UnescapeString removes surrounding single quotes and resolves escape sequences.
func UnescapeString(raw string) (result string, err error) {
	if len(raw) < 2 || raw[0] != '\'' || raw[len(raw)-1] != '\'' {
		err = fmt.Errorf("UnescapeString: input must be a single-quoted string, got %q", raw)
		return
	}
	inner := raw[1 : len(raw)-1]
	var buf strings.Builder
	buf.Grow(len(inner))

	i := 0
	for i < len(inner) {
		ch := inner[i]
		switch {
		case ch == '\\' && i+1 < len(inner):
			next := inner[i+1]
			switch next {
			case '\\':
				buf.WriteByte('\\')
				i += 2
			case '\'':
				buf.WriteByte('\'')
				i += 2
			case 'n':
				buf.WriteByte('\n')
				i += 2
			case 't':
				buf.WriteByte('\t')
				i += 2
			case 'r':
				buf.WriteByte('\r')
				i += 2
			case '0':
				buf.WriteByte(0)
				i += 2
			case 'b':
				buf.WriteByte('\b')
				i += 2
			case 'f':
				buf.WriteByte('\f')
				i += 2
			case 'a':
				buf.WriteByte('\a')
				i += 2
			case 'v':
				buf.WriteByte('\v')
				i += 2
			case 'x':
				if i+3 >= len(inner) {
					err = fmt.Errorf("UnescapeString: truncated \\x escape at position %d", i)
					return
				}
				val, parseErr := strconv.ParseUint(inner[i+2:i+4], 16, 8)
				if parseErr != nil {
					err = fmt.Errorf("UnescapeString: invalid \\x escape at position %d: %w", i, parseErr)
					return
				}
				buf.WriteByte(byte(val))
				i += 4
			case 'u':
				if i+5 >= len(inner) {
					err = fmt.Errorf("UnescapeString: truncated \\u escape at position %d", i)
					return
				}
				val, parseErr := strconv.ParseUint(inner[i+2:i+6], 16, 32)
				if parseErr != nil {
					err = fmt.Errorf("UnescapeString: invalid \\u escape at position %d: %w", i, parseErr)
					return
				}
				buf.WriteRune(rune(val))
				i += 6
			case 'U':
				if i+9 >= len(inner) {
					err = fmt.Errorf("UnescapeString: truncated \\U escape at position %d", i)
					return
				}
				val, parseErr := strconv.ParseUint(inner[i+2:i+10], 16, 32)
				if parseErr != nil {
					err = fmt.Errorf("UnescapeString: invalid \\U escape at position %d: %w", i, parseErr)
					return
				}
				if !utf8.ValidRune(rune(val)) {
					err = fmt.Errorf("UnescapeString: invalid Unicode code point U+%04X at position %d", val, i)
					return
				}
				buf.WriteRune(rune(val))
				i += 10
			default:
				buf.WriteByte(next)
				i += 2
			}
		case ch == '\'' && i+1 < len(inner) && inner[i+1] == '\'':
			buf.WriteByte('\'')
			i += 2
		default:
			buf.WriteByte(ch)
			i++
		}
	}
	result = buf.String()
	return
}

// EscapeString produces a ClickHouse single-quoted string literal from a Go string.
func EscapeString(s string) string {
	var buf strings.Builder
	buf.Grow(len(s) + 2)
	buf.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\\':
			buf.WriteString("\\\\")
		case '\'':
			buf.WriteString("\\'")
		case '\n':
			buf.WriteString("\\n")
		case '\t':
			buf.WriteString("\\t")
		case '\r':
			buf.WriteString("\\r")
		case 0:
			buf.WriteString("\\0")
		default:
			buf.WriteByte(ch)
		}
	}
	buf.WriteByte('\'')
	return buf.String()
}

// --- Unmarshal scalar ---

// UnmarshalScalarLiteral parses a ClickHouse SQL literal token into a TypedLiteral
// with Kind == KindScalar.
func UnmarshalScalarLiteral(token string) (result TypedLiteral, err error) {
	result.Kind = KindScalar
	token = strings.TrimSpace(token)
	if len(token) == 0 {
		err = fmt.Errorf("UnmarshalScalarLiteral: empty input")
		return
	}

	if strings.EqualFold(token, "NULL") {
		result.Null = true
		return
	}
	if token == "true" {
		result.ScalarType = ctabb.B
		result.BoolVal = true
		return
	}
	if token == "false" {
		result.ScalarType = ctabb.B
		result.BoolVal = false
		return
	}
	if len(token) >= 2 && token[0] == '\'' {
		result.ScalarType = ctabb.S
		result.StringVal, err = UnescapeString(token)
		if err != nil {
			err = fmt.Errorf("UnmarshalScalarLiteral: %w", err)
		}
		return
	}

	sign := int64(1)
	signF := 1.0
	numPart := token
	if len(token) > 0 && (token[0] == '+' || token[0] == '-') {
		if token[0] == '-' {
			sign = -1
			signF = -1.0
		}
		numPart = token[1:]
		if len(numPart) == 0 {
			err = fmt.Errorf("UnmarshalScalarLiteral: bare sign %q", token)
			return
		}
	}

	upper := strings.ToUpper(numPart)
	if upper == "INF" || upper == "INFINITY" {
		result.ScalarType = ctabb.F64
		result.FloatVal = signF * math.Inf(1)
		return
	}
	if upper == "NAN" {
		result.ScalarType = ctabb.F64
		result.FloatVal = math.NaN()
		return
	}

	if len(numPart) > 2 && numPart[0] == '0' && (numPart[1] == 'x' || numPart[1] == 'X') {
		if containsAnyByte(numPart, "pP.") {
			result.ScalarType = ctabb.F64
			result.FloatVal, err = strconv.ParseFloat(numPart, 64)
			if err != nil {
				err = fmt.Errorf("UnmarshalScalarLiteral: invalid hex float %q: %w", token, err)
				return
			}
			result.FloatVal *= signF
			return
		}
		var val uint64
		val, err = strconv.ParseUint(numPart[2:], 16, 64)
		if err != nil {
			err = fmt.Errorf("UnmarshalScalarLiteral: invalid hex literal %q: %w", token, err)
			return
		}
		if sign >= 0 {
			result.ScalarType = ctabb.U64
			result.UintVal = val
		} else {
			result.ScalarType = ctabb.I64
			result.IntVal = -int64(val)
		}
		return
	}

	if len(numPart) > 1 && numPart[0] == '0' && isOctalDigit(numPart[1]) && !containsAnyByte(numPart, ".eE") {
		var val uint64
		val, err = strconv.ParseUint(numPart[1:], 8, 64)
		if err != nil {
			err = fmt.Errorf("UnmarshalScalarLiteral: invalid octal literal %q: %w", token, err)
			return
		}
		if sign >= 0 {
			result.ScalarType = ctabb.U64
			result.UintVal = val
		} else {
			result.ScalarType = ctabb.I64
			result.IntVal = -int64(val)
		}
		return
	}

	if containsAnyByte(numPart, ".eE") {
		result.ScalarType = ctabb.F64
		result.FloatVal, err = strconv.ParseFloat(numPart, 64)
		if err != nil {
			err = fmt.Errorf("UnmarshalScalarLiteral: invalid float literal %q: %w", token, err)
			return
		}
		result.FloatVal *= signF
		return
	}

	{
		var val uint64
		val, err = strconv.ParseUint(numPart, 10, 64)
		if err != nil {
			err = fmt.Errorf("UnmarshalScalarLiteral: unrecognised literal %q: %w", token, err)
			result.Unknown = true
			return
		}
		if sign >= 0 {
			result.ScalarType = ctabb.U64
			result.UintVal = val
		} else {
			result.ScalarType = ctabb.I64
			result.IntVal = -int64(val)
		}
		return
	}
}

// --- Marshal scalar ---

// MarshalScalarToSQL converts a scalar TypedLiteral to ClickHouse SQL text.
func MarshalScalarToSQL(lit TypedLiteral) (result string, err error) {
	if lit.Kind != KindScalar {
		err = eh.Errorf("MarshalScalarToSQL: expected KindScalar, got %s", lit.Kind)
		return
	}
	if lit.Null {
		result = "NULL"
		return
	}
	if lit.ScalarType == nil {
		err = eh.Errorf("MarshalScalarToSQL: nil ScalarType on non-null literal")
		return
	}
	switch lit.ScalarType.String() {
	case "b":
		if lit.BoolVal {
			result = "true"
		} else {
			result = "false"
		}
	case "s":
		result = EscapeString(lit.StringVal)
	case "i64":
		result = strconv.FormatInt(lit.IntVal, 10)
	case "u64":
		result = strconv.FormatUint(lit.UintVal, 10)
	case "f64":
		if math.IsInf(lit.FloatVal, 1) {
			result = "Inf"
		} else if math.IsInf(lit.FloatVal, -1) {
			result = "-Inf"
		} else if math.IsNaN(lit.FloatVal) {
			result = "NaN"
		} else {
			result = strconv.FormatFloat(lit.FloatVal, 'g', -1, 64)
		}
	default:
		err = eb.Build().Stringer("type", lit.ScalarType).Errorf("MarshalScalarToSQL: unknown scalar type")
	}
	return
}

// --- Marshal composite (TypedLiteral → SQL) ---

// MarshalTypedLiteralToSQL serializes a TypedLiteral to SQL text.
// Cast annotations are serialized as CAST(expr, 'Type') using MapCanonicalToClickHouse.
func MarshalTypedLiteralToSQL(lit TypedLiteral) (sql string, err error) {
	return MarshalTypedLiteralToSQLEx(lit, MapCanonicalToClickHouseTypeStr)
}

// MarshalTypedLiteralToSQLEx serializes a TypedLiteral to SQL text.
// Cast annotations are serialized as CAST(expr, 'Type') using mapCanonicalToClickHouse.
// If mapCanonicalToClickHouse is nil, casts are silently dropped.
func MarshalTypedLiteralToSQLEx(lit TypedLiteral, mapCanonicalToClickHouse func(string) (string, error)) (sql string, err error) {
	innerSQL, innerErr := marshalTypedLiteralInner(lit, mapCanonicalToClickHouse)
	if innerErr != nil {
		err = innerErr
		return
	}
	if lit.CastTypeCanonical != "" && mapCanonicalToClickHouse != nil {
		chType, mapErr := mapCanonicalToClickHouse(lit.CastTypeCanonical)
		if mapErr == nil && chType != "" {
			sql = "CAST(" + innerSQL + ", '" + chType + "')"
			return
		}
	}
	sql = innerSQL
	return
}

func marshalTypedLiteralInner(lit TypedLiteral, mapFunc func(string) (string, error)) (sql string, err error) {
	switch lit.Kind {
	case KindScalar:
		sql, err = MarshalScalarToSQL(lit)
	case KindHomogeneousArray:
		sql, err = marshalHomogeneousArrayToSQL(lit.HomArray)
	case KindHeterogeneousArray:
		sql, err = marshalHeterogeneousArrayToSQL(lit.Elements, mapFunc)
	case KindTuple:
		sql, err = marshalTupleTypedLiteralToSQL(lit.Elements, mapFunc)
	default:
		err = eh.Errorf("marshalTypedLiteralInner: unknown kind %s", lit.Kind)
	}
	return
}

func marshalHomogeneousArrayToSQL(a *HomogeneousArray) (sql string, err error) {
	if a == nil || a.Len() == 0 {
		sql = "array()"
		return
	}
	var sb strings.Builder
	sb.WriteString("array(")
	n := a.Len()
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		elem, getErr := a.GetScalar(i)
		if getErr != nil {
			err = eh.Errorf("marshalHomogeneousArrayToSQL: element %d: %w", i, getErr)
			return
		}
		elemSQL, marshalErr := MarshalScalarToSQL(elem)
		if marshalErr != nil {
			err = eh.Errorf("marshalHomogeneousArrayToSQL: element %d: %w", i, marshalErr)
			return
		}
		sb.WriteString(elemSQL)
	}
	sb.WriteByte(')')
	sql = sb.String()
	return
}

func marshalHeterogeneousArrayToSQL(elems []TypedLiteral, mapFunc func(string) (string, error)) (sql string, err error) {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, elem := range elems {
		if i > 0 {
			sb.WriteString(", ")
		}
		elemSQL, elemErr := MarshalTypedLiteralToSQLEx(elem, mapFunc)
		if elemErr != nil {
			err = eh.Errorf("marshalHeterogeneousArrayToSQL: element %d: %w", i, elemErr)
			return
		}
		sb.WriteString(elemSQL)
	}
	sb.WriteByte(']')
	sql = sb.String()
	return
}

func marshalTupleTypedLiteralToSQL(elems []TypedLiteral, mapFunc func(string) (string, error)) (sql string, err error) {
	var sb strings.Builder
	sb.WriteString("tuple(")
	for i, elem := range elems {
		if i > 0 {
			sb.WriteString(", ")
		}
		elemSQL, elemErr := MarshalTypedLiteralToSQLEx(elem, mapFunc)
		if elemErr != nil {
			err = eh.Errorf("marshalTupleTypedLiteralToSQL: element %d: %w", i, elemErr)
			return
		}
		sb.WriteString(elemSQL)
	}
	sb.WriteByte(')')
	sql = sb.String()
	return
}

// --- Marshal Go values ---

// MarshalOptions controls SQL serialization behavior.
type MarshalOptions struct {
	// PreserveCasts wraps values in CAST(expr, 'Type') when the Go type
	// provides more specific type information than ClickHouse would infer.
	PreserveCasts bool

	// MapCanonicalToClickHouse maps canonical type strings to ClickHouse type names.
	// Required for TypedLiteral cast serialization. If nil, casts are dropped.
	MapCanonicalToClickHouse func(string) (string, error)
}

// MarshalGoValueToSQL converts a Go value to SQL without cast preservation.
func MarshalGoValueToSQL(val any) (sql string, err error) {
	return MarshalGoValueToSQLWithOptions(val, MarshalOptions{})
}

// MarshalGoValueToSQLWithOptions converts a Go value to SQL with configurable behavior.
func MarshalGoValueToSQLWithOptions(val any, opts MarshalOptions) (sql string, err error) {
	var c string
	sql, c, err = MarshalGoValueToSQLWithOptionsCast(val, opts)
	if err != nil {
		return
	}
	if c != "" {
		sql = "CAST(" + sql + ",'" + c + "')"
	}
	return
}
func MarshalGoValueToSQLWithOptionsCast(val any, opts MarshalOptions) (sql string, castType string, err error) {
	if val == nil {
		sql = "NULL"
		return
	}
	switch v := val.(type) {
	case TypedLiteral:
		if opts.PreserveCasts && opts.MapCanonicalToClickHouse != nil && v.CastTypeCanonical != "" {
			castType, err = opts.MapCanonicalToClickHouse(v.CastTypeCanonical)
			if err != nil {
				err = eh.Errorf("MarshalGoValueToSQLWithOptions: unable to map cast type%w", err)
				return
			}
		}
		sql, err = MarshalTypedLiteralToSQLEx(v, opts.MapCanonicalToClickHouse)
		if err != nil {
			err = eh.Errorf("MarshalGoValueToSQLWithOptions: %w", err)
		}
		return
	case *TypedLiteral:
		if v == nil {
			sql = "NULL"
			return
		}
		if opts.PreserveCasts && opts.MapCanonicalToClickHouse != nil && v.CastTypeCanonical != "" {
			castType, err = opts.MapCanonicalToClickHouse(v.CastTypeCanonical)
			if err != nil {
				err = eh.Errorf("MarshalGoValueToSQLWithOptions: unable to map cast type%w", err)
				return
			}
		}
		sql, err = MarshalTypedLiteralToSQLEx(*v, opts.MapCanonicalToClickHouse)
		if err != nil {
			err = eh.Errorf("MarshalGoValueToSQLWithOptions: %w", err)
		}
		return

	// --- Arrays ---
	case []any:
		sql, err = marshalGoArray(v, opts)
		return
	case []int64:
		sql, err = marshalGoArray(v, opts)
		if err == nil && opts.PreserveCasts {
			castType = "Array(Int64)"
		}
		return
	case []uint64:
		sql, err = marshalGoArray(v, opts)
		if err == nil && opts.PreserveCasts {
			castType = "Array(UInt64)"
		}
		return
	case []float64:
		sql, err = marshalGoArray(v, opts)
		if err == nil && opts.PreserveCasts {
			castType = "Array(Float64)"
		}
		return
	case []float32:
		sql, err = marshalGoArray(v, opts)
		if err == nil && opts.PreserveCasts {
			castType = "Array(Float32)"
		}
		return
	case []bool:
		sql, err = marshalGoArray(v, opts)
		return
	case []string:
		sql, err = marshalGoArray(v, opts)
		return
	case []int8:
		sql, err = marshalGoArray(v, opts)
		if err == nil && opts.PreserveCasts {
			castType = "Array(Int8)"
		}
		return
	case []int16:
		sql, err = marshalGoArray(v, opts)
		if err == nil && opts.PreserveCasts {
			castType = "Array(Int16)"
		}
		return
	case []int32:
		sql, err = marshalGoArray(v, opts)
		if err == nil && opts.PreserveCasts {
			castType = "Array(Int32)"
		}
		return
	case []uint8:
		sql, err = marshalGoArray(v, opts)
		if err == nil && opts.PreserveCasts {
			castType = "Array(UInt8)"
		}
		return
	case []uint16:
		sql, err = marshalGoArray(v, opts)
		if err == nil && opts.PreserveCasts {
			castType = "Array(UInt16)"
		}
		return
	case []uint32:
		sql, err = marshalGoArray(v, opts)
		if err == nil && opts.PreserveCasts {
			castType = "Array(UInt32)"
		}
		return
	case []TypedLiteral:
		lit := NewHeterogeneousArray(v...)
		sql, err = MarshalTypedLiteralToSQLEx(lit, opts.MapCanonicalToClickHouse)
		if err != nil {
			err = eh.Errorf("MarshalGoValueToSQLWithOptions: %w", err)
		}
		return

	// --- Tuple ---
	case *Tuple:
		sql, err = marshalGoTuple(v, opts)
		return

	// --- Scalars (unambiguous) ---
	case string:
		sql = EscapeString(v)
		return
	case bool:
		sql, err = MarshalScalarToSQL(NewScalarBool(v))
		return

	// --- Scalars (wide) ---
	case int64:
		sql, err = MarshalScalarToSQL(NewScalarInt64(v))
		if err == nil && opts.PreserveCasts {
			castType = "Int64"
		}
		return
	case uint64:
		sql, err = MarshalScalarToSQL(NewScalarUint64(v))
		if err == nil && opts.PreserveCasts {
			castType = "UInt64"
		}
		return
	case float64:
		sql, err = MarshalScalarToSQL(NewScalarFloat64(v))
		if err == nil && opts.PreserveCasts {
			castType = "Float64"
		}
		return

	// --- Scalars (narrow) ---
	case float32:
		sql, err = MarshalScalarToSQL(NewScalarFloat64(float64(v)))
		if err == nil && opts.PreserveCasts {
			castType = "Float32"
		}
		return
	case int8:
		sql, err = MarshalScalarToSQL(NewScalarInt64(int64(v)))
		if err == nil && opts.PreserveCasts {
			castType = "Int8"
		}
		return
	case int16:
		sql, err = MarshalScalarToSQL(NewScalarInt64(int64(v)))
		if err == nil && opts.PreserveCasts {
			castType = "Int16"
		}
		return
	case int32:
		sql, err = MarshalScalarToSQL(NewScalarInt64(int64(v)))
		if err == nil && opts.PreserveCasts {
			castType = "Int32"
		}
		return
	case uint8:
		sql, err = MarshalScalarToSQL(NewScalarUint64(uint64(v)))
		if err == nil && opts.PreserveCasts {
			castType = "UInt8"
		}
		return
	case uint16:
		sql, err = MarshalScalarToSQL(NewScalarUint64(uint64(v)))
		if err == nil && opts.PreserveCasts {
			castType = "UInt16"
		}
		return
	case uint32:
		sql, err = MarshalScalarToSQL(NewScalarUint64(uint64(v)))
		if err == nil && opts.PreserveCasts {
			castType = "UInt32"
		}
		return

	default:
		err = eb.Build().Type("type", val).Errorf("MarshalGoValueToSQLWithOptions: unsupported type")
		return
	}
}

func marshalGoArray[T any](arr []T, opts MarshalOptions) (sql string, err error) {
	if len(arr) == 0 {
		sql = "array()"
		return
	}
	var sb strings.Builder
	sb.WriteString("array(")
	for i, elem := range arr {
		if i > 0 {
			sb.WriteString(", ")
		}
		elemSQL, elemErr := MarshalGoValueToSQLWithOptions(elem, opts)
		if elemErr != nil {
			err = eh.Errorf("marshalGoArray: element %d: %w", i, elemErr)
			return
		}
		sb.WriteString(elemSQL)
	}
	sb.WriteString(")")
	sql = sb.String()
	return
}

func marshalGoTuple(tup *Tuple, opts MarshalOptions) (sql string, err error) {
	n := tup.Len()
	if n == 0 {
		sql = "tuple()"
		return
	}
	var sb strings.Builder
	sb.WriteString("tuple(")
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		elem, found := tup.GetByIndex(i)
		if !found {
			err = eh.Errorf("marshalGoTuple: element %d not found", i)
			return
		}
		elemSQL, elemErr := MarshalGoValueToSQLWithOptions(elem, opts)
		if elemErr != nil {
			err = eh.Errorf("marshalGoTuple: element %d: %w", i, elemErr)
			return
		}
		sb.WriteString(elemSQL)
	}
	sb.WriteByte(')')
	sql = sb.String()
	return
}

// --- Helpers ---

func containsAnyByte(s string, chars string) bool {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return true
			}
		}
	}
	return false
}

func isOctalDigit(b byte) bool {
	return b >= '0' && b <= '7'
}
