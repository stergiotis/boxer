package goplan

// Plain (entity-header) columns map 1:1 onto the leeway entity-builder
// setters: `id`(+`naturalKey`) → SetId, `ts` → SetTimestamp, `expiresAt`
// → SetLifecycle. The setter *names* are the stable leeway entity
// contract; only the argument *types* vary per table, and those are
// taken verbatim from the DTO's plain-field Go types (strict 1:1 — the
// codec inserts no conversions). This file is the single source of truth
// for which Go types a plain column may carry and the Arrow array type
// the read side projects them from.

// PlainArrowArrayType maps a plain-column Go type (source form, e.g.
// "uint64", "time.Time", "[16]byte") to the Arrow array type the read
// side reads it from (e.g. "array.Uint64"). ok is false for any type not
// supported as a plain column.
//
// time.Time maps to array.Timestamp: Arrow has no native time.Time, so
// the value is stored as int64 nanos and reconstructed on read — that
// conversion is Arrow's physical representation, not a DTO convenience.
func PlainArrowArrayType(goType string) (arrowType string, ok bool) {
	switch goType {
	case "uint8":
		return "array.Uint8", true
	case "uint16":
		return "array.Uint16", true
	case "uint32":
		return "array.Uint32", true
	case "uint64":
		return "array.Uint64", true
	case "int8":
		return "array.Int8", true
	case "int16":
		return "array.Int16", true
	case "int32":
		return "array.Int32", true
	case "int64":
		return "array.Int64", true
	case "float32":
		return "array.Float32", true
	case "float64":
		return "array.Float64", true
	case "bool":
		return "array.Boolean", true
	case "string":
		return "array.String", true
	case "[]byte":
		return "array.Binary", true
	case "time.Time":
		return "array.Timestamp", true
	}
	if IsFixedByteArray(goType) {
		return "array.FixedSizeBinary", true
	}
	return "", false
}

// IsSupportedPlainType reports whether goType may be used for a plain
// (entity-header) column.
func IsSupportedPlainType(goType string) bool {
	_, ok := PlainArrowArrayType(goType)
	return ok
}
