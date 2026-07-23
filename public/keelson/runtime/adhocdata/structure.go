package adhocdata

import (
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// maxColumnNameLen bounds one structure identifier — a top-level column
// name or a nested Tuple field name. Every name is backtick-quoted, so this
// is defensive hygiene against a pathological schema, not a quoting
// prerequisite: colons, dashes, and other non-identifier bytes are carried
// verbatim inside the quotes, which is what lets a leeway-encoded columnar
// schema (colon-laden physical names, Array-typed repeated sections) pass
// the gate (ADR-0134 SD1).
const maxColumnNameLen = 256

// maxBareIdentLen bounds a bare (unquoted) identifier — a dataset alias or
// handle — which must stay safe to interpolate without quoting.
const maxBareIdentLen = 64

// StructureFor renders the ClickHouse structure string — a comma-joined
// list of backtick-quoted `name Type` columns — that a
// `file(fifo,'ArrowStream',<structure>)` read requires, because schema
// inference over a pipe is impossible (ADR-0134 SD3). Every column name is
// backtick-quoted, so leeway-encoded / nested columnar schemas — whose
// physical names carry colons and whose repeated sections are Array-typed —
// survive the round trip. The type mapping is recursive and total over the
// bounded set the publish gate admits; it rejects everything else, naming
// the offending column, so an unsupported type is refused at publish, never
// discovered at query time (ADR-0134 SD1).
//
// Supported Arrow types → ClickHouse, applied recursively to list elements,
// struct fields, and map values:
//
//	Utf8, Binary                    → String
//	FixedSizeBinary(N)              → FixedString(N)
//	Bool                            → Bool
//	Int8/16/32/64                   → Int8/16/32/64
//	Uint8/16/32/64                  → UInt8/16/32/64
//	Float32/64                      → Float32/64
//	Date32                          → Date32
//	Timestamp(µs|ns,"UTC")          → DateTime64(6|9,'UTC')
//	Timestamp(µs|ns,"")             → DateTime64(6|9)  (timezone-naive)
//	List/LargeList/FixedSizeList(T) → Array(<T>)
//	Struct(f T, …)                  → Tuple(`f` <T>, …)
//	Map(K,V)                        → Map(<K>, <V>)
//
// A nullable Arrow field maps to Nullable(T), but only for a scalar leaf:
// ClickHouse forbids Nullable(Array)/Nullable(Tuple)/Nullable(Map), and its
// ArrowStream reader coerces a null container to an empty/default one, so
// container-level nullability is dropped (verified against clickhouse-local
// 26.6). Dictionaries, unions, large/view string variants, and a timestamp
// with a non-UTC/non-empty zone or a coarser-than-µs unit are rejected.
func StructureFor(schema *arrow.Schema) (structure string, err error) {
	if schema == nil {
		return "", eh.Errorf("adhocdata: nil schema")
	}
	fields := schema.Fields()
	if len(fields) == 0 {
		return "", eh.Errorf("adhocdata: schema has no columns")
	}
	var b strings.Builder
	for i, f := range fields {
		if nErr := checkColumnName(f.Name, f.Name); nErr != nil {
			return "", nErr
		}
		chType, tErr := chTypeFor(f.Type, f.Nullable, f.Name)
		if tErr != nil {
			return "", tErr
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(quoteIdent(f.Name))
		b.WriteByte(' ')
		b.WriteString(chType)
	}
	return b.String(), nil
}

// chTypeFor maps one Arrow type at column position colName to its
// ClickHouse type. A nested type maps recursively; a scalar leaf wraps in
// Nullable when the Arrow field is nullable. colName names the top-level
// column in errors so an unsupported deeply-nested type is still
// attributable. Container-level nullability (a nullable list/struct/map) is
// not representable in ClickHouse and is dropped by the recursion — its
// reader coerces a null container to an empty one.
func chTypeFor(dt arrow.DataType, nullable bool, colName string) (chType string, err error) {
	switch dt.ID() {
	case arrow.LIST, arrow.LARGE_LIST, arrow.FIXED_SIZE_LIST:
		return arrayTypeFor(dt, colName)
	case arrow.STRUCT:
		return tupleTypeFor(dt, colName)
	case arrow.MAP:
		return mapTypeFor(dt, colName)
	}
	scalar, sErr := scalarTypeFor(dt, colName)
	if sErr != nil {
		return "", sErr
	}
	if nullable {
		return "Nullable(" + scalar + ")", nil
	}
	return scalar, nil
}

// arrayTypeFor maps a list-like Arrow type (List, LargeList, FixedSizeList)
// to Array(<elem>). The list's own nullability is not representable in
// ClickHouse (there is no Nullable(Array)); its ArrowStream reader coerces a
// null list to an empty one, so only the element type and element
// nullability carry through.
func arrayTypeFor(dt arrow.DataType, colName string) (chType string, err error) {
	ll, ok := dt.(arrow.ListLikeType)
	if !ok {
		return "", eh.Errorf("adhocdata: column %q: list type %s exposes no element field", colName, dt)
	}
	elem := ll.ElemField()
	inner, err := chTypeFor(elem.Type, elem.Nullable, colName)
	if err != nil {
		return "", err
	}
	return "Array(" + inner + ")", nil
}

// tupleTypeFor maps an Arrow struct to a named ClickHouse Tuple. Each field
// name is backtick-quoted, so a nested colon-laden name survives; the
// struct's own nullability is dropped (ClickHouse forbids Nullable(Tuple)).
func tupleTypeFor(dt arrow.DataType, colName string) (chType string, err error) {
	st, ok := dt.(*arrow.StructType)
	if !ok {
		return "", eh.Errorf("adhocdata: column %q: struct type assertion failed", colName)
	}
	fields := st.Fields()
	if len(fields) == 0 {
		return "", eh.Errorf("adhocdata: column %q: an empty struct has no ClickHouse Tuple representation", colName)
	}
	var b strings.Builder
	b.WriteString("Tuple(")
	for i, f := range fields {
		if nErr := checkColumnName(f.Name, colName); nErr != nil {
			return "", nErr
		}
		inner, tErr := chTypeFor(f.Type, f.Nullable, colName)
		if tErr != nil {
			return "", tErr
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(quoteIdent(f.Name))
		b.WriteByte(' ')
		b.WriteString(inner)
	}
	b.WriteByte(')')
	return b.String(), nil
}

// mapTypeFor maps an Arrow map to Map(<key>, <value>). Arrow map keys are
// non-nullable by construction; the value maps recursively with its own
// nullability. The map's own nullability is dropped (no Nullable(Map)).
func mapTypeFor(dt arrow.DataType, colName string) (chType string, err error) {
	mt, ok := dt.(*arrow.MapType)
	if !ok {
		return "", eh.Errorf("adhocdata: column %q: map type assertion failed", colName)
	}
	key, kErr := chTypeFor(mt.KeyType(), false, colName)
	if kErr != nil {
		return "", kErr
	}
	item := mt.ItemField()
	val, vErr := chTypeFor(item.Type, item.Nullable, colName)
	if vErr != nil {
		return "", vErr
	}
	return "Map(" + key + ", " + val + ")", nil
}

// scalarTypeFor maps a leaf (non-nested) Arrow type to its ClickHouse scalar
// type, or errors naming colName. Nullability is applied by the caller.
func scalarTypeFor(dt arrow.DataType, colName string) (chType string, err error) {
	switch dt.ID() {
	case arrow.STRING:
		chType = "String"
	case arrow.BINARY:
		chType = "String"
	case arrow.BOOL:
		chType = "Bool"
	case arrow.INT8:
		chType = "Int8"
	case arrow.INT16:
		chType = "Int16"
	case arrow.INT32:
		chType = "Int32"
	case arrow.INT64:
		chType = "Int64"
	case arrow.UINT8:
		chType = "UInt8"
	case arrow.UINT16:
		chType = "UInt16"
	case arrow.UINT32:
		chType = "UInt32"
	case arrow.UINT64:
		chType = "UInt64"
	case arrow.FLOAT32:
		chType = "Float32"
	case arrow.FLOAT64:
		chType = "Float64"
	case arrow.DATE32:
		chType = "Date32"
	case arrow.FIXED_SIZE_BINARY:
		fsb, ok := dt.(*arrow.FixedSizeBinaryType)
		if !ok {
			return "", eh.Errorf("adhocdata: column %q: fixed-size binary type assertion failed", colName)
		}
		chType = "FixedString(" + strconv.Itoa(fsb.ByteWidth) + ")"
	case arrow.TIMESTAMP:
		ts, ok := dt.(*arrow.TimestampType)
		if !ok {
			return "", eh.Errorf("adhocdata: column %q: timestamp type assertion failed", colName)
		}
		var prec string
		switch ts.Unit {
		case arrow.Microsecond:
			prec = "6"
		case arrow.Nanosecond:
			prec = "9"
		default:
			return "", eh.Errorf("adhocdata: column %q: timestamp unit must be microsecond or nanosecond", colName)
		}
		// A UTC zone round-trips as an explicit tz; an empty zone is
		// timezone-naive and maps to a bare DateTime64(N). Fabricating a UTC
		// zone the Arrow schema does not carry would misrepresent it — the
		// epoch value is identical, only the display zone differs. Any other
		// named zone is refused.
		switch ts.TimeZone {
		case "UTC":
			chType = "DateTime64(" + prec + ",'UTC')"
		case "":
			chType = "DateTime64(" + prec + ")"
		default:
			return "", eh.Errorf("adhocdata: column %q: timestamp timezone must be UTC or empty (naive), got %q", colName, ts.TimeZone)
		}
	default:
		return "", eh.Errorf("adhocdata: column %q: arrow type %s is not in the supported set", colName, dt)
	}
	return
}

// checkColumnName rejects an empty or over-long identifier (a top-level
// column or a nested Tuple field name), naming col — the enclosing top-level
// column — for context. Every other byte is legal because the name is
// backtick-quoted (quoteIdent), so the bounded set of physical names a
// leeway columnar schema carries passes unchanged.
func checkColumnName(name, col string) (err error) {
	if name == "" {
		return eh.Errorf("adhocdata: column %q: a column or nested field name may not be empty", col)
	}
	if len(name) > maxColumnNameLen {
		return eh.Errorf("adhocdata: column %q: name %q exceeds %d bytes", col, name, maxColumnNameLen)
	}
	return nil
}

// quoteIdent backtick-quotes a ClickHouse identifier, doubling any embedded
// backtick, so a name carrying colons, dashes, or spaces is carried verbatim
// into the structure string. The structure string is itself wrapped as a
// single-quoted SQL literal downstream (the fifo file() read and the url()
// rewrite), which escapes the quote and backslash bytes; a backtick is not
// special in that literal, so the two escaping layers do not interfere.
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// validColumnName reports whether name is a bare ClickHouse identifier —
// `[A-Za-z_][A-Za-z0-9_]*`, up to maxBareIdentLen bytes — safe to
// interpolate unquoted. Column names in the structure string no longer need
// this (they are backtick-quoted); it now guards the dataset alias and
// handle, which stay bare so they can name a TEMPORARY table and a
// frontmatter binding without quoting (ADR-0134 SD2/SD4).
func validColumnName(name string) (ok bool) {
	if name == "" || len(name) > maxBareIdentLen {
		return
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		valid := c == '_' ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(i > 0 && c >= '0' && c <= '9')
		if !valid {
			return
		}
	}
	ok = true
	return
}
