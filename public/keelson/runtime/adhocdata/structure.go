package adhocdata

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// maxColumnNameLen bounds a column identifier so the generated
// structure string needs neither quoting nor escaping — the same
// discipline the chlocal InputTables prelude enforces on table names.
const maxColumnNameLen = 64

// StructureFor renders the ClickHouse structure string
// (`name Type, name Type`) that a `file(fifo,'ArrowStream',<structure>)`
// read requires, because schema inference over a pipe is impossible
// (ADR-0134 SD3). The mapping is total over the bounded type set the
// publish gate admits and rejects everything else, naming the offending
// column — so an unsupported type is refused at publish, never
// discovered at query time.
//
// Supported Arrow types → ClickHouse:
//
//	Utf8, Binary        → String
//	Bool                → Bool
//	Int8/16/32/64       → Int8/16/32/64
//	Uint8/16/32/64      → UInt8/16/32/64
//	Float32/64          → Float32/64
//	Date32              → Date32
//	Timestamp(µs,"UTC") → DateTime64(6,'UTC')
//	Timestamp(ns,"UTC") → DateTime64(9,'UTC')
//
// Nullable fields, lists, structs, dictionaries, large/view variants,
// and non-UTC or other-unit timestamps are rejected.
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
		if !validColumnName(f.Name) {
			return "", eh.Errorf("adhocdata: column %q: name is not a bare identifier ([A-Za-z_][A-Za-z0-9_]*, <=%d bytes)", f.Name, maxColumnNameLen)
		}
		chType, terr := chTypeFor(f)
		if terr != nil {
			return "", terr
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(f.Name)
		b.WriteByte(' ')
		b.WriteString(chType)
	}
	return b.String(), nil
}

// chTypeFor maps one Arrow field to its ClickHouse type, or errors
// naming the field.
func chTypeFor(f arrow.Field) (chType string, err error) {
	if f.Nullable {
		return "", eh.Errorf("adhocdata: column %q: nullable fields are not supported", f.Name)
	}
	switch f.Type.ID() {
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
	case arrow.TIMESTAMP:
		ts, ok := f.Type.(*arrow.TimestampType)
		if !ok {
			return "", eh.Errorf("adhocdata: column %q: timestamp type assertion failed", f.Name)
		}
		if ts.TimeZone != "UTC" {
			return "", eh.Errorf("adhocdata: column %q: timestamp timezone must be UTC, got %q", f.Name, ts.TimeZone)
		}
		switch ts.Unit {
		case arrow.Microsecond:
			chType = "DateTime64(6,'UTC')"
		case arrow.Nanosecond:
			chType = "DateTime64(9,'UTC')"
		default:
			return "", eh.Errorf("adhocdata: column %q: timestamp unit must be microsecond or nanosecond", f.Name)
		}
	default:
		return "", eh.Errorf("adhocdata: column %q: arrow type %s is not in the supported set", f.Name, f.Type)
	}
	return
}

// validColumnName reports whether name is a bare ClickHouse identifier
// safe to interpolate into a structure string: `[A-Za-z_][A-Za-z0-9_]*`,
// up to maxColumnNameLen bytes. It mirrors the chlocal input-table name
// rule so a schema that passes here also survives the broker prelude.
func validColumnName(name string) (ok bool) {
	if name == "" || len(name) > maxColumnNameLen {
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
